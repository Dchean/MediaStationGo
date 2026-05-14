// Package service — RSS subscriptions for automated downloads.
//
// SubscriptionService periodically polls every Subscription row, fetches
// the configured RSS / Atom feed, and queues new items into the
// DownloadService. Items are deduplicated by GUID stored as a Setting key
// "subscription.<id>.last_guid" so the same episode is never re-queued.
package service

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SubscriptionService runs the polling loop.
type SubscriptionService struct {
	log       *zap.Logger
	repo      *repository.Container
	downloads *DownloadService
	hub       *Hub
	stop      chan struct{}
}

// NewSubscriptionService is the constructor.
func NewSubscriptionService(log *zap.Logger, repo *repository.Container, downloads *DownloadService, hub *Hub) *SubscriptionService {
	return &SubscriptionService{
		log: log, repo: repo, downloads: downloads, hub: hub,
		stop: make(chan struct{}),
	}
}

// Start runs the polling loop in the background.
func (s *SubscriptionService) Start(ctx context.Context) {
	go s.loop(ctx)
}

// Stop shuts the loop down.
func (s *SubscriptionService) Stop() { close(s.stop) }

// rssFeed is the minimal RSS subset we need to decode.
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			GUID        string `xml:"guid"`
			Description string `xml:"description"`
			Enclosure   struct {
				URL string `xml:"url,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

// Create persists a new subscription.
func (s *SubscriptionService) Create(ctx context.Context, sub *model.Subscription) error {
	if sub.Name == "" || sub.FeedURL == "" {
		return errors.New("name and feed_url required")
	}
	return s.repo.Subscription.Create(ctx, sub)
}

// List returns every subscription rule.
func (s *SubscriptionService) List(ctx context.Context) ([]model.Subscription, error) {
	return s.repo.Subscription.List(ctx)
}

// Delete removes a subscription.
func (s *SubscriptionService) Delete(ctx context.Context, id string) error {
	return s.repo.DB.Where("id = ?", id).Delete(&model.Subscription{}).Error
}

// RunNow forces a poll for one subscription, ignoring its schedule. Used
// by the admin UI's "test now" button.
func (s *SubscriptionService) RunNow(ctx context.Context, id string) (int, error) {
	var sub model.Subscription
	if err := s.repo.DB.Where("id = ?", id).First(&sub).Error; err != nil {
		return 0, err
	}
	return s.runOne(ctx, &sub)
}

// loop polls every 10 minutes.
func (s *SubscriptionService) loop(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	// First run shortly after startup.
	first := time.NewTimer(30 * time.Second)
	defer first.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-first.C:
		case <-t.C:
		}
		s.runAll(ctx)
	}
}

func (s *SubscriptionService) runAll(ctx context.Context) {
	subs, err := s.repo.Subscription.List(ctx)
	if err != nil {
		s.log.Warn("subscription list failed", zap.Error(err))
		return
	}
	for i := range subs {
		if !subs[i].Enabled {
			continue
		}
		if n, err := s.runOne(ctx, &subs[i]); err != nil {
			s.log.Warn("subscription run failed",
				zap.String("name", subs[i].Name), zap.Error(err))
		} else if n > 0 {
			s.log.Info("subscription queued items",
				zap.String("name", subs[i].Name), zap.Int("count", n))
		}
	}
}

func (s *SubscriptionService) runOne(ctx context.Context, sub *model.Subscription) (int, error) {
	feed, err := s.fetch(ctx, sub.FeedURL)
	if err != nil {
		return 0, err
	}

	filter := compileFilter(sub.Filter)
	guidKey := fmt.Sprintf("subscription.%s.seen", sub.ID)
	seenRaw, _ := s.repo.Setting.Get(ctx, guidKey)
	seen := splitNonEmpty(seenRaw)
	seenSet := make(map[string]struct{}, len(seen))
	for _, g := range seen {
		seenSet[g] = struct{}{}
	}

	queued := 0
	for _, item := range feed.Channel.Items {
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}
		if _, ok := seenSet[guid]; ok {
			continue
		}
		if filter != nil && !filter.MatchString(item.Title) {
			continue
		}
		download := item.Enclosure.URL
		if download == "" {
			download = item.Link
		}
		if download == "" {
			continue
		}
		if _, err := s.downloads.AddDownload(ctx, sub.UserID, download, ""); err != nil {
			s.log.Warn("subscription enqueue failed",
				zap.String("title", item.Title), zap.Error(err))
			continue
		}
		queued++
		seen = append(seen, guid)
	}
	// Remember the last 200 GUIDs so the seen set doesn't grow forever.
	if len(seen) > 200 {
		seen = seen[len(seen)-200:]
	}
	_ = s.repo.Setting.Set(ctx, guidKey, strings.Join(seen, "\n"))

	now := time.Now()
	_ = s.repo.DB.Model(sub).Updates(map[string]any{"last_run_at": &now}).Error
	if queued > 0 {
		s.hub.Publish("subscription", map[string]any{
			"id":     sub.ID,
			"name":   sub.Name,
			"queued": queued,
		})
	}
	return queued, nil
}

func (s *SubscriptionService) fetch(ctx context.Context, feedURL string) (*rssFeed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MediaStationGo/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rss %s: %d", feedURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var f rssFeed
	if err := xml.Unmarshal(body, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func compileFilter(pat string) *regexp.Regexp {
	pat = strings.TrimSpace(pat)
	if pat == "" {
		return nil
	}
	if r, err := regexp.Compile("(?i)" + pat); err == nil {
		return r
	}
	return nil
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0)
	for _, p := range strings.Split(s, "\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Package service — RSS subscriptions for automated downloads.
//
// SubscriptionService periodically polls every Subscription row, fetches
// the configured RSS / Atom feed, and queues new items into the
// DownloadService. Items are deduplicated by GUID stored as a Setting key
// "subscription.<id>.last_guid" so the same episode is never re-queued.
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SubscriptionService runs the polling loop.
type SubscriptionService struct {
	cfg       *config.Config
	log       *zap.Logger
	repo      *repository.Container
	downloads *DownloadService
	site      *SiteService
	scraper   *ScraperService
	hub       *Hub
	notify    *NotifyChannelService
	mu        sync.Mutex
	stop      chan struct{}
	running   bool
}

const (
	defaultSubscriptionPollInterval = 3 * time.Hour
	minSubscriptionPollInterval     = 3 * time.Hour
	subscriptionStartupDelay        = defaultSubscriptionPollInterval
)

type rssSubscriptionRunState struct {
	seen              []string
	seenSet           map[string]struct{}
	availability      LocalAvailability
	availabilityQuery string
	washOff           bool
}

// NewSubscriptionService is the constructor.
func NewSubscriptionService(cfg *config.Config, log *zap.Logger, repo *repository.Container, downloads *DownloadService, site *SiteService, hub *Hub) *SubscriptionService {
	return &SubscriptionService{
		cfg:       cfg,
		log:       log,
		repo:      repo,
		downloads: downloads,
		site:      site,
		hub:       hub,
	}
}

func (s *SubscriptionService) SetScraper(scraper *ScraperService) {
	s.scraper = scraper
}

func (s *SubscriptionService) SetNotifyChannels(notify *NotifyChannelService) {
	s.notify = notify
}

// Start runs the polling loop in the background.
func (s *SubscriptionService) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.stop = stop
	s.running = true
	s.mu.Unlock()
	go s.loop(ctx, stop)
}

// Stop shuts the loop down.
func (s *SubscriptionService) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stop := s.stop
	s.stop = nil
	s.running = false
	s.mu.Unlock()
	close(stop)
}

// Create persists a new subscription.
func (s *SubscriptionService) Create(ctx context.Context, sub *model.Subscription) error {
	if sub.Name == "" || sub.FeedURL == "" {
		return errors.New("name and feed_url required")
	}
	normalizeSubscriptionDefaults(sub)
	enabled := sub.Enabled
	if err := s.repo.Subscription.Create(ctx, sub); err != nil {
		return err
	}
	if !enabled {
		if err := s.repo.DB.WithContext(ctx).Model(sub).Update("enabled", false).Error; err != nil {
			return err
		}
		sub.Enabled = false
	}
	return nil
}

func normalizeSubscriptionDefaults(sub *model.Subscription) {
	if strings.TrimSpace(sub.SearchMode) == "" {
		sub.SearchMode = "keyword"
	}
	if strings.TrimSpace(sub.Resolution) == "" {
		sub.Resolution = "best"
	}
	if strings.TrimSpace(sub.WashPriority) == "" {
		sub.WashPriority = "balanced"
	}
	if sub.Priority == 0 {
		sub.Priority = 50
	}
}

// List returns every subscription rule.
func (s *SubscriptionService) List(ctx context.Context) ([]model.Subscription, error) {
	return s.repo.Subscription.List(ctx)
}

// Delete removes a subscription.
func (s *SubscriptionService) Delete(ctx context.Context, id string) error {
	var sub model.Subscription
	if err := s.repo.DB.WithContext(ctx).Where("id = ?", id).First(&sub).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return s.repo.DB.WithContext(ctx).Where("id = ?", id).Delete(&model.Subscription{}).Error
	}
	if err := s.deleteSubscriptionDownloads(ctx, &sub); err != nil {
		return err
	}
	if s.repo.Setting != nil {
		_ = s.repo.Setting.Delete(ctx, fmt.Sprintf("subscription.%s.seen", id))
	}
	return s.repo.DB.Where("id = ?", id).Delete(&model.Subscription{}).Error
}

// RunNow forces a poll for one subscription, ignoring its schedule. Used
// by the admin UI's "test now" button.
func (s *SubscriptionService) RunNow(ctx context.Context, id string) (int, error) {
	var sub model.Subscription
	if err := s.repo.DB.Where("id = ?", id).First(&sub).Error; err != nil {
		return 0, err
	}
	if sub.ArchivedAt != nil {
		return 0, nil
	}
	return s.runOne(ctx, &sub)
}

// loop polls subscription feeds and site-search subscriptions at a conservative
// cadence so tracker APIs are not hammered by every alias keyword.
func (s *SubscriptionService) loop(ctx context.Context, stop <-chan struct{}) {
	defer s.markLoopStopped(stop)
	interval := s.pollInterval(ctx)
	delay := subscriptionStartupDelay
	if interval < delay {
		delay = interval
	}
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-stop:
			timer.Stop()
			return
		case <-timer.C:
		}
		s.runAll(ctx)
		// Re-read after every run so changes from the settings page take effect
		// without restarting the service.
		delay = s.pollInterval(ctx)
	}
}

func (s *SubscriptionService) markLoopStopped(stop <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop == stop {
		s.stop = nil
		s.running = false
	}
}

func (s *SubscriptionService) pollInterval(ctx context.Context) time.Duration {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return defaultSubscriptionPollInterval
	}
	raw, err := s.repo.Setting.Get(ctx, "subscription.interval_seconds")
	if err != nil {
		return defaultSubscriptionPollInterval
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || seconds <= 0 {
		return defaultSubscriptionPollInterval
	}
	interval := time.Duration(seconds) * time.Second
	if interval < minSubscriptionPollInterval {
		return minSubscriptionPollInterval
	}
	return interval
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
			if subscriptionSiteSearchShouldStopOnError(err) {
				s.log.Warn("subscription sweep stopped after upstream failure",
					zap.String("name", subs[i].Name), zap.Error(err))
				return
			}
		} else if n > 0 {
			s.log.Info("subscription queued items",
				zap.String("name", subs[i].Name), zap.Int("count", n))
		}
	}
}

func (s *SubscriptionService) runOne(ctx context.Context, sub *model.Subscription) (int, error) {
	s.prepareSubscriptionForRun(ctx, sub)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sub.FeedURL)), "site-search://") {
		return s.runSiteSearch(ctx, sub)
	}

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

	s.updateSubscriptionTotalEpisodes(ctx, sub, s.resolveSubscriptionTotalEpisodes(ctx, sub, inferRSSTotalEpisodes(feed.Channel.Items, sub, filter)))
	// RSS 和站点搜索统一使用候选规划：先按订阅规则过滤，再按洗版优先级/集数去重择优。
	// 非洗版订阅成功下载一次即满足，媒体库与下载中任务会作为可用性输入避免重复下载。
	runState := &rssSubscriptionRunState{
		seen:              seen,
		seenSet:           seenSet,
		availability:      mergeLocalAvailability(SubscriptionLocalAvailability(ctx, s.repo, sub), s.pendingDownloadAvailability(ctx, sub)),
		availabilityQuery: availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)),
		washOff:           !sub.WashEnabled,
	}
	candidates := selectRSSSubscriptionCandidates(feed.Channel.Items, sub, filter, runState.seenSet, runState.availability)
	queued := s.enqueueRSSSubscriptionCandidates(ctx, sub, candidates, runState)
	s.finishRSSSubscriptionRun(ctx, sub, guidKey, runState, queued)
	return queued, nil
}

func (s *SubscriptionService) enqueueRSSSubscriptionCandidates(ctx context.Context, sub *model.Subscription, candidates []siteSearchCandidate, state *rssSubscriptionRunState) int {
	queued := 0
	for _, candidate := range candidates {
		if s.enqueueRSSSubscriptionCandidate(ctx, sub, candidate, state) {
			queued++
		}
	}
	return queued
}

func (s *SubscriptionService) enqueueRSSSubscriptionCandidate(ctx context.Context, sub *model.Subscription, candidate siteSearchCandidate, state *rssSubscriptionRunState) bool {
	item := candidate.Item
	mediaType, mediaCategory := s.classifySubscriptionItem(ctx, sub, item.Title, "")
	savePath := s.resolveSubscriptionSavePath(ctx, sub, mediaType, mediaCategory)
	if s.downloadPathHasCandidate(ctx, sub, item.Title, savePath) {
		state.markTitleAvailable(item.Title)
		return false
	}
	if _, err := s.downloads.AddDownloadWithMeta(ctx, sub.UserID, candidate.Download, savePath, DownloadTaskMeta{
		SubscriptionID:       sub.ID,
		Title:                firstNonEmpty(item.Title, sub.Name),
		PosterURL:            sub.PosterURL,
		BackdropURL:          sub.BackdropURL,
		Overview:             sub.Overview,
		MediaType:            mediaType,
		MediaCategory:        mediaCategory,
		AllowExistingLibrary: sub.WashEnabled,
	}); err != nil {
		if IsDownloadDedupError(err) {
			state.markSeen(candidate.GUID)
			return false
		}
		s.log.Warn("subscription enqueue failed",
			zap.String("title", item.Title),
			zap.String("media_type", mediaType),
			zap.String("media_category", mediaCategory),
			zap.String("save_path", savePath),
			zap.Error(err))
		return false
	}
	state.markTitleAvailable(item.Title)
	state.markSeen(candidate.GUID)
	return true
}

func (s *SubscriptionService) finishRSSSubscriptionRun(ctx context.Context, sub *model.Subscription, guidKey string, state *rssSubscriptionRunState, queued int) {
	state.availability = s.finalizePendingAvailability(sub, state.availability)
	// Remember the last 200 GUIDs so the seen set doesn't grow forever.
	if len(state.seen) > 200 {
		state.seen = state.seen[len(state.seen)-200:]
	}
	_ = s.repo.Setting.Set(ctx, guidKey, strings.Join(state.seen, "\n"))

	now := time.Now()
	_ = s.repo.DB.Model(sub).Updates(map[string]any{"last_run_at": &now}).Error
	_ = s.archiveCompletedSubscription(ctx, sub, state.availability)
	if queued > 0 {
		s.hub.Publish("subscription", map[string]any{
			"id":     sub.ID,
			"name":   sub.Name,
			"queued": queued,
		})
		s.notifySubscriptionHit(sub, queued, nil)
	}
}

func (state *rssSubscriptionRunState) markTitleAvailable(title string) {
	if state.washOff {
		addAvailabilityTitle(title, state.availabilityQuery, &state.availability)
	}
}

func (state *rssSubscriptionRunState) markSeen(guid string) {
	state.seen = append(state.seen, guid)
	if state.seenSet != nil {
		state.seenSet[guid] = struct{}{}
	}
}

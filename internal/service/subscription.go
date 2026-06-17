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
	"net/url"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"

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
	stop      chan struct{}
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
		stop:      make(chan struct{}),
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
	go s.loop(ctx)
}

// Stop shuts the loop down.
func (s *SubscriptionService) Stop() { close(s.stop) }

// rssFeed is the minimal RSS subset we need to decode.
type rssFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Enclosure   struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
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

// History returns completed/archived subscription rules.
func (s *SubscriptionService) History(ctx context.Context) ([]model.Subscription, error) {
	return s.repo.Subscription.History(ctx)
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
	if sub.ArchivedAt != nil {
		return 0, nil
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

	washOff := !sub.WashEnabled
	availQuery := availabilityQuery(subscriptionName(sub), subscriptionFilter(sub))
	// RSS 和站点搜索统一使用候选规划：先按订阅规则过滤，再按洗版优先级/集数去重择优。
	// 非洗版订阅成功下载一次即满足，媒体库与下载中任务会作为可用性输入避免重复下载。
	avail := mergeLocalAvailability(
		SubscriptionLocalAvailability(ctx, s.repo, sub),
		s.pendingDownloadAvailability(ctx, sub),
	)
	candidates := selectRSSSubscriptionCandidates(feed.Channel.Items, sub, filter, seenSet, avail)

	queued := 0
	for _, candidate := range candidates {
		item := candidate.Item
		guid := candidate.GUID
		download := candidate.Download
		mediaType, mediaCategory := s.classifySubscriptionItem(ctx, sub, item.Title, "")
		savePath := s.resolveSubscriptionSavePath(ctx, sub, mediaType, mediaCategory)
		if s.downloadPathHasCandidate(ctx, sub, item.Title, savePath) {
			if washOff {
				addAvailabilityTitle(item.Title, availQuery, &avail)
			}
			continue
		}
		if _, err := s.downloads.AddDownloadWithMeta(ctx, sub.UserID, download, savePath, DownloadTaskMeta{
			Title:                firstNonEmpty(item.Title, sub.Name),
			PosterURL:            sub.PosterURL,
			BackdropURL:          sub.BackdropURL,
			Overview:             sub.Overview,
			MediaType:            mediaType,
			MediaCategory:        mediaCategory,
			AllowExistingLibrary: sub.WashEnabled,
		}); err != nil {
			if IsDownloadDedupError(err) {
				if washOff {
					addAvailabilityTitle(item.Title, availQuery, &avail)
				}
				seen = append(seen, guid)
				seenSet[guid] = struct{}{}
				continue
			}
			s.log.Warn("subscription enqueue failed",
				zap.String("title", item.Title),
				zap.String("media_type", mediaType),
				zap.String("media_category", mediaCategory),
				zap.String("save_path", savePath),
				zap.Error(err))
			continue
		}
		if washOff {
			addAvailabilityTitle(item.Title, availQuery, &avail)
		}
		queued++
		seen = append(seen, guid)
		seenSet[guid] = struct{}{}
	}
	avail = s.finalizePendingAvailability(sub, avail)
	// Remember the last 200 GUIDs so the seen set doesn't grow forever.
	if len(seen) > 200 {
		seen = seen[len(seen)-200:]
	}
	_ = s.repo.Setting.Set(ctx, guidKey, strings.Join(seen, "\n"))

	now := time.Now()
	_ = s.repo.DB.Model(sub).Updates(map[string]any{"last_run_at": &now}).Error
	_ = s.archiveCompletedSubscription(ctx, sub, avail)
	if queued > 0 {
		s.hub.Publish("subscription", map[string]any{
			"id":     sub.ID,
			"name":   sub.Name,
			"queued": queued,
		})
		s.notifySubscriptionHit(sub, queued, nil)
	}
	return queued, nil
}

func (s *SubscriptionService) runSiteSearch(ctx context.Context, sub *model.Subscription) (int, error) {
	if s.site == nil {
		return 0, errors.New("site search service unavailable")
	}
	keyword := siteSearchKeyword(sub)
	if keyword == "" {
		return 0, errors.New("site-search subscription keyword required")
	}

	results, err := s.site.Search(ctx, keyword)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		now := time.Now()
		_ = s.repo.DB.Model(sub).Updates(map[string]any{"last_run_at": &now}).Error
		return 0, nil
	}

	guidKey := fmt.Sprintf("subscription.%s.seen", sub.ID)
	seenRaw, _ := s.repo.Setting.Get(ctx, guidKey)
	seen := splitNonEmpty(seenRaw)
	seenSet := make(map[string]struct{}, len(seen))
	for _, g := range seen {
		seenSet[g] = struct{}{}
	}

	availability := mergeLocalAvailability(
		SubscriptionLocalAvailability(ctx, s.repo, sub),
		s.pendingDownloadAvailability(ctx, sub),
	)
	candidates := selectSiteSearchCandidates(results, sub, seenSet, availability)
	var lastEnqueueErr error
	queued := 0
	var resources []string
	for _, candidate := range candidates {
		item := candidate.Item
		mediaType, mediaCategory := s.classifySubscriptionItem(ctx, sub, item.Title, item.Category)
		if s.shouldSkipExistingTorrent(ctx, mediaType, candidate) {
			addAvailabilityTitle(item.Title, availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)), &availability)
			seen = append(seen, candidate.GUID)
			seenSet[candidate.GUID] = struct{}{}
			continue
		}
		realURL := s.site.ResolveDownloadURL(ctx, candidate.Download)
		savePath := s.resolveSubscriptionSavePath(ctx, sub, mediaType, mediaCategory)
		if s.downloadPathHasCandidate(ctx, sub, candidate.Item.Title, savePath) {
			addAvailabilityTitle(item.Title, availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)), &availability)
			seen = append(seen, candidate.GUID)
			seenSet[candidate.GUID] = struct{}{}
			continue
		}
		if _, err := s.downloads.AddDownloadWithMeta(ctx, sub.UserID, realURL, savePath, DownloadTaskMeta{
			Title:                firstNonEmpty(item.Title, sub.Name),
			PosterURL:            sub.PosterURL,
			BackdropURL:          sub.BackdropURL,
			Overview:             sub.Overview,
			MediaType:            mediaType,
			MediaCategory:        mediaCategory,
			SourceCategory:       item.Category,
			AllowExistingLibrary: sub.WashEnabled,
		}); err != nil {
			if IsDownloadDedupError(err) {
				addAvailabilityTitle(item.Title, availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)), &availability)
				seen = append(seen, candidate.GUID)
				seenSet[candidate.GUID] = struct{}{}
				continue
			}
			lastEnqueueErr = err
			s.log.Warn("site-search subscription enqueue failed",
				zap.String("subscription", sub.Name),
				zap.String("title", item.Title),
				zap.String("site_category", item.Category),
				zap.String("media_type", mediaType),
				zap.String("media_category", mediaCategory),
				zap.String("save_path", savePath),
				zap.Error(err))
			continue
		}
		queued++
		addAvailabilityTitle(item.Title, availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)), &availability)
		resources = append(resources, item.Title)
		seen = append(seen, candidate.GUID)
		seenSet[candidate.GUID] = struct{}{}
	}
	availability = s.finalizePendingAvailability(sub, availability)
	if len(seen) > 200 {
		seen = seen[len(seen)-200:]
	}
	_ = s.repo.Setting.Set(ctx, guidKey, strings.Join(seen, "\n"))
	now := time.Now()
	_ = s.repo.DB.Model(sub).Updates(map[string]any{"last_run_at": &now}).Error
	_ = s.archiveCompletedSubscription(ctx, sub, availability)
	if queued > 0 {
		s.hub.Publish("subscription", map[string]any{
			"id":        sub.ID,
			"name":      sub.Name,
			"queued":    queued,
			"keyword":   keyword,
			"resources": resources,
		})
		s.notifySubscriptionHit(sub, queued, resources)
		return queued, nil
	}
	if lastEnqueueErr != nil {
		return 0, fmt.Errorf("找到 PT 资源但加入下载器失败: %w", lastEnqueueErr)
	}
	return 0, nil
}

func (s *SubscriptionService) notifySubscriptionHit(sub *model.Subscription, queued int, resources []string) {
	if s == nil || s.notify == nil || sub == nil || queued <= 0 {
		return
	}
	body := fmt.Sprintf("订阅：%s\n新增资源：%d", sub.Name, queued)
	if len(resources) > 0 {
		body += "\n资源：\n- " + strings.Join(resources, "\n- ")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.notify.Broadcast(ctx, "MediaStationGo 订阅命中新资源", body, EventSubscriptionHit)
	}()
}

func (s *SubscriptionService) archiveCompletedSubscription(ctx context.Context, sub *model.Subscription, availability LocalAvailability) error {
	if s == nil || s.repo == nil || s.repo.Subscription == nil || sub == nil {
		return nil
	}
	if !subscriptionShouldArchive(sub, availability) {
		return nil
	}
	now := time.Now()
	reason := subscriptionArchiveReason(sub, availability)
	if err := s.repo.Subscription.Archive(ctx, sub.ID, reason, now); err != nil {
		return err
	}
	sub.Enabled = false
	sub.ArchivedAt = &now
	sub.ArchiveReason = reason
	if s.log != nil {
		s.log.Info("subscription completed, moved to history",
			zap.String("id", sub.ID),
			zap.String("name", sub.Name),
			zap.String("reason", reason))
	}
	if s.hub != nil {
		s.hub.Publish("subscription", map[string]any{
			"id":       sub.ID,
			"name":     sub.Name,
			"archived": true,
			"reason":   reason,
		})
	}
	return nil
}

func subscriptionShouldArchive(sub *model.Subscription, availability LocalAvailability) bool {
	if sub == nil || sub.WashEnabled || sub.ArchivedAt != nil {
		return false
	}
	mediaType := strings.ToLower(strings.TrimSpace(sub.MediaType))
	if !isSubscriptionSeriesType(mediaType) {
		return availability.InLibrary || availability.LocalMediaCount > 0 || availability.DownloadedEpisodes > 0
	}
	if availability.HasSeriesPack {
		return true
	}
	total := sub.TotalEpisodes
	if total <= 0 {
		total = availability.TotalEpisodes
	}
	if total > 0 {
		return availability.DownloadedEpisodes >= total && len(availability.MissingEpisodes) == 0
	}
	return subscriptionLooksSingleEpisode(sub) && availability.DownloadedEpisodes > 0
}

func subscriptionArchiveReason(sub *model.Subscription, availability LocalAvailability) string {
	if sub != nil && sub.WashEnabled {
		return ""
	}
	if availability.HasSeriesPack {
		return "整季资源已加入下载/入库"
	}
	if availability.TotalEpisodes > 0 {
		return fmt.Sprintf("订阅完成：%d/%d", availability.DownloadedEpisodes, availability.TotalEpisodes)
	}
	if availability.DownloadedEpisodes > 0 {
		return "单集订阅已加入下载/入库"
	}
	return "订阅媒体已加入下载/入库"
}

func subscriptionLooksSingleEpisode(sub *model.Subscription) bool {
	if sub == nil {
		return false
	}
	for _, value := range []string{sub.Name, sub.Filter} {
		_, episode := ParseEpisode(value)
		if episode > 0 {
			return true
		}
	}
	return false
}

func (s *SubscriptionService) shouldSkipExistingTorrent(ctx context.Context, mediaType string, candidate siteSearchCandidate) bool {
	if s == nil || s.downloads == nil {
		return false
	}
	if isSubscriptionSeriesType(mediaType) && !candidate.Pack && candidate.Episode > 0 {
		return false
	}
	return s.downloads.TorrentExistsByName(ctx, candidate.Item.Title)
}

func siteSearchKeyword(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(sub.SearchMode), "imdb") && strings.TrimSpace(sub.IMDBID) != "" {
		return strings.TrimSpace(sub.IMDBID)
	}
	if u, err := url.Parse(sub.FeedURL); err == nil {
		if keyword := strings.TrimSpace(u.Query().Get("keyword")); keyword != "" {
			return keyword
		}
	}
	if keyword := strings.TrimSpace(sub.Filter); keyword != "" {
		return keyword
	}
	return strings.TrimSpace(sub.Name)
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

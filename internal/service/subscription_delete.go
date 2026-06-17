package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"gorm.io/gorm"
)

func (s *SubscriptionService) deleteSubscriptionDownloads(ctx context.Context, sub *model.Subscription) error {
	if s == nil || s.repo == nil || s.repo.Download == nil || sub == nil {
		return nil
	}
	rows, err := s.repo.Download.List(ctx)
	if err != nil {
		return err
	}
	candidates := make([]model.DownloadTask, 0)
	for _, row := range rows {
		if subscriptionDeleteMatchesTask(ctx, s, sub, row) {
			candidates = append(candidates, row)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	var live []QBitTorrent
	if s.downloads != nil && s.downloads.qb != nil && s.downloads.qb.IsConfigured() {
		live, _ = s.downloads.qb.List(ctx, "")
	}
	deletedHashes := map[string]struct{}{}
	for _, task := range candidates {
		hash := downloadTaskInfoHash(task)
		if hash == "" {
			hash = matchingLiveTorrentHash(task, live)
		}
		if hash != "" && s.downloads != nil && s.downloads.qb != nil && s.downloads.qb.IsConfigured() {
			key := strings.ToLower(hash)
			if _, ok := deletedHashes[key]; !ok {
				if err := s.downloads.Delete(ctx, hash, false); err != nil {
					return fmt.Errorf("删除订阅关联下载任务 %q 失败: %w", task.Title, err)
				}
				deletedHashes[key] = struct{}{}
			}
			continue
		}
		markDownloadTaskDeletedByID(ctx, s.repo.DB, task)
	}
	return nil
}

func subscriptionDeleteMatchesTask(ctx context.Context, s *SubscriptionService, sub *model.Subscription, task model.DownloadTask) bool {
	if strings.TrimSpace(task.Status) != "" && !downloadTaskBlocksReadd(task.Status) {
		return false
	}
	if strings.TrimSpace(task.SubscriptionID) != "" {
		return task.SubscriptionID == sub.ID
	}
	if strings.TrimSpace(sub.UserID) != "" && strings.TrimSpace(task.UserID) != "" && sub.UserID != task.UserID {
		return false
	}
	baseSavePath := s.subscriptionBaseSavePath(ctx, sub)
	if baseSavePath != "" && task.SavePath != "" && !sameOrChildPath(task.SavePath, baseSavePath) && !sameOrChildPath(baseSavePath, task.SavePath) {
		return false
	}
	query := normalizeAvailabilityComparable(availabilityQuery(subscriptionName(sub), subscriptionFilter(sub)))
	if query == "" {
		return false
	}
	title := normalizeAvailabilityComparable(task.Title)
	if title == "" {
		title = normalizeAvailabilityComparable(publicDownloadTitle(task.URL))
	}
	return title != "" && (strings.Contains(title, query) || strings.Contains(query, title))
}

func downloadTaskInfoHash(task model.DownloadTask) string {
	raw := strings.TrimSpace(task.URL)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if strings.EqualFold(parsed.Scheme, "magnet") {
		for _, xt := range parsed.Query()["xt"] {
			const prefix = "urn:btih:"
			if strings.HasPrefix(strings.ToLower(xt), prefix) {
				return strings.TrimSpace(xt[len(prefix):])
			}
		}
	}
	return ""
}

func matchingLiveTorrentHash(task model.DownloadTask, live []QBitTorrent) string {
	key := downloadTaskIdentityKey(task.Title)
	if key == "" {
		key = downloadTaskIdentityKey(publicDownloadTitle(task.URL))
	}
	if key == "" {
		return ""
	}
	for _, torrent := range live {
		current := downloadTaskIdentityKey(torrent.Name)
		if current == "" {
			continue
		}
		if current == key || strings.Contains(current, key) || strings.Contains(key, current) {
			return strings.TrimSpace(torrent.Hash)
		}
	}
	return ""
}

func markDownloadTaskDeletedByID(ctx context.Context, db *gorm.DB, task model.DownloadTask) {
	if db == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	_ = db.WithContext(ctx).Model(&model.DownloadTask{}).
		Where("id = ?", task.ID).
		Updates(map[string]any{
			"status":   "deleted",
			"progress": task.Progress,
		}).Error
}

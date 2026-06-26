// Package service — subscription local and pending-download availability helpers.
package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (s *SubscriptionService) pendingDownloadAvailability(ctx context.Context, sub *model.Subscription) LocalAvailability {
	out := LocalAvailability{
		ExistingEpisodeKeys: map[string]struct{}{},
		MissingEpisodeKeys:  map[string]struct{}{},
	}
	if sub != nil {
		out.TotalEpisodes = sub.TotalEpisodes
	}
	query := availabilityQuery(subscriptionName(sub), subscriptionFilter(sub))
	if query == "" {
		return s.finalizePendingAvailability(sub, out)
	}
	root := s.subscriptionBaseSavePath(ctx, sub)
	if root != "" {
		_ = scanDownloadPath(ctx, root, query, func(path string, season, episode int) bool {
			out.LocalMediaCount++
			if refs := episodeRefsFromTitle(path); len(refs) > 0 {
				for _, ref := range refs {
					out.ExistingEpisodeKeys[episodeKey(ref.Season, ref.Episode)] = struct{}{}
				}
			} else if episode > 0 {
				out.ExistingEpisodeKeys[episodeKey(season, episode)] = struct{}{}
			}
			return true
		})
	}
	s.addDownloadTaskAvailability(ctx, sub, query, &out)
	s.addLiveTorrentAvailability(ctx, query, &out)
	return s.finalizePendingAvailability(sub, out)
}

func (s *SubscriptionService) EnrichProgress(ctx context.Context, items []model.Subscription) {
	for i := range items {
		availability := mergeLocalAvailability(
			SubscriptionLocalAvailability(ctx, s.repo, &items[i]),
			s.pendingDownloadAvailability(ctx, &items[i]),
		)
		items[i].DownloadedEpisodes = availability.DownloadedEpisodes
		items[i].LocalMediaCount = availability.LocalMediaCount
		items[i].MissingEpisodes = availability.MissingEpisodes
		items[i].InLibrary = availability.InLibrary
		if items[i].TotalEpisodes == 0 {
			items[i].TotalEpisodes = availability.TotalEpisodes
		}
	}
}

func (s *SubscriptionService) addDownloadTaskAvailability(ctx context.Context, sub *model.Subscription, query string, out *LocalAvailability) {
	if s == nil || s.repo == nil || s.repo.Download == nil || out == nil {
		return
	}
	rows, err := s.repo.Download.List(ctx)
	if err != nil {
		return
	}
	baseSavePath := s.subscriptionBaseSavePath(ctx, sub)
	for _, row := range rows {
		if !downloadTaskBlocksReadd(row.Status) {
			continue
		}
		linkedToSubscription := sub != nil && strings.TrimSpace(row.SubscriptionID) != "" && row.SubscriptionID == sub.ID
		if !linkedToSubscription && baseSavePath != "" && row.SavePath != "" && !sameOrChildPath(row.SavePath, baseSavePath) && !sameOrChildPath(baseSavePath, row.SavePath) {
			continue
		}
		if linkedToSubscription {
			addTrustedAvailabilityTitle(row.Title, 0, 0, false, out)
			continue
		}
		addAvailabilityTitle(row.Title, query, out)
	}
}

func (s *SubscriptionService) addLiveTorrentAvailability(ctx context.Context, query string, out *LocalAvailability) {
	if s == nil || s.downloads == nil || s.downloads.qb == nil || out == nil {
		return
	}
	live, err := s.downloads.qb.List(ctx, "")
	if err != nil {
		return
	}
	for _, torrent := range live {
		addAvailabilityTitle(torrent.Name, query, out)
	}
}

func addAvailabilityTitle(title, query string, out *LocalAvailability) {
	if out == nil || strings.TrimSpace(title) == "" || strings.TrimSpace(query) == "" {
		return
	}
	if !strings.Contains(normalizeAvailabilityComparable(title), normalizeAvailabilityComparable(query)) {
		return
	}
	out.LocalMediaCount++
	if refs := episodeRefsFromTitle(title); len(refs) > 0 {
		if out.ExistingEpisodeKeys == nil {
			out.ExistingEpisodeKeys = map[string]struct{}{}
		}
		for _, ref := range refs {
			out.ExistingEpisodeKeys[episodeKey(ref.Season, ref.Episode)] = struct{}{}
		}
		return
	}
	if isSeriesPackTitle(title) {
		out.HasSeriesPack = true
	}
}

func addSiteSearchCandidateAvailability(candidate siteSearchCandidate, out *LocalAvailability) {
	addTrustedAvailabilityTitle(subscriptionSearchResultText(candidate.Item), candidate.Season, candidate.Episode, candidate.Pack, out)
}

func addTrustedAvailabilityTitle(title string, season, episode int, pack bool, out *LocalAvailability) {
	if out == nil {
		return
	}
	if strings.TrimSpace(title) == "" && episode <= 0 && !pack {
		return
	}
	out.LocalMediaCount++
	refs := episodeRefsFromTitle(title)
	if len(refs) == 0 && episode > 0 {
		if season <= 0 {
			season = 1
		}
		refs = []episodeRef{{Season: season, Episode: episode}}
	}
	if len(refs) > 0 {
		if out.ExistingEpisodeKeys == nil {
			out.ExistingEpisodeKeys = map[string]struct{}{}
		}
		for _, ref := range refs {
			out.ExistingEpisodeKeys[episodeKey(ref.Season, ref.Episode)] = struct{}{}
		}
		return
	}
	if episode <= 0 {
		season, episode = ParseEpisode(title)
	}
	if episode > 0 {
		if out.ExistingEpisodeKeys == nil {
			out.ExistingEpisodeKeys = map[string]struct{}{}
		}
		out.ExistingEpisodeKeys[episodeKey(season, episode)] = struct{}{}
		return
	}
	if pack || isSeriesPackTitle(title) {
		out.HasSeriesPack = true
	}
}

func sameOrChildPath(pathValue, root string) bool {
	pathValue = filepath.Clean(strings.TrimSpace(pathValue))
	root = filepath.Clean(strings.TrimSpace(root))
	if pathValue == "" || root == "" || pathValue == "." || root == "." {
		return false
	}
	if strings.EqualFold(pathValue, root) {
		return true
	}
	rel, err := filepath.Rel(root, pathValue)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func (s *SubscriptionService) finalizePendingAvailability(sub *model.Subscription, out LocalAvailability) LocalAvailability {
	mediaType := ""
	if sub != nil {
		mediaType = sub.MediaType
	}
	if isSubscriptionSeriesType(mediaType) || len(out.ExistingEpisodeKeys) > 0 {
		out.DownloadedEpisodes = len(out.ExistingEpisodeKeys)
		out.MissingEpisodes = missingEpisodes(out.ExistingEpisodeKeys, out.TotalEpisodes)
		for _, episode := range out.MissingEpisodes {
			out.MissingEpisodeKeys[episodeKey(1, episode)] = struct{}{}
		}
	} else if out.LocalMediaCount > 0 {
		out.DownloadedEpisodes = 1
		if out.TotalEpisodes == 0 {
			out.TotalEpisodes = 1
		}
	}
	return out
}

func (s *SubscriptionService) subscriptionBaseSavePath(ctx context.Context, sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	base := strings.TrimSpace(sub.SavePath)
	if base == "" && s != nil && s.repo != nil && s.repo.Setting != nil {
		base, _ = s.repo.Setting.Get(ctx, "qbittorrent.savepath")
	}
	return base
}

func subscriptionName(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	return sub.Name
}

func subscriptionFilter(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	return sub.Filter
}

func mergeLocalAvailability(values ...LocalAvailability) LocalAvailability {
	out := LocalAvailability{
		ExistingEpisodeKeys: map[string]struct{}{},
		MissingEpisodeKeys:  map[string]struct{}{},
	}
	for _, value := range values {
		if value.TotalEpisodes > out.TotalEpisodes {
			out.TotalEpisodes = value.TotalEpisodes
		}
		out.LocalMediaCount += value.LocalMediaCount
		out.InLibrary = out.InLibrary || value.InLibrary
		out.HasSeriesPack = out.HasSeriesPack || value.HasSeriesPack
		for key := range value.ExistingEpisodeKeys {
			out.ExistingEpisodeKeys[key] = struct{}{}
		}
	}
	out.DownloadedEpisodes = len(out.ExistingEpisodeKeys)
	if out.TotalEpisodes > 0 {
		out.MissingEpisodes = missingEpisodes(out.ExistingEpisodeKeys, out.TotalEpisodes)
		for _, episode := range out.MissingEpisodes {
			out.MissingEpisodeKeys[episodeKey(1, episode)] = struct{}{}
		}
	}
	if out.DownloadedEpisodes == 0 && out.LocalMediaCount > 0 {
		out.DownloadedEpisodes = out.LocalMediaCount
		if out.TotalEpisodes == 0 {
			out.TotalEpisodes = 1
		}
	}
	return out
}

// subscriptionItemAlreadyAvailable 判断某个订阅条目（按其标题解析出的季/集）是否已在媒体库存在。
// 电影/无集号条目：媒体库已有该片即视为已存在；剧集条目：对应季集已入库即视为已存在。
func subscriptionItemAlreadyAvailable(sub *model.Subscription, avail LocalAvailability, title string) bool {
	if avail.LocalMediaCount == 0 && !avail.HasSeriesPack {
		return false
	}
	if !isSubscriptionSeriesType(subscriptionMediaType(sub)) {
		return true
	}
	if avail.HasSeriesPack {
		return true
	}
	wantSeason, wantEpisode := ParseEpisode(title)
	if wantEpisode <= 0 {
		// 整季合集 / 无法解析集号：库里已有内容时保守跳过，避免重复整季下载。
		return true
	}
	if wantSeason <= 0 {
		wantSeason = 1
	}
	_, exists := avail.ExistingEpisodeKeys[episodeKey(wantSeason, wantEpisode)]
	return exists
}

func subscriptionMediaType(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	return sub.MediaType
}

func (s *SubscriptionService) downloadPathHasCandidate(ctx context.Context, sub *model.Subscription, title, savePath string) bool {
	savePath = strings.TrimSpace(savePath)
	if savePath == "" {
		savePath = s.subscriptionBaseSavePath(ctx, sub)
	}
	query := availabilityQuery(title, subscriptionFilter(sub))
	if savePath == "" || query == "" {
		return false
	}
	wanted := episodeRefsFromTitle(title)
	if len(wanted) == 0 {
		wantSeason, wantEpisode := ParseEpisode(title)
		if wantEpisode > 0 {
			wanted = []episodeRef{{Season: wantSeason, Episode: wantEpisode}}
		}
	}
	found := false
	foundEpisodes := map[string]struct{}{}
	_ = scanDownloadPath(ctx, savePath, query, func(path string, season, episode int) bool {
		if len(wanted) == 0 {
			found = true
			return false
		}
		if episode <= 0 {
			return true
		}
		if season <= 0 {
			season = 1
		}
		if refs := episodeRefsFromTitle(path); len(refs) > 0 {
			for _, ref := range refs {
				foundEpisodes[episodeKey(ref.Season, ref.Episode)] = struct{}{}
			}
		} else {
			foundEpisodes[episodeKey(season, episode)] = struct{}{}
		}
		for _, ref := range wanted {
			if _, ok := foundEpisodes[episodeKey(ref.Season, ref.Episode)]; !ok {
				return true
			}
		}
		found = true
		return false
	})
	return found
}

func scanDownloadPath(ctx context.Context, root, query string, visit func(path string, season, episode int) bool) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	normalizedQuery := normalizeAvailabilityComparable(query)
	if normalizedQuery == "" {
		return nil
	}
	visited := 0
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDownloadMediaPath(path) {
			return nil
		}
		visited++
		if visited > 10000 {
			return filepath.SkipAll
		}
		if !strings.Contains(normalizeAvailabilityComparable(path), normalizedQuery) {
			return nil
		}
		season, episode := ParseEpisode(path)
		if !visit(path, season, episode) {
			return filepath.SkipAll
		}
		return nil
	})
}

func isDownloadMediaPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".!qb", ".part", ".aria2", ".crdownload":
		path = strings.TrimSuffix(path, filepath.Ext(path))
		ext = strings.ToLower(filepath.Ext(path))
	}
	_, ok := videoExtensions[ext]
	return ok
}

func normalizeAvailabilityComparable(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

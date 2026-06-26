// Package service — subscription local and pending-download availability helpers.
package service

import (
	"context"
	"strings"

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
	queries := subscriptionAvailabilityQueries(sub)
	if len(queries) == 0 {
		return s.finalizePendingAvailability(sub, out)
	}
	root := s.subscriptionBaseSavePath(ctx, sub)
	if root != "" {
		_ = scanDownloadPathAny(ctx, root, queries, func(path string, season, episode int) bool {
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
	s.addDownloadTaskAvailability(ctx, sub, queries, &out)
	s.addLiveTorrentAvailability(ctx, queries, &out)
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

func (s *SubscriptionService) addDownloadTaskAvailability(ctx context.Context, sub *model.Subscription, queries []string, out *LocalAvailability) {
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
		if !s.downloadTaskCountsAsPending(ctx, row) {
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
		addAvailabilityTitleAny(row.Title, queries, out)
	}
}

func (s *SubscriptionService) downloadTaskCountsAsPending(ctx context.Context, row model.DownloadTask) bool {
	if s == nil || s.downloads == nil {
		return true
	}
	return s.downloads.subscriptionDownloadTaskStillLive(ctx, row)
}

func (s *SubscriptionService) addLiveTorrentAvailability(ctx context.Context, queries []string, out *LocalAvailability) {
	if s == nil || s.downloads == nil || s.downloads.qb == nil || out == nil {
		return
	}
	live, err := s.downloads.qb.List(ctx, "")
	if err != nil {
		return
	}
	for _, torrent := range live {
		addAvailabilityTitleAny(torrent.Name, queries, out)
	}
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

func subscriptionAvailabilityQueries(sub *model.Subscription) []string {
	if sub == nil {
		return nil
	}
	values := []string{availabilityQuery(subscriptionName(sub), subscriptionFilter(sub))}
	for _, keyword := range siteSearchKeywords(sub) {
		values = append(values, cleanAvailabilityTitle(keyword))
	}
	if original := cleanAvailabilityTitle(sub.OriginalName); original != "" {
		values = append(values, original)
	}
	return compactUniqueStrings(values...)
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

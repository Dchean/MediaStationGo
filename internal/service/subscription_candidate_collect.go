package service

import (
	"regexp"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func collectSiteSearchCandidates(results []SearchResult, sub *model.Subscription, seenSet map[string]struct{}, allowQueryMismatch bool, stats *siteSearchSelectionStats) []siteSearchCandidate {
	candidates := make([]siteSearchCandidate, 0, len(results))
	for _, item := range results {
		matchText := subscriptionSearchResultText(item)
		if !subscriptionSearchResultMatchesQuery(sub, item) {
			if allowQueryMismatch {
				stats.RelaxedQueryMatch++
			} else {
				stats.QueryMismatch++
				stats.QueryMismatchExamples = appendLimitedStrings(stats.QueryMismatchExamples, matchText, 5)
				continue
			}
		}
		if !matchesSubscriptionRules(sub, matchText) {
			stats.RuleMismatch++
			continue
		}
		download := strings.TrimSpace(item.DownloadURL)
		if download == "" {
			download = strings.TrimSpace(item.TorrentURL)
		}
		if download == "" {
			stats.MissingDownload++
			continue
		}
		guid := stableSiteSearchGUID(item, download)
		if _, ok := seenSet[guid]; ok {
			stats.Seen++
			continue
		}
		season, episode := ParseEpisode(matchText)
		episodes := episodeNumbersFromRefs(episodeRefsFromTitle(matchText), season)
		score := subscriptionCandidateScore(sub, item)
		stats.Prepared++
		candidates = append(candidates, siteSearchCandidate{
			Item:     item,
			Download: download,
			GUID:     guid,
			Season:   season,
			Episode:  episode,
			Episodes: episodes,
			Pack:     isSeriesPackTitle(item.Title) || len(episodes) > 1,
			Score:    score,
		})
	}
	return candidates
}

func appendLimitedStrings(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 || len(values) >= limit {
		return values
	}
	return append(values, value)
}

func shouldRelaxSiteSearchQueryMatch(sub *model.Subscription, local LocalAvailability) bool {
	if sub == nil {
		return false
	}
	mediaType := normalizeMediaType(sub.MediaType, sub.Name+" "+sub.Filter, "")
	if !isSubscriptionSeriesType(mediaType) {
		return false
	}
	if local.LocalMediaCount == 0 && len(local.ExistingEpisodeKeys) == 0 {
		return false
	}
	return local.TotalEpisodes > 0 || len(local.MissingEpisodes) > 0
}

func subscriptionSearchResultText(item SearchResult) string {
	return strings.TrimSpace(strings.Join([]string{item.Title, item.Subtitle}, " "))
}

func selectRSSSubscriptionCandidates(items []rssItem, sub *model.Subscription, filter *regexp.Regexp, seenSet map[string]struct{}, local LocalAvailability) []siteSearchCandidate {
	if seenSet == nil {
		seenSet = map[string]struct{}{}
	}
	candidates := make([]siteSearchCandidate, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		if filter != nil && !filter.MatchString(title) {
			continue
		}
		if !matchesSubscriptionRules(sub, title) {
			continue
		}
		download := strings.TrimSpace(item.Enclosure.URL)
		if download == "" {
			download = strings.TrimSpace(item.Link)
		}
		if download == "" {
			continue
		}
		guid := stableRSSItemGUID(title, item.GUID, item.Link, item.Enclosure.URL)
		if _, ok := seenSet[guid]; ok {
			continue
		}
		searchItem := SearchResult{Title: title, DownloadURL: download}
		season, episode := ParseEpisode(title)
		episodes := episodeNumbersFromRefs(episodeRefsFromTitle(title), season)
		candidates = append(candidates, siteSearchCandidate{
			Item:     searchItem,
			Download: download,
			GUID:     guid,
			Season:   season,
			Episode:  episode,
			Episodes: episodes,
			Pack:     isSeriesPackTitle(title) || len(episodes) > 1,
			Score:    subscriptionCandidateScore(sub, searchItem),
		})
	}
	return selectPreparedSubscriptionCandidates(candidates, sub, local)
}

func episodeNumbersFromRefs(refs []episodeRef, fallbackSeason int) []int {
	if len(refs) == 0 {
		return nil
	}
	if fallbackSeason <= 0 {
		fallbackSeason = refs[0].Season
	}
	out := make([]int, 0, len(refs))
	seen := map[int]struct{}{}
	for _, ref := range refs {
		season := ref.Season
		if season <= 0 {
			season = 1
		}
		if fallbackSeason > 0 && season != fallbackSeason {
			continue
		}
		if ref.Episode <= 0 {
			continue
		}
		if _, ok := seen[ref.Episode]; ok {
			continue
		}
		seen[ref.Episode] = struct{}{}
		out = append(out, ref.Episode)
	}
	return out
}

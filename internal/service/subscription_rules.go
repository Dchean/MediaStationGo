package service

import (
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func matchesSubscriptionRules(sub *model.Subscription, title string) bool {
	titleFold := strings.ToLower(title)
	if containsAnyExcludeToken(titleFold, defaultExcludeWords) {
		return false
	}
	if sub == nil {
		return true
	}
	if shouldApplyDefaultCompatibilityExcludes(sub.ExcludeWords) && containsAnyExcludeToken(titleFold, defaultCompatibilityExcludeWords) {
		return false
	}
	if sub.ExcludeWords != "" && containsAnyExcludeToken(titleFold, sub.ExcludeWords) {
		return false
	}
	if sub.ReleaseGroups != "" && !containsAnyToken(titleFold, sub.ReleaseGroups) {
		return false
	}
	if sub.Resolution != "" && sub.Resolution != "best" && !titleMatchesResolution(titleFold, sub.Resolution) {
		return false
	}
	if sub.Quality != "" && sub.Quality != "best" && !titleMatchesQuality(titleFold, sub.Quality) {
		return false
	}
	if sub.Effects != "" && !containsAnyEffect(titleFold, sub.Effects) {
		return false
	}
	return true
}

func shouldApplyDefaultCompatibilityExcludes(excludeWords string) bool {
	normalized := normalizeExcludeWords(excludeWords)
	return normalized == "" || normalized == normalizeExcludeWords(legacyFrontendExcludeWords)
}

func normalizeExcludeWords(csv string) string {
	return strings.Join(excludeWordTokens(csv), ",")
}

func isSubscriptionSeriesType(mediaType string) bool {
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		return true
	default:
		return false
	}
}

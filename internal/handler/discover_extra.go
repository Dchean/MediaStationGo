// Package handler — multi-section discover endpoints.
//
// The Vue DiscoverView paginates a configurable list of "sections"
// (trending day/week, popular movies, top rated, etc.) and asks the
// backend for a feed keyed by section name. We mirror that surface so
// the React DiscoverPage can render the same rails without a rewrite.
package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type discoverSectionDef struct {
	Key      string
	Label    string
	Provider string
}

var discoverSectionCatalog = []discoverSectionDef{
	{Key: "tmdb_trending_day", Label: "TMDb 今日趋势", Provider: "tmdb"},
	{Key: "tmdb_trending_week", Label: "TMDb 本周热门", Provider: "tmdb"},
	{Key: "tmdb_popular_movie", Label: "TMDb 热门电影", Provider: "tmdb"},
	{Key: "tmdb_popular_tv", Label: "TMDb 热门剧集", Provider: "tmdb"},
	{Key: "tmdb_top_rated_movie", Label: "TMDb 高分电影", Provider: "tmdb"},
	{Key: "douban_hot_movie", Label: "豆瓣热门电影", Provider: "douban"},
	{Key: "douban_hot_tv", Label: "豆瓣热门剧集", Provider: "douban"},
	{Key: "douban_top_movie", Label: "豆瓣高分电影", Provider: "douban"},
	{Key: "bangumi_calendar", Label: "Bangumi 每日放送", Provider: "bangumi"},
}

// discoverSectionsHandler returns the catalog of sections the UI can
// pick from. The names match the upstream Vue UI so existing settings
// keep working.
func discoverSectionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		sections := make([]gin.H, 0, len(discoverSectionCatalog))
		for _, section := range enabledDiscoverSections(c.Request.Context(), svc) {
			sections = append(sections, gin.H{"key": section.Key, "label": section.Label, "provider": section.Provider})
		}
		c.JSON(http.StatusOK, gin.H{"sections": sections})
	}
}

// discoverFeedHandler resolves one or more section keys (?sections=a,b)
// to TMDb / Douban / Bangumi rails and returns the joined results keyed by
// section name. Unknown keys are silently dropped so URL typos don't break
// the page.
func discoverFeedHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawSections := c.Query("sections")
		if strings.TrimSpace(rawSections) == "" {
			rawSections = strings.Join(defaultDiscoverSectionKeys(c.Request.Context(), svc), ",")
		}
		keys := strings.Split(rawSections, ",")
		out := gin.H{}
		artworkItems := []service.ExternalMediaResult{}
		for _, raw := range keys {
			k := strings.TrimSpace(raw)
			if k == "" {
				continue
			}
			if provider := discoverSectionProvider(k); provider != "" && !discoverProviderEnabled(c.Request.Context(), svc, provider) {
				out[k] = []service.ExternalMediaResult{}
				continue
			}
			items, err := discoverSectionItems(c.Request.Context(), svc, k)
			if err != nil {
				svc.Log.Debug("discover fetch failed")
				items = nil
			}
			artworkItems = append(artworkItems, items...)
			out[k] = items
		}
		svc.Discover.WarmExternalArtwork(artworkItems)
		c.JSON(http.StatusOK, out)
	}
}

func enabledDiscoverSections(ctx context.Context, svc *service.Container) []discoverSectionDef {
	sections := make([]discoverSectionDef, 0, len(discoverSectionCatalog))
	for _, section := range discoverSectionCatalog {
		if !discoverProviderEnabled(ctx, svc, section.Provider) {
			continue
		}
		sections = append(sections, section)
	}
	return sections
}

func defaultDiscoverSectionKeys(ctx context.Context, svc *service.Container) []string {
	preferred := []string{"tmdb_trending_day", "douban_hot_movie", "douban_hot_tv", "bangumi_calendar"}
	enabled := map[string]struct{}{}
	for _, section := range enabledDiscoverSections(ctx, svc) {
		enabled[section.Key] = struct{}{}
	}
	out := make([]string, 0, len(preferred))
	for _, key := range preferred {
		if _, ok := enabled[key]; ok {
			out = append(out, key)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, section := range enabledDiscoverSections(ctx, svc) {
		out = append(out, section.Key)
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func discoverSectionProvider(key string) string {
	for _, section := range discoverSectionCatalog {
		if section.Key == key {
			return section.Provider
		}
	}
	switch key {
	case "trending_day", "trending_week", "popular_movie", "popular_tv", "top_rated_movie", "upcoming_movie":
		return "tmdb"
	default:
		return ""
	}
}

func discoverProviderEnabled(ctx context.Context, svc *service.Container, provider string) bool {
	if svc == nil || svc.APIConfig == nil || strings.TrimSpace(provider) == "" {
		return true
	}
	cfg, err := svc.APIConfig.Get(ctx, provider)
	if err != nil || cfg == nil {
		return true
	}
	return cfg.Enabled
}

func discoverSectionItems(ctx context.Context, svc *service.Container, k string) ([]service.ExternalMediaResult, error) {
	switch k {
	case "tmdb_trending_day", "tmdb_trending_week", "tmdb_popular_movie", "tmdb_popular_tv", "tmdb_top_rated_movie",
		"trending_day", "trending_week", "popular_movie", "popular_tv", "top_rated_movie", "upcoming_movie":
		return svc.Discover.TMDbSection(ctx, k)
	case "douban_hot_movie", "douban_hot_tv", "douban_top_movie":
		if svc.Douban == nil {
			return []service.ExternalMediaResult{}, nil
		}
		return svc.Douban.Discover(ctx, k)
	case "bangumi_calendar":
		if svc.Bangumi == nil {
			return []service.ExternalMediaResult{}, nil
		}
		return svc.Bangumi.Calendar(ctx)
	default:
		return []service.ExternalMediaResult{}, nil
	}
}

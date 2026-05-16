// Package handler — multi-section discover endpoints.
//
// The Vue DiscoverView paginates a configurable list of "sections"
// (trending day/week, popular movies, top rated, etc.) and asks the
// backend for a feed keyed by section name. We mirror that surface so
// the React DiscoverPage can render the same rails without a rewrite.
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// discoverSectionsHandler returns the catalog of sections the UI can
// pick from. The names match the upstream Vue UI so existing settings
// keep working.
func discoverSectionsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"sections": []gin.H{
				{"key": "trending_day", "label": "今日热门"},
				{"key": "trending_week", "label": "本周热门"},
				{"key": "popular_movie", "label": "热门电影"},
				{"key": "popular_tv", "label": "热门剧集"},
				{"key": "top_rated_movie", "label": "高分电影"},
				{"key": "upcoming_movie", "label": "即将上映"},
			},
		})
	}
}

// discoverFeedHandler resolves one or more section keys (?sections=a,b)
// to TMDb endpoint paths and returns the joined results keyed by
// section name. Unknown keys are silently dropped so URL typos don't
// break the page.
func discoverFeedHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys := strings.Split(c.DefaultQuery("sections", "trending_day,popular_movie"), ",")
		out := gin.H{}
		for _, raw := range keys {
			k := strings.TrimSpace(raw)
			path := sectionPath(k)
			if path == "" {
				continue
			}
			items, err := svc.Discover.Fetch(c.Request.Context(), path)
			if err != nil {
				svc.Log.Debug("discover fetch failed", )
				items = nil
			}
			out[k] = items
		}
		c.JSON(http.StatusOK, out)
	}
}

// sectionPath maps the UI-facing key to the TMDb endpoint suffix.
func sectionPath(k string) string {
	switch k {
	case "trending_day":
		return "/trending/movie/day"
	case "trending_week":
		return "/trending/movie/week"
	case "popular_movie":
		return "/movie/popular"
	case "popular_tv":
		return "/tv/popular"
	case "top_rated_movie":
		return "/movie/top_rated"
	case "upcoming_movie":
		return "/movie/upcoming"
	default:
		return ""
	}
}

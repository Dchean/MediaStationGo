// Package handler — watch history endpoints.
//
// The base /history GET / POST routes already exist; these add the three
// auxiliary surfaces the React WatchHistoryPage needs:
//
//   GET  /api/watch-history          paginated list (admin sees every user)
//   GET  /api/watch-history/stats    aggregate watch time + completion
//   GET  /api/watch-history/continue resume rail (incomplete only)
//   DELETE /api/watch-history        clear (?media_item_id= optional)
//   DELETE /api/watch-history/:id    remove one row
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// historyListHandler returns the caller's history rows joined with the
// matching media in a single response.
func historyListHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		if limit <= 0 || limit > 500 {
			limit = 50
		}
		items, err := svc.Playback.RecentHistory(c.Request.Context(), toString(uid), limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, items)
	}
}

// historyStatsHandler returns aggregate watch time + completion counts
// for the caller. Used by the WatchHistoryPage hero card.
func historyStatsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		userID := toString(uid)

		var total int64
		_ = svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ?", userID).Count(&total).Error

		var completed int64
		_ = svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ? AND completed = ?", userID, true).Count(&completed).Error

		var watchedMs int64
		_ = svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ?", userID).
			Select("COALESCE(SUM(position_ms), 0)").
			Row().Scan(&watchedMs)

		var last *time.Time
		row := svc.Repo.DB.Model(&model.PlaybackHistory{}).
			Where("user_id = ?", userID).
			Select("MAX(watched_at)").Row()
		var lastT time.Time
		if err := row.Scan(&lastT); err == nil && !lastT.IsZero() {
			last = &lastT
		}

		c.JSON(http.StatusOK, gin.H{
			"total":         total,
			"completed":     completed,
			"watched_ms":    watchedMs,
			"watched_hours": float64(watchedMs) / 1000.0 / 3600.0,
			"last_watched":  last,
		})
	}
}

// historyContinueHandler returns "Continue Watching" rows: incomplete
// items, most recent first.
func historyContinueHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		if limit <= 0 || limit > 50 {
			limit = 10
		}
		var rows []model.PlaybackHistory
		if err := svc.Repo.DB.
			Where("user_id = ? AND completed = ?", toString(uid), false).
			Order("watched_at desc").
			Limit(limit).
			Find(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Hydrate media in one query.
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.MediaID)
		}
		var media []model.Media
		if len(ids) > 0 {
			_ = svc.Repo.DB.Where("id IN ?", ids).Find(&media).Error
		}
		mIdx := make(map[string]model.Media, len(media))
		for _, m := range media {
			mIdx[m.ID] = m
		}
		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			m, ok := mIdx[r.MediaID]
			if !ok {
				continue
			}
			out = append(out, gin.H{
				"history": r,
				"media":   m,
			})
		}
		c.JSON(http.StatusOK, out)
	}
}

// historyDeleteHandler removes one or all history rows for the caller.
//
//   DELETE /api/watch-history?media_id=xxx  → delete just that media's row
//   DELETE /api/watch-history               → clear all rows for the user
func historyDeleteHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		userID := toString(uid)
		mediaID := c.Query("media_id")

		q := svc.Repo.DB.Where("user_id = ?", userID)
		if mediaID != "" {
			q = q.Where("media_id = ?", mediaID)
		}
		if err := q.Delete(&model.PlaybackHistory{}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func historyDeleteOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		if err := svc.Repo.DB.
			Where("user_id = ? AND id = ?", toString(uid), c.Param("id")).
			Delete(&model.PlaybackHistory{}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

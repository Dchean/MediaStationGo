// Package handler — system config + scheduler trigger + events ticket.
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// listSystemConfigHandler is the non-admin alias for /admin/settings.
// It returns the same key/value rows so the Vue UI's `system.getConfig`
// helper keeps working.
func listSystemConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.Repo.Setting.All(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Hide secret-flavoured keys for non-admins.
		role, _ := c.Get(middleware.CtxUserRole)
		out := make([]model.Setting, 0, len(rows))
		for _, s := range rows {
			if role != "admin" && isSecretKey(s.Key) {
				s.Value = "********"
			}
			out = append(out, s)
		}
		c.JSON(http.StatusOK, gin.H{"items": out})
	}
}

func isSecretKey(k string) bool {
	for _, suffix := range []string{".token", ".secret", ".password", ".api_key", ".cookie"} {
		if endsWith(k, suffix) {
			return true
		}
	}
	return false
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// schemaHandler returns the curated settings schema (used by the
// `getSchema()` Vue helper). It mirrors the SettingsPage groupings but
// in JSON so the upstream UI can render its dynamic form.
func schemaHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"groups": []gin.H{
				{
					"key":   "general",
					"label": "常规",
					"items": []gin.H{
						{"key": "tmdb.language", "type": "select", "label": "TMDb 元数据语言"},
						{"key": "transcode.enabled", "type": "toggle", "label": "启用转码"},
						{"key": "transcode.hw_accel", "type": "select", "label": "硬件加速"},
						{"key": "transcode.max_jobs", "type": "number", "label": "最大并发"},
						{"key": "ffmpeg.path", "type": "text", "label": "FFmpeg 路径"},
						{"key": "ffprobe.path", "type": "text", "label": "FFprobe 路径"},
					},
				},
				{
					"key":   "organize",
					"label": "整理 & 刮削",
					"items": []gin.H{
						{"key": "organizer.auto_after_download", "type": "toggle"},
						{"key": "organizer.smart_classify", "type": "toggle"},
						{"key": "organize.movie_format", "type": "text"},
						{"key": "organize.tv_format", "type": "text"},
						{"key": "organize.anime_format", "type": "text"},
						{"key": "scrape.auto_on_scan", "type": "toggle"},
						{"key": "scrape.providers", "type": "text"},
						{"key": "scrape.language", "type": "text"},
					},
				},
				{
					"key":   "adult",
					"label": "Adult / NSFW",
					"items": []gin.H{
						{"key": "adult.enabled", "type": "toggle"},
						{"key": "adult.require_pin", "type": "toggle"},
						{"key": "adult.pin", "type": "text"},
					},
				},
				{
					"key":   "qbittorrent",
					"label": "qBittorrent",
					"items": []gin.H{
						{"key": "qbittorrent.url", "type": "text"},
						{"key": "qbittorrent.username", "type": "text"},
						{"key": "qbittorrent.password", "type": "text"},
						{"key": "qbittorrent.savepath", "type": "text"},
					},
				},
			},
		})
	}
}

// schedulerTriggerHandler is the alternate path for /admin/scheduler/:name/run.
func schedulerTriggerHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Scheduler.RunNow(c.Request.Context(), c.Param("name")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// ─── SSE ticket store ───────────────────────────────────────────────────────
//
// The Vue UI's SSE event stream wants a one-time signed ticket so the
// EventSource (which can't set Authorization headers) can authenticate.
// We don't expose the SSE stream itself yet, but we persist short-lived
// tickets keyed to the user so the upstream consumer keeps working.

type ticket struct {
	userID  string
	expires time.Time
}

var (
	ticketStore   = map[string]ticket{}
	ticketStoreMu sync.Mutex
)

func newTicket(userID string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	t := hex.EncodeToString(buf)
	ticketStoreMu.Lock()
	defer ticketStoreMu.Unlock()
	ticketStore[t] = ticket{userID: userID, expires: time.Now().Add(60 * time.Second)}
	// GC expired tickets opportunistically.
	for k, v := range ticketStore {
		if time.Now().After(v.expires) {
			delete(ticketStore, k)
		}
	}
	return t
}

func systemEventsTicketHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		c.JSON(http.StatusOK, gin.H{"ticket": newTicket(toString(uid))})
	}
}

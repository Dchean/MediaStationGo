// Package handler — playback metadata endpoints expected by the Vue UI:
//
//	GET  /playback/:id/info
//	POST /playback/:id/progress
//	GET  /playback/:id/external-players
//	GET  /playback/:id/external-url
//	GET  /playback/transcode/:job_id/status
package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// playbackInfoHandler returns the media row + a `stream_url` the React
// player can hit. Mirrors the Python project's surface.
func playbackInfoHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		token := externalPlaybackToken(c, svc)
		profileQuery := externalProfileQuery(c)
		c.JSON(http.StatusOK, gin.H{
			"media":      m,
			"stream_url": "/api/stream/" + m.ID + "?token=" + url.QueryEscape(token) + profileQuery,
			"hls_url":    "/api/hls/" + m.ID + "/index.m3u8?token=" + url.QueryEscape(token) + profileQuery,
		})
	}
}

type playbackProgressReq struct {
	PositionMs int64 `json:"position_ms"`
	DurationMs int64 `json:"duration_ms"`
	Completed  bool  `json:"completed"`
}

func playbackProgressHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req playbackProgressReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		if err := svc.Playback.RecordProgress(
			c.Request.Context(), toString(uid), c.Param("id"),
			req.PositionMs, req.DurationMs,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// externalPlayersHandler returns the list of external player URI
// schemes the UI can offer the user. We lookup the media row to
// produce the per-player launch URL.
func externalPlayersHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		token := externalPlaybackToken(c, svc)
		streamURL := absoluteRequestURL(c, "/api/stream/"+m.ID+"?token="+url.QueryEscape(token)+externalProfileQuery(c))
		escapedStream := url.QueryEscape(streamURL)
		c.JSON(http.StatusOK, gin.H{
			"url": streamURL,
			"players": []gin.H{
				{"name": "VLC", "scheme": "vlc://", "url": "vlc://" + streamURL},
				{"name": "PotPlayer", "scheme": "potplayer://", "url": "potplayer://" + streamURL},
				{"name": "MX Player", "scheme": "intent://", "url": "intent://" + streamURL + "#Intent;package=com.mxtech.videoplayer.ad;end"},
				{"name": "IINA", "scheme": "iina://", "url": "iina://weblink?url=" + escapedStream},
				{"name": "nPlayer", "scheme": "nplayer-", "url": "nplayer-" + streamURL},
			},
		})
	}
}

// externalURLHandler returns just the raw stream URL plus the auth
// token query string the external player needs.
func externalURLHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		token := externalPlaybackToken(c, svc)
		c.JSON(http.StatusOK, gin.H{
			"url":   absoluteRequestURL(c, "/api/stream/"+m.ID+"?token="+url.QueryEscape(token)+externalProfileQuery(c)),
			"token": token,
		})
	}
}

func externalProfileQuery(c *gin.Context) string {
	profileID := strings.TrimSpace(c.GetHeader("X-Play-Profile-ID"))
	if profileID == "" {
		profileID = strings.TrimSpace(c.Query("profile_id"))
	}
	if profileID == "" {
		return ""
	}
	query := "&profile_id=" + url.QueryEscape(profileID)
	pinToken := strings.TrimSpace(c.GetHeader("X-Play-Profile-PIN-Token"))
	if pinToken == "" {
		pinToken = strings.TrimSpace(c.Query("profile_pin_token"))
	}
	if pinToken != "" {
		query += "&profile_pin_token=" + url.QueryEscape(pinToken)
	}
	return query
}

func externalPlaybackToken(c *gin.Context, svc *service.Container) string {
	uid, _ := c.Get(middleware.CtxUserID)
	u, err := svc.Repo.User.FindByID(c.Request.Context(), toString(uid))
	if err != nil || u == nil {
		return ""
	}
	token, err := svc.Auth.IssueToken(u)
	if err != nil {
		return ""
	}
	return token
}

func absoluteRequestURL(c *gin.Context, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return scheme + "://" + host + path
}

func setRedirectNoStoreHeaders(c *gin.Context) {
	if c == nil {
		return
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// transcodeStatusHandler reports the live status of one transcode job.
// We surface the active jobs the transcoder knows about.
func transcodeStatusHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("job_id")
		for _, j := range svc.Transcoder.Active() {
			if j.MediaID == jobID {
				c.JSON(http.StatusOK, gin.H{"job_id": jobID, "status": "running", "job": j})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"job_id": jobID, "status": "idle"})
	}
}

// _ keeps imports tidy when the model package isn't otherwise used.
var _ = model.Media{}
var _ = service.Container{}

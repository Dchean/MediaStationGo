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
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"media":       m,
			"stream_url":  "/api/stream/" + m.ID,
			"hls_url":     "/api/hls/" + m.ID + "/index.m3u8",
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
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		streamURL := "/api/stream/" + m.ID
		c.JSON(http.StatusOK, gin.H{
			"players": []gin.H{
				{"name": "VLC", "scheme": "vlc://", "url": "vlc://" + streamURL},
				{"name": "PotPlayer", "scheme": "potplayer://", "url": "potplayer://" + streamURL},
				{"name": "MX Player", "scheme": "intent://", "url": "intent://" + streamURL + "#Intent;package=com.mxtech.videoplayer.ad;end"},
				{"name": "IINA", "scheme": "iina://", "url": "iina://weblink?url=" + streamURL},
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
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "media not found"})
			return
		}
		// Re-issue a short-lived token for this stream.
		uid, _ := c.Get(middleware.CtxUserID)
		u, err := svc.Repo.User.FindByID(c.Request.Context(), toString(uid))
		if err != nil || u == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		token, err := svc.Auth.IssueToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"url":   "/api/stream/" + m.ID + "?token=" + token,
			"token": token,
		})
	}
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

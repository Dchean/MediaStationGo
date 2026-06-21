// Package handler — HLS / image-proxy / scrape endpoints.
package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func hlsPlaylistHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMedia(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if !enforceScopedPlaybackToken(c, m.ID) {
			return
		}
		err = svc.Stream.ServeHLSPlaylist(c.Writer, c.Request, c.Param("id"))
		if errors.Is(err, service.ErrMediaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if errors.Is(err, service.ErrTranscodeDisabled) {
			c.JSON(http.StatusConflict, gin.H{"error": "transcode disabled"})
			return
		}
		if errors.Is(err, service.ErrTranscodeBusy) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "transcode busy"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
}

func hlsSegmentHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Media.GetMedia(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil || !mediaVisibleForRequest(c, svc, m) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if !enforceScopedPlaybackToken(c, m.ID) {
			return
		}
		err = svc.Stream.ServeHLSSegment(c.Writer, c.Request, c.Param("id"), c.Param("seg"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
	}
}

func stopTranscodeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		svc.Transcoder.StopJob(c.Param("id"))
		c.Status(http.StatusNoContent)
	}
}

func imageProxyHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.Query("url")
		// Serve handles upstream errors internally by returning a 1×1 PNG
		// placeholder, so the only error we can get back here is a malformed
		// URL. In that case we still return 400 to make the misuse visible.
		if err := svc.ImageProxy.Serve(c.Request.Context(), c.Writer, c.Request, raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
}

// scrapeOneHandler enriches a single media via the configured scraper chain.
func scrapeOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		task := startScrapeHTTPTask(svc, "手动刮削媒体", m.Title, m.Path)
		if err := svc.Scraper.EnrichOne(c.Request.Context(), m); err != nil {
			finishHTTPTask(task, err, "scrape", "手动刮削媒体失败", nil, nil)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		refreshed, _ := svc.Repo.Media.FindByID(c.Request.Context(), m.ID)
		metrics := map[string]int64{"processed": 1}
		if refreshed != nil && refreshed.ScrapeStatus == "matched" {
			metrics["matched"] = 1
		}
		finishHTTPTask(task, nil, "completed", "手动刮削媒体结束", metrics, nil)
		c.JSON(http.StatusOK, refreshed)
	}
}

// scrapeLibraryHandler retries every pending/no_match media in a library.
func scrapeLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libID := c.Param("id")
		var task *service.TaskHandle
		if lib, err := svc.Repo.Library.FindByID(c.Request.Context(), libID); err == nil && lib != nil {
			task = startScrapeHTTPTask(svc, "手动刮削媒体库", lib.Name, lib.Path)
		} else {
			task = startScrapeHTTPTask(svc, "手动刮削媒体库", libID, "")
		}
		// Run in the background so HTTP returns instantly; the WS hub
		// pushes per-item progress on the "scrape" topic.
		go func(libID string, task *service.TaskHandle) {
			matched, err := svc.Scraper.EnrichLibrary(context.Background(), libID, true)
			metrics := map[string]int64{"matched": int64(matched)}
			stage := "completed"
			message := "手动刮削媒体库结束"
			if err != nil {
				stage = "scrape"
				message = "手动刮削媒体库失败"
			}
			finishHTTPTask(task, err, stage, message, metrics, nil)
		}(libID, task)
		c.JSON(http.StatusAccepted, gin.H{"status": "scraping"})
	}
}

func startScrapeHTTPTask(svc *service.Container, name, title, path string) *service.TaskHandle {
	if svc == nil || svc.Tasks == nil {
		return nil
	}
	if title != "" {
		name += "：" + title
	}
	return svc.Tasks.Start(service.TaskKindScrape, name, service.TaskUpdate{
		Stage:      "scrape",
		SourcePath: path,
		Message:    "正在刮削元数据",
	})
}

// reprobeHandler re-runs ffprobe against a single media. Admin-only.
func reprobeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Stream.Probe(c.Request.Context(), c.Param("id"), svc.FFprobe); err != nil {
			if errors.Is(err, service.ErrMediaNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			// ffprobe unavailable or file inaccessible — still 200 with error info
			c.JSON(http.StatusOK, gin.H{"code": 1, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
	}
}

// Package handler — HLS / image-proxy / scrape endpoints.
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func hlsPlaylistHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := svc.Stream.ServeHLSPlaylist(c.Writer, c.Request, c.Param("id"))
		if errors.Is(err, service.ErrMediaNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
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
		err := svc.Stream.ServeHLSSegment(c.Writer, c.Request, c.Param("id"), c.Param("seg"))
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
		if err := svc.ImageProxy.Serve(c.Request.Context(), c.Writer, raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
}

// scrapeOneHandler enriches a single media via TMDb. Admin-only.
func scrapeOneHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		m, err := svc.Repo.Media.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || m == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if !svc.TMDb.Enabled() {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "tmdb api key not configured"})
			return
		}
		if err := svc.Scraper.EnrichOne(c.Request.Context(), m); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		refreshed, _ := svc.Repo.Media.FindByID(c.Request.Context(), m.ID)
		c.JSON(http.StatusOK, refreshed)
	}
}

// scrapeLibraryHandler enriches every pending media in a library. Admin-only.
func scrapeLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !svc.TMDb.Enabled() {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "tmdb api key not configured"})
			return
		}
		// Run in the background so HTTP returns instantly; the WS hub
		// pushes per-item progress on the "scrape" topic.
		go func(libID string) {
			_, _ = svc.Scraper.EnrichLibrary(c.Copy().Request.Context(), libID)
		}(c.Param("id"))
		c.JSON(http.StatusAccepted, gin.H{"status": "scraping"})
	}
}

// reprobeHandler re-runs ffprobe against a single media. Admin-only.
func reprobeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Stream.Probe(c.Request.Context(), c.Param("id"), svc.FFprobe); err != nil {
			if errors.Is(err, service.ErrMediaNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

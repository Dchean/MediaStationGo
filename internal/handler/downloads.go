// Package handler — download manager endpoints.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type addDownloadReq struct {
	URL      string `json:"url" binding:"required"`
	SavePath string `json:"save_path"`
}

func addDownloadHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req addDownloadReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		t, err := svc.Downloads.AddDownload(c.Request.Context(), uid.(string), req.URL, req.SavePath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		svc.Audit.Record(c.Request.Context(), uid.(string), "download.add", req.URL, c.ClientIP(), "")
		c.JSON(http.StatusOK, t)
	}
}

func listDownloadsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, live, err := svc.Downloads.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"tasks":    rows,
			"torrents": live,
		})
	}
}

func deleteDownloadHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		hash := c.Param("hash")
		withFiles := c.Query("delete_files") == "true"
		if err := svc.Downloads.Delete(c.Request.Context(), hash, withFiles); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func reloadDownloadConfigHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Downloads.ReloadConfig(c.Request.Context()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

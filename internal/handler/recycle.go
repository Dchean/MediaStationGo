// Package handler — recycle bin endpoints.
package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type recycleBatchReq struct {
	MediaIDs []string `json:"media_ids"`
}

func deleteMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Media.SoftDelete(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func listRecycleHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := svc.Media.ListRecycleBin(c.Request.Context(), 200)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func restoreMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Media.RestoreDeleted(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func restoreMediaBatchHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req recycleBatchReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		applied, errorsOut := runRecycleBatch(c, compactManualScrapeIDs(req.MediaIDs), svc.Media.RestoreDeleted)
		if applied == 0 && len(errorsOut) > 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": strings.Join(errorsOut, "\n")})
			return
		}
		c.JSON(http.StatusOK, gin.H{"applied": applied, "errors": errorsOut})
	}
}

func purgeMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Media.PurgeDeleted(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func purgeMediaBatchHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req recycleBatchReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		applied, errorsOut := runRecycleBatch(c, compactManualScrapeIDs(req.MediaIDs), svc.Media.PurgeDeleted)
		if applied == 0 && len(errorsOut) > 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": strings.Join(errorsOut, "\n")})
			return
		}
		c.JSON(http.StatusOK, gin.H{"applied": applied, "errors": errorsOut})
	}
}

func runRecycleBatch(c *gin.Context, ids []string, action func(context.Context, string) error) (int, []string) {
	if len(ids) == 0 {
		return 0, []string{"media_ids required"}
	}
	applied := 0
	errorsOut := make([]string, 0)
	for _, id := range ids {
		if err := action(c.Request.Context(), id); err != nil {
			errorsOut = append(errorsOut, id+": "+err.Error())
			continue
		}
		applied++
	}
	return applied, errorsOut
}

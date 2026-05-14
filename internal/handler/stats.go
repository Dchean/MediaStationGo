// Package handler — stats / dashboard endpoints.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func statsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		snap, err := svc.Stats.Compute(c.Request.Context(), svc.Cfg.App.DataDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, snap)
	}
}

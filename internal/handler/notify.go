// Package handler — notification test endpoint.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type notifyTestReq struct {
	Title string `json:"title" binding:"required"`
	Body  string `json:"body" binding:"required"`
}

func notifyTestHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req notifyTestReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		svc.Notifier.Send(c.Request.Context(), req.Title, req.Body, "test")
		c.JSON(http.StatusOK, gin.H{"message": "notification dispatched"})
	}
}

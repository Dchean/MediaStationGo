// Package handler — RSS subscription endpoints.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type subscriptionReq struct {
	Name    string `json:"name" binding:"required"`
	FeedURL string `json:"feed_url" binding:"required"`
	Filter  string `json:"filter"`
	Enabled *bool  `json:"enabled"`
}

func createSubscriptionHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req subscriptionReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		s := &model.Subscription{
			UserID:  uid.(string),
			Name:    req.Name,
			FeedURL: req.FeedURL,
			Filter:  req.Filter,
			Enabled: enabled,
		}
		if err := svc.Subscription.Create(c.Request.Context(), s); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, s)
	}
}

func listSubscriptionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := svc.Subscription.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

func deleteSubscriptionHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.Subscription.Delete(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func runSubscriptionHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		n, err := svc.Subscription.RunNow(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"queued": n})
	}
}

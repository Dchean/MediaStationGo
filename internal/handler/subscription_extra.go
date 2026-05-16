// Package handler — subscription update + per-subscription site search.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// updateSubscriptionHandler patches a subscription row.
func updateSubscriptionHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var patch model.Subscription
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := svc.Repo.DB.WithContext(c.Request.Context()).
			Model(&model.Subscription{}).
			Where("id = ?", c.Param("id")).
			Updates(map[string]any{
				"name":     patch.Name,
				"feed_url": patch.FeedURL,
				"filter":   patch.Filter,
				"enabled":  patch.Enabled,
			}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// searchSubscriptionHandler runs a one-off keyword search against the
// configured tracker sites for the given subscription. We treat the
// subscription's filter as the search term; this lets the UI preview
// what would be queued without actually downloading anything.
func searchSubscriptionHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var sub model.Subscription
		err := svc.Repo.DB.WithContext(c.Request.Context()).
			Where("id = ?", c.Param("id")).First(&sub).Error
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			return
		}
		keyword := sub.Filter
		if keyword == "" {
			keyword = sub.Name
		}
		results, err := svc.Site.Search(c.Request.Context(), keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": results, "subscription": sub})
	}
}

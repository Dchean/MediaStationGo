// Package handler — license key endpoints.
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type generateKeyReq struct {
	Customer       string `json:"customer"`
	Plan           string `json:"plan"`
	MaxActivations int    `json:"max_activations"`
	ExpiresAt      string `json:"expires_at,omitempty"` // RFC3339, "" = perpetual
	Notes          string `json:"notes,omitempty"`
}

func licenseGenerateHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req generateKeyReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var expires *time.Time
		if req.ExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, req.ExpiresAt)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "expires_at must be RFC3339"})
				return
			}
			expires = &t
		}
		k, err := svc.License.Generate(
			c.Request.Context(),
			req.Customer, req.Plan, req.Notes,
			req.MaxActivations, expires,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, k)
	}
}

func licenseListHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.License.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, rows)
	}
}

type activateReq struct {
	Key        string `json:"key" binding:"required"`
	DeviceID   string `json:"device_id" binding:"required"`
	DeviceName string `json:"device_name"`
}

func licenseActivateHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req activateReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		a, err := svc.License.Activate(
			c.Request.Context(), req.Key, req.DeviceID, req.DeviceName, c.ClientIP(),
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, a)
	}
}

func licenseListActivationsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.License.ListActivations(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, rows)
	}
}

func licenseUnbindHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.License.Unbind(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func licenseRevokeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.License.Revoke(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func licenseHeartbeatHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		actID := c.Query("activation_id")
		if actID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "activation_id required"})
			return
		}
		if err := svc.License.Heartbeat(c.Request.Context(), actID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func licenseStatusHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyID := c.Query("key_id")
		if keyID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "key_id required"})
			return
		}
		out, err := svc.License.Status(c.Request.Context(), keyID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

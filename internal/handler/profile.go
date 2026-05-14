// Package handler — user profile endpoints.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func updateProfileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var patch service.ProfileUpdate
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		u, err := svc.Profile.UpdateProfile(c.Request.Context(), uid.(string), patch)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

type adminUpdateRoleReq struct {
	Role string `json:"role" binding:"required"`
}

func adminUpdateRoleHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req adminUpdateRoleReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		u, err := svc.Profile.AdminUpdateRole(c.Request.Context(), c.Param("id"), req.Role)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

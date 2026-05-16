// Package handler — per-user feature toggle endpoints.
//
//	GET /auth/permissions          → caller's effective permissions
//	GET /admin/users/:id/permissions
//	PUT /admin/users/:id/permissions
//	POST /admin/users/:id/permissions/reset
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func myPermissionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		row, err := svc.Permissions.Effective(c.Request.Context(), toString(uid))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if row == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusOK, row)
	}
}

func getUserPermissionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		row, err := svc.Permissions.Effective(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if row == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusOK, row)
	}
}

func updateUserPermissionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var p model.UserPermission
		if err := c.ShouldBindJSON(&p); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := svc.Permissions.Save(c.Request.Context(), c.Param("id"), &p); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, p)
	}
}

func resetUserPermissionsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		row, err := svc.Permissions.Reset(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, row)
	}
}

// Package handler — multi-persona play profile CRUD endpoints.
//
// Non-admin users see / mutate only their own profiles. Admins see
// every profile so they can manage child accounts, etc.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// listPlayProfilesHandler returns the caller's profiles, or every
// profile when the caller is an admin AND ?all=true is set.
func listPlayProfilesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		role, _ := c.Get(middleware.CtxUserRole)
		if c.Query("all") == "true" && role == "admin" {
			rows, err := svc.PlayProfiles.List(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, rows)
			return
		}
		rows, err := svc.PlayProfiles.ListByUser(c.Request.Context(), toString(uid))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, rows)
	}
}

func createPlayProfileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in service.PlayProfileInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Default the user_id to the caller; admins can override.
		uid, _ := c.Get(middleware.CtxUserID)
		role, _ := c.Get(middleware.CtxUserRole)
		if in.UserID == "" || role != "admin" {
			in.UserID = toString(uid)
		}
		row, err := svc.PlayProfiles.Create(c.Request.Context(), in)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, row)
	}
}

func updatePlayProfileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in service.PlayProfileInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		row, err := svc.PlayProfiles.Update(c.Request.Context(), c.Param("id"), in)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, row)
	}
}

func deletePlayProfileHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := svc.PlayProfiles.Delete(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// Package handler — auth-related HTTP endpoints.
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type registerReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

func loginHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		u, token, err := svc.Auth.Login(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, service.ErrInvalidCredentials) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"token": token,
			"user":  u,
		})
		svc.Audit.Record(c.Request.Context(), u.ID, "auth.login", u.Username, c.ClientIP(), "")
	}
}

func registerHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		u, err := svc.Auth.Register(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, service.ErrUsernameTaken) {
				c.JSON(http.StatusConflict, gin.H{"error": "username taken"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

func meHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, _ := c.Get(middleware.CtxUserID)
		u, err := svc.Repo.User.FindByID(c.Request.Context(), uid.(string))
		if err != nil || u == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

type changePwdReq struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

func changePasswordHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req changePwdReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uid, _ := c.Get(middleware.CtxUserID)
		if err := svc.Auth.ChangePassword(c.Request.Context(), uid.(string), req.OldPassword, req.NewPassword); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	}
}

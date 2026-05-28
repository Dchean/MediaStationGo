// Package middleware 暴露 Gin 中间件，用于 HTTP 服务器：
// 请求日志、CORS、JWT 认证、管理员守卫和权限检查。
package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// Context keys for values produced by the auth middleware.
const (
	CtxUserID   = "ctx_user_id"
	CtxUserRole = "ctx_user_role"
	CtxUserTier = "ctx_user_tier"
)

// RequestLogger logs one structured line per request.
func RequestLogger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("dur", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		)
	}
}

// CORS implements a permissive cross-origin policy when origins is empty
// (development convenience) and a strict allow-list otherwise.
func CORS(origins []string) gin.HandlerFunc {
	allowAll := len(origins) == 0
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if allowAll {
			c.Header("Access-Control-Allow-Origin", "*")
		} else if _, ok := allowed[origin]; ok && origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With, X-Emby-Token, X-MediaBrowser-Token, X-Emby-Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// Claims is the JWT payload we issue.
type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	Tier   string `json:"tier,omitempty"`
	jwt.RegisteredClaims
}

// AuthRequired parses and validates a JWT from the Authorization header
// (Bearer ...) or the `token` query parameter (used by <video>.src).
func AuthRequired(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := extractToken(c)
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 40101, "message": "missing token"})
			return
		}
		claims := &Claims{}
		_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		})
		if err != nil || claims.UserID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 40101, "message": "invalid token"})
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUserRole, claims.Role)
		c.Set(CtxUserTier, claims.Tier)
		c.Next()
	}
}

// AdminRequired must run AFTER AuthRequired; it enforces role == "admin".
func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(CtxUserRole)
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 40301, "message": "admin only"})
			return
		}
		c.Next()
	}
}

// PlusOrAdminRequired enforces role == "admin" or tier == "plus".
func PlusOrAdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(CtxUserRole)
		tier, _ := c.Get(CtxUserTier)
		if role != "admin" && tier != "plus" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 40301, "message": "plus or admin only"})
			return
		}
		c.Next()
	}
}

// GetUserID extracts the user ID from the Gin context.
func GetUserID(c *gin.Context) string {
	if uid, exists := c.Get(CtxUserID); exists {
		return uid.(string)
	}
	return ""
}

// GetUserRole extracts the user role from the Gin context.
func GetUserRole(c *gin.Context) string {
	if role, exists := c.Get(CtxUserRole); exists {
		return role.(string)
	}
	return ""
}

// GetUserTier extracts the user tier from the Gin context.
func GetUserTier(c *gin.Context) string {
	if tier, exists := c.Get(CtxUserTier); exists {
		return tier.(string)
	}
	return ""
}

// IsAdmin checks if the current user is an admin.
func IsAdmin(c *gin.Context) bool {
	return GetUserRole(c) == "admin"
}

// IsPlus checks if the current user is a plus subscriber.
func IsPlus(c *gin.Context) bool {
	return GetUserTier(c) == "plus" || GetUserRole(c) == "admin"
}

// IsSuperUser checks if the current user is a super user (admin or plus).
func IsSuperUser(c *gin.Context) bool {
	return IsAdmin(c) || IsPlus(c)
}

func extractToken(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	for _, header := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	for _, key := range []string{"token", "api_key", "apiKey", "ApiKey"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	return ""
}

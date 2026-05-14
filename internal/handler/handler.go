// Package handler wires the HTTP routes to the service container.
//
// All routes are mounted under /api/* (matching the original MediaStation
// surface) so the frontend dev-server can proxy a single prefix.
package handler

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// Register attaches every API route to the engine.
func Register(r *gin.Engine, cfg *config.Config, log *zap.Logger, svc *service.Container) {
	api := r.Group("/api")
	{
		api.GET("/health", healthCheck)
		api.GET("/version", versionInfo)

		// Public auth.
		auth := api.Group("/auth")
		{
			auth.POST("/login", loginHandler(svc))
			auth.POST("/register", registerHandler(svc))
		}

		// Authenticated endpoints.
		authed := api.Group("/")
		authed.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret))
		{
			authed.GET("/me", meHandler(svc))
			authed.POST("/me/password", changePasswordHandler(svc))

			authed.GET("/libraries", listLibrariesHandler(svc))
			authed.POST("/libraries", middleware.AdminRequired(), createLibraryHandler(svc))
			authed.DELETE("/libraries/:id", middleware.AdminRequired(), deleteLibraryHandler(svc))
			authed.POST("/libraries/:id/scan", middleware.AdminRequired(), scanLibraryHandler(svc))
			authed.POST("/libraries/:id/scrape", middleware.AdminRequired(), scrapeLibraryHandler(svc))

			authed.GET("/libraries/:id/media", listMediaHandler(svc))
			authed.GET("/media/:id", getMediaHandler(svc))
			authed.GET("/media", searchMediaHandler(svc))
			authed.POST("/media/:id/scrape", middleware.AdminRequired(), scrapeOneHandler(svc))
			authed.POST("/media/:id/probe", middleware.AdminRequired(), reprobeHandler(svc))

			// Streaming.
			authed.GET("/stream/:id", streamHandler(svc))
			authed.GET("/hls/:id/index.m3u8", hlsPlaylistHandler(svc))
			authed.GET("/hls/:id/:seg", hlsSegmentHandler(svc))
			authed.DELETE("/hls/:id", stopTranscodeHandler(svc))

			// Image proxy (URL passed as ?url=...).
			authed.GET("/img", imageProxyHandler(svc))

			// History / favourites / playlists.
			authed.GET("/history", recentHistoryHandler(svc))
			authed.POST("/history", recordProgressHandler(svc))

			authed.GET("/favourites", listFavouritesHandler(svc))
			authed.POST("/favourites/:id", toggleFavouriteHandler(svc))

			authed.GET("/playlists", listPlaylistsHandler(svc))
			authed.POST("/playlists", createPlaylistHandler(svc))
			authed.GET("/playlists/:id", getPlaylistHandler(svc))
			authed.POST("/playlists/:id/items", addPlaylistItemHandler(svc))
			authed.DELETE("/playlists/:id/items/:media_id", removePlaylistItemHandler(svc))
			authed.DELETE("/playlists/:id", deletePlaylistHandler(svc))

			authed.GET("/ws", wsHandler(svc))
		}

		// Admin-only endpoints.
		admin := api.Group("/admin")
		admin.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret), middleware.AdminRequired())
		{
			admin.GET("/users", listUsersHandler(svc))
			admin.DELETE("/users/:id", deleteUserHandler(svc))
			admin.GET("/settings", listSettingsHandler(svc))
			admin.PUT("/settings", updateSettingHandler(svc))
			admin.GET("/logs", recentLogsHandler(svc))
		}
	}
}

func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func versionInfo(c *gin.Context) {
	c.JSON(200, gin.H{"name": "MediaStationGo", "version": "0.1.0"})
}

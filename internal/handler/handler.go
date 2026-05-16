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
			authed.PATCH("/me", updateProfileHandler(svc))
			authed.POST("/me/password", changePasswordHandler(svc))

			// Libraries.
			authed.GET("/libraries", listLibrariesHandler(svc))
			authed.POST("/libraries", middleware.AdminRequired(), createLibraryHandler(svc))
			authed.DELETE("/libraries/:id", middleware.AdminRequired(), deleteLibraryHandler(svc))
			authed.POST("/libraries/:id/scan", middleware.AdminRequired(), scanLibraryHandler(svc))
			authed.POST("/libraries/:id/scrape", middleware.AdminRequired(), scrapeLibraryHandler(svc))

			authed.GET("/libraries/:id/media", listMediaHandler(svc))
			authed.GET("/libraries/:id/seasons", listSeasonsHandler(svc))

			// Media.
			authed.GET("/media/:id", getMediaHandler(svc))
			authed.GET("/media", searchMediaHandler(svc))
			authed.POST("/media/:id/scrape", middleware.AdminRequired(), scrapeOneHandler(svc))
			authed.POST("/media/:id/probe", middleware.AdminRequired(), reprobeHandler(svc))
			authed.DELETE("/media/:id", middleware.AdminRequired(), deleteMediaHandler(svc))
			authed.POST("/media/:id/restore", middleware.AdminRequired(), restoreMediaHandler(svc))
			authed.DELETE("/media/:id/purge", middleware.AdminRequired(), purgeMediaHandler(svc))
			authed.GET("/media/:id/subtitles", listSubtitlesHandler(svc))
			authed.GET("/subtitles/:id", serveSubtitleHandler(svc))
			authed.POST("/media/:id/nfo", middleware.AdminRequired(), exportNFOHandler(svc))
			authed.POST("/libraries/:id/nfo", middleware.AdminRequired(), exportLibraryNFOHandler(svc))

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

			// Downloads.
			authed.GET("/downloads", listDownloadsHandler(svc))
			authed.POST("/downloads", addDownloadHandler(svc))
			authed.DELETE("/downloads/:hash", middleware.AdminRequired(), deleteDownloadHandler(svc))
			authed.POST("/downloads/reload", middleware.AdminRequired(), reloadDownloadConfigHandler(svc))

			// Subscriptions.
			authed.GET("/subscriptions", listSubscriptionsHandler(svc))
			authed.POST("/subscriptions", createSubscriptionHandler(svc))
			authed.DELETE("/subscriptions/:id", deleteSubscriptionHandler(svc))
			authed.POST("/subscriptions/:id/run", runSubscriptionHandler(svc))

			// Stats / dashboard.
			authed.GET("/stats", statsHandler(svc))
			authed.GET("/tasks", tasksHandler(svc))

			// Discover (TMDb trending / popular).
			authed.GET("/discover/trending", trendingHandler(svc))
			authed.GET("/discover/popular", popularHandler(svc))

			// AI.
			authed.GET("/ai/status", aiStatusHandler(svc))
			authed.POST("/ai/search", smartSearchHandler(svc))
			authed.GET("/ai/recommend", aiRecommendHandler(svc))

			// File browser (used by the library-path picker).
			authed.GET("/files", browseFilesHandler(svc))

			// Disk usage breakdown.
			authed.GET("/storage", storageHandler(svc))

			// DLNA discovery + cast.
			authed.GET("/dlna/devices", dlnaListHandler(svc))
			authed.POST("/dlna/cast", dlnaCastHandler(svc))

			// STRM (URL-as-file).
			authed.PUT("/media/:id/strm", middleware.AdminRequired(), setSTRMHandler(svc))
			authed.DELETE("/media/:id/strm", middleware.AdminRequired(), clearSTRMHandler(svc))
			authed.POST("/strm/import", middleware.AdminRequired(), importSTRMHandler(svc))

			// Duplicate finder.
			authed.POST("/duplicates/scan", middleware.AdminRequired(), detectDuplicatesHandler(svc))
			authed.POST("/duplicates/unmark", middleware.AdminRequired(), unmarkDuplicatesHandler(svc))

			// Site management + cross-site torrent search.
			authed.GET("/sites", listSitesHandler(svc))
			authed.GET("/sites/:id", getSiteHandler(svc))
			authed.POST("/sites", middleware.AdminRequired(), createSiteHandler(svc))
			authed.PUT("/sites/:id", middleware.AdminRequired(), updateSiteHandler(svc))
			authed.DELETE("/sites/:id", middleware.AdminRequired(), deleteSiteHandler(svc))
			authed.POST("/sites/:id/test", middleware.AdminRequired(), testSiteHandler(svc))
			authed.GET("/sites/search", siteSearchHandler(svc))

			// Recycle bin.
			authed.GET("/recycle", middleware.AdminRequired(), listRecycleHandler(svc))

			authed.GET("/ws", wsHandler(svc))
		}

		// Admin-only endpoints.
		admin := api.Group("/admin")
		admin.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret), middleware.AdminRequired())
		{
			admin.GET("/users", listUsersHandler(svc))
			admin.PATCH("/users/:id/role", adminUpdateRoleHandler(svc))
			admin.DELETE("/users/:id", deleteUserHandler(svc))
			admin.GET("/settings", listSettingsHandler(svc))
			admin.PUT("/settings", updateSettingHandler(svc))
			admin.GET("/logs", recentLogsHandler(svc))

			// Database backup.
			admin.GET("/backups", listBackupsHandler(svc))
			admin.POST("/backups", createBackupHandler(svc))
			admin.DELETE("/backups", deleteBackupHandler(svc))
			admin.POST("/backups/restore", restoreBackupHandler(svc))

			// Notifications (test endpoint).
			admin.POST("/notify/test", notifyTestHandler(svc))

			// File organizer.
			admin.POST("/media/:id/organize", organizeMediaHandler(svc))
			admin.POST("/libraries/:id/organize", organizeLibraryHandler(svc))

			// API key management (encrypted at rest).
			admin.GET("/api-configs", listAPIConfigsHandler(svc))
			admin.GET("/api-configs/:provider", getAPIConfigHandler(svc))
			admin.PUT("/api-configs/:provider", updateAPIConfigHandler(svc))
			admin.DELETE("/api-configs/:provider", deleteAPIConfigHandler(svc))

			// Scheduled jobs.
			admin.GET("/scheduler", schedulerStatusHandler(svc))
			admin.POST("/scheduler/:name/run", schedulerRunHandler(svc))
		}

		// Emby/Jellyfin compatibility shim (read-only).
		// Mounted at /emby/* (NOT /api/*) to mirror the upstream surface.
	}

	emby := r.Group("/emby")
	emby.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret))
	{
		emby.GET("/System/Info", embySystemInfoHandler(svc))
		emby.GET("/Users", embyListUsersHandler(svc))
		emby.GET("/Users/:userId/Views", embyViewsHandler(svc))
		emby.GET("/Users/:userId/Items", embyItemsHandler(svc))
		emby.GET("/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))
	}
}

func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}

func versionInfo(c *gin.Context) {
	c.JSON(200, gin.H{"name": "MediaStationGo", "version": "0.1.0"})
}

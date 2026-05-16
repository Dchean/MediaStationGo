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

			// ── Auxiliary endpoints used by the React UI rails ──
			authed.GET("/media/recent", recentMediaHandler(svc))
			authed.GET("/media/stats", mediaStatsHandler(svc))

			// Watch history (extra surface beyond /history).
			authed.GET("/watch-history", historyListHandler(svc))
			authed.GET("/watch-history/stats", historyStatsHandler(svc))
			authed.GET("/watch-history/continue", historyContinueHandler(svc))
			authed.DELETE("/watch-history", historyDeleteHandler(svc))
			authed.DELETE("/watch-history/:id", historyDeleteOneHandler(svc))

			// Multi-section TMDb feed used by DiscoverPage.
			authed.GET("/discover/sections", discoverSectionsHandler(svc))
			authed.GET("/discover/feed", discoverFeedHandler(svc))

			// System metadata + read-only scheduler view.
			authed.GET("/system/info", systemInfoHandler(svc))
			authed.GET("/system/status", systemStatusHandler(svc))
			authed.GET("/system/scheduler", systemSchedulerHandler(svc))

			// Richer dashboard rails.
			authed.GET("/stats/overview", statsOverviewHandler(svc))
			authed.GET("/stats/trend", statsTrendHandler(svc))
			authed.GET("/stats/top-content", statsTopContentHandler(svc))
			authed.GET("/stats/libraries", statsLibrariesHandler(svc))
			authed.GET("/stats/monitor", statsMonitorHandler(svc))

			// Multi-persona play profiles (caller-scoped, admins via ?all=true).
			authed.GET("/play-profiles", listPlayProfilesHandler(svc))
			authed.POST("/play-profiles", createPlayProfileHandler(svc))
			authed.PUT("/play-profiles/:id", updatePlayProfileHandler(svc))
			authed.DELETE("/play-profiles/:id", deletePlayProfileHandler(svc))

			// ── Auth extras ──
			authed.POST("/auth/refresh", refreshHandler(svc))
			authed.POST("/auth/logout", logoutHandler(svc))
			authed.GET("/auth/me", meHandler(svc))
			authed.PATCH("/auth/profile", updateProfileHandler(svc))
			authed.POST("/auth/change-password", changePasswordHandler(svc))
			authed.GET("/auth/permissions", myPermissionsHandler(svc))

			// ── Search aliases ──
			authed.GET("/search", searchUnifiedHandler(svc))
			authed.GET("/search/advanced", searchAdvancedHandler(svc))
			authed.GET("/search/tmdb", searchTMDbHandler(svc))
			authed.GET("/search/sites", searchSitesHandler(svc))

			// ── System extras ──
			authed.GET("/system/config", listSystemConfigHandler(svc))
			authed.GET("/settings/schema", schemaHandler(svc))
			authed.GET("/system/events/ticket", systemEventsTicketHandler(svc))

			// ── Per-user stats ──
			authed.GET("/stats/user/:id", statsUserHandler(svc))
			authed.GET("/stats/top-users", statsTopUsersHandler(svc))
			authed.POST("/stats/play", statsPlayHandler(svc))

			// ── Sites extras ──
			authed.GET("/sites/:id/resource", siteResourceHandler(svc))
			authed.GET("/sites/:id/userdata", siteUserdataHandler(svc))

			// ── Subscription extras ──
			authed.PUT("/subscriptions/:id", updateSubscriptionHandler(svc))
			authed.POST("/subscriptions/:id/search", searchSubscriptionHandler(svc))

			// ── Playlist extras ──
			authed.POST("/playlists/:id/reorder", reorderPlaylistHandler(svc))
			authed.DELETE("/playlists/:id/items/by-id/:item_id", deletePlaylistItemByIDHandler(svc))

			// ── DLNA per-renderer control ──
			authed.POST("/dlna/:uuid/play", dlnaPlayHandler(svc))
			authed.POST("/dlna/:uuid/pause", dlnaPauseHandler(svc))
			authed.POST("/dlna/:uuid/stop", dlnaStopHandler(svc))
			authed.GET("/dlna/:uuid/status", dlnaStatusHandler(svc))

			// ── Media favourite alias surface ──
			authed.GET("/favorites", listFavoritesAliasHandler(svc))
			authed.POST("/media/:id/favorite", addMediaFavoriteHandler(svc))
			authed.DELETE("/media/:id/favorite", removeMediaFavoriteHandler(svc))
			authed.GET("/media/:id/favorite/status", getMediaFavoriteStatusHandler(svc))
			authed.POST("/media/:id/ai-scrape", aiScrapeMediaHandler(svc))
			authed.POST("/media/scrape/test", scrapeTestHandler(svc))
			authed.POST("/media/organize", middleware.AdminRequired(), organizeBulkHandler(svc))

			// ── Playback metadata + external player handoff ──
			authed.GET("/playback/:id/info", playbackInfoHandler(svc))
			authed.POST("/playback/:id/progress", playbackProgressHandler(svc))
			authed.GET("/playback/:id/external-players", externalPlayersHandler(svc))
			authed.GET("/playback/:id/external-url", externalURLHandler(svc))
			authed.GET("/playback/transcode/:job_id/status", transcodeStatusHandler(svc))

			// ── Download task ops + sync triggers ──
			authed.POST("/download/:id/pause", downloadPauseHandler(svc))
			authed.POST("/download/:id/resume", downloadResumeHandler(svc))
			authed.POST("/download/:id/organize", middleware.AdminRequired(), downloadOrganizeOneHandler(svc))
			authed.POST("/download/organize", middleware.AdminRequired(), downloadOrganizeAllHandler(svc))
			authed.POST("/download/sync", middleware.AdminRequired(), downloadSyncHandler(svc))
			authed.POST("/download/start-auto-sync", middleware.AdminRequired(), downloadAutoSyncHandler(svc))
			authed.GET("/download/tasks", downloadTasksAliasHandler(svc))
			authed.POST("/download/add", addDownloadHandler(svc))

			// ── License (anyone authenticated can activate / heartbeat) ──
			authed.POST("/license/activate", licenseActivateHandler(svc))
			authed.POST("/license/heartbeat", licenseHeartbeatHandler(svc))
			authed.GET("/license/status", licenseStatusHandler(svc))
			authed.GET("/license/heartbeat-status", licenseStatusHandler(svc))

			// ── Assistant (multi-turn AI chat) ──
			authed.GET("/admin/assistant/sessions", listAssistantSessionsHandler(svc))
			authed.POST("/admin/assistant/sessions", createAssistantSessionHandler(svc))
			authed.GET("/admin/assistant/session/:id", getAssistantSessionHandler(svc))
			authed.DELETE("/admin/assistant/session/:id", deleteAssistantSessionHandler(svc))
			authed.POST("/admin/assistant/chat", assistantChatHandler(svc))
			authed.POST("/admin/assistant/execute", assistantExecuteHandler(svc))
			authed.POST("/admin/assistant/undo/:op_id", assistantUndoHandler(svc))
			authed.GET("/admin/assistant/history", assistantHistoryHandler(svc))
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

			// Permissions admin.
			admin.GET("/users/:id/permissions", getUserPermissionsHandler(svc))
			admin.PUT("/users/:id/permissions", updateUserPermissionsHandler(svc))
			admin.POST("/users/:id/permissions/reset", resetUserPermissionsHandler(svc))

			// Storage configs (Alist / S3 / WebDAV).
			admin.GET("/storage/status", listStorageConfigsHandler(svc))
			admin.GET("/storage/:type", getStorageConfigHandler(svc))
			admin.PUT("/storage/:type", saveStorageConfigHandler(svc))
			admin.POST("/storage/:type/test", testStorageConfigHandler(svc))

			// Download client CRUD.
			admin.GET("/download/clients", listDownloadClientsHandler(svc))
			admin.POST("/download/clients", createDownloadClientHandler(svc))
			admin.PUT("/download/clients/:id", updateDownloadClientHandler(svc))
			admin.DELETE("/download/clients/:id", deleteDownloadClientHandler(svc))
			admin.POST("/download/clients/:id/test", testDownloadClientHandler(svc))
			admin.GET("/download/aria2/stats", aria2StatsHandler(svc))

			// License generation / revocation.
			admin.POST("/license/generate", licenseGenerateHandler(svc))
			admin.GET("/license/list", licenseListHandler(svc))
			admin.GET("/license/:id/activations", licenseListActivationsHandler(svc))
			admin.POST("/license/activation/:id/unbind", licenseUnbindHandler(svc))
			admin.POST("/license/:id/revoke", licenseRevokeHandler(svc))

			// System scheduler trigger alias.
			admin.POST("/system/scheduler/:name/trigger", schedulerTriggerHandler(svc))

			// Database backup.
			admin.GET("/backups", listBackupsHandler(svc))
			admin.POST("/backups", createBackupHandler(svc))
			admin.DELETE("/backups", deleteBackupHandler(svc))
			admin.POST("/backups/restore", restoreBackupHandler(svc))

			// Notifications (test endpoint).
			admin.POST("/notify/test", notifyTestHandler(svc))

			// Notify channels CRUD + per-channel test.
			admin.GET("/notify/channels", listNotifyChannelsHandler(svc))
			admin.POST("/notify/channels", createNotifyChannelHandler(svc))
			admin.PUT("/notify/channels/:id", updateNotifyChannelHandler(svc))
			admin.DELETE("/notify/channels/:id", deleteNotifyChannelHandler(svc))
			admin.POST("/notify/channels/:id/test", testNotifyChannelHandler(svc))

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

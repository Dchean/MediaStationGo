// Package handler — authenticated application routes.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func registerAuthenticatedRoutes(api *gin.RouterGroup, cfg *config.Config, svc *service.Container) {
	// Authenticated endpoints.
	authed := api.Group("/")
	authed.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret))
	authed.Use(activeUserRequired(svc))
	{
		authed.GET("/me", meHandler(svc))
		authed.PATCH("/me", updateProfileHandler(svc))
		authed.POST("/me/password", changePasswordHandler(svc))
		authed.POST("/me/logout", logoutHandler(svc))

		// Permissions.
		authed.GET("/auth/permissions", getMyPermissionsHandler(svc))

		// License activation bridge (admin only; talks to the configured license server).
		authed.GET("/license/status", middleware.AdminRequired(), licenseStatusHandler(svc))
		authed.POST("/license/activate", middleware.AdminRequired(), licenseActivateHandler(svc))
		authed.POST("/license/heartbeat", middleware.AdminRequired(), licenseHeartbeatHandler(svc))

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
		authed.PATCH("/media/:id/metadata", middleware.AdminRequired(), updateMediaMetadataHandler(svc))
		authed.POST("/media/:id/scrape", middleware.AdminRequired(), scrapeOneHandler(svc))
		authed.GET("/media/:id/scrape/search", middleware.AdminRequired(), manualScrapeSearchHandler(svc))
		authed.POST("/media/:id/scrape/apply", middleware.AdminRequired(), manualScrapeApplyOneHandler(svc))
		authed.POST("/media/scrape/apply", middleware.AdminRequired(), manualScrapeApplyBatchHandler(svc))
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
		authed.HEAD("/stream/:id", streamHandler(svc))
		authed.GET("/hls/:id/index.m3u8", hlsPlaylistHandler(svc))
		authed.GET("/hls/:id/:seg", hlsSegmentHandler(svc))
		authed.DELETE("/hls/:id", stopTranscodeHandler(svc))

		// Cloud-disk 302 playback redirect (resolves a fresh direct link).
		authed.GET("/cloud/play/:type", cloudPlayHandler(svc))
		authed.HEAD("/cloud/play/:type", cloudPlayHandler(svc))

		// Image proxy (URL passed as ?url=...).
		authed.GET("/img", imageProxyHandler(svc))

		// History / favourites / playlists.
		authed.GET("/history", recentHistoryHandler(svc))
		authed.POST("/history", recordProgressHandler(svc))

		authed.GET("/favourites", listFavouritesHandler(svc))
		authed.POST("/favourites/:id", toggleFavouriteHandler(svc))

		// Storage breakdown.
		authed.GET("/storage", storageBreakdownHandler(svc))

		authed.GET("/playlists", listPlaylistsHandler(svc))
		authed.POST("/playlists", createPlaylistHandler(svc))
		authed.GET("/playlists/:id", getPlaylistHandler(svc))
		authed.POST("/playlists/:id/items", addPlaylistItemHandler(svc))
		authed.DELETE("/playlists/:id/items/:media_id", removePlaylistItemHandler(svc))
		authed.DELETE("/playlists/:id", deletePlaylistHandler(svc))

		// Downloads.
		authed.GET("/downloads", requirePermission(svc, "can_manage_downloads"), listDownloadsHandler(svc))
		authed.POST("/downloads", requirePermission(svc, "can_manage_downloads"), addDownloadHandler(svc))
		authed.DELETE("/downloads/:hash", requirePermission(svc, "can_manage_downloads"), deleteDownloadHandler(svc))
		authed.POST("/downloads/relocate", requirePermission(svc, "can_manage_downloads"), relocateDownloadHandler(svc))
		authed.POST("/downloads/reload", requirePermission(svc, "can_manage_downloads"), reloadDownloadConfigHandler(svc))

		// Subscriptions.
		authed.GET("/subscriptions", requirePermission(svc, "can_manage_subscriptions"), listSubscriptionsHandler(svc))
		authed.GET("/subscriptions/history", requirePermission(svc, "can_manage_subscriptions"), listSubscriptionHistoryHandler(svc))
		authed.POST("/subscriptions", requirePermission(svc, "can_manage_subscriptions"), createSubscriptionHandler(svc))
		authed.DELETE("/subscriptions/:id", requirePermission(svc, "can_manage_subscriptions"), deleteSubscriptionHandler(svc))
		authed.POST("/subscriptions/:id/restore", requirePermission(svc, "can_manage_subscriptions"), restoreSubscriptionHandler(svc))
		authed.POST("/subscriptions/:id/run", requirePermission(svc, "can_manage_subscriptions"), runSubscriptionHandler(svc))

		// Stats / dashboard.
		authed.GET("/stats", statsHandler(svc))
		authed.GET("/tasks", middleware.AdminRequired(), tasksHandler(svc))

		// Discover (TMDb trending / popular).
		authed.GET("/discover/trending", requirePermission(svc, "can_view_discover"), trendingHandler(svc))
		authed.GET("/discover/popular", requirePermission(svc, "can_view_discover"), popularHandler(svc))

		// AI.
		authed.GET("/ai/status", requirePermission(svc, "can_use_ai"), aiStatusHandler(svc))
		authed.POST("/ai/search", requirePermission(svc, "can_use_ai"), smartSearchHandler(svc))
		authed.GET("/ai/recommend", requirePermission(svc, "can_use_ai"), aiRecommendHandler(svc))

		// File browser (used by the library-path picker).
		authed.GET("/files", middleware.AdminRequired(), browseFilesHandler(svc))
		authed.POST("/files/folders", middleware.AdminRequired(), createFolderHandler(svc))
		authed.PUT("/files/rename", middleware.AdminRequired(), renameFileHandler(svc))
		authed.DELETE("/files", middleware.AdminRequired(), deleteFileHandler(svc))
		authed.POST("/files/transfer", middleware.AdminRequired(), transferFileHandler(svc))

		// DLNA discovery + cast.
		authed.GET("/dlna/devices", dlnaListHandler(svc))
		authed.POST("/dlna/cast", dlnaCastHandler(svc))

		// STRM (URL-as-file).
		authed.PUT("/media/:id/strm", middleware.AdminRequired(), setSTRMHandler(svc))
		authed.DELETE("/media/:id/strm", middleware.AdminRequired(), clearSTRMHandler(svc))
		authed.POST("/strm/import", middleware.AdminRequired(), importSTRMHandler(svc))
		authed.POST("/strm/generate", middleware.AdminRequired(), generateSTRMHandler(svc))

		// Duplicate finder.
		authed.GET("/duplicates", middleware.AdminRequired(), listDuplicatesHandler(svc))
		authed.POST("/duplicates/scan", middleware.AdminRequired(), detectDuplicatesHandler(svc))
		authed.POST("/duplicates/unmark", middleware.AdminRequired(), unmarkDuplicatesHandler(svc))

		// Site management + cross-site torrent search (via SiteHandler).
		siteHandler := NewSiteHandler(svc)
		authed.GET("/sites", requirePermission(svc, "can_manage_sites"), siteHandler.ListSites)
		authed.GET("/sites/types", requirePermission(svc, "can_manage_sites"), siteHandler.GetSiteTypes)
		authed.GET("/sites/auth-types", requirePermission(svc, "can_manage_sites"), siteHandler.GetAuthTypes)
		authed.POST("/sites", requirePermission(svc, "can_manage_sites"), siteHandler.CreateSite)
		authed.GET("/sites/:id", requirePermission(svc, "can_manage_sites"), siteHandler.GetSite)
		authed.PUT("/sites/:id", requirePermission(svc, "can_manage_sites"), siteHandler.UpdateSite)
		authed.DELETE("/sites/:id", requirePermission(svc, "can_manage_sites"), siteHandler.DeleteSite)
		authed.POST("/sites/:id/test", requirePermission(svc, "can_manage_sites"), siteHandler.TestSite)
		authed.GET("/sites/search", requirePermission(svc, "can_manage_sites"), siteSearchHandler(svc))

		// Recycle bin.
		authed.GET("/recycle", middleware.AdminRequired(), listRecycleHandler(svc))
		authed.POST("/recycle/restore", middleware.AdminRequired(), restoreMediaBatchHandler(svc))
		authed.POST("/recycle/purge", middleware.AdminRequired(), purgeMediaBatchHandler(svc))

		authed.GET("/ws", wsHandler(svc))

		// SSE event stream.
		authed.GET("/events", sseHandler(svc))

		// Scheduler.
		authed.GET("/scheduler/tasks", schedulerListTasksHandler(svc))
		authed.POST("/scheduler/tasks/:id/run", middleware.AdminRequired(), schedulerRunTaskHandler(svc))
		authed.GET("/scheduler/status", schedulerGetStatusHandler(svc))

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
		authed.GET("/discover/sections", requirePermission(svc, "can_view_discover"), discoverSectionsHandler(svc))
		authed.GET("/discover/feed", requirePermission(svc, "can_view_discover"), discoverFeedHandler(svc))

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

		// Multi-persona play profiles (caller-scoped).
		authed.GET("/play-profiles", listPlayProfilesHandler(svc))
		authed.POST("/play-profiles", createPlayProfileHandler(svc))
		authed.PUT("/play-profiles/:id", updatePlayProfileHandler(svc))
		authed.POST("/play-profiles/:id/verify-pin", verifyPlayProfilePINHandler(svc))
		authed.DELETE("/play-profiles/:id", deletePlayProfileHandler(svc))

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
		authed.GET("/sites/:id/resource", requirePermission(svc, "can_manage_sites"), siteResourceHandler(svc))
		authed.GET("/sites/:id/userdata", requirePermission(svc, "can_manage_sites"), siteUserdataHandler(svc))

		// ── Subscription extras ──
		authed.PUT("/subscriptions/:id", requirePermission(svc, "can_manage_subscriptions"), updateSubscriptionHandler(svc))
		authed.POST("/subscriptions/:id/search", requirePermission(svc, "can_manage_subscriptions"), searchSubscriptionHandler(svc))

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
		authed.POST("/media/:id/ai-scrape", requirePermission(svc, "can_rescrape"), aiScrapeMediaHandler(svc))
		authed.POST("/media/scrape/test", requirePermission(svc, "can_rescrape"), scrapeTestHandler(svc))
		authed.POST("/media/organize", requirePermission(svc, "can_manage_files"), organizeBulkHandler(svc))

		// ── Playback metadata + external player handoff ──
		authed.GET("/playback/:id/info", playbackInfoHandler(svc))
		authed.POST("/playback/:id/progress", playbackProgressHandler(svc))
		authed.GET("/playback/:id/external-players", externalPlayersHandler(svc))
		authed.GET("/playback/:id/external-url", externalURLHandler(svc))
		authed.GET("/playback/transcode/:job_id/status", transcodeStatusHandler(svc))

		// ── Download task ops + sync triggers ──
		authed.POST("/download/:id/pause", requirePermission(svc, "can_manage_downloads"), downloadPauseHandler(svc))
		authed.POST("/download/:id/resume", requirePermission(svc, "can_manage_downloads"), downloadResumeHandler(svc))
		authed.POST("/download/:id/organize", requirePermission(svc, "can_manage_files"), downloadOrganizeOneHandler(svc))
		authed.POST("/download/organize", requirePermission(svc, "can_manage_files"), downloadOrganizeAllHandler(svc))
		authed.POST("/download/sync", requirePermission(svc, "can_manage_downloads"), downloadSyncHandler(svc))
		authed.POST("/download/start-auto-sync", requirePermission(svc, "can_manage_downloads"), downloadAutoSyncHandler(svc))
		authed.GET("/download/tasks", requirePermission(svc, "can_manage_downloads"), downloadTasksAliasHandler(svc))

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

}

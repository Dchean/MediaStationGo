// Package handler — admin-only routes.
package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func registerAdminRoutes(api *gin.RouterGroup, cfg *config.Config, svc *service.Container) {
	// Admin-only endpoints.
	admin := api.Group("/admin")
	admin.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret), middleware.AdminRequired())
	{
		admin.GET("/users", listUsersHandler(svc))
		admin.POST("/users", createUserHandler(svc))
		admin.PATCH("/users/:id", updateUserHandler(svc))
		admin.PATCH("/users/:id/password", resetUserPasswordHandler(svc))
		admin.PATCH("/users/:id/status", updateUserStatusHandler(svc))
		admin.PATCH("/users/:id/role", adminUpdateRoleHandler(svc))
		admin.DELETE("/users/:id", deleteUserHandler(svc))
		admin.GET("/settings", listSettingsHandler(svc))
		admin.PUT("/settings", updateSettingHandler(svc))
		admin.GET("/logs", recentLogsHandler(svc))

		// Permissions admin.
		admin.GET("/users/:id/permissions", getUserPermissionsHandler(svc))
		admin.PUT("/users/:id/permissions", updateUserPermissionsHandler(svc))
		admin.POST("/users/:id/permissions/reset", resetUserPermissionsHandler(svc))

		// Storage configs (Alist / S3 / WebDAV / 网盘).
		admin.GET("/storage/status", listStorageConfigsHandler(svc))
		admin.GET("/storage/:type", getStorageConfigHandler(svc))
		admin.PUT("/storage/:type", saveStorageConfigHandler(svc))
		admin.POST("/storage/:type/test", testStorageConfigHandler(svc))
		admin.POST("/storage/:type/logout", logoutStorageConfigHandler(svc))
		admin.POST("/storage/:type/upload-local", storageUploadLocalHandler(svc))

		// Cloud disk (115 / 夸克) browsing, QR login and 302 import.
		admin.POST("/cloud/scan-all", cloudScanAllHandler(svc))
		admin.POST("/cloud/scan/cancel", cloudScanCancelHandler(svc))
		admin.GET("/cloud/scan/status", cloudScanStatusHandler(svc))
		admin.GET("/cloud/:type/list", cloudListHandler(svc))
		admin.POST("/cloud/:type/import", cloudImportHandler(svc))
		admin.POST("/cloud/:type/mount", cloudMountHandler(svc))
		admin.POST("/cloud/:type/qr/start", cloud115QRStartHandler(svc))
		admin.POST("/cloud/:type/qr/poll", cloud115QRPollHandler(svc))

		// Download client CRUD.
		admin.GET("/download/clients", listDownloadClientsHandler(svc))
		admin.POST("/download/clients", createDownloadClientHandler(svc))
		admin.PUT("/download/clients/:id", updateDownloadClientHandler(svc))
		admin.DELETE("/download/clients/:id", deleteDownloadClientHandler(svc))
		admin.POST("/download/clients/:id/test", testDownloadClientHandler(svc))
		admin.GET("/download/aria2/stats", aria2StatsHandler(svc))

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

		// Telegram Bot webhook management.
		admin.GET("/telegram/webhook", telegramGetWebhookHandler(svc))
		admin.POST("/telegram/webhook", telegramSetWebhookHandler(svc))
		admin.POST("/telegram/polling/start", telegramStartPollingHandler(svc))
		admin.POST("/telegram/polling/stop", telegramStopPollingHandler(svc))

		// File organizer.
		admin.POST("/media/:id/organize", organizeMediaHandler(svc))
		admin.POST("/libraries/:id/organize", organizeLibraryHandler(svc))
		admin.GET("/organize/sources", organizeSourcesHandler(svc))
		admin.POST("/organize/source", organizeDirectoryHandler(svc))

		// 全库修复+重刮:从路径占位符回填缺失外部 ID,然后批量重刮整库。
		admin.POST("/media/repair-rescrape", repairAndRescrapeAllHandler(svc))
		// 单库修复+重刮:只对指定媒体库回填占位符外部 ID 并重刮。
		admin.POST("/libraries/:id/repair-rescrape", repairAndRescrapeLibraryHandler(svc))

		// API key management (encrypted at rest).
		admin.GET("/api-configs", listAPIConfigsHandler(svc))
		admin.GET("/api-configs/:provider", getAPIConfigHandler(svc))
		admin.PUT("/api-configs/:provider", updateAPIConfigHandler(svc))
		admin.DELETE("/api-configs/:provider", deleteAPIConfigHandler(svc))

		// Scheduled jobs.
		admin.GET("/scheduler", schedulerStatusHandler(svc))
		admin.POST("/scheduler/:name/run", schedulerRunHandler(svc))

	}

}

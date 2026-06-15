// Package service 包含 MediaStationGo 的业务逻辑。
// Handler 反序列化 HTTP 请求，调用 Service 方法，然后序列化响应。
// Services 拥有所有横切策略（认证、扫描、转码等）且不直接处理 HTTP 类型。
package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Container 持有在启动时初始化的每个服务。Handler 接收指向它的指针并选择相关字段。
type Container struct {
	Cfg              *config.Config
	Log              *zap.Logger
	Repo             *repository.Container
	WSHub            *Hub
	SSEHub           *SSEHub
	Tasks            *TaskTrackerService
	Auth             *AuthService
	Media            *MediaService
	Scan             *ScannerService
	Stream           *StreamService
	Transcoder       *TranscoderService
	FFprobe          *FFprobeService
	TMDb             *TMDbProvider
	Bangumi          *BangumiProvider
	TheTVDB          *TheTVDBProvider
	Fanart           *FanartProvider
	Scraper          *ScraperService
	Discover         *DiscoverService
	Playback         *PlaybackService
	ImageProxy       *ImageProxy
	Watcher          *WatcherService
	Downloads        *DownloadService
	Subscription     *SubscriptionService
	Subtitle         *SubtitleService
	Stats            *StatsService
	Profile          *ProfileService
	Audit            *AuditService
	NFO              *NFOService
	AI               *AIService
	APIConfig        *APIConfigService
	Crypto           *CryptoService
	Duplicate        *DuplicateService
	FileManager      *FileManagerService
	DLNA             *DLNAService
	Scheduler        *SchedulerService
	Storage          *StorageService
	Emby             *EmbyService
	Backup           *BackupService
	Notifier         *NotifierService
	NotifyChannels   *NotifyChannelService
	TelegramBot      *TelegramBotService
	PlayProfiles     *PlayProfileService
	Permissions      *PermissionService
	StorageCfg       *StorageConfigService
	STRM             *STRMService
	DownloadClients  *DownloadClientService
	Assistant        *AssistantService
	Organizer        *OrganizerService
	OrganizePipeline *OrganizePipelineService
	Douban           *DoubanProvider
	Token            *TokenService
	ApiConfig        *ApiConfigService
	DownloadMgr      *DownloadManager
	Notify           *NotifyService
	Site             *SiteService
	Device           *DeviceService
	Cache            *RuntimeCacheService

	stopCtx    context.Context
	stopCancel context.CancelFunc
}

// New 构建服务容器。
func New(cfg *config.Config, log *zap.Logger, repos *repository.Container) *Container {
	ApplyRuntimeSettings(context.Background(), cfg, repos, log)

	hub := NewHub(log)
	go hub.Run()
	tasks := NewTaskTrackerService(log, hub)

	// 初始化 SSE Hub
	sseHub := NewSSEHub(log)
	go sseHub.Run()

	probe := NewFFprobeService(cfg, log)
	runtimeCache := NewRuntimeCacheService(cfg, log)
	if searchBackend := repository.NewOpenSearchMediaBackend(cfg.Search); searchBackend != nil && repos != nil && repos.Media != nil {
		repos.Media.SetSearchBackend(searchBackend)
		if log != nil {
			log.Info("opensearch media search enabled", zap.String("index", cfg.Search.Index), zap.String("url", cfg.Search.OpenSearchURL))
		}
	}
	crypto := NewCryptoService(cfg.Secrets.JWTSecret, log)
	apiConfig := NewAPIConfigService(log, repos, crypto)
	tmdb := NewTMDbProvider(cfg, log, apiConfig)
	bangumi := NewBangumiProvider(cfg, log)
	thetvdb := NewTheTVDBProvider(cfg, log)
	douban := NewDoubanProvider(cfg, log)
	fanart := NewFanartProvider(cfg, log)
	adult := NewAdultProvider(log, apiConfig)
	scraper := NewScraperService(cfg, log, repos, tmdb, bangumi, thetvdb, fanart, hub, adult)
	scraper.SetDouban(douban)
	organizer := NewOrganizerService(cfg, log, repos)
	organizer.SetProbe(probe)
	organizer.SetScraper(scraper)
	discover := NewDiscoverService(log, tmdb)
	transcoder := NewTranscoderService(cfg, log, repos, hub)
	scanner := NewScannerService(cfg, log, repos, hub, probe, scraper)
	scanner.SetRuntimeCache(runtimeCache)
	organizePipeline := NewOrganizePipelineService(log, repos, organizer, scanner, tasks)
	watcher := NewWatcherService(log, repos, scanner)
	nfo := NewNFOService(log, repos)
	ai := NewAIService(cfg, log, apiConfig)
	duplicate := NewDuplicateService(log, repos, hub)
	filemanager := NewFileManagerService(cfg, log, repos)
	dlna := NewDLNAService(log)
	storage := NewStorageService(log, repos)
	emby := NewEmbyService(cfg, log, repos)
	backup := NewBackupService(cfg, log, repos.DB)
	notifier := NewNotifierService(log, repos)
	notifyChannels := NewNotifyChannelService(log, repos)
	playProfiles := NewPlayProfileService(log, repos)
	permissions := NewPermissionService(log, repos)
	storageCfg := NewStorageConfigService(log, repos, crypto)
	strmSvc := NewSTRMService(log, repos, cfg)
	scanner.SetStorageConfig(storageCfg)
	emby.SetRuntimeCache(runtimeCache)
	emby.SetCloudProbe(storageCfg, probe)
	downloadClients := NewDownloadClientService(log, repos)
	assistant := NewAssistantService(log, repos, ai)
	scheduler := NewSchedulerService(log, repos, scanner, transcoder, organizer, storageCfg, hub, cfg.Cache.CacheDir)
	scheduler.SetTaskTracker(tasks)
	scheduler.SetOrganizePipeline(organizePipeline)

	// 初始化认证相关服务
	tokenSvc := NewTokenService(cfg, log, repos)
	authSvc := NewAuthService(cfg, log, repos, tokenSvc, permissions)
	deviceSvc := NewDeviceService(log, repos)
	telegramBot := NewTelegramBotService(log, repos, crypto, authSvc)
	telegramBot.SetDeviceService(deviceSvc)
	telegramBot.SetBackupService(backup)
	// Allow the device-enforcement service to DM users (warnings / deletions)
	// through their Telegram binding before any destructive action.
	deviceSvc.SetNotifier(telegramBot.NotifyUserByID)
	apiConfigSvc := NewApiConfigService(cfg, log, repos, crypto)
	downloadMgr := NewDownloadManager(log, repos, crypto)
	notifySvc := NewNotifyService(log, repos, crypto)

	// 构建 FlareSolverr URL（如果启用）
	flareSolverrURL := ""
	if cfg.FlareSolverr.Enabled && cfg.FlareSolverr.URL != "" {
		flareSolverrURL = cfg.FlareSolverr.URL
	}
	siteSvc := NewSiteService(log, repos, flareSolverrURL)
	downloads := NewDownloadService(log, repos, hub, organizer, siteSvc)
	downloads.SetScanner(scanner)
	downloads.SetTaskTracker(tasks)
	downloads.SetOrganizePipeline(organizePipeline)
	subscription := NewSubscriptionService(cfg, log, repos, downloads, siteSvc, hub)
	subscription.SetScraper(scraper)

	// 让图片代理把媒体库根目录视为可读的本地图片位置：海报/封面等
	// sidecar 资源就存放在这些（用户自定义、任意）目录下，否则会被
	// 路径白名单挡掉、退化成占位图导致前端图片不显示。
	imageProxy := NewImageProxy(cfg, log)
	imageProxy.SetLibraryRootsProvider(func() []string {
		libs, err := repos.Library.List(context.Background())
		if err != nil {
			return nil
		}
		roots := make([]string, 0, len(libs))
		for _, l := range libs {
			if strings.TrimSpace(l.Path) != "" {
				roots = append(roots, l.Path)
			}
		}
		return roots
	})
	scanner.SetImageProxy(imageProxy)

	ctx, cancel := context.WithCancel(context.Background())

	return &Container{
		Cfg:              cfg,
		Log:              log,
		Repo:             repos,
		WSHub:            hub,
		SSEHub:           sseHub,
		Tasks:            tasks,
		Auth:             authSvc,
		Media:            NewMediaService(cfg, log, repos).SetRuntimeCache(runtimeCache),
		Scan:             scanner,
		Stream:           NewStreamService(cfg, log, repos, transcoder),
		Transcoder:       transcoder,
		FFprobe:          probe,
		TMDb:             tmdb,
		Bangumi:          bangumi,
		TheTVDB:          thetvdb,
		Fanart:           fanart,
		Scraper:          scraper,
		Discover:         discover,
		Playback:         NewPlaybackService(log, repos),
		ImageProxy:       imageProxy,
		Watcher:          watcher,
		Downloads:        downloads,
		Subscription:     subscription,
		Subtitle:         NewSubtitleService(log, repos),
		Stats:            NewStatsService(log, repos).SetRuntimeCache(runtimeCache),
		Profile:          NewProfileService(log, repos),
		Audit:            NewAuditService(log, repos),
		NFO:              nfo,
		AI:               ai,
		APIConfig:        apiConfig,
		Crypto:           crypto,
		Duplicate:        duplicate,
		FileManager:      filemanager,
		DLNA:             dlna,
		Scheduler:        scheduler,
		Storage:          storage,
		Emby:             emby,
		Backup:           backup,
		Notifier:         notifier,
		NotifyChannels:   notifyChannels,
		TelegramBot:      telegramBot,
		PlayProfiles:     playProfiles,
		Permissions:      permissions,
		StorageCfg:       storageCfg,
		STRM:             strmSvc,
		DownloadClients:  downloadClients,
		Assistant:        assistant,
		Organizer:        organizer,
		OrganizePipeline: organizePipeline,
		Douban:           douban,
		Token:            tokenSvc,
		ApiConfig:        apiConfigSvc,
		DownloadMgr:      downloadMgr,
		Notify:           notifySvc,
		Site:             siteSvc,
		Device:           deviceSvc,
		Cache:            runtimeCache,
		stopCtx:          ctx,
		stopCancel:       cancel,
	}
}

// Boot 启动后台工作进程（watcher, downloads poller, subscription scheduler）。
// 在 AutoMigrate 后调用一次。
func (c *Container) Boot() {
	if err := c.Watcher.Start(c.stopCtx); err != nil {
		c.Log.Warn("watcher start failed", zap.Error(err))
	}
	c.Downloads.Start(c.stopCtx)
	c.Subscription.Start(c.stopCtx)
	if err := c.APIConfig.SeedDefaults(c.stopCtx); err != nil {
		c.Log.Warn("api config seed failed", zap.Error(err))
	}
	if err := c.NormalizeCloudLibraryTypes(c.stopCtx); err != nil {
		c.Log.Warn("normalize cloud library types failed", zap.Error(err))
	}
	go c.warmMediaSearchIndex(c.stopCtx)

	// 加载所有已配置的下载客户端
	if err := c.DownloadMgr.LoadAll(c.stopCtx); err != nil {
		c.Log.Warn("failed to load download clients", zap.Error(err))
	}

	// 启动调度器定时任务
	c.Scheduler.Start(c.stopCtx)

	// 云盘存储健康检查
	c.BootCloudStorageHealthCheck(c.stopCtx)

	// 自动扫描云盘媒体库，使内容对所有用户立即可见
	c.BootCloudLibraries(c.stopCtx)

	// Mgo 保号规则巡检：默认关闭，由管理员通过 Telegram Bot 命令开启。
	// 每天触发一次评估；规则里的窗口可随机，不固定。
	if c.Device != nil {
		go c.runInactivitySweeper(c.stopCtx)
	}
}

func (c *Container) warmMediaSearchIndex(ctx context.Context) {
	if c == nil || c.Repo == nil || c.Repo.Media == nil {
		return
	}
	if !mediaSearchWarmupEnabled(ctx, c.Repo) {
		if c.Log != nil {
			c.Log.Info("media search index warmup disabled")
		}
		return
	}
	// 错峰：FTS 正常由 media 表触发器实时维护，回填只是升级或异常后的
	// 兜底。先让登录、首页等关键路径跑起来，再开始后台补索引。
	select {
	case <-ctx.Done():
		return
	case <-time.After(mediaSearchWarmupDelay(ctx, c.Repo)):
	}
	batchSize := mediaSearchWarmupBatchSize(ctx, c.Repo)
	pause := mediaSearchWarmupPause(ctx, c.Repo)
	total := int64(0)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := c.Repo.Media.BackfillSearchIndex(ctx, batchSize)
		if err != nil {
			c.Log.Debug("media search index warmup stopped", zap.Error(err))
			return
		}
		if n == 0 {
			if total > 0 {
				c.Log.Info("media search index warmed", zap.Int64("indexed", total))
			}
			return
		}
		total += n
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
	}
}

func mediaSearchWarmupEnabled(ctx context.Context, repo *repository.Container) bool {
	if repo == nil || repo.Setting == nil {
		return true
	}
	value, err := repo.Setting.Get(ctx, "search.index_warmup_enabled")
	if err != nil || strings.TrimSpace(value) == "" {
		return true
	}
	return parseBoolSetting(value, true)
}

func mediaSearchWarmupDelay(ctx context.Context, repo *repository.Container) time.Duration {
	seconds := mediaSearchWarmupIntSetting(ctx, repo, "search.index_warmup_delay_seconds", 120)
	if seconds < 30 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

func mediaSearchWarmupBatchSize(ctx context.Context, repo *repository.Container) int {
	size := mediaSearchWarmupIntSetting(ctx, repo, "search.index_warmup_batch_size", 100)
	if size < 10 {
		size = 10
	}
	if size > 1000 {
		size = 1000
	}
	return size
}

func mediaSearchWarmupPause(ctx context.Context, repo *repository.Container) time.Duration {
	ms := mediaSearchWarmupIntSetting(ctx, repo, "search.index_warmup_pause_ms", 2000)
	if ms < 250 {
		ms = 250
	}
	return time.Duration(ms) * time.Millisecond
}

func mediaSearchWarmupIntSetting(ctx context.Context, repo *repository.Container, key string, fallback int) int {
	if repo == nil || repo.Setting == nil {
		return fallback
	}
	value, err := repo.Setting.Get(ctx, key)
	if err != nil {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func (c *Container) NormalizeCloudLibraryTypes(ctx context.Context) error {
	if c == nil || c.Repo == nil || c.Repo.Library == nil || c.Repo.DB == nil {
		return nil
	}
	libs, err := c.Repo.Library.List(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		info, ok := ParseCloudLibraryMount(lib.Path)
		if !ok {
			continue
		}
		want := InferCloudMountMediaType(info.DisplayDir, lib.Name)
		if want == "" || want == lib.Type {
			continue
		}
		if err := c.Repo.DB.WithContext(ctx).
			Model(&model.Library{}).
			Where("id = ?", lib.ID).
			Update("type", want).Error; err != nil {
			return err
		}
	}
	return nil
}

// runInactivitySweeper periodically runs the account-cleanup policy. Kept with
// the historical name to avoid churn in callers.
func (c *Container) runInactivitySweeper(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := c.Device.SweepAccountCleanup(ctx); err != nil {
				c.Log.Warn("account cleanup sweep failed", zap.Error(err))
			} else if n > 0 {
				c.Log.Info("account cleanup sweep removed accounts", zap.Int("count", n))
			}
		}
	}
}

// Close 释放 services 持有的任何资源（websocket hub, ffmpeg 转码, fsnotify, 后台轮询器）。
func (c *Container) Close() {
	if c.stopCancel != nil {
		c.stopCancel()
	}
	if c.Scheduler != nil {
		c.Scheduler.Stop()
	}
	if c.Watcher != nil {
		c.Watcher.Stop()
	}
	if c.Subscription != nil {
		c.Subscription.Stop()
	}
	if c.Downloads != nil {
		c.Downloads.Stop()
	}
	if c.Transcoder != nil {
		c.Transcoder.StopAll()
	}
	if c.Cache != nil {
		_ = c.Cache.Close()
	}
	if c.WSHub != nil {
		c.WSHub.Stop()
	}
	if c.SSEHub != nil {
		c.SSEHub.Stop()
	}
	if c.Scheduler != nil {
		c.Scheduler.Stop()
	}
}

// unused guard
var _ = time.Now

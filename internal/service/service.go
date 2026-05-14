// Package service contains the business logic of MediaStationGo. Handlers
// deserialize the HTTP request, call into a Service method, then serialize
// the response. Services own all cross-cutting policy (auth, scanning,
// transcoding, etc.) and never deal with HTTP types directly.
package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Container holds every service initialized at startup. Handlers receive a
// pointer to it and pick the relevant fields.
type Container struct {
	Cfg          *config.Config
	Log          *zap.Logger
	Repo         *repository.Container
	WSHub        *Hub
	Auth         *AuthService
	Media        *MediaService
	Scan         *ScannerService
	Stream       *StreamService
	Transcoder   *TranscoderService
	FFprobe      *FFprobeService
	TMDb         *TMDbProvider
	Bangumi      *BangumiProvider
	Scraper      *ScraperService
	Playback     *PlaybackService
	ImageProxy   *ImageProxy
	Watcher      *WatcherService
	Downloads    *DownloadService
	Subscription *SubscriptionService
	Subtitle     *SubtitleService
	Stats        *StatsService
	Profile      *ProfileService
	Audit        *AuditService

	stopCtx    context.Context
	stopCancel context.CancelFunc
}

// New builds the service container.
func New(cfg *config.Config, log *zap.Logger, repos *repository.Container) *Container {
	hub := NewHub(log)
	go hub.Run()

	probe := NewFFprobeService(cfg, log)
	tmdb := NewTMDbProvider(cfg, log)
	bangumi := NewBangumiProvider(cfg, log)
	scraper := NewScraperService(cfg, log, repos, tmdb, bangumi, hub)
	transcoder := NewTranscoderService(cfg, log, repos, hub)
	scanner := NewScannerService(cfg, log, repos, hub, probe, scraper)
	downloads := NewDownloadService(log, repos, hub)
	subscription := NewSubscriptionService(log, repos, downloads, hub)
	watcher := NewWatcherService(log, repos, scanner)

	ctx, cancel := context.WithCancel(context.Background())

	return &Container{
		Cfg:          cfg,
		Log:          log,
		Repo:         repos,
		WSHub:        hub,
		Auth:         NewAuthService(cfg, log, repos),
		Media:        NewMediaService(cfg, log, repos),
		Scan:         scanner,
		Stream:       NewStreamService(cfg, log, repos, transcoder),
		Transcoder:   transcoder,
		FFprobe:      probe,
		TMDb:         tmdb,
		Bangumi:      bangumi,
		Scraper:      scraper,
		Playback:     NewPlaybackService(log, repos),
		ImageProxy:   NewImageProxy(cfg, log),
		Watcher:      watcher,
		Downloads:    downloads,
		Subscription: subscription,
		Subtitle:     NewSubtitleService(log, repos),
		Stats:        NewStatsService(log, repos),
		Profile:      NewProfileService(log, repos),
		Audit:        NewAuditService(log, repos),
		stopCtx:      ctx,
		stopCancel:   cancel,
	}
}

// Boot kicks off background workers (watcher, downloads poller,
// subscription scheduler).  Called once after AutoMigrate.
func (c *Container) Boot() {
	if err := c.Watcher.Start(c.stopCtx); err != nil {
		c.Log.Warn("watcher start failed", zap.Error(err))
	}
	c.Downloads.Start(c.stopCtx)
	c.Subscription.Start(c.stopCtx)
}

// Close releases any resources held by services (websocket hub, ffmpeg
// transcodes, fsnotify, background pollers).
func (c *Container) Close() {
	if c.stopCancel != nil {
		c.stopCancel()
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
	if c.WSHub != nil {
		c.WSHub.Stop()
	}
}

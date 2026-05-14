// Package service contains the business logic of MediaStationGo. Handlers
// deserialize the HTTP request, call into a Service method, then serialize
// the response. Services own all cross-cutting policy (auth, scanning,
// transcoding, etc.) and never deal with HTTP types directly.
package service

import (
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Container holds every service initialized at startup. Handlers receive a
// pointer to it and pick the relevant fields.
type Container struct {
	Cfg        *config.Config
	Log        *zap.Logger
	Repo       *repository.Container
	WSHub      *Hub
	Auth       *AuthService
	Media      *MediaService
	Scan       *ScannerService
	Stream     *StreamService
	Transcoder *TranscoderService
	FFprobe    *FFprobeService
	TMDb       *TMDbProvider
	Scraper    *ScraperService
	Playback   *PlaybackService
	ImageProxy *ImageProxy
}

// New builds the service container.
func New(cfg *config.Config, log *zap.Logger, repos *repository.Container) *Container {
	hub := NewHub(log)
	go hub.Run()

	probe := NewFFprobeService(cfg, log)
	tmdb := NewTMDbProvider(cfg, log)
	scraper := NewScraperService(cfg, log, repos, tmdb, hub)
	transcoder := NewTranscoderService(cfg, log, repos, hub)

	return &Container{
		Cfg:        cfg,
		Log:        log,
		Repo:       repos,
		WSHub:      hub,
		Auth:       NewAuthService(cfg, log, repos),
		Media:      NewMediaService(cfg, log, repos),
		Scan:       NewScannerService(cfg, log, repos, hub, probe, scraper),
		Stream:     NewStreamService(cfg, log, repos, transcoder),
		Transcoder: transcoder,
		FFprobe:    probe,
		TMDb:       tmdb,
		Scraper:    scraper,
		Playback:   NewPlaybackService(log, repos),
		ImageProxy: NewImageProxy(cfg, log),
	}
}

// Close releases any resources held by services (websocket hub, ffmpeg
// transcodes).
func (c *Container) Close() {
	if c.Transcoder != nil {
		c.Transcoder.StopAll()
	}
	if c.WSHub != nil {
		c.WSHub.Stop()
	}
}

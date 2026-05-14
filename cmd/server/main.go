// Package main is the MediaStationGo HTTP server entry point.
//
// MediaStationGo is a Go rewrite of the original Python MediaStation project,
// adopting the same tech stack as cropflre/nowen-video:
//
//	Backend:  Go 1.25 + Gin + GORM + SQLite (WAL) + Viper + Zap + JWT
//	Frontend: React 18 + Vite + Tailwind + Zustand + HLS.js
//
// The binary embeds the SPA build artifacts at /app/web/dist and serves them
// alongside the JSON REST API at /api/* and the WebSocket hub at /api/ws.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/handler"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// version is overwritten at build time via -ldflags="-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	logger, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("starting MediaStationGo",
		zap.String("version", version),
		zap.Int("port", cfg.App.Port),
		zap.String("data_dir", cfg.App.DataDir),
	)

	// Ensure data / cache / web dirs exist.
	for _, d := range []string{cfg.App.DataDir, cfg.Cache.CacheDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			logger.Fatal("create dir failed", zap.String("dir", d), zap.Error(err))
		}
	}

	db, err := database.Open(cfg, logger)
	if err != nil {
		logger.Fatal("database open failed", zap.Error(err))
	}
	if err := database.AutoMigrate(db); err != nil {
		logger.Fatal("auto-migrate failed", zap.Error(err))
	}

	repos := repository.New(db)
	services := service.New(cfg, logger, repos)

	if err := services.Auth.SeedAdmin(context.Background()); err != nil {
		logger.Warn("seed admin failed", zap.Error(err))
	}
	services.Boot()

	router := buildRouter(cfg, logger, services)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.App.Port),
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		logger.Info("listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("listen failed", zap.Error(err))
		}
	}()

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Info("shutdown requested")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
	services.Close()
	logger.Info("MediaStationGo stopped")
}

func buildRouter(cfg *config.Config, logger *zap.Logger, svc *service.Container) *gin.Engine {
	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger(logger))
	r.Use(middleware.CORS(cfg.App.CORSOrigins))

	handler.Register(r, cfg, logger, svc)

	// Static SPA fallback.
	if cfg.App.WebDir != "" {
		serveSPA(r, cfg.App.WebDir)
	}
	return r
}

// serveSPA serves the React build artifacts and falls back to index.html for
// non-API, non-asset paths so client-side routing keeps working.
func serveSPA(r *gin.Engine, webDir string) {
	r.Static("/assets", filepath.Join(webDir, "assets"))
	r.StaticFile("/favicon.ico", filepath.Join(webDir, "favicon.ico"))
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// Do not swallow API or WebSocket routes; let them 404 naturally.
		if len(path) >= 5 && path[:5] == "/api/" {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(filepath.Join(webDir, "index.html"))
	})
}

func newLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.App.Debug {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

// Package main is the MediaStationGo HTTP server entry point.
//
// MediaStationGo is a Go rewrite of the legacy Python implementation,
// adopting the same tech stack as cropflre/nowen-video:
//
//	Backend:  Go 1.25 + Gin + GORM + PostgreSQL/SQLite + Viper + Zap + JWT
//	Frontend: React 18 + Vite + Tailwind + Zustand + HLS.js
//
// The binary embeds the SPA build artifacts at /app/web/dist and serves them
// alongside the JSON REST API at /api/* and the WebSocket hub at /api/ws.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

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
		if err := os.MkdirAll(d, 0o750); err != nil {
			logger.Fatal("create dir failed", zap.String("dir", d), zap.Error(err))
		}
	}

	db, err := database.Open(cfg, logger)
	if err != nil {
		logger.Fatal("database open failed", zap.Error(err))
	}
	if err := waitForDatabase(db, logger); err != nil {
		logger.Fatal("database not ready", zap.Error(err))
	}
	if err := database.AutoMigrate(db); err != nil {
		logger.Fatal("auto-migrate failed", zap.Error(err))
	}
	if err := database.MigrateSQLiteToCurrentIfNeeded(cfg, db, logger); err != nil {
		logger.Fatal("sqlite to postgres migration failed", zap.Error(err))
	}

	repos := repository.New(db)
	service.ApplyRuntimeSettings(context.Background(), cfg, repos, logger)
	applyCPUThreadLimit(cfg, logger)
	services := service.New(cfg, logger, repos)

	if err := services.Auth.SeedAdmin(context.Background()); err != nil {
		logger.Warn("seed admin failed", zap.Error(err))
	}

	router := buildRouter(cfg, logger, services)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.App.Port),
		Handler:           router,
		ReadHeaderTimeout: 15 * time.Second,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		logger.Fatal("listen failed", zap.String("addr", srv.Addr), zap.Error(err))
	}
	localIP := getLocalIP()
	logger.Info("server is ready",
		zap.String("local", fmt.Sprintf("http://%s:%d", localIP, cfg.App.Port)),
		zap.String("listen", srv.Addr),
	)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("listen failed", zap.Error(err))
		}
	}()
	go func() {
		if publicIP := getPublicIP(3 * time.Second); publicIP != "" {
			logger.Info("server public endpoint",
				zap.String("public", fmt.Sprintf("http://%s:%d", publicIP, cfg.App.Port)),
			)
		}
	}()
	go services.Boot()
	go services.TelegramBot.StartPolling(context.Background())

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

func applyCPUThreadLimit(cfg *config.Config, logger *zap.Logger) {
	if cfg == nil || cfg.App.MaxCPUThreads < 1 {
		return
	}
	prev := runtime.GOMAXPROCS(cfg.App.MaxCPUThreads)
	if logger != nil {
		logger.Info("runtime CPU thread limit applied",
			zap.Int("max_cpu_threads", cfg.App.MaxCPUThreads),
			zap.Int("previous", prev))
	}
}

func waitForDatabase(db interface{ DB() (*sql.DB, error) }, logger *zap.Logger) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = sqlDB.PingContext(ctx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if logger != nil {
			logger.Warn("database not ready; retrying", zap.Int("attempt", attempt), zap.Error(err))
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return lastErr
}

func buildRouter(cfg *config.Config, logger *zap.Logger, svc *service.Container) *gin.Engine {
	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger(logger))
	if !cfg.App.Debug && len(cfg.App.CORSOrigins) == 0 {
		logger.Warn("CORS: no origins configured in production — CORS headers will be omitted (same-origin enforced). Set app.cors_origins for cross-origin access.")
	}
	r.Use(middleware.CORS(cfg.App.CORSOrigins, cfg.App.Debug))

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
	assets := r.Group("/assets")
	assets.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Next()
	})
	assets.Static("/", filepath.Join(webDir, "assets"))
	for _, icon := range []string{"/favicon.ico", "/favicon.svg"} {
		iconPath := filepath.Join(webDir, strings.TrimPrefix(icon, "/"))
		r.GET(icon, serveNoCacheFile(iconPath))
		r.HEAD(icon, serveNoCacheFile(iconPath))
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// Do not swallow API / Emby compatibility routes; clients expect JSON
		// or 404, not the React index.html fallback.
		if shouldBypassSPAFallback(path) {
			c.Status(http.StatusNotFound)
			return
		}
		serveSPAIndex(c, filepath.Join(webDir, "index.html"))
	})
}

func serveNoCacheFile(filePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		setNoCacheHeaders(c)
		if _, err := os.Stat(filePath); err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(filePath)
	}
}

func serveSPAIndex(c *gin.Context, indexPath string) {
	setNoCacheHeaders(c)
	if _, err := os.Stat(indexPath); err != nil {
		c.String(http.StatusNotFound, "MediaStationGo web UI not found: %s", indexPath)
		return
	}
	c.File(indexPath)
}

func setNoCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

func shouldBypassSPAFallback(path string) bool {
	if isFrontendLibraryRoute(path) {
		return false
	}
	lower := strings.ToLower(path)
	for _, exact := range []string{
		"/emby",
	} {
		if lower == exact {
			return true
		}
	}
	for _, prefix := range []string{
		"/api/",
		"/emby/",
		"/system/",
		"/users/",
		"/items/",
		"/shows/",
		"/library/",
		"/videos/",
		"/sessions/",
		"/displaypreferences/",
		"/branding/",
		"/localization/",
		"/startup/",
		"/quickconnect/",
		"/socket",
		"/embywebsocket",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func isFrontendLibraryRoute(path string) bool {
	const prefix = "/library/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	id := strings.TrimPrefix(path, prefix)
	if strings.Contains(id, "/") {
		return false
	}
	if len(id) != 36 {
		return false
	}
	for i, ch := range id {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
				return false
			}
		}
	}
	return true
}

// newLogger 根据 cfg.Logging 构建 Zap。此前 logging.level / logging.format
// 配置完全没有生效（固定 NewProduction），用户无法在生产环境降低日志量；
// 配合每请求一条 INFO 访问日志，几小时即可产生几十 MB 日志，在 Docker
// json-file 驱动下持续消耗磁盘 IO。
func newLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.App.Debug {
		return zap.NewDevelopment()
	}
	level := productionLogLevel(cfg.Logging.Level)
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	var encoder zapcore.Encoder
	if strings.EqualFold(strings.TrimSpace(cfg.Logging.Format), "console") {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	}
	cores := []zapcore.Core{
		zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), level),
	}
	warnPath, errorPath := logFilePaths(cfg)
	if warnPath != "" {
		warnWriter, err := newRotatingFileWriter(warnPath, cfg.Logging)
		if err != nil {
			return nil, err
		}
		cores = append(cores, zapcore.NewCore(encoder, warnWriter, zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl == zapcore.WarnLevel && level.Enabled(lvl)
		})))
	}
	if errorPath != "" {
		errorWriter, err := newRotatingFileWriter(errorPath, cfg.Logging)
		if err != nil {
			return nil, err
		}
		cores = append(cores, zapcore.NewCore(encoder, errorWriter, zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.ErrorLevel && level.Enabled(lvl)
		})))
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel), zap.ErrorOutput(zapcore.Lock(os.Stderr))), nil
}

func productionLogLevel(raw string) zapcore.Level {
	level := zapcore.WarnLevel
	raw = strings.TrimSpace(raw)
	if raw != "" {
		var parsed zapcore.Level
		if err := parsed.UnmarshalText([]byte(raw)); err == nil {
			level = parsed
		}
	}
	if level < zapcore.WarnLevel {
		return zapcore.WarnLevel
	}
	return level
}

func logFilePaths(cfg *config.Config) (string, string) {
	out := strings.TrimSpace(cfg.Logging.OutputPath)
	if strings.EqualFold(out, "stdout") || strings.EqualFold(out, "stderr") {
		return "", ""
	}
	if out == "" {
		out = filepath.Join(cfg.App.DataDir, "logs")
	}
	if ext := filepath.Ext(out); ext != "" {
		base := strings.TrimSuffix(out, ext)
		return base + ".warn" + ext, base + ".error" + ext
	}
	return filepath.Join(out, "warn.log"), filepath.Join(out, "error.log")
}

// getLocalIP returns the first non-loopback IPv4 address of the machine.
// Falls back to "localhost" if no suitable interface is found.
func getLocalIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "localhost"
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				if ip := v.IP.To4(); ip != nil {
					return ip.String()
				}
			}
		}
	}
	return "localhost"
}

// getPublicIP tries to detect the public-facing IP by querying ipify.org.
// Returns empty string if detection fails (e.g. no internet, timeout).
func getPublicIP(timeout time.Duration) string {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, err := resp.Body.Read(buf)
	if err != nil || n == 0 || resp.StatusCode != http.StatusOK {
		return ""
	}
	return string(buf[:n])
}

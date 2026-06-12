package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestParseEmbyAuthByNameReqAcceptsLowercaseJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader(`{"username":"alice","password":"secret"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	req, err := parseEmbyAuthByNameReq(c)
	if err != nil {
		t.Fatalf("parseEmbyAuthByNameReq returned error: %v", err)
	}
	if req.Username != "alice" || req.Password != "secret" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestParseEmbyAuthByNameReqAcceptsFormBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/Users/AuthenticateByName", strings.NewReader("Username=bob&Pw=secret"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseEmbyAuthByNameReq(c)
	if err != nil {
		t.Fatalf("parseEmbyAuthByNameReq returned error: %v", err)
	}
	if req.Username != "bob" || req.Pw != "secret" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestParseEmbyAuthByNameReqAcceptsJSONWithoutContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/emby/users/authenticatebyname", strings.NewReader(`{"UserName":"carol","PW":"secret"}`))

	req, err := parseEmbyAuthByNameReq(c)
	if err != nil {
		t.Fatalf("parseEmbyAuthByNameReq returned error: %v", err)
	}
	if req.Username != "carol" || req.Pw != "secret" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

func TestEmbyAuthenticateByNameAcceptsCaseVariantUsernameAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserPermission{}, &model.RefreshToken{}, &model.Setting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.JWTSecret = "test-secret"
	log := zap.NewNop()
	permissions := service.NewPermissionService(log, repos)
	auth := service.NewAuthService(cfg, log, repos, service.NewTokenService(cfg, log, repos), permissions)
	if _, _, err := auth.Register(context.Background(), "viewer", "secret-pass"); err != nil {
		t.Fatalf("register: %v", err)
	}

	router := gin.New()
	registerEmbyRoutes(router, cfg.Secrets.JWTSecret, &service.Container{
		Repo:  repos,
		Auth:  auth,
		Emby:  service.NewEmbyService(cfg, log, repos),
		Audit: service.NewAuditService(log, repos),
	})

	req := httptest.NewRequest(http.MethodPost, "/emby/users/authenticatebyname", strings.NewReader(`{"Username":"Viewer","Pw":"secret-pass"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["AccessToken"] == "" {
		t.Fatalf("missing AccessToken: %#v", payload)
	}
}

func TestEmbyWithRequestAddressUsesHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://192.168.1.4:18080/System/Info/Public", nil)

	payload := embyWithRequestAddress(c, map[string]any{"Id": "mediastation-go-001"})

	if payload["LocalAddress"] != "http://192.168.1.4:18080" {
		t.Fatalf("unexpected LocalAddress: %#v", payload["LocalAddress"])
	}
	if payload["WanAddress"] != "http://192.168.1.4:18080" {
		t.Fatalf("unexpected WanAddress: %#v", payload["WanAddress"])
	}
}

func TestEmbyWithRequestAddressHonorsForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1/System/Info/Public", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")
	c.Request.Header.Set("X-Forwarded-Host", "media.example.test")

	payload := embyWithRequestAddress(c, map[string]any{"Id": "mediastation-go-001"})

	if payload["LocalAddress"] != "https://media.example.test" {
		t.Fatalf("unexpected LocalAddress: %#v", payload["LocalAddress"])
	}
}

func TestEmbyPublicSystemInfoLooksLikeModernEmbyServer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	repos := repository.New(db)
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/System/Info/Public", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode system info: %v", err)
	}
	if payload["ProductName"] != "Emby Server" {
		t.Fatalf("ProductName = %#v, want Emby Server", payload["ProductName"])
	}
	version, _ := payload["Version"].(string)
	if !strings.HasPrefix(version, "4.") {
		t.Fatalf("Version = %q, want Emby-compatible 4.x", version)
	}
}

func TestEmbyMobileCompatibilityRoutesAvoidPlaybackBlocking404s(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(&model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "viewer",
		PasswordHash: "hash",
		Role:         "admin",
		IsActive:     true,
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := &config.Config{}
	cfg.Secrets.JWTSecret = "test-secret"
	repos := repository.New(db)
	router := gin.New()
	registerEmbyRoutes(router, cfg.Secrets.JWTSecret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(cfg, zap.NewNop(), repos),
	})

	token := signedTestToken(t, cfg.Secrets.JWTSecret)
	tests := []struct {
		path     string
		auth     bool
		wantCode int
	}{
		{path: "/emby/System/Ext/ServerDomains", wantCode: http.StatusOK},
		{path: "/emby/Items/msgo-series-demo/Similar", auth: true, wantCode: http.StatusOK},
		{path: "/emby/api/danmu/media-demo/raw", auth: true, wantCode: http.StatusOK},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.auth {
			req.Header.Set("X-Emby-Token", token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.wantCode {
			t.Fatalf("%s status = %d body=%s", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestEmbySenPlayerDiscoveryRoutesReturnProtocolResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cfg := &config.Config{}
	cfg.App.Port = 9011
	repos := repository.New(db)
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(cfg, zap.NewNop(), repos),
	})

	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/emby", contentType: "application/json", contains: "Emby Server"},
		{path: "/emby/", contentType: "application/json", contains: "Emby Server"},
		{path: "/Startup/Configuration", contentType: "application/json", contains: "StartupWizardCompleted"},
		{path: "/emby/Startup/Configuration", contentType: "application/json", contains: "StartupWizardCompleted"},
		{path: "/System/Configuration/Public", contentType: "application/json", contains: "IsStartupWizardCompleted"},
		{path: "/emby/System/Configuration/Public", contentType: "application/json", contains: "IsStartupWizardCompleted"},
		{path: "/QuickConnect/Enabled", contentType: "application/json", contains: "false"},
		{path: "/emby/QuickConnect/Enabled", contentType: "application/json", contains: "false"},
		{path: "/Branding/Css", contentType: "text/css", contains: ""},
		{path: "/emby/Branding/Css", contentType: "text/css", contains: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
			if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, tt.contentType) {
				t.Fatalf("Content-Type = %q, want %q", contentType, tt.contentType)
			}
			if tt.contains != "" && !strings.Contains(w.Body.String(), tt.contains) {
				t.Fatalf("body = %q, want contains %q", w.Body.String(), tt.contains)
			}
			if strings.Contains(w.Body.String(), "<html") {
				t.Fatalf("protocol discovery route served SPA HTML: %q", w.Body.String())
			}
		})
	}
}

func TestEmbyUppercaseSessionCapabilitiesRouteNoContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{Repo: repos})

	req := httptest.NewRequest(http.MethodPost, "/Sessions/Capabilities/Full", strings.NewReader(`{}`))
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
}

func TestEmbySessionCapabilitiesRouteAllowsPreAuthProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{})

	for _, path := range []string{"/Sessions/Capabilities", "/Sessions/Capabilities/Full", "/emby/Sessions/Capabilities", "/emby/Sessions/Capabilities/Full"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusNoContent {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestEmbyVirtualFoldersRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Library{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, lib := range []model.Library{
		{Name: "电影", Path: "D:\\media\\movies", Type: "movie", Enabled: true},
		{Name: "剧集", Path: "D:\\media\\tv", Type: "tv", Enabled: true},
		{Name: "综艺", Path: "D:\\media\\variety", Type: "variety", Enabled: true},
	} {
		if err := repos.Library.Create(t.Context(), &lib); err != nil {
			t.Fatalf("create library: %v", err)
		}
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{Repo: repos})

	req := httptest.NewRequest(http.MethodGet, "/Library/VirtualFolders", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q body=%s", contentType, w.Body.String())
	}
	if strings.HasPrefix(strings.TrimSpace(w.Body.String()), "<!doctype html>") {
		t.Fatalf("route returned frontend HTML instead of JSON")
	}

	var folders []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &folders); err != nil {
		t.Fatalf("decode folders: %v", err)
	}
	if len(folders) != 3 {
		t.Fatalf("expected 3 folders, got %d: %#v", len(folders), folders)
	}
	if folders[1]["CollectionType"] != "tvshows" || folders[2]["CollectionType"] != "tvshows" {
		t.Fatalf("episodic libraries should expose tvshows collection type: %#v", folders)
	}
}

func TestEmbyItemsCountsRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{Repo: repos})

	for _, path := range []string{"/Items/Counts", "/Users/user-1/Items/Counts", "/items/counts"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s decode response: %v", path, err)
		}
		if _, ok := body["MovieCount"]; !ok {
			t.Fatalf("%s missing MovieCount: %#v", path, body)
		}
	}
}

func TestEmbyItemImageServesWithoutAPIAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	posterPath := filepath.Join(t.TempDir(), "poster.png")
	if err := os.WriteFile(posterPath, []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}, 0o644); err != nil {
		t.Fatalf("write poster: %v", err)
	}

	repos := repository.New(db)
	cfg := &config.Config{Cache: config.CacheConfig{CacheDir: t.TempDir()}}
	if err := db.Create(&model.Media{
		Base:      model.Base{ID: "media-1"},
		Title:     "Poster Test",
		Path:      "D:\\media\\poster-test.mp4",
		PosterURL: posterPath,
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	router := gin.New()
	registerEmbyRoutes(router, "test-secret", &service.Container{
		Repo:       repos,
		Emby:       service.NewEmbyService(cfg, zap.NewNop(), repos),
		ImageProxy: service.NewImageProxy(cfg, zap.NewNop()),
	})

	req := httptest.NewRequest(http.MethodGet, "/Items/media-1/Images/Primary", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if location := w.Header().Get("Location"); location != "" {
		t.Fatalf("expected direct image response, got redirect to %q", location)
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "image/png") {
		t.Fatalf("expected png content type, got %q", contentType)
	}
}

func TestEmbyUserItemByIDRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Library{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "剧集", Path: "D:\\media\\tv", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		Base:       model.Base{ID: "episode-1"},
		LibraryID:  lib.ID,
		Title:      "Test Show",
		Path:       "D:\\media\\tv\\Test Show\\Season 01\\Test Show - S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
		Container:  "mkv",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/Users/user-1/Items/episode-1", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	req.Header.Set("If-None-Match", `"stale-client-cache"`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if contentType := w.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected JSON content type, got %q body=%s", contentType, w.Body.String())
	}
	var item map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	if item["Id"] != "episode-1" || item["Type"] != "Episode" {
		t.Fatalf("unexpected item payload: %#v", item)
	}
}

func TestEmbyLowercasePlaybackInfoRouteReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "电影", Path: t.TempDir(), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		Base:      model.Base{ID: "media-1"},
		LibraryID: lib.ID,
		Title:     "Lowercase Playback",
		Path:      filepath.Join(lib.Path, "lowercase-playback.mp4"),
		Container: "mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/users/user-1/items/media-1/playbackinfo", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode playback info: %v", err)
	}
	if _, ok := body["MediaSources"]; !ok {
		t.Fatalf("missing MediaSources: %#v", body)
	}
	sources, ok := body["MediaSources"].([]any)
	if !ok || len(sources) == 0 {
		t.Fatalf("unexpected MediaSources: %#v", body["MediaSources"])
	}
	source, ok := sources[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected MediaSource: %#v", sources[0])
	}
	directURL, _ := source["DirectStreamUrl"].(string)
	if !strings.Contains(directURL, "api_key=") {
		t.Fatalf("DirectStreamUrl should carry api_key for clients that do not repeat auth headers: %#v", source)
	}
	transcodeURL, _ := source["TranscodingUrl"].(string)
	if transcodeURL != "" && !strings.Contains(transcodeURL, "api_key=") {
		t.Fatalf("TranscodingUrl should carry api_key: %#v", source)
	}
}

func TestEmbyPlaybackInfoTokenizesCloudPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	lib := model.Library{Name: "OpenList", Path: "cloud://openlist/Movies", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		Base:      model.Base{ID: "cloud-1"},
		LibraryID: lib.ID,
		Title:     "Cloud Movie",
		Path:      "cloud://openlist/Movies/Movie.mkv",
		STRMURL:   "/api/cloud/play/openlist?ref=%2FMovies%2FMovie.mkv",
		Container: "mkv",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo: repos,
		Emby: service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
	})

	req := httptest.NewRequest(http.MethodGet, "/users/user-1/items/cloud-1/playbackinfo", nil)
	req.Header.Set("X-Emby-Token", signedTestToken(t, secret))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode playback info: %v", err)
	}
	source := body["MediaSources"].([]any)[0].(map[string]any)
	pathURL, _ := source["Path"].(string)
	if !strings.HasPrefix(pathURL, "/api/stream/cloud-1") || !strings.Contains(pathURL, "api_key=") {
		t.Fatalf("cloud Path should be tokenized stream URL, got %#v", source)
	}
	if strings.Contains(pathURL, "/api/cloud/play/") {
		t.Fatalf("cloud Path should not expose naked cloud play URL: %#v", source)
	}
	directURL, _ := source["DirectStreamUrl"].(string)
	if !strings.HasPrefix(directURL, "/api/stream/cloud-1") || !strings.Contains(directURL, "api_key=") {
		t.Fatalf("DirectStreamUrl should stay tokenized: %#v", source)
	}
}

func TestEmbyLowercaseVideoStreamRouteServesMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake-video-bytes"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	lib := model.Library{Name: "电影", Path: dir, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		Base:      model.Base{ID: "media-1"},
		LibraryID: lib.ID,
		Title:     "Lowercase Stream",
		Path:      mediaPath,
		Container: "mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo:   repos,
		Emby:   service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
		Stream: service.NewStreamService(&config.Config{}, zap.NewNop(), repos, nil),
	})

	req := httptest.NewRequest(http.MethodGet, "/videos/media-1/stream?api_key="+signedTestToken(t, secret), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "fake-video-bytes" {
		t.Fatalf("unexpected stream body: %q", got)
	}
}

func TestEmbyLowercaseOriginalHeadRouteServesHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake-video-bytes"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	lib := model.Library{Name: "电影", Path: dir, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		Base:      model.Base{ID: "media-1"},
		LibraryID: lib.ID,
		Title:     "Lowercase Original",
		Path:      mediaPath,
		Container: "mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo:   repos,
		Emby:   service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
		Stream: service.NewStreamService(&config.Config{}, zap.NewNop(), repos, nil),
	})

	req := httptest.NewRequest(http.MethodHead, "/videos/media-1/original.mp4?api_key="+signedTestToken(t, secret), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD response should not include body, got %q", w.Body.String())
	}
}

func TestEmbyLowercaseVideoHLSRouteDoesNot404WhenDirectOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	if err := repos.User.Create(t.Context(), &model.User{
		Base:         model.Base{ID: "user-1"},
		Username:     "tester",
		PasswordHash: "x",
		Role:         "admin",
		Tier:         "plus",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "sample.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake-video-bytes"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	lib := model.Library{Name: "电影", Path: dir, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	if err := db.Create(&model.Media{
		Base:      model.Base{ID: "media-1"},
		LibraryID: lib.ID,
		Title:     "Lowercase HLS",
		Path:      mediaPath,
		Container: "mp4",
	}).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	if err := repos.Setting.Set(t.Context(), service.PlaybackDirectOnlySettingKey, "true"); err != nil {
		t.Fatalf("set direct-only: %v", err)
	}

	const secret = "test-secret"
	router := gin.New()
	registerEmbyRoutes(router, secret, &service.Container{
		Repo:   repos,
		Emby:   service.NewEmbyService(&config.Config{}, zap.NewNop(), repos),
		Stream: service.NewStreamService(&config.Config{}, zap.NewNop(), repos, nil),
	})

	req := httptest.NewRequest(http.MethodGet, "/videos/media-1/master.m3u8?api_key="+signedTestToken(t, secret), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatalf("lowercase HLS route should be registered, got 404")
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("direct-only HLS should return 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func signedTestToken(t *testing.T, secret string) string {
	t.Helper()
	claims := middleware.Claims{
		UserID: "user-1",
		Role:   "admin",
		Tier:   "plus",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			Issuer:    "mediastationgo-test",
			Subject:   "user-1",
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return token
}

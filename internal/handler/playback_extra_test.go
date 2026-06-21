package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestExternalURLUsesMediaScopedPlaybackToken(t *testing.T) {
	router, svc, secret := newPlaybackScopeTestRouter(t)
	loginToken := signedTestToken(t, secret)

	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/playback/media-1/external-url", nil)
	req.Header.Set("Authorization", "Bearer "+loginToken)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Token != "" {
		t.Fatalf("external-url should not expose a separate token field")
	}
	if strings.Contains(payload.URL, loginToken) {
		t.Fatalf("external url leaked the caller's login token")
	}
	streamURL, err := url.Parse(payload.URL)
	if err != nil {
		t.Fatalf("parse stream url: %v", err)
	}
	playToken := streamURL.Query().Get("token")
	if playToken == "" {
		t.Fatalf("external url missing playback token: %q", payload.URL)
	}
	claims := &service.Claims{}
	parsed, err := jwt.ParseWithClaims(playToken, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("playback token did not parse: %v", err)
	}
	if claims.Purpose != service.ExternalPlaybackTokenPurpose || claims.MediaID != "media-1" || claims.UserID != "user-1" {
		t.Fatalf("unexpected playback claims: %+v", claims)
	}
	ttl := time.Until(claims.ExpiresAt.Time)
	wantTTL := service.ExternalPlaybackTokenDurationForMedia(2 * 60 * 60)
	if ttl < wantTTL-time.Minute || ttl > wantTTL+time.Second {
		t.Fatalf("playback token ttl = %v, want about %v", ttl, wantTTL)
	}

	user, err := svc.Repo.User.FindByID(req.Context(), "user-1")
	if err != nil || user == nil {
		t.Fatalf("find user: %v", err)
	}
	accountToken, err := svc.Auth.IssueToken(user)
	if err != nil {
		t.Fatalf("issue account token: %v", err)
	}
	if playToken == accountToken {
		t.Fatalf("playback token should not be the reusable account token")
	}
}

func TestScopedPlaybackTokenCannotStreamAnotherMedia(t *testing.T) {
	router, svc, _ := newPlaybackScopeTestRouter(t)
	user, err := svc.Repo.User.FindByID(t.Context(), "user-1")
	if err != nil || user == nil {
		t.Fatalf("find user: %v", err)
	}
	playToken, err := svc.Auth.IssueExternalPlaybackToken(user, "media-1", 2*60*60)
	if err != nil {
		t.Fatalf("issue playback token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/stream/media-2?token="+url.QueryEscape(playToken), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestScopedPlaybackTokenCannotRetargetCloudRef(t *testing.T) {
	router, svc, _ := newPlaybackScopeTestRouter(t)
	user, err := svc.Repo.User.FindByID(t.Context(), "user-1")
	if err != nil || user == nil {
		t.Fatalf("find user: %v", err)
	}
	playToken, err := svc.Auth.IssueExternalPlaybackToken(user, "media-1", 2*60*60)
	if err != nil {
		t.Fatalf("issue playback token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/cloud/play/openlist?ref=other&media_id=media-1&token="+url.QueryEscape(playToken), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestScopedPlaybackTokenCannotCallRegularAPI(t *testing.T) {
	router, svc, _ := newPlaybackScopeTestRouter(t)
	api := router.Group("/api")
	api.Use(middleware.AuthRequired(svc.Cfg.Secrets.JWTSecret))
	api.GET("/me", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	user, err := svc.Repo.User.FindByID(t.Context(), "user-1")
	if err != nil || user == nil {
		t.Fatalf("find user: %v", err)
	}
	playToken, err := svc.Auth.IssueExternalPlaybackToken(user, "media-1", 2*60*60)
	if err != nil {
		t.Fatalf("issue playback token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "http://nas.local/api/me?token="+url.QueryEscape(playToken), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestExternalPlaybackTokenUsesUnknownDurationFallback(t *testing.T) {
	_, svc, secret := newPlaybackScopeTestRouter(t)
	user, err := svc.Repo.User.FindByID(t.Context(), "user-1")
	if err != nil || user == nil {
		t.Fatalf("find user: %v", err)
	}
	playToken, err := svc.Auth.IssueExternalPlaybackToken(user, "media-1", 0)
	if err != nil {
		t.Fatalf("issue playback token: %v", err)
	}
	claims := &service.Claims{}
	parsed, err := jwt.ParseWithClaims(playToken, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("parse playback token: %v", err)
	}
	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl < service.ExternalPlaybackTokenUnknownDuration-time.Minute || ttl > service.ExternalPlaybackTokenUnknownDuration+time.Second {
		t.Fatalf("unknown duration ttl = %v, want about %v", ttl, service.ExternalPlaybackTokenUnknownDuration)
	}
}

func TestExternalPlaybackTokenDurationIsCapped(t *testing.T) {
	if got := service.ExternalPlaybackTokenDurationForMedia(int((72 * time.Hour).Seconds())); got != service.ExternalPlaybackTokenMaxDuration {
		t.Fatalf("duration cap = %v, want %v", got, service.ExternalPlaybackTokenMaxDuration)
	}
}

func newPlaybackScopeTestRouter(t *testing.T) (*gin.Engine, *service.Container, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.UserPermission{},
		&model.RefreshToken{},
		&model.Setting{},
		&model.Library{},
		&model.Media{},
		&model.PlayProfile{},
	); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.JWTSecret = "playback-scope-secret"
	log := zap.NewNop()
	permissions := service.NewPermissionService(log, repos)
	auth := service.NewAuthService(cfg, log, repos, service.NewTokenService(cfg, log, repos), permissions)
	svc := &service.Container{
		Cfg:         cfg,
		Repo:        repos,
		Auth:        auth,
		Media:       service.NewMediaService(cfg, log, repos),
		Stream:      service.NewStreamService(cfg, log, repos, nil),
		Permissions: permissions,
	}
	if err := repos.User.Create(t.Context(), &model.User{
		Base:     model.Base{ID: "user-1"},
		Username: "viewer",
		Role:     "admin",
		Tier:     "plus",
		IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Base: model.Base{ID: "lib-1"}, Name: "OpenList", Path: "cloud://openlist/Movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{
			Base:        model.Base{ID: "media-1"},
			LibraryID:   lib.ID,
			Title:       "Cloud 1",
			Path:        "cloud://openlist/Movies/Movie.mkv",
			DurationSec: 2 * 60 * 60,
			STRMURL:     "/api/cloud/play/openlist?ref=/Movies/Movie.mkv",
		},
		{
			Base:      model.Base{ID: "media-2"},
			LibraryID: lib.ID,
			Title:     "Cloud 2",
			Path:      "cloud://openlist/Movies/Other.mkv",
			STRMURL:   "/api/cloud/play/openlist?ref=/Movies/Other.mkv",
		},
	}
	if err := repos.DB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	api := router.Group("/api")
	api.Use(middleware.AuthRequired(cfg.Secrets.JWTSecret))
	api.GET("/playback/:id/external-url", externalURLHandler(svc))
	api.GET("/stream/:id", streamHandler(svc))
	api.GET("/cloud/play/:type", cloudPlayHandler(svc))
	return router, svc, cfg.Secrets.JWTSecret
}

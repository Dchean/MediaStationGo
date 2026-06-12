package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestWithAuthTokenPropagatesToInternalRedirect(t *testing.T) {
	// <video src=/api/stream/{id}?token=JWT> follows the 302 to the cloud
	// play endpoint, which must stay authenticated.
	r := &http.Request{Header: http.Header{}, URL: &url.URL{RawQuery: "token=jwt123&profile=p"}}
	got := withAuthToken("/api/cloud/play/cloud115?ref=abc", r)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("token") != "jwt123" {
		t.Fatalf("token not propagated: %q", got)
	}
	if u.Query().Get("ref") != "abc" {
		t.Fatalf("existing query lost: %q", got)
	}
}

func TestWithAuthTokenNeverLeaksToAbsoluteURL(t *testing.T) {
	// An absolute external direct link (e.g. cloud CDN) must NOT receive the JWT.
	r := &http.Request{Header: http.Header{}, URL: &url.URL{RawQuery: "token=jwt123"}}
	got := withAuthToken("https://cdn.115.example/x.mp4?sig=1", r)
	if strings.Contains(got, "jwt123") {
		t.Fatalf("JWT leaked to external URL: %q", got)
	}
	if got != "https://cdn.115.example/x.mp4?sig=1" {
		t.Fatalf("external URL mutated: %q", got)
	}
}

func TestWithAuthTokenPropagatesToSameOriginAbsoluteInternalURL(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://media.example/Videos/m-1/stream?api_key=jwt123", nil)
	got := withAuthTokenForInternalRedirect("http://media.example/api/cloud/play/openlist?ref=abc", r, "http://media.example")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Query().Get("token") != "jwt123" || u.Query().Get("ref") != "abc" {
		t.Fatalf("same-origin internal URL should keep ref and receive token: %q", got)
	}
}

func TestServeFileRedirectsInternalSTRMAsAbsoluteURLWithToken(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/cloud-1?api_key=jwt123", nil)
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "cloud-1"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "http://nas.local:18080/api/cloud/play/openlist?") ||
		!strings.Contains(loc, "ref=movie") ||
		!strings.Contains(loc, "token=jwt123") {
		t.Fatalf("redirect Location should be absolute and tokenized, got %q", loc)
	}
}

func TestServeFileRedirectUsesForwardedTunnelHost(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/stream/cloud-1?api_key=jwt123", nil)
	req.Header.Set("X-Forwarded-Host", "media.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	if err := svc.ServeFile(w, req, "cloud-1"); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://media.example.com/api/cloud/play/openlist?") ||
		!strings.Contains(loc, "ref=movie") ||
		!strings.Contains(loc, "token=jwt123") {
		t.Fatalf("redirect Location should use forwarded tunnel host and token, got %q", loc)
	}
}

func TestServeFileRedirectsCloudMediaForVideoStreamMode(t *testing.T) {
	repos := newStreamTestRepo(t)
	if err := repos.Setting.Set(t.Context(), STRMEnabledSettingKey, "false"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:    model.Base{ID: "cloud-1"},
		Title:   "Cloud",
		Path:    "cloud://openlist/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=movie",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewStreamService(&config.Config{}, zap.NewNop(), repos, nil)
	req := httptest.NewRequest(http.MethodGet, "http://nas.local:18080/api/stream/cloud-1?api_key=jwt123", nil)
	w := httptest.NewRecorder()

	err := svc.ServeFile(w, req, "cloud-1")
	if err != nil {
		t.Fatalf("video stream mode should still reach cloud playback endpoint: %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/api/cloud/play/openlist?") || !strings.Contains(loc, "token=jwt123") {
		t.Fatalf("redirect Location should target tokenized cloud play endpoint, got %q", loc)
	}
}

func newStreamTestRepo(t *testing.T) *repository.Container {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return repository.New(db)
}

func TestRequestTokenFromBearerHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer hdrtok")
	r := &http.Request{Header: h, URL: &url.URL{}}
	if got := requestToken(r); got != "hdrtok" {
		t.Fatalf("bearer token not extracted: %q", got)
	}
}

func TestAppendQueryToHLSSegments(t *testing.T) {
	in := "#EXTM3U\n#EXTINF:4.0,\nseg_00000.ts\n#EXTINF:4.0,\nseg_00001.ts?old=1\n"
	got := appendQueryToHLSSegments(in, "token=abc")
	if !strings.Contains(got, "seg_00000.ts?token=abc") {
		t.Fatalf("missing tokenized segment: %q", got)
	}
	if !strings.Contains(got, "seg_00001.ts?old=1") {
		t.Fatalf("existing query should be preserved: %q", got)
	}
}

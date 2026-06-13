package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestGenerateSTRMForLibraryWritesFilesAndRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.STRMRecord{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "电影", Path: "cloud://openlist/电影", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	rows := []model.Media{
		{Base: model.Base{ID: "cloud-media"}, LibraryID: lib.ID, Title: "云盘电影", Year: 2026, Path: "cloud://openlist/电影/云盘电影.mkv", STRMURL: "/api/cloud/play/openlist?ref=movie"},
		{Base: model.Base{ID: "local-media"}, LibraryID: lib.ID, Title: "本地电影", Year: 2025, Path: filepath.Join(t.TempDir(), "本地电影.mkv")},
	}
	for i := range rows {
		if err := repos.DB.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	outDir := filepath.Join(t.TempDir(), "strm")
	svc := NewSTRMService(zap.NewNop(), repos, &config.Config{})

	res, err := svc.GenerateForLibrary(t.Context(), GenerateSTRMOptions{
		LibraryID:     lib.ID,
		OutputDir:     outDir,
		BaseURL:       "http://nas.example:18080",
		IncludeLocal:  true,
		PlaybackToken: "strm-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 2 || res.Skipped != 0 {
		t.Fatalf("result = %#v, want generated=2 skipped=0", res)
	}
	cloudSTRM := filepath.Join(outDir, "云盘电影 (2026)", "云盘电影 (2026).strm")
	localSTRM := filepath.Join(outDir, "本地电影 (2025)", "本地电影 (2025).strm")
	assertFileContains(t, cloudSTRM, "http://nas.example:18080/api/stream/cloud-media?token=strm-token")
	assertFileContains(t, localSTRM, "http://nas.example:18080/api/stream/local-media?token=strm-token")
	if got, err := repos.Setting.Get(t.Context(), "app.server_url"); err != nil || got != "http://nas.example:18080" {
		t.Fatalf("app.server_url = %q, %v; want generated base url", got, err)
	}
	if got, err := repos.Setting.Get(t.Context(), "strm.base_url"); err != nil || got != "http://nas.example:18080" {
		t.Fatalf("strm.base_url = %q, %v; want generated base url", got, err)
	}

	var count int64
	if err := repos.DB.Model(&model.STRMRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("strm record count = %d, want 2", count)
	}

	res, err = svc.GenerateForLibrary(t.Context(), GenerateSTRMOptions{
		LibraryID:     lib.ID,
		OutputDir:     outDir,
		BaseURL:       "http://nas.example:18080",
		IncludeLocal:  true,
		PlaybackToken: "strm-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 2 {
		t.Fatalf("second run skipped = %d, want 2", res.Skipped)
	}
}

func TestGenerateSTRMForLibrarySignsDefaultPlaybackToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.STRMRecord{}, &model.Setting{}, &model.User{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	admin := model.User{Username: "admin", PasswordHash: "x", Role: "admin", Tier: "plus", IsActive: true}
	if err := repos.User.Create(t.Context(), &admin); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "电影", Path: "cloud://openlist/电影", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{Base: model.Base{ID: "cloud-media"}, LibraryID: lib.ID, Title: "云盘电影", Year: 2026, Path: "cloud://openlist/电影/云盘电影.mkv", STRMURL: "/api/cloud/play/openlist?ref=movie"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(t.TempDir(), "strm")
	const secret = "test-secret"
	svc := NewSTRMService(zap.NewNop(), repos, &config.Config{Secrets: config.SecretsConfig{JWTSecret: secret}})
	res, err := svc.GenerateForLibrary(t.Context(), GenerateSTRMOptions{
		LibraryID:    lib.ID,
		OutputDir:    outDir,
		BaseURL:      "http://nas.example:18080",
		IncludeLocal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Generated != 1 || len(res.Errors) != 0 {
		t.Fatalf("result = %#v, want generated=1 with no errors", res)
	}
	cloudSTRM := filepath.Join(outDir, "云盘电影 (2026)", "云盘电影 (2026).strm")
	got := readSTRM(t, cloudSTRM)
	if !strings.HasPrefix(got, "http://nas.example:18080/api/stream/cloud-media?token=") {
		t.Fatalf("generated url = %q, want tokenized /api/stream url", got)
	}
	token := strings.TrimPrefix(got, "http://nas.example:18080/api/stream/cloud-media?token=")
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("generated token did not validate: %v", err)
	}
	if claims.UserID != admin.ID || claims.Role != "admin" || claims.Tier != "plus" {
		t.Fatalf("claims = %#v, want admin identity", claims)
	}
	if ttl := time.Until(claims.ExpiresAt.Time); ttl < EmbyTokenDuration-time.Minute {
		t.Fatalf("token ttl = %v, want close to %v", ttl, EmbyTokenDuration)
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	if got := readSTRM(t, path); got != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func readSTRM(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newScannerTestEnv(t *testing.T) (*ScannerService, *repository.Container) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	sc := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	return sc, repos
}

func countMedia(t *testing.T, repos *repository.Container) int64 {
	t.Helper()
	var n int64
	if err := repos.DB.Model(&model.Media{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIngestPathAddsSingleFile(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Some Movie (2021).mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := sc.IngestPath(t.Context(), lib.ID, file)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if !added {
		t.Fatal("expected file to be added")
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1", got)
	}
	// Non-video file is ignored.
	other := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if added, _ := sc.IngestPath(t.Context(), lib.ID, other); added {
		t.Fatal("non-video file should not be ingested")
	}
}

func TestScanLibraryReadsLocalSTRMTarget(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "STRM", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	strmPath := filepath.Join(root, "Cloud Movie.strm")
	if err := os.WriteFile(strmPath, []byte("https://cdn.example.com/movie.mkv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("scan result = %#v, want added=1", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.Container != "strm" || media.STRMURL != "https://cdn.example.com/movie.mkv" {
		t.Fatalf("strm media not parsed: %#v", media)
	}
}

func TestScanLibrarySkipsUnchangedExistingLocalMedia(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Already In Library (2024).mkv")
	if err := os.WriteFile(file, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 || first.Skipped != 0 {
		t.Fatalf("first scan = %#v, want added=1 skipped=0", first)
	}
	second, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Added != 0 || second.Updated != 0 || second.Skipped != 1 {
		t.Fatalf("second scan = %#v, want unchanged file skipped", second)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1", got)
	}
}

func TestScanLibrarySkipsUnchangedExistingLocalMediaWithMissingTrackMetadata(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Already In Library Missing Tracks (2024).mkv")
	if err := os.WriteFile(file, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Added != 1 {
		t.Fatalf("first scan = %#v, want added=1", first)
	}

	sc.probe = NewFFprobeService(&config.Config{}, zap.NewNop())
	second, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Added != 0 || second.Updated != 0 || second.Skipped != 1 || second.Probed != 0 {
		t.Fatalf("second scan = %#v, want unchanged file skipped without synchronous track probe", second)
	}
}

func TestScanLibraryImportsNewLocalMediaWithoutSynchronousProbe(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	sc.probe = NewFFprobeService(&config.Config{}, zap.NewNop())
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "New Movie (2026).mkv")
	if err := os.WriteFile(file, []byte("new-file"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Added != 1 || res.Probed != 0 {
		t.Fatalf("scan result = %#v, want fast import without synchronous ffprobe", res)
	}
}

func TestScanLibraryReportsPerFileUpsertErrors(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Broken DB", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Cannot Insert (2024).mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Exec("DROP TABLE media").Error; err != nil {
		t.Fatal(err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan should continue and report file errors, got top-level error: %v", err)
	}
	if res.ErrorCount != 1 || len(res.Errors) != 1 {
		t.Fatalf("scan errors = count %d details %#v, want one detailed error", res.ErrorCount, res.Errors)
	}
	if res.Visited != 1 {
		t.Fatalf("visited = %d, want 1", res.Visited)
	}
}

func TestScanLibraryMapsPersistedHostLibraryPath(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(containerLibrary, "狂飙.S01E01.2023.mkv")
	if err := os.WriteFile(file, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `Q:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	lib := model.Library{Name: "国产剧", Path: `Q:\media\电视剧\国产剧`, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan mapped host path: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("scan result = %#v, want added=1", res)
	}
	var stored model.Library
	if err := repos.DB.First(&stored, "id = ?", lib.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Path != filepath.Clean(containerLibrary) {
		t.Fatalf("stored path = %q, want %q", stored.Path, filepath.Clean(containerLibrary))
	}
}

func TestRemovePathDeletesVanishedMedia(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	cache := NewRuntimeCacheService(&config.Config{}, zap.NewNop())
	sc.SetRuntimeCache(cache)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "Gone (2020).mkv")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.IngestPath(t.Context(), lib.ID, file); err != nil {
		t.Fatal(err)
	}
	if countMedia(t, repos) != 1 {
		t.Fatal("expected 1 media before removal")
	}
	cache.SetJSON(t.Context(), "media:list:test", map[string]string{"state": "stale"}, time.Minute)
	var cached map[string]string
	if !cache.GetJSON(t.Context(), "media:list:test", &cached) {
		t.Fatal("expected media cache to be primed")
	}
	// A still-present file is not removed.
	if removed, _ := sc.RemovePath(t.Context(), file); removed != 0 {
		t.Fatalf("present file should not be removed, got %d", removed)
	}
	if !cache.GetJSON(t.Context(), "media:list:test", &cached) {
		t.Fatal("present file should not invalidate media cache")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	removed, err := sc.RemovePath(t.Context(), file)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if countMedia(t, repos) != 0 {
		t.Fatal("expected 0 media after removal")
	}
	if cache.GetJSON(t.Context(), "media:list:test", &cached) {
		t.Fatal("vanished media removal should invalidate media cache")
	}
}

// TestScanSkipsHardlinkDuplicate verifies that a hardlink (same inode) kept
// for seeding is not imported as a second media item, preventing duplicate
// recognition and double-counted storage.
func TestScanSkipsHardlinkDuplicate(t *testing.T) {
	sc, repos := newScannerTestEnv(t)
	root := t.TempDir()
	lib := model.Library{Name: "Movies", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(root, "Movie (2019).mkv")
	if err := os.WriteFile(orig, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "Movie (2019) [organized].mkv")
	if err := os.Link(orig, linked); err != nil {
		t.Skipf("hardlinks unsupported on this fs: %v", err)
	}
	res, err := sc.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got := countMedia(t, repos); got != 1 {
		t.Fatalf("media count = %d, want 1 (hardlink should be deduped)", got)
	}
	if res.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", res.Skipped)
	}
}

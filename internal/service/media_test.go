package service

import (
	"fmt"
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

func TestResolveAccessibleLibraryPathMapsConfiguredHostMediaDir(t *testing.T) {
	root := t.TempDir()
	hostRoot := filepath.Join(root, "nas", "host", "media")
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", hostRoot)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	got, err := resolveAccessibleLibraryPath(filepath.Join(hostRoot, "电视剧", "国产剧"))
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestResolveAccessibleLibraryPathMapsWindowsDriveBeforeLinuxAbs(t *testing.T) {
	root := t.TempDir()
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `Q:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	for _, input := range []string{
		`Q:\media\电视剧\国产剧`,
		`Q:/media/电视剧/国产剧`,
		`/app/Q:\media\电视剧\国产剧`,
		`/app/Q:/media/电视剧/国产剧`,
	} {
		t.Run(input, func(t *testing.T) {
			got, err := resolveAccessibleLibraryPath(input)
			if err != nil {
				t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
			}
			if got != filepath.Clean(containerLibrary) {
				t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
			}
		})
	}
}

func TestResolveAccessibleLibraryPathRecoversDockerPollutedWindowsDrive(t *testing.T) {
	root := t.TempDir()
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `F:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	got, err := resolveAccessibleLibraryPath(`/app/F:\media\电视剧\国产剧`)
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestResolveAccessibleLibraryPathKeepsAccessibleContainerPath(t *testing.T) {
	containerLibrary := filepath.Join(t.TempDir(), "media", "电影")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAccessibleLibraryPath(containerLibrary)
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestInferLibraryKindFromCategoryPathOverridesMovieDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "国产剧", path: `/media/电视剧/国产剧`, want: "tv"},
		{name: "日漫", path: `/media/电视剧/日漫`, want: "anime"},
		{name: "综艺", path: `/media/电视剧/综艺`, want: "variety"},
		{name: "成人", path: `/media/成人`, want: "adult"},
		{name: "动画电影", path: `/media/电影/动画电影`, want: "movie"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferLibraryKind(tc.name, tc.path, "movie"); got != tc.want {
				t.Fatalf("inferLibraryKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMappedPathCandidatesMapWindowsDriveDownloadMarker(t *testing.T) {
	root := t.TempDir()
	containerDownloads := filepath.Join(root, "container", "downloads")
	containerLibrary := filepath.Join(containerDownloads, "国产剧")
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", containerDownloads)

	want := filepath.Clean(containerLibrary)
	for _, got := range mappedPathCandidates(`F:\downloads\国产剧`) {
		if got == want {
			return
		}
	}
	t.Fatalf("mappedPathCandidates() missing %q", want)
}

func TestResolveMappedDestinationPathPrefersConfiguredContainerMapping(t *testing.T) {
	root := t.TempDir()
	containerMedia := filepath.Join(root, "container", "media")
	if err := os.MkdirAll(containerMedia, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `Q:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerMedia)

	for _, input := range []string{`Q:\media`, `Q:/media`, `/app/Q:\media`} {
		t.Run(input, func(t *testing.T) {
			got := resolveMappedDestinationPath(input)
			if got != filepath.Clean(containerMedia) {
				t.Fatalf("resolveMappedDestinationPath() = %q, want %q", got, filepath.Clean(containerMedia))
			}
		})
	}
}

func TestResolveAccessibleMappedPathMapsWindowsDownloadVariants(t *testing.T) {
	root := t.TempDir()
	containerDownloads := filepath.Join(root, "container", "downloads")
	containerSource := filepath.Join(containerDownloads, "国产剧")
	if err := os.MkdirAll(containerSource, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_DOWNLOAD_DIR", `Q:\downloads`)
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", containerDownloads)

	for _, input := range []string{`Q:\downloads\国产剧`, `Q:/downloads/国产剧`, `/app/Q:\downloads\国产剧`} {
		t.Run(input, func(t *testing.T) {
			got, info, err := resolveAccessibleMappedPath(input)
			if err != nil {
				t.Fatalf("resolveAccessibleMappedPath() error = %v", err)
			}
			if !info.IsDir() {
				t.Fatalf("resolved path is not dir")
			}
			if got != filepath.Clean(containerSource) {
				t.Fatalf("resolveAccessibleMappedPath() = %q, want %q", got, filepath.Clean(containerSource))
			}
		})
	}
}

func TestDeleteCloudLibraryPurgesMountWithoutRecycleBin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "OpenList · 剑来", Path: "cloud://openlist/Anime/JianLai", Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(t.Context(), &model.Media{
		LibraryID: lib.ID,
		Title:     "剑来",
		Path:      "cloud://openlist/Anime/JianLai/Season 1/01.mkv",
		STRMURL:   "/api/cloud/play/openlist?ref=/Anime/JianLai/Season%201/01.mkv",
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	if err := svc.DeleteLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}

	var mediaCount int64
	if err := db.Unscoped().Model(&model.Media{}).Where("library_id = ?", lib.ID).Count(&mediaCount).Error; err != nil {
		t.Fatal(err)
	}
	if mediaCount != 0 {
		t.Fatalf("cloud mount media should be purged, count=%d", mediaCount)
	}
	recycle, err := svc.ListRecycleBin(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recycle) != 0 {
		t.Fatalf("cloud mount removal must not populate recycle bin: %#v", recycle)
	}
	var libCount int64
	if err := db.Unscoped().Model(&model.Library{}).Where("id = ?", lib.ID).Count(&libCount).Error; err != nil {
		t.Fatal(err)
	}
	if libCount != 0 {
		t.Fatalf("cloud mount library should be purged, count=%d", libCount)
	}
}

func TestSoftDeleteCloudMediaPurgesRecordWithoutRecycleBin(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	media := model.Media{
		Base:    model.Base{ID: "cloud-media"},
		Title:   "网盘电影",
		Path:    "cloud://openlist/电影/Movie.mkv",
		STRMURL: "/api/cloud/play/openlist?ref=/电影/Movie.mkv",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	if err := svc.SoftDelete(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Unscoped().Model(&model.Media{}).Where("id = ?", media.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cloud media should be purged, count=%d", count)
	}
	recycle, err := svc.ListRecycleBin(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(recycle) != 0 {
		t.Fatalf("cloud media removal must not populate recycle bin: %#v", recycle)
	}
}

func TestListRecycleBinPrunesOldRowsOverLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	now := time.Now()
	for i := 0; i < maxRecycleBinRecords+5; i++ {
		deletedAt := now.Add(time.Duration(i) * time.Second)
		media := model.Media{
			Base: model.Base{
				ID:        fmt.Sprintf("media-%03d", i),
				DeletedAt: gorm.DeletedAt{Time: deletedAt, Valid: true},
			},
			Title: fmt.Sprintf("Movie %03d", i),
			Path:  filepath.Join(t.TempDir(), fmt.Sprintf("Movie %03d.mkv", i)),
		}
		if err := db.Unscoped().Create(&media).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	rows, err := svc.ListRecycleBin(t.Context(), 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != maxRecycleBinRecords {
		t.Fatalf("recycle rows = %d, want %d", len(rows), maxRecycleBinRecords)
	}
	var count int64
	if err := db.Unscoped().Model(&model.Media{}).Where("deleted_at IS NOT NULL").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != maxRecycleBinRecords {
		t.Fatalf("stored recycle rows = %d, want %d", count, maxRecycleBinRecords)
	}
	var oldCount int64
	if err := db.Unscoped().Model(&model.Media{}).Where("id IN ?", []string{"media-000", "media-001", "media-002", "media-003", "media-004"}).Count(&oldCount).Error; err != nil {
		t.Fatal(err)
	}
	if oldCount != 0 {
		t.Fatalf("oldest recycle rows were not pruned, count=%d", oldCount)
	}
}

func TestSoftDeleteInvalidatesMediaAndStatsCache(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	media := model.Media{
		Base:  model.Base{ID: "local-media"},
		Title: "Cached Movie",
		Path:  filepath.Join(t.TempDir(), "Cached Movie.mkv"),
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	cache := NewRuntimeCacheService(&config.Config{}, zap.NewNop())
	cache.SetJSON(t.Context(), "media:list:test", map[string]string{"state": "stale"}, time.Minute)
	cache.SetJSON(t.Context(), "stats:snapshot:base", map[string]int{"media": 1}, time.Minute)
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos).SetRuntimeCache(cache)
	if err := svc.SoftDelete(t.Context(), media.ID); err != nil {
		t.Fatal(err)
	}

	var mediaCache map[string]string
	if cache.GetJSON(t.Context(), "media:list:test", &mediaCache) {
		t.Fatal("soft delete should invalidate media cache")
	}
	var statsCache map[string]int
	if cache.GetJSON(t.Context(), "stats:snapshot:base", &statsCache) {
		t.Fatal("soft delete should invalidate stats cache")
	}
}

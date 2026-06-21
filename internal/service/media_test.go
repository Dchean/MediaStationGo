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

func TestGroupMediaVersionsMergesEpisodeByExternalIDAcrossLibraries(t *testing.T) {
	local := model.Media{
		LibraryID:  "local-tv",
		Title:      "折腰",
		Path:       "/media/国产剧/折腰 (2025)/Season 1/折腰.S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
		TMDbID:     296753,
		SizeBytes:  100,
		PosterURL:  "https://image.tmdb.org/t/p/w500/poster.jpg",
	}
	cloud := model.Media{
		LibraryID:  "cloud-tv",
		Title:      "折腰",
		Path:       "cloud://openlist/国产剧/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
		TMDbID:     296753,
		SizeBytes:  200,
		STRMURL:    "/api/cloud/play/openlist?ref=/国产剧/折腰/01.mkv",
	}

	grouped := groupMediaVersions([]model.Media{local, cloud})
	if len(grouped) != 1 {
		t.Fatalf("grouped len = %d, want 1: %#v", len(grouped), grouped)
	}
	if len(grouped[0].Versions) != 2 {
		t.Fatalf("versions len = %d, want 2: %#v", len(grouped[0].Versions), grouped[0].Versions)
	}
	if grouped[0].Media.Path != local.Path {
		t.Fatalf("local version should remain primary, got %q want %q", grouped[0].Media.Path, local.Path)
	}
	if grouped[0].Versions[0].Path != local.Path || grouped[0].Versions[1].Path != cloud.Path {
		t.Fatalf("versions should be ordered local before cloud, got %#v", grouped[0].Versions)
	}
}

func TestUpdateMediaMetadataMarksManualMatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "自采集", Path: "/media/custom", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{Base: model.Base{ID: "custom-media"}, LibraryID: lib.ID, Title: "raw", Path: "/media/custom/raw.mp4", ScrapeStatus: "no_match"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)
	title := "手动标题"
	overview := "手动简介"
	season := 0
	episode := 1
	tmdbID := 12345
	nsfw := true
	updated, err := svc.UpdateMetadata(t.Context(), media.ID, MediaMetadataUpdate{
		Title:      &title,
		Overview:   &overview,
		SeasonNum:  &season,
		EpisodeNum: &episode,
		TMDbID:     &tmdbID,
		NSFW:       &nsfw,
	})
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Title != title || updated.Overview != overview || updated.ScrapeStatus != "matched" {
		t.Fatalf("metadata not saved: %#v", updated)
	}
	if updated.SeasonNum != 0 || updated.EpisodeNum != 1 || updated.TMDbID != tmdbID || !updated.NSFW {
		t.Fatalf("ids/episode metadata not saved: %#v", updated)
	}
}

func TestMediaUpsertBackfillsExternalIDsForPendingCloudRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	path := "cloud://openlist/国漫/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv"
	if err := repos.DB.Create(&model.Media{
		LibraryID:    "cloud-tv",
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(t.Context(), &model.Media{
		LibraryID:    "cloud-tv",
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       296753,
		Year:         2025,
		ScrapeStatus: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "path = ?", path).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 296753 || got.Year != 2025 || got.ScrapeStatus != "pending" {
		t.Fatalf("pending cloud row was not backfilled correctly: tmdb=%d year=%d status=%q", got.TMDbID, got.Year, got.ScrapeStatus)
	}
}

func TestMediaUpsertCorrectsCloudExternalIDConflicts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	path := "cloud://openlist/国产剧/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv"
	if err := repos.DB.Create(&model.Media{
		LibraryID:    "cloud-tv",
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       220269,
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.Upsert(t.Context(), &model.Media{
		LibraryID:    "cloud-tv",
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       296753,
		Year:         2025,
		ScrapeStatus: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "path = ?", path).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 296753 || got.ScrapeStatus != "pending" {
		t.Fatalf("cloud external id conflict was not corrected: tmdb=%d status=%q", got.TMDbID, got.ScrapeStatus)
	}
}

func TestRepairCloudPathMetadataBackfillsExistingPlaceholders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	path := "cloud://openlist/动画电影/雄狮少年2 (2024) {tmdb-1154478}/雄狮少年2 (2024) - 2160p.WEB-DL.H.265.DDP 5.1-ADWeb.mp4"
	if err := repos.DB.Create(&model.Media{
		LibraryID:    "cloud-movie",
		Title:        "雄狮少年2 adweb",
		Path:         path,
		ScrapeStatus: "no_match",
	}).Error; err != nil {
		t.Fatal(err)
	}
	container := &Container{Repo: repos, Log: zap.NewNop()}
	repaired, err := container.RepairCloudPathMetadata(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
	var got model.Media
	if err := repos.DB.First(&got, "path = ?", path).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 1154478 || got.Year != 2024 || got.Title != "雄狮少年2" || got.ScrapeStatus != "pending" {
		t.Fatalf("placeholder was not repaired: title=%q tmdb=%d year=%d status=%q", got.Title, got.TMDbID, got.Year, got.ScrapeStatus)
	}
}

func TestRepairCloudPathMetadataCorrectsConflictingMatchedID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	path := "cloud://openlist/国产剧/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv"
	if err := repos.DB.Create(&model.Media{
		LibraryID:    "cloud-tv",
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       220269,
		ScrapeStatus: "matched",
	}).Error; err != nil {
		t.Fatal(err)
	}
	container := &Container{Repo: repos, Log: zap.NewNop()}
	repaired, err := container.RepairCloudPathMetadata(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
	var got model.Media
	if err := repos.DB.First(&got, "path = ?", path).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 296753 || got.ScrapeStatus != "pending" {
		t.Fatalf("conflicting matched id was not repaired: tmdb=%d status=%q", got.TMDbID, got.ScrapeStatus)
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

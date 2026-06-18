package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/database"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestMediaUpsertSkipsUnchangedExistingRow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "已有影片",
		Path:         "/media/movie/existing.mkv",
		SizeBytes:    1024,
		DurationSec:  60,
		Width:        1920,
		Height:       1080,
		VideoCodec:   "h264",
		AudioCodec:   "aac",
		Container:    "matroska,webm",
		ScrapeStatus: "pending",
	}
	if err := repos.Media.Upsert(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	var before model.Media
	if err := repos.DB.Where("path = ?", media.Path).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	again := model.Media{
		LibraryID:    lib.ID,
		Title:        before.Title,
		Path:         before.Path,
		SizeBytes:    before.SizeBytes,
		DurationSec:  before.DurationSec,
		Width:        before.Width,
		Height:       before.Height,
		VideoCodec:   before.VideoCodec,
		AudioCodec:   before.AudioCodec,
		Container:    before.Container,
		ScrapeStatus: before.ScrapeStatus,
	}
	if err := repos.Media.Upsert(t.Context(), &again); err != nil {
		t.Fatal(err)
	}
	var after model.Media
	if err := repos.DB.Where("path = ?", media.Path).First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("unchanged upsert touched updated_at: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestMediaUpsertRefreshesCloudExternalIDFromPathHint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "OpenList · 国产剧", Path: "cloud://openlist/国产剧", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	path := "cloud://openlist/国产剧/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv"
	existing := model.Media{
		LibraryID:    lib.ID,
		Title:        "折腰",
		Path:         path,
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       220269,
		ScrapeStatus: "matched",
	}
	if err := repos.Media.Upsert(t.Context(), &existing); err != nil {
		t.Fatal(err)
	}
	next := model.Media{
		LibraryID:  lib.ID,
		Title:      "折腰",
		Path:       path,
		SeasonNum:  1,
		EpisodeNum: 1,
		TMDbID:     296753,
	}
	if err := repos.Media.Upsert(t.Context(), &next); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.Where("path = ?", path).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.TMDbID != 296753 || got.ScrapeStatus != "pending" {
		t.Fatalf("cloud path hint should refresh tmdb and retry scrape, got tmdb=%d status=%q", got.TMDbID, got.ScrapeStatus)
	}
}

type fakeMediaSearchBackend struct {
	ids []string
	err error
}

func (f fakeMediaSearchBackend) SearchMediaIDs(context.Context, string, int, int, MediaQueryFilter) ([]string, int64, error) {
	if f.err != nil {
		return nil, 0, f.err
	}
	return append([]string(nil), f.ids...), int64(len(f.ids)), nil
}

func TestMediaSearchUsesExternalBackendAndFallsBack(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "Movies", Path: "/media/movie", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.Media{
		{Base: model.Base{ID: "m-1"}, LibraryID: lib.ID, Title: "Alpha", Path: "/media/a.mkv"},
		{Base: model.Base{ID: "m-2"}, LibraryID: lib.ID, Title: "Beta", Path: "/media/b.mkv"},
	} {
		if err := repos.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	repos.Media.SetSearchBackend(fakeMediaSearchBackend{ids: []string{"m-2", "m-1"}})
	items, total, err := repos.Media.SearchFilteredPage(t.Context(), "anything", 0, 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 || items[0].ID != "m-2" || items[1].ID != "m-1" {
		t.Fatalf("external search result total=%d items=%#v", total, items)
	}

	repos.Media.SetSearchBackend(fakeMediaSearchBackend{err: errors.New("opensearch down")})
	items, total, err = repos.Media.SearchFilteredPage(t.Context(), "Alpha", 0, 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "m-1" {
		t.Fatalf("fallback result total=%d items=%#v", total, items)
	}
}

func TestMediaSearchFilteredSupportsChineseFuzzyTerms(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "国产剧", Path: "/media/国产剧", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	rows := []model.Media{
		{
			Base:         model.Base{ID: "m-ferry"},
			LibraryID:    lib.ID,
			Title:        "灵魂摆渡·十年",
			OriginalName: "The Ferry Man 10th Anniversary",
			Path:         "/media/国产剧/灵魂摆渡·十年/S01E01.mkv",
			Genres:       "悬疑,奇幻",
		},
		{
			Base:         model.Base{ID: "m-ashes"},
			LibraryID:    lib.ID,
			Title:        "翘楚",
			OriginalName: "Ashes to Crown",
			Path:         "/media/国产剧/翘楚/S01E01.mkv",
			Genres:       "剧情",
		},
	}
	for i := range rows {
		if err := repos.Media.Upsert(t.Context(), &rows[i]); err != nil {
			t.Fatalf("upsert media: %v", err)
		}
	}

	items, err := repos.Media.SearchFiltered(t.Context(), "灵魂 十年", 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("search chinese terms: %v", err)
	}
	if len(items) == 0 || items[0].ID != "m-ferry" {
		t.Fatalf("chinese fuzzy search missed target: %#v", items)
	}

	items, err = repos.Media.SearchFiltered(t.Context(), "Ferry", 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("search original name: %v", err)
	}
	if len(items) == 0 || items[0].ID != "m-ferry" {
		t.Fatalf("original-name search missed target: %#v", items)
	}

	items, err = repos.Media.SearchFiltered(t.Context(), "悬疑", 10, MediaQueryFilter{IncludeNSFW: true})
	if err != nil {
		t.Fatalf("search genre: %v", err)
	}
	if len(items) == 0 || items[0].ID != "m-ferry" {
		t.Fatalf("genre search missed target: %#v", items)
	}
}

func TestMediaSearchIndexBackfillRunsInBatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := New(db)
	lib := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		Base:      model.Base{ID: "m-backfill"},
		LibraryID: lib.ID,
		Title:     "后台索引",
		Path:      "/media/movie/后台索引.mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 插入触发器应当同步维护 FTS 行。
	var indexed int64
	if err := repos.DB.Raw(`SELECT COUNT(*) FROM media_search_fts`).Scan(&indexed).Error; err != nil {
		t.Fatal(err)
	}
	if indexed != 1 {
		t.Fatalf("insert trigger should index new media, got %d rows", indexed)
	}
	// 清空 FTS 模拟旧库升级后索引缺失，回填应按批补齐且 rowid 对齐。
	if err := repos.DB.Exec(`DELETE FROM media_search_fts`).Error; err != nil {
		t.Fatal(err)
	}
	n, err := repos.Media.BackfillSearchIndex(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("backfilled rows = %d, want 1", n)
	}
	var aligned int64
	if err := repos.DB.Raw(`SELECT COUNT(*) FROM media_search_fts f JOIN media m ON f.rowid = m.rowid AND f.media_id = m.id`).Scan(&aligned).Error; err != nil {
		t.Fatal(err)
	}
	if aligned != 1 {
		t.Fatalf("fts rows aligned with media rowid = %d, want 1", aligned)
	}
	// 软删除后触发器应清理对应 FTS 行，避免搜索命中已删媒体。
	if err := repos.DB.Delete(&model.Media{}, "id = ?", "m-backfill").Error; err != nil {
		t.Fatal(err)
	}
	var after int64
	if err := repos.DB.Raw(`SELECT COUNT(*) FROM media_search_fts`).Scan(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("soft delete should drop fts row, got %d", after)
	}
}

package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestCleanQuery(t *testing.T) {
	cases := []struct {
		in        string
		wantTitle string
		wantYear  int
	}{
		{"Inception.2010.1080p.BluRay.x264.mkv", "inception", 2010},
		{"The_Matrix_(1999).1080p.WEB-DL.H265.mp4", "the matrix", 1999},
		{"interstellar.2014.4k.hdr.dts.atmos.mkv", "interstellar", 2014},
		{"My Movie 2022 [HDR] (1080p) [TGx].mp4", "my movie", 2022},
		{"NoYearOrTags.mkv", "noyearortags", 0},
		{"亏成首富从游戏开始 The Richest in Game - S01E11 - 4K.mp4", "亏成首富从游戏开始 the richest in game", 0},
		{"紫川.2024.S02E24.第24集.2160p.WEB-DL.H.265-ColorTV.mkv", "紫川", 2024},
		{"紫川 (2024) {tmdb-247590}", "紫川", 2024},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotTitle, gotYear := CleanQuery(tc.in)
			if gotTitle != tc.wantTitle || gotYear != tc.wantYear {
				t.Errorf("CleanQuery(%q) = (%q, %d), want (%q, %d)",
					tc.in, gotTitle, gotYear, tc.wantTitle, tc.wantYear)
			}
		})
	}
}

func TestExternalIDHintsFromText(t *testing.T) {
	hints := externalIDHintsFromText("国漫/折腰 (2025) {tmdb 296753}/Season 1/折腰.S01E01.mkv")
	if hints.TMDbID != 296753 {
		t.Fatalf("tmdb hint = %d, want 296753", hints.TMDbID)
	}
	hints = externalIDHintsFromText("Movie (2026) {tmdb-1630433} [douban=3622222] {bgm 456789} {tvdb:12345}")
	if hints.TMDbID != 1630433 || hints.DoubanID != "3622222" || hints.BangumiID != 456789 || hints.TheTVDBID != "12345" {
		t.Fatalf("external hints not parsed: %+v", hints)
	}
}

func TestPathHintMetadataDoesNotMarkMediaMatched(t *testing.T) {
	meta, hints := pathHintMetadata("cloud://openlist/国漫/折腰 (2025) {tmdb 296753}/Season 1/折腰.S01E01.mkv", true)
	if meta == nil || hints.TMDbID != 296753 || meta.TMDbID != 296753 || meta.Title != "折腰" || meta.Year != 2025 {
		t.Fatalf("path hint metadata = %+v hints=%+v", meta, hints)
	}
	media := &model.Media{Title: "折腰", ScrapeStatus: "pending"}
	applyLocalMetadata(media, meta)
	if media.ScrapeStatus != "pending" {
		t.Fatalf("path hints alone must not mark media matched, got %q", media.ScrapeStatus)
	}
}

func TestEnrichOneCloudPathHintOverridesStaleTMDbID(t *testing.T) {
	var requested []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/296753":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             296753,
				"name":           "折腰",
				"overview":       "正确的剧集条目",
				"poster_path":    "/zheyao.jpg",
				"first_air_date": "2025-05-13",
				"origin_country": []string{"CN"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	cfg.Secrets.TMDbImageProxy = upstream.URL + "/images"
	log := zap.NewNop()
	scraper := NewScraperService(cfg, log, repos, NewTMDbProvider(cfg, log, nil), nil, nil, nil, NewHub(log))

	lib := model.Library{Name: "OpenList · 国产剧", Path: "cloud://openlist/国产剧", Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "折腰",
		Path:         "cloud://openlist/国产剧/折腰 (2025) {tmdb-296753}/Season 1/折腰.S01E01.mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       220269,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "matched" || got.TMDbID != 296753 || got.Title != "折腰" || got.PosterURL == "" {
		t.Fatalf("path hint was not authoritative: status=%q tmdb=%d title=%q poster=%q", got.ScrapeStatus, got.TMDbID, got.Title, got.PosterURL)
	}
	for _, path := range requested {
		if path == "/tv/220269" || path == "/movie/220269" {
			t.Fatalf("scraper queried stale tmdb id; requests=%v", requested)
		}
	}
}

func TestManualRequestMatchFallsBackToCandidatePayload(t *testing.T) {
	scraper := &ScraperService{}
	match, err := scraper.manualRequestMatch(t.Context(), ManualScrapeRequest{
		Source:   "douban",
		Title:    "手动选择的电影",
		DoubanID: "1234567",
		Year:     2026,
	})
	if err != nil {
		t.Fatal(err)
	}
	if match.Title != "手动选择的电影" || match.DoubanID != "1234567" || match.Year != 2026 {
		t.Fatalf("fallback match = %#v", match)
	}
}

func TestManualSearchReturnsTMDbCandidatePage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search/movie" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{
					"id":            101,
					"title":         "错误的同名电影",
					"poster_path":   "/wrong.jpg",
					"release_date":  "2021-01-01",
					"vote_average":  5.1,
					"genre_ids":     []int{18},
					"backdrop_path": "/wrong-backdrop.jpg",
				},
				{
					"id":            202,
					"title":         "正确的同名电影",
					"poster_path":   "/right.jpg",
					"release_date":  "2021-08-01",
					"vote_average":  8.2,
					"genre_ids":     []int{28},
					"backdrop_path": "/right-backdrop.jpg",
				},
			},
		})
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	log := zap.NewNop()
	scraper := NewScraperService(cfg, log, repos, NewTMDbProvider(cfg, log, nil), nil, nil, nil, NewHub(log))

	lib := model.Library{Name: "电影", Path: "/media/movie", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "同名电影", Path: "/media/movie/同名电影.mkv"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	results, err := scraper.ManualSearch(t.Context(), &media, "同名电影", "tmdb", "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].TMDbID != 101 || results[1].TMDbID != 202 {
		t.Fatalf("manual TMDb candidates = %#v", results)
	}
}

func TestManualSearchIncludesAdultProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<a class="box" href="/v/ssis001"><strong>SSIS-001 手动候选</strong></a>`))
		case "/v/ssis001":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<h2 class="title"><strong>SSIS-001 手动成人标题</strong></h2><img class="video-cover" src="/cover.jpg">`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}, &model.APIConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	apiConfig := NewAPIConfigService(zap.NewNop(), repos, NewCryptoService("", zap.NewNop()))
	baseURL := upstream.URL
	if _, err := apiConfig.Update(t.Context(), "adult", APIConfigPatch{BaseURL: &baseURL}); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	scraper := NewScraperService(&config.Config{}, log, repos, nil, nil, nil, nil, NewHub(log), NewAdultProvider(log, apiConfig))

	lib := model.Library{Name: "成人", Path: "/media/adult", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{LibraryID: lib.ID, Title: "SSIS-001", OriginalName: "SSIS-001", Path: "/media/adult/SSIS-001.mkv"}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	results, err := scraper.ManualSearch(t.Context(), &media, "SSIS-001", "adult", "adult")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Source != "adult" || results[0].MediaType != "adult" || !results[0].NSFW || results[0].OriginalName != "SSIS-001" {
		t.Fatalf("manual adult candidates = %#v", results)
	}
}

func TestApplyManualMatchSavesSelectedCloudMatchWhenDetailsSlow(t *testing.T) {
	oldTimeout := tmdbDetailsTimeout
	tmdbDetailsTimeout = 20 * time.Millisecond
	defer func() { tmdbDetailsTimeout = oldTimeout }()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/77" {
			http.NotFound(w, r)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    77,
				"title": "Slow Details",
			})
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	log := zap.NewNop()
	scraper := NewScraperService(cfg, log, repos, NewTMDbProvider(cfg, log, nil), nil, nil, nil, NewHub(log))

	lib := model.Library{Name: "OpenList · Movies", Path: "cloud://openlist/Movies", Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "bad cloud title",
		Path:         "cloud://openlist/Movies/Bad.Title.2026.mkv",
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if _, err := scraper.ApplyManualMatch(t.Context(), media.ID, ManualScrapeRequest{
		Source:    "manual",
		MediaType: "movie",
		Title:     "Correct Cloud Movie",
		TMDbID:    77,
		Year:      2026,
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("manual apply waited for optional details: %s", elapsed)
	}

	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Title != "Correct Cloud Movie" || got.ScrapeStatus != "matched" || got.TMDbID != 77 {
		t.Fatalf("manual cloud match was not saved: title=%q status=%q tmdb=%d", got.Title, got.ScrapeStatus, got.TMDbID)
	}
}

func TestEnrichOneUsesExistingTMDbIDForCloudMedia(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "OpenList · 国漫", Path: "cloud://openlist/国漫", Type: "anime", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "dirty release title",
		Path:         "cloud://openlist/国漫/间谍过家家 (2022) {tmdb-12345}/Season 1/间谍过家家.S01E01.2160p.mkv",
		SeasonNum:    1,
		EpisodeNum:   1,
		TMDbID:       12345,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}
	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "matched" || got.Title != "间谍过家家" || got.TMDbID != 12345 || got.PosterURL == "" {
		t.Fatalf("tmdb id scrape did not apply match: title=%q status=%q tmdb=%d poster=%q", got.Title, got.ScrapeStatus, got.TMDbID, got.PosterURL)
	}
}

func TestScrapeQueryCandidatesPreferSeriesFolderAndCJKTitle(t *testing.T) {
	lib := &model.Library{
		Path: `F:\downloads\国产剧`,
		Type: "movie",
	}
	media := &model.Media{
		Title:      "亏成首富从游戏开始 the ri est in game",
		Path:       `F:\downloads\国产剧\亏成首富从游戏开始 The Richest in Game\Season 01\亏成首富从游戏开始 The Richest in Game - S01E11 - 4K.mp4`,
		SeasonNum:  1,
		EpisodeNum: 11,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] != "亏成首富从游戏开始" {
		t.Fatalf("first query candidate = %q, want Chinese series title", got[0])
	}
	for _, candidate := range got {
		if strings.Contains(candidate, "ri est") {
			t.Fatalf("query candidate kept substring-stripped title: %#v", got)
		}
	}
}

func TestScrapeQueryCandidatesSkipCategoryFolderAsSeriesTitle(t *testing.T) {
	lib := &model.Library{
		Path: `/downloads`,
		Type: "tv",
	}
	media := &model.Media{
		Title:      "Ashes To Crown",
		Path:       `/downloads/国产剧/Ashes.to.Crown.S01E06.1080p.WEB-DL.mkv`,
		SeasonNum:  1,
		EpisodeNum: 6,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] == "国产剧" {
		t.Fatalf("first query candidate = %q, category folders must not be used as title candidates: %#v", got[0], got)
	}
	if !strings.EqualFold(got[0], "Ashes To Crown") {
		t.Fatalf("first query candidate = %q, want release title; all candidates=%#v", got[0], got)
	}
}

func TestEnrichOneWritesTMDbIDColumn(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}).Error; err != nil {
		t.Fatal(err)
	}

	var media model.Media
	if err := repos.DB.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "matched" || got.TMDbID != 12345 {
		t.Fatalf("unexpected scraped media: status=%q tmdb=%d", got.ScrapeStatus, got.TMDbID)
	}
}

func TestEnrichOneWritesTMDbEpisodeMetadata(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E01.mkv")
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   1,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.OriginalName != "任务代号: 猫" || got.Overview != "单集剧情" {
		t.Fatalf("episode metadata not saved: original=%q overview=%q", got.OriginalName, got.Overview)
	}
	if !strings.HasSuffix(got.BackdropURL, "/images/w500/still.jpg") || got.DurationSec != 24*60 {
		t.Fatalf("episode still/runtime not saved: backdrop=%q duration=%d", got.BackdropURL, got.DurationSec)
	}
	if got.Rating < 9.09 || got.Rating > 9.11 {
		t.Fatalf("episode rating = %v, want 9.1", got.Rating)
	}
}

func TestEnrichOneRejectsWrongYearMatchFromSeriesFolder(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/tv":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":             999,
					"name":           "Parade of Stars Auto Show",
					"first_air_date": "1952-01-01",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	log := zap.NewNop()
	scraper := NewScraperService(cfg, log, repos, NewTMDbProvider(cfg, log, nil), nil, nil, nil, NewHub(log))

	root := t.TempDir()
	mediaPath := filepath.Join(root, "Auto Show (2026)", "Season 1", "Auto Show - S01E03 - 第 3 集.mkv")
	lib := model.Library{Name: "剧集", Path: root, Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "auto show",
		Path:         mediaPath,
		SeasonNum:    1,
		EpisodeNum:   3,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "no_match" || got.Title != "auto show" || got.Year != 0 || got.TMDbID != 0 {
		t.Fatalf("wrong-year scrape should be rejected, got status=%q title=%q year=%d tmdb=%d", got.ScrapeStatus, got.Title, got.Year, got.TMDbID)
	}
}

func TestEnrichOnePrefersLocalMetadataWithoutProvider(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	scraper := NewScraperService(&config.Config{}, log, repos, nil, nil, nil, nil, NewHub(log))

	root := t.TempDir()
	showDir := filepath.Join(root, "间谍过家家")
	seasonDir := filepath.Join(showDir, "Season 02")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(`<tvshow>
<title>间谍过家家</title>
<year>2022</year>
<tmdbid>120089</tmdbid>
<genre>Animation</genre>
</tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "间谍过家家 - S02E12.mkv")
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<episodedetails>
<title>企鹅公园</title>
<showtitle>间谍过家家</showtitle>
<season>2</season>
<episode>12</episode>
<plot>本地剧情</plot>
</episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	lib := model.Library{Name: "番剧", Path: root, Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "bad title",
		Path:         mediaPath,
		ScrapeStatus: "pending",
	}
	if err := repos.DB.Create(&media).Error; err != nil {
		t.Fatal(err)
	}

	if err := scraper.EnrichOne(t.Context(), &media); err != nil {
		t.Fatal(err)
	}

	var got model.Media
	if err := repos.DB.First(&got, "id = ?", media.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ScrapeStatus != "matched" || got.Title != "间谍过家家" || got.TMDbID != 120089 {
		t.Fatalf("unexpected local scrape: status=%q title=%q tmdb=%d", got.ScrapeStatus, got.Title, got.TMDbID)
	}
	if got.SeasonNum != 2 || got.EpisodeNum != 12 || got.Overview != "本地剧情" {
		t.Fatalf("unexpected local episode data: s=%d e=%d overview=%q", got.SeasonNum, got.EpisodeNum, got.Overview)
	}
}

func TestManualEnrichLibraryRetriesNoMatchAndCountsRealMatches(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()

	lib := model.Library{Name: "番剧", Path: t.TempDir(), Type: "tv", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(lib.Path, "间谍过家家 - S02E02.mkv")
	if err := repos.DB.Create(&model.Media{
		LibraryID:    lib.ID,
		Title:        "间谍过家家",
		Path:         mediaPath,
		SeasonNum:    2,
		EpisodeNum:   2,
		ScrapeStatus: "no_match",
	}).Error; err != nil {
		t.Fatal(err)
	}

	if matched, err := scraper.EnrichLibrary(t.Context(), lib.ID); err != nil || matched != 0 {
		t.Fatalf("default EnrichLibrary matched=%d err=%v, want skipped no_match", matched, err)
	}
	if matched, err := scraper.EnrichLibrary(t.Context(), lib.ID, true); err != nil || matched != 1 {
		t.Fatalf("manual EnrichLibrary matched=%d err=%v, want one real match", matched, err)
	}
}

func TestScrapeDelayUsesSettings(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}

	if got := scraper.scrapeDelay(t.Context()); got < 250*time.Millisecond || got > 500*time.Millisecond {
		t.Fatalf("default scrapeDelay = %s, want 250-500ms", got)
	}

	if err := repos.Setting.Set(t.Context(), "scrape.delay_min_ms", "0"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "scrape.delay_max_ms", "0"); err != nil {
		t.Fatal(err)
	}
	if got := scraper.scrapeDelay(t.Context()); got != 0 {
		t.Fatalf("disabled scrapeDelay = %s, want 0", got)
	}

	if err := repos.Setting.Set(t.Context(), "scrape.delay_min_ms", "800"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "scrape.delay_max_ms", "200"); err != nil {
		t.Fatal(err)
	}
	if got := scraper.scrapeDelay(t.Context()); got != 800*time.Millisecond {
		t.Fatalf("normalized scrapeDelay = %s, want 800ms", got)
	}
}

func newTestScraper(t *testing.T) (*ScraperService, *repository.Container, func()) {
	t.Helper()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/tv"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":             12345,
					"name":           "间谍过家家",
					"overview":       "测试简介",
					"poster_path":    "/poster.jpg",
					"backdrop_path":  "/backdrop.jpg",
					"first_air_date": "2022-04-09",
					"vote_average":   8.6,
				}},
			})
		case r.URL.Path == "/tv/12345/season/2/episode/1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":         "任务代号: 猫",
				"overview":     "单集剧情",
				"still_path":   "/still.jpg",
				"air_date":     "2023-10-07",
				"vote_average": 9.1,
				"runtime":      24,
			})
		case strings.HasPrefix(r.URL.Path, "/tv/12345"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             12345,
				"name":           "间谍过家家",
				"overview":       "测试简介",
				"poster_path":    "/poster.jpg",
				"backdrop_path":  "/backdrop.jpg",
				"first_air_date": "2022-04-09",
				"vote_average":   8.6,
				"origin_country": []string{"JP"},
				"spoken_languages": []map[string]any{{
					"iso_639_1": "ja",
				}},
				"genres": []map[string]any{{
					"name": "Animation",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}); err != nil {
		upstream.Close()
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test-key"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	cfg.Secrets.TMDbImageProxy = upstream.URL + "/images"
	log := zap.NewNop()
	tmdb := NewTMDbProvider(cfg, log, nil)
	scraper := NewScraperService(cfg, log, repos, tmdb, nil, nil, nil, NewHub(log))

	return scraper, repos, upstream.Close
}

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
		case strings.HasPrefix(r.URL.Path, "/tv/12345"):
			_ = json.NewEncoder(w).Encode(map[string]any{
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

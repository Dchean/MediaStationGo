package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
		{"HNTV.Spring.Festival.Gala.FPS.HLG-QHStudio.S01E202-DD5.QHstudIo.6.4K.ts", "hntv spring festival gala", 0},
		{"Hntv Spring Festival Gala S01e (2026)", "hntv spring festival gala", 2026},
		{"Motherhood Of Taihang Aac2 Mweb - S01E01-Aac2.Mweb.mkv", "motherhood of taihang", 0},
		{"For All Mankind Atvp Hhweb - S05E06-DDP5.HHWEB.4K.mkv", "for all mankind", 0},
		{"Hntv Spring Festival Gala Fps Hlg Qhstudio S01e (2026)", "hntv spring festival gala", 2026},
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

func TestScrapeQueryCandidatesCleanDirtySeriesFolder(t *testing.T) {
	lib := &model.Library{
		Path: `F:\media\电视剧\欧美剧`,
		Type: "tv",
	}
	media := &model.Media{
		Title:      "motherhood of taihang aac2 mweb aac2 mweb",
		Path:       `F:\media\电视剧\欧美剧\Motherhood Of Taihang Aac2 Mweb\Season 1\Motherhood Of Taihang Aac2 Mweb - S01E01-Aac2.Mweb - 第 1 集.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] != "motherhood of taihang" {
		t.Fatalf("first query candidate = %q, want cleaned series title; all candidates=%#v", got[0], got)
	}
	for _, candidate := range got {
		lower := strings.ToLower(candidate)
		if strings.Contains(lower, "mweb") || strings.Contains(lower, "aac2") {
			t.Fatalf("query candidate kept release noise %q: %#v", candidate, got)
		}
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

func TestPathHintMetadataIgnoresEpisodeFileIDForSeries(t *testing.T) {
	meta, hints := pathHintMetadata("cloud://openlist/国漫/遮天 (2023)/Season 1/遮天.S01E01.{tmdb-4375419}.mkv", true)
	if meta == nil || meta.Title != "遮天" || meta.Year != 2023 {
		t.Fatalf("series path hint metadata = %+v", meta)
	}
	if hints.TMDbID != 0 || meta.TMDbID != 0 {
		t.Fatalf("episode filename tmdb id must not become series id: meta=%+v hints=%+v", meta, hints)
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

func TestEnrichOneRejectsStaleEpisodeTMDbIDBySeriesTitle(t *testing.T) {
	var requested []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/tv/220269":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             220269,
				"name":           "错误剧集",
				"overview":       "stale episode id resolved as another show",
				"first_air_date": "2024-01-01",
			})
		case "/search/tv":
			if r.URL.Query().Get("query") != "折腰" {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{
					"id":             296753,
					"name":           "折腰",
					"overview":       "正确的剧集条目",
					"poster_path":    "/zheyao.jpg",
					"first_air_date": "2025-05-13",
					"origin_country": []string{"CN"},
				}},
			})
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
		Path:         "cloud://openlist/国产剧/折腰 (2025)/Season 1/折腰.S01E01.mkv",
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
		t.Fatalf("stale tmdb id should be rejected and repaired by title search: status=%q tmdb=%d title=%q poster=%q requests=%v",
			got.ScrapeStatus, got.TMDbID, got.Title, got.PosterURL, requested)
	}
	if firstIndexFunc(requested, func(path string) bool { return path == "/tv/220269" }) < 0 {
		t.Fatalf("test did not exercise stale id lookup: requests=%v", requested)
	}
	if firstIndexFunc(requested, func(path string) bool { return path == "/search/tv" }) < 0 {
		t.Fatalf("scraper did not fall back to title search after stale id rejection: requests=%v", requested)
	}
}

func TestMediaYearHintUsesSeriesFolderYearForEpisodes(t *testing.T) {
	media := &model.Media{
		Year:       2026,
		Path:       "cloud://openlist/国产剧/折腰 (2025)/Season 1/折腰.S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
	}
	if got := mediaYearHint(media); got != 2025 {
		t.Fatalf("mediaYearHint = %d, want series folder year 2025", got)
	}
}

func TestMediaYearHintIgnoresEpisodeRowYearWithoutSeriesFolderYear(t *testing.T) {
	media := &model.Media{
		Year:       2026,
		Path:       "cloud://openlist/综艺/哈哈哈哈哈/Season 6/哈哈哈哈哈 - S06E01.mkv",
		SeasonNum:  6,
		EpisodeNum: 1,
	}
	if got := mediaYearHint(media); got != 0 {
		t.Fatalf("mediaYearHint = %d, want no year when only episode row year is available", got)
	}
}

func TestEnrichOneUsesLocalPathExternalIDHints(t *testing.T) {
	var requested []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/movie/27205":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             27205,
				"title":          "Inception",
				"overview":       "A thief enters dreams.",
				"poster_path":    "/inception.jpg",
				"release_date":   "2010-07-16",
				"vote_average":   8.4,
				"original_title": "Inception",
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
	mediaPath := filepath.Join(root, "错误标题 (2010) {tmdb-27205}", "bad-file-name.mkv")
	lib := model.Library{Name: "电影", Path: root, Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "bad local title",
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
	if got.ScrapeStatus != "matched" || got.TMDbID != 27205 || got.Title != "Inception" {
		t.Fatalf("local path tmdb hint was not used: status=%q tmdb=%d title=%q requests=%v", got.ScrapeStatus, got.TMDbID, got.Title, requested)
	}
	if len(requested) == 0 || requested[0] != "/movie/27205" {
		t.Fatalf("scraper should query by hinted tmdb id first, requests=%v", requested)
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

func TestScrapeQueryCandidatesUseCloudSeriesFolder(t *testing.T) {
	lib := &model.Library{
		Path: "cloud://openlist/国产剧",
		Type: "movie",
	}
	media := &model.Media{
		Title:      "折腰 S01E01",
		Path:       "cloud://openlist/国产剧/折腰 (2025)/Season 1/折腰.S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] != "折腰" {
		t.Fatalf("first query candidate = %q, want cloud series folder title; all candidates=%#v", got[0], got)
	}
}

func TestScrapeQueryCandidatesUseSeriesLibraryRootWhenMountedAtShowFolder(t *testing.T) {
	lib := &model.Library{
		Path: `/downloads/国产剧/折腰 (2025)`,
		Type: "tv",
	}
	media := &model.Media{
		Title:      "第 1 集",
		Path:       `/downloads/国产剧/折腰 (2025)/Season 01/第01集.mkv`,
		SeasonNum:  1,
		EpisodeNum: 1,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] != "折腰" {
		t.Fatalf("first query candidate = %q, want library root show title; all candidates=%#v", got[0], got)
	}
}

func TestScrapeQueryCandidatesSkipEpisodeOnlyTitles(t *testing.T) {
	lib := &model.Library{
		Path: `F:\media\电视剧\欧美剧`,
		Type: "tv",
	}
	media := &model.Media{
		Title:      "第1期上：最狠开局！五哈团命悬一线好刺激",
		Path:       `F:\media\电视剧\欧美剧\第1期上：最狠开局！五哈团命悬一线好刺激 (2026)\Season 6\第1期上：最狠开局！五哈团命悬一线好刺激 - S06E01 - 第 1 集.mkv`,
		SeasonNum:  6,
		EpisodeNum: 1,
	}

	got := scrapeQueryCandidates(media, lib)
	for _, candidate := range got {
		if unsafeAutomaticEpisodeQuery(candidate) {
			t.Fatalf("query candidates kept unsafe episode title %q: %#v", candidate, got)
		}
	}
}

func TestOrganizeMetadataRejectsEpisodeOnlyQuery(t *testing.T) {
	match := &Match{Title: "错误节目", Year: 2026, TMDbID: 284725}
	for _, query := range []string{"第 11 集", "第1期上：最狠开局！五哈团命悬一线好刺激"} {
		if organizeMetadataMatchTrusted(query, 2026, match) {
			t.Fatalf("episode-only query %q must not be trusted for automatic match", query)
		}
	}
}

func TestOrganizeMetadataTrustsExactOriginalTitle(t *testing.T) {
	match := &Match{Title: "镖人", OriginalName: "Blades of the Guardians", Year: 2023, TMDbID: 107463}
	if !organizeMetadataMatchTrusted("blades of the guardians", 2023, match) {
		t.Fatal("exact original title should be trusted")
	}
}

func TestOrganizeMetadataTrustsCleanedBroadcastReleaseQuery(t *testing.T) {
	match := &Match{Title: "湖南卫视春节联欢晚会", OriginalName: "HNTV Spring Festival Gala", Year: 2026, TMDbID: 123456}
	query := "hntv spring festival gala fps hlg qhstudio s01e qhstudio"
	if !organizeMetadataMatchTrusted(query, 2026, match) {
		t.Fatal("release noise should not block an otherwise matching title")
	}
}

func TestOrganizeMetadataRejectsLooseSingleTokenQuery(t *testing.T) {
	match := &Match{Title: "步步惊心泰版", OriginalName: "Scarlet Heart Thailand", Year: 2025, TMDbID: 252886}
	if organizeMetadataMatchTrusted("thailand", 0, match) {
		t.Fatal("single generic token must not trust a broader title")
	}
}

func TestOrganizeMetadataRejectsDirtyUnrelatedQuery(t *testing.T) {
	match := &Match{Title: "春节联欢晚会", OriginalName: "Spring Festival Gala", Year: 2026, TMDbID: 284725}
	query := "hntv spring festival gala fps hlg qhstudio s01e qhstudio"
	if organizeMetadataMatchTrusted(query, 2026, match) {
		t.Fatal("dirty query with a missing broadcaster token must not trust a partial title")
	}
}

func TestSeriesTitleFromMediaPathIgnoresEpisodeOnlyFolder(t *testing.T) {
	got := seriesTitleFromMediaPath(`F:\media\电视剧\欧美剧\第 11 集 (2026)\Season 6\第 11 集 - S06E11.mkv`)
	if got != "" {
		t.Fatalf("series title from episode-only folder = %q, want empty", got)
	}
}

func TestMediaIsEpisodicUsesEpisodePatternInPath(t *testing.T) {
	lib := &model.Library{
		Path: `/media/movies`,
		Type: "movie",
	}
	media := &model.Media{
		Title: "折腰 S01E01",
		Path:  `/media/movies/折腰/Season 01/折腰.S01E01.mkv`,
	}

	if !mediaIsEpisodic(media, lib) {
		t.Fatal("media with an SxxEyy path should be treated as episodic even in a movie library")
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

func TestScrapeQueryCandidatesUseMovieFolderForGenericFilename(t *testing.T) {
	lib := &model.Library{
		Path: `/media/movies`,
		Type: "movie",
	}
	media := &model.Media{
		Title: "00000",
		Path:  `/media/movies/Inception (2010)/BDMV/STREAM/00000.m2ts`,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] != "inception" {
		t.Fatalf("first query candidate = %q, want movie folder title; all candidates=%#v", got[0], got)
	}
	for _, candidate := range got {
		switch strings.ToLower(candidate) {
		case "bdmv", "stream":
			t.Fatalf("query candidates kept technical filename/folder: %#v", got)
		}
	}
}

func TestScrapeQueryCandidatesUseMovieLibraryRootWhenMountedAtMovieFolder(t *testing.T) {
	lib := &model.Library{
		Path: `/media/movies/Inception (2010)`,
		Type: "movie",
	}
	media := &model.Media{
		Title: "00000",
		Path:  `/media/movies/Inception (2010)/BDMV/STREAM/00000.m2ts`,
	}

	got := scrapeQueryCandidates(media, lib)
	if len(got) == 0 {
		t.Fatal("scrapeQueryCandidates returned no candidates")
	}
	if got[0] != "inception" {
		t.Fatalf("first query candidate = %q, want movie library root title; all candidates=%#v", got[0], got)
	}
}

func TestEnrichOneUsesMovieFolderWhenFilenameIsGeneric(t *testing.T) {
	var queries []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search/movie" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("query") != "inception" {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{
				"id":             27205,
				"title":          "Inception",
				"overview":       "A thief enters dreams.",
				"poster_path":    "/inception.jpg",
				"release_date":   "2010-07-16",
				"vote_average":   8.4,
				"original_title": "Inception",
			}},
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

	lib := model.Library{Name: "Movies", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := repos.DB.Create(&lib).Error; err != nil {
		t.Fatal(err)
	}
	media := model.Media{
		LibraryID:    lib.ID,
		Title:        "00000",
		Path:         `/media/movies/Inception (2010)/BDMV/STREAM/00000.m2ts`,
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
	if got.ScrapeStatus != "matched" || got.TMDbID != 27205 || got.Title != "Inception" {
		t.Fatalf("generic filename scrape did not use folder title: status=%q tmdb=%d title=%q queries=%v", got.ScrapeStatus, got.TMDbID, got.Title, queries)
	}
	if len(queries) == 0 || queries[0] != "inception" {
		t.Fatalf("first tmdb query = %q, want folder title; all queries=%v", firstQuery(queries), queries)
	}
}

package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSelectSiteSearchCandidatesPrefersSeriesPack(t *testing.T) {
	sub := &model.Subscription{Name: "间谍过家家 自动订阅", Filter: "间谍过家家 2022", MediaType: "tv"}
	results := []SearchResult{
		{Title: "间谍过家家 S01E01 1080p", DownloadURL: "https://pt/download/1", Seeders: 80},
		{Title: "间谍过家家 S01 Complete 1080p", DownloadURL: "https://pt/download/pack", Seeders: 50},
		{Title: "间谍过家家 S01E02 1080p", DownloadURL: "https://pt/download/2", Seeders: 70},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{})
	if len(got) != 1 {
		t.Fatalf("selected %d candidates, want 1", len(got))
	}
	if got[0].Download != "https://pt/download/pack" || !got[0].Pack {
		t.Fatalf("selected %#v, want complete pack", got[0])
	}
}

func TestSelectSiteSearchCandidatesQueuesDistinctEpisodesWhenNoPack(t *testing.T) {
	sub := &model.Subscription{Name: "葬送的芙莉莲 自动订阅", Filter: "葬送的芙莉莲", MediaType: "anime", WashEnabled: true, WashPriority: "resolution"}
	results := []SearchResult{
		{Title: "葬送的芙莉莲 S01E01 1080p", DownloadURL: "https://pt/download/1a", Seeders: 90},
		{Title: "葬送的芙莉莲 S01E01 2160p", DownloadURL: "https://pt/download/1b", Seeders: 80},
		{Title: "葬送的芙莉莲 S01E02 1080p", DownloadURL: "https://pt/download/2", Seeders: 70},
		{Title: "葬送的芙莉莲 S01E03 1080p", DownloadURL: "https://pt/download/3", Seeders: 60},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{})
	if len(got) != 3 {
		t.Fatalf("selected %d candidates, want 3", len(got))
	}
	if got[0].Episode != 1 || got[1].Episode != 2 || got[2].Episode != 3 {
		t.Fatalf("episodes = %d,%d,%d; want 1,2,3", got[0].Episode, got[1].Episode, got[2].Episode)
	}
	if got[0].Download != "https://pt/download/1b" {
		t.Fatalf("duplicate episode should keep wash-priority best result, got %q", got[0].Download)
	}
}

func TestSelectSiteSearchCandidatesKeepsMovieSingleBest(t *testing.T) {
	sub := &model.Subscription{Name: "Inception 自动订阅", Filter: "Inception 2010", MediaType: "movie", WashPriority: "seeders"}
	results := []SearchResult{
		{Title: "Inception 2010 1080p", DownloadURL: "https://pt/download/1080", Seeders: 90},
		{Title: "Inception 2010 2160p", DownloadURL: "https://pt/download/2160", Seeders: 80},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{})
	if len(got) != 1 || got[0].Download != "https://pt/download/1080" {
		t.Fatalf("selected %#v, want movie best only", got)
	}
}

func TestSelectSiteSearchCandidatesDoesNotWashByDefault(t *testing.T) {
	sub := &model.Subscription{Name: "Inception 自动订阅", Filter: "Inception 2010", MediaType: "movie", WashPriority: "resolution"}
	results := []SearchResult{
		{Title: "Inception 2010 1080p", DownloadURL: "https://pt/download/1080", Seeders: 90},
		{Title: "Inception 2010 2160p", DownloadURL: "https://pt/download/2160", Seeders: 80},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{})
	if len(got) != 1 || got[0].Download != "https://pt/download/1080" {
		t.Fatalf("selected %#v, want seeders best when wash disabled", got)
	}
}

func TestSelectSiteSearchCandidatesAppliesQualityRules(t *testing.T) {
	sub := &model.Subscription{
		Name:         "Dune 自动订阅",
		Filter:       "Dune 2021",
		MediaType:    "movie",
		Resolution:   "2160p",
		Quality:      "remux",
		Effects:      "hdr",
		ExcludeWords: "cam,ts",
	}
	results := []SearchResult{
		{Title: "Dune 2021 2160p WEB-DL HDR", DownloadURL: "https://pt/download/web", Seeders: 100},
		{Title: "Dune 2021 2160p UHD BluRay REMUX HDR", DownloadURL: "https://pt/download/remux", Seeders: 30},
		{Title: "Dune 2021 2160p REMUX HDR CAM", DownloadURL: "https://pt/download/cam", Seeders: 200},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{})
	if len(got) != 1 || got[0].Download != "https://pt/download/remux" {
		t.Fatalf("selected %#v, want filtered remux", got)
	}
}

func TestSiteSearchKeywordCanUseIMDB(t *testing.T) {
	sub := &model.Subscription{Name: "沙丘 自动订阅", Filter: "Dune 2021", SearchMode: "imdb", IMDBID: "tt1160419"}
	if got := siteSearchKeyword(sub); got != "tt1160419" {
		t.Fatalf("keyword = %q, want imdb id", got)
	}
}

func TestStableSiteSearchGUIDIgnoresPrivateTokenChanges(t *testing.T) {
	item := SearchResult{
		SiteID:   "mteam",
		Title:    "Some Show S01E01 1080p",
		Category: "TV",
		Size:     1024,
	}
	first := stableSiteSearchGUID(item, "https://pt.example/download?id=123&passkey=old")
	second := stableSiteSearchGUID(item, "https://pt.example/download?id=123&passkey=new")
	if first != second {
		t.Fatalf("stableSiteSearchGUID changed with token: %q != %q", first, second)
	}
	if strings.Contains(first, "passkey") || strings.Contains(first, "old") || strings.Contains(first, "new") {
		t.Fatalf("stableSiteSearchGUID leaked private token: %q", first)
	}
}

func TestDeleteSubscriptionRemovesDownloaderTaskAndSeenState(t *testing.T) {
	const title = "Delete Subscription Show S01E01 1080p"
	const hash = "abcdef1234567890abcdef1234567890abcdef12"
	var deleteCalls atomic.Int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[{"hash":"` + hash + `","name":"` + title + `","state":"downloading","progress":0.2}]`))
		case "/api/v2/torrents/delete":
			deleteCalls.Add(1)
			if got := r.FormValue("deleteFiles"); got != "false" {
				t.Fatalf("deleteFiles = %q, want false", got)
			}
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	if err := downloads.ReloadConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, downloads, nil, NewHub(zap.NewNop()))
	sub := &model.Subscription{Name: "Delete Subscription Show 自动订阅", Filter: "Delete Subscription Show", FeedURL: "https://rss.example/feed", UserID: "u1", SavePath: "/downloads/tv"}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	task := &model.DownloadTask{
		UserID:         "u1",
		SubscriptionID: sub.ID,
		Source:         "qbittorrent",
		URL:            "https://pt.example/download?id=1",
		Title:          title,
		SavePath:       "/downloads/tv",
		Status:         "downloading",
		Progress:       0.2,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "subscription."+sub.ID+".seen", "guid-1"); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(t.Context(), sub.ID); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}
	if got := deleteCalls.Load(); got != 1 {
		t.Fatalf("qb delete calls = %d, want 1", got)
	}
	var updated model.DownloadTask
	if err := db.Where("id = ?", task.ID).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != "deleted" {
		t.Fatalf("download task status = %q, want deleted", updated.Status)
	}
	seen, err := repos.Setting.Get(t.Context(), "subscription."+sub.ID+".seen")
	if err != nil {
		t.Fatal(err)
	}
	if seen != "" {
		t.Fatalf("seen state = %q, want cleared", seen)
	}
	var count int64
	if err := db.Model(&model.Subscription{}).Where("id = ?", sub.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("active subscription count = %d, want 0", count)
	}
}

func TestDeletedDownloadTaskDoesNotBlockSubscriptionReadd(t *testing.T) {
	if downloadTaskBlocksReadd("deleted") {
		t.Fatal("deleted download task must not block subscription re-add")
	}
	if downloadTaskBlocksReadd("removed") {
		t.Fatal("removed download task must not block subscription re-add")
	}
}

func TestSelectSiteSearchCandidatesOnlyQueuesMissingLocalEpisodes(t *testing.T) {
	sub := &model.Subscription{Name: "间谍过家家 自动订阅", Filter: "间谍过家家", MediaType: "tv", TotalEpisodes: 3}
	results := []SearchResult{
		{Title: "间谍过家家 S01 Complete 1080p", DownloadURL: "https://pt/download/pack", Seeders: 100},
		{Title: "间谍过家家 S01E01 1080p", DownloadURL: "https://pt/download/1", Seeders: 90},
		{Title: "间谍过家家 S01E02 1080p", DownloadURL: "https://pt/download/2", Seeders: 80},
		{Title: "间谍过家家 S01E03 1080p", DownloadURL: "https://pt/download/3", Seeders: 70},
	}
	availability := LocalAvailability{
		TotalEpisodes:       3,
		LocalMediaCount:     2,
		MissingEpisodes:     []int{3},
		ExistingEpisodeKeys: map[string]struct{}{episodeKey(1, 1): {}, episodeKey(1, 2): {}},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 1 || got[0].Episode != 3 {
		t.Fatalf("selected %#v, want only missing episode 3", got)
	}
}

func TestSelectSiteSearchCandidatesWithUnknownTotalSkipsExistingEpisodes(t *testing.T) {
	sub := &model.Subscription{Name: "葬送的芙莉莲 自动订阅", Filter: "葬送的芙莉莲", MediaType: "anime"}
	results := []SearchResult{
		{Title: "葬送的芙莉莲 S01 Complete 1080p", DownloadURL: "https://pt/download/pack", Seeders: 100},
		{Title: "葬送的芙莉莲 S01E01 1080p", DownloadURL: "https://pt/download/1", Seeders: 90},
		{Title: "葬送的芙莉莲 S01E02 1080p", DownloadURL: "https://pt/download/2", Seeders: 80},
		{Title: "葬送的芙莉莲 S01E03 1080p", DownloadURL: "https://pt/download/3", Seeders: 70},
	}
	availability := LocalAvailability{
		LocalMediaCount:     2,
		ExistingEpisodeKeys: map[string]struct{}{episodeKey(1, 1): {}, episodeKey(1, 2): {}},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 1 || got[0].Episode != 3 {
		t.Fatalf("selected %#v, want only not-yet-local episode 3", got)
	}
}

func TestSelectSiteSearchCandidatesSingleExistingEpisodeIsSkipped(t *testing.T) {
	sub := &model.Subscription{Name: "葬送的芙莉莲 自动订阅", Filter: "葬送的芙莉莲", MediaType: "anime", TotalEpisodes: 3}
	results := []SearchResult{
		{Title: "葬送的芙莉莲 S01E01 1080p", DownloadURL: "https://pt/download/1", Seeders: 90},
	}
	availability := LocalAvailability{
		TotalEpisodes:       3,
		LocalMediaCount:     1,
		MissingEpisodes:     []int{2, 3},
		ExistingEpisodeKeys: map[string]struct{}{episodeKey(1, 1): {}},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 0 {
		t.Fatalf("selected %#v, want none because E01 already exists", got)
	}
}

func TestSelectSiteSearchCandidatesSinglePackIsSkippedWhenLibraryPartiallyExists(t *testing.T) {
	sub := &model.Subscription{Name: "间谍过家家 自动订阅", Filter: "间谍过家家", MediaType: "tv", TotalEpisodes: 3}
	results := []SearchResult{
		{Title: "间谍过家家 S01 Complete 1080p", DownloadURL: "https://pt/download/pack", Seeders: 100},
	}
	availability := LocalAvailability{
		TotalEpisodes:       3,
		LocalMediaCount:     2,
		MissingEpisodes:     []int{3},
		ExistingEpisodeKeys: map[string]struct{}{episodeKey(1, 1): {}, episodeKey(1, 2): {}},
	}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 0 {
		t.Fatalf("selected %#v, want none because a full pack would redownload existing episodes", got)
	}
}

func TestSelectSiteSearchCandidatesSingleExistingMovieIsSkippedWhenNotWashing(t *testing.T) {
	sub := &model.Subscription{Name: "Inception 自动订阅", Filter: "Inception 2010", MediaType: "movie"}
	results := []SearchResult{
		{Title: "Inception 2010 1080p WEB-DL", DownloadURL: "https://pt/download/web", Seeders: 90},
	}
	availability := LocalAvailability{LocalMediaCount: 1, InLibrary: true, DownloadedEpisodes: 1, TotalEpisodes: 1}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 0 {
		t.Fatalf("selected %#v, want none because movie already exists and wash is disabled", got)
	}
}

func TestSubscriptionPendingDownloadAvailabilitySkipsUnorganizedEpisodes(t *testing.T) {
	root := t.TempDir()
	seasonDir := filepath.Join(root, "间谍过家家", "Season 01")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"间谍过家家 - S01E01.mkv",
		"间谍过家家 - S01E02.mkv.!qB",
	} {
		if err := os.WriteFile(filepath.Join(seasonDir, name), []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sub := &model.Subscription{
		Name:          "间谍过家家 自动订阅",
		Filter:        "间谍过家家",
		MediaType:     "tv",
		SavePath:      root,
		TotalEpisodes: 3,
	}
	svc := NewSubscriptionService(nil, nil, nil, nil, nil, nil)
	availability := svc.pendingDownloadAvailability(t.Context(), sub)
	if availability.DownloadedEpisodes != 2 {
		t.Fatalf("downloaded episodes = %d, want 2", availability.DownloadedEpisodes)
	}
	if _, ok := availability.ExistingEpisodeKeys[episodeKey(1, 1)]; !ok {
		t.Fatalf("missing pending E01 key: %#v", availability.ExistingEpisodeKeys)
	}
	if _, ok := availability.ExistingEpisodeKeys[episodeKey(1, 2)]; !ok {
		t.Fatalf("missing pending E02 key: %#v", availability.ExistingEpisodeKeys)
	}

	results := []SearchResult{
		{Title: "间谍过家家 S01 Complete 1080p", DownloadURL: "https://pt/download/pack", Seeders: 100},
		{Title: "间谍过家家 S01E01 1080p", DownloadURL: "https://pt/download/1", Seeders: 90},
		{Title: "间谍过家家 S01E02 1080p", DownloadURL: "https://pt/download/2", Seeders: 80},
		{Title: "间谍过家家 S01E03 1080p", DownloadURL: "https://pt/download/3", Seeders: 70},
	}
	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 1 || got[0].Episode != 3 {
		t.Fatalf("selected %#v, want only not-yet-downloaded episode 3", got)
	}
	if !svc.downloadPathHasCandidate(t.Context(), sub, "间谍过家家 S01E02 1080p", root) {
		t.Fatal("expected existing pending E02 file to be detected")
	}
	if svc.downloadPathHasCandidate(t.Context(), sub, "间谍过家家 S01E03 1080p", root) {
		t.Fatal("did not expect missing E03 to be detected")
	}
}

func TestSubscriptionPendingDownloadAvailabilityIncludesQueuedTasks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Download.Create(t.Context(), &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:2222222222222222222222222222222222222222",
		Title:    "间谍过家家 S01E02 1080p",
		SavePath: "/downloads/tv",
		Status:   "queued",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewSubscriptionService(nil, nil, repos, nil, nil, nil)
	sub := &model.Subscription{
		Name:          "间谍过家家 自动订阅",
		Filter:        "间谍过家家",
		MediaType:     "tv",
		SavePath:      "/downloads/tv",
		TotalEpisodes: 3,
	}

	availability := svc.pendingDownloadAvailability(t.Context(), sub)
	if availability.DownloadedEpisodes != 1 {
		t.Fatalf("downloaded episodes = %d, want 1", availability.DownloadedEpisodes)
	}
	if availability.InLibrary {
		t.Fatal("queued download should not be reported as already in library")
	}
	if _, ok := availability.ExistingEpisodeKeys[episodeKey(1, 2)]; !ok {
		t.Fatalf("missing queued E02 key: %#v", availability.ExistingEpisodeKeys)
	}

	results := []SearchResult{
		{Title: "间谍过家家 S01E02 1080p WEB-DL", DownloadURL: "https://pt/download/2", Seeders: 80},
		{Title: "间谍过家家 S01E03 1080p WEB-DL", DownloadURL: "https://pt/download/3", Seeders: 70},
	}
	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 1 || got[0].Episode != 3 {
		t.Fatalf("selected %#v, want only not-yet-downloaded episode 3", got)
	}
}

func TestSubscriptionEnrichProgressIncludesPendingDownloads(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadTask{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Download.Create(t.Context(), &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:4444444444444444444444444444444444444444",
		Title:    "Inception 2010 1080p",
		SavePath: "/downloads/movies",
		Status:   "completed",
		Progress: 1,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewSubscriptionService(nil, nil, repos, nil, nil, nil)
	items := []model.Subscription{{
		Name:      "Inception 2010",
		Filter:    "Inception 2010",
		MediaType: "movie",
		SavePath:  "/downloads/movies",
	}}

	svc.EnrichProgress(t.Context(), items)
	if items[0].InLibrary {
		t.Fatal("pending download should not be reported as in-library media")
	}
	if items[0].DownloadedEpisodes != 1 || items[0].LocalMediaCount != 1 || items[0].TotalEpisodes != 1 {
		t.Fatalf("unexpected enriched progress: %+v", items[0])
	}
}

func TestSubscriptionLocalAvailabilityMatchesMediaPath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := db.Create(&model.Media{
		Title:      "Scraped English Title",
		Path:       "/media/电视剧/国产剧/凡人修仙传/Season 01/凡人修仙传 - S01E146.mkv",
		SeasonNum:  1,
		EpisodeNum: 146,
	}).Error; err != nil {
		t.Fatal(err)
	}
	sub := &model.Subscription{
		Name:          "凡人修仙传 年番",
		Filter:        "凡人修仙传",
		MediaType:     "tv",
		TotalEpisodes: 146,
	}

	availability := SubscriptionLocalAvailability(t.Context(), repos, sub)
	if _, ok := availability.ExistingEpisodeKeys[episodeKey(1, 146)]; !ok {
		t.Fatalf("missing path-matched E146 key: %#v", availability.ExistingEpisodeKeys)
	}
	results := []SearchResult{
		{Title: "凡人修仙传 年番 - 146 1080p", DownloadURL: "https://pt/download/146", Seeders: 80},
	}
	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 0 {
		t.Fatalf("selected %#v, want none because path-matched local episode exists", got)
	}
}

func TestSubscriptionPendingDownloadAvailabilityIgnoresDeletedTasks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	if err := repos.Download.Create(t.Context(), &model.DownloadTask{
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:3333333333333333333333333333333333333333",
		Title:    "间谍过家家 S01E02 1080p",
		SavePath: "/downloads/tv",
		Status:   "deleted",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewSubscriptionService(nil, nil, repos, nil, nil, nil)
	sub := &model.Subscription{
		Name:          "间谍过家家 自动订阅",
		Filter:        "间谍过家家",
		MediaType:     "tv",
		SavePath:      "/downloads/tv",
		TotalEpisodes: 3,
	}

	availability := svc.pendingDownloadAvailability(t.Context(), sub)
	if _, ok := availability.ExistingEpisodeKeys[episodeKey(1, 2)]; ok {
		t.Fatalf("deleted E02 task should not count as available: %#v", availability.ExistingEpisodeKeys)
	}
	results := []SearchResult{
		{Title: "间谍过家家 S01E02 1080p WEB-DL", DownloadURL: "https://pt/download/2", Seeders: 80},
		{Title: "间谍过家家 S01E03 1080p WEB-DL", DownloadURL: "https://pt/download/3", Seeders: 70},
	}
	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 2 || got[0].Episode != 2 || got[1].Episode != 3 {
		t.Fatalf("selected %#v, want deleted episode 2 and new episode 3", got)
	}
}

func TestSubscriptionPendingDownloadAvailabilityIncludesLiveQBTorrents(t *testing.T) {
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[{"hash":"abc123","name":"间谍过家家 S01E01 1080p","state":"downloading","progress":0.2}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.DownloadTask{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	downloads.qb.Configure(QBitConfig{BaseURL: qb.URL, Username: "admin", Password: "admin"})
	svc := NewSubscriptionService(nil, nil, repos, downloads, nil, nil)
	sub := &model.Subscription{
		Name:          "间谍过家家 自动订阅",
		Filter:        "间谍过家家",
		MediaType:     "tv",
		SavePath:      "/downloads/tv",
		TotalEpisodes: 2,
	}

	availability := svc.pendingDownloadAvailability(t.Context(), sub)
	if availability.DownloadedEpisodes != 1 {
		t.Fatalf("downloaded episodes = %d, want 1", availability.DownloadedEpisodes)
	}
	if _, ok := availability.ExistingEpisodeKeys[episodeKey(1, 1)]; !ok {
		t.Fatalf("missing live qB E01 key: %#v", availability.ExistingEpisodeKeys)
	}
}

func TestSubscriptionRunOneArchivesCompletedMovieRSS(t *testing.T) {
	rss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss><channel>
  <item>
    <title>Dune 2021 1080p WEB-DL</title>
    <guid>dune-1080-web</guid>
    <link>magnet:?xt=urn:btih:dddddddddddddddddddddddddddddddddddddddd&amp;dn=Dune+2021+1080p+WEB-DL</link>
  </item>
</channel></rss>`))
	}))
	defer rss.Close()

	var addCalls int32
	var added bool
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if added {
				_, _ = w.Write([]byte(`[{"hash":"dunehash","name":"Dune 2021 1080p WEB-DL","state":"downloading","progress":0.1}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			added = true
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}, &model.DownloadClient{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, downloads, nil, NewHub(zap.NewNop()))

	sub := &model.Subscription{
		Name:      "Dune 自动订阅",
		FeedURL:   rss.URL,
		Filter:    "Dune 2021",
		MediaType: "movie",
		SavePath:  "/downloads/movies",
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.runOne(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
	active, err := repos.Subscription.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active subscriptions = %d, want 0 after completion", len(active))
	}
	history, err := repos.Subscription.History(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ArchivedAt == nil {
		t.Fatalf("history = %#v, want one archived subscription", history)
	}
}

func TestSubscriptionArchiveCompletedSingleEpisodeTV(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, nil, nil, NewHub(zap.NewNop()))
	sub := &model.Subscription{
		Name:      "Some Show S01E01 自动订阅",
		FeedURL:   "site-search://search?keyword=Some%20Show%20S01E01",
		Filter:    "Some Show S01E01",
		MediaType: "tv",
		Enabled:   true,
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}

	err = svc.archiveCompletedSubscription(t.Context(), sub, LocalAvailability{
		DownloadedEpisodes: 1,
		LocalMediaCount:    1,
		InLibrary:          true,
		ExistingEpisodeKeys: map[string]struct{}{
			episodeKey(1, 1): {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := repos.Subscription.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active subscriptions = %d, want 0", len(active))
	}
	history, err := repos.Subscription.History(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ArchiveReason == "" {
		t.Fatalf("history = %#v, want archived single episode", history)
	}
}

func TestSubscriptionArchiveKeepsGenericUnknownTotalSeriesActive(t *testing.T) {
	sub := &model.Subscription{
		Name:      "Some Show 自动订阅",
		Filter:    "Some Show",
		MediaType: "tv",
	}
	availability := LocalAvailability{
		DownloadedEpisodes: 1,
		LocalMediaCount:    1,
		InLibrary:          true,
		ExistingEpisodeKeys: map[string]struct{}{
			episodeKey(1, 1): {},
		},
	}
	if subscriptionShouldArchive(sub, availability) {
		t.Fatal("generic series with unknown total should stay active for incremental episodes")
	}
}

func TestInferSubscriptionTotalEpisodesFromSearchAndRSS(t *testing.T) {
	sub := &model.Subscription{Name: "Some Show 自动订阅", Filter: "Some Show", MediaType: "tv"}
	results := []SearchResult{
		{Title: "Some Show S01E01 1080p"},
		{Title: "Some Show S01E12 1080p"},
		{Title: "Other Show S01E99 1080p"},
	}
	if got := inferSearchTotalEpisodes(results, sub); got != 12 {
		t.Fatalf("search inferred total = %d, want 12", got)
	}
	items := []rssItem{
		{Title: "Some Show S01E02 WEB-DL"},
		{Title: "Some Show S01E10 WEB-DL"},
	}
	if got := inferRSSTotalEpisodes(items, sub, compileFilter("Some Show")); got != 10 {
		t.Fatalf("rss inferred total = %d, want 10", got)
	}
}

func TestResolveSubscriptionTotalEpisodesPrefersTMDbOverTitleFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":42,"name":"Some Show","first_air_date":"2026-01-01"}]}`))
		case "/tv/42":
			_, _ = w.Write([]byte(`{"number_of_episodes":13}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cfg := &config.Config{}
	cfg.Secrets.TMDbAPIKey = "test"
	cfg.Secrets.TMDbAPIProxy = upstream.URL
	tmdb := NewTMDbProvider(cfg, zap.NewNop(), nil)
	svc := NewSubscriptionService(cfg, zap.NewNop(), nil, nil, nil, NewHub(zap.NewNop()))
	svc.SetScraper(NewScraperService(cfg, zap.NewNop(), nil, tmdb, nil, nil, nil, NewHub(zap.NewNop())))

	sub := &model.Subscription{Name: "Some Show 自动订阅", Filter: "Some Show", MediaType: "tv"}
	if got := svc.resolveSubscriptionTotalEpisodes(t.Context(), sub, 10); got != 13 {
		t.Fatalf("resolved total = %d, want TMDb total 13", got)
	}
}

func TestSubscriptionArchiveKeepsWashSubscriptionActive(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, nil, nil, NewHub(zap.NewNop()))
	sub := &model.Subscription{
		Name:        "Dune 自动订阅",
		FeedURL:     "site-search://search?keyword=Dune",
		Filter:      "Dune 2021",
		MediaType:   "movie",
		WashEnabled: true,
		Enabled:     true,
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}

	err = svc.archiveCompletedSubscription(t.Context(), sub, LocalAvailability{
		DownloadedEpisodes: 1,
		LocalMediaCount:    1,
		InLibrary:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := repos.Subscription.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active subscriptions = %d, want wash subscription to stay active", len(active))
	}
	history, err := repos.Subscription.History(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("history subscriptions = %d, want 0", len(history))
	}
}

func TestSubscriptionRunOneDeduplicatesDuplicateRSSGUIDInSameFeed(t *testing.T) {
	rss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss><channel>
  <item>
    <title>Some Show S01E01 1080p</title>
    <guid>episode-1</guid>
    <link>magnet:?xt=urn:btih:1111111111111111111111111111111111111111&amp;dn=Some+Show+S01E01</link>
  </item>
  <item>
    <title>Some Show S01E01 1080p</title>
    <guid>episode-1</guid>
    <link>magnet:?xt=urn:btih:1111111111111111111111111111111111111111&amp;dn=Some+Show+S01E01</link>
  </item>
</channel></rss>`))
	}))
	defer rss.Close()

	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if atomic.LoadInt32(&addCalls) > 0 {
				_, _ = w.Write([]byte(`[{"hash":"abc123","name":"Some Show S01E01 1080p","state":"downloading","progress":0.1}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}, &model.DownloadClient{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, downloads, nil, NewHub(zap.NewNop()))

	sub := &model.Subscription{
		Name:      "Some Show 自动订阅",
		FeedURL:   rss.URL,
		Filter:    "Some Show",
		MediaType: "tv",
		SavePath:  "/downloads/tv",
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.runOne(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
	rows, err := repos.Download.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("download rows = %d, want 1", len(rows))
	}
}

func TestSubscriptionRunOneSkipsSameEpisodeAddedEarlierInFeed(t *testing.T) {
	rss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss><channel>
  <item>
    <title>Some Show S01E01 1080p</title>
    <guid>episode-1-a</guid>
    <link>magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&amp;dn=Some+Show+S01E01+1080p</link>
  </item>
  <item>
    <title>Some Show S01E01 WEB-DL</title>
    <guid>episode-1-b</guid>
    <link>magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&amp;dn=Some+Show+S01E01+WEB-DL</link>
  </item>
</channel></rss>`))
	}))
	defer rss.Close()

	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if atomic.LoadInt32(&addCalls) > 0 {
				_, _ = w.Write([]byte(`[{"hash":"abc123","name":"Some Show S01E01 1080p","state":"downloading","progress":0.1}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}, &model.DownloadClient{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, downloads, nil, NewHub(zap.NewNop()))

	sub := &model.Subscription{
		Name:          "Some Show 自动订阅",
		FeedURL:       rss.URL,
		Filter:        "Some Show",
		MediaType:     "tv",
		SavePath:      "/downloads/tv",
		TotalEpisodes: 12,
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.runOne(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
	rows, err := repos.Download.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("download rows = %d, want 1", len(rows))
	}
}

func TestSubscriptionRunOneRSSWashQueuesOnlyBestMovieVariant(t *testing.T) {
	rss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss><channel>
  <item>
    <title>Dune 2021 1080p WEB-DL</title>
    <guid>dune-1080-web</guid>
    <link>magnet:?xt=urn:btih:dddddddddddddddddddddddddddddddddddddddd&amp;dn=Dune+2021+1080p+WEB-DL</link>
  </item>
  <item>
    <title>Dune 2021 2160p UHD BluRay REMUX HDR</title>
    <guid>dune-2160-remux</guid>
    <link>magnet:?xt=urn:btih:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee&amp;dn=Dune+2021+2160p+REMUX</link>
  </item>
  <item>
    <title>Dune 2021 720p HDTV</title>
    <guid>dune-720-hdtv</guid>
    <link>magnet:?xt=urn:btih:ffffffffffffffffffffffffffffffffffffffff&amp;dn=Dune+2021+720p+HDTV</link>
  </item>
</channel></rss>`))
	}))
	defer rss.Close()

	var addCalls int32
	var addedTitles []string
	addedHashes := make([]string, 0, 3)
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if len(addedHashes) == 0 {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			var items []string
			for _, hash := range addedHashes {
				items = append(items, `{"hash":"`+hash+`","name":"Dune 2021","state":"downloading","progress":0.1}`)
			}
			_, _ = w.Write([]byte(`[` + strings.Join(items, ",") + `]`))
		case "/api/v2/torrents/add":
			call := atomic.AddInt32(&addCalls, 1)
			_ = r.ParseMultipartForm(10 << 20)
			addedTitles = append(addedTitles, r.FormValue("urls"))
			addedHashes = append(addedHashes, strings.Repeat(fmt.Sprintf("%x", call), 40))
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}, &model.DownloadClient{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, downloads, nil, NewHub(zap.NewNop()))

	sub := &model.Subscription{
		Name:         "Dune 自动订阅",
		FeedURL:      rss.URL,
		Filter:       "Dune 2021",
		MediaType:    "movie",
		WashEnabled:  true,
		WashPriority: "resolution",
		SavePath:     "/downloads/movies",
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}
	queued, err := svc.runOne(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1 best movie variant", queued)
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
	if len(addedTitles) != 1 || !strings.Contains(addedTitles[0], "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee") {
		t.Fatalf("added %#v, want 2160p REMUX variant only", addedTitles)
	}
}

func TestSubscriptionRunOneDoesNotUseDeletedDownloader(t *testing.T) {
	rss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss><channel>
  <item>
    <title>Deleted Downloader Show S01E01 1080p</title>
    <guid>deleted-downloader-episode-1</guid>
    <link>magnet:?xt=urn:btih:cccccccccccccccccccccccccccccccccccccccc&amp;dn=Deleted+Downloader+Show+S01E01</link>
  </item>
</channel></rss>`))
	}))
	defer rss.Close()

	var qbCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&qbCalls, 1)
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}, &model.DownloadClient{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	client := &model.DownloadClient{Name: "qB deleted", Type: "qbittorrent", Host: qb.URL, Username: "admin", Password: "admin", IsDefault: true, Enabled: true}
	if err := repos.DownloadClient.Create(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), settingDownloadClientsManaged, "true"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DownloadClient.Delete(t.Context(), client.ID); err != nil {
		t.Fatal(err)
	}

	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc := NewSubscriptionService(nil, zap.NewNop(), repos, downloads, nil, NewHub(zap.NewNop()))
	sub := &model.Subscription{
		Name:      "Deleted Downloader Show 自动订阅",
		FeedURL:   rss.URL,
		Filter:    "Deleted Downloader Show",
		MediaType: "tv",
		SavePath:  "/downloads/tv",
	}
	if err := repos.Subscription.Create(t.Context(), sub); err != nil {
		t.Fatal(err)
	}

	queued, err := svc.runOne(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("queued = %d, want 0 when default downloader was deleted", queued)
	}
	if got := atomic.LoadInt32(&qbCalls); got != 0 {
		t.Fatalf("qB calls = %d, want 0 after downloader deletion", got)
	}
	rows, err := repos.Download.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("download rows = %d, want 0", len(rows))
	}
}

func TestMatchesSubscriptionRulesUserExcludeWords(t *testing.T) {
	sub := &model.Subscription{ExcludeWords: "10bit,dolby vision,杜比"}
	cases := []struct {
		title string
		want  bool
	}{
		{"Movie 2024 1080p WEB-DL", true},
		{"Movie 2024 2160p 10bit HEVC", false},
		{"Movie 2024 2160p Dolby Vision", false},
		{"电影 2024 杜比全景声", false},
	}
	for _, c := range cases {
		if got := matchesSubscriptionRules(sub, c.title); got != c.want {
			t.Errorf("matchesSubscriptionRules(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestMatchesSubscriptionRulesDefaultExcludesJunkReleases(t *testing.T) {
	sub := &model.Subscription{}
	for _, title := range []string{
		"Some Movie 2024 CAM",
		"Some Movie 2024 HDTS",
		"某电影 2024 枪版",
		"Some Movie 2024 TELESYNC",
		"Some Show 预告",
	} {
		if matchesSubscriptionRules(sub, title) {
			t.Errorf("expected default rules to exclude junk release %q", title)
		}
	}
}

func TestMatchesSubscriptionRulesWordBoundaryAvoidsFalsePositives(t *testing.T) {
	sub := &model.Subscription{}
	// "ts" / "cam" / "tc" 作为子串出现在合法标题里时不应被默认排除误伤。
	for _, title := range []string{
		"Tsukihime 2024 1080p WEB-DL",
		"Camp Rock 2024 1080p BluRay",
		"Catch Me 2024 1080p WEB-DL",
	} {
		if !matchesSubscriptionRules(sub, title) {
			t.Errorf("word-boundary match wrongly excluded %q", title)
		}
	}
}

func TestSelectSiteSearchCandidatesSkipsExistingMovieWhenNotWashing(t *testing.T) {
	sub := &model.Subscription{Name: "Inception 自动订阅", Filter: "Inception 2010", MediaType: "movie"}
	results := []SearchResult{
		{Title: "Inception 2010 2160p 10bit Dolby Vision Atmos", DownloadURL: "https://pt/download/dovi", Seeders: 500},
		{Title: "Inception 2010 1080p WEB-DL", DownloadURL: "https://pt/download/web", Seeders: 90},
	}
	availability := LocalAvailability{LocalMediaCount: 1, InLibrary: true, DownloadedEpisodes: 1, TotalEpisodes: 1}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 0 {
		t.Fatalf("selected %#v, want none (movie already in library, wash disabled)", got)
	}
}

func TestSelectSiteSearchCandidatesAllowsMovieWashUpgrade(t *testing.T) {
	sub := &model.Subscription{Name: "Inception 自动订阅", Filter: "Inception 2010", MediaType: "movie", WashEnabled: true, WashPriority: "resolution"}
	results := []SearchResult{
		{Title: "Inception 2010 2160p REMUX", DownloadURL: "https://pt/download/2160", Seeders: 80},
		{Title: "Inception 2010 1080p WEB-DL", DownloadURL: "https://pt/download/1080", Seeders: 200},
	}
	availability := LocalAvailability{LocalMediaCount: 1, InLibrary: true, DownloadedEpisodes: 1, TotalEpisodes: 1}

	got := selectSiteSearchCandidates(results, sub, map[string]struct{}{}, availability)
	if len(got) != 1 || got[0].Download != "https://pt/download/2160" {
		t.Fatalf("selected %#v, want 2160p upgrade allowed when washing", got)
	}
}

func TestSubscriptionItemAlreadyAvailable(t *testing.T) {
	movieSub := &model.Subscription{MediaType: "movie"}
	if !subscriptionItemAlreadyAvailable(movieSub, LocalAvailability{LocalMediaCount: 1}, "Inception 2010 2160p") {
		t.Fatal("movie already in library should be reported available")
	}
	if subscriptionItemAlreadyAvailable(movieSub, LocalAvailability{}, "Inception 2010 2160p") {
		t.Fatal("empty library should not be reported available")
	}
	tvSub := &model.Subscription{MediaType: "tv"}
	avail := LocalAvailability{LocalMediaCount: 1, ExistingEpisodeKeys: map[string]struct{}{episodeKey(1, 2): {}}}
	if !subscriptionItemAlreadyAvailable(tvSub, avail, "Show S01E02 1080p") {
		t.Fatal("existing episode should be reported available")
	}
	if subscriptionItemAlreadyAvailable(tvSub, avail, "Show S01E03 1080p") {
		t.Fatal("missing episode should not be reported available")
	}
}

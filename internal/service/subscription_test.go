package service

import (
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
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	downloads.qb.Configure(QBitConfig{BaseURL: qb.URL, Username: "admin", Password: "admin"})
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
	if err := db.AutoMigrate(&model.Subscription{}, &model.Setting{}, &model.DownloadTask{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	downloads := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	downloads.qb.Configure(QBitConfig{BaseURL: qb.URL, Username: "admin", Password: "admin"})
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

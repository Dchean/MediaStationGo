package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestScanCloudLibraryImportsRecursivePlayableMedia(t *testing.T) {
	empty := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file/sort" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if empty {
			_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[]}}`))
			return
		}
		switch r.URL.Query().Get("pdir_fid") {
		case "0":
			_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[
				{"fid":"d1","file_name":"Movies","dir":true,"size":0},
				{"fid":"f1","file_name":"Root.Movie.2024.mkv","dir":false,"size":123}
			]}}`))
		case "d1":
			_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[
				{"fid":"f2","file_name":"Nested.Show.S01E02.mp4","dir":false,"size":456}
			]}}`))
		default:
			t.Fatalf("unexpected pdir_fid %q", r.URL.Query().Get("pdir_fid"))
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "quark",
		Config: map[string]any{
			"cookie": "kps=test",
			"base":   upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "夸克网盘", Path: "cloud://quark/0", Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Visited != 2 || res.Added != 2 {
		t.Fatalf("scan result = %#v, want visited=2 added=2", res)
	}
	var rows []model.Media
	if err := repos.DB.Order("path").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("media rows = %d, want 2: %#v", len(rows), rows)
	}
	if rows[0].Path != "cloud://quark/Movies/Nested.Show.S01E02.mp4" || !strings.Contains(rows[0].STRMURL, "ref=f2") {
		t.Fatalf("nested media path/strm wrong: path=%q strm=%q", rows[0].Path, rows[0].STRMURL)
	}
	if rows[0].SeasonNum != 1 || rows[0].EpisodeNum != 2 {
		t.Fatalf("nested episode metadata wrong: %#v", rows[0])
	}
	if rows[1].Path != "cloud://quark/Root.Movie.2024.mkv" || rows[1].STRMURL != "/api/cloud/play/quark?ref=f1" {
		t.Fatalf("root media path/strm wrong: path=%q strm=%q", rows[0].Path, rows[0].STRMURL)
	}

	empty = true
	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("rescan cloud: %v", err)
	}
	if res.Removed != 2 {
		t.Fatalf("removed = %d, want 2", res.Removed)
	}
	if got := countMedia(t, repos); got != 0 {
		t.Fatalf("media count after prune = %d, want 0", got)
	}
}

func TestCloudLibraryPathParsing(t *testing.T) {
	typ, dir, ok := parseCloudLibraryPath("cloud://cloud115/abc%20123?ignored=1")
	if !ok || typ != "cloud115" || dir != "abc 123" {
		t.Fatalf("parse path got typ=%q dir=%q ok=%v", typ, dir, ok)
	}
	typ, dir, ok = parseCloudLibraryPath("cloud://quark?dir=0")
	if !ok || typ != "quark" || dir != "" {
		t.Fatalf("parse query got typ=%q dir=%q ok=%v", typ, dir, ok)
	}
	if ref := cloudEntryRef("cloud115", "fid", "pick"); ref != "pick" {
		t.Fatalf("115 ref = %q, want pick", ref)
	}
}

func TestCloudMountConflictDetectsNestedMounts(t *testing.T) {
	root := model.Library{Base: model.Base{ID: "root"}, Name: "115", Path: "cloud://cloud115", Enabled: true}
	childPath := BuildCloudLibraryPath("cloud115", "child-id", "parent-id/child-id")
	info, ok := ParseCloudLibraryMount(childPath)
	if !ok || info.ScanDir != "child-id" || info.DisplayDir != "parent-id/child-id" {
		t.Fatalf("parse child mount = %#v ok=%v", info, ok)
	}

	conflict := FindCloudMountConflict([]model.Library{root}, "cloud115", "child-id", "parent-id/child-id")
	if conflict != nil {
		t.Fatalf("child mount under existing root should be allowed, got conflict %#v", conflict)
	}

	sibling := model.Library{Base: model.Base{ID: "sibling"}, Name: "Sibling", Path: BuildCloudLibraryPath("cloud115", "sibling-id", "parent-id/sibling-id"), Enabled: true}
	conflict = FindCloudMountConflict([]model.Library{sibling}, "cloud115", "child-id", "parent-id/child-id")
	if conflict != nil {
		t.Fatalf("sibling conflict = %#v, want nil", conflict)
	}

	conflict = FindCloudMountConflict([]model.Library{sibling}, "cloud115", "parent-id", "parent-id")
	if conflict == nil || !conflict.Nested {
		t.Fatalf("parent mount over existing child = %#v, want nested conflict", conflict)
	}
	oldIDPath := BuildCloudLibraryPath("cloud115", "child-id", "old-parent-id/child-id")
	conflict = FindCloudMountConflict([]model.Library{{Base: model.Base{ID: "old"}, Name: "Old", Path: oldIDPath, Enabled: true}}, "cloud115", "child-id", "父目录/子目录")
	if conflict == nil || !conflict.Exact {
		t.Fatalf("same scan dir with renamed display path = %#v, want exact conflict", conflict)
	}

	root.CreatedAt = root.CreatedAt.Add(-1)
	child := model.Library{Base: model.Base{ID: "child"}, Name: "Child", Path: childPath, Enabled: true}
	if shadow := CloudLibraryShadowed([]model.Library{root, child}, child); shadow != nil {
		t.Fatalf("child should not be shadowed by root: %#v", shadow)
	}
	if shadow := CloudLibraryShadowed([]model.Library{root, child}, root); shadow == nil || !shadow.Nested {
		t.Fatalf("root should be shadowed by child, got %#v", shadow)
	}
}

func TestInferCloudMountMediaType(t *testing.T) {
	cases := map[string]string{
		"/日漫":      "anime",
		"/国漫":      "anime",
		"/欧美动漫":    "anime",
		"/电视剧/国产剧": "tv",
		"/电视剧/欧美剧": "tv",
		"/电视剧/日韩剧": "tv",
		"/电影/动画电影": "movie",
		"/电影/华语电影": "movie",
		"/电影/外语电影": "movie",
		"/综艺":      "variety",
	}
	for dir, want := range cases {
		if got := InferCloudMountMediaType(dir, "OpenList · "+dir); got != want {
			t.Fatalf("%s type = %s, want %s", dir, got, want)
		}
	}
}

func TestScan115CloudLibraryKeepsDisplayHierarchyAndSeasonCounts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("cid") {
		case "100":
			_, _ = w.Write([]byte(`{"state":true,"data":[
				{"cid":"s1","n":"Season 1","s":0},
				{"cid":"s2","n":"Season 2","s":0}
			]}`))
		case "s1":
			_, _ = w.Write([]byte(`{"state":true,"data":[
				{"fid":"f101","n":"剑来 - S01E01.mkv","s":1001,"pc":"pick101"},
				{"fid":"f125","n":"剑来 - S01E25.mkv","s":1025,"pc":"pick125"}
			]}`))
		case "s2":
			_, _ = w.Write([]byte(`{"state":true,"data":[
				{"fid":"f201","n":"剑来 - S02E01.mkv","s":2001,"pc":"pick201"},
				{"fid":"f204","n":"剑来 - S02E04.mkv","s":2004,"pc":"pick204"}
			]}`))
		default:
			t.Fatalf("unexpected cid %q", r.URL.Query().Get("cid"))
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "cloud115",
		Config: map[string]any{
			"cookie": "UID=test",
			"base":   upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "115 · 国漫 · 剑来", Path: BuildCloudLibraryPath("cloud115", "100", "动漫/国漫/剑来"), Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud115: %v", err)
	}
	if res.Added != 4 {
		t.Fatalf("scan result = %#v, want added=4", res)
	}
	var rows []model.Media
	if err := repos.DB.Order("path").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("media rows = %d, want 4", len(rows))
	}
	want := map[string][2]int{
		"cloud://cloud115/动漫/国漫/剑来/Season 1/剑来 - S01E01.mkv": {1, 1},
		"cloud://cloud115/动漫/国漫/剑来/Season 1/剑来 - S01E25.mkv": {1, 25},
		"cloud://cloud115/动漫/国漫/剑来/Season 2/剑来 - S02E01.mkv": {2, 1},
		"cloud://cloud115/动漫/国漫/剑来/Season 2/剑来 - S02E04.mkv": {2, 4},
	}
	for _, row := range rows {
		seasonEpisode, ok := want[row.Path]
		if !ok {
			t.Fatalf("unexpected path %q", row.Path)
		}
		if row.Title != "剑来" {
			t.Fatalf("title = %q, want 剑来", row.Title)
		}
		if row.SeasonNum != seasonEpisode[0] || row.EpisodeNum != seasonEpisode[1] {
			t.Fatalf("%s season/episode = %d/%d, want %d/%d", row.Path, row.SeasonNum, row.EpisodeNum, seasonEpisode[0], seasonEpisode[1])
		}
		if !strings.Contains(row.STRMURL, "ref=pick") {
			t.Fatalf("115 playback should keep pickcode ref, got %q", row.STRMURL)
		}
	}
}

func TestCloudSeriesTitlePrefersShowFolder(t *testing.T) {
	title, year := cloudSeriesTitleFromMediaPath("cloud://openlist/国产剧/紫川 (2024) {tmdb-247590}/Season 2/紫川.2024.S02E24.第24集.2160p.WEB-DL.H.265-ColorTV.mkv")
	if title != "紫川" || year != 2024 {
		t.Fatalf("cloud series title = %q/%d, want 紫川/2024", title, year)
	}
	title, year = cloudSeriesTitleFromMediaPath("cloud://openlist/国产剧/紫川.2024.S02E24.mkv")
	if title != "" || year != 0 {
		t.Fatalf("single category folder should not override title, got %q/%d", title, year)
	}
}

func TestScanCloudLibraryReadsRemoteSTRMTarget(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			if r.URL.Path != "/dav/Links" {
				t.Fatalf("unexpected propfind path %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/Links/</d:href>
    <d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/Links/Movie.strm</d:href>
    <d:propstat><d:prop><d:displayname>Movie.strm</d:displayname><d:getcontentlength>32</d:getcontentlength><d:resourcetype/></d:prop></d:propstat>
  </d:response>
</d:multistatus>`))
		case http.MethodGet:
			if r.URL.Path != "/dav/Links/Movie.strm" {
				t.Fatalf("unexpected get path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte("https://cdn.example.com/Movie.mkv\n"))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"url": upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList · Links", Path: "cloud://openlist/Links", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("scan result = %#v, want added=1", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.Path != "cloud://openlist/Links/Movie.strm" {
		t.Fatalf("path = %q", media.Path)
	}
	if media.STRMURL != "https://cdn.example.com/Movie.mkv" {
		t.Fatalf("strm target = %q", media.STRMURL)
	}
}

func TestScanCloudLibraryReadsRemoteNFOAndArtwork(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			switch r.URL.Path {
			case "/dav/Anime/JianLai":
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/Anime/JianLai/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Anime/JianLai/tvshow.nfo</d:href><d:propstat><d:prop><d:displayname>tvshow.nfo</d:displayname><d:getcontentlength>64</d:getcontentlength><d:resourcetype/></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Anime/JianLai/poster.jpg</d:href><d:propstat><d:prop><d:displayname>poster.jpg</d:displayname><d:getcontentlength>1024</d:getcontentlength><d:resourcetype/></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Anime/JianLai/Season1/</d:href><d:propstat><d:prop><d:displayname>Season1</d:displayname><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
</d:multistatus>`))
			case "/dav/Anime/JianLai/Season1":
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/Anime/JianLai/Season1/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Anime/JianLai/Season1/JianLai.S01E01.mkv</d:href><d:propstat><d:prop><d:displayname>JianLai.S01E01.mkv</d:displayname><d:getcontentlength>2048</d:getcontentlength><d:resourcetype/></d:prop></d:propstat></d:response>
  <d:response><d:href>/dav/Anime/JianLai/Season1/JianLai.S01E01.nfo</d:href><d:propstat><d:prop><d:displayname>JianLai.S01E01.nfo</d:displayname><d:getcontentlength>128</d:getcontentlength><d:resourcetype/></d:prop></d:propstat></d:response>
</d:multistatus>`))
			default:
				t.Fatalf("unexpected propfind path %s", r.URL.Path)
			}
		case http.MethodGet:
			switch r.URL.Path {
			case "/dav/Anime/JianLai/tvshow.nfo":
				_, _ = w.Write([]byte(`<tvshow><title>剑来</title><year>2024</year><plot>天地有剑气</plot></tvshow>`))
			case "/dav/Anime/JianLai/Season1/JianLai.S01E01.nfo":
				_, _ = w.Write([]byte(`<episodedetails><showtitle>剑来</showtitle><title>第一集</title><season>1</season><episode>1</episode></episodedetails>`))
			default:
				t.Fatalf("unexpected get path %s", r.URL.Path)
			}
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer upstream.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}, &model.StorageConfig{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	storage := NewStorageConfigService(log, repos, NewCryptoService("", log))
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"url": upstream.URL,
		},
	}); err != nil {
		t.Fatal(err)
	}
	lib := model.Library{Name: "OpenList · 国漫 · 剑来", Path: "cloud://openlist/Anime/JianLai", Type: "anime", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)

	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatalf("scan cloud: %v", err)
	}
	if res.Added != 1 || res.LocalMetadata != 1 {
		t.Fatalf("scan result = %#v, want added=1 local_metadata=1", res)
	}
	var media model.Media
	if err := repos.DB.First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.Title != "剑来" || media.OriginalName != "第一集" || media.Year != 2024 {
		t.Fatalf("metadata not applied: %#v", media)
	}
	if media.SeasonNum != 1 || media.EpisodeNum != 1 {
		t.Fatalf("episode numbers = %d/%d", media.SeasonNum, media.EpisodeNum)
	}
	if media.PosterURL != "/api/cloud/play/openlist?ref=%2FAnime%2FJianLai%2Fposter.jpg" {
		t.Fatalf("poster url = %q", media.PosterURL)
	}
	if media.ScrapeStatus != "matched" {
		t.Fatalf("scrape status = %q", media.ScrapeStatus)
	}
}

package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

func TestEmbyItemsExposeSeriesSeasonEpisodeHierarchy(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "番剧", Path: `F:\downloads\日番`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	for _, media := range []model.Media{
		{
			Base:         model.Base{ID: "ep-1"},
			LibraryID:    lib.ID,
			Title:        "间谍过家家",
			OriginalName: "第 1 集",
			Path:         `F:\downloads\日番\剧集\间谍过家家\Season 02\间谍过家家 - S02E01.mkv`,
			PosterURL:    `F:\poster.jpg`,
			SeasonNum:    2,
			EpisodeNum:   1,
		},
		{
			Base:         model.Base{ID: "ep-2"},
			LibraryID:    lib.ID,
			Title:        "间谍过家家",
			OriginalName: "第 2 集",
			Path:         `F:\downloads\日番\剧集\间谍过家家\Season 02\间谍过家家 - S02E02.mkv`,
			PosterURL:    `F:\poster.jpg`,
			SeasonNum:    2,
			EpisodeNum:   2,
		},
	} {
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	rootItems := root["Items"].([]map[string]any)
	if len(rootItems) != 1 {
		t.Fatalf("expected one series card, got %#v", rootItems)
	}
	seriesID := rootItems[0]["Id"].(string)
	if rootItems[0]["Type"] != "Series" || rootItems[0]["IsFolder"] != true || rootItems[0]["Name"] != "间谍过家家" {
		t.Fatalf("unexpected series payload: %#v", rootItems[0])
	}

	seasons, err := svc.Items(t.Context(), ItemsParams{ParentID: seriesID, Limit: 50})
	if err != nil {
		t.Fatalf("series items: %v", err)
	}
	seasonItems := seasons["Items"].([]map[string]any)
	if len(seasonItems) != 1 || seasonItems[0]["Type"] != "Season" || seasonItems[0]["IndexNumber"] != 2 {
		t.Fatalf("unexpected seasons: %#v", seasonItems)
	}

	episodes, err := svc.Items(t.Context(), ItemsParams{ParentID: seasonItems[0]["Id"].(string), IncludeItemTypes: []string{"Episode"}, Recursive: true, Limit: 50})
	if err != nil {
		t.Fatalf("season episodes: %v", err)
	}
	episodeItems := episodes["Items"].([]map[string]any)
	if len(episodeItems) != 2 || episodeItems[0]["Type"] != "Episode" || episodeItems[0]["Name"] != "第 1 集" {
		t.Fatalf("unexpected episodes: %#v", episodeItems)
	}
	if episodeItems[0]["SeriesId"] != seriesID || episodeItems[0]["ParentId"] != seasonItems[0]["Id"] {
		t.Fatalf("episode hierarchy not linked: %#v", episodeItems[0])
	}

	latest, err := svc.LatestItems(t.Context(), "user-1", lib.ID, 10)
	if err != nil {
		t.Fatalf("latest items: %v", err)
	}
	if len(latest) != 1 || latest[0]["Type"] != "Series" {
		t.Fatalf("latest should be grouped by series: %#v", latest)
	}

	playback, err := svc.PlaybackInfo(t.Context(), seriesID, "user-1")
	if err != nil {
		t.Fatalf("series playback fallback: %v", err)
	}
	sources := playback["MediaSources"].([]map[string]any)
	if sources[0]["Id"] != "ep-1" {
		t.Fatalf("series playback should fall back to first episode: %#v", sources)
	}
	if sources[0]["DirectStreamUrl"] != "/Videos/ep-1/stream.mkv" {
		t.Fatalf("playback should use Emby-compatible stream URL: %#v", sources[0])
	}
}

func TestEmbyVirtualSeriesArtworkUsesListCache(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "番剧", Path: `/media/anime`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:        model.Base{ID: "ep-1"},
		LibraryID:   lib.ID,
		Title:       "剑来",
		Path:        `/media/anime/剑来/Season 01/剑来 - S01E01.mkv`,
		PosterURL:   `/poster.jpg`,
		BackdropURL: `/backdrop.jpg`,
		SeasonNum:   1,
		EpisodeNum:  1,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	seriesID := items[0]["Id"].(string)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	poster, err := svc.ImageURL(cancelled, seriesID, "Primary")
	if err != nil {
		t.Fatalf("image url from cache: %v", err)
	}
	if poster != "/poster.jpg" {
		t.Fatalf("poster = %q, want cached poster", poster)
	}
	backdrop, err := svc.ImageURL(cancelled, seriesID, "Backdrop")
	if err != nil {
		t.Fatalf("backdrop url from cache: %v", err)
	}
	if backdrop != "/backdrop.jpg" {
		t.Fatalf("backdrop = %q, want cached backdrop", backdrop)
	}
}

func TestEmbyCloudAnimeUsesSeriesNameFromChineseSeasonFolder(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "OpenList · 国漫", Path: `cloud://openlist/国漫`, Type: "anime", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	for _, media := range []model.Media{
		{
			Base:       model.Base{ID: "cloud-ep-1"},
			LibraryID:  lib.ID,
			Title:      "04",
			Path:       `cloud://openlist/国漫/剑来/第二季/04.mkv`,
			SeasonNum:  2,
			EpisodeNum: 4,
		},
		{
			Base:       model.Base{ID: "cloud-ep-2"},
			LibraryID:  lib.ID,
			Title:      "05",
			Path:       `cloud://openlist/国漫/剑来/第二季/05.mkv`,
			SeasonNum:  2,
			EpisodeNum: 5,
		},
	} {
		if err := svc.repo.DB.Create(&media).Error; err != nil {
			t.Fatalf("create media: %v", err)
		}
	}

	root, err := svc.Items(t.Context(), ItemsParams{ParentID: lib.ID, Limit: 50})
	if err != nil {
		t.Fatalf("library items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Type"] != "Series" || items[0]["Name"] != "剑来" {
		t.Fatalf("cloud anime should be grouped as one series named 剑来, got %#v", items)
	}
}

func TestEmbyRootItemsExposeLibraries(t *testing.T) {
	svc := newTestEmbyService(t)
	for _, lib := range []model.Library{
		{Name: "电影", Path: `F:\downloads\电影`, Type: "movie", Enabled: true},
		{Name: "综艺", Path: `F:\downloads\综艺`, Type: "variety", Enabled: true},
	} {
		if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
			t.Fatalf("create library: %v", err)
		}
	}

	root, err := svc.Items(t.Context(), ItemsParams{Limit: 50})
	if err != nil {
		t.Fatalf("root items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	if len(items) != 2 {
		t.Fatalf("expected root items to expose libraries, got %#v", items)
	}
	if items[0]["Type"] != "CollectionFolder" || items[1]["Type"] != "CollectionFolder" {
		t.Fatalf("root should return collection folders: %#v", items)
	}
	if items[1]["CollectionType"] != "tvshows" {
		t.Fatalf("variety libraries should use tvshows collection type: %#v", items[1])
	}
}

func TestEmbyUserPolicyDisablesDownloadsForViewers(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Username: "viewer", Role: "user", Tier: "free", IsActive: true}
	admin := &model.User{Username: "admin", Role: "admin", Tier: "plus", IsActive: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	if err := svc.repo.User.Create(t.Context(), admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}

	viewerPayload, err := svc.FindUser(t.Context(), viewer.ID)
	if err != nil {
		t.Fatalf("viewer payload: %v", err)
	}
	adminPayload, err := svc.FindUser(t.Context(), admin.ID)
	if err != nil {
		t.Fatalf("admin payload: %v", err)
	}
	viewerPolicy := viewerPayload["Policy"].(map[string]any)
	adminPolicy := adminPayload["Policy"].(map[string]any)
	if viewerPolicy["EnableMediaPlayback"] != true {
		t.Fatalf("viewer must keep playback enabled: %#v", viewerPolicy)
	}
	if viewerPolicy["EnableContentDownloading"] != false ||
		viewerPolicy["EnableSyncTranscoding"] != false ||
		viewerPolicy["EnableMediaConversion"] != false {
		t.Fatalf("viewer must not be allowed to download/sync media: %#v", viewerPolicy)
	}
	if adminPolicy["EnableContentDownloading"] != true {
		t.Fatalf("admin should keep downloading capability: %#v", adminPolicy)
	}
}

func TestEmbyHidesAdultLibrariesForUserLock(t *testing.T) {
	svc := newTestEmbyService(t)
	viewer := &model.User{Username: "viewer", Role: "user", Tier: "free", IsActive: true, HideAdult: true}
	if err := svc.repo.User.Create(t.Context(), viewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	safe := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	adult := model.Library{Name: "9KG 成人", Path: `/media/9KG`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &safe); err != nil {
		t.Fatalf("create safe library: %v", err)
	}
	if err := svc.repo.Library.Create(t.Context(), &adult); err != nil {
		t.Fatalf("create adult library: %v", err)
	}
	if err := svc.repo.Setting.Set(t.Context(), AdultLibraryIDsSettingKey, `["`+adult.ID+`"]`); err != nil {
		t.Fatalf("set adult libraries: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Media{LibraryID: safe.ID, Title: "安全电影", Path: `/media/movies/a.mkv`}).Error; err != nil {
		t.Fatalf("create safe media: %v", err)
	}
	if err := svc.repo.DB.Create(&model.Media{LibraryID: adult.ID, Title: "成人电影", Path: `/media/9KG/a.mkv`}).Error; err != nil {
		t.Fatalf("create adult media: %v", err)
	}

	root, err := svc.Items(t.Context(), ItemsParams{UserID: viewer.ID, Limit: 50})
	if err != nil {
		t.Fatalf("root items: %v", err)
	}
	items := root["Items"].([]map[string]any)
	if len(items) != 1 || items[0]["Name"] != "电影" {
		t.Fatalf("adult library should be hidden: %#v", items)
	}
	adultItems, err := svc.Items(t.Context(), ItemsParams{UserID: viewer.ID, ParentID: adult.ID, Limit: 50})
	if err != nil {
		t.Fatalf("adult items: %v", err)
	}
	if got := adultItems["TotalRecordCount"]; got != int64(0) {
		t.Fatalf("adult media should be hidden, total=%#v payload=%#v", got, adultItems)
	}
}

func TestEmbyPlaybackInfoRespectsDirectPlayOnly(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "电影", Path: `/media/movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{Base: model.Base{ID: "m-1"}, LibraryID: lib.ID, Title: "Inception", Path: `/media/movies/inception.mkv`}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	// 默认（关闭）：宿主机可转码，下发 TranscodingUrl。
	pb, err := svc.PlaybackInfo(t.Context(), "m-1", "user-1")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["SupportsTranscoding"] != true {
		t.Fatalf("expected SupportsTranscoding=true by default, got %#v", src["SupportsTranscoding"])
	}
	if _, ok := src["TranscodingUrl"]; !ok {
		t.Fatalf("expected TranscodingUrl present by default: %#v", src)
	}
	if src["TranscodingUrl"] != "/Videos/m-1/master.m3u8" {
		t.Fatalf("expected HLS TranscodingUrl by default, got %#v", src["TranscodingUrl"])
	}

	// 开启「客户端直连解码」：不再下发转码能力 / TranscodingUrl，仍保留 DirectStream。
	if err := svc.repo.Setting.Set(t.Context(), PlaybackDirectOnlySettingKey, "true"); err != nil {
		t.Fatalf("enable direct-only: %v", err)
	}
	pb, err = svc.PlaybackInfo(t.Context(), "m-1", "user-1")
	if err != nil {
		t.Fatalf("playback info (direct-only): %v", err)
	}
	src = pb["MediaSources"].([]map[string]any)[0]
	if src["SupportsTranscoding"] != false {
		t.Fatalf("expected SupportsTranscoding=false in direct-only mode, got %#v", src["SupportsTranscoding"])
	}
	if _, ok := src["TranscodingUrl"]; ok {
		t.Fatalf("expected no TranscodingUrl in direct-only mode: %#v", src)
	}
	if src["SupportsDirectPlay"] != true || src["DirectStreamUrl"] != "/Videos/m-1/stream.mkv" {
		t.Fatalf("direct-only must still allow direct play: %#v", src)
	}
}

func TestEmbyPlaybackInfoKeepsSTRMBehindStreamEndpoint(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "夸克网盘", Path: `cloud://quark/0`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:      model.Base{ID: "cloud-1"},
		LibraryID: lib.ID,
		Title:     "Cloud Movie",
		Path:      `cloud://quark/f1`,
		STRMURL:   `/api/cloud/play/quark?ref=f1`,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pb, err := svc.PlaybackInfo(t.Context(), "cloud-1", "user-1")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["IsRemote"] != true {
		t.Fatalf("strm media should be marked remote: %#v", src)
	}
	if src["DirectStreamUrl"] != "/api/stream/cloud-1" {
		t.Fatalf("strm playback should prefer /api/stream when enabled: %#v", src)
	}
	if src["Path"] != "/api/stream/cloud-1" {
		t.Fatalf("path should prefer /api/stream when enabled: %#v", src)
	}
	streams := src["MediaStreams"].([]map[string]any)
	if len(streams) == 0 || streams[0]["Type"] != "Video" {
		t.Fatalf("strm media should expose a fallback video stream for Android clients: %#v", src)
	}
}

func TestEmbyPlaybackInfoUsesVideoStreamWhenSTRMDisabled(t *testing.T) {
	svc := newTestEmbyService(t)
	if err := svc.repo.Setting.Set(t.Context(), STRMEnabledSettingKey, "false"); err != nil {
		t.Fatalf("set strm disabled: %v", err)
	}
	lib := model.Library{Name: "OpenList", Path: `cloud://openlist/Movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:      model.Base{ID: "cloud-302"},
		LibraryID: lib.ID,
		Title:     "Cloud 302 Movie",
		Path:      `cloud://openlist/Movies/Movie.mkv`,
		STRMURL:   `/api/cloud/play/openlist?ref=%2FMovies%2FMovie.mkv`,
		Container: "mkv",
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}

	pb, err := svc.PlaybackInfo(t.Context(), "cloud-302", "user-1")
	if err != nil {
		t.Fatalf("playback info: %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["DirectStreamUrl"] != "/Videos/cloud-302/stream.mkv" {
		t.Fatalf("302/proxy mode should use Emby video stream URL: %#v", src)
	}
	if src["Path"] != "/Videos/cloud-302/stream.mkv" {
		t.Fatalf("302/proxy mode path should use Emby video stream URL: %#v", src)
	}
}

func TestEmbyPlaybackInfoProbesMissingCloudTrackMetadata(t *testing.T) {
	svc := newTestEmbyService(t)
	lib := model.Library{Name: "OpenList", Path: `cloud://openlist/Movies`, Type: "movie", Enabled: true}
	if err := svc.repo.Library.Create(t.Context(), &lib); err != nil {
		t.Fatalf("create library: %v", err)
	}
	media := model.Media{
		Base:      model.Base{ID: "cloud-probe-1"},
		LibraryID: lib.ID,
		Title:     "云盘电影",
		Path:      `cloud://openlist/Movies/Movie.mkv`,
		STRMURL:   `http://nas.local/api/cloud/play/openlist?ref=%2FMovies%2FMovie.mkv`,
	}
	if err := svc.repo.DB.Create(&media).Error; err != nil {
		t.Fatalf("create media: %v", err)
	}
	resolver := &fakeCloudPlaybackResolver{
		link: &cloud.DirectLink{
			URL:     "http://cdn.example.test/Movie.mkv",
			Headers: map[string]string{"Authorization": "Bearer probe-token"},
		},
	}
	prober := &fakeCloudPlaybackProber{
		probe: &ProbeResult{
			DurationSec: 3661,
			Width:       3840,
			Height:      2160,
			VideoCodec:  "hevc",
			AudioCodec:  "eac3",
			Container:   "matroska,webm",
		},
	}
	svc.SetCloudProbe(resolver, prober)

	if _, err := svc.PlaybackInfo(t.Context(), "cloud-probe-1", "user-1"); err != nil {
		t.Fatalf("playback info: %v", err)
	}

	// 探测现在是异步的（同步探测曾把起播拖慢最多 8 秒并放大云盘流量）。
	// 轮询等待后台探测结果落库。
	var persisted model.Media
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := svc.repo.DB.First(&persisted, "id = ?", "cloud-probe-1").Error; err != nil {
			t.Fatalf("reload media: %v", err)
		}
		if persisted.DurationSec > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if persisted.DurationSec != 3661 || persisted.Width != 3840 || persisted.Height != 2160 || persisted.VideoCodec != "hevc" || persisted.AudioCodec != "eac3" {
		t.Fatalf("probe metadata not persisted: %#v", persisted)
	}
	if resolver.typ != "openlist" || resolver.ref != "/Movies/Movie.mkv" {
		t.Fatalf("resolver called with typ=%q ref=%q", resolver.typ, resolver.ref)
	}
	if prober.rawURL != "http://cdn.example.test/Movie.mkv" || prober.headers["Authorization"] != "Bearer probe-token" {
		t.Fatalf("probe called with url=%q headers=%#v", prober.rawURL, prober.headers)
	}

	// 落库之后，再次请求 PlaybackInfo 应当带上完整轨道元数据。
	pb, err := svc.PlaybackInfo(t.Context(), "cloud-probe-1", "user-1")
	if err != nil {
		t.Fatalf("playback info (second): %v", err)
	}
	src := pb["MediaSources"].([]map[string]any)[0]
	if src["RunTimeTicks"] != int64(3661)*10_000_000 {
		t.Fatalf("runtime ticks not filled after async probe: %#v", src)
	}
	streams := src["MediaStreams"].([]map[string]any)
	if len(streams) != 2 || streams[0]["Codec"] != "hevc" || streams[1]["Codec"] != "eac3" {
		t.Fatalf("media streams not filled after async probe: %#v", streams)
	}
}

func newTestEmbyService(t *testing.T) *EmbyService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// 内存库 + 异步探测协程：限制为单连接，避免连接池新建连接时
	// 拿到一个空白的 :memory: 实例（no such table）。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Series{}, &model.Media{}, &model.Favorite{}, &model.PlaybackHistory{}, &model.User{}, &model.Setting{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.New(db)
	return NewEmbyService(&config.Config{}, zap.NewNop(), repos)
}

type fakeCloudPlaybackResolver struct {
	link *cloud.DirectLink
	typ  string
	ref  string
	ua   string
}

func (f *fakeCloudPlaybackResolver) CloudResolve(_ context.Context, typ, fileRef, clientUA string) (*cloud.DirectLink, error) {
	f.typ = typ
	f.ref = fileRef
	f.ua = clientUA
	return f.link, nil
}

type fakeCloudPlaybackProber struct {
	probe   *ProbeResult
	rawURL  string
	headers map[string]string
}

func (f *fakeCloudPlaybackProber) ProbeHTTP(_ context.Context, rawURL string, headers map[string]string) (*ProbeResult, error) {
	f.rawURL = rawURL
	f.headers = headers
	return f.probe, nil
}

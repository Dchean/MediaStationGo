package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestReadLocalMovieMetadata(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "Inception.2010.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>盗梦空间</title>
  <originaltitle>Inception</originaltitle>
  <year>2010</year>
  <plot>梦境盗窃。</plot>
  <rating>8.8</rating>
  <uniqueid type="tmdb">27205</uniqueid>
  <genre>科幻</genre>
  <genre>动作</genre>
</movie>`
	if err := os.WriteFile(nfoPath(mediaPath), []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "盗梦空间" || got.OriginalName != "Inception" || got.Year != 2010 || got.TMDbID != 27205 {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.Genres != "科幻,动作" {
		t.Fatalf("genres = %q", got.Genres)
	}
}

func TestReadLocalEpisodeMetadataMergesShowAndEpisode(t *testing.T) {
	root := t.TempDir()
	showDir := filepath.Join(root, "Show")
	seasonDir := filepath.Join(showDir, "Season 02")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "Show - EP03.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "tvshow.nfo"), []byte(`<tvshow><title>正确剧名</title><year>2024</year><tmdbid>123</tmdbid></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<episodedetails><title>第三集</title><season>2</season><episode>3</episode><plot>本集简介</plot></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "正确剧名" || got.OriginalName != "第三集" || got.SeasonNum != 2 || got.EpisodeNum != 3 {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.Overview != "本集简介" || got.TMDbID != 123 {
		t.Fatalf("episode/show merge failed: %+v", got)
	}
}

func TestReadLocalVarietyMetadataUsesLocalArtwork(t *testing.T) {
	root := t.TempDir()
	showDir := filepath.Join(root, "哈哈哈哈哈")
	seasonDir := filepath.Join(showDir, "Season 06")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "哈哈哈哈哈 - S06E17.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(showDir, "哈哈哈哈哈.nfo"), []byte(`<tvshow><title>哈哈哈哈哈</title><genre>综艺</genre></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	episodeThumb := filepath.Join(seasonDir, "哈哈哈哈哈 - S06E17-thumb.jpg")
	if err := os.WriteFile(episodeThumb, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdrop := filepath.Join(showDir, "fanart.jpg")
	if err := os.WriteFile(backdrop, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("metadata is nil")
	}
	if got.Title != "哈哈哈哈哈" || got.Genres != "综艺" {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.PosterURL != episodeThumb {
		t.Fatalf("PosterURL = %q, want %q", got.PosterURL, episodeThumb)
	}
	if got.BackdropURL != backdrop {
		t.Fatalf("BackdropURL = %q, want %q", got.BackdropURL, backdrop)
	}
}

func TestReadLocalMetadataWithoutNFOStillFindsArtwork(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Movie.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "Movie-poster.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PosterURL != poster {
		t.Fatalf("unexpected artwork metadata: %+v", got)
	}
}

func TestReadAdultLocalMetadataAndArtwork(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "SSIS-001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(dir, "SSIS-001.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>成人影片标题</title>
  <originaltitle>SSIS-001</originaltitle>
  <num>SSIS-001</num>
  <releasedate>2024-05-01</releasedate>
  <plot>本地简介</plot>
  <poster>SSIS-001-poster.jpg</poster>
  <fanart><thumb>SSIS-001-fanart.jpg</thumb></fanart>
  <studio>测试片商</studio>
  <genre>剧情</genre>
  <tag>中文字幕</tag>
  <actor><name>演员A</name></actor>
</movie>`
	if err := os.WriteFile(nfoPath(mediaPath), []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(dir, "SSIS-001-poster.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	fanart := filepath.Join(dir, "SSIS-001-fanart.jpg")
	if err := os.WriteFile(fanart, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "成人影片标题" || got.AdultCode != "SSIS-001" || !got.NSFW {
		t.Fatalf("unexpected adult metadata: %+v", got)
	}
	if got.OriginalName != "SSIS-001" || got.Year != 2024 || got.Overview != "本地简介" {
		t.Fatalf("unexpected adult fields: %+v", got)
	}
	if got.PosterURL != poster || got.BackdropURL != fanart {
		t.Fatalf("artwork poster=%q fanart=%q", got.PosterURL, got.BackdropURL)
	}
	if got.Genres != "剧情,中文字幕,测试片商,演员A" {
		t.Fatalf("genres = %q", got.Genres)
	}
}

func TestReadAdultMovieNFOFallbackInSingleMovieFolder(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "MIDV-123")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.nfo"), []byte(`<movie><title>本地番号电影</title><num>MIDV-123</num></movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(dir, "video-cover.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "本地番号电影" || got.AdultCode != "MIDV-123" || got.PosterURL != poster {
		t.Fatalf("unexpected fallback metadata: %+v", got)
	}
}

func TestReadAdultNFOByCodeForStackedFile(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "SSIS-001-C.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SSIS-001.nfo"), []byte(`<movie>
  <title>按番号命中的本地 NFO</title>
  <originaltitle>SSIS-001</originaltitle>
  <art><poster>SSIS-001-poster.jpg</poster><fanart>SSIS-001-fanart.jpg</fanart></art>
</movie>`), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "SSIS-001-poster.jpg")
	fanart := filepath.Join(root, "SSIS-001-fanart.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fanart, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "按番号命中的本地 NFO" || got.AdultCode != "SSIS-001" || !got.HasNFO {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	if got.PosterURL != poster || got.BackdropURL != fanart {
		t.Fatalf("artwork poster=%q fanart=%q", got.PosterURL, got.BackdropURL)
	}
}

func TestReadAdultArtworkByCodeWithoutNFO(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "SSIS-001-CD1.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "SSIS-001-poster.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalMetadata(mediaPath, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PosterURL != poster || !got.HasArtwork || got.HasNFO {
		t.Fatalf("unexpected artwork metadata: %+v", got)
	}
}

func TestScanLibraryUsesLocalMetadata(t *testing.T) {
	root := t.TempDir()
	seasonDir := filepath.Join(root, "Show", "Season 02")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "Show - EP03.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Show", "tvshow.nfo"), []byte(`<tvshow><title>本地剧名</title><year>2025</year></tvshow>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfoPath(mediaPath), []byte(`<episodedetails><title>本地第三集</title><season>2</season><episode>3</episode></episodedetails>`), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.LocalMetadata != 1 {
		t.Fatalf("LocalMetadata = %d, want 1", res.LocalMetadata)
	}
	if res.Added != 1 || res.Updated != 0 {
		t.Fatalf("scan counts added=%d updated=%d, want 1/0", res.Added, res.Updated)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if media.Title != "本地剧名" || media.OriginalName != "本地第三集" || media.SeasonNum != 2 || media.EpisodeNum != 3 || media.ScrapeStatus != "matched" {
		t.Fatalf("unexpected scanned media: %+v", media)
	}

	res, err = scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 0 || res.Updated != 1 {
		t.Fatalf("repeat scan counts added=%d updated=%d, want 0/1", res.Added, res.Updated)
	}
}

func TestScanLibraryDoesNotMarkArtworkOnlyAsMatched(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "SSIS-001-CD1.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "SSIS-001-poster.jpg")
	if err := os.WriteFile(poster, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "Adult", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.LocalMetadata != 1 {
		t.Fatalf("LocalMetadata = %d, want 1", res.LocalMetadata)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if media.PosterURL != poster {
		t.Fatalf("poster_url = %q, want %q", media.PosterURL, poster)
	}
	if media.ScrapeStatus == "matched" {
		t.Fatalf("artwork-only media should remain enrichable, got status %q", media.ScrapeStatus)
	}
}

func TestScanLibraryParsesEpisodesForMovieTypedLibrary(t *testing.T) {
	root := t.TempDir()
	seasonDir := filepath.Join(root, "哈哈哈哈哈 (2020)", "Season 06")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(seasonDir, "哈哈哈哈哈 - S06E17 - 第 17 集.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "综艺", Path: root, Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	if _, err := scanner.ScanLibrary(t.Context(), lib.ID); err != nil {
		t.Fatal(err)
	}
	var media model.Media
	if err := db.First(&media, "path = ?", mediaPath).Error; err != nil {
		t.Fatal(err)
	}
	if media.SeasonNum != 6 || media.EpisodeNum != 17 {
		t.Fatalf("season/episode = %d/%d, want 6/17", media.SeasonNum, media.EpisodeNum)
	}
}

func TestScanLibraryPrunesMissingMedia(t *testing.T) {
	root := t.TempDir()
	mediaPath := filepath.Join(root, "Show.S02E03.mkv")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Library{}, &model.Media{}, &model.Setting{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	lib := model.Library{Name: "TV", Path: root, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	stale := model.Media{
		LibraryID:    lib.ID,
		Title:        "Show",
		Path:         filepath.Join(root, "old", "Show.S02E03.mkv"),
		SizeBytes:    123,
		ScrapeStatus: "pending",
	}
	if err := db.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	res, err := scanner.ScanLibrary(t.Context(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 {
		t.Fatalf("Removed = %d, want 1", res.Removed)
	}
	var count int64
	if err := db.Model(&model.Media{}).Where("library_id = ?", lib.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("media count = %d, want 1", count)
	}
}

package service

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func writeOrgFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestOrganizeDirectoryNewMedia organizes a brand-new movie from a source
// directory (e.g. the download dir) into the destination — no library row
// required.
func TestOrganizeDirectoryNewMedia(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "Dune 2021 2160p WEB-DL.mkv"), "dune-uhd")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 1 || res.Replaced != 0 || res.Skipped != 0 {
		t.Fatalf("expected organized=1 replaced=0 skipped=0, got %+v", res)
	}
	want := filepath.Join(dest, "电影", "Dune (2021)", "Dune (2021).mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected organized file at %q: %v", want, err)
	}
}

func TestOrganizeDirectoryDryRunReturnsPreviewWithoutWriting(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Dune 2021 2160p WEB-DL.mkv")
	writeOrgFile(t, sourceFile, "dune-uhd")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("organize directory dry-run: %v", err)
	}
	if !res.DryRun || res.Organized != 1 || len(res.Items) != 1 {
		t.Fatalf("unexpected dry-run result: %+v", res)
	}
	want := filepath.Join(dest, "电影", "Dune (2021)", "Dune (2021).mkv")
	if res.Items[0].Source != sourceFile || res.Items[0].Target != want || res.Items[0].Action != "organize" {
		t.Fatalf("preview item = %#v, want source=%q target=%q action=organize", res.Items[0], sourceFile, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create %q, stat err=%v", want, err)
	}
}

func TestOrganizeDirectoryUsesConfiguredSourceWhenRequestSourceEmpty(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "Dune 2021 2160p WEB-DL.mkv"), "dune-uhd")

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organize.source_dir", src); err != nil {
		t.Fatal(err)
	}
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory with configured source: %v", err)
	}
	if res.SourcePath != filepath.Clean(src) || res.Organized != 1 {
		t.Fatalf("result = %+v, want source=%q organized=1", res, src)
	}
}

func TestOrganizeDirectoryMapsConfiguredHostPathsToContainerPaths(t *testing.T) {
	root := t.TempDir()
	hostDownloads := filepath.Join(root, "nas-host", "downloads")
	hostMedia := filepath.Join(root, "nas-host", "media")
	containerDownloads := filepath.Join(root, "container", "downloads")
	containerMedia := filepath.Join(root, "container", "media")
	containerSource := filepath.Join(containerDownloads, "国产剧")
	writeOrgFile(t, filepath.Join(containerSource, "Some Show S01E01 2024 1080p.mkv"), "show-e01")

	t.Setenv("MEDIASTATION_DOWNLOAD_DIR", hostDownloads)
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", containerDownloads)
	t.Setenv("MEDIASTATION_MEDIA_DIR", hostMedia)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerMedia)

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organize.source_dir":    filepath.Join(hostDownloads, "国产剧"),
		"organize.target_dir":    hostMedia,
		"organize.transfer_mode": "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{})
	if err != nil {
		t.Fatalf("organize mapped host paths: %v", err)
	}
	if res.SourcePath != filepath.Clean(containerSource) || res.DestPath != filepath.Clean(containerMedia) {
		t.Fatalf("result paths = source %q dest %q, want %q -> %q", res.SourcePath, res.DestPath, containerSource, containerMedia)
	}
	want := filepath.Join(containerMedia, "电视剧", "国产剧", "Some Show", "Season 01", "Some Show - S01E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected organized file at %q: %v", want, err)
	}
}

func TestOrganizeDirectoryAcceptsSingleVideoFileSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "Dune 2021 2160p WEB-DL.mkv")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, src, "dune-uhd")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize single file: %v", err)
	}
	if res.Organized != 1 {
		t.Fatalf("result = %+v, want organized=1", res)
	}
	want := filepath.Join(dest, "电影", "Dune (2021)", "Dune (2021).mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected organized file at %q: %v", want, err)
	}
}

func TestOrganizeDirectoryHonorsManualMediaType(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "Some Show S01E01 2024 1080p.mkv"), "show-e01")
	writeOrgFile(t, filepath.Join(src, "Some Movie 2024 1080p.mkv"), "movie")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	tvRes, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   filepath.Join(src, "Some Show S01E01 2024 1080p.mkv"),
		DestPath:     dest,
		MediaType:    "tv",
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize manual tv type: %v", err)
	}
	if tvRes.Organized != 1 {
		t.Fatalf("tv result = %+v, want organized=1", tvRes)
	}
	tvWant := filepath.Join(dest, "电视剧", "Some Show", "Season 01", "Some Show - S01E01.mkv")
	if _, err := os.Stat(tvWant); err != nil {
		t.Fatalf("expected tv file at %q: %v", tvWant, err)
	}

	movieRes, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   filepath.Join(src, "Some Movie 2024 1080p.mkv"),
		DestPath:     dest,
		MediaType:    "movie",
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize manual movie type: %v", err)
	}
	if movieRes.Organized != 1 {
		t.Fatalf("movie result = %+v, want organized=1", movieRes)
	}
	movieWant := filepath.Join(dest, "电影", "Some Movie (2024)", "Some Movie (2024).mkv")
	if _, err := os.Stat(movieWant); err != nil {
		t.Fatalf("expected movie file at %q: %v", movieWant, err)
	}
}

func TestOrganizeDirectoryHonorsAdultMediaTypeRoot(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "ABP-123.mkv")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, src, "adult")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		MediaType:    "adult",
		TransferMode: TransferCopy,
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("organize adult type: %v", err)
	}
	if res.Organized != 1 || len(res.Items) != 1 {
		t.Fatalf("result = %+v, want one preview item", res)
	}
	if !pathWithin(res.Items[0].Target, filepath.Join(dest, "成人")) {
		t.Fatalf("adult target = %q, want under %q", res.Items[0].Target, filepath.Join(dest, "成人"))
	}
	if res.Items[0].MediaType != "adult" {
		t.Fatalf("media type = %q, want adult", res.Items[0].MediaType)
	}
}

func TestOrganizeSourceCandidatesOnlyReturnAccessibleDirectories(t *testing.T) {
	root := t.TempDir()
	configuredDir := filepath.Join(root, "configured-downloads")
	downloadDir := filepath.Join(root, "downloads")
	mediaDir := filepath.Join(root, "media")
	if err := os.MkdirAll(configuredDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", downloadDir)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", filepath.Join(root, "missing-media"))

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organize.source_dir", configuredDir); err != nil {
		t.Fatal(err)
	}
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	candidates := org.OrganizeSourceCandidates(t.Context())
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want configured source + accessible download dir", candidates)
	}
	if candidates[0].Path != filepath.Clean(configuredDir) || candidates[0].Kind != "source" {
		t.Fatalf("first candidate = %#v, want configured source %q", candidates[0], configuredDir)
	}
	if candidates[1].Path != filepath.Clean(downloadDir) || candidates[1].Kind != "download" {
		t.Fatalf("second candidate = %#v, want accessible download dir %q", candidates[1], downloadDir)
	}

	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", mediaDir)
	candidates = org.OrganizeSourceCandidates(t.Context())
	if len(candidates) != 3 {
		t.Fatalf("candidates = %#v, want configured source, download and media dirs", candidates)
	}
}

// TestOrganizeDirectoryDedup verifies that media already present in the
// destination is NOT organized again from the source (去重), and the existing
// file is left untouched.
func TestOrganizeDirectoryDedup(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	// Source release at 1080p.
	writeOrgFile(t, filepath.Join(src, "The Matrix 1999 1080p BluRay.mkv"), "matrix-source")
	// Destination already holds the organized 1080p version.
	existing := filepath.Join(dest, "电影", "The Matrix (1999)", "The Matrix (1999).mkv")
	writeOrgFile(t, existing, "matrix-existing")

	repos := newOrganizerTestRepo(t)
	// Scanned destination row carries real 1080p dimensions.
	row := model.Media{Title: "The Matrix", Path: existing, Year: 1999, Container: "mkv", Width: 1920, Height: 1080}
	if err := repos.Media.Upsert(t.Context(), &row); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 0 || res.Replaced != 0 || res.Skipped != 1 {
		t.Fatalf("expected organized=0 replaced=0 skipped=1 (dedup), got %+v", res)
	}
	// Existing destination file must be untouched.
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "matrix-existing" {
		t.Fatalf("existing file must be untouched, got %q err=%v", string(got), err)
	}
}

func TestOrganizeDirectoryTreatsScannedTargetPathAsLibraryDuplicate(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	writeOrgFile(t, filepath.Join(src, "Weird.Release.2024.1080p.mkv"), "source")
	existing := filepath.Join(dest, "电影", "Weird Release (2024)", "Weird Release (2024).mkv")
	writeOrgFile(t, existing, "existing")

	repos := newOrganizerTestRepo(t)
	row := model.Media{Title: "刮削后的正式片名", Path: existing, Year: 2024, Container: "mkv", Width: 1920, Height: 1080}
	if err := repos.Media.Upsert(t.Context(), &row); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 0 || res.Skipped != 1 {
		t.Fatalf("result = %+v, want organized=0 skipped=1", res)
	}
	if len(res.Items) != 1 || res.Items[0].Reason != organizeSkipDuplicateLibrary {
		t.Fatalf("items = %+v, want duplicate in library", res.Items)
	}
	if OrganizeResultNeedsVisibilitySync(res) {
		t.Fatal("path already present in DB should not trigger another visibility scan")
	}
}

func TestOrganizeDirectorySkipsSampleClips(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	writeOrgFile(t, filepath.Join(src, "Demo.Movie.2024.1080p.mkv"), "main")
	writeOrgFile(t, filepath.Join(src, "Samples", "Sample1.mkv"), "sample-dir")
	writeOrgFile(t, filepath.Join(src, "Demo.Movie.2024.sample.mkv"), "sample-file")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 1 || res.Skipped != 2 {
		t.Fatalf("result = %+v, want organized=1 skipped=2", res)
	}
	if got := OrganizeSkipReasonCounts(res)[organizeSkipSampleClip]; got != 2 {
		t.Fatalf("sample skip count = %d, want 2; items=%+v", got, res.Items)
	}
	if _, err := os.Stat(filepath.Join(dest, "电影", "Sample1", "Sample1.mkv")); !os.IsNotExist(err) {
		t.Fatalf("sample directory clip should not be organized, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "电影", "Demo Movie Sample (2024)", "Demo Movie Sample (2024).mkv")); !os.IsNotExist(err) {
		t.Fatalf("sample suffix clip should not be organized, stat err=%v", err)
	}
}

// TestOrganizeDirectorySkipsHigherResolutionWhenReplacementDisabled verifies
// that dedup wins by default: even a higher-resolution source must not replace
// an existing library item unless the caller explicitly allows washing.
func TestOrganizeDirectorySkipsHigherResolutionWhenReplacementDisabled(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	// Source is 2160p; filename token drives resolutionArea when no DB row.
	writeOrgFile(t, filepath.Join(src, "Inception 2010 2160p BluRay.mkv"), "inception-uhd")
	// Destination already holds an organized 1080p version (scanned dims).
	existing := filepath.Join(dest, "电影", "Inception (2010)", "Inception (2010).mkv")
	writeOrgFile(t, existing, "inception-1080p")

	repos := newOrganizerTestRepo(t)
	row := model.Media{Title: "Inception", Path: existing, Year: 2010, Container: "mkv", Width: 1920, Height: 1080}
	if err := repos.Media.Upsert(t.Context(), &row); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Skipped != 1 || res.Replaced != 0 || res.Organized != 0 {
		t.Fatalf("expected skipped=1 replaced=0 organized=0 (wash disabled), got %+v", res)
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "inception-1080p" {
		t.Fatalf("destination must keep existing version when wash disabled, got %q err=%v", string(got), err)
	}
	var count int64
	if err := repos.DB.Model(&model.Media{}).Where("path = ?", existing).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected existing media DB row kept, found %d", count)
	}
}

// TestOrganizeDirectoryReplaceHigherResolutionWhenAllowed verifies 洗版: a
// higher-resolution source replaces the lower-resolution version already in the
// destination only when the caller explicitly allows replacement.
func TestOrganizeDirectoryReplaceHigherResolutionWhenAllowed(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	writeOrgFile(t, filepath.Join(src, "Inception 2010 2160p BluRay.mkv"), "inception-uhd")
	existing := filepath.Join(dest, "电影", "Inception (2010)", "Inception (2010).mkv")
	writeOrgFile(t, existing, "inception-1080p")

	repos := newOrganizerTestRepo(t)
	row := model.Media{Title: "Inception", Path: existing, Year: 2010, Container: "mkv", Width: 1920, Height: 1080}
	if err := repos.Media.Upsert(t.Context(), &row); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:           src,
		DestPath:             dest,
		TransferMode:         TransferCopy,
		AllowReplaceExisting: true,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Replaced != 1 || res.Organized != 0 || res.Skipped != 0 {
		t.Fatalf("expected replaced=1 organized=0 skipped=0 (洗版), got %+v", res)
	}
	// Destination file must now contain the higher-resolution source content.
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "inception-uhd" {
		t.Fatalf("destination must hold the higher-res source content, got %q err=%v", string(got), err)
	}
	// The replaced DB row should be gone.
	var count int64
	if err := repos.DB.Model(&model.Media{}).Where("path = ?", existing).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected replaced media DB row removed, found %d", count)
	}
}

// TestOrganizeDirectoryKeepsHigherResolutionExisting verifies that a LOWER-res
// source does NOT replace a higher-res existing file (it is treated as a
// duplicate and skipped).
func TestOrganizeDirectoryKeepsHigherResolutionExisting(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	writeOrgFile(t, filepath.Join(src, "Inception 2010 720p.mkv"), "inception-720p")
	existing := filepath.Join(dest, "电影", "Inception (2010)", "Inception (2010).mkv")
	writeOrgFile(t, existing, "inception-2160p")

	repos := newOrganizerTestRepo(t)
	row := model.Media{Title: "Inception", Path: existing, Year: 2010, Container: "mkv", Width: 3840, Height: 2160}
	if err := repos.Media.Upsert(t.Context(), &row); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Skipped != 1 || res.Replaced != 0 || res.Organized != 0 {
		t.Fatalf("expected skipped=1 (keep higher-res existing), got %+v", res)
	}
	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "inception-2160p" {
		t.Fatalf("higher-res existing must be kept, got %q err=%v", string(got), err)
	}
}

// TestOrganizeDirectoryTVEpisodeDedup verifies per-episode dedup for TV media.
func TestOrganizeDirectoryTVEpisodeDedup(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")

	writeOrgFile(t, filepath.Join(src, "Friends S01E01 1080p.mkv"), "friends-s01e01-src")
	existing := filepath.Join(dest, "电视剧", "Friends", "Season 01", "Friends - S01E01.mkv")
	writeOrgFile(t, existing, "friends-s01e01-existing")
	// A different episode that should still be organized fresh.
	writeOrgFile(t, filepath.Join(src, "Friends S01E02 1080p.mkv"), "friends-s01e02-src")

	repos := newOrganizerTestRepo(t)
	row := model.Media{Title: "Friends", Path: existing, SeasonNum: 1, EpisodeNum: 1, Container: "mkv", Width: 1920, Height: 1080}
	if err := repos.Media.Upsert(t.Context(), &row); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	// E01 deduped (skipped), E02 organized fresh.
	if res.Organized != 1 || res.Skipped != 1 || res.Replaced != 0 {
		t.Fatalf("expected organized=1 skipped=1 replaced=0, got %+v", res)
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "friends-s01e01-existing" {
		t.Fatalf("existing E01 must be untouched, got %q err=%v", string(got), err)
	}
	e02 := filepath.Join(dest, "电视剧", "Friends", "Season 01", "Friends - S01E02.mkv")
	if _, err := os.Stat(e02); err != nil {
		t.Fatalf("expected E02 organized at %q: %v", e02, err)
	}
}

func TestOrganizeDirectoryUsesDownloadCategoryLayout(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "国产剧", "狂飙.S01E01.2023.1080p.WEB-DL.mkv"), "kuangbiao-e01")
	writeOrgFile(t, filepath.Join(src, "华语电影", "流浪地球2.2023.2160p.WEB-DL.H265.mkv"), "wandering-earth-2")

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), newOrganizerTestRepo(t))
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 2 || res.Replaced != 0 || res.Skipped != 0 {
		t.Fatalf("expected organized=2 replaced=0 skipped=0, got %+v", res)
	}

	tv := filepath.Join(dest, "电视剧", "国产剧", "狂飙", "Season 01", "狂飙 - S01E01.mkv")
	if _, err := os.Stat(tv); err != nil {
		t.Fatalf("expected TV episode organized at %q: %v", tv, err)
	}
	movie := filepath.Join(dest, "电影", "华语电影", "流浪地球2 (2023)", "流浪地球2 (2023).mkv")
	if _, err := os.Stat(movie); err != nil {
		t.Fatalf("expected movie organized at %q: %v", movie, err)
	}
}

func TestOrganizeDirectoryUsesExplicitCategoryLibraryRoot(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads", "Motherhood.of.Taihang.S01E01.2026.1080p.mkv")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, src, "episode")

	repos := newOrganizerTestRepo(t)
	libraryRoot := filepath.Join(dest, "电视剧", "国产剧")
	wrongType := model.Library{Name: "国产剧", Path: libraryRoot, Type: "movie", Enabled: true}
	rightType := model.Library{Name: "国产剧", Path: libraryRoot, Type: "tv", Enabled: true}
	if err := repos.Library.Create(t.Context(), &wrongType); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &rightType); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:    src,
		DestPath:      dest,
		MediaType:     "tv",
		MediaCategory: "国产剧",
		TransferMode:  TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize explicit category: %v", err)
	}
	if res.Organized != 1 || len(res.Items) != 1 {
		t.Fatalf("result = %+v, want one organized item", res)
	}
	if !pathWithin(res.Items[0].Target, libraryRoot) {
		t.Fatalf("target = %q, want under %q", res.Items[0].Target, libraryRoot)
	}
	if pathWithin(res.Items[0].Target, filepath.Join(dest, "电视剧")) && !pathWithin(res.Items[0].Target, libraryRoot) {
		t.Fatalf("target landed outside category library: %q", res.Items[0].Target)
	}
}

func TestOrganizeDirectoryCreatesMissingCategoryLibraryForVisibility(t *testing.T) {
	root := t.TempDir()
	srcRoot := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	source := filepath.Join(srcRoot, "Gourd.Brothers.S01E01.2026.1080p.mkv")
	target := filepath.Join(dest, "电视剧", "未分类", "Gourd Brothers", "Season 01", "Gourd Brothers - S01E01.mkv")
	writeOrgFile(t, source, "source")
	writeOrgFile(t, target, "already-there")

	repos := newOrganizerTestRepo(t)
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:           srcRoot,
		DestPath:             dest,
		MediaType:            "tv",
		MediaCategory:        "未分类",
		TransferMode:         TransferCopy,
		AllowReplaceExisting: false,
	})
	if err != nil {
		t.Fatalf("organize missing category: %v", err)
	}
	if res.Organized != 0 || res.Skipped != 1 || len(res.Items) != 1 || res.Items[0].Reason != organizeSkipTargetExists {
		t.Fatalf("result = %+v, want skipped target exists", res)
	}

	var lib model.Library
	if err := repos.DB.Where("path = ?", filepath.Join(dest, "电视剧", "未分类")).First(&lib).Error; err != nil {
		t.Fatalf("missing auto-created category library: %v", err)
	}
	if lib.Name != "未分类" || lib.Type != "tv" || !lib.Enabled {
		t.Fatalf("auto-created library = %+v, want enabled tv 未分类", lib)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	scans := scanner.ScanLibrariesForPath(t.Context(), res.DestPath, "")
	added := 0
	for _, scan := range scans {
		if scan.Error != "" {
			t.Fatalf("scan failed: %#v", scan)
		}
		added += scan.Added
	}
	if added != 1 {
		t.Fatalf("scan added = %d, want 1; scans=%#v", added, scans)
	}
}

func TestOrganizeDirectorySmartClassifiesUncategorizedSources(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "流浪地球2.2023.2160p.WEB-DL.mkv"), "cn-movie")
	writeOrgFile(t, filepath.Join(src, "Dune.2021.2160p.WEB-DL.mkv"), "foreign-movie")
	writeOrgFile(t, filepath.Join(src, "狂飙.S01E01.2023.1080p.WEB-DL.mkv"), "cn-tv")
	writeOrgFile(t, filepath.Join(src, "The.Last.of.Us.S01E01.2023.1080p.WEB-DL.mkv"), "western-tv")

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organizer.smart_classify", "true"); err != nil {
		t.Fatal(err)
	}
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 4 {
		t.Fatalf("organized = %d, want 4; result=%+v", res.Organized, res)
	}

	for _, want := range []string{
		filepath.Join(dest, "电影", "华语电影", "流浪地球2 (2023)", "流浪地球2 (2023).mkv"),
		filepath.Join(dest, "电影", "外语电影", "Dune (2021)", "Dune (2021).mkv"),
		filepath.Join(dest, "电视剧", "国产剧", "狂飙", "Season 01", "狂飙 - S01E01.mkv"),
		filepath.Join(dest, "电视剧", "欧美剧", "The Last Of Us", "Season 01", "The Last Of Us - S01E01.mkv"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("expected smart classified file at %q: %v; items=%+v", want, err, res.Items)
		}
	}
}

func TestOrganizeDirectorySmartClassifiesWithLocalNFO(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "Some.Show.S01E01.2024.1080p.mkv"), "jp-anime")
	writeOrgFile(t, filepath.Join(src, "tvshow.nfo"), `<tvshow>
  <title>Some Show</title>
  <genre>Animation</genre>
  <country>JP</country>
  <language>ja</language>
</tvshow>`)

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organizer.smart_classify", "true"); err != nil {
		t.Fatal(err)
	}
	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1", res.Organized)
	}
	want := filepath.Join(dest, "电视剧", "日番", "Some Show", "Season 01", "Some Show - S01E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected NFO classified episode at %q: %v", want, err)
	}
}

func TestOrganizeDirectoryScanAfterRecursesNestedDownloadFolders(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "国产剧", "子目录", "狂飙.S01E01.2023.1080p.WEB-DL.mkv"), "kuangbiao-e01")
	writeOrgFile(t, filepath.Join(src, "华语电影", "更深", "流浪地球2.2023.2160p.WEB-DL.H265.mkv"), "wandering-earth-2")

	repos := newOrganizerTestRepo(t)
	tvLib := model.Library{Name: "国产剧", Path: filepath.Join(dest, "电视剧", "国产剧"), Type: "tv", Enabled: true}
	movieLib := model.Library{Name: "华语电影", Path: filepath.Join(dest, "电影", "华语电影"), Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &tvLib); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &movieLib); err != nil {
		t.Fatal(err)
	}

	org := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := org.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 2 {
		t.Fatalf("organized = %d, want 2", res.Organized)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	scans := scanner.ScanLibrariesForPath(t.Context(), res.DestPath, "")
	if len(scans) != 2 {
		t.Fatalf("scans = %#v, want two matching libraries", scans)
	}
	added := 0
	for _, scan := range scans {
		if scan.Error != "" {
			t.Fatalf("scan failed: %#v", scan)
		}
		added += scan.Added
	}
	if added != 2 {
		t.Fatalf("scan added = %d, want 2", added)
	}
	var count int64
	if err := repos.DB.Model(&model.Media{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("media rows = %d, want 2", count)
	}
}

func TestSelectOrganizeScanTargetsDedupesSamePathByPathType(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "media", "电视剧", "国产剧")
	libraries := []model.Library{
		{Name: "国产剧", Path: path, Type: "movie", Enabled: true},
		{Name: "国产剧", Path: path, Type: "tv", Enabled: true},
	}

	targets := selectOrganizeScanTargets(libraries, filepath.Join(root, "media"), "")
	if len(targets) != 1 {
		t.Fatalf("targets = %#v, want one deduped target", targets)
	}
	if targets[0].Type != "tv" {
		t.Fatalf("target type = %q, want tv", targets[0].Type)
	}
}

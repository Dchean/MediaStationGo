package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAccessibleLibraryPathMapsConfiguredHostMediaDir(t *testing.T) {
	root := t.TempDir()
	hostRoot := filepath.Join(root, "nas", "host", "media")
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", hostRoot)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	got, err := resolveAccessibleLibraryPath(filepath.Join(hostRoot, "电视剧", "国产剧"))
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestResolveAccessibleLibraryPathMapsWindowsDriveBeforeLinuxAbs(t *testing.T) {
	root := t.TempDir()
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `Q:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	for _, input := range []string{
		`Q:\media\电视剧\国产剧`,
		`Q:/media/电视剧/国产剧`,
		`/app/Q:\media\电视剧\国产剧`,
		`/app/Q:/media/电视剧/国产剧`,
	} {
		t.Run(input, func(t *testing.T) {
			got, err := resolveAccessibleLibraryPath(input)
			if err != nil {
				t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
			}
			if got != filepath.Clean(containerLibrary) {
				t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
			}
		})
	}
}

func TestResolveAccessibleLibraryPathRecoversDockerPollutedWindowsDrive(t *testing.T) {
	root := t.TempDir()
	containerRoot := filepath.Join(root, "container", "media")
	containerLibrary := filepath.Join(containerRoot, "电视剧", "国产剧")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `F:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerRoot)

	got, err := resolveAccessibleLibraryPath(`/app/F:\media\电视剧\国产剧`)
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestResolveAccessibleLibraryPathKeepsAccessibleContainerPath(t *testing.T) {
	containerLibrary := filepath.Join(t.TempDir(), "media", "电影")
	if err := os.MkdirAll(containerLibrary, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveAccessibleLibraryPath(containerLibrary)
	if err != nil {
		t.Fatalf("resolveAccessibleLibraryPath() error = %v", err)
	}
	if got != filepath.Clean(containerLibrary) {
		t.Fatalf("resolveAccessibleLibraryPath() = %q, want %q", got, filepath.Clean(containerLibrary))
	}
}

func TestInferLibraryKindFromCategoryPathOverridesMovieDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "国产剧", path: `/media/电视剧/国产剧`, want: "tv"},
		{name: "日漫", path: `/media/电视剧/日漫`, want: "anime"},
		{name: "综艺", path: `/media/电视剧/综艺`, want: "variety"},
		{name: "成人", path: `/media/成人`, want: "adult"},
		{name: "动画电影", path: `/media/电影/动画电影`, want: "movie"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inferLibraryKind(tc.name, tc.path, "movie"); got != tc.want {
				t.Fatalf("inferLibraryKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMappedPathCandidatesMapWindowsDriveDownloadMarker(t *testing.T) {
	root := t.TempDir()
	containerDownloads := filepath.Join(root, "container", "downloads")
	containerLibrary := filepath.Join(containerDownloads, "国产剧")
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", containerDownloads)

	want := filepath.Clean(containerLibrary)
	for _, got := range mappedPathCandidates(`F:\downloads\国产剧`) {
		if got == want {
			return
		}
	}
	t.Fatalf("mappedPathCandidates() missing %q", want)
}

func TestResolveMappedDestinationPathPrefersConfiguredContainerMapping(t *testing.T) {
	root := t.TempDir()
	containerMedia := filepath.Join(root, "container", "media")
	if err := os.MkdirAll(containerMedia, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_MEDIA_DIR", `Q:\media`)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", containerMedia)

	for _, input := range []string{`Q:\media`, `Q:/media`, `/app/Q:\media`} {
		t.Run(input, func(t *testing.T) {
			got := resolveMappedDestinationPath(input)
			if got != filepath.Clean(containerMedia) {
				t.Fatalf("resolveMappedDestinationPath() = %q, want %q", got, filepath.Clean(containerMedia))
			}
		})
	}
}

func TestResolveAccessibleMappedPathMapsWindowsDownloadVariants(t *testing.T) {
	root := t.TempDir()
	containerDownloads := filepath.Join(root, "container", "downloads")
	containerSource := filepath.Join(containerDownloads, "国产剧")
	if err := os.MkdirAll(containerSource, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEDIASTATION_DOWNLOAD_DIR", `Q:\downloads`)
	t.Setenv("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", containerDownloads)

	for _, input := range []string{`Q:\downloads\国产剧`, `Q:/downloads/国产剧`, `/app/Q:\downloads\国产剧`} {
		t.Run(input, func(t *testing.T) {
			got, info, err := resolveAccessibleMappedPath(input)
			if err != nil {
				t.Fatalf("resolveAccessibleMappedPath() error = %v", err)
			}
			if !info.IsDir() {
				t.Fatalf("resolved path is not dir")
			}
			if got != filepath.Clean(containerSource) {
				t.Fatalf("resolveAccessibleMappedPath() = %q, want %q", got, filepath.Clean(containerSource))
			}
		})
	}
}

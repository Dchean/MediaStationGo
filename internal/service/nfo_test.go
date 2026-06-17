package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestWriteMediaNFOUsesMappedDestinationPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIASTATION_MEDIA_DIR", root)
	t.Setenv("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media")
	mediaPath := filepath.Join(root, "电影", "测试电影.mkv")
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("media"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := WriteMediaNFO(&model.Media{
		Title: "测试电影",
		Path:  "/media/电影/测试电影.mkv",
		Year:  2026,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "电影", "测试电影.nfo")
	if got != want {
		t.Fatalf("nfo path = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

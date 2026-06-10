package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestSchedulerOrganizeSourceDisabledByDefault(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "国产剧", "狂飙.S01E01.2023.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	repos := newOrganizerTestRepo(t)
	if err := repos.Setting.Set(t.Context(), "organize.source_dir", src); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organize.target_dir", dest); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "organize.transfer_mode", "copy"); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, organizer, nil, NewHub(zap.NewNop()), "")
	if err := scheduler.jobOrganizeSource(t.Context()); err != nil {
		t.Fatalf("disabled organize source job should be a no-op: %v", err)
	}

	want := filepath.Join(dest, "电视剧", "国产剧", "狂飙", "Season 01", "狂飙 - S01E01.mkv")
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("disabled job should not create %q, stat err=%v", want, err)
	}
}

func TestSchedulerOrganizeSourceUsesConfiguredSourceAndDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "国产剧", "狂飙.S01E01.2023.1080p.mkv"), "episode")

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organize.auto":          "true",
		"organize.source_dir":    src,
		"organize.target_dir":    dest,
		"organize.transfer_mode": "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, organizer, nil, NewHub(zap.NewNop()), "")
	if err := scheduler.jobOrganizeSource(t.Context()); err != nil {
		t.Fatalf("organize source job: %v", err)
	}

	want := filepath.Join(dest, "电视剧", "国产剧", "狂飙", "Season 01", "狂飙 - S01E01.mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected scheduled organize output at %q: %v", want, err)
	}
}

func TestSchedulerRunNowOrganizeSourceBypassesDisabledSwitch(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	writeOrgFile(t, filepath.Join(src, "Dune 2021 1080p.mkv"), "movie")

	repos := newOrganizerTestRepo(t)
	for key, value := range map[string]string{
		"organize.source_dir":    src,
		"organize.target_dir":    dest,
		"organize.transfer_mode": "copy",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, organizer, nil, NewHub(zap.NewNop()), "")
	scheduler.jobs = []*scheduledJob{{
		name:     "organize_source",
		interval: time.Minute,
		run:      scheduler.jobOrganizeSource,
	}}

	if err := scheduler.RunNow(t.Context(), "organize_source"); err != nil {
		t.Fatalf("run now organize source: %v", err)
	}

	want := filepath.Join(dest, "电影", "Dune (2021)", "Dune (2021).mkv")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected run-now organize output at %q: %v", want, err)
	}
}

func TestSchedulerCloudSyncImportsMountedCloudLibrary(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file/sort" || r.URL.Query().Get("pdir_fid") != "0" {
			t.Fatalf("unexpected cloud list request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"status":200,"code":0,"data":{"list":[
			{"fid":"f1","file_name":"Cloud.Movie.2026.mkv","dir":false,"size":1024}
		]}}`))
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
	lib := model.Library{Name: "夸克网盘", Path: "cloud://quark/0", Type: "movie", Enabled: true}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}
	scanner := NewScannerService(&config.Config{}, log, repos, NewHub(log), nil, nil)
	scanner.SetStorageConfig(storage)
	scheduler := NewSchedulerService(log, repos, scanner, nil, nil, storage, NewHub(log), "")

	if err := scheduler.jobSyncCloudLibraries(t.Context()); err != nil {
		t.Fatalf("cloud sync: %v", err)
	}
	var media model.Media
	if err := repos.DB.First(&media, "path = ?", "cloud://quark/Cloud.Movie.2026.mkv").Error; err != nil {
		t.Fatalf("cloud media not imported: %v", err)
	}
	if media.STRMURL != "/api/cloud/play/quark?ref=f1" {
		t.Fatalf("strm url = %q", media.STRMURL)
	}
}

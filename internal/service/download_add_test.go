package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestPublicDownloadTitleUsesMagnetDisplayName(t *testing.T) {
	got := publicDownloadTitle("magnet:?xt=urn:btih:abc&dn=%E6%B5%8B%E8%AF%95%E5%BD%B1%E7%89%87")
	if got != "测试影片" {
		t.Fatalf("publicDownloadTitle = %q, want %q", got, "测试影片")
	}
}

func configureTestDefaultQB(t *testing.T, repos *repository.Container, baseURL string) {
	t.Helper()
	if err := repos.DownloadClient.Create(t.Context(), &model.DownloadClient{
		Name:      "qB test",
		Type:      "qbittorrent",
		Host:      baseURL,
		Username:  "admin",
		Password:  "admin",
		IsDefault: true,
		Enabled:   true,
	}); err != nil {
		t.Fatalf("create default qB client: %v", err)
	}
	if err := repos.Setting.Set(t.Context(), settingDownloadClientsManaged, "true"); err != nil {
		t.Fatalf("mark download clients managed: %v", err)
	}
}

func TestAddDownloadWithMetaSkipsExistingTaskBeforeQBAdd(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	existing := &model.DownloadTask{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://pt.example/download?id=old&passkey=old",
		Title:    "Some Show S01E01 1080p",
		SavePath: "/downloads/tv",
		Status:   "completed",
		Progress: 1,
	}
	if err := repos.Download.Create(t.Context(), existing); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	svc.qb.Configure(QBitConfig{BaseURL: qb.URL, Username: "admin", Password: "admin"})
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "https://pt.example/download?id=new&passkey=new", "/downloads/tv", DownloadTaskMeta{
		Title: "Some Show S01E01 2160p WEB-DL",
	})
	if !errors.Is(err, ErrDownloadAlreadyExists) {
		t.Fatalf("err = %v, want ErrDownloadAlreadyExists", err)
	}
	if task == nil || task.ID != existing.ID {
		t.Fatalf("task = %#v, want existing task %#v", task, existing)
	}
	if got := atomic.LoadInt32(&addCalls); got != 0 {
		t.Fatalf("qb add calls = %d, want 0", got)
	}
}

func TestAddDownloadWithMetaSkipsUserDeletedTaskBeforeQBAdd(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.Media{}, &model.Setting{})
	repos := repository.New(db)
	if err := repos.Setting.Set(t.Context(), "qbittorrent.url", qb.URL); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.username", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), "qbittorrent.password", "admin"); err != nil {
		t.Fatal(err)
	}
	existing := &model.DownloadTask{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://pt.example/download?id=old&passkey=old",
		Title:    "User Deleted Show S01E01 1080p",
		SavePath: "/downloads/tv",
		Status:   "deleted",
	}
	if err := repos.Download.Create(t.Context(), existing); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "https://pt.example/download?id=new&passkey=new", "/downloads/tv", DownloadTaskMeta{
		Title: "User Deleted Show S01E01 1080p WEB-DL",
	})
	if !errors.Is(err, ErrDownloadAlreadyExists) {
		t.Fatalf("err = %v, want ErrDownloadAlreadyExists", err)
	}
	if task == nil || task.ID != existing.ID {
		t.Fatalf("task = %#v, want existing task %#v", task, existing)
	}
	if got := atomic.LoadInt32(&addCalls); got != 0 {
		t.Fatalf("qb add calls = %d, want 0", got)
	}
}

func TestDeleteMarksMatchingDownloadTaskDeleted(t *testing.T) {
	const hash = "abc123"
	const title = "Delete Marker Show S01E01 1080p"
	var deleteCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[{"hash":"abc123","name":"Delete Marker Show S01E01 1080p","state":"downloading","progress":0.5}]`))
		case "/api/v2/torrents/delete":
			atomic.AddInt32(&deleteCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	task := &model.DownloadTask{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://pt.example/download?id=1",
		Title:    title,
		SavePath: "/downloads/tv",
		Status:   "downloading",
		Progress: 0.5,
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	if err := svc.ReloadConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(t.Context(), hash, false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&deleteCalls); got != 1 {
		t.Fatalf("delete calls = %d, want 1", got)
	}

	var updated model.DownloadTask
	if err := db.Where("id = ?", task.ID).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != "deleted" {
		t.Fatalf("status = %q, want deleted", updated.Status)
	}
}

func TestDeleteMarksMagnetTaskDeletedWhenLiveTorrentNameMissing(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef0123c0de"
	var deleteCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/delete":
			atomic.AddInt32(&deleteCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	task := &model.DownloadTask{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "magnet:?xt=urn:btih:" + hash + "&dn=Codex.Path.Verify.S01E01.2026",
		Title:    "Codex Path Verify S01E01 2026",
		SavePath: "/downloads/tv",
		Status:   "queued",
	}
	if err := repos.Download.Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	if err := svc.ReloadConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(t.Context(), hash, false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&deleteCalls); got != 1 {
		t.Fatalf("delete calls = %d, want 1", got)
	}

	var updated model.DownloadTask
	if err := db.Where("id = ?", task.ID).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Status != "deleted" {
		t.Fatalf("status = %q, want deleted", updated.Status)
	}
}

func TestAddDownloadWithMetaSkipsExistingLocalMovieBeforeQBAdd(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	if err := db.Create(&model.Media{
		Title: "Inception",
		Path:  "/media/movies/Inception (2010)/Inception (2010).mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:cccccccccccccccccccccccccccccccccccccccc&dn=Inception+2010+1080p", "/downloads", DownloadTaskMeta{
		Title: "Inception 2010 1080p WEB-DL",
	})
	if !errors.Is(err, ErrMediaAlreadyInLibrary) {
		t.Fatalf("err = %v, want ErrMediaAlreadyInLibrary", err)
	}
	if task != nil {
		t.Fatalf("task = %#v, want nil because local media already exists", task)
	}
	rows, err := repos.Download.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("download rows = %d, want 0", len(rows))
	}
}

func TestAddDownloadWithMetaSkipsExistingLocalEpisodeBeforeQBAdd(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	if err := db.Create(&model.Media{
		Title:      "Some Show",
		Path:       "/media/tv/Some Show/Season 01/Some Show - S01E01.mkv",
		SeasonNum:  1,
		EpisodeNum: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:dddddddddddddddddddddddddddddddddddddddd&dn=Some+Show+S01E01", "/downloads", DownloadTaskMeta{
		Title: "Some Show S01E01 2160p WEB-DL",
	})
	if !errors.Is(err, ErrMediaAlreadyInLibrary) {
		t.Fatalf("err = %v, want ErrMediaAlreadyInLibrary", err)
	}
	if task != nil {
		t.Fatalf("task = %#v, want nil because local episode already exists", task)
	}
	rows, err := repos.Download.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("download rows = %d, want 0", len(rows))
	}
}

func TestAddDownloadWithMetaQueuesEpisodeRangeWhenOnlyPartlyInLibrary(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			http.Error(w, "temporary list unavailable", http.StatusInternalServerError)
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.Media{}, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	if err := db.Create(&model.Media{
		Title:      "Archives The Nanyang Mystery",
		Path:       "/media/tv/Archives The Nanyang Mystery/Season 01/Archives The Nanyang Mystery - S01E07.mkv",
		SeasonNum:  1,
		EpisodeNum: 7,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:abababababababababababababababababababab&dn=Archives+The+Nanyang+Mystery+2026+S01E07-S01E08", "/downloads", DownloadTaskMeta{
		Title: "Archives The Nanyang Mystery 2026 S01E07-S01E08 2160p WEB-DL",
	})
	if err != nil {
		t.Fatalf("AddDownloadWithMeta returned %v, want queued because E08 is missing", err)
	}
	if task == nil {
		t.Fatal("task = nil, want queued task")
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
}

func TestAddDownloadWithMetaSkipsEpisodeRangeWhenFullyInLibrary(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	for _, episode := range []int{7, 8} {
		if err := db.Create(&model.Media{
			Title:      "Archives The Nanyang Mystery",
			Path:       filepath.Join("/media/tv/Archives The Nanyang Mystery/Season 01", fmt.Sprintf("Archives The Nanyang Mystery - S01E%02d.mkv", episode)),
			SeasonNum:  1,
			EpisodeNum: episode,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:babababababababababababababababababababa&dn=Archives+The+Nanyang+Mystery+2026+S01E07-S01E08", "/downloads", DownloadTaskMeta{
		Title: "Archives The Nanyang Mystery 2026 S01E07-S01E08 2160p WEB-DL",
	})
	if !errors.Is(err, ErrMediaAlreadyInLibrary) {
		t.Fatalf("err = %v, want ErrMediaAlreadyInLibrary", err)
	}
	if task != nil {
		t.Fatalf("task = %#v, want nil", task)
	}
}

func TestAddDownloadWithMetaDoesNotDedupRangeAgainstSingleEpisodeTask(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			http.Error(w, "temporary list unavailable", http.StatusInternalServerError)
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	if err := repos.Download.Create(t.Context(), &model.DownloadTask{
		UserID:   "u1",
		Source:   "qbittorrent",
		URL:      "https://pt.example/download?id=old",
		Title:    "Archives The Nanyang Mystery 2026 S01E07 2160p WEB-DL",
		SavePath: "/downloads/tv",
		Status:   "completed",
		Progress: 1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd&dn=Archives+The+Nanyang+Mystery+2026+S01E07-S01E08", "/downloads", DownloadTaskMeta{
		Title: "Archives The Nanyang Mystery 2026 S01E07-S01E08 2160p WEB-DL",
	})
	if err != nil {
		t.Fatalf("AddDownloadWithMeta returned %v, want queued because existing task covers only E07", err)
	}
	if task == nil {
		t.Fatal("task = nil, want queued task")
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
}

func TestAddDownloadWithMetaDoesNotDedupRangeAgainstSeasonOnlyTask(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			http.Error(w, "temporary list unavailable", http.StatusInternalServerError)
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	if err := repos.Download.Create(t.Context(), &model.DownloadTask{
		UserID:         "u1",
		SubscriptionID: "sub-nanyang",
		Source:         "qbittorrent",
		URL:            "https://pt.example/download?id=old-season",
		Title:          "Archives The Nanyang Mystery 2026 S01 2160p WEB-DL",
		SavePath:       "/downloads/tv",
		Status:         "completed",
		Progress:       1,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:abababababababababababababababababababab&dn=Archives+The+Nanyang+Mystery+2026+S01E09-E10", "/downloads/tv", DownloadTaskMeta{
		SubscriptionID: "sub-nanyang",
		Title:          "Archives The Nanyang Mystery 2026 S01E09-E10 2160p WEB-DL",
	})
	if err != nil {
		t.Fatalf("AddDownloadWithMeta returned %v, want queued because season-only task does not prove E09-E10 exists", err)
	}
	if task == nil {
		t.Fatal("task = nil, want queued task")
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
}

func TestDownloadTitleCoversRequestKeepsCompletePackDedup(t *testing.T) {
	if !downloadTitleCoversRequest("Archives The Nanyang Mystery 2026 S01 Complete 2160p WEB-DL", "Archives The Nanyang Mystery 2026 S01E09-E10 2160p WEB-DL") {
		t.Fatal("complete pack should cover requested episode range")
	}
	if downloadTitleCoversRequest("Archives The Nanyang Mystery 2026 S01 2160p WEB-DL", "Archives The Nanyang Mystery 2026 S01E09-E10 2160p WEB-DL") {
		t.Fatal("season-only title must not cover requested episode range")
	}
}

func TestAddDownloadWithMetaScopesSubscriptionDedupBySubscriptionOrSavePath(t *testing.T) {
	var addCalls int32
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			http.Error(w, "temporary list unavailable", http.StatusInternalServerError)
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadTask{}, &model.DownloadClient{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	for _, existing := range []model.DownloadTask{
		{
			UserID:         "u1",
			SubscriptionID: "other-subscription",
			Source:         "qbittorrent",
			URL:            "https://pt.example/download?id=old-sub",
			Title:          "Archives The Nanyang Mystery 2026 S01E07-S01E08 2160p WEB-DL",
			SavePath:       "/downloads/other",
			Status:         "completed",
			Progress:       1,
		},
		{
			UserID:   "u1",
			Source:   "qbittorrent",
			URL:      "https://pt.example/download?id=old-manual",
			Title:    "Archives The Nanyang Mystery 2026 S01E09-S01E10 2160p WEB-DL",
			SavePath: "/downloads/archive",
			Status:   "completed",
			Progress: 1,
		},
	} {
		row := existing
		if err := repos.Download.Create(t.Context(), &row); err != nil {
			t.Fatal(err)
		}
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:efefefefefefefefefefefefefefefefefefefef&dn=Archives+The+Nanyang+Mystery+2026+S01E07-S01E08", "/downloads/tv", DownloadTaskMeta{
		SubscriptionID: "current-subscription",
		Title:          "Archives The Nanyang Mystery 2026 S01E07-S01E08 2160p WEB-DL",
	})
	if err != nil {
		t.Fatalf("AddDownloadWithMeta returned %v, want queued because old task is outside current subscription scope", err)
	}
	if task == nil {
		t.Fatal("task = nil, want queued task")
	}
	if got := atomic.LoadInt32(&addCalls); got != 1 {
		t.Fatalf("qb add calls = %d, want 1", got)
	}
}

func TestAddDownloadWithMetaAutoClassifiesSavePathAndQBitCategory(t *testing.T) {
	var addCalls int32
	var gotSavePath string
	var gotCategory string
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if atomic.LoadInt32(&addCalls) > 0 {
				_, _ = w.Write([]byte(`[{"hash":"auto123","name":"声生不息 S01E01","state":"downloading","progress":0.1}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			if err := r.ParseMultipartForm(1024 * 1024); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotSavePath = r.FormValue("savepath")
			gotCategory = r.FormValue("category")
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadClient{}, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	if err := repos.Setting.Set(t.Context(), "qbittorrent.savepath", "/downloads"); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee&dn=%E5%A3%B0%E7%94%9F%E4%B8%8D%E6%81%AF+S01E01", "", DownloadTaskMeta{
		Title:          "声生不息 S01E01",
		SourceCategory: "综艺",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join("/downloads", "综艺")
	if task.SavePath != wantPath {
		t.Fatalf("task save path = %q, want %q", task.SavePath, wantPath)
	}
	if gotSavePath != wantPath {
		t.Fatalf("qb savepath = %q, want %q", gotSavePath, wantPath)
	}
	if gotCategory != "综艺" {
		t.Fatalf("qb category = %q, want 综艺", gotCategory)
	}
}

func TestDownloadSavePathCategoryRootKeepsWindowsClientSeparators(t *testing.T) {
	if got := downloadSavePathCategoryRoot(`F:\downloads`, "国产剧"); got != `F:\downloads\国产剧` {
		t.Fatalf("downloadSavePathCategoryRoot() = %q, want Windows qB path", got)
	}
	if got := downloadSavePathCategoryRoot(`F:\downloads\国产剧`, "国产剧"); got != `F:\downloads\国产剧` {
		t.Fatalf("downloadSavePathCategoryRoot() duplicated category: %q", got)
	}
	if got := downloadSavePathCategoryRoot(`/downloads`, "国产剧"); got != filepath.Join(`/downloads`, "国产剧") {
		t.Fatalf("downloadSavePathCategoryRoot() = %q, want local path", got)
	}
}

func TestTranslateClientPathMapsWindowsQBitPathToContainerDownloadPath(t *testing.T) {
	root := t.TempDir()
	containerDownloads := filepath.Join(root, "downloads")
	want := filepath.Join(containerDownloads, "国产剧", "Show.S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := translateClientPath(`F:\downloads\国产剧\Show.S01E01.mkv`, map[string]string{
		`F:\downloads`: containerDownloads,
	})
	if got != want {
		t.Fatalf("translateClientPath() = %q, want %q", got, want)
	}
}

func TestAddDownloadWithMetaCanDisableAutoClassifiedSavePath(t *testing.T) {
	var addCalls int32
	var gotSavePath string
	var gotCategory string
	qb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if atomic.LoadInt32(&addCalls) > 0 {
				_, _ = w.Write([]byte(`[{"hash":"auto456","name":"声生不息 S01E01","state":"downloading","progress":0.1}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/add":
			atomic.AddInt32(&addCalls, 1)
			if err := r.ParseMultipartForm(1024 * 1024); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotSavePath = r.FormValue("savepath")
			gotCategory = r.FormValue("category")
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer qb.Close()

	db := newServiceTestDB(t, &model.DownloadClient{}, &model.DownloadTask{}, &model.Setting{})
	repos := repository.New(db)
	configureTestDefaultQB(t, repos, qb.URL)
	if err := repos.Setting.Set(t.Context(), "qbittorrent.savepath", "/downloads"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(t.Context(), DownloadSmartClassifySettingKey, "false"); err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:ffffffffffffffffffffffffffffffffffffffff&dn=%E5%A3%B0%E7%94%9F%E4%B8%8D%E6%81%AF+S01E01", "", DownloadTaskMeta{
		Title:          "声生不息 S01E01",
		SourceCategory: "综艺",
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.SavePath != "/downloads" {
		t.Fatalf("task save path = %q, want /downloads", task.SavePath)
	}
	if gotSavePath != "/downloads" {
		t.Fatalf("qb savepath = %q, want /downloads", gotSavePath)
	}
	if gotCategory != "" {
		t.Fatalf("qb category = %q, want empty", gotCategory)
	}
}

func TestAddDownloadWithMetaSkipsExistingLocalEpisodeWithReleaseGroup(t *testing.T) {
	db := newServiceTestDB(t, &model.Media{}, &model.DownloadTask{}, &model.Setting{}, &model.DownloadClient{})
	repos := repository.New(db)
	if err := db.Create(&model.Media{
		Title:      "凡人修仙传",
		Path:       "/media/动漫/国漫/凡人修仙传/Season 01/凡人修仙传 - S01E146.mkv",
		SeasonNum:  1,
		EpisodeNum: 146,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewDownloadService(zap.NewNop(), repos, NewHub(zap.NewNop()), nil)
	task, err := svc.AddDownloadWithMeta(t.Context(), "u1", "magnet:?xt=urn:btih:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee&dn=%5BMagicStar%5D+%E5%87%A1%E4%BA%BA%E4%BF%AE%E4%BB%99%E4%BC%A0+%E5%B9%B4%E7%95%AA+-+146+%5B1080p%5D", "/downloads", DownloadTaskMeta{
		Title: "[MagicStar] 凡人修仙传 年番 - 146 [1080p][WEB-DL]",
	})
	if !errors.Is(err, ErrMediaAlreadyInLibrary) {
		t.Fatalf("err = %v, want ErrMediaAlreadyInLibrary", err)
	}
	if task != nil {
		t.Fatalf("task = %#v, want nil", task)
	}
	rows, err := repos.Download.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("download rows = %d, want 0", len(rows))
	}
}

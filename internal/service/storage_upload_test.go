package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestStorageConfigUploadLocalToAlist(t *testing.T) {
	var uploaded []string
	var authHeaders []string
	alist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/mkdir":
			_ = json.NewDecoder(r.Body).Decode(&map[string]string{})
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		case "/api/fs/get":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
		case "/api/fs/put":
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			decoded, err := url.PathUnescape(r.Header.Get("File-Path"))
			if err != nil {
				t.Fatalf("decode file path: %v", err)
			}
			uploaded = append(uploaded, decoded)
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		default:
			t.Fatalf("unexpected alist path %s", r.URL.Path)
		}
	}))
	defer alist.Close()

	repos, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "alist",
		Config: map[string]any{
			"server":           alist.URL,
			"token":            "alist-token",
			"transfer_enabled": "true",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Movie.2026.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "Movie.2026.nfo"), []byte("nfo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ignore.txt"), []byte("txt"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := storage.UploadLocal(t.Context(), CloudUploadInput{
		Type:            "alist",
		SourcePath:      source,
		DestPath:        "/backup",
		Recursive:       true,
		IncludeSidecars: true,
	})
	if err != nil {
		t.Fatalf("upload local: %v", err)
	}
	if res.Uploaded != 2 || res.Skipped != 0 || len(res.Errors) != 0 {
		t.Fatalf("result = %+v", res)
	}
	sort.Strings(uploaded)
	want := []string{"/backup/Movie.2026.mkv", "/backup/Movie.2026.nfo"}
	if strings.Join(uploaded, "\n") != strings.Join(want, "\n") {
		t.Fatalf("uploaded = %#v, want %#v", uploaded, want)
	}
	for _, header := range authHeaders {
		if header != "alist-token" {
			t.Fatalf("authorization header = %q", header)
		}
	}
	if got, _ := repos.StorageConfig.Get(t.Context(), "alist"); got == nil {
		t.Fatalf("storage config should remain saved")
	}
}

func TestStorageConfigUploadLocalToOpenListAPI(t *testing.T) {
	var uploaded []string
	openlist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/mkdir":
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		case "/api/fs/get":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
		case "/api/fs/put":
			if r.Header.Get("Authorization") != "openlist-token" {
				t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
			}
			decoded, err := url.PathUnescape(r.Header.Get("File-Path"))
			if err != nil {
				t.Fatalf("decode file path: %v", err)
			}
			uploaded = append(uploaded, decoded)
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		default:
			t.Fatalf("unexpected openlist path %s", r.URL.Path)
		}
	}))
	defer openlist.Close()

	_, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server":           openlist.URL,
			"token":            "openlist-token",
			"transfer_enabled": "true",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Movie.2026.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := storage.UploadLocal(t.Context(), CloudUploadInput{
		Type:       "openlist",
		SourcePath: source,
		DestPath:   "/OpenList",
		Recursive:  true,
	})
	if err != nil {
		t.Fatalf("upload local: %v", err)
	}
	if res.Uploaded != 1 || len(uploaded) != 1 || uploaded[0] != "/OpenList/Movie.2026.mkv" {
		t.Fatalf("result = %+v uploaded=%#v", res, uploaded)
	}
}

func TestStorageConfigUploadLocalToOpenListAPIWithUsernamePassword(t *testing.T) {
	var loginSeen bool
	var uploaded []string
	openlist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/auth/login":
			loginSeen = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if body["username"] != "alice" || body["password"] != "secret" {
				t.Fatalf("login body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"code":200,"data":{"token":"openlist-session-token"}}`))
		case "/api/fs/mkdir":
			if r.Header.Get("Authorization") != "openlist-session-token" {
				t.Fatalf("mkdir authorization = %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		case "/api/fs/get":
			if r.Header.Get("Authorization") != "openlist-session-token" {
				t.Fatalf("get authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
		case "/api/fs/put":
			if r.Header.Get("Authorization") != "openlist-session-token" {
				t.Fatalf("put authorization = %q", r.Header.Get("Authorization"))
			}
			decoded, err := url.PathUnescape(r.Header.Get("File-Path"))
			if err != nil {
				t.Fatalf("decode file path: %v", err)
			}
			uploaded = append(uploaded, decoded)
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		case "/dav":
			t.Fatal("OpenList username/password upload should use API, not WebDAV")
		default:
			t.Fatalf("unexpected openlist path %s", r.URL.Path)
		}
	}))
	defer openlist.Close()

	_, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server":           openlist.URL,
			"username":         "alice",
			"password":         "secret",
			"transfer_enabled": "true",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Movie.2026.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := storage.UploadLocal(t.Context(), CloudUploadInput{
		Type:       "openlist",
		SourcePath: source,
		DestPath:   "/OpenList",
		Recursive:  true,
	})
	if err != nil {
		t.Fatalf("upload local: %v", err)
	}
	if !loginSeen {
		t.Fatal("expected OpenList API login")
	}
	if res.Uploaded != 1 || len(uploaded) != 1 || uploaded[0] != "/OpenList/Movie.2026.mkv" {
		t.Fatalf("result = %+v uploaded=%#v", res, uploaded)
	}
}

func TestStorageConfigOpenListHTTPSAgainstHTTPHint(t *testing.T) {
	openlist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	defer openlist.Close()

	_, storage := newStorageUploadTestService(t)
	badHTTPS := "https://" + strings.TrimPrefix(openlist.URL, "http://")
	err := storage.Test(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"server": badHTTPS,
		},
	})
	if err == nil {
		t.Fatal("want protocol mismatch error")
	}
	if !strings.Contains(err.Error(), "请改用 http://") || !strings.Contains(err.Error(), "server gave HTTP response to HTTPS client") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStorageConfigCloudProviderRejectsDisabledConfig(t *testing.T) {
	_, storage := newStorageUploadTestService(t)
	enabled := false
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "openlist",
		Config: map[string]any{
			"url": "http://127.0.0.1:5244/dav",
		},
		Enabled: &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := storage.CloudProvider(t.Context(), "openlist")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled provider error = %v, want disabled", err)
	}
}

func TestSchedulerCloudUploadUsesConfiguredLocalSource(t *testing.T) {
	var uploaded []string
	alist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/mkdir":
			_, _ = w.Write([]byte(`{"code":200}`))
		case "/api/fs/get":
			w.WriteHeader(http.StatusNotFound)
		case "/api/fs/put":
			decoded, _ := url.PathUnescape(r.Header.Get("File-Path"))
			uploaded = append(uploaded, decoded)
			_, _ = w.Write([]byte(`{"code":200}`))
		default:
			t.Fatalf("unexpected alist path %s", r.URL.Path)
		}
	}))
	defer alist.Close()

	repos, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "alist",
		Config: map[string]any{
			"server":           alist.URL,
			"token":            "token",
			"transfer_enabled": "true",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Show.S01E01.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		CloudUploadAutoEnabledKey: "true",
		CloudUploadProviderKey:    "alist",
		CloudUploadSourceDirKey:   source,
		CloudUploadDestPathKey:    "/cloud-media",
		CloudUploadRecursiveKey:   "true",
		CloudUploadSidecarsKey:    "false",
	} {
		if err := repos.Setting.Set(t.Context(), key, value); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := NewSchedulerService(zap.NewNop(), repos, nil, nil, nil, storage, NewHub(zap.NewNop()), "")
	if err := scheduler.jobUploadLocalToCloud(t.Context()); err != nil {
		t.Fatalf("cloud upload job: %v", err)
	}
	if len(uploaded) != 1 || uploaded[0] != "/cloud-media/Show.S01E01.mkv" {
		t.Fatalf("uploaded = %#v", uploaded)
	}
}

func TestStorageConfigUploadLocalToCloudDrive2(t *testing.T) {
	var uploaded []string
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			uploaded = append(uploaded, r.URL.Path)
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected method %s %s", r.Method, r.URL.Path)
		}
	}))
	defer dav.Close()

	_, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "clouddrive2",
		Config: map[string]any{
			"url":              dav.URL + "/dav",
			"username":         "user",
			"password":         "pass",
			"transfer_enabled": "true",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Movie.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := storage.UploadLocal(t.Context(), CloudUploadInput{
		Type:       "clouddrive2",
		SourcePath: source,
		DestPath:   "/MediaStationGo",
		Recursive:  true,
	})
	if err != nil {
		t.Fatalf("upload local: %v", err)
	}
	if res.Uploaded != 1 || len(uploaded) != 1 || uploaded[0] != "/dav/MediaStationGo/Movie.mkv" {
		t.Fatalf("result = %+v uploaded=%#v", res, uploaded)
	}
}

func TestStorageConfigUploadLocalRequiresTransferEnabled(t *testing.T) {
	_, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "alist",
		Config: map[string]any{
			"server": "http://alist.test",
			"token":  "token",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "Movie.mkv"), []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := storage.UploadLocal(t.Context(), CloudUploadInput{
		Type:       "alist",
		SourcePath: source,
		DestPath:   "/MediaStationGo",
		Recursive:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "transfer is disabled") {
		t.Fatalf("upload error = %v, want transfer disabled", err)
	}
}

func TestStorageConfigUploadLocalMoveDeletesSourceAfterUpload(t *testing.T) {
	var uploaded []string
	alist := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/mkdir":
			_, _ = w.Write([]byte(`{"code":200}`))
		case "/api/fs/get":
			w.WriteHeader(http.StatusNotFound)
		case "/api/fs/put":
			decoded, _ := url.PathUnescape(r.Header.Get("File-Path"))
			uploaded = append(uploaded, decoded)
			_, _ = w.Write([]byte(`{"code":200}`))
		default:
			t.Fatalf("unexpected alist path %s", r.URL.Path)
		}
	}))
	defer alist.Close()

	_, storage := newStorageUploadTestService(t)
	if _, err := storage.Save(t.Context(), StorageInput{
		Type: "alist",
		Config: map[string]any{
			"server":           alist.URL,
			"token":            "token",
			"transfer_enabled": "true",
			"transfer_mode":    "move",
		},
	}); err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	file := filepath.Join(source, "Movie.mkv")
	if err := os.WriteFile(file, []byte("movie"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := storage.UploadLocal(t.Context(), CloudUploadInput{
		Type:       "alist",
		SourcePath: source,
		DestPath:   "/MediaStationGo",
		Recursive:  true,
	})
	if err != nil {
		t.Fatalf("upload local move: %v", err)
	}
	if res.Uploaded != 1 || res.Moved != 1 || len(uploaded) != 1 {
		t.Fatalf("result = %+v uploaded=%#v", res, uploaded)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("source should be removed after move upload, stat err=%v", err)
	}
}

func newStorageUploadTestService(t *testing.T) (*repository.Container, *StorageConfigService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.StorageConfig{}, &model.Setting{}, &model.Library{}, &model.Media{}); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	log := zap.NewNop()
	return repos, NewStorageConfigService(log, repos, NewCryptoService("", log))
}

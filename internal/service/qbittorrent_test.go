package service

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestQBitLoginUsesMinimalRequestFirst(t *testing.T) {
	var loginAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginAttempts.Add(1)
			if r.Header.Get("Origin") != "" || r.Header.Get("Referer") != "" {
				http.Error(w, "unexpected csrf headers", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	if err := client.Login(context.Background()); err != nil {
		t.Fatalf("expected minimal login to succeed: %v", err)
	}
	if loginAttempts.Load() != 1 {
		t.Fatalf("login attempts = %d, want 1", loginAttempts.Load())
	}
}

func TestQBitLoginTimeoutSuggestsDockerHostAddress(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		}),
	}

	err := qbitLogin(context.Background(), client, "http://192.168.1.125:8085", "admin", "adminadmin")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	msg := err.Error()
	for _, want := range []string{"连接 http://192.168.1.125:8085 超时", "host.docker.internal", "172.17.0.1"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("timeout hint %q missing %q", msg, want)
		}
	}
}

func TestQBitLoginRetriesWithRefererWhenRequired(t *testing.T) {
	var loginAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginAttempts.Add(1)
			if r.Header.Get("Referer") == "" {
				http.Error(w, "missing referer", http.StatusForbidden)
				return
			}
			if r.Header.Get("Origin") != "" {
				http.Error(w, "origin blocked", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{Jar: jar}
	if err := qbitLogin(context.Background(), httpClient, server.URL, "admin", "adminadmin"); err != nil {
		t.Fatalf("expected referer retry to succeed: %v", err)
	}
	if loginAttempts.Load() != 2 {
		t.Fatalf("login attempts = %d, want 2", loginAttempts.Load())
	}
}

func TestQBitLoginAcceptsNoContentFromNewerWebUI(t *testing.T) {
	var loginAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			loginAttempts.Add(1)
			if r.Header.Get("Referer") == "" || r.Header.Get("Origin") == "" {
				http.Error(w, "csrf headers required", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	httpClient := &http.Client{Jar: jar}
	if err := qbitLogin(context.Background(), httpClient, server.URL, "admin", "adminadmin"); err != nil {
		t.Fatalf("expected 204 login response to succeed: %v", err)
	}
	if loginAttempts.Load() != 3 {
		t.Fatalf("login attempts = %d, want 3", loginAttempts.Load())
	}
}

func TestQBitAddTorrentRequiresVisibleNewTask(t *testing.T) {
	oldAttempts := qbitAddVerifyAttempts
	oldInterval := qbitAddVerifyInterval
	qbitAddVerifyAttempts = 2
	qbitAddVerifyInterval = time.Millisecond
	defer func() {
		qbitAddVerifyAttempts = oldAttempts
		qbitAddVerifyInterval = oldInterval
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	err := client.AddTorrent(context.Background(), server.URL+"/missing.torrent", "")
	if err == nil {
		t.Fatal("expected add to fail when no new torrent appears")
	}
	if !strings.Contains(err.Error(), "下载器未出现新任务") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQBitAddTorrentUploadsFetchedTorrentFile(t *testing.T) {
	var added atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/fixture.torrent":
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write([]byte("d4:infod4:name7:fixtureee"))
		case "/api/v2/torrents/add":
			reader, err := r.MultipartReader()
			if err != nil {
				t.Errorf("expected multipart add request: %v", err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			if !multipartHasTorrentFile(reader) {
				t.Error("expected qbit add request to upload torrent file")
				http.Error(w, "missing file", http.StatusBadRequest)
				return
			}
			added.Store(true)
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/info":
			if added.Load() {
				_, _ = w.Write([]byte(`[{"hash":"abc123","name":"fixture"}]`))
				return
			}
			_, _ = w.Write([]byte("[]"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	if err := client.AddTorrent(context.Background(), server.URL+"/fixture.torrent", ""); err != nil {
		t.Fatalf("expected fetched torrent upload to succeed: %v", err)
	}
}

func TestQBitAddTorrentFileReturnsDedupForExistingInfoHash(t *testing.T) {
	torrentData := []byte("d4:infod4:name7:fixtureee")
	hash := torrentInfoHash(torrentData)
	if hash == "" {
		t.Fatal("expected fixture info hash")
	}
	var addCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			addCalled.Store(true)
			_, _ = w.Write([]byte("Fails."))
		case "/api/v2/torrents/info":
			_, _ = w.Write([]byte(`[{"hash":"` + hash + `","name":"fixture"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})

	if err := client.AddTorrentFile(context.Background(), torrentData, "fixture.torrent", ""); !errors.Is(err, ErrDownloadAlreadyExists) {
		t.Fatalf("err = %v, want ErrDownloadAlreadyExists", err)
	}
	if addCalled.Load() {
		t.Fatal("expected qbit add to be skipped for existing infohash")
	}
}

func multipartHasTorrentFile(reader *multipart.Reader) bool {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return false
		}
		if part.FormName() == "torrents" && part.FileName() != "" {
			return true
		}
	}
}

func TestQBitSetLocationPostsHashAndLocation(t *testing.T) {
	var gotHash, gotLocation string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/setLocation":
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			gotHash = r.PostFormValue("hashes")
			gotLocation = r.PostFormValue("location")
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})
	if err := client.SetLocation(context.Background(), "abc123", "/data/media/Movie"); err != nil {
		t.Fatalf("setLocation: %v", err)
	}
	if gotHash != "abc123" {
		t.Fatalf("hashes = %q, want abc123", gotHash)
	}
	if gotLocation != "/data/media/Movie" {
		t.Fatalf("location = %q, want /data/media/Movie", gotLocation)
	}
}

func TestQBitSetLocationSurfacesConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/setLocation":
			http.Error(w, "cannot write", http.StatusConflict)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQBitClient(zap.NewNop(), QBitConfig{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "adminadmin",
	})
	err := client.SetLocation(context.Background(), "abc123", "/data/media/Movie")
	if err == nil {
		t.Fatal("expected error on 409 conflict")
	}
	if !strings.Contains(err.Error(), "无法写入目标路径") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQBitAdapterPauseResumeFallsBackToQBit52Actions(t *testing.T) {
	var pauseCalled, stopCalled, resumeCalled, startCalled atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/pause":
			pauseCalled.Add(1)
			http.NotFound(w, r)
		case "/api/v2/torrents/stop":
			stopCalled.Add(1)
			if r.Header.Get("Origin") != serverOrigin(r) {
				t.Errorf("stop Origin = %q, want %q", r.Header.Get("Origin"), serverOrigin(r))
			}
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/resume":
			resumeCalled.Add(1)
			http.NotFound(w, r)
		case "/api/v2/torrents/start":
			startCalled.Add(1)
			if r.Header.Get("Origin") != serverOrigin(r) {
				t.Errorf("start Origin = %q, want %q", r.Header.Get("Origin"), serverOrigin(r))
			}
			_, _ = w.Write([]byte("Ok."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewQBitAdapter()
	if err := adapter.Initialize(context.Background(), DownloadClientConfig{Host: server.URL, Username: "admin", Password: "adminadmin"}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := adapter.Pause(context.Background(), "abc123"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := adapter.Resume(context.Background(), "abc123"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if pauseCalled.Load() != 1 || stopCalled.Load() != 1 || resumeCalled.Load() != 1 || startCalled.Load() != 1 {
		t.Fatalf("calls pause=%d stop=%d resume=%d start=%d, want all 1",
			pauseCalled.Load(), stopCalled.Load(), resumeCalled.Load(), startCalled.Load())
	}
}

func TestQBitAdapterAddTorrentSendsOriginAndRejectsFailsBody(t *testing.T) {
	var addCalled atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/add":
			addCalled.Add(1)
			if r.Header.Get("Origin") != serverOrigin(r) {
				t.Errorf("Origin = %q, want %q", r.Header.Get("Origin"), serverOrigin(r))
			}
			_, _ = w.Write([]byte("Fails."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := NewQBitAdapter()
	if err := adapter.Initialize(context.Background(), DownloadClientConfig{Host: server.URL, Username: "admin", Password: "adminadmin"}); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := adapter.AddTorrent(context.Background(), "magnet:?xt=urn:btih:abc", "/downloads")
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("expected rejected add error, got %v", err)
	}
	if addCalled.Load() != 1 {
		t.Fatalf("add calls = %d, want 1", addCalled.Load())
	}
}

func serverOrigin(r *http.Request) string {
	return "http://" + r.Host
}

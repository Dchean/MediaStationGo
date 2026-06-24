package service

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestUniqueDiscoverArtworkURLsFiltersDuplicatesAndLimits(t *testing.T) {
	urls := uniqueDiscoverArtworkURLs([]string{
		"",
		"/local/poster.jpg",
		"https://image.tmdb.org/t/p/w500/a.jpg",
		"https://image.tmdb.org/t/p/w500/a.jpg",
		"https://image.tmdb.org/t/p/w500/b.jpg",
		"https://image.tmdb.org/t/p/w500/c.jpg",
	}, 2)
	if len(urls) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(urls), urls)
	}
	if urls[0] != "https://image.tmdb.org/t/p/w500/a.jpg" || urls[1] != "https://image.tmdb.org/t/p/w500/b.jpg" {
		t.Fatalf("urls = %v", urls)
	}
}

func TestDiscoverWarmExternalArtworkPrefetchesAndCaches(t *testing.T) {
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	var calls int32
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	discover := NewDiscoverService(zap.NewNop(), nil).SetImageProxy(proxy)
	poster := "https://image.tmdb.org/t/p/w500/discover-poster.jpg"
	backdrop := "https://image.tmdb.org/t/p/w1280/discover-backdrop.jpg"
	queued := discover.WarmExternalArtwork([]ExternalMediaResult{
		{Title: "A", PosterURL: poster, BackdropURL: backdrop},
		{Title: "B", PosterURL: poster},
		{Title: "Local", PosterURL: "/media/poster.jpg"},
	})
	if queued != 2 {
		t.Fatalf("queued = %d, want 2", queued)
	}
	for _, raw := range []string{poster, backdrop} {
		_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
		if err != nil {
			t.Fatal(err)
		}
		waitForDiscoverArtworkCache(t, &calls, 2, cachePath, raw)
	}

	callsAfterCache := atomic.LoadInt32(&calls)
	queued = discover.WarmExternalArtwork([]ExternalMediaResult{{Title: "Cached", PosterURL: poster, BackdropURL: backdrop}})
	if queued != 2 {
		t.Fatalf("queued cached = %d, want 2", queued)
	}
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != callsAfterCache {
		t.Fatalf("cached prefetch should not call upstream again: got %d want %d", got, callsAfterCache)
	}
}

func TestDiscoverWarmArtworkNoImageProxyIsNoop(t *testing.T) {
	discover := NewDiscoverService(zap.NewNop(), nil)
	if got := discover.WarmMatchArtwork([]Match{{PosterURL: "https://image.tmdb.org/t/p/w500/a.jpg"}}); got != 0 {
		t.Fatalf("queued = %d, want 0 without image proxy", got)
	}
}

func waitForDiscoverArtworkCache(t *testing.T, calls *int32, wantCalls int32, cachePath, raw string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(calls) >= wantCalls {
			if _, err := os.Stat(cachePath); err == nil {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected cached artwork %q after %d upstream calls: %v", raw, atomic.LoadInt32(calls), os.ErrNotExist)
}

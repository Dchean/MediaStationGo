package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

var testJPEG = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F',
	0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01,
	0x00, 0x00, 0xff, 0xd9,
}

func TestImageProxyServesLocalImagePath(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "episode-thumb.png")
	if err := os.WriteFile(imagePath, transparent1x1PNG, 0o644); err != nil {
		t.Fatal(err)
	}

	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(dir, "cache")}}, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/api/img", nil)
	rec := httptest.NewRecorder()

	if err := proxy.Serve(t.Context(), rec, req, imagePath); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got == "" {
		t.Fatal("missing content-type")
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("body length = %d, want %d", rec.Body.Len(), len(transparent1x1PNG))
	}
}

// TestImageProxyServesPosterUnderLibraryRoot verifies that sidecar posters
// stored under an arbitrary media library root (not the configured
// data/cache/movies dirs) are served rather than dropped to the placeholder.
// This is the regression that made web/Emby posters disappear.
func TestImageProxyServesPosterUnderLibraryRoot(t *testing.T) {
	libDir := t.TempDir()
	posterPath := filepath.Join(libDir, "Inception (2010)", "poster.png")
	if err := os.MkdirAll(filepath.Dir(posterPath), 0o755); err != nil {
		t.Fatal(err)
	}
	realPoster := testJPEG
	if err := os.WriteFile(posterPath, realPoster, 0o644); err != nil {
		t.Fatal(err)
	}

	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())

	// Without a library-roots provider the poster lives outside every allowed
	// root, so it must fall back to the transparent placeholder.
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), posterPath); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("expected placeholder before provider set, got %d bytes", rec.Body.Len())
	}

	// Once the library root is known, the real poster bytes are served.
	proxy.SetLibraryRootsProvider(func() []string { return []string{libDir} })
	rec = httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), posterPath); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, realPoster) {
		t.Fatalf("served %x, want real poster bytes", got)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected static image ETag")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/img", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, req, posterPath); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("conditional body length = %d, want 0", rec.Body.Len())
	}
}

func TestImageProxyCachesFailedRemoteImageFetch(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
			Request:    req,
		}, nil
	})}
	raw := "https://image.tmdb.org/t/p/w500/poster.jpg"
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.Len() != len(transparent1x1PNG) {
			t.Fatalf("body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
		}
		if got := rec.Header().Get("Cache-Control"); got != imagePlaceholderCacheControl {
			t.Fatalf("Cache-Control = %q, want %q", got, imagePlaceholderCacheControl)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 due to negative cache", got)
	}
}

func TestImageProxyRemoveFailedAllowsRetry(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&calls, 1)
		if call == 1 {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Status:     "502 Bad Gateway",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://image.tmdb.org/t/p/w500/retry-poster.jpg"
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("first body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
	}
	if err := proxy.RemoveFailed(raw); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?v=retry", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("retried body = %x, want poster bytes", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 after retry", got)
	}
}

func TestImageProxyPrefetchRemoteUsesProviderHeaders(t *testing.T) {
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Referer"); got != "https://movie.douban.com/" {
			t.Fatalf("Referer = %q, want Douban movie referer", got)
		}
		if got := req.Header.Get("User-Agent"); !strings.Contains(got, "Mozilla/5.0") {
			t.Fatalf("User-Agent = %q, want browser-like UA", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://img9.doubanio.com/view/photo/s_ratio_poster/public/p2933012346.jpg"
	if err := proxy.PrefetchRemote(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
}

func TestImageProxyRemoveCachedAllowsRefresh(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	raw := "https://image.tmdb.org/t/p/w500/refresh-poster.jpg"
	if err := proxy.PrefetchRemote(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err := proxy.RemoveCached(raw); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls before refresh = %d, want 1", got)
	}
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?refresh=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("upstream calls after refresh = %d, want 2", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestImageProxyRefreshKeepsCachedImageOnUpstreamFailure(t *testing.T) {
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("upstream unavailable")),
			Request:    req,
		}, nil
	})}

	raw := "https://image.tmdb.org/t/p/w500/cached-poster.jpg"
	_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	cachedPoster := testJPEG
	if err := os.WriteFile(cachePath, cachedPoster, 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?refresh=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, cachedPoster) {
		t.Fatalf("body = %x, want cached poster after failed refresh", got)
	}
}

func TestImageProxyRefetchesTransparentPlaceholderCache(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
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

	raw := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p2925358079.jpg"
	_, cachePath, failPath, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, transparent1x1PNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img?retry=1", nil), raw); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("body = %x, want refetched poster bytes", got)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestImageProxyDoesNotCacheNonImageRemoteResponse(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/html"}},
			Body:       io.NopCloser(strings.NewReader("<html>not image</html>")),
			Request:    req,
		}, nil
	})}

	raw := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p-bad.jpg"
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
	}
	_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-image response should not be cached, stat err=%v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestImageProxyDoesNotCacheMislabeledRemoteResponse(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
			Body:       io.NopCloser(strings.NewReader("<html>not a poster</html>")),
			Request:    req,
		}, nil
	})}

	raw := "https://img1.doubanio.com/view/photo/s_ratio_poster/public/p-mislabeled.jpg"
	rec := httptest.NewRecorder()
	if err := proxy.Serve(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/img", nil), raw); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != len(transparent1x1PNG) {
		t.Fatalf("body length = %d, want placeholder %d", rec.Body.Len(), len(transparent1x1PNG))
	}
	_, cachePath, _, err := proxy.remoteImageCachePaths(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mislabeled non-image response should not be cached, stat err=%v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}
}

func TestImageProxyCachesCloudResolvedImage(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	link := &cloud.DirectLink{URL: "http://cloud-provider.invalid/poster.png"}
	if proxy.ServeCloudCached(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/cloud/play/openlist?ref=poster.png", nil), "openlist:poster.png") {
		t.Fatal("ServeCloudCached returned true before the cloud image was cached")
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		if err := proxy.ServeCloudResolved(t.Context(), rec, httptest.NewRequest(http.MethodGet, "/api/cloud/play/openlist?ref=poster.png", nil), "openlist:poster.png", link); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := rec.Header().Get("Cache-Control"); got != imageBrowserCacheControl {
			t.Fatalf("Cache-Control = %q, want %q", got, imageBrowserCacheControl)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 due to cloud image cache", got)
	}

	rec := httptest.NewRecorder()
	if !proxy.ServeCloudCached(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/play/openlist?ref=poster.png", nil), "openlist:poster.png") {
		t.Fatal("ServeCloudCached returned false after the cloud image was cached")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls after ServeCloudCached = %d, want 1", got)
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("cached body = %x, want cached cloud image", got)
	}
}

func TestImageProxyPrefetchCloudResolvedImage(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	proxy.client = &http.Client{Transport: imageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(testJPEG)),
			Request:    req,
		}, nil
	})}

	link := &cloud.DirectLink{URL: "http://cloud-provider.invalid/folder.png"}
	if err := proxy.PrefetchCloudResolved(t.Context(), "openlist:folder.png", link); err != nil {
		t.Fatal(err)
	}
	if err := proxy.PrefetchCloudResolved(t.Context(), "openlist:folder.png", link); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 after prefetch cache hit", got)
	}
	rec := httptest.NewRecorder()
	if !proxy.ServeCloudCached(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/play/openlist?ref=folder.png", nil), "openlist:folder.png") {
		t.Fatal("prefetched cloud image was not served from cache")
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("cached body = %x, want prefetched cloud image", got)
	}
}

func TestImageProxyPrefetchCloudResolvedRefetchesInvalidCache(t *testing.T) {
	var calls int32
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
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

	stableKey := "openlist:bad-cache-poster.jpg"
	_, cachePath, failPath := proxy.cloudImageCachePaths(stableKey)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("<html>old bad cache</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}

	link := &cloud.DirectLink{URL: "http://cloud-provider.invalid/bad-cache-poster.jpg"}
	if err := proxy.PrefetchCloudResolved(t.Context(), stableKey, link); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 after invalid cache cleanup", got)
	}
	rec := httptest.NewRecorder()
	if !proxy.ServeCloudCached(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/play/openlist?ref=bad-cache-poster.jpg", nil), stableKey) {
		t.Fatal("refetched cloud image was not served from cache")
	}
	if got := rec.Body.Bytes(); !bytes.Equal(got, testJPEG) {
		t.Fatalf("cached body = %x, want refetched cloud image", got)
	}
}

func TestImageProxyServeCloudCachedSkipsInvalidCache(t *testing.T) {
	proxy := NewImageProxy(&config.Config{Cache: config.CacheConfig{CacheDir: filepath.Join(t.TempDir(), "cache")}}, zap.NewNop())
	stableKey := "openlist:invalid-cached-poster.jpg"
	_, cachePath, failPath := proxy.cloudImageCachePaths(stableKey)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, transparent1x1PNG, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failPath, []byte("failed"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	if proxy.ServeCloudCached(rec, httptest.NewRequest(http.MethodGet, "/api/cloud/play/openlist?ref=invalid-cached-poster.jpg", nil), stableKey) {
		t.Fatal("ServeCloudCached should skip invalid cached cloud artwork")
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid cache should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(failPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale fail marker should be removed with invalid cache, stat err=%v", err)
	}
}

type imageRoundTripFunc func(*http.Request) (*http.Response, error)

func (f imageRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestIsPrivateHost(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.5", "192.168.1.10", "169.254.169.254", "0.0.0.0", "::1", ""}
	for _, h := range blocked {
		if !isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = false, want true (literal private/loopback IP)", h)
		}
	}
	// Hostnames must NOT be blocked even though GFW DNS poisoning may resolve
	// them to private/loopback IPs — blocking them broke legitimate posters.
	allowed := []string{"image.tmdb.org", "lain.bgm.tv", "example.com", "8.8.8.8"}
	for _, h := range allowed {
		if isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = true, want false", h)
		}
	}
}

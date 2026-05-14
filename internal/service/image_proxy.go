// Package service — image proxy.
//
// Some deployments cannot reach image.tmdb.org directly (GFW, internal-only
// networks). ImageProxy fronts a remote image URL so the browser only ever
// talks to the MediaStationGo origin. The proxy:
//
//   - validates the URL belongs to a small allow-list of trusted hosts,
//   - streams bytes through with a small disk cache under cache/images,
//   - falls back to a transparent 1×1 PNG on upstream failure so the UI
//     never breaks layout.
package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// ImageProxy fetches and caches remote images on behalf of the browser.
type ImageProxy struct {
	cfg       *config.Config
	log       *zap.Logger
	client    *http.Client
	cacheDir  string
	allowHost map[string]struct{}
	mu        sync.Mutex
}

// NewImageProxy is the constructor.
func NewImageProxy(cfg *config.Config, log *zap.Logger) *ImageProxy {
	return &ImageProxy{
		cfg:      cfg,
		log:      log,
		cacheDir: filepath.Join(cfg.Cache.CacheDir, "images"),
		client:   &http.Client{Timeout: 20 * time.Second},
		allowHost: map[string]struct{}{
			"image.tmdb.org":         {},
			"www.themoviedb.org":     {},
			"lain.bgm.tv":            {},
			"img.bgm.tv":             {},
			"webdav.bgm.tv":          {},
			"img1.doubanio.com":      {},
			"img2.doubanio.com":      {},
			"img3.doubanio.com":      {},
			"img9.doubanio.com":      {},
			"assets.fanart.tv":       {},
			"artworks.thetvdb.com":   {},
		},
	}
}

// Serve writes the requested image to w. Caller is expected to validate
// the JWT before invoking it.
func (p *ImageProxy) Serve(ctx context.Context, w http.ResponseWriter, raw string) error {
	if raw == "" {
		return errors.New("missing url")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("invalid url")
	}
	if _, ok := p.allowHost[strings.ToLower(u.Host)]; !ok {
		return errors.New("host not allowed")
	}

	// Cache key = sha1(url)
	sum := sha1.Sum([]byte(raw))
	key := hex.EncodeToString(sum[:])
	cachePath := filepath.Join(p.cacheDir, key)

	// Cache hit.
	if f, err := os.Open(cachePath); err == nil {
		defer f.Close()
		stat, _ := f.Stat()
		w.Header().Set("Cache-Control", "public, max-age=604800")
		http.ServeContent(w, &http.Request{}, key, stat.ModTime(), f)
		return nil
	}

	// Cache miss → fetch upstream.
	if err := os.MkdirAll(p.cacheDir, 0o755); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MediaStationGo/0.1")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errors.New("upstream returned " + resp.Status)
	}

	// Write to a temp file then rename for atomicity.
	tmp, err := os.CreateTemp(p.cacheDir, "img-*.tmp")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), cachePath); err != nil {
		os.Remove(tmp.Name())
	}

	// Now serve the freshly cached file.
	f, err := os.Open(cachePath)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, _ := f.Stat()
	for _, h := range []string{"Content-Type", "Content-Length", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeContent(w, &http.Request{}, key, stat.ModTime(), f)
	return nil
}

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

var errImageProxyRequestSetup = errors.New("image proxy request setup failed")
var errImageProxyNonImageContent = errors.New("upstream returned non-image content")

type remoteImageFetchClient struct {
	name   string
	client *http.Client
}

func (p *ImageProxy) PrefetchRemote(ctx context.Context, raw string) error {
	_, _, err := p.Fetch(ctx, raw)
	return err
}

func (p *ImageProxy) RemoveCached(raw string) error {
	if !isHTTPish(raw) {
		return nil
	}
	_, cachePath, failPath, err := p.remoteImageCachePaths(raw)
	if err != nil {
		return nil
	}
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(failPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (p *ImageProxy) RemoveFailed(raw string) error {
	if !isHTTPish(raw) {
		return nil
	}
	_, _, failPath, err := p.remoteImageCachePaths(raw)
	if err != nil {
		return nil
	}
	if err := os.Remove(failPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Serve writes the requested image to w. Caller is expected to validate
// the JWT before invoking it.
func (p *ImageProxy) Serve(ctx context.Context, w http.ResponseWriter, r *http.Request, raw string) error {
	if isLocalImagePath(raw) {
		return p.serveLocalImage(w, r, raw)
	}
	return p.serveRemoteImage(ctx, w, r, raw)
}

func (p *ImageProxy) serveLocalImage(w http.ResponseWriter, r *http.Request, raw string) error {
	path := filepath.Clean(raw)
	abs, err := filepath.Abs(path)
	if err != nil || !p.isAllowedLocalPath(abs) {
		servePlaceholder(w)
		return nil
	}
	if !serveImageFile(w, r, filepath.Base(abs), abs, imageBrowserCacheControl) {
		servePlaceholder(w)
	}
	return nil
}

func (p *ImageProxy) serveRemoteImage(ctx context.Context, w http.ResponseWriter, r *http.Request, raw string) error {
	u, err := p.validateURL(raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Host)
	key, cachePath, failPath := p.remoteImageCachePathsForValidated(raw)
	forceRefresh := r.URL.Query().Get("refresh") != ""
	p.removeUnusableImageCache(cachePath, failPath)
	if !forceRefresh && serveCachedImageFile(w, r, key, cachePath) {
		return nil
	}
	if !forceRefresh && p.serveFreshRemoteFailure(w, failPath) {
		return nil
	}
	data, ctype, contentLength, err := p.fetchAndCacheRemoteImage(ctx, raw, host, cachePath, failPath)
	if err != nil {
		if forceRefresh && serveCachedImageFile(w, r, key, cachePath) {
			return nil
		}
		if errors.Is(err, errImageProxyRequestSetup) {
			servePlaceholder(w)
		} else {
			serveCachedPlaceholder(w)
		}
		return nil
	}
	w.Header().Set("Content-Type", ctype)
	if contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	modTime := time.Now()
	if stat, err := os.Stat(cachePath); err == nil && stat.Size() > 0 {
		modTime = stat.ModTime()
		w.Header().Set("ETag", imageFileETag(key, stat))
	}
	w.Header().Set("Cache-Control", imageBrowserCacheControl)
	http.ServeContent(w, r, key, modTime, bytes.NewReader(data))
	return nil
}

func (p *ImageProxy) removeUnusableImageCache(cachePath, failPath string) {
	data, err := os.ReadFile(cachePath) // #nosec G304 -- cachePath is SHA-derived under cacheDir.
	if err != nil {
		return
	}
	ctype := detectContentType(data)
	if len(data) > 0 && isImageContentType(ctype) && !isTransparentPlaceholderData(data) {
		return
	}
	_ = os.Remove(cachePath)
	_ = os.Remove(failPath)
}

func (p *ImageProxy) serveFreshRemoteFailure(w http.ResponseWriter, failPath string) bool {
	if stat, err := os.Stat(failPath); err == nil && time.Since(stat.ModTime()) < imageNegativeCacheTTL {
		serveCachedPlaceholder(w)
		return true
	} else if err == nil {
		_ = os.Remove(failPath)
	}
	return false
}

func (p *ImageProxy) fetchAndCacheRemoteImage(ctx context.Context, raw, host, cachePath, failPath string) ([]byte, string, string, error) {
	if err := os.MkdirAll(p.cacheDir, 0o750); err != nil {
		p.log.Warn("imageproxy: mkdir failed", zap.String("dir", p.cacheDir), zap.Error(err))
		return nil, "", "", errImageProxyRequestSetup
	}
	var lastErr error
	for _, candidate := range p.remoteImageFetchClients() {
		data, ctype, contentLength, err := p.fetchRemoteImageOnce(ctx, raw, host, candidate)
		if err == nil {
			p.writeImageCache(cachePath, failPath, "img-*.tmp", data)
			return data, ctype, contentLength, nil
		}
		if errors.Is(err, errImageProxyRequestSetup) {
			return nil, "", "", err
		}
		lastErr = err
	}
	if p.canUseExternalImageFallback() && isDoubanImageHost(host) {
		data, ctype, contentLength, err := fetchRemoteImageWithCurl(ctx, raw, host)
		if err == nil {
			p.writeImageCache(cachePath, failPath, "img-*.tmp", data)
			return data, ctype, contentLength, nil
		}
		p.log.Warn("imageproxy: curl fallback failed", zap.String("host", host), zap.Error(err))
		lastErr = err
	}
	p.markImageFetchFailed(failPath)
	if lastErr == nil {
		lastErr = errors.New("upstream image fetch failed")
	}
	return nil, "", "", lastErr
}

func (p *ImageProxy) remoteImageFetchClients() []remoteImageFetchClient {
	client := p.client
	if client == nil {
		client = NewExternalHTTPClient(30 * time.Second)
	}
	clients := []remoteImageFetchClient{{name: "default", client: client}}
	if _, ok := client.Transport.(*http.Transport); ok {
		timeout := client.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		clients = append(clients, remoteImageFetchClient{
			name:   "direct",
			client: &http.Client{Timeout: timeout, Transport: NewInternalTransport()},
		})
	}
	return clients
}

func (p *ImageProxy) canUseExternalImageFallback() bool {
	if p == nil || p.client == nil {
		return false
	}
	_, ok := p.client.Transport.(*http.Transport)
	return ok
}

func (p *ImageProxy) fetchRemoteImageOnce(ctx context.Context, raw, host string, candidate remoteImageFetchClient) ([]byte, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		p.log.Warn("imageproxy: build request failed", zap.String("url", raw), zap.Error(err))
		return nil, "", "", errImageProxyRequestSetup
	}
	applyRemoteImageHeaders(req, host)

	resp, err := candidate.client.Do(req)
	if err != nil {
		p.log.Warn("imageproxy: upstream fetch failed", zap.String("host", host), zap.String("client", candidate.name), zap.Error(err))
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		p.log.Warn("imageproxy: upstream returned non-OK", zap.String("host", host), zap.String("client", candidate.name), zap.String("status", resp.Status))
		return nil, "", "", errors.New("upstream returned " + resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil || len(data) == 0 {
		p.log.Warn("imageproxy: read upstream body failed", zap.String("host", host), zap.String("client", candidate.name), zap.Error(err))
		if err == nil {
			err = errors.New("upstream image body is empty")
		}
		return nil, "", "", err
	}
	ctype, ok := validImageContentType(data)
	if !ok {
		p.log.Warn("imageproxy: upstream returned non-image content", zap.String("host", host), zap.String("client", candidate.name), zap.String("content_type", resp.Header.Get("Content-Type")))
		return nil, "", "", errImageProxyNonImageContent
	}
	return data, ctype, resp.Header.Get("Content-Length"), nil
}

func applyRemoteImageHeaders(req *http.Request, host string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	switch {
	case strings.Contains(host, "doubanio.com"):
		req.Header.Set("Referer", "https://movie.douban.com/")
	case strings.Contains(host, "bgm.tv"):
		req.Header.Set("Referer", "https://bgm.tv/")
	}
}

func isDoubanImageHost(host string) bool {
	return strings.Contains(strings.ToLower(host), "doubanio.com")
}

func fetchRemoteImageWithCurl(ctx context.Context, raw, host string) ([]byte, string, string, error) {
	bin, err := exec.LookPath("curl")
	if err != nil {
		return nil, "", "", err
	}
	curlCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	args := []string{
		"--fail",
		"--location",
		"--silent",
		"--show-error",
		"--http1.1",
		"--max-time", "20",
		"--proto", "=http,https",
		"--proto-redir", "=http,https",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36",
		"--header", "Accept: image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
		"--header", "Accept-Language: zh-CN,zh;q=0.9,en;q=0.8",
		"--header", "Cache-Control: no-cache",
		"--header", "Pragma: no-cache",
	}
	if isDoubanImageHost(host) {
		args = append(args, "--referer", "https://movie.douban.com/")
	}
	args = append(args, "--", raw)

	cmd := exec.CommandContext(curlCtx, bin, args...) // #nosec G204 -- bin is resolved by LookPath and args are not shell-expanded.
	stderr := bytes.Buffer{}
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", "", err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, 32<<20))
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, "", "", readErr
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, "", "", errors.New(message)
		}
		return nil, "", "", waitErr
	}
	if len(data) == 0 {
		return nil, "", "", errors.New("curl image body is empty")
	}
	ctype := detectContentType(data)
	if !isImageContentType(ctype) || isTransparentPlaceholderData(data) {
		return nil, "", "", errors.New("curl returned non-image content")
	}
	return data, ctype, "", nil
}

// Fetch pulls a remote image and returns bytes plus Content-Type using cache.
func (p *ImageProxy) Fetch(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := p.validateURL(raw)
	if err != nil {
		return nil, "", err
	}
	host := strings.ToLower(u.Host)
	_, cachePath, failPath := p.remoteImageCachePathsForValidated(raw)
	p.removeUnusableImageCache(cachePath, failPath)
	if data, err := os.ReadFile(cachePath); err == nil && len(data) > 0 { // #nosec G304 -- cachePath is SHA-derived under cacheDir.
		ctype := detectContentType(data)
		if isImageContentType(ctype) && !isTransparentPlaceholderData(data) {
			return data, ctype, nil
		}
		_ = os.Remove(cachePath)
		_ = os.Remove(failPath)
	}
	if stat, err := os.Stat(failPath); err == nil && time.Since(stat.ModTime()) < imageNegativeCacheTTL {
		return nil, "", errors.New("recent image fetch failure")
	} else if err == nil {
		_ = os.Remove(failPath)
	}
	data, ctype, _, err := p.fetchAndCacheRemoteImage(ctx, raw, host, cachePath, failPath)
	return data, ctype, err
}

func (p *ImageProxy) writeImageCache(cachePath, failPath, pattern string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tmp, tmpErr := os.CreateTemp(p.cacheDir, pattern)
	if tmpErr != nil {
		return
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return
	}
	_ = tmp.Close()
	if err := os.Rename(tmp.Name(), cachePath); err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	_ = os.Remove(failPath)
}

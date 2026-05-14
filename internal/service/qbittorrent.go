// Package service — qBittorrent Web UI client.
//
// QBitClient is a thin wrapper around the qBittorrent /api/v2 REST API
// (https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API).
//
// We only need three operations for the download flow:
//
//   POST /auth/login
//   POST /torrents/add  (multipart, accepts magnet URL or .torrent bytes)
//   GET  /torrents/info (filtered by hash)
//
// The client stores the SID cookie returned by /auth/login and reuses it
// across calls. Re-auth happens transparently on 403.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// QBitConfig holds the connection settings (typically loaded from the
// system Setting table or an env var).
type QBitConfig struct {
	BaseURL  string
	Username string
	Password string
}

// QBitTorrent is the subset of /torrents/info we surface to the API.
type QBitTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Progress float32 `json:"progress"`
	DLSpeed  int64   `json:"dlspeed"`
	UpSpeed  int64   `json:"upspeed"`
	NumSeeds int     `json:"num_seeds"`
	NumLeech int     `json:"num_leechs"`
	Size     int64   `json:"size"`
	SavePath string  `json:"save_path"`
}

// QBitClient is a thread-safe qBittorrent v2 API client.
type QBitClient struct {
	log    *zap.Logger
	mu     sync.Mutex
	cfg    QBitConfig
	client *http.Client
}

// NewQBitClient builds a fresh client, applying default URL if blank.
func NewQBitClient(log *zap.Logger, cfg QBitConfig) *QBitClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8080"
	}
	jar, _ := cookiejar.New(nil)
	return &QBitClient{
		log:    log,
		cfg:    cfg,
		client: &http.Client{Jar: jar, Timeout: 20 * time.Second},
	}
}

// Configure rotates the client to a new endpoint and re-auths next call.
func (q *QBitClient) Configure(cfg QBitConfig) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cfg = cfg
	jar, _ := cookiejar.New(nil)
	q.client.Jar = jar
}

// Login performs POST /api/v2/auth/login.
func (q *QBitClient) Login(ctx context.Context) error {
	if q.cfg.BaseURL == "" {
		return errors.New("qbittorrent base url not configured")
	}
	form := url.Values{}
	form.Set("username", q.cfg.Username)
	form.Set("password", q.cfg.Password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(q.cfg.BaseURL, "/")+"/api/v2/auth/login",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", q.cfg.BaseURL)

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 || strings.TrimSpace(string(body)) != "Ok." {
		return fmt.Errorf("qbittorrent login failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

// AddTorrent submits a magnet URL or HTTP(S) URL to qBittorrent.
func (q *QBitClient) AddTorrent(ctx context.Context, magnetOrURL, savePath string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.ensureAuth(ctx); err != nil {
		return err
	}

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	_ = w.WriteField("urls", magnetOrURL)
	if savePath != "" {
		_ = w.WriteField("savepath", savePath)
	}
	_ = w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(q.cfg.BaseURL, "/")+"/api/v2/torrents/add", body,
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Referer", q.cfg.BaseURL)

	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qbittorrent add: %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// List returns every torrent (optionally filtered by status: all / downloading / completed).
func (q *QBitClient) List(ctx context.Context, filter string) ([]QBitTorrent, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.ensureAuth(ctx); err != nil {
		return nil, err
	}
	u := strings.TrimRight(q.cfg.BaseURL, "/") + "/api/v2/torrents/info"
	if filter != "" {
		u += "?filter=" + url.QueryEscape(filter)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", q.cfg.BaseURL)
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qbittorrent list: %d", resp.StatusCode)
	}
	var out []QBitTorrent
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes a torrent (optionally with its files).
func (q *QBitClient) Delete(ctx context.Context, hash string, deleteFiles bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.ensureAuth(ctx); err != nil {
		return err
	}
	form := url.Values{}
	form.Set("hashes", hash)
	if deleteFiles {
		form.Set("deleteFiles", "true")
	} else {
		form.Set("deleteFiles", "false")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(q.cfg.BaseURL, "/")+"/api/v2/torrents/delete",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", q.cfg.BaseURL)
	resp, err := q.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("qbittorrent delete: %d", resp.StatusCode)
	}
	return nil
}

// ensureAuth makes sure we have a valid SID cookie. Cheap on the happy
// path; logs in transparently otherwise.
func (q *QBitClient) ensureAuth(ctx context.Context) error {
	u, err := url.Parse(q.cfg.BaseURL)
	if err != nil {
		return err
	}
	if cookies := q.client.Jar.Cookies(u); len(cookies) > 0 {
		for _, c := range cookies {
			if strings.EqualFold(c.Name, "SID") && c.Value != "" {
				return nil
			}
		}
	}
	return q.Login(ctx)
}

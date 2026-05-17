// Package service — download manager.
//
// DownloadService persists user-initiated downloads, dispatches them to
// the configured client (currently qBittorrent) and pushes live progress
// to the WS hub so the React UI can render a live table.
//
// Settings consumed (system Setting table):
//
//   qbittorrent.url       e.g. http://127.0.0.1:8080
//   qbittorrent.username  qBittorrent WebUI user
//   qbittorrent.password  qBittorrent WebUI password
//   qbittorrent.savepath  optional default save dir
//
// Settings can be updated at runtime via the admin UI; ReloadConfig()
// re-reads them and re-authenticates.
package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DownloadService is the single download orchestrator.
type DownloadService struct {
	log          *zap.Logger
	repo         *repository.Container
	hub          *Hub
	qb           *QBitClient
	organizer    *OrganizerService

	mu           sync.Mutex
	stopCh       chan struct{}
	pollOnce     sync.Once
	prevStates   map[string]bool // hash -> wasCompleted
}

// NewDownloadService is the constructor.
func NewDownloadService(log *zap.Logger, repo *repository.Container, hub *Hub, organizer *OrganizerService) *DownloadService {
	return &DownloadService{
		log:       log,
		repo:      repo,
		hub:       hub,
		qb:        NewQBitClient(log, QBitConfig{}),
		organizer: organizer,
		prevStates: make(map[string]bool),
		stopCh:    make(chan struct{}),
	}
}

// Start kicks off the background poller (idempotent).
func (d *DownloadService) Start(ctx context.Context) {
	d.pollOnce.Do(func() {
		_ = d.ReloadConfig(ctx)
		go d.poll(ctx)
	})
}

// Stop terminates the poller.
func (d *DownloadService) Stop() {
	close(d.stopCh)
}

// ReloadConfig rebuilds the qBittorrent client from the system settings.
func (d *DownloadService) ReloadConfig(ctx context.Context) error {
	cfg := QBitConfig{}
	for _, key := range []struct{ from, into *string }{} {
		_ = key
	}
	get := func(k string) string {
		v, _ := d.repo.Setting.Get(ctx, k)
		return v
	}
	cfg.BaseURL = get("qbittorrent.url")
	cfg.Username = get("qbittorrent.username")
	cfg.Password = get("qbittorrent.password")
	d.qb.Configure(cfg)
	return nil
}

// AddDownload accepts a magnet URL / HTTP URL and persists a tracking row.
func (d *DownloadService) AddDownload(ctx context.Context, userID, urlStr, savePath string) (*model.DownloadTask, error) {
	if urlStr == "" {
		return nil, errors.New("empty url")
	}
	if savePath == "" {
		savePath, _ = d.repo.Setting.Get(ctx, "qbittorrent.savepath")
	}
	if err := d.qb.AddTorrent(ctx, urlStr, savePath); err != nil {
		return nil, err
	}
	t := &model.DownloadTask{
		UserID:   userID,
		Source:   "qbittorrent",
		URL:      urlStr,
		SavePath: savePath,
		Status:   "queued",
	}
	if err := d.repo.Download.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// List returns every persisted download task augmented with live data
// from qBittorrent when available.
func (d *DownloadService) List(ctx context.Context) ([]model.DownloadTask, []QBitTorrent, error) {
	rows, err := d.repo.Download.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	live, err := d.qb.List(ctx, "")
	if err != nil {
		// Network failure shouldn't break the page — return rows with no
		// live data and let the UI render the persisted snapshot.
		d.log.Debug("qbittorrent list failed", zap.Error(err))
		return rows, nil, nil
	}
	return rows, live, nil
}

// Delete removes a torrent (and optionally its files) from qBittorrent.
func (d *DownloadService) Delete(ctx context.Context, hash string, withFiles bool) error {
	return d.qb.Delete(ctx, hash, withFiles)
}

// poll fans out qBittorrent /torrents/info every 5 s as WS events. The
// payload is opaque to the client; the React store merges by hash.
func (d *DownloadService) poll(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	// prevStates tracks previous completion states to detect "just finished"
	if d.prevStates == nil {
		d.prevStates = make(map[string]bool)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-t.C:
		}
		live, err := d.qb.List(ctx, "")
		if err != nil {
			continue
		}
		// Detect completed downloads and trigger organize
		for _, t := range live {
			hash := t.Hash
			complete := t.Progress >= 1.0
			if complete && !d.prevStates[hash] {
				// Just completed: trigger organize
				go d.onTorrentComplete(ctx, hash, t.SavePath)
			}
			d.prevStates[hash] = complete
		}
		d.hub.Publish("download", map[string]any{"torrents": live})
	}
}

// onTorrentComplete handles a torrent that just finished downloading.
// It tries to find the associated Media record and trigger organize.
func (d *DownloadService) onTorrentComplete(ctx context.Context, hash string, savePath string) {
	if d.organizer == nil || savePath == "" {
		return
	}
	// Check if auto-organize after download is enabled
	autoOrganize := d.organizer.isSmartClassifyEnabled(ctx)
	// Also check dedicated config key
	if v, err := d.repo.Setting.Get(ctx, "organizer.auto_after_download"); err == nil {
		autoOrganize = autoOrganize || v == "true" || v == "1" || v == "on"
	}
	if !autoOrganize {
		d.log.Info("download completed, auto-organize disabled", zap.String("hash", hash))
		return
	}
	d.log.Info("download completed, triggering organize", zap.String("hash", hash), zap.String("save_path", savePath))
	// Find Media record by path prefix
	var medias []model.Media
	if err := d.repo.DB.WithContext(ctx).Where("path LIKE ?", savePath+"%").Find(&medias).Error; err != nil {
		d.log.Error("find media by path", zap.Error(err))
		return
	}
	for i := range medias {
		if _, err := d.organizer.OrganizeMedia(ctx, medias[i].ID); err != nil {
			d.log.Error("organize media", zap.String("media_id", medias[i].ID), zap.Error(err))
		}
	}
}

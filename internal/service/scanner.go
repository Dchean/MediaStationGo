// Package service — filesystem scanner.
//
// ScannerService walks the configured library roots looking for video
// files, then upserts a model.Media row per file. Each upsert also runs
// ffprobe (when available) and queues a TMDb lookup for newly added rows.
//
// The scan is synchronous from the HTTP layer's point of view, but it
// publishes WebSocket progress events on the "scan" topic so the React
// UI can render a live counter / spinner.
package service

import (
	"context"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// videoExtensions lists the file extensions treated as media. Matches the
// MediaStation Python defaults.
var videoExtensions = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".m4v":  {},
	".avi":  {},
	".mov":  {},
	".webm": {},
	".ts":   {},
	".rmvb": {},
	".rm":   {},
	".3gp":  {},
	".mpg":  {},
	".mpeg": {},
	".strm": {},
}

// ScannerService walks libraries on disk and upserts model.Media rows.
type ScannerService struct {
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	hub     *Hub
	probe   *FFprobeService
	scraper *ScraperService
}

// NewScannerService is the constructor.
func NewScannerService(
	cfg *config.Config,
	log *zap.Logger,
	repo *repository.Container,
	hub *Hub,
	probe *FFprobeService,
	scraper *ScraperService,
) *ScannerService {
	return &ScannerService{
		cfg: cfg, log: log, repo: repo, hub: hub,
		probe: probe, scraper: scraper,
	}
}

// ScanResult summarises a scan run.
type ScanResult struct {
	LibraryID string `json:"library_id"`
	Visited   int    `json:"visited"`
	Added     int    `json:"added"`
	Probed    int    `json:"probed"`
}

// ScanLibrary walks the library root and persists discovered media files.
//
// Workflow per file:
//   1. fast filename-based title cleanup.
//   2. ffprobe → duration / resolution / codecs (best effort).
//   3. upsert into the media table.
//   4. publish progress over the WS hub.
//
// After the walk we kick off the TMDb scraper for every still-pending
// row in the same library. The scraper has its own throttle.
func (s *ScannerService) ScanLibrary(ctx context.Context, libraryID string) (*ScanResult, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, err
	}
	res := &ScanResult{LibraryID: lib.ID}

	walkFn := func(path string, info walkInfo) error {
		if info.isDir {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := videoExtensions[ext]; !ok {
			return nil
		}
		res.Visited++

		title, year := CleanQuery(path)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), ext)
		}

		m := &model.Media{
			LibraryID: lib.ID,
			Title:     title,
			Year:      year,
			Path:      path,
			SizeBytes: info.size,
			Container: strings.TrimPrefix(ext, "."),
		}

		// Best-effort ffprobe; failure does not abort the file.
		if s.probe != nil {
			if probe, err := s.probe.Probe(ctx, path); err == nil && probe != nil {
				m.DurationSec = probe.DurationSec
				m.Width = probe.Width
				m.Height = probe.Height
				m.VideoCodec = probe.VideoCodec
				m.AudioCodec = probe.AudioCodec
				if probe.Container != "" {
					m.Container = probe.Container
				}
				res.Probed++
			} else if err != nil {
				s.log.Debug("ffprobe failed", zap.String("path", path), zap.Error(err))
			}
		}

		if err := s.repo.Media.Upsert(ctx, m); err != nil {
			s.log.Warn("upsert media failed", zap.String("path", path), zap.Error(err))
			return nil
		}
		res.Added++
		s.hub.Publish("scan", map[string]any{
			"library_id": lib.ID,
			"path":       path,
			"visited":    res.Visited,
			"added":      res.Added,
			"probed":     res.Probed,
		})
		return nil
	}

	if err := walk(lib.Path, walkFn); err != nil {
		return res, err
	}

	s.hub.Publish("scan", map[string]any{
		"library_id": lib.ID,
		"finished":   true,
		"visited":    res.Visited,
		"added":      res.Added,
		"probed":     res.Probed,
	})

	// Fire-and-forget metadata enrichment when a TMDb key is configured.
	if s.scraper != nil && s.scraper.tmdb != nil && s.scraper.tmdb.Enabled() {
		go func(libID string) {
			if _, err := s.scraper.EnrichLibrary(context.Background(), libID); err != nil {
				s.log.Warn("scraper enrich failed", zap.Error(err))
			}
		}(lib.ID)
	}
	return res, nil
}

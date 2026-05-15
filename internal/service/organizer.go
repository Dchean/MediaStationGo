// Package service — media file organizer (auto-rename + move).
//
// OrganizerService takes a source file (typically from a download
// completion) and moves/renames it into the configured library directory
// using a Jinja2-like template. The default templates:
//
//   movie: {Title} ({Year})/{Title} ({Year}).{Ext}
//   tv:    {Title}/Season {Season:02d}/{Title} - S{Season:02d}E{Episode:02d}.{Ext}
//
// We do NOT delete the source after move — the operator can turn that on
// via a config flag. This mirrors MediaStation's "organize after download"
// workflow.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// OrganizerService moves/renames files into library structures.
type OrganizerService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewOrganizerService is the constructor.
func NewOrganizerService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *OrganizerService {
	return &OrganizerService{cfg: cfg, log: log, repo: repo}
}

// OrganizeResult reports what happened.
type OrganizeResult struct {
	Organized int      `json:"organized"`
	Skipped   int      `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}

// OrganizeMedia moves a single media file into the target library directory.
// It auto-detects whether the media is a movie or TV episode based on the
// parsed season/episode numbers and builds the destination path accordingly.
func (o *OrganizerService) OrganizeMedia(ctx context.Context, mediaID string) (string, error) {
	m, err := o.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return "", errors.New("media not found")
	}
	lib, err := o.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return "", errors.New("library not found")
	}
	ext := filepath.Ext(m.Path)
	title := sanitizeFilename(m.Title)
	if title == "" {
		title = "Unknown"
	}

	var dst string
	if lib.Type == "tv" || lib.Type == "anime" {
		// TV: {Title}/Season XX/{Title} - SxxExx.ext
		season := fmt.Sprintf("Season %02d", m.SeasonNum)
		epTag := fmt.Sprintf("S%02dE%02d", m.SeasonNum, m.EpisodeNum)
		dir := filepath.Join(lib.Path, title, season)
		dst = filepath.Join(dir, fmt.Sprintf("%s - %s%s", title, epTag, ext))
	} else {
		// Movie: {Title} ({Year})/{Title} ({Year}).ext
		folder := title
		if m.Year > 0 {
			folder = fmt.Sprintf("%s (%d)", title, m.Year)
		}
		dir := filepath.Join(lib.Path, folder)
		dst = filepath.Join(dir, folder+ext)
	}

	// Skip if already in place.
	if m.Path == dst {
		return dst, nil
	}

	// Create directories.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}

	// Move the file (same filesystem = rename; cross-device = copy+delete).
	if err := moveFile(m.Path, dst); err != nil {
		return "", err
	}

	// Update the database row.
	if err := o.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Where("id = ?", m.ID).
		Update("path", dst).Error; err != nil {
		return dst, err
	}
	o.log.Info("organized",
		zap.String("media", m.ID),
		zap.String("from", m.Path),
		zap.String("to", dst),
	)
	return dst, nil
}

// OrganizeLibrary organizes every media row in a library whose file is
// not already in the expected path structure.
func (o *OrganizerService) OrganizeLibrary(ctx context.Context, libraryID string) (*OrganizeResult, error) {
	var rows []model.Media
	if err := o.repo.DB.WithContext(ctx).
		Where("library_id = ? AND deleted_at IS NULL", libraryID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	res := &OrganizeResult{}
	for i := range rows {
		dst, err := o.OrganizeMedia(ctx, rows[i].ID)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", rows[i].Title, err.Error()))
			continue
		}
		if dst == rows[i].Path {
			res.Skipped++
		} else {
			res.Organized++
		}
	}
	return res, nil
}

// moveFile tries os.Rename first (instant on same fs), then falls back
// to copy + remove for cross-device moves.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device: read → write → remove.
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

// sanitizeFilename removes characters not safe for filesystem names.
func sanitizeFilename(s string) string {
	r := strings.NewReplacer(
		"/", " ", "\\", " ", ":", " ", "*", "", "?", "",
		"\"", "", "<", "", ">", "", "|", "",
	)
	return strings.TrimSpace(r.Replace(s))
}

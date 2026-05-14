// Package service — scraper orchestrator.
//
// ScraperService takes a Media row and tries to enrich it with metadata
// from one or more providers (currently TMDb only). It is invoked at the
// end of every scan cycle for media items whose `scrape_status` is still
// "pending"; it can also be re-triggered manually from the admin UI.
//
// The orchestrator is deliberately stateless: it loops media → provider →
// repository, publishing scrape progress events to the WS hub.
package service

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ScraperService coordinates metadata enrichment across providers.
type ScraperService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
	tmdb *TMDbProvider
	hub  *Hub
}

// NewScraperService is the constructor.
func NewScraperService(cfg *config.Config, log *zap.Logger, repo *repository.Container, tmdb *TMDbProvider, hub *Hub) *ScraperService {
	return &ScraperService{cfg: cfg, log: log, repo: repo, tmdb: tmdb, hub: hub}
}

// yearPattern extracts a 4-digit year from a filename (1900-2099).
var yearPattern = regexp.MustCompile(`(?:^|[^\d])(19\d{2}|20\d{2})(?:[^\d]|$)`)

// noiseTokens are aggressively stripped from filenames before search.
// Keep in sync with nowen-video's filename_parser.go intent.
var noiseTokens = []string{
	"1080p", "2160p", "4k", "720p", "480p",
	"hdrip", "bluray", "blu-ray", "webrip", "web-dl", "web",
	"x264", "x265", "h264", "h265", "hevc", "avc",
	"hdr", "sdr", "dts", "ddp", "atmos", "aac", "ac3", "flac",
	"remux", "extended", "uncut", "directors-cut", "directors_cut",
	"hkfree", "yify", "rarbg", "ettv", "fgt",
}

// bracketedTag matches "[anything]" or "(anything)" segments, which are
// almost always release-group / encoder tags in scene filenames.
var bracketedTag = regexp.MustCompile(`[\[\(][^\]\)]*[\]\)]`)

// CleanQuery converts a filename like "Inception.2010.1080p.BluRay.x264.mkv"
// into a TMDb-friendly title plus an optional year hint.
func CleanQuery(raw string) (title string, year int) {
	name := strings.TrimSuffix(filepath.Base(raw), filepath.Ext(raw))
	lower := strings.ToLower(name)

	// 1. Year first — bracketed years (1999) must survive the next step.
	if m := yearPattern.FindStringSubmatch(lower); len(m) >= 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			year = v
			lower = strings.ReplaceAll(lower, m[1], " ")
		}
	}

	// 2. Drop everything inside square / round brackets — those are tags.
	lower = bracketedTag.ReplaceAllString(lower, " ")

	for _, t := range noiseTokens {
		lower = strings.ReplaceAll(lower, t, " ")
	}
	// collapse separators / spaces
	for _, sep := range []string{".", "_", "-", "[", "]", "(", ")"} {
		lower = strings.ReplaceAll(lower, sep, " ")
	}
	fields := strings.Fields(lower)
	title = strings.Join(fields, " ")
	return strings.TrimSpace(title), year
}

// EnrichOne runs the provider chain for a single media row.
func (s *ScraperService) EnrichOne(ctx context.Context, m *model.Media) error {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		return nil
	}
	query := m.Title
	if query == "" {
		query, _ = CleanQuery(m.Path)
	} else {
		query, _ = CleanQuery(query)
	}
	year := m.Year
	if year == 0 {
		_, year = CleanQuery(filepath.Base(m.Path))
	}
	match, err := s.tmdb.SearchMovie(ctx, query, year)
	if err != nil || match == nil {
		return err
	}
	updates := map[string]any{
		"title":         match.Title,
		"overview":      match.Overview,
		"poster_url":    match.PosterURL,
		"backdrop_url":  match.BackdropURL,
		"rating":        match.Rating,
		"year":          match.Year,
		"tmdb_id":       match.TMDbID,
		"scrape_status": "matched",
	}
	if err := s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).
		Updates(updates).Error; err != nil {
		return err
	}
	s.hub.Publish("scrape", map[string]any{
		"media_id": m.ID,
		"title":    match.Title,
		"tmdb_id":  match.TMDbID,
	})
	return nil
}

// EnrichLibrary runs the provider chain for every "pending" media in a
// library. It throttles to 4 RPS to stay below TMDb's rate limit and
// publishes a summary event when done.
func (s *ScraperService) EnrichLibrary(ctx context.Context, libraryID string) (int, error) {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		return 0, nil
	}
	var rows []model.Media
	q := s.repo.DB.Where("scrape_status = ?", "pending")
	if libraryID != "" {
		q = q.Where("library_id = ?", libraryID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return 0, err
	}
	matched := 0
	for i := range rows {
		select {
		case <-ctx.Done():
			return matched, ctx.Err()
		default:
		}
		if err := s.EnrichOne(ctx, &rows[i]); err != nil {
			s.log.Warn("enrich failed", zap.String("media", rows[i].ID), zap.Error(err))
			continue
		}
		matched++
		time.Sleep(250 * time.Millisecond) // ~4 RPS
	}
	s.hub.Publish("scrape", map[string]any{
		"library_id": libraryID,
		"finished":   true,
		"matched":    matched,
	})
	return matched, nil
}

// Package service — TMDb metadata provider.
//
// TMDbProvider implements the (minimal) MetadataProvider interface and uses
// the public The Movie Database REST API. The API key is taken from
// secrets.tmdb_api_key; when empty the provider returns nil from every
// method so the scraper can no-op gracefully.
//
// We only call the two endpoints the scrape pipeline actually needs:
//
//   GET /search/movie?query=...&year=...
//   GET /movie/{id}?language=zh-CN
//
// TV / anime support follows the same pattern; for the bootstrap we expose
// a single SearchMovie path so that the home page and library gallery can
// show real posters as soon as a TMDb key is configured.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// TMDbProvider talks to https://api.themoviedb.org/3.
type TMDbProvider struct {
	cfg       *config.Config
	log       *zap.Logger
	client    *http.Client
	base      string
	imgCDN    string
	apiConfig *APIConfigService
}

// NewTMDbProvider is the constructor. APIBase / image CDN can be overridden
// via secrets.tmdb_api_proxy + tmdb_image_proxy for users behind GFW.
// apiConfig is optional; when non-nil, the provider will also check the
// api_configs table for TMDB API key.
func NewTMDbProvider(cfg *config.Config, log *zap.Logger, apiConfig *APIConfigService) *TMDbProvider {
	base := cfg.Secrets.TMDbAPIProxy
	if base == "" {
		base = "https://api.themoviedb.org/3"
	}
	img := cfg.Secrets.TMDbImageProxy
	if img == "" {
		img = "https://image.tmdb.org/t/p"
	}
	return &TMDbProvider{
		cfg:       cfg,
		log:       log,
		apiConfig: apiConfig,
		base:      base,
		imgCDN:    img,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled reports whether the operator has supplied an API key.
// It checks both the config file and the database (via apiConfig).
func (t *TMDbProvider) Enabled() bool {
	// Fast path: check config
	if t.cfg.Secrets.TMDbAPIKey != "" {
		return true
	}
	// Secondary check: if we have apiConfig, the key might be in the database
	// We can't query the database here (no ctx), so we rely on the caller
	// to check properly before making API calls.
	// The actual key resolution happens in resolveAPIKey(ctx).
	return t.apiConfig != nil
}

// resolveAPIKey returns the TMDb API key, checking config first, then database.
func (t *TMDbProvider) resolveAPIKey(ctx context.Context) string {
	// Check config first (fast path)
	if t.cfg.Secrets.TMDbAPIKey != "" {
		return t.cfg.Secrets.TMDbAPIKey
	}
	// Fall back to database
	if t.apiConfig != nil {
		resolved, err := t.apiConfig.Resolve(ctx, "tmdb")
		if err == nil && resolved.APIKey != "" {
			return resolved.APIKey
		}
	}
	return ""
}

// resolveBaseURL returns the TMDb base URL, checking config first, then database.
func (t *TMDbProvider) resolveBaseURL(ctx context.Context) string {
	// Check config first
	base := t.cfg.Secrets.TMDbAPIProxy
	if base == "" {
		base = "https://api.themoviedb.org/3"
	}
	// Override from database if available
	if t.apiConfig != nil {
		resolved, err := t.apiConfig.Resolve(ctx, "tmdb")
		if err == nil && resolved.BaseURL != "" {
			base = resolved.BaseURL
		}
	}
	return base
}

// Match describes a successful metadata match. The same struct is reused
// across providers; provider-specific IDs sit side-by-side so the scraper
// orchestrator can write them all into a single update.
type Match struct {
	TMDbID      int     `json:"tmdb_id"`
	BangumiID   int     `json:"bangumi_id"`
	Title       string  `json:"title"`
	Overview    string  `json:"overview"`
	PosterURL   string  `json:"poster_url"`
	BackdropURL string  `json:"backdrop_url"`
	Year        int     `json:"year"`
	Rating      float32 `json:"rating"`
}

// SearchMovie issues `/search/movie` and returns the best match, or nil
// when no result is found. The `year` argument is optional (0 = any).
func (t *TMDbProvider) SearchMovie(ctx context.Context, query string, year int) (*Match, error) {
	if query == "" {
		return nil, errors.New("empty query")
	}

	// Resolve API key from config or database
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return nil, nil
	}
	base := t.resolveBaseURL(ctx)

	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("query", query)
	q.Set("language", "zh-CN")
	q.Set("include_adult", "false")
	if year > 0 {
		q.Set("year", fmt.Sprintf("%d", year))
	}
	u := base + "/search/movie?" + q.Encode()

	type result struct {
		ID           int     `json:"id"`
		Title        string  `json:"title"`
		Overview     string  `json:"overview"`
		PosterPath   string  `json:"poster_path"`
		BackdropPath string  `json:"backdrop_path"`
		ReleaseDate  string  `json:"release_date"`
		VoteAverage  float32 `json:"vote_average"`
	}
	type page struct {
		Results []result `json:"results"`
	}

	var p page
	if err := t.getJSON(ctx, u, &p); err != nil {
		return nil, err
	}
	if len(p.Results) == 0 {
		return nil, nil
	}
	r := p.Results[0]
	m := &Match{
		TMDbID:   r.ID,
		Title:    r.Title,
		Overview: r.Overview,
		Rating:   r.VoteAverage,
	}
	if r.PosterPath != "" {
		m.PosterURL = t.imgCDN + "/w500" + r.PosterPath
	}
	if r.BackdropPath != "" {
		m.BackdropURL = t.imgCDN + "/w1280" + r.BackdropPath
	}
	if len(r.ReleaseDate) >= 4 {
		fmt.Sscanf(r.ReleaseDate[:4], "%d", &m.Year)
	}
	return m, nil
}

func (t *TMDbProvider) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tmdb %s: %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

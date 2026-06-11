// Package service — TMDb metadata provider.
//
// TMDbProvider implements the (minimal) MetadataProvider interface and uses
// the public The Movie Database REST API. The API key is taken from
// secrets.tmdb_api_key; when empty the provider returns nil from every
// method so the scraper can no-op gracefully.
//
// We only call the two endpoints the scrape pipeline actually needs:
//
//	GET /search/movie?query=...&year=...
//	GET /movie/{id}?language=zh-CN
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
		// 默认 8s 超时：首页同时调 trending + popular，15s 太久会让用户感觉
		// 卡死。如果 TMDb 真有问题，handler 层会快速降级返回空列表。
		// 同时让 client 读取 HTTP(S)_PROXY 与 Windows 本机系统代理。
		client: NewExternalHTTPClient(8 * time.Second),
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
		t.log.Debug("tmdb: using API key from config file")
		return t.cfg.Secrets.TMDbAPIKey
	}
	// Fall back to database
	if t.apiConfig != nil {
		resolved, err := t.apiConfig.Resolve(ctx, "tmdb")
		if err != nil {
			t.log.Warn("tmdb: failed to resolve API key from database", zap.Error(err))
		} else if resolved.APIKey == "" {
			t.log.Warn("tmdb: API key is empty in database")
		} else {
			t.log.Debug("tmdb: using API key from database")
			return resolved.APIKey
		}
	} else {
		t.log.Warn("tmdb: apiConfig is nil, cannot resolve API key from database")
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
	TMDbID       int      `json:"tmdb_id"`
	BangumiID    int      `json:"bangumi_id"`
	Title        string   `json:"title"`
	OriginalName string   `json:"original_name,omitempty"`
	Overview     string   `json:"overview"`
	PosterURL    string   `json:"poster_url"`
	BackdropURL  string   `json:"backdrop_url"`
	Year         int      `json:"year"`
	Rating       float32  `json:"rating"`
	Languages    []string `json:"languages,omitempty"`
	Countries    []string `json:"countries,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	NSFW         bool     `json:"nsfw,omitempty"`
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
		_, _ = fmt.Sscanf(r.ReleaseDate[:4], "%d", &m.Year)
	}
	return m, nil
}

// SearchTV issues `/search/tv` and returns the best match. Used by anime /
// tv libraries before falling back to SearchMovie.
func (t *TMDbProvider) SearchTV(ctx context.Context, query string, year int) (*Match, error) {
	if query == "" {
		return nil, errors.New("empty query")
	}

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
		q.Set("first_air_date_year", fmt.Sprintf("%d", year))
	}
	u := base + "/search/tv?" + q.Encode()

	type result struct {
		ID           int     `json:"id"`
		Name         string  `json:"name"`
		OriginalName string  `json:"original_name"`
		Overview     string  `json:"overview"`
		PosterPath   string  `json:"poster_path"`
		BackdropPath string  `json:"backdrop_path"`
		FirstAirDate string  `json:"first_air_date"`
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
		Title:    r.Name,
		Overview: r.Overview,
		Rating:   r.VoteAverage,
	}
	if m.Title == "" {
		m.Title = r.OriginalName
	}
	if r.PosterPath != "" {
		m.PosterURL = t.imgCDN + "/w500" + r.PosterPath
	}
	if r.BackdropPath != "" {
		m.BackdropURL = t.imgCDN + "/w1280" + r.BackdropPath
	}
	if len(r.FirstAirDate) >= 4 {
		_, _ = fmt.Sscanf(r.FirstAirDate[:4], "%d", &m.Year)
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

// TMDbDetails holds extended metadata from the /movie/{id} or /tv/{id} endpoints.
type TMDbDetails struct {
	Languages []string `json:"languages"`
	Countries []string `json:"countries"`
	Genres    []string `json:"genres"`
}

// GetDetails fetches extended metadata for a TMDb ID.
// It calls /movie/{id} or /tv/{id} with append_to_response=genres
// and extracts languages, production countries, and genres.
// mediaType should be "movie" or "tv".
func (t *TMDbProvider) GetDetails(ctx context.Context, tmdbID int, mediaType string) (*TMDbDetails, error) {
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return nil, fmt.Errorf("tmdb: no API key available")
	}
	base := t.resolveBaseURL(ctx)

	path := "/movie/" + fmt.Sprint(tmdbID)
	if mediaType == "tv" {
		path = "/tv/" + fmt.Sprint(tmdbID)
	}

	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", "zh-CN")
	q.Set("append_to_response", "genres")
	u := base + path + "?" + q.Encode()

	// Response structs for /movie/{id} and /tv/{id}
	type genre struct {
		Name string `json:"name"`
	}
	type movieResult struct {
		OriginalLanguage    string `json:"original_language"`
		ProductionCountries []struct {
			Iso3166_1 string `json:"iso_3166_1"`
		} `json:"production_countries"`
		SpokenLanguages []struct {
			Iso639_1 string `json:"iso_639_1"`
		} `json:"spoken_languages"`
		Genres []genre `json:"genres"`
	}
	type tvResult struct {
		OriginCountry   []string `json:"origin_country"`
		SpokenLanguages []struct {
			Iso639_1 string `json:"iso_639_1"`
		} `json:"spoken_languages"`
		Genres []genre `json:"genres"`
	}

	var (
		languages []string
		countries []string
		genres    []string
	)

	if mediaType == "tv" {
		var r tvResult
		if err := t.getJSON(ctx, u, &r); err != nil {
			return nil, err
		}
		// Spoken languages
		for _, l := range r.SpokenLanguages {
			languages = append(languages, l.Iso639_1)
		}
		// Origin countries
		countries = append(countries, r.OriginCountry...)
		// Genres
		for _, g := range r.Genres {
			genres = append(genres, g.Name)
		}
	} else {
		var r movieResult
		if err := t.getJSON(ctx, u, &r); err != nil {
			return nil, err
		}
		// Original language
		if r.OriginalLanguage != "" {
			languages = append(languages, r.OriginalLanguage)
		}
		// Spoken languages
		for _, l := range r.SpokenLanguages {
			languages = append(languages, l.Iso639_1)
		}
		// Production countries
		for _, c := range r.ProductionCountries {
			countries = append(countries, c.Iso3166_1)
		}
		// Genres
		for _, g := range r.Genres {
			genres = append(genres, g.Name)
		}
	}

	// Deduplicate
	languages = deduplicate(languages)
	countries = deduplicate(countries)
	genres = deduplicate(genres)

	t.log.Debug("tmdb: getDetails",
		zap.Int("tmdb_id", tmdbID),
		zap.String("type", mediaType),
		zap.Strings("languages", languages),
		zap.Strings("countries", countries),
		zap.Strings("genres", genres),
	)

	return &TMDbDetails{
		Languages: languages,
		Countries: countries,
		Genres:    genres,
	}, nil
}

func (t *TMDbProvider) GetTVEpisodeCount(ctx context.Context, tmdbID int) (int, error) {
	if tmdbID <= 0 {
		return 0, nil
	}
	apiKey := t.resolveAPIKey(ctx)
	if apiKey == "" {
		return 0, nil
	}
	base := t.resolveBaseURL(ctx)
	q := url.Values{}
	q.Set("api_key", apiKey)
	q.Set("language", "zh-CN")
	u := base + "/tv/" + fmt.Sprint(tmdbID) + "?" + q.Encode()
	var r struct {
		NumberOfEpisodes int `json:"number_of_episodes"`
	}
	if err := t.getJSON(ctx, u, &r); err != nil {
		return 0, err
	}
	return r.NumberOfEpisodes, nil
}

// deduplicate removes duplicates from a string slice.
func deduplicate(s []string) []string {
	if len(s) == 0 {
		return s
	}
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

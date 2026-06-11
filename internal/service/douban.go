// Package service — Douban (豆瓣) metadata provider.
//
// Douban is the dominant Chinese movie/TV rating and metadata site. Its
// unofficial API returns rich Chinese-language titles, overviews, ratings
// and poster URLs. A valid Douban cookie is required to avoid IP bans.
//
// We use the search endpoint at:
//
//	https://movie.douban.com/j/subject_suggest?q=...
//
// And the detail endpoint at:
//
//	https://movie.douban.com/j/subject_abstract?subject_id=...
//
// The provider is used as a supplemental source: after TMDb matches we
// attempt a Douban lookup to grab a localized Chinese title + overview.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// DoubanProvider talks to the unofficial Douban movie API.
type DoubanProvider struct {
	cfg    *config.Config
	log    *zap.Logger
	client *http.Client
}

// NewDoubanProvider is the constructor.
func NewDoubanProvider(cfg *config.Config, log *zap.Logger) *DoubanProvider {
	return &DoubanProvider{
		cfg:    cfg,
		log:    log,
		client: NewExternalHTTPClient(15 * time.Second),
	}
}

// Enabled reports whether Douban lookup is available. Public movie.douban.com
// suggest endpoints work without an API key; a cookie is optional and only
// helps when Douban applies stricter anti-scraping rules.
func (d *DoubanProvider) Enabled() bool {
	return true
}

// userAgents for anti-scraping randomization.
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
}

// DoubanMatch is the result of a Douban search hit.
type DoubanMatch struct {
	DoubanID string  `json:"douban_id"`
	Title    string  `json:"title"`
	Year     string  `json:"year"`
	Img      string  `json:"img"`
	Rating   float32 `json:"rating"`
	Type     string  `json:"type,omitempty"`
}

// Search runs a Douban subject_suggest query and returns the top match.
func (d *DoubanProvider) Search(ctx context.Context, query string) (*DoubanMatch, error) {
	if !d.Enabled() || query == "" {
		return nil, nil
	}
	u := "https://movie.douban.com/j/subject_suggest?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	d.setHeaders(req)

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("douban search: %d", resp.StatusCode)
	}

	type suggestion struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Year  string `json:"year"`
		Img   string `json:"img"`
		Type  string `json:"type"`
	}
	var results []suggestion
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	r := results[0]
	return &DoubanMatch{
		DoubanID: r.ID,
		Title:    r.Title,
		Year:     r.Year,
		Img:      r.Img,
		Type:     r.Type,
	}, nil
}

func (d *DoubanProvider) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", userAgents[secureRandomIntn(len(userAgents))])
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if cookie := strings.TrimSpace(d.cfg.Secrets.DoubanCookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
}

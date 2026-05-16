// Package service — site management (PT/BT tracker CRUD + connection test).
//
// SiteService owns the lifecycle of Site rows and exposes a cross-site
// search dispatcher that fans out a keyword query to every enabled site's
// adapter, collects results and returns them merged + sorted.
package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SiteService manages PT/BT site configurations.
type SiteService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewSiteService is the constructor.
func NewSiteService(log *zap.Logger, repo *repository.Container) *SiteService {
	return &SiteService{log: log, repo: repo}
}

// Create persists a new site.
func (s *SiteService) Create(ctx context.Context, site *model.Site) error {
	if strings.TrimSpace(site.Name) == "" || strings.TrimSpace(site.BaseURL) == "" {
		return errors.New("name and base_url required")
	}
	site.BaseURL = strings.TrimRight(site.BaseURL, "/")
	if site.SiteType == "" {
		site.SiteType = "nexusphp"
	}
	if site.AuthType == "" {
		site.AuthType = "cookie"
	}
	if site.Timeout <= 0 {
		site.Timeout = 15
	}
	return s.repo.DB.WithContext(ctx).Create(site).Error
}

// List returns every site ordered by priority (lower = higher priority).
func (s *SiteService) List(ctx context.Context) ([]model.Site, error) {
	var sites []model.Site
	err := s.repo.DB.WithContext(ctx).Order("priority asc, created_at asc").Find(&sites).Error
	return sites, err
}

// FindByID returns a single site or nil.
func (s *SiteService) FindByID(ctx context.Context, id string) (*model.Site, error) {
	var site model.Site
	err := s.repo.DB.WithContext(ctx).Where("id = ?", id).First(&site).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &site, err
}

// Update applies a partial patch to an existing site.
func (s *SiteService) Update(ctx context.Context, id string, updates map[string]any) error {
	return s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).Updates(updates).Error
}

// Delete removes a site.
func (s *SiteService) Delete(ctx context.Context, id string) error {
	return s.repo.DB.WithContext(ctx).Where("id = ?", id).Delete(&model.Site{}).Error
}

// TestConnection tries to reach the site's base URL with the configured
// credentials and reports success/failure.
func (s *SiteService) TestConnection(ctx context.Context, id string) (bool, string, error) {
	site, err := s.FindByID(ctx, id)
	if err != nil || site == nil {
		return false, "site not found", err
	}

	client := &http.Client{Timeout: time.Duration(site.Timeout) * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site.BaseURL, nil)
	if err != nil {
		return false, err.Error(), nil
	}

	// Apply auth headers.
	req.Header.Set("User-Agent", effectiveUA(site))
	switch site.AuthType {
	case "cookie":
		if site.Cookie != "" {
			req.Header.Set("Cookie", site.Cookie)
		}
	case "api_key":
		if site.APIKey != "" {
			req.Header.Set("x-api-key", site.APIKey)
		}
	case "authorization":
		if site.AuthHeader != "" {
			req.Header.Set("Authorization", site.AuthHeader)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		status := "fail"
		_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
			Update("login_status", status).Error
		return false, err.Error(), nil
	}
	defer resp.Body.Close()

	var ok bool
	var msg string
	switch {
	case resp.StatusCode == 200:
		ok, msg = true, "连接成功"
	case resp.StatusCode == 403:
		ok, msg = false, "认证失败 (HTTP 403)"
	case resp.StatusCode == 401:
		ok, msg = false, "未授权 (HTTP 401)"
	default:
		ok, msg = resp.StatusCode < 400, "HTTP "+resp.Status
	}

	loginStatus := "ok"
	if !ok {
		loginStatus = "fail"
	}
	_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
		Updates(map[string]any{"login_status": loginStatus, "last_check": time.Now()}).Error
	return ok, msg, nil
}

// SearchResult is one torrent returned by a site adapter search.
type SearchResult struct {
	SiteName    string `json:"site_name"`
	SiteID      string `json:"site_id"`
	Title       string `json:"title"`
	TorrentURL  string `json:"torrent_url"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	Free        bool   `json:"free"`
}

// Search fans out a keyword query to every enabled site and returns
// merged results sorted by seeders descending.
func (s *SiteService) Search(ctx context.Context, keyword string) ([]SearchResult, error) {
	if strings.TrimSpace(keyword) == "" {
		return nil, errors.New("keyword required")
	}
	sites, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	for i := range sites {
		if !sites[i].Enabled {
			continue
		}
		adapter := NewSiteAdapter(&sites[i])
		if adapter == nil {
			continue
		}
		items, err := adapter.Search(ctx, keyword)
		if err != nil {
			s.log.Debug("site search failed",
				zap.String("site", sites[i].Name), zap.Error(err))
			continue
		}
		for _, item := range items {
			results = append(results, SearchResult{
				SiteName:    sites[i].Name,
				SiteID:      sites[i].ID,
				Title:       item.Title,
				TorrentURL:  item.TorrentURL,
				DownloadURL: item.DownloadURL,
				Size:        item.Size,
				Seeders:     item.Seeders,
				Leechers:    item.Leechers,
				Free:        item.Free,
			})
		}
	}

	// Sort by seeders desc.
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Seeders > results[i].Seeders {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results, nil
}

func effectiveUA(site *model.Site) string {
	if site.UserAgent != "" {
		return site.UserAgent
	}
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
}

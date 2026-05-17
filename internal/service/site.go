// Package service — site management (PT/BT tracker CRUD + connection test).
//
// SiteService owns the lifecycle of Site rows and exposes a cross-site
// search dispatcher that fans out a keyword query to every enabled site's
// adapter, collects results and returns them merged + sorted.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/helper"
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
	if strings.TrimSpace(site.Name) == "" || strings.TrimSpace(site.URL) == "" {
		return errors.New("name and url required")
	}
	site.URL = strings.TrimRight(site.URL, "/")
	if site.Type == "" {
		site.Type = "nexusphp"
	}
	if site.AuthType == "" {
		site.AuthType = "cookie"
	}
	return s.repo.DB.WithContext(ctx).Create(site).Error
}

// List returns every site ordered by priority (lower = higher priority).
func (s *SiteService) List(ctx context.Context) ([]model.Site, error) {
	var sites []model.Site
	err := s.repo.DB.WithContext(ctx).Order("created_at asc").Find(&sites).Error
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
// Now uses helper.TestSiteConnectivity with browser-like headers
// and optional FlareSolverr support.
func (s *SiteService) TestConnection(ctx context.Context, id string) (bool, string, error) {
	site, err := s.FindByID(ctx, id)
	if err != nil || site == nil {
		return false, "site not found", err
	}

	// Get timeout from site config (default 15 seconds)
	timeout := site.Timeout
	if timeout <= 0 {
		timeout = 15
	}

	// TODO: Get flareSolverrURL from config (e.g. from config.yaml)
	// For now, pass empty string to skip FlareSolverr
	flareSolverrURL := "" // TODO: load from config

	ok, msg, err := helper.TestSiteConnectivity(site, flareSolverrURL, timeout, s.log)
	if err != nil {
		now := time.Now()
		_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
			Updates(map[string]any{"last_error": err.Error(), "last_check_at": &now}).Error
		return false, err.Error(), nil
	}

	loginStatus := "ok"
	if !ok {
		loginStatus = "fail"
	}
	now := time.Now()
	_ = s.repo.DB.WithContext(ctx).Model(&model.Site{}).Where("id = ?", id).
		Updates(map[string]any{"last_error": loginStatus, "last_check_at": &now}).Error
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
		cfg := siteModelToConfig(&sites[i])
		result, err := adapter.Search(ctx, cfg, keyword, 1)
		if err != nil {
			s.log.Debug("site search failed",
				zap.String("site", sites[i].Name), zap.Error(err))
			continue
		}
		for _, item := range result.Items {
			results = append(results, SearchResult{
				SiteName:    sites[i].Name,
				SiteID:      sites[i].ID,
				Title:       item.Title,
				TorrentURL:  item.DetailURL,
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

// siteModelToConfig 将 model.Site 转换为适配器使用的 SiteConfig。
func siteModelToConfig(s *model.Site) SiteConfig {
	return SiteConfig{
		Name:       s.Name,
		Type:       s.Type,
		URL:        s.URL,
		AuthType:   s.AuthType,
		Cookie:     s.Cookie,
		APIKey:     s.APIKey,
		AuthHeader: s.AuthHeader,
	}
}

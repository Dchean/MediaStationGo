package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// SearchResult is one torrent returned by a site adapter search.
type SearchResult struct {
	SiteName      string `json:"site_name"`
	SiteID        string `json:"site_id"`
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle,omitempty"`
	TorrentURL    string `json:"torrent_url"`
	DownloadURL   string `json:"download_url"`
	Category      string `json:"category,omitempty"`
	SearchKeyword string `json:"search_keyword,omitempty"`
	Size          int64  `json:"size"`
	Seeders       int    `json:"seeders"`
	Leechers      int    `json:"leechers"`
	Free          bool   `json:"free"`
}

// Search fans out a keyword query to every enabled site and returns
// merged results sorted by seeders descending.
// Uses concurrent search with sync.WaitGroup for performance.
func (s *SiteService) Search(ctx context.Context, keyword string) ([]SearchResult, error) {
	if strings.TrimSpace(keyword) == "" {
		return []SearchResult{}, nil
	}
	sites, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var (
		mu           sync.Mutex
		wg           sync.WaitGroup
		enabledCount int
		failedCount  int
		failureErrs  []error
		failures     []string
		results      []SearchResult
	)

	for i := range sites {
		if !sites[i].Enabled {
			continue
		}
		enabledCount++
		wg.Add(1)
		go func(site model.Site) {
			defer wg.Done()

			adapter := NewSiteAdapter(&site)
			if adapter == nil {
				mu.Lock()
				failedCount++
				err := fmt.Errorf("%s: unsupported site type %s", site.Name, site.Type)
				failureErrs = append(failureErrs, err)
				failures = append(failures, err.Error())
				mu.Unlock()
				return
			}

			cfg := s.siteModelToConfig(&site)

			// Use site timeout or default 30s
			timeout := time.Duration(site.Timeout) * time.Second
			if timeout <= 0 {
				timeout = 30 * time.Second
			}
			ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			result, err := adapter.Search(ctxWithTimeout, cfg, keyword, 1)
			if err != nil {
				mu.Lock()
				failedCount++
				failureErr := fmt.Errorf("%s: %w", site.Name, err)
				failureErrs = append(failureErrs, failureErr)
				failures = append(failures, failureErr.Error())
				mu.Unlock()
				s.log.Warn("site search failed",
					zap.String("site", site.Name),
					zap.String("type", site.Type),
					zap.String("url", site.URL),
					zap.String("keyword", keyword),
					zap.Duration("timeout", timeout),
					zap.Error(err))
				return
			}
			if result == nil {
				return
			}
			items := result.Items
			if items == nil {
				items = []TorrentItem{}
			}
			for _, item := range items {
				mu.Lock()
				results = append(results, SearchResult{
					SiteName:      site.Name,
					SiteID:        site.ID,
					Title:         item.Title,
					Subtitle:      item.Subtitle,
					TorrentURL:    item.DetailURL,
					DownloadURL:   item.DownloadURL,
					Category:      item.Category,
					SearchKeyword: keyword,
					Size:          item.Size,
					Seeders:       item.Seeders,
					Leechers:      item.Leechers,
					Free:          item.Free,
				})
				mu.Unlock()
			}
		}(sites[i])
	}
	wg.Wait()

	// Ensure results is never nil (return [] instead of null in JSON)
	if results == nil {
		results = []SearchResult{}
	}

	// Sort by seeders desc.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Seeders > results[j].Seeders
	})
	if s.log != nil {
		s.log.Info("site search completed",
			zap.String("keyword", keyword),
			zap.Int("enabled_sites", enabledCount),
			zap.Int("failed_sites", failedCount),
			zap.Int("results_count", len(results)))
	}
	if enabledCount > 0 && failedCount >= enabledCount && len(results) == 0 {
		if len(failureErrs) > 0 {
			return results, fmt.Errorf("all enabled sites failed while searching %q: %w", keyword, errors.Join(failureErrs...))
		}
		return results, fmt.Errorf("all enabled sites failed while searching %q: %s", keyword, strings.Join(failures, "; "))
	}
	return results, nil
}

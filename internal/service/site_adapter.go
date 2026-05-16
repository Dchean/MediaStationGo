// Package service — site adapter implementations for PT/BT tracker search.
//
// Each adapter knows how to search a specific site type and parse the
// results into a uniform SiteResourceItem slice. The factory function
// NewSiteAdapter picks the right adapter based on site.SiteType.
//
// Supported site types:
//   nexusphp   — HTML scraping of torrents.php (most CN PT sites)
//   gazelle    — JSON API /ajax.php?action=browse (HDBits, OPS, RED)
//   unit3d     — REST API /api/torrents/filter (BeyondHD, BluTopia)
//   mteam      — M-Team v3 REST API /api/torrent/search
//   custom_rss — RSS/Atom feed parsing (generic)
package service

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// SiteResourceItem is one torrent result returned by an adapter.
type SiteResourceItem struct {
	Title       string
	TorrentURL  string
	DownloadURL string
	Size        int64
	Seeders     int
	Leechers    int
	Free        bool
}

// SiteAdapter is the interface every site-type implementation satisfies.
type SiteAdapter interface {
	Search(ctx context.Context, keyword string) ([]SiteResourceItem, error)
}

// NewSiteAdapter picks the correct adapter for the given site config.
// Returns nil for unknown site types.
func NewSiteAdapter(site *model.Site) SiteAdapter {
	switch site.SiteType {
	case "nexusphp":
		return &nexusPhpAdapter{site: site}
	case "gazelle":
		return &gazelleAdapter{site: site}
	case "unit3d":
		return &unit3dAdapter{site: site}
	case "mteam":
		return &mteamAdapter{site: site}
	case "custom_rss":
		return &rssAdapter{site: site}
	default:
		return &nexusPhpAdapter{site: site}
	}
}

// ─── Shared helpers ──────────────────────────────────────────────────────────

func siteClient(site *model.Site) *http.Client {
	timeout := time.Duration(site.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func siteHeaders(site *model.Site) http.Header {
	h := http.Header{}
	h.Set("User-Agent", effectiveUA(site))
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	h.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	switch site.AuthType {
	case "cookie":
		if site.Cookie != "" {
			h.Set("Cookie", site.Cookie)
		}
	case "api_key":
		if site.APIKey != "" {
			h.Set("x-api-key", site.APIKey)
		}
	case "authorization":
		if site.AuthHeader != "" {
			h.Set("Authorization", site.AuthHeader)
		}
	}
	return h
}

var sizeUnits = map[string]int64{
	"TIB": 1 << 40, "GIB": 1 << 30, "MIB": 1 << 20, "KIB": 1 << 10,
	"TB": 1e12, "GB": 1e9, "MB": 1e6, "KB": 1e3, "B": 1,
}

func parseSize(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ",", ".")
	for unit, mult := range sizeUnits {
		if strings.Contains(s, unit) {
			numStr := strings.TrimSpace(strings.ReplaceAll(s, unit, ""))
			if f, err := strconv.ParseFloat(numStr, 64); err == nil {
				return int64(f * float64(mult))
			}
		}
	}
	return 0
}

// ─── NexusPHP adapter ────────────────────────────────────────────────────────

type nexusPhpAdapter struct{ site *model.Site }

func (a *nexusPhpAdapter) Search(ctx context.Context, keyword string) ([]SiteResourceItem, error) {
	baseURL := strings.TrimRight(a.site.BaseURL, "/")
	u := fmt.Sprintf("%s/torrents.php?search=%s&inclbookmarked=0&incldead=0",
		baseURL, url.QueryEscape(keyword))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = siteHeaders(a.site)

	resp, err := siteClient(a.site).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("nexusphp search: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Simple regex-based extraction (avoids heavyweight HTML parser dep).
	// NexusPHP pages have torrent rows with links to details.php?id=N and
	// download.php?id=N. We extract titles from <a href="details.php?id=...">
	titleRe := regexp.MustCompile(`<a[^>]+href="details\.php\?id=(\d+)[^"]*"[^>]*>([^<]+)</a>`)
	matches := titleRe.FindAllStringSubmatch(string(body), -1)

	var results []SiteResourceItem
	seen := map[string]bool{}
	for _, m := range matches {
		tid := m[1]
		title := strings.TrimSpace(m[2])
		if title == "" || seen[tid] {
			continue
		}
		seen[tid] = true
		results = append(results, SiteResourceItem{
			Title:       title,
			TorrentURL:  fmt.Sprintf("%s/details.php?id=%s", baseURL, tid),
			DownloadURL: fmt.Sprintf("%s/download.php?id=%s", baseURL, tid),
		})
	}
	return results, nil
}

// ─── Gazelle adapter ─────────────────────────────────────────────────────────

type gazelleAdapter struct{ site *model.Site }

func (a *gazelleAdapter) Search(ctx context.Context, keyword string) ([]SiteResourceItem, error) {
	baseURL := strings.TrimRight(a.site.BaseURL, "/")
	u := fmt.Sprintf("%s/ajax.php?action=browse&searchstr=%s",
		baseURL, url.QueryEscape(keyword))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = siteHeaders(a.site)
	req.Header.Set("Accept", "application/json")

	resp, err := siteClient(a.site).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gazelle search: HTTP %d", resp.StatusCode)
	}

	type torrent struct {
		ID       int  `json:"torrentId"`
		Size     int64 `json:"size"`
		Seeders  int  `json:"seeders"`
		Leechers int  `json:"leechers"`
		Free     bool `json:"isFreeleech"`
		Format   string `json:"format"`
		Encoding string `json:"encoding"`
	}
	type group struct {
		GroupID   int       `json:"groupId"`
		GroupName string    `json:"groupName"`
		Artist    string    `json:"artist"`
		Torrents  []torrent `json:"torrents"`
	}
	type response struct {
		Status   string `json:"status"`
		Response struct {
			Results []group `json:"results"`
		} `json:"response"`
	}

	var data response
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.Status != "success" {
		return nil, fmt.Errorf("gazelle: status=%s", data.Status)
	}

	var results []SiteResourceItem
	for _, g := range data.Response.Results {
		for _, t := range g.Torrents {
			title := strings.TrimSpace(fmt.Sprintf("%s - %s [%s/%s]",
				g.Artist, g.GroupName, t.Format, t.Encoding))
			results = append(results, SiteResourceItem{
				Title:       title,
				TorrentURL:  fmt.Sprintf("%s/torrents.php?id=%d&torrentid=%d", baseURL, g.GroupID, t.ID),
				DownloadURL: fmt.Sprintf("%s/torrents.php?action=download&id=%d", baseURL, t.ID),
				Size:        t.Size,
				Seeders:     t.Seeders,
				Leechers:    t.Leechers,
				Free:        t.Free,
			})
		}
	}
	return results, nil
}

// ─── UNIT3D adapter ──────────────────────────────────────────────────────────

type unit3dAdapter struct{ site *model.Site }

func (a *unit3dAdapter) Search(ctx context.Context, keyword string) ([]SiteResourceItem, error) {
	baseURL := strings.TrimRight(a.site.BaseURL, "/")
	params := url.Values{"name": {keyword}, "perPage": {"50"}}
	if a.site.APIKey != "" {
		params.Set("api_token", a.site.APIKey)
	}
	u := fmt.Sprintf("%s/api/torrents/filter?%s", baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = siteHeaders(a.site)
	req.Header.Set("Accept", "application/json")

	resp, err := siteClient(a.site).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("unit3d search: HTTP %d", resp.StatusCode)
	}

	type attrs struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Seeders  int    `json:"seeders"`
		Leechers int    `json:"leechers"`
		Free     any    `json:"freeleech"`
		DLLink   string `json:"download_link"`
	}
	type item struct {
		ID         int   `json:"id"`
		Attributes attrs `json:"attributes"`
	}
	type page struct {
		Data []item `json:"data"`
	}

	var data page
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []SiteResourceItem
	for _, it := range data.Data {
		a := it.Attributes
		if a.Name == "" {
			a = attrs(it.Attributes)
			if a.ID == 0 {
				a.ID = it.ID
			}
		}
		tid := a.ID
		if tid == 0 {
			tid = it.ID
		}
		free := false
		switch v := a.Free.(type) {
		case bool:
			free = v
		case float64:
			free = v > 0
		}
		dlURL := a.DLLink
		if dlURL == "" {
			dlURL = fmt.Sprintf("%s/torrents/%d/download", baseURL, tid)
		}
		results = append(results, SiteResourceItem{
			Title:       a.Name,
			TorrentURL:  fmt.Sprintf("%s/torrents/%d", baseURL, tid),
			DownloadURL: dlURL,
			Size:        a.Size,
			Seeders:     a.Seeders,
			Leechers:    a.Leechers,
			Free:        free,
		})
	}
	return results, nil
}

// ─── M-Team adapter ─────────────────────────────────────────────────────────

type mteamAdapter struct{ site *model.Site }

func (a *mteamAdapter) Search(ctx context.Context, keyword string) ([]SiteResourceItem, error) {
	baseURL := strings.TrimRight(a.site.BaseURL, "/")
	apiURL := baseURL + "/api/torrent/search"

	payload, _ := json.Marshal(map[string]any{
		"pageNumber": 1,
		"pageSize":   50,
		"keyword":    keyword,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL,
		strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", effectiveUA(a.site))
	if a.site.APIKey != "" {
		req.Header.Set("x-api-key", a.site.APIKey)
	}

	resp, err := siteClient(a.site).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mteam search: HTTP %d", resp.StatusCode)
	}

	type statusInfo struct {
		Seeders  int    `json:"seeders"`
		Leechers int    `json:"leechers"`
		Discount string `json:"discount"`
	}
	type torrentItem struct {
		ID     any        `json:"id"`
		Name   string     `json:"name"`
		Title  string     `json:"title"`
		Size   any        `json:"size"`
		Status statusInfo `json:"status"`
	}
	type dataWrap struct {
		Data []torrentItem `json:"data"`
	}
	type apiResp struct {
		Code    any      `json:"code"`
		Message string   `json:"message"`
		Data    dataWrap `json:"data"`
	}

	var data apiResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	codeStr := fmt.Sprintf("%v", data.Code)
	if codeStr != "0" {
		return nil, fmt.Errorf("mteam: code=%v msg=%s", data.Code, data.Message)
	}

	var results []SiteResourceItem
	for _, item := range data.Data.Data {
		tidStr := fmt.Sprintf("%v", item.ID)
		title := item.Name
		if title == "" {
			title = item.Title
		}
		if title == "" || tidStr == "" {
			continue
		}
		var size int64
		switch v := item.Size.(type) {
		case float64:
			size = int64(v)
		case string:
			size, _ = strconv.ParseInt(v, 10, 64)
		}
		free := item.Status.Discount != "" && item.Status.Discount != "normal"

		results = append(results, SiteResourceItem{
			Title:       title,
			TorrentURL:  fmt.Sprintf("%s/detail/%s", baseURL, tidStr),
			DownloadURL: fmt.Sprintf("%s/api/torrent/genDlToken?id=%s", baseURL, tidStr),
			Size:        size,
			Seeders:     item.Status.Seeders,
			Leechers:    item.Status.Leechers,
			Free:        free,
		})
	}
	return results, nil
}

// ─── Custom RSS adapter ─────────────────────────────────────────────────────

type rssAdapter struct{ site *model.Site }

func (a *rssAdapter) Search(ctx context.Context, keyword string) ([]SiteResourceItem, error) {
	rssURL := a.site.RSSURL
	if rssURL == "" {
		rssURL = a.site.BaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rssURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = siteHeaders(a.site)

	resp, err := siteClient(a.site).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rss: HTTP %d", resp.StatusCode)
	}

	type enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
	}
	type rssItem struct {
		Title     string    `xml:"title"`
		Link      string    `xml:"link"`
		Enclosure enclosure `xml:"enclosure"`
	}
	type channel struct {
		Items []rssItem `xml:"item"`
	}
	type rss struct {
		Channel channel `xml:"channel"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var feed rss
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}

	kw := strings.ToLower(keyword)
	var results []SiteResourceItem
	for _, item := range feed.Channel.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		// Filter by keyword (simple contains).
		if kw != "" && !strings.Contains(strings.ToLower(title), kw) {
			continue
		}
		dlURL := item.Enclosure.URL
		if dlURL == "" {
			dlURL = item.Link
		}
		results = append(results, SiteResourceItem{
			Title:       title,
			TorrentURL:  item.Link,
			DownloadURL: dlURL,
			Size:        item.Enclosure.Length,
		})
	}
	return results, nil
}

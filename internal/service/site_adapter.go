// Package service — PT 站点适配器接口及 6 种适配器实现。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/helper"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// SiteConfig 站点配置（从 model.Site 解密后的纯文本）。
type SiteConfig struct {
	Name            string
	Type            string
	URL             string
	AuthType        string
	Cookie          string
	APIKey          string
	AuthHeader      string
	UserAgent        string            // 自定义 User-Agent
	Timeout          time.Duration     // 请求超时
	Extra            map[string]string // JSON 扩展配置
	FlareSolverrURL  string            // FlareSolverr 服务地址（用于浏览器模拟绕过 Cloudflare/WAF）
}

// SiteSearchResult 站点搜索结果（按站点分组的批量搜索结果）。
type SiteSearchResult struct {
	SiteName string        `json:"site_name"`
	Items    []TorrentItem `json:"items"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
}

// TorrentItem 种子条目。
type TorrentItem struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Subtitle    string     `json:"subtitle"`
	Category    string     `json:"category"`
	Size        int64      `json:"size"`
	Seeders     int        `json:"seeders"`
	Leechers    int        `json:"leechers"`
	Snatched    int        `json:"snatched"`
	Free        bool       `json:"free"`
	FreeEndAt   *time.Time `json:"free_end_at"`
	UploadTime  time.Time  `json:"upload_time"`
	DetailURL   string     `json:"detail_url"`
	DownloadURL string     `json:"download_url"`
}

// TorrentDetail 种子详情。
type TorrentDetail struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Subtitle    string     `json:"subtitle"`
	Category    string     `json:"category"`
	Size        int64      `json:"size"`
	Seeders     int        `json:"seeders"`
	Leechers    int        `json:"leechers"`
	Snatched    int        `json:"snatched"`
	Free        bool       `json:"free"`
	FreeEndAt   *time.Time `json:"free_end_at"`
	UploadTime  time.Time  `json:"upload_time"`
	DetailURL   string     `json:"detail_url"`
	DownloadURL string     `json:"download_url"`
	InfoHash    string     `json:"info_hash,omitempty"`
	ImdbID      string     `json:"imdb_id,omitempty"`
	Description string     `json:"description,omitempty"`
	Files       []string   `json:"files,omitempty"`
}

// SiteAdapter 站点适配器接口。
type SiteAdapter interface {
	// Authenticate 测试站点认证是否有效。
	Authenticate(ctx context.Context, cfg SiteConfig) error

	// Search 搜索种子。
	Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error)

	// Browse 浏览种子列表。
	Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error)

	// GetDetail 获取种子详情。
	GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error)

	// GetDownloadURL 获取下载链接。
	GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error)
}

// newHTTPClient 创建带有认证头的 HTTP 客户端。
func newHTTPClient(cfg SiteConfig, timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}

// buildRequest 构建带认证的 HTTP 请求。
func buildRequest(ctx context.Context, method, rawURL string, cfg SiteConfig, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}

	switch cfg.AuthType {
	case "cookie":
		if cfg.Cookie != "" {
			req.Header.Set("Cookie", cfg.Cookie)
		}
	case "api_key":
		// MTeam 使用 Authorization: Bearer 格式
		if cfg.Type == "mteam" {
			if cfg.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
			}
		} else if cfg.APIKey != "" {
			req.Header.Set("X-API-Key", cfg.APIKey)
		}
	case "auth_header":
		if cfg.AuthHeader != "" {
			parts := strings.SplitN(cfg.AuthHeader, ":", 2)
			if len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			} else {
				req.Header.Set("Authorization", "Bearer "+cfg.AuthHeader)
			}
		}
	}

	// 使用 SiteConfig 中的 UserAgent（如果提供），否则使用默认值
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = model.DefaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	return req, nil
}

// doRequest 执行 HTTP 请求并返回响应体。
// 当 cfg.FlareSolverrURL 已配置且方法为 GET 时，通过 FlareSolverr 代理请求
// 以绕过 Cloudflare/WAF 挑战验证。
func doRequest(ctx context.Context, client *http.Client, method, rawURL string, cfg SiteConfig, body io.Reader) ([]byte, int, error) {
	// ── FlareSolverr 浏览器模拟路径（仅 GET） ──────────────────────────
	if cfg.FlareSolverrURL != "" && method == "GET" {
		timeout := int(cfg.Timeout.Seconds())
		if timeout <= 0 {
			timeout = 30
		}
		pageBody, err := helper.FetchURLWithFlareSolverr(
			cfg.FlareSolverrURL, rawURL, cfg.Cookie, timeout, "", nil)
		if err != nil {
			return nil, 0, fmt.Errorf("flareSolverr: %w", err)
		}
		return []byte(pageBody), http.StatusOK, nil
	}

	// ── 直接 HTTP 请求路径 ─────────────────────────────────────────────
	req, err := buildRequest(ctx, method, rawURL, cfg, body)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

// ─── NexusPHP 适配器 ─────────────────────────────────────────────────────────

// NexusPHPAdapter NexusPHP 框架适配器（馒头、HDHome、CHDBits 等）。
type NexusPHPAdapter struct {
	client *http.Client
}

// NewNexusPHPAdapter 创建 NexusPHP 适配器。
func NewNexusPHPAdapter() *NexusPHPAdapter {
	return &NexusPHPAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *NexusPHPAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	resp, err := buildRequest(ctx, "GET", cfg.URL+"/index.php", cfg, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpResp, err := a.client.Do(resp)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode == http.StatusFound {
		return fmt.Errorf("authentication failed: redirected to login page")
	}
	if httpResp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed: status %d", httpResp.StatusCode)
	}

	body, _ := io.ReadAll(httpResp.Body)
	bodyStr := string(body)
	// NexusPHP 登录页面通常包含 logout 或 userdetails
	if strings.Contains(bodyStr, "userdetails") || strings.Contains(bodyStr, "logout") {
		return nil
	}
	// Check for common login indicators
	if strings.Contains(bodyStr, "login") && !strings.Contains(bodyStr, "userdetails") {
		return fmt.Errorf("authentication failed: not logged in")
	}
	return nil
}

func (a *NexusPHPAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	params.Set("search", keyword)
	params.Set("page", strconv.Itoa(page))
	params.Set("inclbookmarked", "0")
	params.Set("incldead", "0")

	u := cfg.URL + "/torrents.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("search request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	return parseNexusPHPHTML(string(data), cfg.Name, cfg.URL)
}

func (a *NexusPHPAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	if category != "" {
		params.Set("cat", category)
	}
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/torrents.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("browse request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("browse failed: status %d", status)
	}

	return parseNexusPHPHTML(string(data), cfg.Name, cfg.URL)
}

func (a *NexusPHPAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	u := cfg.URL + "/details.php?id=" + id
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("detail request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("detail failed: status %d", status)
	}

	return parseNexusPHPDetailHTML(string(data), id, cfg.URL)
}

func (a *NexusPHPAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	return cfg.URL + "/download.php?id=" + id, nil
}

// parseNexusPHPHTML 解析 NexusPHP 种子列表 HTML。
func parseNexusPHPHTML(html, siteName, baseURL string) (*SiteSearchResult, error) {
	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
		Page:     1,
	}

	// Extract table rows from torrent table
	rowRegex := regexp.MustCompile(`<tr[^>]*>\s*<td[^>]*class="rowfollow"[^>]*>.*?</tr>`)
	matches := rowRegex.FindAllString(html, -1)

	for _, row := range matches {
		item := parseNexusPHPRow(row, baseURL)
		if item.ID != "" {
			result.Items = append(result.Items, item)
		}
	}

	result.Total = len(result.Items)
	return result, nil
}

// parseNexusPHPRow 解析单行种子条目。
func parseNexusPHPRow(row, baseURL string) TorrentItem {
	item := TorrentItem{}

	// Extract torrent ID and title
	idRegex := regexp.MustCompile(`details\.php\?id=(\d+)[^"]*"[^>]*>([^<]+)`)
	idMatches := idRegex.FindStringSubmatch(row)
	if len(idMatches) >= 3 {
		item.ID = idMatches[1]
		item.Title = strings.TrimSpace(idMatches[2])
		item.DetailURL = baseURL + "/details.php?id=" + item.ID
	}

	// Extract download link
	dlRegex := regexp.MustCompile(`download\.php\?id=(\d+)`)
	if dlMatches := dlRegex.FindStringSubmatch(row); len(dlMatches) >= 2 {
		item.DownloadURL = baseURL + "/download.php?id=" + dlMatches[1]
	}

	// Extract size
	sizeRegex := regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(GB|MB|TB|KB)`)
	if sizeMatches := sizeRegex.FindStringSubmatch(row); len(sizeMatches) >= 3 {
		item.Size = parseSizeString(sizeMatches[1], sizeMatches[2])
	}

	// Extract seeders and leechers
	seedersRegex := regexp.MustCompile(`seeders[^"]*"[^>]*>(\d+)<`)
	if m := seedersRegex.FindStringSubmatch(row); len(m) >= 2 {
		item.Seeders, _ = strconv.Atoi(m[1])
	}
	leechersRegex := regexp.MustCompile(`leechers[^"]*"[^>]*>(\d+)<`)
	if m := leechersRegex.FindStringSubmatch(row); len(m) >= 2 {
		item.Leechers, _ = strconv.Atoi(m[1])
	}

	// Extract snatched
	snatchedRegex := regexp.MustCompile(`snatched[^"]*"[^>]*>(\d+)`)
	if m := snatchedRegex.FindStringSubmatch(row); len(m) >= 2 {
		item.Snatched, _ = strconv.Atoi(m[1])
	}

	// Check for free flag
	freeRegex := regexp.MustCompile(`(?i)(class="free|free2|twoupfree|free_download|促销|免费)`)
	item.Free = freeRegex.MatchString(row)

	// Extract upload time
	timeRegex := regexp.MustCompile(`(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2})`)
	if m := timeRegex.FindStringSubmatch(row); len(m) >= 2 {
		if t, err := time.Parse("2006-01-02 15:04", m[1]); err == nil {
			item.UploadTime = t
		}
	}

	// Extract category
	catRegex := regexp.MustCompile(`cat=(\d+)[^"]*"[^>]*title="([^"]+)"`)
	if m := catRegex.FindStringSubmatch(row); len(m) >= 3 {
		item.Category = strings.TrimSpace(m[2])
	}

	return item
}

// parseNexusPHPDetailHTML 解析种子详情页。
func parseNexusPHPDetailHTML(html, id, baseURL string) (*TorrentDetail, error) {
	detail := &TorrentDetail{
		ID:        id,
		DetailURL: baseURL + "/details.php?id=" + id,
	}

	// Title
	titleRegex := regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`)
	if m := titleRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.Title = strings.TrimSpace(m[1])
	}

	// Subtitle
	subRegex := regexp.MustCompile(`<span[^>]*class="[^"]*sub[^"]*"[^>]*>([^<]+)</span>`)
	if m := subRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.Subtitle = strings.TrimSpace(m[1])
	}

	// Info hash
	hashRegex := regexp.MustCompile(`(?i)info_hash[^<]*</td>\s*<td[^>]*>([^<]+)</td>`)
	if m := hashRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.InfoHash = strings.TrimSpace(m[1])
	}

	// IMDB ID
	imdbRegex := regexp.MustCompile(`(?i)imdb[^<]*</td>\s*<td[^>]*>[^<]*(tt\d+)`)
	if m := imdbRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.ImdbID = m[1]
	}

	// Size
	sizeRegex := regexp.MustCompile(`(?i)size[^<]*</td>\s*<td[^>]*>(\d+\.?\d*)\s*(GB|MB|TB|KB)`)
	if m := sizeRegex.FindStringSubmatch(html); len(m) >= 3 {
		detail.Size = parseSizeString(m[1], m[2])
	}

	// Seeders / Leechers / Snatched
	slRegex := regexp.MustCompile(`seeders[^<]*</td>\s*<td[^>]*>(\d+)</td>\s*<td[^>]*>\s*</td>\s*<td[^>]*>\s*</td>\s*<td[^>]*>leechers[^<]*</td>\s*<td[^>]*>(\d+)`)
	if m := slRegex.FindStringSubmatch(html); len(m) >= 3 {
		detail.Seeders, _ = strconv.Atoi(m[1])
		detail.Leechers, _ = strconv.Atoi(m[2])
	}

	snRegex := regexp.MustCompile(`(?i)times completed[^<]*</td>\s*<td[^>]*>(\d+)`)
	if m := snRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.Snatched, _ = strconv.Atoi(m[1])
	}

	// Description
	descRegex := regexp.MustCompile(`(?i)<div[^>]*id="kdescr"[^>]*>(.*?)</div>`)
	if m := descRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.Description = stripHTML(m[1])
	}

	detail.DownloadURL = baseURL + "/download.php?id=" + id
	detail.Free = strings.Contains(html, "free") || strings.Contains(html, "免费")
	return detail, nil
}

// ─── Gazelle 适配器 ──────────────────────────────────────────────────────────

// GazelleAdapter Gazelle 框架适配器（What.cd 开源）。
type GazelleAdapter struct {
	client *http.Client
}

// NewGazelleAdapter 创建 Gazelle 适配器。
func NewGazelleAdapter() *GazelleAdapter {
	return &GazelleAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *GazelleAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	u := cfg.URL + "/ajax.php?action=index"
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("authenticate failed: status %d", status)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if statusMsg, ok := result["status"].(string); ok && statusMsg == "failure" {
		return fmt.Errorf("authentication failed: %v", result["error"])
	}
	return nil
}

func (a *GazelleAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	params.Set("action", "browse")
	params.Set("searchstr", keyword)
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/ajax.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	return parseGazelleJSON(data, cfg.Name, cfg.URL)
}

func (a *GazelleAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	params.Set("action", "browse")
	if category != "" {
		params.Set("filter_cat["+category+"]", "1")
	}
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/ajax.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("browse failed: status %d", status)
	}

	return parseGazelleJSON(data, cfg.Name, cfg.URL)
}

func (a *GazelleAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	params := url.Values{}
	params.Set("action", "torrent")
	params.Set("id", id)

	u := cfg.URL + "/ajax.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("detail: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("detail failed: status %d", status)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	torrent, ok := resp["torrent"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("torrent not found")
	}

	detail := &TorrentDetail{
		ID:          id,
		DetailURL:   cfg.URL + "/torrents.php?torrentid=" + id,
		DownloadURL: cfg.URL + "/torrents.php?action=download&id=" + id,
	}

	if v, ok := torrent["groupName"].(string); ok {
		detail.Title = v
	}
	if v, ok := torrent["subName"].(string); ok {
		detail.Subtitle = v
	}
	if v, ok := torrent["size"].(float64); ok {
		detail.Size = int64(v)
	}
	if v, ok := torrent["seeders"].(float64); ok {
		detail.Seeders = int(v)
	}
	if v, ok := torrent["leechers"].(float64); ok {
		detail.Leechers = int(v)
	}
	if v, ok := torrent["snatched"].(float64); ok {
		detail.Snatched = int(v)
	}
	if v, ok := torrent["freeTorrent"].(string); ok && v == "1" {
		detail.Free = true
	}
	if v, ok := torrent["freeTorrent"].(bool); ok {
		detail.Free = v
	}
	if v, ok := torrent["infoHash"].(string); ok {
		detail.InfoHash = v
	}
	if v, ok := torrent["groupDesc"].(string); ok {
		detail.Description = stripHTML(v)
	}

	return detail, nil
}

func (a *GazelleAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	return cfg.URL + "/torrents.php?action=download&id=" + id, nil
}

// parseGazelleJSON 解析 Gazelle JSON 响应。
func parseGazelleJSON(data []byte, siteName, baseURL string) (*SiteSearchResult, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
	}

	if status, ok := resp["status"].(string); ok && status == "failure" {
		return result, nil
	}

	results, ok := resp["results"].([]interface{})
	if !ok {
		return result, nil
	}

	for _, r := range results {
		torrent, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		item := TorrentItem{}
		if v, ok := torrent["torrentId"].(float64); ok {
			item.ID = strconv.Itoa(int(v))
		}
		if v, ok := torrent["groupName"].(string); ok {
			item.Title = v
		}
		if v, ok := torrent["artist"].(string); ok {
			item.Subtitle = v
		}
		if v, ok := torrent["category"].(string); ok {
			item.Category = v
		}
		if v, ok := torrent["size"].(float64); ok {
			item.Size = int64(v)
		}
		if v, ok := torrent["seeders"].(float64); ok {
			item.Seeders = int(v)
		}
		if v, ok := torrent["leechers"].(float64); ok {
			item.Leechers = int(v)
		}
		if v, ok := torrent["snatched"].(float64); ok {
			item.Snatched = int(v)
		}
		if v, ok := torrent["freeTorrent"].(string); ok && v == "1" {
			item.Free = true
		}
		if v, ok := torrent["freeTorrent"].(bool); ok {
			item.Free = v
		}
		if v, ok := torrent["time"].(string); ok {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				item.UploadTime = t
			}
		}

		item.DetailURL = baseURL + "/torrents.php?torrentid=" + item.ID
		item.DownloadURL = baseURL + "/torrents.php?action=download&id=" + item.ID
		result.Items = append(result.Items, item)
	}

	if total, ok := resp["totalResults"].(float64); ok {
		result.Total = int(total)
	} else {
		result.Total = len(result.Items)
	}
	return result, nil
}

// ─── UNIT3D 适配器 ───────────────────────────────────────────────────────────

// UNIT3DAdapter UNIT3D 框架适配器。
type UNIT3DAdapter struct {
	client *http.Client
}

// NewUNIT3DAdapter 创建 UNIT3D 适配器。
func NewUNIT3DAdapter() *UNIT3DAdapter {
	return &UNIT3DAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *UNIT3DAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	u := cfg.URL + "/api/torrents?limit=1"
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("authentication failed: status %d", status)
	}
	if status != http.StatusOK {
		return fmt.Errorf("authenticate failed: status %d", status)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err == nil {
		if errMsg, ok := resp["message"].(string); ok {
			return fmt.Errorf("authentication failed: %s", errMsg)
		}
	}
	return nil
}

func (a *UNIT3DAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	params.Set("search", keyword)
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/api/torrents?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	return parseUNIT3DJSON(data, cfg.Name, cfg.URL)
}

func (a *UNIT3DAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	if category != "" {
		params.Set("category", category)
	}
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/api/torrents?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("browse failed: status %d", status)
	}

	return parseUNIT3DJSON(data, cfg.Name, cfg.URL)
}

func (a *UNIT3DAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	u := cfg.URL + "/api/torrents/" + id
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("detail: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("detail failed: status %d", status)
	}

	var torrent map[string]interface{}
	if err := json.Unmarshal(data, &torrent); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	detail := &TorrentDetail{
		ID:        id,
		DetailURL: cfg.URL + "/torrents/" + id,
	}

	if v, ok := torrent["name"].(string); ok {
		detail.Title = v
	}
	if v, ok := torrent["description"].(string); ok {
		detail.Description = stripHTML(v)
	}
	if v, ok := torrent["size"].(float64); ok {
		detail.Size = int64(v)
	}
	if v, ok := torrent["seeders"].(float64); ok {
		detail.Seeders = int(v)
	}
	if v, ok := torrent["leechers"].(float64); ok {
		detail.Leechers = int(v)
	}
	if v, ok := torrent["times_completed"].(float64); ok {
		detail.Snatched = int(v)
	}
	if v, ok := torrent["free"].(bool); ok {
		detail.Free = v
	}
	if v, ok := torrent["info_hash"].(string); ok {
		detail.InfoHash = v
	}

	detail.DownloadURL = cfg.URL + "/api/torrents/" + id + "/download"
	return detail, nil
}

func (a *UNIT3DAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	return cfg.URL + "/api/torrents/" + id + "/download", nil
}

// parseUNIT3DJSON 解析 UNIT3D JSON 响应。
func parseUNIT3DJSON(data []byte, siteName, baseURL string) (*SiteSearchResult, error) {
	var resp struct {
		Data []map[string]interface{} `json:"data"`
		Meta struct {
			Total       int `json:"total"`
			CurrentPage int `json:"current_page"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
		Page:     resp.Meta.CurrentPage,
		Total:    resp.Meta.Total,
	}

	for _, t := range resp.Data {
		item := TorrentItem{}
		if v, ok := t["id"].(float64); ok {
			item.ID = strconv.Itoa(int(v))
		}
		if v, ok := t["name"].(string); ok {
			item.Title = v
		}
		if v, ok := t["category"].(map[string]interface{}); ok {
			if name, ok := v["name"].(string); ok {
				item.Category = name
			}
		}
		if v, ok := t["size"].(float64); ok {
			item.Size = int64(v)
		}
		if v, ok := t["seeders"].(float64); ok {
			item.Seeders = int(v)
		}
		if v, ok := t["leechers"].(float64); ok {
			item.Leechers = int(v)
		}
		if v, ok := t["times_completed"].(float64); ok {
			item.Snatched = int(v)
		}
		if v, ok := t["free"].(bool); ok {
			item.Free = v
		}
		if v, ok := t["created_at"].(string); ok {
			if t2, err := time.Parse(time.RFC3339, v); err == nil {
				item.UploadTime = t2
			}
		}

		item.DetailURL = baseURL + "/torrents/" + item.ID
		item.DownloadURL = baseURL + "/api/torrents/" + item.ID + "/download"
		result.Items = append(result.Items, item)
	}

	return result, nil
}

// ─── MTeam 适配器 ────────────────────────────────────────────────────────────

// MTeamAdapter MTeam.cc 独立站适配器。
type MTeamAdapter struct {
	client *http.Client
}

// NewMTeamAdapter 创建 MTeam 适配器。
func NewMTeamAdapter() *MTeamAdapter {
	return &MTeamAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *MTeamAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	u := cfg.URL + "/api/torrent/search"
	payload := `{"mode":"search","keyword":"","page":1,"pageSize":1}`
	data, status, err := doRequestJSON(ctx, a.client, "POST", u, cfg, []byte(payload))
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed: unauthorized")
	}
	if status != http.StatusOK {
		return fmt.Errorf("authenticate failed: status %d", status)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err == nil {
		if code, ok := resp["code"].(float64); ok && code != 0 {
			return fmt.Errorf("authentication failed: code %v", code)
		}
	}
	return nil
}

func (a *MTeamAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	payload := map[string]interface{}{
		"mode":     "search",
		"keyword":  keyword,
		"page":     page,
		"pageSize": 50,
	}
	body, _ := json.Marshal(payload)

	u := cfg.URL + "/api/torrent/search"
	data, status, err := doRequestJSON(ctx, a.client, "POST", u, cfg, body)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	return parseMTeamJSON(data, cfg.Name, cfg.URL)
}

func (a *MTeamAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	payload := map[string]interface{}{
		"mode":     "browse",
		"category": category,
		"page":     page,
		"pageSize": 50,
	}
	body, _ := json.Marshal(payload)

	u := cfg.URL + "/api/torrent/search"
	data, status, err := doRequestJSON(ctx, a.client, "POST", u, cfg, body)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("browse failed: status %d", status)
	}

	return parseMTeamJSON(data, cfg.Name, cfg.URL)
}

func (a *MTeamAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	u := cfg.URL + "/api/torrent/detail?id=" + id
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("detail: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("detail failed: status %d", status)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	dataField, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("detail not found")
	}

	detail := &TorrentDetail{
		ID:        id,
		DetailURL: cfg.URL + "/detail/" + id,
	}

	if v, ok := dataField["name"].(string); ok {
		detail.Title = v
	}
	if v, ok := dataField["subtitle"].(string); ok {
		detail.Subtitle = v
	}
	if v, ok := dataField["size"].(float64); ok {
		detail.Size = int64(v)
	}
	if v, ok := dataField["status"].(map[string]interface{}); ok {
		if seeders, ok := v["seeders"].(float64); ok {
			detail.Seeders = int(seeders)
		}
		if leechers, ok := v["leechers"].(float64); ok {
			detail.Leechers = int(leechers)
		}
		if snatched, ok := v["completed"].(float64); ok {
			detail.Snatched = int(snatched)
		}
	}
	if v, ok := dataField["free"].(bool); ok {
		detail.Free = v
	}
	if v, ok := dataField["download"].(string); ok {
		detail.DownloadURL = v
	}
	if v, ok := dataField["description"].(string); ok {
		detail.Description = stripHTML(v)
	}

	return detail, nil
}

func (a *MTeamAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	return cfg.URL + "/api/torrent/detail?id=" + id, nil
}

// parseMTeamJSON 解析 MTeam JSON 响应。
func parseMTeamJSON(data []byte, siteName, baseURL string) (*SiteSearchResult, error) {
	var resp struct {
		Code int                    `json:"code"`
		Data struct {
			Total  int                     `json:"total"`
			Lists  []map[string]interface{} `json:"lists"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
		Total:    resp.Data.Total,
	}

	for _, t := range resp.Data.Lists {
		item := TorrentItem{}
		if v, ok := t["id"].(string); ok {
			item.ID = v
		} else if v, ok := t["id"].(float64); ok {
			item.ID = strconv.Itoa(int(v))
		}
		if v, ok := t["name"].(string); ok {
			item.Title = v
		}
		if v, ok := t["subtitle"].(string); ok {
			item.Subtitle = v
		}
		if v, ok := t["category"].(map[string]interface{}); ok {
			if name, ok := v["name"].(string); ok {
				item.Category = name
			}
		}
		if v, ok := t["size"].(float64); ok {
			item.Size = int64(v)
		}
		if v, ok := t["status"].(map[string]interface{}); ok {
			if seeders, ok := v["seeders"].(float64); ok {
				item.Seeders = int(seeders)
			}
			if leechers, ok := v["leechers"].(float64); ok {
				item.Leechers = int(leechers)
			}
			if snatched, ok := v["completed"].(float64); ok {
				item.Snatched = int(snatched)
			}
		}
		if v, ok := t["free"].(bool); ok {
			item.Free = v
		}
		if v, ok := t["uploadTime"].(float64); ok {
			item.UploadTime = time.Unix(int64(v), 0)
		}

		item.DetailURL = baseURL + "/detail/" + item.ID
		result.Items = append(result.Items, item)
	}

	return result, nil
}

// ─── Discuz 适配器 ───────────────────────────────────────────────────────────

// DiscuzAdapter 基于 Discuz! X 的站点适配器。
type DiscuzAdapter struct {
	client *http.Client
}

// NewDiscuzAdapter 创建 Discuz 适配器。
func NewDiscuzAdapter() *DiscuzAdapter {
	return &DiscuzAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *DiscuzAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	u := cfg.URL + "/home.php?mod=space"
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if status == http.StatusFound {
		return fmt.Errorf("authentication failed: redirected to login")
	}
	if status != http.StatusOK {
		return fmt.Errorf("authenticate failed: status %d", status)
	}
	body := string(data)
	if strings.Contains(body, "login") && !strings.Contains(body, "我的空间") {
		return fmt.Errorf("authentication failed: not logged in")
	}
	return nil
}

func (a *DiscuzAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	params.Set("mod", "forum")
	params.Set("srchtxt", keyword)
	params.Set("searchsubmit", "true")
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/search.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	return parseDiscuzHTML(string(data), cfg.Name, cfg.URL)
}

func (a *DiscuzAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	if category != "" {
		params.Set("fid", category)
	}
	params.Set("page", strconv.Itoa(page))

	u := cfg.URL + "/forum.php?" + params.Encode()
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("browse: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("browse failed: status %d", status)
	}

	return parseDiscuzHTML(string(data), cfg.Name, cfg.URL)
}

func (a *DiscuzAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	u := cfg.URL + "/forum.php?mod=viewthread&tid=" + id
	data, status, err := doRequest(ctx, a.client, "GET", u, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("detail: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("detail failed: status %d", status)
	}

	html := string(data)
	detail := &TorrentDetail{
		ID:        id,
		DetailURL: cfg.URL + "/forum.php?mod=viewthread&tid=" + id,
	}

	// Title
	titleRegex := regexp.MustCompile(`<span[^>]*id="thread_subject"[^>]*>([^<]+)</span>`)
	if m := titleRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.Title = strings.TrimSpace(m[1])
	}

	// Extract magnet/torrent links
	magnetRegex := regexp.MustCompile(`magnet:\?[^\s"'<>]+`)
	if m := magnetRegex.FindString(html); m != "" {
		detail.DownloadURL = m
	}
	torrentRegex := regexp.MustCompile(`(attachment\.php\?aid=\d+)`)
	if m := torrentRegex.FindString(html); m != "" && detail.DownloadURL == "" {
		detail.DownloadURL = cfg.URL + "/" + m
	}

	// Description
	descRegex := regexp.MustCompile(`<div[^>]*class="t_fsz"[^>]*>(.*?)</div>`)
	if m := descRegex.FindStringSubmatch(html); len(m) >= 2 {
		detail.Description = stripHTML(m[1])
	}

	return detail, nil
}

func (a *DiscuzAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	return cfg.URL + "/forum.php?mod=viewthread&tid=" + id, nil
}

// parseDiscuzHTML 解析 Discuz HTML 响应。
func parseDiscuzHTML(html, siteName, baseURL string) (*SiteSearchResult, error) {
	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
		Page:     1,
	}

	// Extract thread links
	threadRegex := regexp.MustCompile(`<a[^>]*href="(?:forum\.php\?mod=viewthread&tid=|thread-(\d+)-1-1)\.html"[^>]*>([^<]+)</a>`)
	matches := threadRegex.FindAllStringSubmatch(html, -1)

	for _, m := range matches {
		item := TorrentItem{}
		if m[1] != "" {
			item.ID = m[1]
		} else {
			// Extract tid from URL
			tidRegex := regexp.MustCompile(`tid=(\d+)`)
			if tidM := tidRegex.FindStringSubmatch(m[0]); len(tidM) >= 2 {
				item.ID = tidM[1]
			}
		}
		if item.ID == "" {
			continue
		}

		item.Title = strings.TrimSpace(m[2])
		item.DetailURL = baseURL + "/forum.php?mod=viewthread&tid=" + item.ID
		item.UploadTime = time.Now()

		result.Items = append(result.Items, item)
	}

	result.Total = len(result.Items)
	return result, nil
}

// ─── Custom RSS 适配器 ───────────────────────────────────────────────────────

// CustomRSSAdapter 自定义 RSS 源适配器。
type CustomRSSAdapter struct {
	client *http.Client
}

// NewCustomRSSAdapter 创建 Custom RSS 适配器。
func NewCustomRSSAdapter() *CustomRSSAdapter {
	return &CustomRSSAdapter{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *CustomRSSAdapter) Authenticate(ctx context.Context, cfg SiteConfig) error {
	// RSS 源通常不需要认证，或者认证通过 URL 参数
	if cfg.URL == "" {
		return fmt.Errorf("RSS URL is required")
	}
	_, status, err := doRequest(ctx, a.client, "GET", cfg.URL, cfg, nil)
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("authenticate failed: status %d", status)
	}
	return nil
}

func (a *CustomRSSAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	searchURL := cfg.URL
	// If extra has search URL template, use it
	if searchTpl, ok := cfg.Extra["search_url"]; ok && searchTpl != "" {
		searchURL = strings.ReplaceAll(searchTpl, "{keyword}", url.QueryEscape(keyword))
		searchURL = strings.ReplaceAll(searchURL, "{page}", strconv.Itoa(page))
	}

	data, status, err := doRequest(ctx, a.client, "GET", searchURL, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	result, err := parseRSSXML(data, cfg.Name, keyword)
	if err != nil {
		return nil, fmt.Errorf("parse RSS: %w", err)
	}

	if page > 1 {
		// Simple pagination for RSS: skip items already seen
		start := (page - 1) * 50
		if start < len(result.Items) {
			result.Items = result.Items[start:]
		} else {
			result.Items = []TorrentItem{}
		}
	}
	result.Page = page

	return result, nil
}

func (a *CustomRSSAdapter) Browse(ctx context.Context, cfg SiteConfig, category string, page int) (*SiteSearchResult, error) {
	// RSS browse is essentially the same as search with empty keyword
	return a.Search(ctx, cfg, "", page)
}

func (a *CustomRSSAdapter) GetDetail(ctx context.Context, cfg SiteConfig, id string) (*TorrentDetail, error) {
	// RSS typically doesn't support detail page; return basic info
	return &TorrentDetail{
		ID:    id,
		Title: id,
	}, nil
}

func (a *CustomRSSAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	return id, nil // In RSS, the ID is often the download URL
}

// parseRSSXML 解析 RSS XML 内容。
func parseRSSXML(data []byte, siteName, keyword string) (*SiteSearchResult, error) {
	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
	}

	html := string(data)
	// Simple regex-based XML parsing for RSS items
	itemRegex := regexp.MustCompile(`<item>(.*?)</item>`)
	items := itemRegex.FindAllStringSubmatch(html, -1)

	for i, item := range items {
		ri := TorrentItem{}

		// Title
		titleRegex := regexp.MustCompile(`<title>(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?</title>`)
		if m := titleRegex.FindStringSubmatch(item[1]); len(m) >= 2 {
			ri.Title = strings.TrimSpace(m[1])
		}

		// Filter by keyword
		if keyword != "" && !strings.Contains(strings.ToLower(ri.Title), strings.ToLower(keyword)) {
			continue
		}

		ri.ID = strconv.Itoa(i)

		// Link
		linkRegex := regexp.MustCompile(`<link>(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?</link>`)
		if m := linkRegex.FindStringSubmatch(item[1]); len(m) >= 2 {
			ri.DetailURL = strings.TrimSpace(m[1])
			ri.DownloadURL = strings.TrimSpace(m[1])
		}

		// Description
		descRegex := regexp.MustCompile(`<description>(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?</description>`)
		if m := descRegex.FindStringSubmatch(item[1]); len(m) >= 2 {
			desc := stripHTML(m[1])
			ri.Subtitle = desc
		}

		// Size from description
		sizeRegex := regexp.MustCompile(`(\d+\.?\d*)\s*(GB|MB|TB|KB)`)
		if m := sizeRegex.FindStringSubmatch(item[1]); len(m) >= 3 {
			ri.Size = parseSizeString(m[1], m[2])
		}

		// Category
		catRegex := regexp.MustCompile(`<category>(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?</category>`)
		if m := catRegex.FindStringSubmatch(item[1]); len(m) >= 2 {
			ri.Category = strings.TrimSpace(m[1])
		}

		// Date
		dateRegex := regexp.MustCompile(`<pubDate>(?:<!\[CDATA\[)?(.*?)(?:\]\]>)?</pubDate>`)
		if m := dateRegex.FindStringSubmatch(item[1]); len(m) >= 2 {
			for _, layout := range []string{
				time.RFC1123, time.RFC1123Z, time.RFC3339,
				"2006-01-02 15:04:05", "2006-01-02T15:04:05-07:00",
			} {
				if t, err := time.Parse(layout, strings.TrimSpace(m[1])); err == nil {
					ri.UploadTime = t
					break
				}
			}
		}

		result.Items = append(result.Items, ri)
	}

	result.Total = len(result.Items)
	return result, nil
}

// TorrentDetail has a Description field used by RSS adapter.
// (Already defined above)

// ─── 辅助函数 ────────────────────────────────────────────────────────────────

// doRequestJSON 执行 JSON 请求。
func doRequestJSON(ctx context.Context, client *http.Client, method, rawURL string, cfg SiteConfig, body []byte) ([]byte, int, error) {
	req, err := buildRequest(ctx, method, rawURL, cfg, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if body != nil {
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		req.ContentLength = int64(len(body))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

// parseSizeString 将带单位的字符串转换为字节数。
func parseSizeString(value string, unit string) int64 {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(unit) {
	case "kb":
		return int64(v * 1024)
	case "mb":
		return int64(v * 1024 * 1024)
	case "gb":
		return int64(v * 1024 * 1024 * 1024)
	case "tb":
		return int64(v * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(v)
	}
}

// stripHTML 移除 HTML 标签。
func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}

// GetAdapterForType 根据站点类型返回对应的适配器实例。
func GetAdapterForType(siteType string) SiteAdapter {
	switch strings.ToLower(siteType) {
	case "nexusphp":
		return NewNexusPHPAdapter()
	case "gazelle":
		return NewGazelleAdapter()
	case "unit3d":
		return NewUNIT3DAdapter()
	case "mteam":
		return NewMTeamAdapter()
	case "discuz":
		return NewDiscuzAdapter()
	case "custom_rss":
		return NewCustomRSSAdapter()
	default:
		return NewNexusPHPAdapter()
	}
}

// NewSiteAdapter 根据站点模型创建对应的适配器。
func NewSiteAdapter(site *model.Site) SiteAdapter {
	return GetAdapterForType(site.Type)
}

// Package service — NexusPHP site adapter.
package service

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

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
	// 走 doRequest 以便复用代理 / FlareSolverr / 浏览器头。
	data, status, err := doRequest(ctx, a.client, "GET", cfg.URL+"/index.php", cfg, nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if status == http.StatusFound {
		return fmt.Errorf("authentication failed: redirected to login page")
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("authentication failed: status %d", status)
	}
	if status >= 400 {
		return fmt.Errorf("authentication failed: status %d", status)
	}

	body := string(data)
	// NexusPHP 登录后页面通常包含 logout 或 userdetails；
	// 仅当二者都不存在且明确显示登录表单时才判失败。
	if strings.Contains(body, "userdetails") || strings.Contains(body, "logout") || strings.Contains(body, "退出") {
		return nil
	}
	if strings.Contains(body, "takelogin.php") || strings.Contains(body, "id=\"loginform\"") {
		return fmt.Errorf("authentication failed: not logged in")
	}
	// 状态码 OK 但页面不含明显标记时不再武断判失败。
	return nil
}

func (a *NexusPHPAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	params := url.Values{}
	params.Set("searchstr", keyword)
	params.Set("search", keyword)
	params.Set("search_area", "0")
	params.Set("search_mode", "0")
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

	for _, row := range nexusPHPTorrentRows(html) {
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
	if link := firstNexusPHPLink(row, "details.php"); link != nil {
		item.ID = link.query.Get("id")
		item.Title = nexusPHPTitleFromLink(*link)
		item.Subtitle = nexusPHPSubtitle(row)
		item.DetailURL = resolveSiteURL(baseURL, link.href)
	}

	// Extract download link
	if link := firstNexusPHPLink(row, "download.php"); link != nil {
		item.DownloadURL = resolveSiteURL(baseURL, link.href)
	}

	// Extract size
	sizeRegex := regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(GiB|MiB|TiB|KiB|GB|MB|TB|KB)`)
	if sizeMatches := sizeRegex.FindStringSubmatch(row); len(sizeMatches) >= 3 {
		item.Size = parseSizeString(sizeMatches[1], sizeMatches[2])
	}

	// Extract seeders and leechers
	if value, ok := nexusPHPIntByClass(row, "seeders"); ok {
		item.Seeders = value
	}
	if value, ok := nexusPHPIntByClass(row, "leechers"); ok {
		item.Leechers = value
	}
	if value, ok := nexusPHPIntByClass(row, "snatched"); ok {
		item.Snatched = value
	}

	// Extract snatched
	snatchedRegex := regexp.MustCompile(`snatched[^"]*"[^>]*>(\d+)`)
	if item.Snatched == 0 {
		if m := snatchedRegex.FindStringSubmatch(row); len(m) >= 2 {
			item.Snatched, _ = strconv.Atoi(m[1])
		}
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

type nexusPHPLink struct {
	href  string
	attrs string
	text  string
	query url.Values
}

func nexusPHPTorrentRows(pageHTML string) []string {
	rowRegex := regexp.MustCompile(`(?is)<tr\b[^>]*>.*?</tr>`)
	rows := rowRegex.FindAllString(pageHTML, -1)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row), "details.php") {
			out = append(out, row)
		}
	}
	return out
}

func firstNexusPHPLink(row, path string) *nexusPHPLink {
	pattern := regexp.MustCompile(`(?is)<a\b([^>]*href\s*=\s*["']([^"']*` + regexp.QuoteMeta(path) + `[^"']*)["'][^>]*)>(.*?)</a>`)
	for _, match := range pattern.FindAllStringSubmatch(row, -1) {
		if len(match) < 4 {
			continue
		}
		href := html.UnescapeString(strings.TrimSpace(match[2]))
		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		return &nexusPHPLink{
			href:  href,
			attrs: match[1],
			text:  cleanNexusPHPText(match[3]),
			query: parsed.Query(),
		}
	}
	return nil
}

func nexusPHPTitleFromLink(link nexusPHPLink) string {
	for _, attr := range []string{"title", "data-title"} {
		if value := htmlAttr(link.attrs, attr); value != "" {
			return value
		}
	}
	return link.text
}

func nexusPHPSubtitle(row string) string {
	for _, pattern := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)<span\b[^>]*(?:class|id)\s*=\s*["'][^"']*(?:subtitle|small_descr|descr|sub)[^"']*["'][^>]*>(.*?)</span>`),
		regexp.MustCompile(`(?is)<font\b[^>]*(?:class|id)\s*=\s*["'][^"']*(?:subtitle|small_descr|descr|sub)[^"']*["'][^>]*>(.*?)</font>`),
	} {
		if match := pattern.FindStringSubmatch(row); len(match) >= 2 {
			return cleanNexusPHPText(match[1])
		}
	}
	return ""
}

func nexusPHPIntByClass(row, className string) (int, bool) {
	pattern := regexp.MustCompile(`(?is)<td\b[^>]*(?:class|id)\s*=\s*["'][^"']*` + regexp.QuoteMeta(className) + `[^"']*["'][^>]*>(.*?)</td>`)
	if match := pattern.FindStringSubmatch(row); len(match) >= 2 {
		text := cleanNexusPHPText(match[1])
		valueMatch := regexp.MustCompile(`\d+`).FindString(text)
		if valueMatch != "" {
			value, _ := strconv.Atoi(valueMatch)
			return value, true
		}
	}
	return 0, false
}

func htmlAttr(attrs, name string) string {
	pattern := regexp.MustCompile(`(?is)\b` + regexp.QuoteMeta(name) + `\s*=\s*["']([^"']*)["']`)
	if match := pattern.FindStringSubmatch(attrs); len(match) >= 2 {
		return cleanNexusPHPText(match[1])
	}
	return ""
}

func cleanNexusPHPText(value string) string {
	return strings.Join(strings.Fields(html.UnescapeString(stripHTML(value))), " ")
}

func resolveSiteURL(baseURL, href string) string {
	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return strings.TrimSpace(href)
	}
	ref, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return strings.TrimSpace(href)
	}
	return base.ResolveReference(ref).String()
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

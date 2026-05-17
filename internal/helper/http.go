// Package helper provides shared HTTP client utilities
// with browser-like headers and Cloudflare/WAF bypass support.
package helper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

// HTTPHeaderPresets returns a map of realistic browser HTTP headers.
// These mimic a real Chrome browser to avoid WAF/bot detection.
func HTTPHeaderPresets() map[string]string {
	return map[string]string{
		"User-Agent":      model.DefaultUserAgent,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Accept-Encoding": "gzip, deflate, br",
		"Connection":      "keep-alive",
		"Upgrade-Insecure-Requests": "1",
		"Sec-Fetch-Dest":          "document",
		"Sec-Fetch-Mode":          "navigate",
		"Sec-Fetch-Site":          "none",
		"Sec-Fetch-User":          "?1",
		"Cache-Control":            "max-age=0",
	}
}

// ─── FlareSolverr Support ───────────────────────────────────────────────

// FlareSolverrRequest represents a request to FlareSolverr.
type FlareSolverrRequest struct {
	Cmd       string                `json:"cmd"`
	URL       string                `json:"url"`
	Session   string                `json:"session,omitempty"`
	MaxTimeout int                   `json:"maxTimeout,omitempty"`
	Proxy     *FlareSolverrProxy  `json:"proxy,omitempty"`
	Cookies   []FlareSolverrCookie `json:"cookies,omitempty"`
}

// FlareSolverrProxy represents proxy config for FlareSolverr.
type FlareSolverrProxy struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// FlareSolverrCookie represents a cookie for FlareSolverr.
type FlareSolverrCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
}

// FlareSolverrResponse represents FlareSolverr's response.
type FlareSolverrResponse struct {
	Status   string                 `json:"status"`
	Message  string                 `json:"message"`
	Solution *FlareSolverrSolution `json:"solution,omitempty"`
}

// FlareSolverrSolution contains the solved challenge result.
type FlareSolverrSolution struct {
	URL       string                `json:"url"`
	Status    int                   `json:"status"`
	Headers   map[string]string     `json:"headers"`
	Cookies   []FlareSolverrCookie `json:"cookies"`
	UserAgent string                `json:"userAgent"`
	Response  string                `json:"response"`
}

// FetchURLWithFlareSolverr uses FlareSolverr to fetch a URL,
// bypassing Cloudflare/WAF challenges.
func FetchURLWithFlareSolverr(flareSolverrURL string, targetURL string, cookieStr string, timeout int, proxyURL string, log *zap.Logger) (string, error) {
	if flareSolverrURL == "" {
		return "", fmt.Errorf("FlareSolverr URL not configured")
	}
	if timeout <= 0 {
		timeout = 60
	}

	// Parse cookies
	var cookies []FlareSolverrCookie
	if cookieStr != "" {
		cookies = parseCookiesForFlareSolverr(cookieStr)
	}

	// Build request
	reqBody := FlareSolverrRequest{
		Cmd:       "request.get",
		URL:       targetURL,
		MaxTimeout: timeout * 1000,
		Cookies:   cookies,
	}
	if proxyURL != "" {
		reqBody.Proxy = &FlareSolverrProxy{URL: proxyURL}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal FlareSolverr request: %w", err)
	}

	// Send request to FlareSolverr
	client := &http.Client{Timeout: time.Duration(timeout+10) * time.Second}
	resp, err := client.Post(flareSolverrURL, "application/json", strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", fmt.Errorf("FlareSolverr request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read FlareSolverr response: %w", err)
	}

	var fsResp FlareSolverrResponse
	if err := json.Unmarshal(body, &fsResp); err != nil {
		return "", fmt.Errorf("failed to parse FlareSolverr response: %w", err)
	}

	if fsResp.Status != "ok" {
		return "", fmt.Errorf("FlareSolverr error: %s", fsResp.Message)
	}

	if fsResp.Solution != nil {
		return fsResp.Solution.Response, nil
	}
	return "", fmt.Errorf("FlareSolverr returned no solution")
}

// parseCookiesForFlareSolverr converts a cookie header string to FlareSolverr format.
func parseCookiesForFlareSolverr(cookieStr string) []FlareSolverrCookie {
	var cookies []FlareSolverrCookie
	parts := strings.Split(cookieStr, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			cookies = append(cookies, FlareSolverrCookie{
				Name:  kv[0],
				Value: kv[1],
			})
		}
	}
	return cookies
}

// ─── Cloudflare Challenge Detection ─────────────────────────────────────

// isCloudflareChallenge checks if the HTML content is a Cloudflare challenge page.
func IsCloudflareChallenge(html string) bool {
	challengeTitles := []string{
		"Just a moment...",
		"请稍候…",
		"DDOS-GUARD",
	}
	challengeSelectors := []string{
		"#cf-challenge-running",
		".ray_id",
		".attack-box",
		"#cf-please-wait",
		"#challenge-spinner",
		"#trk_jschal_js",
	}

	lowerHTML := strings.ToLower(html)
	for _, title := range challengeTitles {
		// Check for <title>...</title> with the challenge title
		titleLower := strings.ToLower(title)
		if strings.Contains(lowerHTML, strings.ToLower("<title>"+title)) ||
			strings.Contains(lowerHTML, titleLower) {
			return true
		}
	}

	for _, selector := range challengeSelectors {
		if strings.Contains(lowerHTML, strings.ToLower(selector)) {
			return true
		}
	}

	return false
}

// ─── Site Connectivity Test ─────────────────────────────────────────────

// TestSiteConnectivity performs a site connectivity test with browser-like headers.
// If flareSolverrURL is non-empty, it will attempt to use FlareSolverr first.
// Returns (ok, message, error).
func TestSiteConnectivity(site *model.Site, flareSolverrURL string, timeout int, log *zap.Logger) (bool, string, error) {
	// Try FlareSolverr first if configured
	if flareSolverrURL != "" {
		log.Info("Trying FlareSolverr for site test", zap.String("url", site.URL))
		body, err := FetchURLWithFlareSolverr(flareSolverrURL, site.URL, site.Cookie, timeout, "", log)
		if err == nil {
			// Successfully got page via FlareSolverr
			if IsCloudflareChallenge(body) {
				return false, "站点被 Cloudflare/WAF 拦截，但 FlareSolverr 未能完全解决", nil
			}
			return true, "连接成功 (via FlareSolverr)", nil
		}
		log.Warn("FlareSolverr failed, falling back to direct request", zap.Error(err))
		// Fall through to direct request
	}

	// Direct HTTP request with browser-like headers
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", site.URL, nil)
	if err != nil {
		return false, err.Error(), nil
	}

	// Apply browser-like headers
	headers := HTTPHeaderPresets()
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Apply auth headers
	ApplySiteAuthHeaders(req, site)

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return false, err.Error(), nil
	}
	defer resp.Body.Close()

	// Read response body for Cloudflare challenge detection
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Check for Cloudflare challenge
	if IsCloudflareChallenge(bodyStr) {
		log.Warn("Cloudflare challenge detected", zap.String("url", site.URL))
		return false, "站点被 Cloudflare/WAF 拦截，请配置 FlareSolverr 或浏览器模拟", nil
	}

	// Evaluate status code (same logic as before)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, fmt.Sprintf("连接成功 (%s)", resp.Status), nil
	case resp.StatusCode == 301 || resp.StatusCode == 302 || resp.StatusCode == 307 || resp.StatusCode == 308:
		loc := resp.Header.Get("Location")
		if loc == "" {
			loc = "(unknown)"
		}
		return true, fmt.Sprintf("站点可达，但返回重定向至 %s", loc), nil
	case resp.StatusCode == 401:
		return true, "站点可达，需要认证 (HTTP 401)", nil
	case resp.StatusCode == 403:
		return true, "站点可达，但访问被拒绝 — 可能被 Cloudflare/WAF 拦截 (HTTP 403)", nil
	case resp.StatusCode == 429:
		return true, "站点可达，但被限流 (HTTP 429)", nil
	case resp.StatusCode == 503:
		return true, "站点可达，服务暂时不可用 (HTTP 503)", nil
	default:
		ok := resp.StatusCode >= 400 && resp.StatusCode < 500
		return ok, resp.Status, nil
	}
}

// ApplySiteAuthHeaders applies authentication headers based on site config.
func ApplySiteAuthHeaders(req *http.Request, site *model.Site) {
	switch site.AuthType {
	case "cookie":
		if site.Cookie != "" {
			req.Header.Set("Cookie", site.Cookie)
		}
	case "api_key":
		if site.APIKey != "" {
			req.Header.Set("x-api-key", site.APIKey)
		}
	case "auth_header":
		if site.AuthHeader != "" {
			req.Header.Set("Authorization", site.AuthHeader)
		}
	}

	// Apply custom User-Agent if configured
	if site.UserAgent != "" {
		req.Header.Set("User-Agent", site.UserAgent)
	}
}

// GetPageSource fetches a page with browser-like headers.
// Returns (pageSource, cookies, error).
func GetPageSource(url string, site *model.Site, timeout int, log *zap.Logger) (string, string, error) {
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", "", err
	}

	// Apply browser-like headers
	headers := HTTPHeaderPresets()
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Apply auth
	ApplySiteAuthHeaders(req, site)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	// Extract cookies from response
	cookies := ""
	for _, c := range resp.Cookies() {
		if cookies != "" {
			cookies += "; "
		}
		cookies += c.Name + "=" + c.Value
	}

	return string(body), cookies, nil
}

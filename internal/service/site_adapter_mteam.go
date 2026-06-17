// Package service — M-Team site adapter.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

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
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("M-Team 需要填写 API Access Token（控制台 → 实验室 → 存取令牌），不能使用 Cookie 访问开放 API")
	}
	// 与旧版参考实现对齐：
	// 用 camelCase 参数（pageNumber / pageSize），同时接受 code 为字符串 "0"
	// 或数值 0；兼容 M-Team v3 API 不同版本的返回。
	u := cfg.URL + "/api/torrent/search"
	payload := `{"pageNumber":1,"pageSize":1,"mode":"all"}`
	data, status, err := doRequestJSON(ctx, a.client, "POST", u, cfg, []byte(payload))
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	preview := string(data)
	if len(preview) > 400 {
		preview = preview[:400] + "..."
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("authentication failed: status %d, body=%s", status, preview)
	}
	if status >= 300 && status < 400 {
		return fmt.Errorf("authentication failed: HTTP %d (API Key 无效或未登录), body=%s", status, preview)
	}
	if status != http.StatusOK {
		return fmt.Errorf("authenticate failed: status %d, body=%s", status, preview)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse response: %w (body=%s)", err, preview)
	}
	if mteamCodeOK(resp["code"]) {
		return nil
	}
	msg, _ := resp["message"].(string)
	if msg == "" {
		msg = fmt.Sprintf("code=%s", mteamCodeString(resp["code"]))
	}
	return fmt.Errorf("authentication failed: %s (body=%s)", msg, preview)
}

func (a *MTeamAdapter) Search(ctx context.Context, cfg SiteConfig, keyword string, page int) (*SiteSearchResult, error) {
	// 与参考项目对齐：使用 camelCase 字段名，page 从 1 开始。
	if page <= 0 {
		page = 1
	}
	payload := map[string]interface{}{
		"keyword":    keyword,
		"pageNumber": page,
		"pageSize":   50,
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
	if page <= 0 {
		page = 1
	}
	payload := map[string]interface{}{
		"keyword":    "",
		"pageNumber": page,
		"pageSize":   50,
	}
	if category != "" {
		payload["categories"] = []string{category}
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
	u := cfg.URL + "/api/torrent/detail?id=" + url.QueryEscape(id)
	data, status, err := doRequestJSON(ctx, a.client, "POST", u, cfg, nil)
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

// GetDownloadURL 解析 M-Team 种子的真实下载链接。
//
// M-Team v3 流程：
//
//	POST /api/torrent/genDlToken?id={tid}     (带 x-api-key)
//	→ {"code":"0","data":"https://api.m-team.cc/api/rss/dlv2?sign=..."}
//
// 拿到的 sign URL 可被任何下载客户端无认证地直接 GET。这是旧版参考实现
// _download_torrent_file 方法的子集。
func (a *MTeamAdapter) GetDownloadURL(ctx context.Context, cfg SiteConfig, id string) (string, error) {
	u := cfg.URL + "/api/torrent/genDlToken?id=" + id
	// genDlToken 是 POST 但参数走 query string；body 留空。
	data, status, err := doRequestJSON(ctx, a.client, "POST", u, cfg, []byte("{}"))
	if err != nil {
		return "", fmt.Errorf("genDlToken: %w", err)
	}
	if status >= 300 {
		return "", fmt.Errorf("genDlToken: HTTP %d", status)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("genDlToken parse: %w", err)
	}
	codeStr := ""
	switch v := resp["code"].(type) {
	case string:
		codeStr = v
	case float64:
		codeStr = strconv.Itoa(int(v))
	}
	if codeStr != "0" && codeStr != "200" {
		msg, _ := resp["message"].(string)
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("genDlToken: %s", msg)
	}
	dl, _ := resp["data"].(string)
	if dl == "" {
		return "", fmt.Errorf("genDlToken: empty data field")
	}
	return dl, nil
}

// parseMTeamJSON 解析 MTeam v3 JSON 响应。
//
// 响应结构（与旧版参考实现一致）：
//
//	{
//	  "code": "0",          // 字符串 "0" 表示成功
//	  "message": "SUCCESS",
//	  "data": {
//	    "total": "123",
//	    "data": [ ... ]    // 旧字段名 "lists" 已被替换为 "data"
//	  }
//	}
func parseMTeamJSON(data []byte, siteName, baseURL string) (*SiteSearchResult, error) {
	// 用 map 反序列化以兼容 code/total 既可能是字符串又可能是数字。
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	// code 兼容字符串与数字。
	codeStr := ""
	switch v := raw["code"].(type) {
	case string:
		codeStr = v
	case float64:
		codeStr = strconv.Itoa(int(v))
	}
	if codeStr != "" && codeStr != "0" && codeStr != "200" {
		msg, _ := raw["message"].(string)
		if msg == "" {
			msg = fmt.Sprintf("code=%s", codeStr)
		}
		return nil, fmt.Errorf("mteam: %s", msg)
	}

	dataField, _ := raw["data"].(map[string]interface{})
	if dataField == nil {
		return &SiteSearchResult{SiteName: siteName, Items: []TorrentItem{}}, nil
	}

	// total 兼容字符串与数字。
	total := 0
	switch v := dataField["total"].(type) {
	case string:
		total, _ = strconv.Atoi(v)
	case float64:
		total = int(v)
	}

	// data.data（v3）优先；兜底兼容旧的 data.lists。
	var rows []interface{}
	switch v := dataField["data"].(type) {
	case []interface{}:
		rows = v
	}
	if rows == nil {
		if v, ok := dataField["lists"].([]interface{}); ok {
			rows = v
		}
	}

	result := &SiteSearchResult{
		SiteName: siteName,
		Items:    []TorrentItem{},
		Total:    total,
	}

	for _, rawT := range rows {
		t, ok := rawT.(map[string]interface{})
		if !ok {
			continue
		}
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
		} else if v, ok := t["size"].(string); ok {
			// v3 API 把 size 序列化成字符串。
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				item.Size = n
			}
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
		// 标记 download_url 指向 genDlToken；真正的下载链接由 handler 层
		// 在用户点"下载"时通过 MTeamAdapter.GetDownloadURL 解析。
		// 这样前端 SiteSearchPage 才知道这一行有可用的下载入口。
		item.DownloadURL = baseURL + "/api/torrent/genDlToken?id=" + item.ID
		result.Items = append(result.Items, item)
	}

	return result, nil
}

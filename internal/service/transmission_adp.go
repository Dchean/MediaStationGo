// Package service — Transmission 下载适配器。
//
// TransmissionAdapter 实现了 DownloadAdapter 接口，通过 Transmission RPC API
// 管理下载任务。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// transmissionRPCRequest 是 Transmission RPC 请求的通用结构。
type transmissionRPCRequest struct {
	Method    string                 `json:"method"`
	Arguments map[string]interface{} `json:"arguments"`
	Tag       int                    `json:"tag,omitempty"`
}

// transmissionRPCResponse 是 Transmission RPC 响应的通用结构。
type transmissionRPCResponse struct {
	Result    string                 `json:"result"`
	Arguments map[string]interface{} `json:"arguments"`
	Tag       int                    `json:"tag"`
}

// TransmissionAdapter 是 Transmission 的 DownloadAdapter 实现。
type TransmissionAdapter struct {
	mu        sync.Mutex
	cfg       DownloadClientConfig
	client    *http.Client
	tag       int
	sessionID string
}

// NewTransmissionAdapter 创建新的 Transmission 适配器。
func NewTransmissionAdapter() *TransmissionAdapter {
	return &TransmissionAdapter{
		client: NewInternalHTTPClient(20 * time.Second),
	}
}

// Initialize 配置并初始化 Transmission RPC 连接。
func (a *TransmissionAdapter) Initialize(ctx context.Context, cfg DownloadClientConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = cfg
	a.sessionID = ""
	a.tag = 0
	return a.pingLocked(ctx)
}

// Ping 测试连接。
func (a *TransmissionAdapter) Ping(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pingLocked(ctx)
}

// pingLocked 内部 ping 实现（调用者必须持有锁）。
func (a *TransmissionAdapter) pingLocked(ctx context.Context) error {
	rpcURL := a.cfg.Host
	if !strings.HasSuffix(rpcURL, "/rpc") && !strings.HasSuffix(rpcURL, "/transmission/rpc") {
		if !strings.Contains(rpcURL, "/rpc") {
			rpcURL = strings.TrimRight(rpcURL, "/") + "/transmission/rpc"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rpcURL, nil)
	if err != nil {
		return err
	}
	if a.cfg.Username != "" {
		req.SetBasicAuth(a.cfg.Username, a.cfg.Password)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == 409 {
		// 正常：需要 CSRF token
		a.sessionID = resp.Header.Get("X-Transmission-Session-Id")
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("transmission rpc: %d", resp.StatusCode)
	}
	return nil
}

// rpcLocked 发送 RPC 请求（调用者必须持有锁）。
func (a *TransmissionAdapter) rpcLocked(ctx context.Context, method string, args map[string]interface{}) (*transmissionRPCResponse, error) {
	rpcURL := a.cfg.Host
	if !strings.Contains(rpcURL, "/rpc") {
		rpcURL = strings.TrimRight(rpcURL, "/") + "/transmission/rpc"
	}

	a.tag++
	body, err := json.Marshal(transmissionRPCRequest{
		Method:    method,
		Arguments: args,
		Tag:       a.tag,
	})
	if err != nil {
		return nil, err
	}

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if a.sessionID != "" {
			req.Header.Set("X-Transmission-Session-Id", a.sessionID)
		}
		if a.cfg.Username != "" {
			req.SetBasicAuth(a.cfg.Username, a.cfg.Password)
		}

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 409 {
			a.sessionID = resp.Header.Get("X-Transmission-Session-Id")
			continue
		}
		if resp.StatusCode >= 400 {
			raw, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("transmission rpc error: %d: %s", resp.StatusCode, string(raw))
		}

		var result transmissionRPCResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		if result.Result != "success" {
			return nil, fmt.Errorf("transmission rpc result: %s", result.Result)
		}
		return &result, nil
	}
	return nil, fmt.Errorf("transmission: failed after CSRF retry")
}

// AddTorrent 通过 URL 添加种子。
func (a *TransmissionAdapter) AddTorrent(ctx context.Context, torrentURL, savePath string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	args := map[string]interface{}{"filename": torrentURL}
	if savePath != "" {
		args["download-dir"] = savePath
	}
	resp, err := a.rpcLocked(ctx, "torrent-add", args)
	if err != nil {
		return "", err
	}
	if added, ok := resp.Arguments["torrent-added"].(map[string]interface{}); ok {
		if hashStr, ok := added["hashString"].(string); ok {
			return hashStr, nil
		}
	}
	if dup, ok := resp.Arguments["torrent-duplicate"].(map[string]interface{}); ok {
		if hashStr, ok := dup["hashString"].(string); ok {
			return hashStr, nil
		}
	}
	return "", nil
}

// AddMagnet 通过磁力链接添加种子。
func (a *TransmissionAdapter) AddMagnet(ctx context.Context, magnet, savePath string) (string, error) {
	return a.AddTorrent(ctx, magnet, savePath)
}

// Pause 暂停种子。
func (a *TransmissionAdapter) Pause(ctx context.Context, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.rpcLocked(ctx, "torrent-stop", map[string]interface{}{
		"ids": []string{hash},
	})
	return err
}

// Resume 恢复种子。
func (a *TransmissionAdapter) Resume(ctx context.Context, hash string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.rpcLocked(ctx, "torrent-start", map[string]interface{}{
		"ids": []string{hash},
	})
	return err
}

// Remove 删除种子。
func (a *TransmissionAdapter) Remove(ctx context.Context, hash string, deleteFiles bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.rpcLocked(ctx, "torrent-remove", map[string]interface{}{
		"ids":               []string{hash},
		"delete-local-data": deleteFiles,
	})
	return err
}

// List 列出种子。
func (a *TransmissionAdapter) List(ctx context.Context, filter string) ([]TorrentInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	args := map[string]interface{}{
		"fields": []string{
			"hashString", "name", "totalSize", "percentDone",
			"rateDownload", "rateUpload", "status", "downloadDir",
			"peersSendingToUs", "peersGettingFromUs", "addedDate",
			"labels", "isStalled",
		},
	}
	resp, err := a.rpcLocked(ctx, "torrent-get", args)
	if err != nil {
		return nil, err
	}

	torrentsRaw, ok := resp.Arguments["torrents"].([]interface{})
	if !ok {
		return nil, nil
	}

	result := make([]TorrentInfo, 0, len(torrentsRaw))
	for _, tr := range torrentsRaw {
		t, ok := tr.(map[string]interface{})
		if !ok {
			continue
		}

		hash, _ := t["hashString"].(string)
		name, _ := t["name"].(string)
		size := toInt64(t["totalSize"])
		progress := toFloat64(t["percentDone"])
		dlSpeed := toInt64(t["rateDownload"])
		upSpeed := toInt64(t["rateUpload"])
		savePath, _ := t["downloadDir"].(string)
		numSeeds := int(toInt64(t["peersSendingToUs"]))
		numLeechs := int(toInt64(t["peersGettingFromUs"]))
		addedOn := int64(toFloat64(t["addedDate"]))

		// Transmission 状态码转字符串
		status := int(toFloat64(t["status"]))
		state := transmissionStateStr(status)

		// 过滤
		if filter != "" && !strings.EqualFold(state, filter) {
			continue
		}

		result = append(result, TorrentInfo{
			Hash:      hash,
			Name:      name,
			Size:      size,
			Progress:  progress * 100,
			DLSpeed:   dlSpeed,
			UPSpeed:   upSpeed,
			State:     state,
			SavePath:  savePath,
			NumSeeds:  numSeeds,
			NumLeechs: numLeechs,
			AddedOn:   time.Unix(addedOn, 0),
			Tags:      toJSONLabels(t["labels"]),
		})
	}
	return result, nil
}

// GetInfo 获取单个种子信息。
func (a *TransmissionAdapter) GetInfo(ctx context.Context, hash string) (*TorrentInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	args := map[string]interface{}{
		"ids": []string{hash},
		"fields": []string{
			"hashString", "name", "totalSize", "percentDone",
			"rateDownload", "rateUpload", "status", "downloadDir",
			"peersSendingToUs", "peersGettingFromUs", "addedDate", "labels",
		},
	}
	resp, err := a.rpcLocked(ctx, "torrent-get", args)
	if err != nil {
		return nil, err
	}
	torrentsRaw, ok := resp.Arguments["torrents"].([]interface{})
	if !ok || len(torrentsRaw) == 0 {
		return nil, fmt.Errorf("torrent %s not found", hash)
	}
	t, ok := torrentsRaw[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("torrent %s: invalid response", hash)
	}

	status := int(toFloat64(t["status"]))
	info := &TorrentInfo{
		Hash:      hash,
		Name:      strVal(t["name"]),
		Size:      toInt64(t["totalSize"]),
		Progress:  toFloat64(t["percentDone"]) * 100,
		DLSpeed:   toInt64(t["rateDownload"]),
		UPSpeed:   toInt64(t["rateUpload"]),
		State:     transmissionStateStr(status),
		SavePath:  strVal(t["downloadDir"]),
		NumSeeds:  int(toInt64(t["peersSendingToUs"])),
		NumLeechs: int(toInt64(t["peersGettingFromUs"])),
		AddedOn:   time.Unix(int64(toFloat64(t["addedDate"])), 0),
		Tags:      toJSONLabels(t["labels"]),
	}
	return info, nil
}

// transmissionStateStr 将 Transmission 状态码转为可读字符串。
func transmissionStateStr(status int) string {
	switch status {
	case 0:
		return "stopped"
	case 1:
		return "check_pending"
	case 2:
		return "checking"
	case 3:
		return "download_pending"
	case 4:
		return "downloading"
	case 5:
		return "seed_pending"
	case 6:
		return "seeding"
	default:
		return "unknown"
	}
}

// toInt64 安全地将 interface{} 转为 int64。
func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int:
		return int64(val)
	case int64:
		return val
	case json.Number:
		n, _ := val.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	default:
		return 0
	}
}

// toFloat64 安全地将 interface{} 转为 float64。
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case json.Number:
		n, _ := val.Float64()
		return n
	case string:
		n, _ := strconv.ParseFloat(val, 64)
		return n
	default:
		return 0
	}
}

// strVal 安全地提取字符串。
func strVal(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// toJSONLabels 将 Transmission labels 转为逗号分隔字符串。
func toJSONLabels(v interface{}) string {
	if v == nil {
		return ""
	}
	arr, ok := v.([]interface{})
	if !ok {
		return ""
	}
	labels := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			labels = append(labels, s)
		}
	}
	return strings.Join(labels, ",")
}

// Package service — Telegram Bot 交互命令服务。
//
// 处理通过 Telegram Bot API 接收的用户命令，提供系统状态查询、
// 媒体搜索、下载管理等功能。同时支持 Webhook 和 Long Polling 两种模式。
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

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// TelegramUpdate 是 Telegram Bot API 推送的 update 对象。
type TelegramUpdate struct {
	UpdateID int              `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
}

// TelegramMessage 是 Telegram 消息对象。
type TelegramMessage struct {
	MessageID int          `json:"message_id"`
	From      TelegramUser `json:"from"`
	Chat      TelegramChat `json:"chat"`
	Text      string       `json:"text,omitempty"`
	Date      int          `json:"date"`
}

// TelegramUser 是 Telegram 用户对象。
type TelegramUser struct {
	ID        int    `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

// TelegramChat 是 Telegram 聊天对象。
type TelegramChat struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}

// TelegramBotService 处理 Telegram Bot 的交互命令。
type TelegramBotService struct {
	log    *zap.Logger
	repo   *repository.Container
	crypto *CryptoService

	pollingMu     sync.Mutex
	pollingCancel map[string]context.CancelFunc // bot_token -> cancel
}

// NewTelegramBotService 创建 Telegram Bot 服务。
func NewTelegramBotService(log *zap.Logger, repo *repository.Container, crypto *CryptoService) *TelegramBotService {
	return &TelegramBotService{
		log:           log,
		repo:          repo,
		crypto:        crypto,
		pollingCancel: make(map[string]context.CancelFunc),
	}
}

// HandleWebhook 处理 Telegram 推送的 Webhook/Polling 消息。
func (s *TelegramBotService) HandleWebhook(ctx context.Context, body []byte) error {
	var update TelegramUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		return fmt.Errorf("invalid update: %w", err)
	}

	if update.Message == nil || update.Message.Text == "" {
		return nil
	}

	msg := update.Message
	text := strings.TrimSpace(msg.Text)

	s.log.Info("telegram command received",
		zap.Int("chat_id", msg.Chat.ID),
		zap.String("user", msg.From.Username),
		zap.String("text", text),
	)

	// 获取该 chat_id 对应的通知渠道配置
	channel := s.findChannelByChatID(ctx, msg.Chat.ID)

	// 解析并执行命令
	reply, err := s.executeCommand(ctx, channel, msg, text)
	if err != nil {
		s.log.Error("command failed", zap.Error(err))
		_ = s.reply(ctx, channel, msg.Chat.ID, "命令执行失败: "+err.Error())
		return nil
	}

	if reply != "" {
		if err := s.reply(ctx, channel, msg.Chat.ID, reply); err != nil {
			s.log.Error("reply failed", zap.Error(err))
		}
	}

	return nil
}

// executeCommand 解析命令并执行。
func (s *TelegramBotService) executeCommand(ctx context.Context, channel *model.NotifyChannel, msg *TelegramMessage, text string) (string, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return "", nil
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/start":
		return s.cmdStart(msg), nil
	case "/help":
		return s.cmdHelp(), nil
	case "/status":
		return s.cmdStatus(ctx)
	case "/search":
		return s.cmdSearch(ctx, args)
	case "/downloads":
		return s.cmdDownloads(ctx)
	case "/stats":
		return s.cmdStats(ctx)
	default:
		return fmt.Sprintf("未知命令: %s\n\n输入 /help 查看可用命令列表。", cmd), nil
	}
}

// cmdStart 处理 /start 命令。
func (s *TelegramBotService) cmdStart(msg *TelegramMessage) string {
	name := msg.From.FirstName
	if msg.From.Username != "" {
		name = "@" + msg.From.Username
	}
	return fmt.Sprintf(
		"<b>欢迎使用 MediaStationGo</b>\n\n"+
			"你好 %s！你已成功连接到媒体中心。\n\n"+
			"<b>可用命令：</b>\n"+
			"📊 /status — 系统运行状态\n"+
			"🔍 /search 关键词 — 搜索媒体\n"+
			"📥 /downloads — 下载进度\n"+
			"📈 /stats — 媒体库统计\n"+
			"❓ /help — 帮助信息",
		name,
	)
}

// cmdHelp 处理 /help 命令。
func (s *TelegramBotService) cmdHelp() string {
	return "<b>MediaStationGo 命令列表</b>\n\n" +
		"<b>/start</b> — 开始使用\n" +
		"<b>/help</b> — 帮助信息\n" +
		"<b>/status</b> — 系统运行状态\n" +
		"<b>/search 关键词</b> — 搜索媒体库\n" +
		"<b>/downloads</b> — 下载列表\n" +
		"<b>/stats</b> — 媒体库统计\n\n" +
		"<b>自动推送事件：</b>\n" +
		"• 订阅命中新资源\n" +
		"• 下载任务完成\n" +
		"• 刮削失败告警\n" +
		"• 系统异常通知"
}

// cmdStatus 处理 /status 命令。
func (s *TelegramBotService) cmdStatus(ctx context.Context) (string, error) {
	var mediaCount int64
	s.repo.DB.Model(&model.Media{}).Count(&mediaCount)

	var totalSize int64
	s.repo.DB.Raw("SELECT COALESCE(SUM(size_bytes), 0) FROM media").Scan(&totalSize)
	totalSizeGB := float64(totalSize) / 1024 / 1024 / 1024

	return fmt.Sprintf(
		"<b>系统运行状态</b>\n\n"+
			"🎬 媒体总数: <b>%d</b>\n"+
			"💾 存储占用: <b>%.1f GB</b>",
		mediaCount, totalSizeGB,
	), nil
}

// cmdSearch 处理 /search 命令。
func (s *TelegramBotService) cmdSearch(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "请提供搜索关键词\n例: <code>/search 哥斯拉</code>", nil
	}

	keyword := strings.Join(args, " ")
	var results []model.Media
	err := s.repo.DB.Where("title LIKE ?", "%"+keyword+"%").
		Order("year DESC").Limit(8).
		Find(&results).Error
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("未找到与 <b>%s</b> 相关的媒体", keyword), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>搜索: %s</b>\n\n", keyword))
	for i, m := range results {
		year := ""
		if m.Year > 0 {
			year = fmt.Sprintf(" (%d)", m.Year)
		}
		ep := ""
		if m.SeasonNum > 0 && m.EpisodeNum > 0 {
			ep = fmt.Sprintf(" S%02dE%02d", m.SeasonNum, m.EpisodeNum)
		}
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>%s%s — %s\n", i+1, m.Title, year, ep, formatSize(m.SizeBytes)))
	}

	return sb.String(), nil
}

// cmdDownloads 处理 /downloads 命令。
func (s *TelegramBotService) cmdDownloads(ctx context.Context) (string, error) {
	type Row struct {
		Title  string
		Status string
	}
	var rows []Row
	if err := s.repo.DB.Raw(
		"SELECT COALESCE(NULLIF(title,''),'下载任务') as title, COALESCE(status,'unknown') as status FROM download_tasks ORDER BY created_at DESC LIMIT 8",
	).Scan(&rows).Error; err != nil {
		return "", err
	}

	if len(rows) == 0 {
		return "当前没有下载任务。", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>下载任务 (%d)</b>\n\n", len(rows)))
	for _, r := range rows {
		icon := "⏳"
		switch r.Status {
		case "completed":
			icon = "✅"
		case "downloading":
			icon = "📥"
		case "error":
			icon = "❌"
		}
		name := strings.TrimSpace(r.Title)
		if name == "" {
			name = "下载任务"
		}
		if len(name) > 60 {
			name = name[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s %s\n", icon, name))
	}

	return sb.String(), nil
}

// cmdStats 处理 /stats 命令。
func (s *TelegramBotService) cmdStats(ctx context.Context) (string, error) {
	var totalMedia int64
	s.repo.DB.Model(&model.Media{}).Count(&totalMedia)

	var totalSize int64
	s.repo.DB.Raw("SELECT COALESCE(SUM(size_bytes), 0) FROM media").Scan(&totalSize)

	type LibStat struct {
		Name  string
		Type  string
		Count int64
	}
	var libs []LibStat
	s.repo.DB.Raw(
		"SELECT l.name, l.type, COUNT(m.id) as count FROM libraries l LEFT JOIN media m ON m.library_id = l.id GROUP BY l.id ORDER BY count DESC",
	).Scan(&libs)

	var sb strings.Builder
	sb.WriteString("<b>媒体库统计</b>\n\n")
	sb.WriteString(fmt.Sprintf("📚 总数: <b>%d</b>\n", totalMedia))
	sb.WriteString(fmt.Sprintf("💾 大小: <b>%s</b>\n", formatSize(totalSize)))

	if len(libs) > 0 {
		sb.WriteString("\n<b>各库分布:</b>\n")
		for _, l := range libs {
			icon := "🎬"
			switch l.Type {
			case "tv":
				icon = "📺"
			case "anime":
				icon = "🍥"
			case "music":
				icon = "🎵"
			}
			sb.WriteString(fmt.Sprintf("%s <b>%s</b>: %d\n", icon, l.Name, l.Count))
		}
	}

	return sb.String(), nil
}

// ── Polling ──

// StartPolling 为所有已启用的 Telegram 通知渠道启动长轮询。
func (s *TelegramBotService) StartPolling(ctx context.Context) {
	channels, err := s.repo.NotifyChannel.ListByType(ctx, "telegram")
	if err != nil {
		s.log.Error("failed to list telegram channels for polling", zap.Error(err))
		return
	}

	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		configStr := ch.Config
		if s.crypto != nil && configStr != "" {
			configStr = s.crypto.Decrypt(configStr)
		}
		var cfg map[string]string
		if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
			continue
		}
		botToken := cfg["bot_token"]
		if botToken == "" {
			continue
		}

		s.pollingMu.Lock()
		if _, running := s.pollingCancel[botToken]; running {
			s.pollingMu.Unlock()
			continue
		}
		pollCtx, cancel := context.WithCancel(context.Background())
		s.pollingCancel[botToken] = cancel
		s.pollingMu.Unlock()

		go s.pollLoop(pollCtx, botToken)
		s.log.Info("started telegram polling", zap.String("channel", ch.Name))
	}
}

// StopPolling 停止所有 Telegram 长轮询。
func (s *TelegramBotService) StopPolling() {
	s.pollingMu.Lock()
	defer s.pollingMu.Unlock()
	for token, cancel := range s.pollingCancel {
		cancel()
		delete(s.pollingCancel, token)
	}
	s.log.Info("telegram polling stopped")
}

// pollLoop 对单个 Bot Token 执行长轮询。
func (s *TelegramBotService) pollLoop(ctx context.Context, botToken string) {
	var offset int64 = 0
	pollURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates", botToken)
	client := &http.Client{Timeout: 45 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		reqBody, _ := json.Marshal(map[string]interface{}{
			"offset":  offset,
			"timeout": 30,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, pollURL, bytes.NewReader(reqBody))
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			OK     bool             `json:"ok"`
			Result []TelegramUpdate `json:"result"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil || !result.OK {
			time.Sleep(3 * time.Second)
			continue
		}

		for _, upd := range result.Result {
			if upd.UpdateID >= int(offset) {
				offset = int64(upd.UpdateID) + 1
			}
			if upd.Message == nil || upd.Message.Text == "" {
				continue
			}
			go func(u TelegramUpdate) {
				raw, _ := json.Marshal(u)
				_ = s.HandleWebhook(context.Background(), raw)
			}(upd)
		}
	}
}

// ── Message Sending ──

// reply 通过 Telegram Bot API 发送回复消息。
func (s *TelegramBotService) reply(ctx context.Context, channel *model.NotifyChannel, chatID int, text string) error {
	botToken := ""
	if channel != nil {
		configStr := channel.Config
		if s.crypto != nil && configStr != "" {
			configStr = s.crypto.Decrypt(configStr)
		}
		var cfg map[string]string
		if err := json.Unmarshal([]byte(configStr), &cfg); err == nil {
			botToken = cfg["bot_token"]
		}
	}
	if botToken == "" {
		return fmt.Errorf("bot_token not configured")
	}

	payload := map[string]interface{}{
		"chat_id":    strconv.Itoa(chatID),
		"text":       text,
		"parse_mode": "HTML",
	}
	body, _ := json.Marshal(payload)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram api error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// findChannelByChatID 根据 chat_id 查找已配置的通知渠道。
func (s *TelegramBotService) findChannelByChatID(ctx context.Context, chatID int) *model.NotifyChannel {
	channels, err := s.repo.NotifyChannel.ListByType(ctx, "telegram")
	if err != nil {
		return nil
	}
	target := strconv.Itoa(chatID)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		configStr := ch.Config
		if s.crypto != nil && configStr != "" {
			configStr = s.crypto.Decrypt(configStr)
		}
		var cfg map[string]string
		json.Unmarshal([]byte(configStr), &cfg)
		if cfg["chat_id"] == target {
			return &ch
		}
	}
	return nil
}

// ── Webhook Management ──

// SetWebhook 注册 Telegram Bot Webhook URL。
func (s *TelegramBotService) SetWebhook(ctx context.Context, botToken, webhookURL string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"url":             webhookURL,
		"allowed_updates": []string{"message"},
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", botToken),
		bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("setWebhook failed: %s", string(body))
	}
	return nil
}

// GetWebhookInfo 获取 Webhook 配置信息。
func (s *TelegramBotService) GetWebhookInfo(ctx context.Context, botToken string) (map[string]interface{}, error) {
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(
		fmt.Sprintf("https://api.telegram.org/bot%s/getWebhookInfo", botToken),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &result)
	return result, nil
}

// formatSize 格式化字节数为可读字符串。
func formatSize(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(bytes)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

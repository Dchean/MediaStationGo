// Package service — multi-channel push notifications.
//
// NotifierService dispatches structured messages to one or more channels
// configured in the system settings table:
//
//	notify.telegram.bot_token + notify.telegram.chat_id
//	notify.bark.server + notify.bark.key
//	notify.wechat.sendkey
//	notify.webhook.url + notify.webhook.method
//
// Notifications are triggered by the subscription poller, the download
// poller, the scan / scrape completions, and any future event worth
// surfacing to the operator's phone.
package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// NotifierService dispatches push notifications.
type NotifierService struct {
	log    *zap.Logger
	repo   *repository.Container
	client *http.Client
}

// NewNotifierService is the constructor.
func NewNotifierService(log *zap.Logger, repo *repository.Container) *NotifierService {
	return &NotifierService{
		log:    log,
		repo:   repo,
		client: NewExternalHTTPClient(10 * time.Second),
	}
}

// Send dispatches a notification to every configured channel. Failures
// are logged but do not propagate — notifications are best-effort.
func (n *NotifierService) Send(ctx context.Context, title, body, eventType string) {
	n.sendTelegram(ctx, title, body)
	n.sendBark(ctx, title, body)
	n.sendWechat(ctx, title, body)
	n.sendWebhook(ctx, title, body, eventType)
}

func (n *NotifierService) get(ctx context.Context, key string) string {
	v, _ := n.repo.Setting.Get(ctx, key)
	return strings.TrimSpace(v)
}

func (n *NotifierService) sendTelegram(ctx context.Context, title, body string) {
	token := n.get(ctx, "notify.telegram.bot_token")
	chatID := n.get(ctx, "notify.telegram.chat_id")
	if token == "" || chatID == "" {
		return
	}
	text := fmt.Sprintf("<b>%s</b>\n\n%s", escapeHTML(title), escapeHTML(body))
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	form.Set("parse_mode", "HTML")
	cfg := map[string]string{"bot_token": token}
	if apiBase := n.get(ctx, "notify.telegram.api_base_url"); apiBase != "" {
		cfg["api_base_url"] = apiBase
	}
	if proxyURL := n.get(ctx, "notify.telegram.proxy_url"); proxyURL != "" {
		cfg["proxy_url"] = proxyURL
	}
	if err := telegramPostForm(ctx, cfg, "sendMessage", form, 15*time.Second); err != nil {
		n.log.Debug("telegram notify failed", zap.Error(err))
	}
}

func (n *NotifierService) sendBark(ctx context.Context, title, body string) {
	server := n.get(ctx, "notify.bark.server")
	key := n.get(ctx, "notify.bark.key")
	if key == "" {
		return
	}
	if server == "" {
		server = "https://api.day.app"
	}
	u := fmt.Sprintf("%s/%s/%s/%s",
		strings.TrimRight(server, "/"),
		url.PathEscape(key),
		url.PathEscape(title),
		url.PathEscape(body),
	)
	resp, err := n.client.Get(u)
	if err != nil {
		n.log.Debug("bark notify failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
}

func (n *NotifierService) sendWechat(ctx context.Context, title, body string) {
	sendkey := n.get(ctx, "notify.wechat.sendkey")
	if sendkey == "" {
		return
	}
	u := fmt.Sprintf("https://sctapi.ftqq.com/%s.send", sendkey)
	form := url.Values{}
	form.Set("title", title)
	form.Set("desp", body)
	resp, err := n.client.PostForm(u, form)
	if err != nil {
		n.log.Debug("wechat notify failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
}

func (n *NotifierService) sendWebhook(ctx context.Context, title, body, eventType string) {
	webhookURL := n.get(ctx, "notify.webhook.url")
	if webhookURL == "" {
		return
	}
	payload := fmt.Sprintf(`{"title":%q,"content":%q,"type":%q}`, title, body, eventType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL,
		strings.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		n.log.Debug("webhook notify failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

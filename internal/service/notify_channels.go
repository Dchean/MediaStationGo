// Package service — notification channel CRUD + multi-channel dispatch.
//
// The original NotifierService reads a single set of keys from the
// settings table. NotifyChannelService persists *named* channels in
// their own table so the operator can add multiple Telegram bots, Bark
// servers, etc. and pick which events flow to which channel.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// NotifyChannelService manages notify channels and dispatches messages.
type NotifyChannelService struct {
	log    *zap.Logger
	repo   *repository.Container
	client *http.Client
}

// NewNotifyChannelService is the constructor.
func NewNotifyChannelService(log *zap.Logger, repo *repository.Container) *NotifyChannelService {
	return &NotifyChannelService{
		log:    log,
		repo:   repo,
		client: NewExternalHTTPClient(10 * time.Second),
	}
}

// ChannelInput is the shape accepted by Create / Update. Config is a
// generic map; it gets serialised to JSON before being persisted.
type ChannelInput struct {
	Name    string         `json:"name" binding:"required"`
	Type    string         `json:"type" binding:"required"`
	Config  map[string]any `json:"config"`
	Events  []string       `json:"events"`
	Enabled *bool          `json:"enabled,omitempty"`
}

// channelView is the public shape — Config is decoded back to a map so
// the React form can edit it directly without unwrapping JSON twice.
type channelView struct {
	model.NotifyChannel
	Config map[string]any `json:"config"`
	Events []string       `json:"events"`
}

// toView decodes Config + Events from their persisted JSON strings.
func toView(n model.NotifyChannel) channelView {
	v := channelView{NotifyChannel: n}
	if n.Config != "" {
		_ = json.Unmarshal([]byte(n.Config), &v.Config)
	}
	if v.Config == nil {
		v.Config = map[string]any{}
	}
	if n.Events != "" {
		_ = json.Unmarshal([]byte(n.Events), &v.Events)
	}
	if v.Events == nil {
		v.Events = []string{}
	}
	return v
}

// List returns every channel as a decoded view.
func (s *NotifyChannelService) List(ctx context.Context) ([]channelView, error) {
	rows, err := s.repo.NotifyChannel.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]channelView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toView(r))
	}
	return out, nil
}

// Create persists a new channel.
func (s *NotifyChannelService) Create(ctx context.Context, in ChannelInput) (*channelView, error) {
	normalizeChannelInput(&in)
	if err := validateChannel(in); err != nil {
		return nil, err
	}
	cfgBlob, _ := json.Marshal(in.Config)
	evBlob, _ := json.Marshal(in.Events)
	n := &model.NotifyChannel{
		Name:    strings.TrimSpace(in.Name),
		Type:    in.Type,
		Config:  string(cfgBlob),
		Events:  string(evBlob),
		Enabled: true,
	}
	if in.Enabled != nil {
		n.Enabled = *in.Enabled
	}
	if err := s.repo.NotifyChannel.Create(ctx, n); err != nil {
		return nil, err
	}
	v := toView(*n)
	return &v, nil
}

// Update applies a partial patch to an existing channel.
func (s *NotifyChannelService) Update(ctx context.Context, id string, in ChannelInput) (*channelView, error) {
	normalizeChannelInput(&in)
	if err := validateChannel(in); err != nil {
		return nil, err
	}
	cfgBlob, _ := json.Marshal(in.Config)
	evBlob, _ := json.Marshal(in.Events)
	patch := map[string]any{
		"name":   strings.TrimSpace(in.Name),
		"type":   in.Type,
		"config": string(cfgBlob),
		"events": string(evBlob),
	}
	if in.Enabled != nil {
		patch["enabled"] = *in.Enabled
	}
	// Fetch existing row, apply patch via repo Update
	existing, err := s.repo.NotifyChannel.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("channel not found")
	}
	existing.Name = patch["name"].(string)
	existing.Type = patch["type"].(string)
	existing.Config = patch["config"].(string)
	existing.Events = patch["events"].(string)
	if en, ok := patch["enabled"]; ok {
		existing.Enabled = en.(bool)
	}
	if err := s.repo.NotifyChannel.Update(ctx, existing); err != nil {
		return nil, err
	}
	row, err := s.repo.NotifyChannel.FindByID(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	v := toView(*row)
	return &v, nil
}

// Delete removes the channel.
func (s *NotifyChannelService) Delete(ctx context.Context, id string) error {
	return s.repo.NotifyChannel.Delete(ctx, id)
}

// Test sends a "测试通知" through a single channel.
func (s *NotifyChannelService) Test(ctx context.Context, id string) error {
	row, err := s.repo.NotifyChannel.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if row == nil {
		return errors.New("channel not found")
	}
	return s.dispatchOne(ctx, *row, "MediaStationGo 测试通知", "如果你看到这条消息,说明该通道工作正常。")
}

// Broadcast sends a message to every enabled channel that subscribes to
// `event` (an empty Events slice means "all events"). Failures are
// logged and never abort the loop.
func (s *NotifyChannelService) Broadcast(ctx context.Context, title, body, event string) {
	rows, err := s.repo.NotifyChannel.ListEnabled(ctx)
	if err != nil {
		s.log.Warn("notify list failed", zap.Error(err))
		return
	}
	for _, r := range rows {
		if !channelSubscribes(r, event) {
			continue
		}
		if err := s.dispatchOne(ctx, r, title, body); err != nil {
			s.log.Warn("notify dispatch failed", zap.String("channel", r.Name), zap.Error(err))
		}
	}
}

// channelSubscribes returns true when the channel's Events list is
// empty (= all events) or contains `event`.
func channelSubscribes(n model.NotifyChannel, event string) bool {
	if event == "" || n.Events == "" || n.Events == "[]" {
		return true
	}
	var ev []string
	if err := json.Unmarshal([]byte(n.Events), &ev); err != nil {
		return true
	}
	if len(ev) == 0 {
		return true
	}
	for _, e := range ev {
		if e == event {
			return true
		}
	}
	return false
}

// dispatchOne is the inner dispatcher; the channel type drives which
// HTTP request gets built.
func (s *NotifyChannelService) dispatchOne(ctx context.Context, n model.NotifyChannel, title, body string) error {
	cfg := map[string]any{}
	_ = json.Unmarshal([]byte(n.Config), &cfg)

	switch n.Type {
	case "telegram":
		telegramCfg := telegramStringConfigFromAny(cfg)
		token := telegramCfg["bot_token"]
		chats := telegramTargetChatIDs(telegramCfg)
		if token == "" || len(chats) == 0 {
			return errors.New("telegram missing bot_token / group_chat_id / channel_chat_id")
		}
		text := fmt.Sprintf("<b>%s</b>\n\n%s", escapeHTML(title), escapeHTML(body))
		var firstErr error
		for _, chat := range chats {
			form := url.Values{}
			form.Set("chat_id", chat)
			form.Set("text", text)
			form.Set("parse_mode", "HTML")
			if err := telegramPostForm(ctx, telegramCfg, "sendMessage", form, 15*time.Second); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr

	case "bark":
		key := str(cfg["device_key"])
		if key == "" {
			return errors.New("bark missing device_key")
		}
		server := str(cfg["server"])
		if server == "" {
			server = "https://api.day.app"
		}
		u := fmt.Sprintf("%s/%s/%s/%s",
			strings.TrimRight(server, "/"),
			url.PathEscape(key),
			url.PathEscape(title),
			url.PathEscape(body),
		)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		return s.do(req)

	case "wechat":
		key := str(cfg["sendkey"])
		if key == "" {
			return errors.New("wechat missing sendkey")
		}
		u := fmt.Sprintf("https://sctapi.ftqq.com/%s.send", url.PathEscape(key))
		form := url.Values{}
		form.Set("title", title)
		form.Set("desp", body)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return s.do(req)

	case "webhook":
		urlS := str(cfg["url"])
		if urlS == "" {
			return errors.New("webhook missing url")
		}
		method := strings.ToUpper(str(cfg["method"]))
		if method == "" {
			method = "POST"
		}
		// Substitute {{title}} / {{message}} in the body template.
		bodyTpl := str(cfg["body_template"])
		if bodyTpl == "" {
			bodyTpl = `{"title":"{{title}}","message":"{{message}}"}`
		}
		bodyStr := strings.NewReplacer("{{title}}", title, "{{message}}", body).Replace(bodyTpl)
		req, _ := http.NewRequestWithContext(ctx, method, urlS, strings.NewReader(bodyStr))
		// Apply custom headers (encoded as JSON in the config).
		if hdrRaw := str(cfg["headers"]); hdrRaw != "" {
			var hdr map[string]string
			if err := json.Unmarshal([]byte(hdrRaw), &hdr); err == nil {
				for k, v := range hdr {
					req.Header.Set(k, v)
				}
			}
		}
		if req.Header.Get("Content-Type") == "" && method != http.MethodGet {
			req.Header.Set("Content-Type", "application/json")
		}
		return s.do(req)
	}
	return fmt.Errorf("unknown channel type %q", n.Type)
}

func (s *NotifyChannelService) do(req *http.Request) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return nil
}

// validateChannel rejects obviously-malformed inputs early so the API
// returns a useful 400 rather than a database constraint error.
func validateChannel(in ChannelInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name required")
	}
	switch in.Type {
	case "telegram", "wechat", "bark", "webhook", "email":
	default:
		return fmt.Errorf("unsupported channel type %q", in.Type)
	}
	if in.Type == "telegram" {
		cfg := in.Config
		if str(cfg["bot_token"]) == "" {
			return errors.New("telegram bot_token required")
		}
		if str(cfg["admin_user_ids"]) == "" {
			return errors.New("telegram admin_user_ids required")
		}
	}
	return nil
}

func normalizeChannelInput(in *ChannelInput) {
	if in == nil || in.Type != "telegram" {
		return
	}
	if in.Config == nil {
		in.Config = map[string]any{}
	}
	chatID := str(in.Config["chat_id"])
	if chatID == "" || !strings.HasPrefix(chatID, "-") {
		return
	}
	if str(in.Config["group_chat_id"]) == "" && str(in.Config["channel_chat_id"]) == "" && str(in.Config["command_chat_id"]) == "" {
		in.Config["group_chat_id"] = chatID
	}
}

func telegramTargetChatIDs(cfg map[string]string) []string {
	seen := map[string]bool{}
	targets := []string{}
	for _, key := range []string{"group_chat_id", "channel_chat_id"} {
		chatID := strings.TrimSpace(cfg[key])
		if chatID == "" || seen[chatID] {
			continue
		}
		seen[chatID] = true
		targets = append(targets, chatID)
	}
	if len(targets) == 0 {
		chatID := strings.TrimSpace(cfg["chat_id"])
		if strings.HasPrefix(chatID, "-") {
			targets = append(targets, chatID)
		}
	}
	if len(targets) == 0 {
		for _, userID := range telegramConfiguredUserIDs(cfg["admin_user_ids"]) {
			if seen[userID] {
				continue
			}
			seen[userID] = true
			targets = append(targets, userID)
		}
	}
	return targets
}

func telegramConfiguredUserIDs(raw string) []string {
	out := []string{}
	for _, value := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == ' ' || r == '\n' || r == '\t'
	}) {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

// str safely extracts a string from an interface{} loaded from JSON.
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

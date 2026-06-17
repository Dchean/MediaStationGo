package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"go.uber.org/zap"
)

func TestTelegramMethodURLUsesCustomAPIBase(t *testing.T) {
	got, err := telegramMethodURL(map[string]string{
		"api_base_url": "https://tg.example.com/",
	}, "123456:ABC-def", "sendMessage")
	if err != nil {
		t.Fatalf("telegramMethodURL returned error: %v", err)
	}
	want := "https://tg.example.com/bot123456:ABC-def/sendMessage"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeTelegramErrorRedactsBotToken(t *testing.T) {
	err := sanitizeTelegramError(errors.New(`Post "https://api.telegram.org/bot123456:SECRET/sendMessage": context deadline exceeded`))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRET") || strings.Contains(msg, "123456:") {
		t.Fatalf("telegram token leaked in error: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Fatalf("expected timeout hint, got: %s", msg)
	}
}

func TestValidateTelegramChannelDoesNotRequireLegacyChatID(t *testing.T) {
	err := validateChannel(ChannelInput{
		Name: "Telegram",
		Type: "telegram",
		Config: map[string]any{
			"bot_token":      "123456:ABC-def",
			"admin_user_ids": "10001",
		},
	})
	if err != nil {
		t.Fatalf("validateChannel returned error: %v", err)
	}
}

func TestTelegramTargetChatIDsFallsBackToAdmins(t *testing.T) {
	got := telegramTargetChatIDs(map[string]string{
		"admin_user_ids": "10001, 10002",
	})
	if len(got) != 2 || got[0] != "10001" || got[1] != "10002" {
		t.Fatalf("got %#v, want admin user ids", got)
	}
}

func TestNormalizeTelegramChannelMigratesLegacyChatID(t *testing.T) {
	input := ChannelInput{
		Name: "Telegram",
		Type: "telegram",
		Config: map[string]any{
			"chat_id": "-10001",
		},
	}
	normalizeChannelInput(&input)
	if got := str(input.Config["group_chat_id"]); got != "-10001" {
		t.Fatalf("group_chat_id = %q, want -10001", got)
	}
}

func TestNormalizeTelegramChannelMigratesLegacyPrivateChatIDToAdmin(t *testing.T) {
	cfg := map[string]string{"chat_id": "5812333517"}
	normalizeTelegramConfig(cfg)
	if got := cfg["admin_user_ids"]; got != "5812333517" {
		t.Fatalf("admin_user_ids = %q, want legacy chat_id", got)
	}
}

func TestTelegramTargetChatIDsUsesLegacyPrivateChatID(t *testing.T) {
	got := telegramTargetChatIDs(map[string]string{
		"chat_id": "5812333517",
	})
	if len(got) != 1 || got[0] != "5812333517" {
		t.Fatalf("got %#v, want legacy private chat target", got)
	}
}

func TestRegisterTelegramBotCommands(t *testing.T) {
	var gotPath string
	var payloads []struct {
		Commands []telegramBotCommand `json:"commands"`
		Scope    map[string]any       `json:"scope"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload struct {
			Commands []telegramBotCommand `json:"commands"`
			Scope    map[string]any       `json:"scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payloads = append(payloads, payload)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	err := registerTelegramBotCommands(t.Context(), map[string]string{
		"bot_token":    "123456:ABC",
		"api_base_url": server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bot123456:ABC/setMyCommands" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(payloads) < 3 {
		t.Fatalf("expected default/private/group command registrations, got %d", len(payloads))
	}
	if len(payloads[0].Commands) == 0 || payloads[0].Commands[0].Command != "start" {
		t.Fatalf("commands not registered: %#v", payloads[0].Commands)
	}
	var groupCommands []telegramBotCommand
	for _, payload := range payloads {
		if payload.Scope["type"] == "all_group_chats" {
			groupCommands = payload.Commands
			break
		}
	}
	if len(groupCommands) == 0 {
		t.Fatal("group command scope was not registered")
	}
	for _, command := range groupCommands {
		if command.Command == "users" || command.Command == "status" || command.Command == "cleanup" || command.Command == "register" || command.Command == "redeem" {
			t.Fatalf("group commands must not expose private/admin command %q", command.Command)
		}
	}
}

func TestDeleteTelegramWebhookBeforePolling(t *testing.T) {
	var gotPath string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	err := deleteTelegramWebhook(t.Context(), map[string]string{
		"bot_token":    "123456:ABC",
		"api_base_url": server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bot123456:ABC/deleteWebhook" {
		t.Fatalf("path = %q", gotPath)
	}
	if got := payload["drop_pending_updates"]; got != false {
		t.Fatalf("drop_pending_updates = %#v, want false", got)
	}
}

func TestTelegramCommandMenusSeparateGroupAndAdminCommands(t *testing.T) {
	privateNames := telegramCommandNames(telegramPrivateBotCommandMenu())
	for _, required := range []string{"setname", "setpass"} {
		if !privateNames[required] {
			t.Fatalf("private menu should include %s", required)
		}
	}
	for _, hiddenAlias := range []string{"myinfo", "count"} {
		if privateNames[hiddenAlias] {
			t.Fatalf("private menu should hide compatibility alias %s", hiddenAlias)
		}
		if !telegramSupportedCommand("/" + hiddenAlias) {
			t.Fatalf("compatibility alias /%s should remain executable", hiddenAlias)
		}
	}

	groupNames := telegramCommandNames(telegramGroupBotCommandMenu())
	for _, forbidden := range []string{"status", "search", "downloads", "stats", "users", "cleanup", "cleanup_rule", "register", "redeem"} {
		if groupNames[forbidden] {
			t.Fatalf("group menu should not expose %s", forbidden)
		}
	}
	for _, required := range []string{"start", "menu", "help", "account", "signin", "devices", "kick", "hideadult"} {
		if !groupNames[required] {
			t.Fatalf("group menu should include %s", required)
		}
	}
	adminCommands := telegramAdminBotCommandMenu()
	adminNames := telegramCommandNames(adminCommands)
	for _, required := range []string{"users", "status", "cleanup_mode", "cleanup_rule", "ucr", "uinfo", "rmemby", "only_rm_record", "renewall", "userip", "auditip", "auditdevice", "auditclient", "udeviceid", "syncunbound", "syncgroupm", "check_ex", "deleted", "embyadmin", "banall", "unbanall", "prouser", "revuser", "embylibs_blockall", "embylibs_unblockall", "proadmin", "revadmin", "backup_db", "restore_from_db"} {
		if !adminNames[required] {
			t.Fatalf("admin menu should include %s", required)
		}
	}
	for _, hiddenAlias := range []string{"myinfo", "count", "low_activity", "urm", "only_rm_emby", "extraembylibs_blockall", "extraembylibs_unblockall"} {
		if adminNames[hiddenAlias] {
			t.Fatalf("admin menu should hide compatibility alias %s", hiddenAlias)
		}
	}
	for _, command := range adminCommands {
		if strings.Contains(command.Description, "Mgo 兼容") {
			t.Fatalf("admin menu command %s should use native Mgo wording: %q", command.Command, command.Description)
		}
	}
	help := telegramMgoAdminCommandHelp()
	for _, want := range []string{"用户：", "审计：", "清理：", "权限：", "运维："} {
		if !strings.Contains(help, want) {
			t.Fatalf("mgo admin help should include category %q in %q", want, help)
		}
	}
	if strings.Contains(help, "/setpass") {
		t.Fatalf("mgo admin help should not include user self-service command /setpass")
	}
}

func telegramCommandNames(commands []telegramBotCommand) map[string]bool {
	names := make(map[string]bool, len(commands))
	for _, command := range commands {
		names[command.Command] = true
	}
	return names
}

func TestTelegramProxyCandidatesDefaultLocalFallbacks(t *testing.T) {
	got := telegramProxyCandidates(map[string]string{})
	joined := strings.Join(got, ",")
	for _, want := range []string{"host.docker.internal:20171", "socks5://172.17.0.1:20170", "127.0.0.1:10808", "172.17.0.1:7890"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("default proxy candidates %q missing %q", joined, want)
		}
	}
}

func TestTelegramHTTPClientsPreferConfiguredProxy(t *testing.T) {
	clients := telegramHTTPClients(time.Second, map[string]string{
		"proxy_url": "http://proxy.example:7890",
	})
	if len(clients) == 0 {
		t.Fatal("expected telegram clients")
	}
	if got := telegramClientProxyString(t, clients[0]); got != "http://proxy.example:7890" {
		t.Fatalf("first client proxy = %q, want configured proxy", got)
	}
}

func telegramClientProxyString(t *testing.T, client *http.Client) string {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		return ""
	}
	req, err := http.NewRequest(http.MethodGet, defaultTelegramAPIBaseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL == nil {
		return ""
	}
	return proxyURL.String()
}

func TestTelegramReplyAutoDeletesSentMessage(t *testing.T) {
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			requests <- "sendMessage"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":777}}`))
		case strings.HasSuffix(r.URL.Path, "/deleteMessage"):
			requests <- "deleteMessage"
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, _ := json.Marshal(map[string]string{
		"bot_token":           "123456:ABC-def",
		"api_base_url":        server.URL,
		"auto_delete_seconds": "0",
	})
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfg)}
	if err := bot.reply(context.Background(), channel, 42, telegramCommandReply{Text: "hello"}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitForTelegramMethod(t, requests, "sendMessage")
	waitForTelegramMethod(t, requests, "deleteMessage")
}

func TestTelegramGroupCommandSendsPanelInGroup(t *testing.T) {
	var payloads []struct {
		ChatID      any            `json:"chat_id"`
		Text        string         `json:"text"`
		ReplyMarkup map[string]any `json:"reply_markup"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			ChatID      any            `json:"chat_id"`
			Text        string         `json:"text"`
			ReplyMarkup map[string]any `json:"reply_markup"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode sendMessage: %v", err)
		}
		payloads = append(payloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":777}}`))
	}))
	defer server.Close()

	repos, bot := newBotTestService(t)
	cfg, _ := json.Marshal(map[string]string{
		"bot_token":           "123456:ABC-def",
		"api_base_url":        server.URL,
		"group_chat_id":       "-100123",
		"auto_delete_seconds": "-1",
	})
	if err := repos.DB.Create(&model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfg)}).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	update, _ := json.Marshal(TelegramUpdate{
		UpdateID: 1,
		Message: &TelegramMessage{
			MessageID: 55,
			From:      TelegramUser{ID: 9002, Username: "viewer", FirstName: "Viewer"},
			Chat:      TelegramChat{ID: -100123, Type: "supergroup"},
			Text:      "/menu",
		},
	})
	if err := bot.HandleWebhook(t.Context(), update); err != nil {
		t.Fatalf("handle webhook: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("sendMessage count = %d, payloads=%#v", len(payloads), payloads)
	}
	if got := fmt.Sprint(payloads[0].ChatID); got != "-100123" {
		t.Fatalf("message should stay in group, chat_id=%s payload=%#v", got, payloads[0])
	}
	if strings.Contains(payloads[0].Text, "管理员入口") {
		t.Fatalf("normal group user must not see admin panel: %#v", payloads[0])
	}
}

func TestTelegramGroupCallbackIsRejected(t *testing.T) {
	var callbackPayloads []struct {
		CallbackID string `json:"callback_query_id"`
		Text       string `json:"text"`
		ShowAlert  bool   `json:"show_alert"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			CallbackID string `json:"callback_query_id"`
			Text       string `json:"text"`
			ShowAlert  bool   `json:"show_alert"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode answerCallbackQuery: %v", err)
		}
		callbackPayloads = append(callbackPayloads, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "secret-pass")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	if err := repos.DB.AutoMigrate(&model.NotifyChannel{}); err != nil {
		t.Fatalf("migrate notify channel: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 9002,
		TelegramName:   "@viewer",
		ChatID:         9002,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	cfg, _ := json.Marshal(map[string]string{
		"bot_token":           "123456:ABC-def",
		"api_base_url":        server.URL,
		"group_chat_id":       "-100123",
		"auto_delete_seconds": "-1",
	})
	if err := repos.DB.Create(&model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfg)}).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	before, _ := repos.User.FindByID(ctx, user.ID)
	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	update, _ := json.Marshal(TelegramUpdate{
		UpdateID: 2,
		CallbackQuery: &TelegramCallbackQuery{
			ID:      "cb-group",
			From:    TelegramUser{ID: 9002, Username: "viewer", FirstName: "Viewer"},
			Message: &TelegramMessage{MessageID: 56, Chat: TelegramChat{ID: -100123, Type: "supergroup"}},
			Data:    "adult_toggle",
		},
	})
	if err := bot.HandleWebhook(ctx, update); err != nil {
		t.Fatalf("handle webhook: %v", err)
	}
	if len(callbackPayloads) != 1 {
		t.Fatalf("answerCallbackQuery count = %d", len(callbackPayloads))
	}
	if !callbackPayloads[0].ShowAlert || !strings.Contains(callbackPayloads[0].Text, "群组内按钮面板已禁用") {
		t.Fatalf("unexpected callback answer: %#v", callbackPayloads[0])
	}
	after, _ := repos.User.FindByID(ctx, user.ID)
	if before == nil || after == nil || before.HideAdult != after.HideAdult {
		t.Fatalf("group callback should not mutate user adult visibility: before=%#v after=%#v", before, after)
	}
}

func waitForTelegramMethod(t *testing.T, requests <-chan string, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case got := <-requests:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for telegram %s", want)
		}
	}
}

func TestTelegramCommandFiltering(t *testing.T) {
	if telegramIsCommandText("今天看什么") {
		t.Fatal("plain chat message should not be treated as command")
	}
	if !telegramIsCommandText("/start user pass") {
		t.Fatal("/start should be treated as command")
	}
	if got := telegramCommandName("/hideadult@MediaStationGoBot on"); got != "/hideadult" {
		t.Fatalf("telegramCommandName = %q, want /hideadult", got)
	}
	if telegramSupportedCommand("/签到") {
		t.Fatal("unrelated group bot command should not be handled")
	}
	for _, cmd := range []string{"/signin", "/redeem", "/gencode", "/users", "/renew_user", "/delete_user", "/cleanup_rule"} {
		if !telegramSupportedCommand(cmd) {
			t.Fatalf("%s should be supported so group slash commands get feedback", cmd)
		}
	}
	for _, cmd := range []string{"/restart", "/update_bot", "/coins", "/red", "/white_channel", "/config"} {
		if telegramSupportedCommand(cmd) {
			t.Fatalf("%s should not be treated as supported until it has a real Mgo implementation", cmd)
		}
	}
}

func TestTelegramSupportedCommandSetMatchesRegistry(t *testing.T) {
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "admin"}, Chat: TelegramChat{ID: 9001, Type: "private"}}
	for _, def := range bot.telegramCommandDefinitions(t.Context(), channel, msg) {
		for _, alias := range def.Aliases {
			if !telegramSupportedCommand(alias) {
				t.Fatalf("registered command %s must be in telegramSupportedCommandSet", alias)
			}
		}
	}
}

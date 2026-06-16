package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestTelegramUpdateActionableDispatchesCallbackQuery(t *testing.T) {
	if !telegramUpdateActionable(TelegramUpdate{CallbackQuery: &TelegramCallbackQuery{Data: "adult_toggle"}}) {
		t.Fatal("callback_query update must be dispatched, otherwise inline buttons break")
	}
	if !telegramUpdateActionable(TelegramUpdate{Message: &TelegramMessage{Text: "/help"}}) {
		t.Fatal("text command message must be dispatched")
	}
	if telegramUpdateActionable(TelegramUpdate{}) {
		t.Fatal("empty update must be skipped")
	}
	if telegramUpdateActionable(TelegramUpdate{Message: &TelegramMessage{}}) {
		t.Fatal("message without text must be skipped")
	}
}

func TestTelegramCallbackTogglesAdultVisibility(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "secret-pass")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 30001,
		TelegramName:   "@viewer",
		ChatID:         30001,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	if err := repos.DB.AutoMigrate(&model.NotifyChannel{}); err != nil {
		t.Fatalf("migrate notify_channels: %v", err)
	}
	// 配置一个绑定该 Telegram 用户的渠道（无 bot_token，避免测试触发网络请求）。
	cfg, _ := json.Marshal(map[string]string{"admin_user_ids": "30001"})
	if err := repos.DB.Create(&model.NotifyChannel{
		Name:    "Telegram",
		Type:    "telegram",
		Enabled: true,
		Config:  string(cfg),
	}).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}

	before, err := repos.User.FindByID(ctx, user.ID)
	if err != nil || before == nil {
		t.Fatalf("load user before toggle: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	update, _ := json.Marshal(TelegramUpdate{
		UpdateID: 1,
		CallbackQuery: &TelegramCallbackQuery{
			ID:      "cb1",
			From:    TelegramUser{ID: 30001, Username: "viewer", FirstName: "Viewer"},
			Message: &TelegramMessage{MessageID: 5, Chat: TelegramChat{ID: 30001, Type: "private"}},
			Data:    "adult_toggle",
		},
	})
	// reply 因 bot_token 为空会返回错误，但成人目录状态应已在数据库中被切换。
	_ = bot.HandleWebhook(ctx, update)

	updated, err := repos.User.FindByID(ctx, user.ID)
	if err != nil || updated == nil {
		t.Fatalf("reload user: %v", err)
	}
	if updated.HideAdult == before.HideAdult {
		t.Fatalf("adult_toggle callback should have flipped HideAdult (was %v)", before.HideAdult)
	}
}

func TestTelegramRegisterRespectsAdminToggle(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	if err := repos.DB.AutoMigrate(&model.Setting{}, &model.NotifyChannel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 预置一个管理员，确保通过 Bot 注册的用户是普通角色而非首个管理员。
	if _, _, err := auth.Register(ctx, "rootadmin", "admin-pass"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	// 把注册者放进 admin_user_ids，即可让 telegramUserCanBind 通过（私聊场景，
	// 无需走 getChatMember 网络校验）；注册流程本身不依赖角色。
	cfgJSON, _ := json.Marshal(map[string]string{"admin_user_ids": "999"})
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfgJSON)}

	msg := &TelegramMessage{From: TelegramUser{ID: 999, Username: "newbie", FirstName: "Newbie"}, Chat: TelegramChat{ID: 999, Type: "private"}}

	// 默认关闭：拒绝且不创建用户。
	if reply := bot.cmdRegister(ctx, channel, msg, []string{"newbie", "secret-pass"}); !strings.Contains(reply.Text, "未开放") {
		t.Fatalf("registration disabled by default, got %q", reply.Text)
	}
	if u, _ := repos.User.FindByUsername(ctx, "newbie"); u != nil {
		t.Fatal("no user should be created while registration disabled")
	}

	// 管理员开启后注册成功并自动绑定。
	if err := bot.setRegistrationEnabled(ctx, true); err != nil {
		t.Fatalf("enable registration: %v", err)
	}
	reply := bot.cmdRegister(ctx, channel, msg, []string{"newbie", "secret-pass"})
	if !strings.Contains(reply.Text, "注册并绑定成功") {
		t.Fatalf("expected success reply, got %q", reply.Text)
	}
	created, err := repos.User.FindByUsername(ctx, "newbie")
	if err != nil || created == nil {
		t.Fatalf("user should be created after enabling: %v", err)
	}
	if created.Role != "user" {
		t.Fatalf("bot-registered account should be a regular user, got role %q", created.Role)
	}
	if binding := bot.telegramBinding(ctx, 999); binding == nil || binding.UserID != created.ID {
		t.Fatalf("telegram should be bound to the newly registered user")
	}

	// 重复注册：已绑定 → 提示无需重复注册。
	if reply := bot.cmdRegister(ctx, channel, msg, []string{"another", "pass-2"}); !strings.Contains(reply.Text, "无需重复注册") {
		t.Fatalf("expected already-bound reply, got %q", reply.Text)
	}
}

func TestTelegramGroupHidesAdminPanelFromRegularUsers(t *testing.T) {
	ctx := t.Context()
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"group_chat_id":"-100123","admin_user_ids":"9001"}`}
	msg := &TelegramMessage{
		From: TelegramUser{ID: 9002, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: -100123, Type: "supergroup"},
	}

	menu := bot.mainMenu(ctx, channel, msg)
	if strings.Contains(menu.Text, "管理员") || telegramReplyHasButtonPrefix(menu, "adm_") {
		t.Fatalf("regular group user must not see admin panel: text=%q buttons=%#v", menu.Text, menu.Buttons)
	}

	reply, err := bot.executeCommand(ctx, channel, msg, "/users")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "" || len(reply.Buttons) != 0 {
		t.Fatalf("regular group user admin command should be ignored, got %#v", reply)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/start viewer secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "请私聊 Bot") {
		t.Fatalf("group credential command should point to private chat, got %q", reply.Text)
	}
}

func TestTelegramGroupAdminMenuDoesNotExposeButtonsInGroup(t *testing.T) {
	ctx := t.Context()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"group_chat_id":"-100123","admin_user_ids":"9001"}`}
	msg := &TelegramMessage{
		From: TelegramUser{ID: 9001, Username: "admin", FirstName: "Admin"},
		Chat: TelegramChat{ID: -100123, Type: "group"},
	}

	menu := bot.mainMenu(ctx, channel, msg)
	if telegramReplyHasButtonPrefix(menu, "adm_") {
		t.Fatalf("admin group menu must not expose admin buttons publicly: %#v", menu.Buttons)
	}
	if !strings.Contains(menu.Text, "请私聊 Bot") {
		t.Fatalf("admin group menu should tell admins to use private chat, got %q", menu.Text)
	}

	reply, handled := bot.handleMenuCallback(ctx, channel, msg, "adm_users")
	if !handled {
		t.Fatal("admin callback should be handled")
	}
	if !strings.Contains(reply.Text, "请私聊 Bot") || telegramReplyHasButtonPrefix(reply, "adm_") {
		t.Fatalf("group admin callback should not render admin panel publicly: %#v", reply)
	}

	reply, err := bot.executeCommand(ctx, channel, msg, "/users")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "用户管理") {
		t.Fatalf("bound group admin text command should run, got %q", reply.Text)
	}
	if len(reply.Buttons) != 0 {
		t.Fatalf("group admin text command must not expose inline buttons publicly: %#v", reply.Buttons)
	}
}

func TestTelegramPollingChannelHintWinsForPrivateMessages(t *testing.T) {
	ctx := t.Context()
	repos, bot := newBotTestService(t)
	msg := &TelegramMessage{
		From: TelegramUser{ID: 9101, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: 9101, Type: "private"},
	}
	bad := model.NotifyChannel{Name: "BadToken", Type: "telegram", Enabled: true, Config: `{"bot_token":"bad","admin_user_ids":"9101"}`}
	good := model.NotifyChannel{Name: "GoodToken", Type: "telegram", Enabled: true, Config: `{"bot_token":"good","admin_user_ids":"9101"}`}
	if err := repos.DB.Create(&bad).Error; err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&good).Error; err != nil {
		t.Fatal(err)
	}

	if first := bot.findChannelForMessage(ctx, msg); first == nil || first.ID != bad.ID {
		t.Fatalf("setup expected normal private lookup to pick first channel, got %#v", first)
	}
	if hinted := bot.channelForMessage(ctx, msg, &good); hinted == nil || hinted.ID != good.ID {
		t.Fatalf("polling channel hint should route replies through the token that received the update, got %#v", hinted)
	}
}

func TestTelegramMgoCompatibleUserCommands(t *testing.T) {
	ctx := t.Context()
	repos, bot := newBotTestService(t)
	user := &model.User{Username: "viewer", PasswordHash: "hash", Role: "user", IsActive: true}
	if err := repos.User.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{TelegramUserID: 9102, ChatID: 9102, UserID: user.ID}).Error; err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9102"}`}
	msg := &TelegramMessage{
		From: TelegramUser{ID: 9102, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: 9102, Type: "private"},
	}

	info, err := bot.executeCommand(ctx, channel, msg, "/myinfo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Text, "我的账号") {
		t.Fatalf("/myinfo should show account info, got %q", info.Text)
	}
	count, err := bot.executeCommand(ctx, channel, msg, "/count")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(count.Text, "媒体库统计") {
		t.Fatalf("/count should show library counts, got %q", count.Text)
	}
}

func telegramReplyHasButtonPrefix(reply telegramCommandReply, prefix string) bool {
	for _, row := range reply.Buttons {
		for _, button := range row {
			if strings.HasPrefix(button.Data, prefix) {
				return true
			}
		}
	}
	return false
}

func TestTelegramStartClearsStaleUserBinding(t *testing.T) {
	repos, auth, _, _ := newAuthTestServices(t)
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20001,
		TelegramName:   "@viewer",
		ChatID:         20001,
		UserID:         "deleted-user",
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)

	reply := bot.cmdStart(t.Context(), &TelegramMessage{
		From: TelegramUser{ID: 20001, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: 20001, Type: "private"},
	}, nil)

	if !strings.Contains(reply.Text, "已不存在") {
		t.Fatalf("expected stale binding message, got %q", reply.Text)
	}
	var count int64
	if err := repos.DB.Model(&model.TelegramBinding{}).Where("telegram_user_id = ?", 20001).Count(&count).Error; err != nil {
		t.Fatalf("count binding: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale binding should be removed, got %d", count)
	}
}

func TestTelegramStartRejectsAccountAlreadyBoundToAnotherTelegram(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "secret-pass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20001,
		TelegramName:   "@viewer-one",
		ChatID:         20001,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	cfgJSON, _ := json.Marshal(map[string]string{"admin_user_ids": "20002"})
	if err := repos.DB.AutoMigrate(&model.NotifyChannel{}); err != nil {
		t.Fatalf("migrate notify channel: %v", err)
	}
	if err := repos.DB.Create(&model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfgJSON)}).Error; err != nil {
		t.Fatalf("create notify channel: %v", err)
	}
	msg := &TelegramMessage{
		From: TelegramUser{ID: 20002, Username: "viewer-two", FirstName: "Viewer Two"},
		Chat: TelegramChat{ID: 20002, Type: "private"},
	}
	reply := bot.cmdStart(ctx, msg, []string{"viewer", "secret-pass"})

	if !strings.Contains(reply.Text, "已绑定其他 Telegram") {
		t.Fatalf("expected already-bound rejection, got %q", reply.Text)
	}
	var accountBindings int64
	if err := repos.DB.Model(&model.TelegramBinding{}).Where("user_id = ?", user.ID).Count(&accountBindings).Error; err != nil {
		t.Fatalf("count account bindings: %v", err)
	}
	if accountBindings != 1 {
		t.Fatalf("account should keep exactly one telegram binding, got %d", accountBindings)
	}
	if binding := bot.telegramBinding(ctx, 20002); binding != nil {
		t.Fatal("second telegram account must not be bound")
	}
}

func TestTelegramStartUnbindsWhenBoundPasswordChanged(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "old-password")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20003,
		TelegramName:   "@viewer",
		ChatID:         20003,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	if err := auth.ResetPassword(ctx, user.ID, "new-password"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if err := repos.DB.AutoMigrate(&model.NotifyChannel{}); err != nil {
		t.Fatalf("migrate notify channel: %v", err)
	}
	cfgJSON, _ := json.Marshal(map[string]string{"admin_user_ids": "20003"})
	if err := repos.DB.Create(&model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: string(cfgJSON)}).Error; err != nil {
		t.Fatalf("create notify channel: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	msg := &TelegramMessage{
		From: TelegramUser{ID: 20003, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: 20003, Type: "private"},
	}
	reply := bot.cmdStart(ctx, msg, []string{"viewer", "old-password"})

	if !strings.Contains(reply.Text, "已自动解绑") {
		t.Fatalf("expected auto unbind reply, got %q", reply.Text)
	}
	if binding := bot.telegramBinding(ctx, 20003); binding != nil {
		t.Fatal("stale binding should be removed after password mismatch")
	}
}

func TestTelegramSelfSetNameRequiresCurrentPassword(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "old-password")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20004,
		TelegramName:   "@viewer",
		ChatID:         20004,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	msg := &TelegramMessage{From: TelegramUser{ID: 20004, Username: "viewer"}, Chat: TelegramChat{ID: 20004, Type: "private"}}
	if reply := bot.selfSetName(ctx, msg, "renamed"); !strings.Contains(reply.Text, "当前密码 新用户名") {
		t.Fatalf("expected usage reply, got %q", reply.Text)
	}
	if reply := bot.selfSetName(ctx, msg, "old-password renamed"); !strings.Contains(reply.Text, "用户名已修改") {
		t.Fatalf("expected rename success, got %q", reply.Text)
	}
	updated, _ := repos.User.FindByID(ctx, user.ID)
	if updated == nil || updated.Username != "renamed" {
		t.Fatalf("username not updated: %#v", updated)
	}
}

func TestTelegramSelfSetPassWrongCurrentPasswordUnbinds(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "old-password")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20005,
		TelegramName:   "@viewer",
		ChatID:         20005,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	msg := &TelegramMessage{From: TelegramUser{ID: 20005, Username: "viewer"}, Chat: TelegramChat{ID: 20005, Type: "private"}}
	reply := bot.selfSetPass(ctx, msg, "wrong-password new-password")

	if !strings.Contains(reply.Text, "已自动解绑") {
		t.Fatalf("expected auto unbind reply, got %q", reply.Text)
	}
	if binding := bot.telegramBinding(ctx, 20005); binding != nil {
		t.Fatal("binding should be removed after wrong current password")
	}
	if _, err := auth.Login(ctx, "viewer", "old-password"); err != nil {
		t.Fatalf("old password should remain valid after failed change: %v", err)
	}
}

func TestTelegramSelfSetPassChangesPasswordWithCurrentPassword(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "old-password")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 20006,
		TelegramName:   "@viewer",
		ChatID:         20006,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}

	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	msg := &TelegramMessage{From: TelegramUser{ID: 20006, Username: "viewer"}, Chat: TelegramChat{ID: 20006, Type: "private"}}
	reply := bot.selfSetPass(ctx, msg, "old-password new-password")

	if !strings.Contains(reply.Text, "密码已修改") {
		t.Fatalf("expected password change success, got %q", reply.Text)
	}
	if _, err := auth.Login(ctx, "viewer", "old-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password should fail, got %v", err)
	}
	if _, err := auth.Login(ctx, "viewer", "new-password"); err != nil {
		t.Fatalf("new password should login: %v", err)
	}
	if binding := bot.telegramBinding(ctx, 20006); binding == nil {
		t.Fatal("successful password change should keep telegram binding")
	}
}

func TestTelegramBindingFromGroupStoresPrivateUserChatID(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "secret-pass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	msg := &TelegramMessage{
		From: TelegramUser{ID: 21001, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: -100123456, Type: "group"},
	}

	if err := bot.upsertTelegramBinding(ctx, msg, user.ID); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}
	binding := bot.telegramBinding(ctx, 21001)
	if binding == nil {
		t.Fatal("binding should be created")
	}
	if binding.ChatID != 21001 {
		t.Fatalf("group binding must store private user chat id, got %d", binding.ChatID)
	}
}

func TestTelegramBindingFromGroupPreservesExistingPrivateChatID(t *testing.T) {
	ctx := t.Context()
	repos, auth, _, _ := newAuthTestServices(t)
	user, _, err := auth.Register(ctx, "viewer", "secret-pass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 21002,
		TelegramName:   "@viewer",
		ChatID:         987654,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	bot := NewTelegramBotService(zap.NewNop(), repos, nil, auth)
	msg := &TelegramMessage{
		From: TelegramUser{ID: 21002, Username: "viewer", FirstName: "Viewer"},
		Chat: TelegramChat{ID: -100123456, Type: "supergroup"},
	}

	if err := bot.upsertTelegramBinding(ctx, msg, user.ID); err != nil {
		t.Fatalf("upsert binding: %v", err)
	}
	binding := bot.telegramBinding(ctx, 21002)
	if binding == nil {
		t.Fatal("binding should exist")
	}
	if binding.ChatID != 987654 {
		t.Fatalf("group command must not overwrite existing private chat id, got %d", binding.ChatID)
	}
}

func TestTelegramPrivateNotifyChatIDFallsBackFromLegacyGroupBinding(t *testing.T) {
	binding := model.TelegramBinding{
		TelegramUserID: 21003,
		ChatID:         -100123456,
	}
	if got := telegramPrivateChatIDFromBinding(binding); got != 21003 {
		t.Fatalf("legacy group binding should notify private user chat, got %d", got)
	}
}

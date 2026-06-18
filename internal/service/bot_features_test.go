package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func newBotTestService(t *testing.T) (*repository.Container, *TelegramBotService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repos := repository.New(db)
	cfg := &config.Config{}
	cfg.Secrets.JWTSecret = "test-secret"
	log := zap.NewNop()
	perms := NewPermissionService(log, repos)
	tokenSvc := NewTokenService(cfg, log, repos)
	auth := NewAuthService(cfg, log, repos, tokenSvc, perms)
	crypto := NewCryptoService("test-secret", log)
	bot := NewTelegramBotService(log, repos, crypto, auth)
	return repos, bot
}

// ── pure logic ──────────────────────────────────────────────────────────────

func TestRenewExpiry(t *testing.T) {
	// 永久（0 天）→ nil
	if got := renewExpiry(nil, 0); got != nil {
		t.Fatalf("expected nil for permanent, got %v", got)
	}
	// 从现在起 +30 天（当前为空）
	got := renewExpiry(nil, 30)
	if got == nil || got.Before(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("expected ~30d expiry, got %v", got)
	}
	// 已有未来到期 → 在原到期基础上叠加
	future := time.Now().Add(10 * 24 * time.Hour)
	got = renewExpiry(&future, 30)
	if got == nil || got.Before(future.Add(29*24*time.Hour)) {
		t.Fatalf("expected stacking on future expiry, got %v", got)
	}
	// 已过期 → 从现在起算
	past := time.Now().Add(-10 * 24 * time.Hour)
	got = renewExpiry(&past, 5)
	if got == nil || got.Before(time.Now().Add(4*24*time.Hour)) {
		t.Fatalf("expected fresh window from now, got %v", got)
	}
}

func TestCapacityRemaining(t *testing.T) {
	cases := []struct {
		name string
		c    capacityInfo
		want int64
	}{
		{"license only", capacityInfo{UsedUsers: 5, MaxUsers: 20}, 15},
		{"quota tighter", capacityInfo{UsedUsers: 5, MaxUsers: 100, OpenRegLimit: 10, OpenRegUsed: 3}, 7},
		{"license tighter", capacityInfo{UsedUsers: 95, MaxUsers: 100, OpenRegLimit: 50, OpenRegUsed: 0}, 5},
		{"full", capacityInfo{UsedUsers: 20, MaxUsers: 20}, 0},
		{"quota exhausted", capacityInfo{UsedUsers: 1, MaxUsers: 100, OpenRegLimit: 5, OpenRegUsed: 5}, 0},
	}
	for _, tc := range cases {
		if got := tc.c.Remaining(); got != tc.want {
			t.Errorf("%s: Remaining()=%d want %d", tc.name, got, tc.want)
		}
	}
}

func TestRandomWindowDays(t *testing.T) {
	for i := 0; i < 200; i++ {
		d := randomWindowDays(3, 5)
		if d < 3 || d > 5 {
			t.Fatalf("randomWindowDays(3,5)=%d out of range", d)
		}
	}
	if d := randomWindowDays(4, 4); d != 4 {
		t.Fatalf("randomWindowDays(4,4)=%d want 4", d)
	}
}

func TestFingerprintStability(t *testing.T) {
	a := fingerprint("Infuse", "iPhone")
	b := fingerprint("infuse", " iPhone ")
	if a != b {
		t.Fatalf("fingerprint should be case/space-insensitive: %s != %s", a, b)
	}
	if a != fingerprint("Emby", "iPhone") {
		t.Fatal("different apps on the same terminal must share one fingerprint")
	}
	if a == fingerprint("Infuse", "iPad") {
		t.Fatal("different device names must yield different fingerprints")
	}
}

// ── DB-backed ─────────────────────────────────────────────────────────────

func TestSignInStreak(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	u := &model.User{Username: "alice", PasswordHash: "x", Role: "user"}
	if err := repos.User.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	res, err := bot.signIn(ctx, u.ID)
	if err != nil || res.Streak != 1 || res.Total != 1 {
		t.Fatalf("first sign-in: %+v err=%v", res, err)
	}
	// 同日重复签到 → 不增长
	res, _ = bot.signIn(ctx, u.ID)
	if !res.AlreadySigned || res.Streak != 1 {
		t.Fatalf("same-day re-signin should be no-op: %+v", res)
	}
	// 模拟昨天签到 → 连续 +1
	rec, _ := repos.SignIn.Get(ctx, u.ID)
	rec.LastSignIn = time.Now().Add(-24 * time.Hour)
	_ = repos.SignIn.Save(ctx, rec)
	res, _ = bot.signIn(ctx, u.ID)
	if res.Streak != 2 || res.Total != 2 {
		t.Fatalf("consecutive day should bump streak: %+v", res)
	}
	// 中断（前天）→ 重置为 1
	rec, _ = repos.SignIn.Get(ctx, u.ID)
	rec.LastSignIn = time.Now().Add(-72 * time.Hour)
	_ = repos.SignIn.Save(ctx, rec)
	res, _ = bot.signIn(ctx, u.ID)
	if res.Streak != 1 {
		t.Fatalf("broken streak should reset to 1: %+v", res)
	}
}

func TestRegistrationCodeRedeemOnce(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)

	code, err := bot.generateCode(ctx, model.RegistrationCodeRenew, 30, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// 首次校验通过
	rc, msg := bot.lookupRedeemableCode(ctx, code.Code, model.RegistrationCodeRenew)
	if rc == nil {
		t.Fatalf("expected valid code, got msg=%q", msg)
	}
	// 标记使用后不可再用
	if err := repos.RegCode.MarkUsed(ctx, rc.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if _, msg := bot.lookupRedeemableCode(ctx, code.Code, model.RegistrationCodeRenew); msg == "" {
		t.Fatal("used code must not validate again")
	}
	// 第二次 MarkUsed 应失败（防止双花）
	if err := repos.RegCode.MarkUsed(ctx, rc.ID, "user-2"); err == nil {
		t.Fatal("double-spend should be rejected")
	}
	// 类型不匹配应被拒
	reg, _ := bot.generateCode(ctx, model.RegistrationCodeRegister, 0, 0, "")
	if _, msg := bot.lookupRedeemableCode(ctx, reg.Code, model.RegistrationCodeRenew); msg == "" {
		t.Fatal("register code should not validate as renew")
	}
}

func TestRegistrationCodeCanBeGeneratedForMultipleUses(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)

	code, err := bot.generateCodeWithUses(ctx, model.RegistrationCodeRenew, 30, 0, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	rc, msg := bot.lookupRedeemableCode(ctx, code.Code, model.RegistrationCodeRenew)
	if rc == nil {
		t.Fatalf("expected valid code, got msg=%q", msg)
	}
	if err := repos.RegCode.MarkUsed(ctx, rc.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	rc, msg = bot.lookupRedeemableCode(ctx, code.Code, model.RegistrationCodeRenew)
	if rc == nil {
		t.Fatalf("code should remain redeemable after first use, got msg=%q", msg)
	}
	if err := repos.RegCode.MarkUsed(ctx, rc.ID, "user-2"); err != nil {
		t.Fatal(err)
	}
	if _, msg := bot.lookupRedeemableCode(ctx, code.Code, model.RegistrationCodeRenew); msg == "" {
		t.Fatal("code should be exhausted after max uses")
	}
	var used model.RegistrationCode
	if err := repos.DB.Where("id = ?", code.ID).First(&used).Error; err != nil {
		t.Fatal(err)
	}
	if used.UsedCount != 2 || used.UsedAt == nil {
		t.Fatalf("expected exhausted code with used_count=2, got %+v", used)
	}
}

func TestRenewalClearsExpiry(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	past := time.Now().Add(-time.Hour)
	u := &model.User{Username: "bob", PasswordHash: "x", Role: "user", IsActive: false, ExpiredAt: &past}
	if err := repos.User.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := bot.applyRenewal(ctx, u.ID, 30); err != nil {
		t.Fatal(err)
	}
	got, _ := repos.User.FindByID(ctx, u.ID)
	if !got.IsActive {
		t.Fatal("renewal should re-activate account")
	}
	if got.ExpiredAt == nil || got.ExpiredAt.Before(time.Now()) {
		t.Fatalf("renewal should set future expiry, got %v", got.ExpiredAt)
	}
}

func TestDeviceKickAndConcurrency(t *testing.T) {
	ctx := context.Background()
	repos, _ := newBotTestService(t)
	dev := NewDeviceService(zap.NewNop(), repos)
	u := &model.User{Username: "carol", PasswordHash: "x", Role: "user", IsActive: true}
	if err := repos.User.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	dev.RecordLogin(ctx, u.ID, "dev-1", "iPhone", "Infuse", "1.2.3.4")
	dev.RecordPlayback(ctx, u.ID, "dev-1", "iPhone", "Infuse")
	devices, _ := dev.ListDevices(ctx, u.ID)
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}

	// 踢下线后命中 kicked
	if err := dev.KickDevice(ctx, u.ID, "dev-1"); err != nil {
		t.Fatal(err)
	}
	if !dev.IsDeviceKicked(ctx, u.ID, "dev-1") {
		t.Fatal("device should be kicked")
	}
	// 重新登录清除 kicked
	dev.RecordLogin(ctx, u.ID, "dev-1", "iPhone", "Infuse", "1.2.3.4")
	if dev.IsDeviceKicked(ctx, u.ID, "dev-1") {
		t.Fatal("re-login should clear kicked flag")
	}

	// 并发播放计数
	now := time.Now()
	for i, id := range []string{"d1", "d2", "d3", "d4"} {
		_ = repos.UserDevice.Create(ctx, &model.UserDevice{
			UserID: u.ID, DeviceID: id, FirstSeenAt: now, LastSeenAt: now, LastPlayAt: &now,
		})
		_ = i
	}
	n, err := repos.UserDevice.CountConcurrentPlaying(ctx, u.ID, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n < 4 {
		t.Fatalf("expected >=4 concurrent playing, got %d", n)
	}
}

func TestTerminalDeviceLimitDeduplicatesAppsOnSameDevice(t *testing.T) {
	ctx := context.Background()
	repos, _ := newBotTestService(t)
	dev := NewDeviceService(zap.NewNop(), repos)
	u := &model.User{Username: "device-user", PasswordHash: "x", Role: "user", IsActive: true}
	if err := repos.User.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAntiShareEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingMaxLoggedClients, "3"); err != nil {
		t.Fatal(err)
	}

	for _, login := range []struct {
		id     string
		name   string
		client string
	}{
		{id: "phone-infuse", name: "iPhone", client: "Infuse"},
		{id: "phone-emby", name: " iPhone ", client: "Emby"},
		{id: "phone-jellyfin", name: "IPHONE", client: "Jellyfin"},
	} {
		dev.RecordLogin(ctx, u.ID, login.id, login.name, login.client, "1.2.3.4")
	}
	count, err := repos.UserDevice.CountActiveClients(ctx, u.ID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("same terminal through multiple apps should count as 1, got %d", count)
	}
	got, _ := repos.User.FindByID(ctx, u.ID)
	if !got.IsActive {
		t.Fatal("same terminal through multiple apps must not disable the account")
	}

	dev.RecordLogin(ctx, u.ID, "tablet", "iPad", "Infuse", "1.2.3.4")
	dev.RecordLogin(ctx, u.ID, "pc", "Windows PC", "Browser", "1.2.3.4")
	count, err = repos.UserDevice.CountActiveClients(ctx, u.ID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("three distinct terminal devices should count as 3, got %d", count)
	}
	got, _ = repos.User.FindByID(ctx, u.ID)
	if !got.IsActive {
		t.Fatal("device limit is inclusive; 3 of 3 terminals should stay active")
	}

	dev.RecordLogin(ctx, u.ID, "tv", "Apple TV", "Emby", "1.2.3.4")
	got, _ = repos.User.FindByID(ctx, u.ID)
	if got.IsActive {
		t.Fatal("fourth distinct terminal should disable the account")
	}
}

func TestConcurrentPlaybackDeduplicatesAppsOnSameDevice(t *testing.T) {
	ctx := context.Background()
	repos, _ := newBotTestService(t)
	u := &model.User{Username: "play-user", PasswordHash: "x", Role: "user", IsActive: true}
	if err := repos.User.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	fp := fingerprint("Infuse", "Living Room TV")
	for _, row := range []model.UserDevice{
		{UserID: u.ID, DeviceID: "tv-emby", DeviceName: "Living Room TV", Client: "Emby", Fingerprint: fp, FirstSeenAt: now, LastSeenAt: now, LastPlayAt: &now},
		{UserID: u.ID, DeviceID: "tv-jellyfin", DeviceName: "living room tv", Client: "Jellyfin", Fingerprint: fp, FirstSeenAt: now, LastSeenAt: now, LastPlayAt: &now},
		{UserID: u.ID, DeviceID: "phone", DeviceName: "iPhone", Client: "Infuse", Fingerprint: fingerprint("Infuse", "iPhone"), FirstSeenAt: now, LastSeenAt: now, LastPlayAt: &now},
	} {
		if err := repos.UserDevice.Create(ctx, &row); err != nil {
			t.Fatal(err)
		}
	}
	count, err := repos.UserDevice.CountConcurrentPlaying(ctx, u.ID, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("same terminal playback through multiple apps should count as 1 terminal, got %d", count)
	}
}

func TestProtectedAdminNeverViolated(t *testing.T) {
	ctx := context.Background()
	repos, _ := newBotTestService(t)
	dev := NewDeviceService(zap.NewNop(), repos)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	_ = repos.Setting.Set(ctx, SettingAntiShareEnabled, "true")
	cfg := loadBotConfig(ctx, repos)
	// 多次违规也不应删除/警告/禁用管理员
	for i := 0; i < 5; i++ {
		dev.registerFingerprintWarning(ctx, admin.ID, "test", cfg)
	}
	got, _ := repos.User.FindByID(ctx, admin.ID)
	if got == nil {
		t.Fatal("admin must never be auto-deleted")
	}
	if !got.IsActive {
		t.Fatal("admin must never be auto-disabled")
	}
	if got.ShareWarnings != 0 {
		t.Fatalf("admin should accrue no warnings, got %d", got.ShareWarnings)
	}
}

func TestBotAdminCommandsManageDevicePolicy(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 9001,
		TelegramName:   "@root",
		ChatID:         9001,
		UserID:         admin.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "root"}, Chat: TelegramChat{ID: 9001, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/antishare on play=4 login=5 warn=3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "防共享：<b>已开启</b>") {
		t.Fatalf("expected antishare enabled reply, got %q", reply.Text)
	}
	cfg := loadBotConfig(ctx, repos)
	if !cfg.AntiShareEnabled || cfg.MaxConcurrentPlay != 4 || cfg.MaxLoggedClients != 5 || cfg.WarnThreshold != 3 {
		t.Fatalf("unexpected device policy: %+v", cfg)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup_mode count 2")
	if err != nil {
		t.Fatal(err)
	}
	cfg = loadBotConfig(ctx, repos)
	if cfg.AccountCleanupKeepMode != "any" || cfg.AccountCleanupRequiredCount != 1 {
		t.Fatalf("unexpected cleanup mode: %+v; reply=%q", cfg, reply.Text)
	}
	if !strings.Contains(reply.Text, "满足任意一条") {
		t.Fatalf("cleanup mode should explain fixed any-rule policy, got %q", reply.Text)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup_rule add recent_login login_7d 七天内登录 7")
	if err != nil {
		t.Fatal(err)
	}
	cfg = loadBotConfig(ctx, repos)
	found := false
	for _, rule := range cfg.AccountCleanupRules {
		if rule.ID == "login_7d" && rule.Type == "recent_login" && rule.WindowDaysMax == 7 {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleanup rule not added; reply=%q rules=%+v", reply.Text, cfg.AccountCleanupRules)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup_rule edit recent_login login_7d 十四天内登录 14")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已更新规则") {
		t.Fatalf("expected cleanup rule update reply, got %q", reply.Text)
	}
	cfg = loadBotConfig(ctx, repos)
	matches := 0
	for _, rule := range cfg.AccountCleanupRules {
		if rule.ID == "login_7d" {
			matches++
			if rule.Type != "recent_login" || rule.WindowDaysMax != 14 || rule.Name != "十四天内登录" {
				t.Fatalf("cleanup rule should be updated in place, got %+v", rule)
			}
		}
	}
	if matches != 1 {
		t.Fatalf("cleanup rule update should not create duplicates, got %d rules=%+v", matches, cfg.AccountCleanupRules)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup_rule 修改 recent_login login_7d 二十一天内登录 21")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已更新规则") {
		t.Fatalf("expected Chinese cleanup rule update reply, got %q", reply.Text)
	}
	cfg = loadBotConfig(ctx, repos)
	matches = 0
	for _, rule := range cfg.AccountCleanupRules {
		if rule.ID == "login_7d" {
			matches++
			if rule.WindowDaysMax != 21 || rule.Name != "二十一天内登录" {
				t.Fatalf("Chinese cleanup rule update should update values, got %+v", rule)
			}
		}
	}
	if matches != 1 {
		t.Fatalf("Chinese cleanup rule update should not create duplicates, got %d rules=%+v", matches, cfg.AccountCleanupRules)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup_rule add account_age_grace new_7d 7")
	if err != nil {
		t.Fatal(err)
	}
	cfg = loadBotConfig(ctx, repos)
	found = false
	for _, rule := range cfg.AccountCleanupRules {
		if rule.ID == "new_7d" && rule.Type == "account_age_grace" && rule.MinCount == 7 {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleanup shorthand rule not added; reply=%q rules=%+v", reply.Text, cfg.AccountCleanupRules)
	}
}

func TestBotCleanupRulesDefaultToEmpty(t *testing.T) {
	ctx := context.Background()
	repos, _ := newBotTestService(t)

	cfg := loadBotConfig(ctx, repos)
	if len(cfg.AccountCleanupRules) != 0 {
		t.Fatalf("default cleanup rules should be empty, got %+v", cfg.AccountCleanupRules)
	}
}

func TestBotCleanupRulesCanBeDeletedUntilEmpty(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "root"}, Chat: TelegramChat{ID: 9001, Type: "private"}}

	if _, err := bot.executeCommand(ctx, channel, msg, "/cleanup_rule add watch_hours watch_3_5d_6h 观看3到5天满6小时 3 5 6"); err != nil {
		t.Fatal(err)
	}
	reply, err := bot.executeCommand(ctx, channel, msg, "/cleanup_rule del watch_3_5d_6h")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadBotConfig(ctx, repos)
	if len(cfg.AccountCleanupRules) != 0 {
		t.Fatalf("cleanup rules should stay empty after deleting the last rule; reply=%q rules=%+v", reply.Text, cfg.AccountCleanupRules)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup_rule list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "暂无规则") {
		t.Fatalf("expected empty rule list, got %q", reply.Text)
	}
}

func TestBotCleanupRunPreviewsBeforeConfirm(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)
	stale := &model.User{Username: "stale", PasswordHash: "x", Role: "user", IsActive: true}
	stale.CreatedAt = old
	stale.LastLoginAt = &old
	recent := &model.User{Username: "recent", PasswordHash: "x", Role: "user", IsActive: true}
	recent.CreatedAt = old
	recent.LastLoginAt = &now
	newUser := &model.User{Username: "newbie", PasswordHash: "x", Role: "user", IsActive: true}
	newUser.CreatedAt = now
	for _, user := range []*model.User{stale, recent, newUser} {
		if err := repos.User.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupKeepMode, "any"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupRules, `[
		{"id":"login_7d","name":"最近登录","type":"recent_login","enabled":true,"window_days_max":7},
		{"id":"new_7d","name":"新号宽限","type":"account_age_grace","enabled":true,"min_count":7}
	]`); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "root"}, Chat: TelegramChat{ID: 9001, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/cleanup run")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "当前只是预览") || !strings.Contains(reply.Text, "stale") || !strings.Contains(reply.Text, "/cleanup run confirm") {
		t.Fatalf("cleanup run should preview candidates and confirmation command, got %q", reply.Text)
	}
	if got, _ := repos.User.FindByID(ctx, stale.ID); got == nil {
		t.Fatal("cleanup preview must not delete the stale user")
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/deleted")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "当前只是预览") {
		t.Fatalf("/deleted alias should preview only, got %q", reply.Text)
	}
	if got, _ := repos.User.FindByID(ctx, stale.ID); got == nil {
		t.Fatal("/deleted preview alias must not delete users")
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup run confirm")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已清理 <b>1</b>") {
		t.Fatalf("cleanup confirm should delete exactly one stale user, got %q", reply.Text)
	}
	if got, _ := repos.User.FindByID(ctx, stale.ID); got != nil {
		t.Fatal("stale user should be deleted after explicit confirmation")
	}
	for _, user := range []*model.User{recent, newUser, admin} {
		if got, _ := repos.User.FindByID(ctx, user.ID); got == nil {
			t.Fatalf("%s should be kept by保号 rules/protection", user.Username)
		}
	}
}

func TestBotCleanupLegacyCountModeStillKeepsSingleMatchedRule(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)
	recent := &model.User{Username: "recent", PasswordHash: "x", Role: "user", IsActive: true}
	recent.CreatedAt = old
	recent.LastLoginAt = &now
	stale := &model.User{Username: "stale", PasswordHash: "x", Role: "user", IsActive: true}
	stale.CreatedAt = old
	stale.LastLoginAt = &old
	for _, user := range []*model.User{recent, stale} {
		if err := repos.User.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupKeepMode, "count"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupRequiredCount, "2"); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupRules, `[
		{"id":"login_7d","name":"最近登录","type":"recent_login","enabled":true,"window_days_max":7},
		{"id":"new_7d","name":"新号宽限","type":"account_age_grace","enabled":true,"min_count":7}
	]`); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "root"}, Chat: TelegramChat{ID: 9001, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/cleanup run")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply.Text, "recent") {
		t.Fatalf("user matching one keep rule must not be a cleanup candidate, got %q", reply.Text)
	}
	if !strings.Contains(reply.Text, "stale") {
		t.Fatalf("user matching no keep rules should be a candidate, got %q", reply.Text)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/cleanup run confirm")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := repos.User.FindByID(ctx, recent.ID); got == nil {
		t.Fatal("legacy count mode must not delete a user matching one keep rule")
	}
	if got, _ := repos.User.FindByID(ctx, stale.ID); got != nil {
		t.Fatalf("stale user should be deleted after confirm, reply=%q", reply.Text)
	}
}

func TestBotCleanupConfirmRequiresEnabledRules(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	user := &model.User{Username: "viewer", PasswordHash: "x", Role: "user", IsActive: true}
	user.CreatedAt = time.Now().Add(-30 * 24 * time.Hour)
	if err := repos.User.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "root"}, Chat: TelegramChat{ID: 9001, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/cleanup run confirm")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "没有启用的保号规则") {
		t.Fatalf("cleanup confirm without rules should be blocked, got %q", reply.Text)
	}
	if got, _ := repos.User.FindByID(ctx, user.ID); got == nil {
		t.Fatal("cleanup confirm without enabled rules must not delete users")
	}
}

func TestBotCleanupRuleListInfersDaysAndHidesDuplicateNames(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingAccountCleanupRules, `[
		{"id":"login_7d","name":"login_7d","type":"recent_login","enabled":true,"window_days_min":1,"window_days_max":5,"min_count":1},
		{"id":"new_7d","name":"new_7d","type":"account_age_grace","enabled":true,"window_days_min":1,"window_days_max":1,"min_count":1}
	]`); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9001, Username: "root"}, Chat: TelegramChat{ID: 9001, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/cleanup_rule")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"login_7d</code> · login_7d", "new_7d</code> · new_7d", "5 天内登录", "新号宽限 1 天", "add watch_hours", "Mgo 保号规则命令"} {
		if strings.Contains(reply.Text, bad) {
			t.Fatalf("rule list still contains bad fragment %q: %s", bad, reply.Text)
		}
	}
	for _, want := range []string{"login_7d", "7 天内登录", "new_7d", "新号宽限 7 天"} {
		if !strings.Contains(reply.Text, want) {
			t.Fatalf("rule list missing %q: %s", want, reply.Text)
		}
	}
}

func TestBotRegistrationCommandUsesOpenRegQuota(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := repos.Setting.Set(ctx, SettingOpenRegEnabled, "false"); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 9051,
		TelegramName:   "@root",
		ChatID:         9051,
		UserID:         admin.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9051"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9051, Username: "root"}, Chat: TelegramChat{ID: 9051, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/registration on 2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "2 个名额") {
		t.Fatalf("expected quota feedback, got %q", reply.Text)
	}
	capacity := bot.loadCapacity(ctx)
	if !capacity.OpenRegOn || capacity.OpenRegLimit != 2 || capacity.OpenRegUsed != 0 {
		t.Fatalf("registration command should open quota-aware registration, got %+v", capacity)
	}
}

func TestBotUserCommandsAndAdminGate(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	user := &model.User{Username: "viewer", PasswordHash: "x", Role: "user", IsActive: true}
	if err := repos.User.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 9101,
		TelegramName:   "@viewer",
		ChatID:         9101,
		UserID:         user.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repos.UserDevice.Create(ctx, &model.UserDevice{
		UserID: user.ID, DeviceID: "dev-1", DeviceName: "iPhone", Client: "Infuse", FirstSeenAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9101, Username: "viewer"}, Chat: TelegramChat{ID: 9101, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/antishare on")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "仅管理员") {
		t.Fatalf("regular user should not manage policy, got %q", reply.Text)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/devices")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "我的登录设备") {
		t.Fatalf("expected device list, got %q", reply.Text)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/kick 1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已踢下线") {
		t.Fatalf("expected kick feedback, got %q", reply.Text)
	}
	if kicked := bot.device; kicked != nil {
		t.Fatal("test should not require wired device service")
	}
	if ok := NewDeviceService(zap.NewNop(), repos).IsDeviceKicked(ctx, user.ID, "dev-1"); !ok {
		t.Fatal("device should be marked kicked")
	}
}

func TestBotRedeemRegisterRequiresAllowedTelegramUser(t *testing.T) {
	ctx := context.Background()
	_, bot := newBotTestService(t)
	code, err := bot.generateCode(ctx, model.RegistrationCodeRegister, 30, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9001"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9201, Username: "outsider"}, Chat: TelegramChat{ID: 9201, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/redeem_register "+code.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "不在管理员配置") {
		t.Fatalf("outsider should not redeem register code, got %q", reply.Text)
	}

	channel.Config = `{"admin_user_ids":"9201"}`
	reply, err = bot.executeCommand(ctx, channel, msg, "/redeem_register "+code.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "兑换成功") {
		t.Fatalf("allowed user should redeem register code, got %q", reply.Text)
	}
	if binding := bot.telegramBinding(ctx, 9201); binding == nil {
		t.Fatal("redeemed account should be bound to telegram user")
	}
}

func TestBotRedeemRegisterCodeCreatesOnlyOneAccount(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	code, err := bot.generateCode(ctx, model.RegistrationCodeRegister, 30, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9201,9202"}`}

	first := &TelegramMessage{From: TelegramUser{ID: 9201, Username: "first"}, Chat: TelegramChat{ID: 9201, Type: "private"}}
	reply, err := bot.executeCommand(ctx, channel, first, "/redeem_register "+code.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "兑换成功") {
		t.Fatalf("first redeem should succeed, got %q", reply.Text)
	}

	second := &TelegramMessage{From: TelegramUser{ID: 9202, Username: "second"}, Chat: TelegramChat{ID: 9202, Type: "private"}}
	reply, err = bot.executeCommand(ctx, channel, second, "/redeem_register "+code.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "兑换码已被使用") && !strings.Contains(reply.Text, "兑换码刚刚被使用") {
		t.Fatalf("second redeem should be rejected as used, got %q", reply.Text)
	}
	var users int64
	if err := repos.DB.Model(&model.User{}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 1 {
		t.Fatalf("one register code must create exactly one user, got %d", users)
	}
	if binding := bot.telegramBinding(ctx, 9202); binding != nil {
		t.Fatal("second telegram user must not be bound by an already-used register code")
	}
}

func TestBotRegisterCommandAcceptsRegistrationCode(t *testing.T) {
	ctx := context.Background()
	_, bot := newBotTestService(t)
	code, err := bot.generateCode(ctx, model.RegistrationCodeRegister, 30, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9301"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9301, Username: "codeuser"}, Chat: TelegramChat{ID: 9301, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/register "+strings.ToLower(code.Code[:4])+"-"+strings.ToLower(code.Code[4:]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "兑换成功") {
		t.Fatalf("/register CODE should redeem registration code, got %q", reply.Text)
	}
	if binding := bot.telegramBinding(ctx, 9301); binding == nil {
		t.Fatal("register code should bind the newly created account")
	}
}

func TestBotPlainRegistrationCodeMessageRedeems(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	code, err := bot.generateCode(ctx, model.RegistrationCodeRegister, 30, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.NotifyChannel{
		Name:    "Telegram",
		Type:    "telegram",
		Enabled: true,
		Config:  `{"admin_user_ids":"9302"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	update, _ := json.Marshal(TelegramUpdate{
		UpdateID: 1,
		Message: &TelegramMessage{
			MessageID: 12,
			Text:      strings.ToLower(code.Code),
			From:      TelegramUser{ID: 9302, Username: "plaincode"},
			Chat:      TelegramChat{ID: 9302, Type: "private"},
		},
	})

	if err := bot.HandleWebhook(ctx, update); err != nil {
		t.Fatal(err)
	}
	if binding := bot.telegramBinding(ctx, 9302); binding == nil {
		t.Fatal("plain code private message should redeem and bind account")
	}
	var used model.RegistrationCode
	if err := repos.DB.Where("code = ?", code.Code).First(&used).Error; err != nil {
		t.Fatal(err)
	}
	if used.UsedAt == nil || used.UsedByUserID == "" {
		t.Fatal("plain code message should mark registration code as used")
	}
}

func TestBotAdminCodeAndUserCommands(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	user := &model.User{Username: "viewer", PasswordHash: "x", Role: "user", IsActive: true}
	if err := repos.User.Create(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err := repos.User.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.TelegramBinding{
		TelegramUserID: 9301,
		TelegramName:   "@root",
		ChatID:         9301,
		UserID:         admin.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9301"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9301, Username: "root"}, Chat: TelegramChat{ID: 9301, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/gencode renew 90 7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已生成续期码") {
		t.Fatalf("expected generated renew code, got %q", reply.Text)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/renew_user viewer 30")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "viewer") {
		t.Fatalf("renew command should return user actions, got %q", reply.Text)
	}
	updated, _ := repos.User.FindByID(ctx, user.ID)
	if updated.ExpiredAt == nil || updated.ExpiredAt.Before(time.Now()) {
		t.Fatalf("renew_user should set future expiry, got %v", updated.ExpiredAt)
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/delete_user viewer")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "需要确认") {
		t.Fatalf("delete without confirm should be rejected, got %q", reply.Text)
	}
}

func TestBotGroupMenuShowsAdminActionsOnlyForAdmins(t *testing.T) {
	ctx := context.Background()
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9301","group_chat_id":"-1001"}`}
	adminMsg := &TelegramMessage{From: TelegramUser{ID: 9301, Username: "admin"}, Chat: TelegramChat{ID: -1001, Type: "group"}}
	reply, err := bot.executeCommand(ctx, channel, adminMsg, "/menu")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "管理员入口") || len(reply.Buttons) == 0 {
		t.Fatalf("admin group menu should expose management actions, got %#v", reply)
	}

	userMsg := &TelegramMessage{From: TelegramUser{ID: 9302, Username: "user"}, Chat: TelegramChat{ID: -1001, Type: "group"}}
	reply, err = bot.executeCommand(ctx, channel, userMsg, "/menu")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(reply.Text, "管理员入口") {
		t.Fatalf("non-admin group menu must not expose management actions, got %#v", reply)
	}
}

func TestBotAdminUnbindMultipleUsers(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true}
	viewer := &model.User{Username: "viewer", PasswordHash: "x", Role: "user", IsActive: true}
	guest := &model.User{Username: "guest", PasswordHash: "x", Role: "user", IsActive: true}
	for _, user := range []*model.User{admin, viewer, guest} {
		if err := repos.User.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	bindings := []model.TelegramBinding{
		{TelegramUserID: 9401, TelegramName: "@root", ChatID: 9401, UserID: admin.ID},
		{TelegramUserID: 9402, TelegramName: "@viewer", ChatID: 9402, UserID: viewer.ID},
		{TelegramUserID: 9403, TelegramName: "@guest", ChatID: 9403, UserID: guest.ID},
	}
	for i := range bindings {
		if err := repos.DB.Create(&bindings[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9401"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9401, Username: "root"}, Chat: TelegramChat{ID: 9401, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/unbind viewer,guest missing root")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已解绑：<b>2</b>") || !strings.Contains(reply.Text, "root(管理员)") || !strings.Contains(reply.Text, "missing") {
		t.Fatalf("unexpected unbind reply: %q", reply.Text)
	}
	for _, user := range []*model.User{viewer, guest} {
		var count int64
		if err := repos.DB.Model(&model.TelegramBinding{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s binding count = %d, want 0", user.Username, count)
		}
	}
	if binding := bot.telegramBinding(ctx, 9401); binding == nil {
		t.Fatal("admin binding should be protected from /unbind by username")
	}
}

func TestBotAdminUnbindInactiveAndInvalidBindings(t *testing.T) {
	ctx := context.Background()
	repos, bot := newBotTestService(t)
	oldTime := time.Now().Add(-45 * 24 * time.Hour)
	recentTime := time.Now().Add(-2 * 24 * time.Hour)
	admin := &model.User{Username: "root", PasswordHash: "x", Role: "admin", IsActive: true, LastLoginAt: &oldTime}
	oldUser := &model.User{Username: "old", PasswordHash: "x", Role: "user", IsActive: true, LastLoginAt: &oldTime}
	recentUser := &model.User{Username: "recent", PasswordHash: "x", Role: "user", IsActive: true, LastLoginAt: &recentTime}
	for _, user := range []*model.User{admin, oldUser, recentUser} {
		if err := repos.User.Create(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	for _, binding := range []model.TelegramBinding{
		{TelegramUserID: 9501, TelegramName: "@root", ChatID: 9501, UserID: admin.ID},
		{TelegramUserID: 9502, TelegramName: "@old", ChatID: 9502, UserID: oldUser.ID},
		{TelegramUserID: 9503, TelegramName: "@recent", ChatID: 9503, UserID: recentUser.ID},
		{TelegramUserID: 9504, TelegramName: "@ghost", ChatID: 9504, UserID: "missing-user"},
	} {
		row := binding
		if err := repos.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"admin_user_ids":"9501"}`}
	msg := &TelegramMessage{From: TelegramUser{ID: 9501, Username: "root"}, Chat: TelegramChat{ID: 9501, Type: "private"}}

	reply, err := bot.executeCommand(ctx, channel, msg, "/unbind_inactive 30")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已解绑：<b>1</b>") || !strings.Contains(reply.Text, "old") {
		t.Fatalf("unexpected inactive unbind reply: %q", reply.Text)
	}
	if binding := bot.telegramBinding(ctx, 9502); binding != nil {
		t.Fatal("old user binding should be removed")
	}
	if binding := bot.telegramBinding(ctx, 9501); binding == nil {
		t.Fatal("admin binding should be skipped by inactive cleanup")
	}
	if binding := bot.telegramBinding(ctx, 9503); binding == nil {
		t.Fatal("recent user binding should remain")
	}

	reply, err = bot.executeCommand(ctx, channel, msg, "/unbind_duplicates")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.Text, "已解绑：<b>1</b>") || !strings.Contains(reply.Text, "tg:9504") {
		t.Fatalf("unexpected duplicate cleanup reply: %q", reply.Text)
	}
	if binding := bot.telegramBinding(ctx, 9504); binding != nil {
		t.Fatal("invalid binding should be removed")
	}
}

func TestTelegramMembershipChatIDsIncludesCommandChatID(t *testing.T) {
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"command_chat_id":"-100123"}`}
	got := bot.telegramMembershipChatIDs(channel)
	if len(got) != 1 || got[0] != "-100123" {
		t.Fatalf("telegramMembershipChatIDs() = %#v, want command_chat_id", got)
	}
}

func TestTelegramMembershipChatIDsDedupesGroupChannelAndCommandIDs(t *testing.T) {
	_, bot := newBotTestService(t)
	channel := &model.NotifyChannel{Name: "Telegram", Type: "telegram", Enabled: true, Config: `{"group_chat_id":"-100123","channel_chat_id":"-100124","command_chat_id":"-100123"}`}
	got := bot.telegramMembershipChatIDs(channel)
	if len(got) != 2 || got[0] != "-100123" || got[1] != "-100124" {
		t.Fatalf("telegramMembershipChatIDs() = %#v, want deduped ids", got)
	}
}
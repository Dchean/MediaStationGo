package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// DeviceService implements device/session tracking and the two decoupled
// enforcement policies requested by the operator:
//
//	① 防共享: too many concurrent playbacks / logged-in clients disables the
//	   account immediately; fingerprint mismatch is warning-based and disables
//	   the account after the configured warning threshold.
//	② Mgo 保号规则: admins define one or more keep rules; a sweep deletes
//	   accounts that do not satisfy the configured any/all/count rule set.
//
// Safeguards: admin / protected accounts are never auto disabled or deleted;
// a Telegram notification is sent before a destructive action; every policy
// defaults to OFF.
type DeviceService struct {
	log  *zap.Logger
	repo *repository.Container

	// notifyUser sends a Telegram message to the local user (resolved to their
	// Telegram binding). Wired by the bot service; nil disables notifications.
	notifyUser func(ctx context.Context, userID, text string)
}

// NewDeviceService constructs a DeviceService.
func NewDeviceService(log *zap.Logger, repo *repository.Container) *DeviceService {
	return &DeviceService{log: log, repo: repo}
}

// SetNotifier wires the per-user Telegram notification callback.
func (s *DeviceService) SetNotifier(fn func(ctx context.Context, userID, text string)) {
	s.notifyUser = fn
}

// fingerprint derives a stable short hash from the client + device name. A
// changed fingerprint for the same device id signals the session was cloned
// onto different hardware/software.
func fingerprint(client, deviceName string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(client)) + "|" + strings.ToLower(strings.TrimSpace(deviceName))))
	return hex.EncodeToString(sum[:])[:16]
}

// isProtected reports whether a user must never be auto disabled/deleted.
// Admins are always protected; the earliest admin (default admin) too.
func (s *DeviceService) isProtected(ctx context.Context, u *model.User) bool {
	return UserIsProtectedAccount(ctx, s.repo, u)
}

// RecordLogin records (or refreshes) a device session at authentication time
// and runs the logged-in-client + fingerprint anti-share checks. It is safe to
// call on every Emby/Jellyfin AuthenticateByName request.
func (s *DeviceService) RecordLogin(ctx context.Context, userID, deviceID, deviceName, client, ip string) {
	if userID == "" {
		return
	}
	if deviceID == "" {
		// Fall back to a fingerprint-derived id so headless clients still count.
		deviceID = "fp-" + fingerprint(client, deviceName)
	}
	fp := fingerprint(client, deviceName)
	now := time.Now()

	existing, _ := s.repo.UserDevice.Find(ctx, userID, deviceID)
	mismatch := false
	if existing == nil {
		_ = s.repo.UserDevice.Create(ctx, &model.UserDevice{
			UserID:      userID,
			DeviceID:    deviceID,
			DeviceName:  deviceName,
			Client:      client,
			Fingerprint: fp,
			LastIP:      ip,
			FirstSeenAt: now,
			LastSeenAt:  now,
		})
	} else {
		if existing.Fingerprint != "" && existing.Fingerprint != fp {
			mismatch = true
		}
		existing.DeviceName = deviceName
		existing.Client = client
		existing.Fingerprint = fp
		existing.LastIP = ip
		existing.LastSeenAt = now
		existing.Kicked = false
		_ = s.repo.UserDevice.Save(ctx, existing)
	}

	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AntiShareEnabled {
		return
	}

	if mismatch {
		s.registerFingerprintWarning(ctx, userID, fmt.Sprintf("设备指纹变更（设备：%s）", deviceLabel(deviceName, client)), cfg)
		return
	}
	since := now.Add(-time.Duration(cfg.ClientActiveDays) * 24 * time.Hour)
	if n, err := s.repo.UserDevice.CountActiveClients(ctx, userID, since); err == nil && int(n) > cfg.MaxLoggedClients {
		s.disableForPolicy(ctx, userID, fmt.Sprintf("同时登录客户端 %d 台，超过上限 %d 台", n, cfg.MaxLoggedClients))
	}
}

// RecordPlayback marks a device as actively playing and runs the concurrent
// playback anti-share check. Call from playback-progress reporting.
func (s *DeviceService) RecordPlayback(ctx context.Context, userID, deviceID, deviceName, client string) {
	if userID == "" {
		return
	}
	if deviceID == "" {
		deviceID = "fp-" + fingerprint(client, deviceName)
	}
	now := time.Now()
	existing, _ := s.repo.UserDevice.Find(ctx, userID, deviceID)
	if existing == nil {
		existing = &model.UserDevice{
			UserID:      userID,
			DeviceID:    deviceID,
			DeviceName:  deviceName,
			Client:      client,
			Fingerprint: fingerprint(client, deviceName),
			FirstSeenAt: now,
			LastSeenAt:  now,
		}
		existing.LastPlayAt = &now
		_ = s.repo.UserDevice.Create(ctx, existing)
	} else {
		existing.LastSeenAt = now
		existing.LastPlayAt = &now
		_ = s.repo.UserDevice.Save(ctx, existing)
	}

	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AntiShareEnabled {
		return
	}
	since := now.Add(-time.Duration(cfg.PlayWindowSeconds) * time.Second)
	if n, err := s.repo.UserDevice.CountConcurrentPlaying(ctx, userID, since); err == nil && int(n) > cfg.MaxConcurrentPlay {
		s.disableForPolicy(ctx, userID, fmt.Sprintf("同时播放设备 %d 台，超过上限 %d 台", n, cfg.MaxConcurrentPlay))
	}
}

// registerFingerprintWarning increments the fingerprint warning counter for a
// user and disables the account once warnings exceed the threshold. Violations
// are debounced so a single burst counts at most once per minute.
func (s *DeviceService) registerFingerprintWarning(ctx context.Context, userID, reason string, cfg botConfig) {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil || u == nil {
		return
	}
	if s.isProtected(ctx, u) {
		s.log.Info("anti-share: skipping protected account", zap.String("user", u.Username), zap.String("reason", reason))
		return
	}
	now := time.Now()
	if u.LastShareWarnAt != nil && now.Sub(*u.LastShareWarnAt) < time.Minute {
		return // debounce burst
	}

	warnings := u.ShareWarnings + 1
	if warnings > cfg.WarnThreshold {
		s.disableForPolicy(ctx, userID, fmt.Sprintf("多次设备指纹异常：%s", reason))
		s.log.Warn("anti-share: disabling account after fingerprint warnings",
			zap.String("user", u.Username), zap.Int("warnings", u.ShareWarnings), zap.String("reason", reason))
		return
	}
	_ = s.repo.User.UpdateFields(ctx, userID, map[string]any{
		"share_warnings":     warnings,
		"last_share_warn_at": &now,
	})
	left := cfg.WarnThreshold + 1 - warnings
	s.notify(ctx, userID, fmt.Sprintf("⚠️ 账号 <b>%s</b> 触发设备指纹警告：%s\n这是第 <b>%d</b> 次警告，再异常 <b>%d</b> 次将禁用账号。请使用 Bot 的「我的设备」踢下线异常设备。", u.Username, reason, warnings, left))
	s.log.Info("anti-share: warning issued", zap.String("user", u.Username), zap.Int("warnings", warnings), zap.String("reason", reason))
}

func (s *DeviceService) disableForPolicy(ctx context.Context, userID, reason string) {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil || u == nil || !u.IsActive {
		return
	}
	if s.isProtected(ctx, u) {
		s.log.Info("device policy: skipping protected account", zap.String("user", u.Username), zap.String("reason", reason))
		return
	}
	now := time.Now()
	_ = s.repo.User.UpdateFields(ctx, userID, map[string]any{
		"is_active":          false,
		"last_share_warn_at": &now,
	})
	_ = s.repo.UserDevice.SetKickedByUser(ctx, userID, true)
	s.notify(ctx, userID, fmt.Sprintf("⛔️ 账号 <b>%s</b> 因触发设备规则已被禁用：%s\n请联系管理员解除禁用，或通过「我的设备」踢下线多余设备后再申请恢复。", u.Username, reason))
	s.log.Warn("device policy: disabled account", zap.String("user", u.Username), zap.String("reason", reason))
}

// SweepInactiveUsers is kept for compatibility with the existing scheduler; it
// now delegates to the custom account-cleanup policy.
func (s *DeviceService) SweepInactiveUsers(ctx context.Context) (int, error) {
	return s.SweepAccountCleanup(ctx)
}

// SweepAccountCleanup runs the admin-defined account cleanup policy once.
// Users are kept when they satisfy enough enabled keep rules according to
// keep_mode: any / all / count. Users that do not meet the policy are deleted.
func (s *DeviceService) SweepAccountCleanup(ctx context.Context) (int, error) {
	cfg := loadBotConfig(ctx, s.repo)
	if !cfg.AccountCleanupEnabled {
		return 0, nil
	}
	users, err := s.repo.User.List(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	for i := range users {
		u := &users[i]
		if s.isProtected(ctx, u) || !u.IsActive {
			continue
		}
		keep, details := s.userMatchesCleanupPolicy(ctx, u, cfg)
		if keep {
			continue
		}
		s.notify(ctx, u.ID, fmt.Sprintf("⛔️ 账号 <b>%s</b> 未满足保号规则，已被清理。\n规则结果：%s\n如需恢复请联系管理员。", u.Username, details))
		s.log.Warn("account cleanup: deleting account", zap.String("user", u.Username), zap.String("details", details))
		_ = s.repo.UserDevice.DeleteByUser(ctx, u.ID)
		if err := s.repo.User.Delete(ctx, u.ID); err == nil {
			removed++
		}
	}
	return removed, nil
}

// KickDevice marks a device as kicked so the next request from it is rejected
// (the client must log in again). Returns the affected device for messaging.
func (s *DeviceService) KickDevice(ctx context.Context, userID, deviceID string) error {
	d, err := s.repo.UserDevice.Find(ctx, userID, deviceID)
	if err != nil {
		return err
	}
	if d == nil {
		return fmt.Errorf("device not found")
	}
	return s.repo.UserDevice.SetKicked(ctx, d.ID, true)
}

// KickAllDevices marks all devices for a user as kicked.
func (s *DeviceService) KickAllDevices(ctx context.Context, userID string) error {
	return s.repo.UserDevice.SetKickedByUser(ctx, userID, true)
}

// ListDevices returns the device sessions for a user.
func (s *DeviceService) ListDevices(ctx context.Context, userID string) ([]model.UserDevice, error) {
	return s.repo.UserDevice.ListByUser(ctx, userID)
}

// IsDeviceKicked reports whether a (user, device) pair was kicked and should be
// forced to re-authenticate.
func (s *DeviceService) IsDeviceKicked(ctx context.Context, userID, deviceID string) bool {
	if userID == "" || deviceID == "" {
		return false
	}
	d, err := s.repo.UserDevice.Find(ctx, userID, deviceID)
	return err == nil && d != nil && d.Kicked
}

func (s *DeviceService) notify(ctx context.Context, userID, text string) {
	if s.notifyUser != nil {
		s.notifyUser(ctx, userID, text)
	}
}

// randomWindowDays returns a random integer in [min,max]. The window is
// intentionally non-fixed per the operator's requirement ("随机触发").
func randomWindowDays(min, max int) int {
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	if max == min {
		return min
	}
	return min + secureRandomIntn(max-min+1)
}

func (s *DeviceService) userMatchesCleanupPolicy(ctx context.Context, u *model.User, cfg botConfig) (bool, string) {
	rules := make([]accountCleanupRule, 0, len(cfg.AccountCleanupRules))
	for _, r := range cfg.AccountCleanupRules {
		if r.Enabled {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return true, "无启用规则，跳过"
	}
	matches := 0
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		ok, detail := s.userMatchesCleanupRule(ctx, u, r)
		if ok {
			matches++
			parts = append(parts, "✅ "+detail)
		} else {
			parts = append(parts, "❌ "+detail)
		}
	}
	required := 1
	switch cfg.AccountCleanupKeepMode {
	case "all":
		required = len(rules)
	case "count":
		required = cfg.AccountCleanupRequiredCount
		if required > len(rules) {
			required = len(rules)
		}
	default:
		required = 1
	}
	return matches >= required, fmt.Sprintf("满足 %d/%d 条，需要 %d 条；%s", matches, len(rules), required, strings.Join(parts, "；"))
}

func (s *DeviceService) userMatchesCleanupRule(ctx context.Context, u *model.User, r accountCleanupRule) (bool, string) {
	switch r.Type {
	case "watch_hours":
		windowDays := randomWindowDays(r.WindowDaysMin, r.WindowDaysMax)
		since := time.Now().Add(-time.Duration(windowDays) * 24 * time.Hour)
		watched, _ := s.repo.UserDevice.WatchedMillisSince(ctx, u.ID, since)
		hours := float64(watched) / 3600000
		return hours >= r.MinHours, fmt.Sprintf("%s：近 %d 天观看 %.1f/%.1f 小时", r.Name, windowDays, hours, r.MinHours)
	case "recent_login":
		days := r.WindowDaysMax
		if days < 1 {
			days = r.WindowDaysMin
		}
		ok := u.LastLoginAt != nil && u.LastLoginAt.After(time.Now().Add(-time.Duration(days)*24*time.Hour))
		return ok, fmt.Sprintf("%s：%d 天内登录", r.Name, days)
	case "signin_streak":
		rec, _ := s.repo.SignIn.Get(ctx, u.ID)
		streak := 0
		if rec != nil {
			streak = rec.StreakDays
		}
		return streak >= r.MinCount, fmt.Sprintf("%s：连续签到 %d/%d 天", r.Name, streak, r.MinCount)
	case "account_age_grace":
		days := r.MinCount
		if days < 1 {
			days = r.WindowDaysMax
		}
		ok := u.CreatedAt.After(time.Now().Add(-time.Duration(days) * 24 * time.Hour))
		return ok, fmt.Sprintf("%s：新账号 %d 天宽限", r.Name, days)
	default:
		return false, r.Name + "：未知规则"
	}
}

func deviceLabel(name, client string) string {
	name = strings.TrimSpace(name)
	client = strings.TrimSpace(client)
	switch {
	case name != "" && client != "":
		return name + " / " + client
	case name != "":
		return name
	case client != "":
		return client
	default:
		return "未知设备"
	}
}

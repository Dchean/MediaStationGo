package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// Bot / 设备管控相关的设置键。全部存储在 settings 表，由管理员通过
// Telegram Bot 命令调整。带安全默认值：所有"自动删号"策略默认关闭。
const (
	// 开放注册（开注名额）。
	SettingOpenRegEnabled = "telegram.openreg_enabled" // 是否开放注册
	SettingOpenRegLimit   = "telegram.openreg_limit"   // 本轮开注名额上限（0=不限）
	SettingOpenRegUsed    = "telegram.openreg_used"    // 本轮已用名额

	// 防共享（警告制：并发播放 / 登录客户端 / 设备指纹）。
	SettingAntiShareEnabled  = "device.antishare_enabled"   // 总开关（默认关）
	SettingMaxConcurrentPlay = "device.max_concurrent_play" // 最大并发播放设备
	SettingMaxLoggedClients  = "device.max_logged_clients"  // 最大同时登录客户端
	SettingWarnThreshold     = "device.warn_threshold"      // 警告几次后删号
	SettingPlayWindowSeconds = "device.play_window_seconds" // 并发播放判定窗口（秒）
	SettingClientActiveDays  = "device.client_active_days"  // 登录设备活跃天数窗口

	// 不活跃清理（独立开关）。
	SettingInactiveEnabled   = "device.inactive_enabled"         // 总开关（默认关）
	SettingInactiveMinHours  = "device.inactive_min_hours"       // 窗口内最低观看小时
	SettingInactiveWindowMin = "device.inactive_window_days_min" // 随机窗口下限（天）
	SettingInactiveWindowMax = "device.inactive_window_days_max" // 随机窗口上限（天）
	SettingInactiveGraceDays = "device.inactive_grace_days"      // 新号宽限期（天）

	// 自定义删号/保号规则。规则默认关闭；开启后按 KeepMode 计算用户
	// 是否满足足够的保号条件，未满足才会删号。
	SettingAccountCleanupEnabled       = "device.account_cleanup_enabled"
	SettingAccountCleanupKeepMode      = "device.account_cleanup_keep_mode"      // any / all / count
	SettingAccountCleanupRequiredCount = "device.account_cleanup_required_count" // keep_mode=count 时需要满足几条
	SettingAccountCleanupRules         = "device.account_cleanup_rules"          // JSON []accountCleanupRule
)

// botConfig 是设备管控的已解析配置（含默认值）。
type botConfig struct {
	AntiShareEnabled  bool
	MaxConcurrentPlay int
	MaxLoggedClients  int
	WarnThreshold     int
	PlayWindowSeconds int
	ClientActiveDays  int

	InactiveEnabled   bool
	InactiveMinHours  int
	InactiveWindowMin int
	InactiveWindowMax int
	InactiveGraceDays int

	AccountCleanupEnabled       bool
	AccountCleanupKeepMode      string
	AccountCleanupRequiredCount int
	AccountCleanupRules         []accountCleanupRule
}

// accountCleanupRule is one admin-defined "保号" condition. A user is deleted
// only when the cleanup policy is enabled and the user does not satisfy the
// configured combination of enabled keep rules.
//
// Supported types:
//   - watch_hours: watched hours in a random [min,max] day window >= MinHours
//   - recent_login: LastLoginAt is within WindowDaysMax days
//   - signin_streak: current sign-in streak >= MinCount
//   - account_age_grace: account age <= MinCount days (new-user grace)
type accountCleanupRule struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Enabled       bool    `json:"enabled"`
	WindowDaysMin int     `json:"window_days_min,omitempty"`
	WindowDaysMax int     `json:"window_days_max,omitempty"`
	MinHours      float64 `json:"min_hours,omitempty"`
	MinCount      int     `json:"min_count,omitempty"`
}

// defaultBotConfig returns the safe defaults requested by the operator.
func defaultBotConfig() botConfig {
	return botConfig{
		AntiShareEnabled:  false, // 自动删号默认关闭，需管理员显式开启
		MaxConcurrentPlay: 3,
		MaxLoggedClients:  3,
		WarnThreshold:     2, // 两次警告后再犯删号
		PlayWindowSeconds: 90,
		ClientActiveDays:  30,

		InactiveEnabled:   false, // 默认关闭
		InactiveMinHours:  6,
		InactiveWindowMin: 3,
		InactiveWindowMax: 5,
		InactiveGraceDays: 7,

		AccountCleanupEnabled:       false,
		AccountCleanupKeepMode:      "any",
		AccountCleanupRequiredCount: 1,
		AccountCleanupRules: []accountCleanupRule{
			{
				ID:            "watch_3_5d_6h",
				Name:          "3~5 天观看满 6 小时",
				Type:          "watch_hours",
				Enabled:       true,
				WindowDaysMin: 3,
				WindowDaysMax: 5,
				MinHours:      6,
			},
		},
	}
}

// loadBotConfig reads the device-management configuration from settings,
// falling back to safe defaults for any missing/invalid key.
func loadBotConfig(ctx context.Context, repo *repository.Container) botConfig {
	cfg := defaultBotConfig()
	get := func(key string) string {
		v, err := repo.Setting.Get(ctx, key)
		if err != nil {
			return ""
		}
		return v
	}
	cfg.AntiShareEnabled = parseBoolSetting(get(SettingAntiShareEnabled), cfg.AntiShareEnabled)
	cfg.InactiveEnabled = parseBoolSetting(get(SettingInactiveEnabled), cfg.InactiveEnabled)
	cfg.MaxConcurrentPlay = parseIntSettingDefault(get(SettingMaxConcurrentPlay), cfg.MaxConcurrentPlay)
	cfg.MaxLoggedClients = parseIntSettingDefault(get(SettingMaxLoggedClients), cfg.MaxLoggedClients)
	cfg.WarnThreshold = parseIntSettingDefault(get(SettingWarnThreshold), cfg.WarnThreshold)
	cfg.PlayWindowSeconds = parseIntSettingDefault(get(SettingPlayWindowSeconds), cfg.PlayWindowSeconds)
	cfg.ClientActiveDays = parseIntSettingDefault(get(SettingClientActiveDays), cfg.ClientActiveDays)
	cfg.InactiveMinHours = parseIntSettingDefault(get(SettingInactiveMinHours), cfg.InactiveMinHours)
	cfg.InactiveWindowMin = parseIntSettingDefault(get(SettingInactiveWindowMin), cfg.InactiveWindowMin)
	cfg.InactiveWindowMax = parseIntSettingDefault(get(SettingInactiveWindowMax), cfg.InactiveWindowMax)
	cfg.InactiveGraceDays = parseIntSettingDefault(get(SettingInactiveGraceDays), cfg.InactiveGraceDays)
	cfg.AccountCleanupEnabled = parseBoolSetting(get(SettingAccountCleanupEnabled), cfg.AccountCleanupEnabled)
	cfg.AccountCleanupKeepMode = normalizeCleanupKeepMode(get(SettingAccountCleanupKeepMode), cfg.AccountCleanupKeepMode)
	cfg.AccountCleanupRequiredCount = parseIntSettingDefault(get(SettingAccountCleanupRequiredCount), cfg.AccountCleanupRequiredCount)
	if raw := strings.TrimSpace(get(SettingAccountCleanupRules)); raw != "" {
		var rules []accountCleanupRule
		if err := json.Unmarshal([]byte(raw), &rules); err == nil && len(rules) > 0 {
			cfg.AccountCleanupRules = normalizeCleanupRules(rules)
		}
	}
	if cfg.InactiveWindowMax < cfg.InactiveWindowMin {
		cfg.InactiveWindowMax = cfg.InactiveWindowMin
	}
	if cfg.AccountCleanupRequiredCount < 1 {
		cfg.AccountCleanupRequiredCount = 1
	}
	return cfg
}

// parseIntSettingDefault parses an int setting, returning fallback on error.
func parseIntSettingDefault(value string, fallback int) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func normalizeCleanupKeepMode(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "any", "all", "count":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		if fallback == "" {
			return "any"
		}
		return fallback
	}
}

func normalizeCleanupRules(rules []accountCleanupRule) []accountCleanupRule {
	out := make([]accountCleanupRule, 0, len(rules))
	for _, r := range rules {
		r.Type = strings.ToLower(strings.TrimSpace(r.Type))
		r.ID = strings.TrimSpace(r.ID)
		r.Name = strings.TrimSpace(r.Name)
		if r.ID == "" {
			r.ID = r.Type
		}
		if r.Name == "" {
			r.Name = r.ID
		}
		if r.WindowDaysMin < 1 {
			r.WindowDaysMin = 1
		}
		if r.WindowDaysMax < r.WindowDaysMin {
			r.WindowDaysMax = r.WindowDaysMin
		}
		if r.MinCount < 0 {
			r.MinCount = 0
		}
		if r.MinHours < 0 {
			r.MinHours = 0
		}
		switch r.Type {
		case "watch_hours", "recent_login", "signin_streak", "account_age_grace":
			out = append(out, r)
		}
	}
	return out
}

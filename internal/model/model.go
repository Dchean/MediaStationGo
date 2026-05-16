// Package model 定义 GORM 数据模型和自动迁移使用的注册表。
// 每个子系统在 MediaStationGo 中拥有一个表切片；AllModels 返回联合以供 db.AutoMigrate 使用。
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base 嵌入每个域实体共享的字段:
//
//   - ID:         UUID v4 字符串主键
//   - CreatedAt / UpdatedAt: 由 GORM 管理
//   - DeletedAt:  软删除（查询自动过滤）
type Base struct {
	ID        string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate 如果调用者未提供则生成 UUID。
func (b *Base) BeforeCreate(_ *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

// User 是本地账户。第一个注册的管理员（或种子管理员）获得 "admin" 角色；
// 其他所有用户默认为 "user"。
type User struct {
	Base
	Username           string     `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash       string     `gorm:"size:128;not null" json:"-"`
	Role               string     `gorm:"size:16;not null;default:user" json:"role"`
	Tier               string     `gorm:"size:16;default:free" json:"tier"` // free / plus
	Nickname           string     `gorm:"size:128" json:"nickname,omitempty"`
	Email              string     `gorm:"size:128" json:"email,omitempty"`
	AvatarURL          string     `gorm:"size:255" json:"avatar_url,omitempty"`
	ForcePasswordReset bool       `gorm:"default:false" json:"force_password_reset"`
	IsActive           bool       `gorm:"default:true" json:"is_active"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
}

// Library 表示用户定义的媒体根目录。
type Library struct {
	Base
	Name    string `gorm:"size:128;not null" json:"name"`
	Path    string `gorm:"size:1024;not null" json:"path"`
	Type    string `gorm:"size:16;not null;default:movie" json:"type"` // movie / tv / anime / music
	Enabled bool   `gorm:"default:true" json:"enabled"`
}

// Media 是单个可播放项。剧集链接到 SeriesID；电影 SeriesID == ""。
type Media struct {
	Base
	LibraryID    string  `gorm:"index;size:36" json:"library_id"`
	SeriesID     string  `gorm:"index;size:36" json:"series_id,omitempty"`
	Title        string  `gorm:"size:255;not null" json:"title"`
	OriginalName string  `gorm:"size:255" json:"original_name,omitempty"`
	Path         string  `gorm:"uniqueIndex;size:1024;not null" json:"path"`
	SizeBytes    int64   `json:"size_bytes"`
	DurationSec  int     `json:"duration_sec"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	VideoCodec   string  `gorm:"size:32" json:"video_codec,omitempty"`
	AudioCodec   string  `gorm:"size:32" json:"audio_codec,omitempty"`
	Container    string  `gorm:"size:16" json:"container,omitempty"`
	PosterURL    string  `gorm:"size:1024" json:"poster_url,omitempty"`
	BackdropURL  string  `gorm:"size:1024" json:"backdrop_url,omitempty"`
	Overview     string  `gorm:"type:text" json:"overview,omitempty"`
	Rating       float32 `json:"rating"`
	Year         int     `json:"year"`
	SeasonNum    int     `json:"season_num"`
	EpisodeNum   int     `json:"episode_num"`
	ScrapeStatus string  `gorm:"size:16;default:pending" json:"scrape_status"`
	TMDbID       int     `json:"tmdb_id"`
	BangumiID    int     `json:"bangumi_id"`
	NSFW         bool    `gorm:"default:false" json:"nsfw"`

	// STRMURL is the indirection target for .strm files: when present the
	// stream handler redirects to it instead of opening the local file.
	// Used to expose WebDAV / Alist / S3 / HTTP direct links as media items.
	STRMURL string `gorm:"size:2048" json:"strm_url,omitempty"`

	// FileHash is a sparse-sample MD5 used for duplicate detection.
	// Computed on-demand by the duplicate finder; format: "<hex>-<size>".
	FileHash string `gorm:"index;size:64" json:"file_hash,omitempty"`

	// IsDuplicate flags this media as a duplicate of another media row.
	IsDuplicate bool   `gorm:"default:false" json:"is_duplicate"`
	DuplicateOf string `gorm:"size:36" json:"duplicate_of,omitempty"`
}

// APIConfig stores third-party data-source configuration. The api_key
// column is encrypted with AES-GCM (see internal/service/crypto.go) so an
// SQLite leak does not expose third-party credentials.
//
// Provider values mirror the original Python project:
//
//	tmdb        — themoviedb.org
//	bangumi     — bgm.tv
//	thetvdb     — thetvdb.com
//	fanart      — fanart.tv
//	douban      — douban.com (cookie)
//	openai      — OpenAI / DeepSeek / Qwen / Ollama (compatible)
type APIConfig struct {
	Base
	Provider    string `gorm:"uniqueIndex;size:32;not null" json:"provider"`
	APIKey      string `gorm:"type:text" json:"-"`              // ciphertext (never serialised)
	BaseURL     string `gorm:"size:512" json:"base_url,omitempty"`
	Extra       string `gorm:"type:text" json:"extra,omitempty"` // free-form JSON
	Enabled     bool   `gorm:"default:true" json:"enabled"`
	Description string `gorm:"size:255" json:"description,omitempty"`
}

// Series 将属于同一节目的剧集分组。
type Series struct {
	Base
	LibraryID   string  `gorm:"index;size:36" json:"library_id"`
	Title       string  `gorm:"size:255;not null" json:"title"`
	PosterURL   string  `gorm:"size:1024" json:"poster_url,omitempty"`
	BackdropURL string  `gorm:"size:1024" json:"backdrop_url,omitempty"`
	Overview    string  `gorm:"type:text" json:"overview,omitempty"`
	Rating      float32 `json:"rating"`
	Year        int     `json:"year"`
	TMDbID      int     `json:"tmdb_id"`
	BangumiID   int     `json:"bangumi_id"`
}

// PlaybackHistory 记录当前播放位置以支持续播。
type PlaybackHistory struct {
	Base
	UserID     string    `gorm:"index;size:36;not null" json:"user_id"`
	MediaID    string    `gorm:"index;size:36;not null" json:"media_id"`
	PositionMs int64     `json:"position_ms"`
	DurationMs int64     `json:"duration_ms"`
	WatchedAt  time.Time `json:"watched_at"`
	Completed  bool      `json:"completed"`
}

// Favorite 将媒体项标记为给定用户的收藏。
type Favorite struct {
	Base
	UserID  string `gorm:"index;size:36;not null;uniqueIndex:uniq_user_media" json:"user_id"`
	MediaID string `gorm:"index;size:36;not null;uniqueIndex:uniq_user_media" json:"media_id"`
}

// Playlist 是用户策划的、有序的媒体列表。
type Playlist struct {
	Base
	UserID   string `gorm:"index;size:36;not null" json:"user_id"`
	Name     string `gorm:"size:128;not null" json:"name"`
	IsPublic bool   `gorm:"default:false" json:"is_public"`
}

// PlaylistItem 是 Playlist 和 Media 的连接表，带有排序。
type PlaylistItem struct {
	Base
	PlaylistID string `gorm:"index;size:36;not null" json:"playlist_id"`
	MediaID    string `gorm:"index;size:36;not null" json:"media_id"`
	Position   int    `json:"position"`
}

// DownloadTask 是待处理（或已完成）的 torrent / HTTP 下载。
type DownloadTask struct {
	Base
	UserID   string  `gorm:"index;size:36" json:"user_id"`
	Source   string  `gorm:"size:32;not null" json:"source"` // qbittorrent / transmission / http
	URL      string  `gorm:"size:2048;not null" json:"url"`
	SavePath string  `gorm:"size:1024" json:"save_path"`
	Status   string  `gorm:"size:32;default:queued" json:"status"`
	Progress float32 `json:"progress"`
}

// Subscription 是自动化规则，轮询 RSS 源并将匹配种子排队到配置的下载客户端。
type Subscription struct {
	Base
	UserID    string     `gorm:"index;size:36" json:"user_id"`
	Name      string     `gorm:"size:128;not null" json:"name"`
	FeedURL   string     `gorm:"size:2048;not null" json:"feed_url"`
	Filter    string     `gorm:"size:512" json:"filter"`
	Enabled   bool       `gorm:"default:true" json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
}

// Setting 是单个键/值系统级偏好（供管理 UI 使用）。
type Setting struct {
	Key       string    `gorm:"primaryKey;size:128" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AccessLog 是结构化审计跟踪条目。存储在 SQLite 中供管理活动面板使用。
type AccessLog struct {
	Base
	UserID string `gorm:"index;size:36" json:"user_id"`
	Action string `gorm:"size:64;not null" json:"action"`
	Target string `gorm:"size:255" json:"target"`
	IP     string `gorm:"size:64" json:"ip"`
	Detail string `gorm:"type:text" json:"detail"`
}

// Site stores a PT/BT tracker site configuration used by the subscription
// and cross-site search system. Mirrors the original MediaStation sites table.
//
// Supported site types: nexusphp / gazelle / unit3d / mteam / custom_rss
// Supported auth types: cookie / api_key / authorization
type Site struct {
	Base
	Name       string `gorm:"size:128;not null" json:"name"`
	BaseURL    string `gorm:"size:512;not null" json:"base_url"`
	SiteType   string `gorm:"size:32;default:nexusphp" json:"site_type"`
	AuthType   string `gorm:"size:32;default:cookie" json:"auth_type"`
	Cookie     string `gorm:"type:text" json:"cookie,omitempty"`
	APIKey     string `gorm:"size:512" json:"api_key,omitempty"`
	AuthHeader string `gorm:"size:512" json:"auth_header,omitempty"`
	UserAgent  string `gorm:"size:512" json:"user_agent,omitempty"`
	RSSURL     string `gorm:"size:1024" json:"rss_url,omitempty"`
	Timeout    int    `gorm:"default:15" json:"timeout"`
	Priority   int    `gorm:"default:50" json:"priority"`
	UseProxy   bool   `gorm:"default:false" json:"use_proxy"`
	Enabled    bool   `gorm:"default:true" json:"enabled"`
	LoginStatus string `gorm:"size:20;default:unknown" json:"login_status"`
	Downloader  string `gorm:"size:50" json:"downloader,omitempty"`
}

// NotifyChannel is one named outbound notification destination.
//
// The Config column holds a JSON blob whose schema depends on the
// ChannelType (telegram/wechat/bark/webhook):
//
//	telegram → {bot_token, chat_id}
//	wechat   → {sendkey}
//	bark     → {device_key, server?}
//	webhook  → {url, method, headers (JSON string), body_template}
//
// The Events column is a JSON array of event-type strings the channel
// subscribes to; an empty array means "all events".
type NotifyChannel struct {
	Base
	Name        string `gorm:"size:128;not null" json:"name"`
	ChannelType string `gorm:"size:32;not null" json:"channel_type"`
	Config      string `gorm:"type:text;not null" json:"config"`
	Enabled     bool   `gorm:"default:true" json:"enabled"`
	Events      string `gorm:"type:text;default:'[]'" json:"events"`
}

// PlayProfile lets one user define multiple "viewing personas" with
// different content-rating limits, library access, and player defaults.
// The original Vue project sketched this out as a forward-looking
// feature; we materialise it server-side so the React port can fully
// function without dropping the screen.
//
// AllowedLibraryIDs is a JSON array of library UUIDs (empty = all).
type PlayProfile struct {
	Base
	UserID                string `gorm:"index;size:36;not null" json:"user_id"`
	Name                  string `gorm:"size:64;not null" json:"name"`
	IsDefault             bool   `gorm:"default:false" json:"is_default"`
	ContentRatingLimit    string `gorm:"size:16" json:"content_rating_limit,omitempty"`
	AllowAdult            bool   `gorm:"default:false" json:"allow_adult"`
	RequirePIN            bool   `gorm:"default:false" json:"require_pin"`
	PINHash               string `gorm:"size:128" json:"-"`
	PreferredSubtitleLang string `gorm:"size:16" json:"preferred_subtitle_lang,omitempty"`
	PreferredAudioLang    string `gorm:"size:16" json:"preferred_audio_lang,omitempty"`
	AutoplayNext          bool   `gorm:"default:true" json:"autoplay_next"`
	SkipIntro             bool   `gorm:"default:false" json:"skip_intro"`
	AllowedLibraryIDs     string `gorm:"type:text;default:'[]'" json:"allowed_library_ids"`
	TotalWatchTime        int64  `gorm:"default:0" json:"total_watch_time"`
	LastActiveAt          *time.Time `json:"last_active_at,omitempty"`
}

// UserPermission stores per-user feature toggles for the React UI's
// menu visibility + route guards. The original Python project surfaces
// 11 boolean flags; we mirror the same set so the existing frontend
// can swap to the Go API without code changes.
type UserPermission struct {
	UserID                 string    `gorm:"primaryKey;size:36" json:"user_id"`
	CanPlayMedia           bool      `gorm:"default:true" json:"can_play_media"`
	CanFavorite            bool      `gorm:"default:true" json:"can_favorite"`
	CanViewHistory         bool      `gorm:"default:true" json:"can_view_history"`
	CanViewDashboard       bool      `gorm:"default:true" json:"can_view_dashboard"`
	CanViewDiscover        bool      `gorm:"default:true" json:"can_view_discover"`
	CanManageDownloads     bool      `gorm:"default:false" json:"can_manage_downloads"`
	CanManageSubscriptions bool      `gorm:"default:false" json:"can_manage_subscriptions"`
	CanManageSites         bool      `gorm:"default:false" json:"can_manage_sites"`
	CanManageFiles         bool      `gorm:"default:false" json:"can_manage_files"`
	CanManageSTRM          bool      `gorm:"default:false" json:"can_manage_strm"`
	CanCast                bool      `gorm:"default:true" json:"can_cast"`
	CanUseAIAssistant      bool      `gorm:"default:false" json:"can_use_ai_assistant"`
	CanAccessSettings      bool      `gorm:"default:false" json:"can_access_settings"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// StorageConfig holds the connection settings for one external storage
// backend (Alist / S3 / WebDAV). Type column makes the row poly-typed
// — Config is a JSON blob whose shape is determined by Type.
//
//	alist  → {server, token}
//	s3     → {endpoint, region, bucket, access_key, secret_key, force_path_style}
//	webdav → {url, username, password}
type StorageConfig struct {
	Base
	Type      string `gorm:"uniqueIndex;size:16;not null" json:"type"`
	Config    string `gorm:"type:text;not null" json:"-"` // ciphertext
	Enabled   bool   `gorm:"default:true" json:"enabled"`
	LastError string `gorm:"size:512" json:"last_error,omitempty"`
}

// LicenseKey is one issued license for a customer. Activations live in
// a child table so a single key can bind to multiple devices when its
// MaxActivations > 1.
type LicenseKey struct {
	Base
	Key            string     `gorm:"uniqueIndex;size:64;not null" json:"key"`
	Customer       string     `gorm:"size:128" json:"customer,omitempty"`
	Plan           string     `gorm:"size:32;default:basic" json:"plan"`
	MaxActivations int        `gorm:"default:1" json:"max_activations"`
	IssuedAt       time.Time  `json:"issued_at"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	Revoked        bool       `gorm:"default:false" json:"revoked"`
	Notes          string     `gorm:"type:text" json:"notes,omitempty"`
}

// LicenseActivation is one (key, device) binding.
type LicenseActivation struct {
	Base
	KeyID      string     `gorm:"index;size:36;not null" json:"key_id"`
	DeviceID   string     `gorm:"size:128;not null" json:"device_id"`
	DeviceName string     `gorm:"size:128" json:"device_name,omitempty"`
	IP         string     `gorm:"size:64" json:"ip,omitempty"`
	UnboundAt  *time.Time `json:"unbound_at,omitempty"`
	HeartbeatAt *time.Time `json:"heartbeat_at,omitempty"`
}

// DownloadClient is one configured downloader (qBittorrent / Aria2 /
// Transmission). We keep the password column out of JSON so list calls
// don't leak secrets to the React UI.
type DownloadClient struct {
	Base
	Name     string `gorm:"size:128;not null" json:"name"`
	Type     string `gorm:"size:16;not null" json:"type"` // qbittorrent / transmission / aria2
	URL      string `gorm:"size:512;not null" json:"url"`
	Username string `gorm:"size:128" json:"username,omitempty"`
	Password string `gorm:"size:512" json:"-"`
	SavePath string `gorm:"size:1024" json:"save_path,omitempty"`
	IsDefault bool  `gorm:"default:false" json:"is_default"`
	Enabled  bool   `gorm:"default:true" json:"enabled"`
}

// AssistantSession groups a multi-turn chat with the AI assistant.
type AssistantSession struct {
	Base
	UserID string `gorm:"index;size:36;not null" json:"user_id"`
	Title  string `gorm:"size:255" json:"title,omitempty"`
}

// AssistantMessage is one entry in an AssistantSession transcript.
//
// Role is "user" | "assistant" | "system".  The optional OperationID
// links a message to an action the assistant proposed (so the UI can
// offer Undo).
type AssistantMessage struct {
	Base
	SessionID   string `gorm:"index;size:36;not null" json:"session_id"`
	Role        string `gorm:"size:16;not null" json:"role"`
	Content     string `gorm:"type:text;not null" json:"content"`
	OperationID string `gorm:"size:36" json:"operation_id,omitempty"`
}

// AllModels returns the slice consumed by gorm.AutoMigrate.
func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&Library{},
		&Series{},
		&Media{},
		&PlaybackHistory{},
		&Favorite{},
		&Playlist{},
		&PlaylistItem{},
		&DownloadTask{},
		&Subscription{},
		&Setting{},
		&Site{},
		&AccessLog{},
		&APIConfig{},
		&UserPermission{},
		&RefreshToken{},
		&ApiConfig{},
		&DownloadClient{},
		&NotifyChannel{},
		&STRMRecord{},
		&PlayProfile{},
		&StorageConfig{},
		&LicenseKey{},
		&LicenseActivation{},
		&AssistantSession{},
		&AssistantMessage{},
	}
}

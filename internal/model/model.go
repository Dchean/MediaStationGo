// Package model defines GORM data models and the registry used by
// auto-migration. Each subsystem in MediaStationGo owns a slice of tables
// here; AllModels returns the union for db.AutoMigrate.
package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base captures the fields embedded in every domain entity:
//
//   - ID:         UUID v4 string primary key.
//   - CreatedAt / UpdatedAt: managed by GORM.
//   - DeletedAt:  soft-delete (queries auto-filter on it).
type Base struct {
	ID        string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate generates a UUID if the caller did not supply one.
func (b *Base) BeforeCreate(_ *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

// User is a local account. The first registered admin (or seeded admin)
// gains the "admin" role; everyone else defaults to "user".
type User struct {
	Base
	Username           string     `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash       string     `gorm:"size:128;not null" json:"-"`
	Role               string     `gorm:"size:16;not null;default:user" json:"role"`
	Email              string     `gorm:"size:128" json:"email,omitempty"`
	AvatarURL          string     `gorm:"size:255" json:"avatar_url,omitempty"`
	ForcePasswordReset bool       `gorm:"default:false" json:"force_password_reset"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
}

// Library represents a user-defined media root directory.
type Library struct {
	Base
	Name    string `gorm:"size:128;not null" json:"name"`
	Path    string `gorm:"size:1024;not null" json:"path"`
	Type    string `gorm:"size:16;not null;default:movie" json:"type"` // movie / tv / anime / music
	Enabled bool   `gorm:"default:true" json:"enabled"`
}

// Media is a single playable item. Series episodes link to a SeriesID; movies
// have SeriesID == "".
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

// Series groups episodes that belong to the same show.
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

// PlaybackHistory records the current playback position for resume support.
type PlaybackHistory struct {
	Base
	UserID     string    `gorm:"index;size:36;not null" json:"user_id"`
	MediaID    string    `gorm:"index;size:36;not null" json:"media_id"`
	PositionMs int64     `json:"position_ms"`
	DurationMs int64     `json:"duration_ms"`
	WatchedAt  time.Time `json:"watched_at"`
	Completed  bool      `json:"completed"`
}

// Favorite marks a media item as favourite for a given user.
type Favorite struct {
	Base
	UserID  string `gorm:"index;size:36;not null;uniqueIndex:uniq_user_media" json:"user_id"`
	MediaID string `gorm:"index;size:36;not null;uniqueIndex:uniq_user_media" json:"media_id"`
}

// Playlist is a user-curated, ordered list of media items.
type Playlist struct {
	Base
	UserID   string `gorm:"index;size:36;not null" json:"user_id"`
	Name     string `gorm:"size:128;not null" json:"name"`
	IsPublic bool   `gorm:"default:false" json:"is_public"`
}

// PlaylistItem is the join table between Playlists and Media with ordering.
type PlaylistItem struct {
	Base
	PlaylistID string `gorm:"index;size:36;not null" json:"playlist_id"`
	MediaID    string `gorm:"index;size:36;not null" json:"media_id"`
	Position   int    `json:"position"`
}

// DownloadTask is an outstanding (or completed) torrent / HTTP download.
type DownloadTask struct {
	Base
	UserID   string  `gorm:"index;size:36" json:"user_id"`
	Source   string  `gorm:"size:32;not null" json:"source"` // qbittorrent / transmission / http
	URL      string  `gorm:"size:2048;not null" json:"url"`
	SavePath string  `gorm:"size:1024" json:"save_path"`
	Status   string  `gorm:"size:32;default:queued" json:"status"`
	Progress float32 `json:"progress"`
}

// Subscription is an automation rule that polls an RSS feed and queues
// matching torrents into the configured download client.
type Subscription struct {
	Base
	UserID    string `gorm:"index;size:36" json:"user_id"`
	Name      string `gorm:"size:128;not null" json:"name"`
	FeedURL   string `gorm:"size:2048;not null" json:"feed_url"`
	Filter    string `gorm:"size:512" json:"filter"`
	Enabled   bool   `gorm:"default:true" json:"enabled"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
}

// Setting is a single key/value system-wide preference (used by the admin UI).
type Setting struct {
	Key       string    `gorm:"primaryKey;size:128" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AccessLog is a structured audit-trail entry. Stored in SQLite for the
// admin Activity panel.
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
	}
}

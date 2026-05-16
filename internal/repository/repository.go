// Package repository implements a thin GORM-based data-access layer over the
// types declared in internal/model. Each method takes a context.Context so we
// can plug in cancellation / tracing later.
//
// Repositories are intentionally narrow: they only know how to persist data,
// not how to interpret it. Domain logic lives in internal/service.
package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// Container is the registry of all repositories injected into services.
type Container struct {
	DB              *gorm.DB
	User            *UserRepository
	Library         *LibraryRepository
	Media           *MediaRepository
	Series          *SeriesRepository
	History         *HistoryRepository
	Favorite        *FavoriteRepository
	Playlist        *PlaylistRepository
	Download        *DownloadRepository
	Subscription    *SubscriptionRepository
	Setting         *SettingRepository
	Log             *AccessLogRepository
	NotifyChannel   *NotifyChannelRepository
	PlayProfile     *PlayProfileRepository
	Permission      *PermissionRepository
	StorageConfig   *StorageConfigRepository
	License         *LicenseRepository
	DownloadClient  *DownloadClientRepository
	Assistant       *AssistantRepository
}

// New wires every repository to a single *gorm.DB.
func New(db *gorm.DB) *Container {
	return &Container{
		DB:             db,
		User:           &UserRepository{db: db},
		Library:        &LibraryRepository{db: db},
		Media:          &MediaRepository{db: db},
		Series:         &SeriesRepository{db: db},
		History:        &HistoryRepository{db: db},
		Favorite:       &FavoriteRepository{db: db},
		Playlist:       &PlaylistRepository{db: db},
		Download:       &DownloadRepository{db: db},
		Subscription:   &SubscriptionRepository{db: db},
		Setting:        &SettingRepository{db: db},
		Log:            &AccessLogRepository{db: db},
		NotifyChannel:  &NotifyChannelRepository{db: db},
		PlayProfile:    &PlayProfileRepository{db: db},
		Permission:     &PermissionRepository{db: db},
		StorageConfig:  &StorageConfigRepository{db: db},
		License:        &LicenseRepository{db: db},
		DownloadClient: &DownloadClientRepository{db: db},
		Assistant:      &AssistantRepository{db: db},
	}
}

// ─── User ────────────────────────────────────────────────────────────────────

// UserRepository persists model.User records.
type UserRepository struct{ db *gorm.DB }

// Create inserts a new user. Caller must pre-hash the password.
func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

// FindByUsername returns the user matching username, or (nil, nil) when absent.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID returns the user with the matching primary key, or (nil, nil).
func (r *UserRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CountAdmins returns the number of users that hold the admin role.
func (r *UserRepository) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("role = ?", "admin").Count(&n).Error
	return n, err
}

// List returns all users ordered by creation time desc.
func (r *UserRepository) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&users).Error
	return users, err
}

// UpdatePassword sets a new password hash and clears ForcePasswordReset.
func (r *UserRepository) UpdatePassword(ctx context.Context, id, hash string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"password_hash": hash, "force_password_reset": false}).Error
}

// TouchLogin updates the last login timestamp.
func (r *UserRepository) TouchLogin(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("last_login_at", &now).Error
}

// Delete removes a user (soft-delete via gorm.DeletedAt).
func (r *UserRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.User{}, "id = ?", id).Error
}

// ─── Library ─────────────────────────────────────────────────────────────────

// LibraryRepository persists model.Library records.
type LibraryRepository struct{ db *gorm.DB }

// Create persists a new library row.
func (r *LibraryRepository) Create(ctx context.Context, l *model.Library) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// List returns all enabled+disabled libraries.
func (r *LibraryRepository) List(ctx context.Context) ([]model.Library, error) {
	var ls []model.Library
	err := r.db.WithContext(ctx).Order("created_at asc").Find(&ls).Error
	return ls, err
}

// FindByID returns the library, or (nil, nil) when missing.
func (r *LibraryRepository) FindByID(ctx context.Context, id string) (*model.Library, error) {
	var l model.Library
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&l).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Delete removes a library and (soft) cascades to its media via repository
// callers — we do not run CASCADE here to keep this method narrow.
func (r *LibraryRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.Library{}, "id = ?", id).Error
}

// ─── Media ───────────────────────────────────────────────────────────────────

// MediaRepository persists model.Media records.
type MediaRepository struct{ db *gorm.DB }

// Upsert inserts or updates a media row keyed by Path (unique index).
func (r *MediaRepository) Upsert(ctx context.Context, m *model.Media) error {
	return r.db.WithContext(ctx).Where("path = ?", m.Path).
		Assign(*m).FirstOrCreate(m).Error
}

// FindByID returns the media row or (nil, nil).
func (r *MediaRepository) FindByID(ctx context.Context, id string) (*model.Media, error) {
	var m model.Media
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByLibrary returns paginated media items for a library.
func (r *MediaRepository) ListByLibrary(ctx context.Context, libraryID string, offset, limit int) ([]model.Media, int64, error) {
	var items []model.Media
	var total int64
	q := r.db.WithContext(ctx).Model(&model.Media{}).Where("library_id = ?", libraryID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("created_at desc").Offset(offset).Limit(limit).Find(&items).Error
	return items, total, err
}

// Search runs a LIKE search against the title field. Empty query returns the
// most recently added items.
func (r *MediaRepository) Search(ctx context.Context, query string, limit int) ([]model.Media, error) {
	var items []model.Media
	q := r.db.WithContext(ctx).Model(&model.Media{}).Limit(limit)
	if query != "" {
		like := "%" + query + "%"
		q = q.Where("title LIKE ? OR original_name LIKE ?", like, like)
	}
	err := q.Order("created_at desc").Find(&items).Error
	return items, err
}

// DeleteByLibrary purges all media tied to a library.
func (r *MediaRepository) DeleteByLibrary(ctx context.Context, libraryID string) error {
	return r.db.WithContext(ctx).Where("library_id = ?", libraryID).Delete(&model.Media{}).Error
}

// ─── Series ──────────────────────────────────────────────────────────────────

// SeriesRepository persists model.Series records.
type SeriesRepository struct{ db *gorm.DB }

// FindByID returns the series or (nil, nil).
func (r *SeriesRepository) FindByID(ctx context.Context, id string) (*model.Series, error) {
	var s model.Series
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// List returns all series (ordered by title).
func (r *SeriesRepository) List(ctx context.Context) ([]model.Series, error) {
	var s []model.Series
	err := r.db.WithContext(ctx).Order("title asc").Find(&s).Error
	return s, err
}

// ─── Playback History ────────────────────────────────────────────────────────

// HistoryRepository persists model.PlaybackHistory entries. The application
// upserts on (UserID, MediaID) so resume always reads the latest position.
type HistoryRepository struct{ db *gorm.DB }

// Upsert atomically inserts/updates the resume position.
func (r *HistoryRepository) Upsert(ctx context.Context, h *model.PlaybackHistory) error {
	var existing model.PlaybackHistory
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND media_id = ?", h.UserID, h.MediaID).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Create(h).Error
	}
	if err != nil {
		return err
	}
	existing.PositionMs = h.PositionMs
	existing.DurationMs = h.DurationMs
	existing.WatchedAt = h.WatchedAt
	existing.Completed = h.Completed
	return r.db.WithContext(ctx).Save(&existing).Error
}

// ListByUser returns the most recent history rows for the user.
func (r *HistoryRepository) ListByUser(ctx context.Context, userID string, limit int) ([]model.PlaybackHistory, error) {
	var rows []model.PlaybackHistory
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("watched_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

// ─── Favorite ────────────────────────────────────────────────────────────────

// FavoriteRepository persists model.Favorite records.
type FavoriteRepository struct{ db *gorm.DB }

// Toggle flips the favourite flag for (user, media). Returns the new state.
func (r *FavoriteRepository) Toggle(ctx context.Context, userID, mediaID string) (bool, error) {
	var f model.Favorite
	err := r.db.WithContext(ctx).Where("user_id = ? AND media_id = ?", userID, mediaID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fav := model.Favorite{UserID: userID, MediaID: mediaID}
		return true, r.db.WithContext(ctx).Create(&fav).Error
	}
	if err != nil {
		return false, err
	}
	return false, r.db.WithContext(ctx).Delete(&f).Error
}

// ListByUser returns all favourite media IDs for a user.
func (r *FavoriteRepository) ListByUser(ctx context.Context, userID string) ([]model.Favorite, error) {
	var rows []model.Favorite
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error
	return rows, err
}

// ─── Playlist ────────────────────────────────────────────────────────────────

// PlaylistRepository persists model.Playlist + PlaylistItem.
type PlaylistRepository struct{ db *gorm.DB }

// Create inserts a new playlist.
func (r *PlaylistRepository) Create(ctx context.Context, p *model.Playlist) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// ListByUser returns playlists owned by a user.
func (r *PlaylistRepository) ListByUser(ctx context.Context, userID string) ([]model.Playlist, error) {
	var rows []model.Playlist
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ─── Download ────────────────────────────────────────────────────────────────

// DownloadRepository persists model.DownloadTask records.
type DownloadRepository struct{ db *gorm.DB }

// Create inserts a new download task.
func (r *DownloadRepository) Create(ctx context.Context, t *model.DownloadTask) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// List returns all download tasks (admin view).
func (r *DownloadRepository) List(ctx context.Context) ([]model.DownloadTask, error) {
	var rows []model.DownloadTask
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ─── Subscription ────────────────────────────────────────────────────────────

// SubscriptionRepository persists model.Subscription records.
type SubscriptionRepository struct{ db *gorm.DB }

// Create inserts a new subscription rule.
func (r *SubscriptionRepository) Create(ctx context.Context, s *model.Subscription) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// List returns all subscription rules.
func (r *SubscriptionRepository) List(ctx context.Context) ([]model.Subscription, error) {
	var rows []model.Subscription
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ─── Setting ─────────────────────────────────────────────────────────────────

// SettingRepository persists key/value preferences.
type SettingRepository struct{ db *gorm.DB }

// Get returns the value or empty string when absent.
func (r *SettingRepository) Get(ctx context.Context, key string) (string, error) {
	var s model.Setting
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return s.Value, err
}

// Set upserts a setting value.
func (r *SettingRepository) Set(ctx context.Context, key, value string) error {
	s := model.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Save(&s).Error
}

// All returns every key/value pair (used by the admin UI).
func (r *SettingRepository) All(ctx context.Context) ([]model.Setting, error) {
	var rows []model.Setting
	err := r.db.WithContext(ctx).Find(&rows).Error
	return rows, err
}

// ─── Access Log ──────────────────────────────────────────────────────────────

// AccessLogRepository persists model.AccessLog records.
type AccessLogRepository struct{ db *gorm.DB }

// Create inserts one structured audit-trail entry.
func (r *AccessLogRepository) Create(ctx context.Context, l *model.AccessLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

// Recent returns the latest access-log entries (admin Activity panel).
func (r *AccessLogRepository) Recent(ctx context.Context, limit int) ([]model.AccessLog, error) {
	var rows []model.AccessLog
	err := r.db.WithContext(ctx).Order("created_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}


// ─── Notify Channel ──────────────────────────────────────────────────────────

// NotifyChannelRepository persists model.NotifyChannel records.
type NotifyChannelRepository struct{ db *gorm.DB }

// Create inserts a new notify channel.
func (r *NotifyChannelRepository) Create(ctx context.Context, n *model.NotifyChannel) error {
	return r.db.WithContext(ctx).Create(n).Error
}

// FindByID returns the channel or (nil, nil).
func (r *NotifyChannelRepository) FindByID(ctx context.Context, id string) (*model.NotifyChannel, error) {
	var n model.NotifyChannel
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// List returns every channel ordered by creation time desc.
func (r *NotifyChannelRepository) List(ctx context.Context) ([]model.NotifyChannel, error) {
	var rows []model.NotifyChannel
	err := r.db.WithContext(ctx).Order("created_at desc").Find(&rows).Error
	return rows, err
}

// ListEnabled is the variant the dispatcher uses; honours Enabled flag.
func (r *NotifyChannelRepository) ListEnabled(ctx context.Context) ([]model.NotifyChannel, error) {
	var rows []model.NotifyChannel
	err := r.db.WithContext(ctx).Where("enabled = ?", true).
		Order("created_at desc").Find(&rows).Error
	return rows, err
}

// Update applies a partial patch addressed by ID. The map keys must use
// snake_case GORM column names.
func (r *NotifyChannelRepository) Update(ctx context.Context, id string, patch map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.NotifyChannel{}).
		Where("id = ?", id).Updates(patch).Error
}

// Delete soft-deletes a channel.
func (r *NotifyChannelRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.NotifyChannel{}, "id = ?", id).Error
}

// ─── Play Profile ────────────────────────────────────────────────────────────

// PlayProfileRepository persists model.PlayProfile records.
type PlayProfileRepository struct{ db *gorm.DB }

// Create inserts a new play profile.
func (r *PlayProfileRepository) Create(ctx context.Context, p *model.PlayProfile) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// FindByID returns the profile or (nil, nil).
func (r *PlayProfileRepository) FindByID(ctx context.Context, id string) (*model.PlayProfile, error) {
	var p model.PlayProfile
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListByUser returns every profile owned by a user.
func (r *PlayProfileRepository) ListByUser(ctx context.Context, userID string) ([]model.PlayProfile, error) {
	var rows []model.PlayProfile
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("is_default desc, created_at asc").Find(&rows).Error
	return rows, err
}

// List returns every profile across users (admin view).
func (r *PlayProfileRepository) List(ctx context.Context) ([]model.PlayProfile, error) {
	var rows []model.PlayProfile
	err := r.db.WithContext(ctx).
		Order("user_id asc, is_default desc, created_at asc").Find(&rows).Error
	return rows, err
}

// ClearDefaultsFor flips all is_default flags to false for the given
// user; called inside the same transaction that promotes a new default.
func (r *PlayProfileRepository) ClearDefaultsFor(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Model(&model.PlayProfile{}).
		Where("user_id = ?", userID).Update("is_default", false).Error
}

// Update applies a partial patch addressed by ID.
func (r *PlayProfileRepository) Update(ctx context.Context, id string, patch map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.PlayProfile{}).
		Where("id = ?", id).Updates(patch).Error
}

// Delete soft-deletes a profile.
func (r *PlayProfileRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.PlayProfile{}, "id = ?", id).Error
}


// ─── Permissions ─────────────────────────────────────────────────────────────

// PermissionRepository persists model.UserPermission.
type PermissionRepository struct{ db *gorm.DB }

// Get returns the row, or (nil, nil) when not yet seeded.
func (r *PermissionRepository) Get(ctx context.Context, userID string) (*model.UserPermission, error) {
	var p model.UserPermission
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Save upserts the row keyed by UserID.
func (r *PermissionRepository) Save(ctx context.Context, p *model.UserPermission) error {
	return r.db.WithContext(ctx).Save(p).Error
}

// Delete removes the row (used when a user is deleted).
func (r *PermissionRepository) Delete(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.UserPermission{}).Error
}

// ─── Storage Config ──────────────────────────────────────────────────────────

// StorageConfigRepository persists model.StorageConfig records (Alist / S3 / WebDAV).
type StorageConfigRepository struct{ db *gorm.DB }

// Get returns the config for the given type, or (nil, nil).
func (r *StorageConfigRepository) Get(ctx context.Context, kind string) (*model.StorageConfig, error) {
	var s model.StorageConfig
	err := r.db.WithContext(ctx).Where("type = ?", kind).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Upsert inserts or updates the row keyed by Type.
func (r *StorageConfigRepository) Upsert(ctx context.Context, s *model.StorageConfig) error {
	return r.db.WithContext(ctx).Where("type = ?", s.Type).
		Assign(map[string]any{
			"config":     s.Config,
			"enabled":    s.Enabled,
			"last_error": s.LastError,
		}).
		FirstOrCreate(s).Error
}

// List returns every storage config (admin overview).
func (r *StorageConfigRepository) List(ctx context.Context) ([]model.StorageConfig, error) {
	var rows []model.StorageConfig
	err := r.db.WithContext(ctx).Order("type asc").Find(&rows).Error
	return rows, err
}

// ─── License ─────────────────────────────────────────────────────────────────

// LicenseRepository persists model.LicenseKey and model.LicenseActivation.
type LicenseRepository struct{ db *gorm.DB }

// Create inserts a new license key.
func (r *LicenseRepository) Create(ctx context.Context, k *model.LicenseKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

// FindByKey returns the license key matching the value, or (nil, nil).
func (r *LicenseRepository) FindByKey(ctx context.Context, key string) (*model.LicenseKey, error) {
	var k model.LicenseKey
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// FindByID returns the license key by primary key.
func (r *LicenseRepository) FindByID(ctx context.Context, id string) (*model.LicenseKey, error) {
	var k model.LicenseKey
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// List returns every license key (admin view).
func (r *LicenseRepository) List(ctx context.Context) ([]model.LicenseKey, error) {
	var rows []model.LicenseKey
	err := r.db.WithContext(ctx).Order("issued_at desc").Find(&rows).Error
	return rows, err
}

// Update applies a partial patch (revoke / extend expiry, etc.).
func (r *LicenseRepository) Update(ctx context.Context, id string, patch map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.LicenseKey{}).
		Where("id = ?", id).Updates(patch).Error
}

// AddActivation creates an activation entry for a key.
func (r *LicenseRepository) AddActivation(ctx context.Context, a *model.LicenseActivation) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// CountActiveActivations counts non-unbound activations for a key.
func (r *LicenseRepository) CountActiveActivations(ctx context.Context, keyID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.LicenseActivation{}).
		Where("key_id = ? AND unbound_at IS NULL", keyID).
		Count(&n).Error
	return n, err
}

// ListActivations returns all activations for a key.
func (r *LicenseRepository) ListActivations(ctx context.Context, keyID string) ([]model.LicenseActivation, error) {
	var rows []model.LicenseActivation
	err := r.db.WithContext(ctx).Where("key_id = ?", keyID).Find(&rows).Error
	return rows, err
}

// UnbindActivation marks an activation as unbound (soft).
func (r *LicenseRepository) UnbindActivation(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LicenseActivation{}).
		Where("id = ?", id).Update("unbound_at", &now).Error
}

// TouchHeartbeat bumps the heartbeat_at column.
func (r *LicenseRepository) TouchHeartbeat(ctx context.Context, id string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&model.LicenseActivation{}).
		Where("id = ?", id).Update("heartbeat_at", &now).Error
}

// ─── Download Clients ────────────────────────────────────────────────────────

// DownloadClientRepository persists model.DownloadClient records.
type DownloadClientRepository struct{ db *gorm.DB }

// Create inserts a new client config.
func (r *DownloadClientRepository) Create(ctx context.Context, c *model.DownloadClient) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// FindByID returns one client by ID.
func (r *DownloadClientRepository) FindByID(ctx context.Context, id string) (*model.DownloadClient, error) {
	var c model.DownloadClient
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// List returns every configured client.
func (r *DownloadClientRepository) List(ctx context.Context) ([]model.DownloadClient, error) {
	var rows []model.DownloadClient
	err := r.db.WithContext(ctx).Order("created_at asc").Find(&rows).Error
	return rows, err
}

// Update applies a partial patch.
func (r *DownloadClientRepository) Update(ctx context.Context, id string, patch map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.DownloadClient{}).
		Where("id = ?", id).Updates(patch).Error
}

// Delete soft-deletes one client.
func (r *DownloadClientRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.DownloadClient{}, "id = ?", id).Error
}

// ─── Assistant ───────────────────────────────────────────────────────────────

// AssistantRepository persists AssistantSession + AssistantMessage rows.
type AssistantRepository struct{ db *gorm.DB }

// CreateSession inserts a new session.
func (r *AssistantRepository) CreateSession(ctx context.Context, s *model.AssistantSession) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// FindSession returns the session row + ownership.
func (r *AssistantRepository) FindSession(ctx context.Context, id string) (*model.AssistantSession, error) {
	var s model.AssistantSession
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSessions returns sessions for the user (or all for admins).
func (r *AssistantRepository) ListSessions(ctx context.Context, userID string) ([]model.AssistantSession, error) {
	var rows []model.AssistantSession
	q := r.db.WithContext(ctx).Order("updated_at desc")
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	err := q.Find(&rows).Error
	return rows, err
}

// DeleteSession removes one session and cascades to its messages.
func (r *AssistantRepository) DeleteSession(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Where("session_id = ?", id).Delete(&model.AssistantMessage{}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Delete(&model.AssistantSession{}, "id = ?", id).Error
}

// AppendMessage inserts a message in the given session.
func (r *AssistantRepository) AppendMessage(ctx context.Context, m *model.AssistantMessage) error {
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return err
	}
	// Bump the parent session's updated_at so list ordering reflects activity.
	return r.db.WithContext(ctx).Model(&model.AssistantSession{}).
		Where("id = ?", m.SessionID).Update("updated_at", time.Now()).Error
}

// ListMessages returns the transcript ordered by creation time.
func (r *AssistantRepository) ListMessages(ctx context.Context, sessionID string) ([]model.AssistantMessage, error) {
	var rows []model.AssistantMessage
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).
		Order("created_at asc").Find(&rows).Error
	return rows, err
}

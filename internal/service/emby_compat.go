// Package service — Emby/Jellyfin compatibility shim.
//
// EmbyService produces JSON envelopes shaped like the most-consumed
// Emby-API endpoints so existing players (Infuse / Yamby / Hills /
// Senplayer / Kodi NextPVR extension / iOS native clients) can talk to
// MediaStationGo without a custom plugin.
//
// The shim is read-mostly: items, images, playback are fully covered;
//播放进度上报 / 收藏切换 是写路径但走我们自己的 PlaybackHistory /
// Favorite 表，所以 Emby 客户端的"标记已看 / 收藏"也会反向同步到
// 我们自己的 React UI。
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// 用一个固定的 ServerId 字符串。Emby 客户端会缓存这个 id，第一次见到
// 该 id 后会把所有派生数据（cookie/收藏/历史）和它绑定。
const embyServerID = "mediastation-go-001"

// EmbyService produces Emby-shaped JSON.
type EmbyService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewEmbyService is the constructor.
func NewEmbyService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *EmbyService {
	return &EmbyService{cfg: cfg, log: log, repo: repo}
}

// ─── System ──────────────────────────────────────────────────────────────────

// SystemInfo returns the full Emby identity payload.
func (e *EmbyService) SystemInfo() map[string]any {
	return map[string]any{
		"Id":                    embyServerID,
		"ServerId":              embyServerID,
		"ServerName":            "MediaStationGo",
		"Version":               "10.8.13",
		"ProductName":           "Jellyfin Server",
		"OperatingSystem":       "Windows",
		"Architecture":          "X64",
		"LocalAddress":          "",
		"WanAddress":            "",
		"HasPendingRestart":     false,
		"IsShuttingDown":        false,
		"SupportsLibraryMonitor": true,
		"SupportsHttps":          false,
		"SupportsAutoDiscovery":  true,
		"HttpServerPortNumber":   e.cfg.App.Port,
		"HttpsPortNumber":        0,
		"PublishedServerUrl":     "",
		"WebSocketPortNumber":    e.cfg.App.Port,
		"CompletedInstallations": []any{},
		"CanSelfRestart":         false,
		"CanLaunchWebBrowser":    false,
		"CanRestart":             false,
	}
}

// SystemInfoPublic 是不需要认证的精简版（Emby Web 客户端登陆前会拉）。
func (e *EmbyService) SystemInfoPublic() map[string]any {
	return map[string]any{
		"Id":                     embyServerID,
		"ServerId":               embyServerID,
		"ServerName":             "MediaStationGo",
		"Version":                "10.8.13",
		"ProductName":            "Jellyfin Server",
		"OperatingSystem":        "Windows",
		"LocalAddress":           "",
		"WanAddress":             "",
		"HttpServerPortNumber":    e.cfg.App.Port,
		"HttpsPortNumber":         0,
		"SupportsHttps":           false,
		"SupportsAutoDiscovery":   true,
		"StartupWizardCompleted": true,
	}
}

// ─── Users ───────────────────────────────────────────────────────────────────

// ListUsers returns Emby-shaped users.
func (e *EmbyService) ListUsers(ctx context.Context) ([]map[string]any, error) {
	users, err := e.repo.User.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, e.userPayload(&u))
	}
	return out, nil
}

// FindUser 用 ID 查用户，用于 /Users/Me 与 /Users/{id}。
func (e *EmbyService) FindUser(ctx context.Context, id string) (map[string]any, error) {
	u, err := e.repo.User.FindByID(ctx, id)
	if err != nil || u == nil {
		return nil, err
	}
	return e.userPayload(u), nil
}

func (e *EmbyService) userPayload(u *model.User) map[string]any {
	return map[string]any{
		"Id":                        u.ID,
		"Name":                      u.Username,
		"ServerId":                  embyServerID,
		"ServerName":                "MediaStationGo",
		"HasPassword":               true,
		"HasConfiguredPassword":     true,
		"HasConfiguredEasyPassword": false,
		"EnableAutoLogin":           false,
		"LastLoginDate":             u.LastLoginAt,
		"LastActivityDate":          u.UpdatedAt,
		"Configuration": map[string]any{
			"PlayDefaultAudioTrack":         true,
			"DisplayCollectionsView":        true,
			"DisplayMissingEpisodes":        false,
			"SubtitleMode":                  "Default",
			"EnableNextEpisodeAutoPlay":     true,
			"AudioLanguagePreference":       "",
			"SubtitleLanguagePreference":    "",
		},
		"Policy": map[string]any{
			"IsAdministrator":              u.Role == "admin",
			"IsHidden":                     false,
			"IsDisabled":                   !u.IsActive,
			"EnableUserPreferenceAccess":   true,
			"EnableRemoteAccess":           true,
			"EnableMediaPlayback":          true,
			"EnableAudioPlaybackTranscoding": true,
			"EnableVideoPlaybackTranscoding": true,
			"EnablePlaybackRemuxing":       true,
			"EnableLiveTvAccess":           false,
			"EnableContentDownloading":     true,
			"EnableSyncTranscoding":        true,
			"EnableMediaConversion":        true,
			"EnableAllChannels":            true,
			"EnableAllFolders":             true,
			"EnableAllDevices":             true,
			"AuthenticationProviderId":     "Emby.Server.Implementations.LocalAuthenticationProvider",
			"PasswordResetProviderId":      "Emby.Server.Implementations.LocalPasswordResetProvider",
		},
	}
}

// ─── Views / MediaFolders ────────────────────────────────────────────────────

// Views 返回 Emby 中"虚拟根目录"——每个 library 一个条目。
func (e *EmbyService) Views(ctx context.Context) (map[string]any, error) {
	libs, err := e.repo.Library.List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(libs))
	for _, l := range libs {
		items = append(items, e.libraryAsView(&l))
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

func (e *EmbyService) libraryAsView(l *model.Library) map[string]any {
	collectionType := "movies"
	switch l.Type {
	case "tv":
		collectionType = "tvshows"
	case "anime":
		collectionType = "tvshows" // Emby 没有专门的 anime CollectionType
	case "music":
		collectionType = "music"
	}
	return map[string]any{
		"Id":             l.ID,
		"Name":           l.Name,
		"CollectionType": collectionType,
		"ServerId":       embyServerID,
		"Type":           "CollectionFolder",
		"IsFolder":       true,
		"ImageTags":      map[string]string{},
		"BackdropImageTags": []string{},
		"UserData": map[string]any{
			"PlaybackPositionTicks": 0,
			"PlayCount":             0,
			"IsFavorite":            false,
			"Played":                false,
			"UnplayedItemCount":     0,
		},
	}
}

// ─── Items ───────────────────────────────────────────────────────────────────

// ItemsParams 是 /Items 与 /Users/{uid}/Items 共用的查询参数。
type ItemsParams struct {
	UserID            string
	ParentID          string
	IDs               []string
	SearchTerm        string
	IncludeItemTypes  []string
	Recursive         bool
	SortBy            string
	SortOrder         string
	Limit             int
	StartIndex        int
}

// Items paginates media in Emby's flat shape.
func (e *EmbyService) Items(ctx context.Context, p ItemsParams) (map[string]any, error) {
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 50
	}
	if p.StartIndex < 0 {
		p.StartIndex = 0
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("deleted_at IS NULL")
	if p.ParentID != "" {
		// ParentID 既可能是 library_id 也可能是 series_id（剧集详情下钻）
		q = q.Where("library_id = ? OR series_id = ?", p.ParentID, p.ParentID)
	}
	if len(p.IDs) > 0 {
		q = q.Where("id IN ?", p.IDs)
	}
	if p.SearchTerm != "" {
		q = q.Where("title LIKE ?", "%"+p.SearchTerm+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}

	// 排序
	order := "created_at desc"
	switch strings.ToLower(p.SortBy) {
	case "sortname", "name":
		order = "title"
	case "premieredate", "productionyear":
		order = "year"
	case "datecreated":
		order = "created_at"
	case "communityrating":
		order = "rating"
	}
	if strings.EqualFold(p.SortOrder, "Descending") {
		if !strings.HasSuffix(order, " desc") {
			order = order + " desc"
		}
	}

	var rows []model.Media
	if err := q.Order(order).Offset(p.StartIndex).Limit(p.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}

	// User-data: 收藏 + 进度
	userFavs := map[string]bool{}
	userPos := map[string]int64{}
	if p.UserID != "" {
		var favs []model.Favorite
		_ = e.repo.DB.WithContext(ctx).Where("user_id = ?", p.UserID).Find(&favs).Error
		for _, f := range favs {
			userFavs[f.MediaID] = true
		}
		var hist []model.PlaybackHistory
		_ = e.repo.DB.WithContext(ctx).Where("user_id = ?", p.UserID).Find(&hist).Error
		for _, h := range hist {
			userPos[h.MediaID] = h.PositionMs
		}
	}

	items := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		items = append(items, e.itemPayload(&m, userFavs[m.ID], userPos[m.ID]))
	}
	return map[string]any{
		"Items":            items,
		"TotalRecordCount": total,
		"StartIndex":       p.StartIndex,
	}, nil
}

// Item 单条目详情。
func (e *EmbyService) Item(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	m, err := e.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	fav := false
	pos := int64(0)
	if userID != "" {
		var f model.Favorite
		ferr := e.repo.DB.WithContext(ctx).Where("user_id = ? AND media_id = ?", userID, mediaID).First(&f).Error
		if ferr == nil {
			fav = true
		}
		var h model.PlaybackHistory
		herr := e.repo.DB.WithContext(ctx).Where("user_id = ? AND media_id = ?", userID, mediaID).
			Order("watched_at desc").First(&h).Error
		if herr == nil {
			pos = h.PositionMs
		}
	}
	return e.itemPayload(m, fav, pos), nil
}

// LatestItems 最近添加，全库或指定库。
func (e *EmbyService) LatestItems(ctx context.Context, userID, parentID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("deleted_at IS NULL")
	if parentID != "" {
		q = q.Where("library_id = ?", parentID)
	}
	var rows []model.Media
	if err := q.Order("created_at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	favs := map[string]bool{}
	if userID != "" {
		var fr []model.Favorite
		_ = e.repo.DB.WithContext(ctx).Where("user_id = ?", userID).Find(&fr).Error
		for _, f := range fr {
			favs[f.MediaID] = true
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, e.itemPayload(&m, favs[m.ID], 0))
	}
	return out, nil
}

// ResumeItems 列出有未完成播放进度的媒体。
func (e *EmbyService) ResumeItems(ctx context.Context, userID string, limit int) (map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	type row struct {
		MediaID    string
		PositionMs int64
		DurationMs int64
	}
	var hist []model.PlaybackHistory
	if err := e.repo.DB.WithContext(ctx).
		Where("user_id = ? AND completed = ? AND position_ms > 0", userID, false).
		Order("watched_at desc").Limit(limit).Find(&hist).Error; err != nil {
		return nil, err
	}
	if len(hist) == 0 {
		return map[string]any{"Items": []any{}, "TotalRecordCount": 0}, nil
	}
	ids := make([]string, 0, len(hist))
	posByID := map[string]int64{}
	for _, h := range hist {
		ids = append(ids, h.MediaID)
		posByID[h.MediaID] = h.PositionMs
	}
	var medias []model.Media
	if err := e.repo.DB.WithContext(ctx).Where("id IN ?", ids).Find(&medias).Error; err != nil {
		return nil, err
	}
	// 维持时间倒序
	byID := map[string]*model.Media{}
	for i := range medias {
		byID[medias[i].ID] = &medias[i]
	}
	items := make([]map[string]any, 0, len(hist))
	for _, h := range hist {
		if m, ok := byID[h.MediaID]; ok {
			items = append(items, e.itemPayload(m, false, posByID[h.MediaID]))
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

func (e *EmbyService) itemPayload(m *model.Media, fav bool, posMs int64) map[string]any {
	itemType := "Movie"
	if m.SeasonNum > 0 || m.EpisodeNum > 0 {
		itemType = "Episode"
	}
	imageTags := map[string]string{}
	backdropTags := []string{}
	if m.PosterURL != "" {
		imageTags["Primary"] = m.ID
	}
	if m.BackdropURL != "" {
		backdropTags = append(backdropTags, m.ID+"-bd")
	}

	runTimeTicks := int64(m.DurationSec) * 10_000_000
	durationMs := int64(m.DurationSec) * 1000
	played := posMs > 0 && durationMs > 0 && posMs >= durationMs*9/10
	pct := 0.0
	if durationMs > 0 {
		pct = float64(posMs) / float64(durationMs) * 100
	}

	return map[string]any{
		"Id":                m.ID,
		"Name":              m.Title,
		"OriginalTitle":     m.OriginalName,
		"ServerId":          embyServerID,
		"Type":              itemType,
		"MediaType":         "Video",
		"IsFolder":          false,
		"ProductionYear":    m.Year,
		"ParentIndexNumber": m.SeasonNum,
		"IndexNumber":       m.EpisodeNum,
		"Overview":          m.Overview,
		"RunTimeTicks":      runTimeTicks,
		"CommunityRating":   m.Rating,
		"Container":         m.Container,
		"Width":             m.Width,
		"Height":            m.Height,
		"DateCreated":       m.CreatedAt,
		"Path":              m.Path,
		"ParentId":          m.LibraryID,
		"SeriesId":          m.SeriesID,
		"ImageTags":         imageTags,
		"BackdropImageTags": backdropTags,
		"Genres":            splitCSV(m.Genres),
		"ProviderIds": map[string]string{
			"Tmdb":    intToStr(m.TMDbID),
			"Bangumi": intToStr(m.BangumiID),
		},
		"UserData": map[string]any{
			"PlaybackPositionTicks": posMs * 10_000,
			"PlayCount":             0,
			"IsFavorite":            fav,
			"Played":                played,
			"PlayedPercentage":      pct,
		},
		"MediaSources": []map[string]any{e.mediaSource(m, true)},
	}
}

// ─── Playback ────────────────────────────────────────────────────────────────

// PlaybackInfo returns a PlaybackInfoResponse usable by Emby clients.
func (e *EmbyService) PlaybackInfo(ctx context.Context, mediaID string) (map[string]any, error) {
	m, err := e.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return nil, err
	}
	return map[string]any{
		"MediaSources":  []map[string]any{e.mediaSource(m, false)},
		"PlaySessionId": fmt.Sprintf("%s-%d", m.ID, time.Now().Unix()),
	}, nil
}

// mediaSource 是 /Items 与 /PlaybackInfo 共享的 MediaSource 结构。
//
// asEmbedded=true：嵌在 /Items 列表里，不包含完整 stream URL（避免暴露
// 直链给搜索接口）。/PlaybackInfo 走 false 路径，URL 完整指向
// /api/stream/{id}（Emby 客户端会自动 append ?api_key=token）。
func (e *EmbyService) mediaSource(m *model.Media, asEmbedded bool) map[string]any {
	src := map[string]any{
		"Id":                   m.ID,
		"Name":                 m.Title,
		"Path":                 m.Path,
		"Container":            m.Container,
		"Size":                 m.SizeBytes,
		"Protocol":             "Http",
		"Type":                 "Default",
		"IsRemote":             false,
		"SupportsTranscoding":  true,
		"SupportsDirectStream": true,
		"SupportsDirectPlay":   true,
		"SupportsProbing":      true,
		"RunTimeTicks":         int64(m.DurationSec) * 10_000_000,
		"MediaStreams":         e.mediaStreams(m),
	}
	if !asEmbedded {
		// 完整 URL，让 Infuse 直接 GET。Emby 客户端会自动加 ?api_key=token。
		src["DirectStreamUrl"] = "/api/stream/" + m.ID
	}
	if strings.TrimSpace(m.STRMURL) != "" {
		// STRM 重定向：客户端直接拉远端，跳过我们这一层。
		src["IsRemote"] = true
		src["DirectStreamUrl"] = m.STRMURL
		src["Path"] = m.STRMURL
	}
	return src
}

func (e *EmbyService) mediaStreams(m *model.Media) []map[string]any {
	streams := []map[string]any{}
	if m.VideoCodec != "" || m.Width > 0 {
		streams = append(streams, map[string]any{
			"Codec":       m.VideoCodec,
			"Type":        "Video",
			"Index":       0,
			"Width":       m.Width,
			"Height":      m.Height,
			"AspectRatio": "",
			"IsDefault":   true,
			"IsForced":    false,
			"IsExternal":  false,
			"DisplayTitle": fmt.Sprintf("%dx%d %s", m.Width, m.Height, m.VideoCodec),
		})
	}
	if m.AudioCodec != "" {
		streams = append(streams, map[string]any{
			"Codec":      m.AudioCodec,
			"Type":       "Audio",
			"Index":      1,
			"IsDefault":  true,
			"IsForced":   false,
			"IsExternal": false,
		})
	}
	return streams
}

// ─── 收藏 / 已看（Emby 客户端写路径） ──────────────────────────────────────

// SetFavorite 把 mediaID 标为 userID 的收藏。
func (e *EmbyService) SetFavorite(ctx context.Context, userID, mediaID string, favorite bool) error {
	if favorite {
		var f model.Favorite
		err := e.repo.DB.WithContext(ctx).
			Where("user_id = ? AND media_id = ?", userID, mediaID).First(&f).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return e.repo.DB.WithContext(ctx).Create(&model.Favorite{
				UserID: userID, MediaID: mediaID,
			}).Error
		}
		return err
	}
	return e.repo.DB.WithContext(ctx).
		Where("user_id = ? AND media_id = ?", userID, mediaID).
		Delete(&model.Favorite{}).Error
}

// MarkPlayed 把 mediaID 标为已看（写一个 100% 进度的 history 行）。
func (e *EmbyService) MarkPlayed(ctx context.Context, userID, mediaID string, played bool) error {
	if !played {
		return e.repo.DB.WithContext(ctx).
			Where("user_id = ? AND media_id = ?", userID, mediaID).
			Delete(&model.PlaybackHistory{}).Error
	}
	m, err := e.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return errors.New("media not found")
	}
	dur := int64(m.DurationSec) * 1000
	if dur <= 0 {
		dur = 1
	}
	return e.repo.History.Upsert(ctx, &model.PlaybackHistory{
		UserID:     userID,
		MediaID:    mediaID,
		PositionMs: dur,
		DurationMs: dur,
		WatchedAt:  time.Now(),
		Completed:  true,
	})
}

// RecordProgress 记录播放进度（来自 Emby 客户端的 /Sessions/Playing/Progress）。
func (e *EmbyService) RecordProgress(ctx context.Context, userID, mediaID string, positionTicks, runtimeTicks int64) error {
	pos := positionTicks / 10_000
	dur := runtimeTicks / 10_000
	if dur <= 0 {
		// runtimeTicks 缺失时回退到 media.DurationSec
		if m, _ := e.repo.Media.FindByID(ctx, mediaID); m != nil {
			dur = int64(m.DurationSec) * 1000
		}
	}
	completed := dur > 0 && pos >= dur*9/10
	return e.repo.History.Upsert(ctx, &model.PlaybackHistory{
		UserID:     userID,
		MediaID:    mediaID,
		PositionMs: pos,
		DurationMs: dur,
		WatchedAt:  time.Now(),
		Completed:  completed,
	})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func intToStr(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

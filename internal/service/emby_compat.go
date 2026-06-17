// Package service — Emby/Jellyfin compatibility shim.
//
// EmbyService produces JSON envelopes shaped like the most-consumed
// Emby-API endpoints so existing players (Infuse / Yamby / Hills /
// Senplayer / Kodi NextPVR extension / iOS native clients) can talk to
// MediaStationGo without a custom plugin.
//
// The shim is read-mostly: items, images, playback are fully covered;
// 播放进度上报 / 收藏切换 是写路径但走我们自己的 PlaybackHistory /
// Favorite 表，所以 Emby 客户端的"标记已看 / 收藏"也会反向同步到
// 我们自己的 React UI。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

// 用一个固定的 ServerId 字符串。Emby 客户端会缓存这个 id，第一次见到
// 该 id 后会把所有派生数据（cookie/收藏/历史）和它绑定。
const embyServerID = "mediastation-go-001"

// embyCompatVersion deliberately reports an Emby 4.x server. Official Emby
// clients reject Jellyfin-style 10.x identities as unsupported/too old during
// the login handshake, even when the API shape is compatible enough for us.
const embyCompatVersion = "4.8.10.0"

const (
	embyLocalAuthenticationProviderID = "Emby.Server.Implementations.LocalAuthenticationProvider" // #nosec G101 -- Emby provider identifier, not a credential.
	embyLocalPasswordResetProviderID  = "Emby.Server.Implementations.LocalPasswordResetProvider"  // #nosec G101 -- Emby provider identifier, not a credential.
)

// PlaybackDirectOnlySettingKey 控制「客户端直连解码」模式：开启后宿主机
// 不再提供转码，所有播放交给第三方客户端本地解码（direct play / 302 直链），
// 以释放宿主机 CPU 资源。
const PlaybackDirectOnlySettingKey = "playback.direct_only"

// EmbyService produces Emby-shaped JSON.
type EmbyService struct {
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	storage cloudPlaybackResolver
	probe   cloudPlaybackProber
	cache   *RuntimeCacheService

	virtualMu      sync.RWMutex
	virtualSeries  map[string]embySeriesCacheEntry
	virtualSeasons map[string]embySeasonCacheEntry
	virtualArtwork map[string]embyArtworkCacheEntry

	visibilityMu    sync.RWMutex
	visibilityCache map[string]embyVisibilityCacheEntry

	cloudProbeMu       sync.Mutex
	cloudProbeInFlight map[string]struct{}
}

type cloudPlaybackResolver interface {
	CloudResolve(ctx context.Context, typ, fileRef, clientUA string) (*cloud.DirectLink, error)
}

type cloudPlaybackProber interface {
	ProbeHTTP(ctx context.Context, rawURL string, headers map[string]string) (*ProbeResult, error)
}

// NewEmbyService is the constructor.
func NewEmbyService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *EmbyService {
	return &EmbyService{cfg: cfg, log: log, repo: repo}
}

func (e *EmbyService) SetRuntimeCache(cache *RuntimeCacheService) *EmbyService {
	if e != nil {
		e.cache = cache
	}
	return e
}

func (e *EmbyService) SetCloudProbe(storage cloudPlaybackResolver, probe cloudPlaybackProber) {
	if e == nil {
		return
	}
	e.storage = storage
	e.probe = probe
}

// ─── System ──────────────────────────────────────────────────────────────────

// SystemInfo returns the full Emby identity payload.
func (e *EmbyService) SystemInfo() map[string]any {
	return map[string]any{
		"Id":                     embyServerID,
		"ServerId":               embyServerID,
		"ServerName":             "MediaStationGo",
		"Version":                embyCompatVersion,
		"ServerVersion":          embyCompatVersion,
		"ProductName":            "Emby Server",
		"OperatingSystem":        "Windows",
		"Architecture":           "X64",
		"LocalAddress":           "",
		"WanAddress":             "",
		"HasPendingRestart":      false,
		"IsShuttingDown":         false,
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
		"Version":                embyCompatVersion,
		"ServerVersion":          embyCompatVersion,
		"ProductName":            "Emby Server",
		"OperatingSystem":        "Windows",
		"LocalAddress":           "",
		"WanAddress":             "",
		"HttpServerPortNumber":   e.cfg.App.Port,
		"HttpsPortNumber":        0,
		"SupportsHttps":          false,
		"SupportsAutoDiscovery":  true,
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
	canDownload := u.Role == "admin"
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
			"PlayDefaultAudioTrack":      true,
			"DisplayCollectionsView":     true,
			"DisplayMissingEpisodes":     false,
			"SubtitleMode":               "Default",
			"EnableNextEpisodeAutoPlay":  true,
			"AudioLanguagePreference":    "",
			"SubtitleLanguagePreference": "",
		},
		"Policy": map[string]any{
			"IsAdministrator":                u.Role == "admin",
			"IsHidden":                       false,
			"IsDisabled":                     !u.IsActive,
			"EnableUserPreferenceAccess":     true,
			"EnableRemoteAccess":             true,
			"EnableMediaPlayback":            true,
			"EnableAudioPlaybackTranscoding": true,
			"EnableVideoPlaybackTranscoding": true,
			"EnablePlaybackRemuxing":         true,
			"EnableLiveTvAccess":             false,
			"EnableContentDownloading":       canDownload,
			"EnableSyncTranscoding":          canDownload,
			"EnableMediaConversion":          canDownload,
			"EnableAllChannels":              true,
			"EnableAllFolders":               true,
			"EnableAllDevices":               true,
			"AuthenticationProviderId":       embyLocalAuthenticationProviderID,
			"PasswordResetProviderId":        embyLocalPasswordResetProviderID,
		},
	}
}

// ─── Views / MediaFolders ────────────────────────────────────────────────────

// Views 返回 Emby 中"虚拟根目录"——每个 library 一个条目。
func (e *EmbyService) Views(ctx context.Context, userID string) (map[string]any, error) {
	libs, err := e.repo.Library.List(ctx)
	if err != nil {
		return nil, err
	}
	libs = FilterDisplayCloudLibraries(ctx, e.repo, libs)
	visibility := e.mediaVisibility(ctx, userID)
	items := make([]map[string]any, 0, len(libs))
	for _, l := range libs {
		if !e.libraryVisibleFromCachedVisibility(l, visibility) {
			continue
		}
		items = append(items, e.libraryAsView(&l))
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0}, nil
}

func (e *EmbyService) libraryAsView(l *model.Library) map[string]any {
	collectionType := "movies"
	switch l.Type {
	case "tv":
		collectionType = "tvshows"
	case "anime":
		collectionType = "tvshows" // Emby 没有专门的 anime CollectionType
	case "variety":
		collectionType = "tvshows"
	case "music":
		collectionType = "music"
	}
	return map[string]any{
		"Id":                       l.ID,
		"Name":                     l.Name,
		"CollectionType":           collectionType,
		"ServerId":                 embyServerID,
		"Type":                     "CollectionFolder",
		"IsFolder":                 true,
		"Path":                     l.Path,
		"SortName":                 strings.ToLower(l.Name),
		"DateCreated":              l.CreatedAt.UTC().Format(time.RFC3339),
		"CanDelete":                false,
		"CanDownload":              false,
		"DisplayPreferencesId":     l.ID,
		"PrimaryImageItemId":       l.ID,
		"PrimaryImageAspectRatio":  1.7777777777777777,
		"RecursiveItemCount":       0,
		"ChildCount":               0,
		"SpecialFeatureCount":      0,
		"EnableMediaSourceDisplay": true,
		"PlayAccess":               "Full",
		"ExternalUrls":             []any{},
		"ProviderIds":              map[string]string{},
		"Genres":                   []string{},
		"Tags":                     []string{},
		"ImageTags":                map[string]string{},
		"BackdropImageTags":        []string{},
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
	UserID           string
	ParentID         string
	IDs              []string
	SearchTerm       string
	IncludeItemTypes []string
	Filters          []string
	Recursive        bool
	SortBy           string
	SortOrder        string
	Limit            int
	StartIndex       int
}

const (
	embyVirtualSeriesPrefix = "msgo-series-"
	embyVirtualSeasonPrefix = "msgo-season-"
	embyVirtualCacheTTL     = 10 * time.Minute
	embyVisibilityCacheTTL  = 30 * time.Second
	embySeriesGroupingLimit = 5000
)

var (
	embySeasonDirRE    = regexp.MustCompile(`(?i)^(season[\s._-]*\d+|s\d+|第\s*[0-9一二三四五六七八九十百零两]+\s*季)$`)
	embyYearSuffixRE   = regexp.MustCompile(`\s*[\(（\[]\d{4}[\)）\]]\s*$`)
	embyEpisodeTitleRE = regexp.MustCompile(`(?i)\s*[-_ ]*s\d{1,2}e\d{1,3}.*$`)
)

type embySeriesGroup struct {
	ID          string
	LibraryID   string
	Name        string
	PosterURL   string
	BackdropURL string
	Overview    string
	Rating      float32
	Year        int
	TMDbID      int
	BangumiID   int
	CreatedAt   time.Time
	Episodes    []model.Media
}

type embySeasonGroup struct {
	ID        string
	SeriesID  string
	LibraryID string
	Name      string
	SeasonNum int
	Series    embySeriesGroup
	Episodes  []model.Media
}

type embySeriesCacheEntry struct {
	group     embySeriesGroup
	expiresAt time.Time
}

type embySeasonCacheEntry struct {
	season    embySeasonGroup
	expiresAt time.Time
}

type embyArtworkCacheEntry struct {
	primary   string
	backdrop  string
	expiresAt time.Time
}

type embyVisibilityCacheEntry struct {
	visibility MediaVisibility
	expiresAt  time.Time
}

// Items paginates media in Emby's hierarchy. Episodic libraries are exposed as
// Series -> Season -> Episode so Infuse/Vidhub/SenPlayer stop treating every
// episode as a separate movie card.
func (e *EmbyService) Items(ctx context.Context, p ItemsParams) (map[string]any, error) {
	if p.Limit <= 0 || p.Limit > 500 {
		p.Limit = 50
	}
	if p.StartIndex < 0 {
		p.StartIndex = 0
	}
	if len(p.IncludeItemTypes) > 0 && !containsSupportedEmbyItemType(p.IncludeItemTypes) {
		return emptyItemsEnvelope(p.StartIndex), nil
	}

	if len(p.IDs) > 0 {
		items := make([]map[string]any, 0, len(p.IDs))
		for _, id := range p.IDs {
			item, err := e.Item(ctx, id, p.UserID)
			if err != nil {
				return nil, err
			}
			if item != nil {
				items = append(items, item)
			}
		}
		return map[string]any{"Items": items, "TotalRecordCount": len(items), "StartIndex": 0}, nil
	}

	if containsOnlyFolderItemTypes(p.IncludeItemTypes) {
		if p.ParentID == "" {
			return e.Views(ctx, p.UserID)
		}
		if episodic, err := e.libraryIsEpisodic(ctx, p.ParentID); err != nil {
			return nil, err
		} else if episodic {
			return e.seriesItemsForLibrary(ctx, p.ParentID, p)
		}
		return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": p.StartIndex}, nil
	}

	if p.ParentID == "" && p.SearchTerm == "" && !p.Recursive && len(p.IncludeItemTypes) == 0 && len(p.Filters) == 0 {
		return e.Views(ctx, p.UserID)
	}

	if season, ok, err := e.findSeasonGroup(ctx, p.ParentID, p.UserID); err != nil {
		return nil, err
	} else if ok {
		return e.episodeItems(ctx, season.Episodes, p)
	}

	if series, ok, err := e.findSeriesGroup(ctx, p.ParentID, p.UserID); err != nil {
		return nil, err
	} else if ok {
		if p.Recursive || containsItemType(p.IncludeItemTypes, "Episode") {
			return e.episodeItems(ctx, series.Episodes, p)
		}
		seasons := e.seasonsForSeries(series)
		items := make([]map[string]any, 0, len(seasons))
		for _, season := range pageSlice(seasons, p.StartIndex, p.Limit) {
			items = append(items, e.seasonPayload(season))
		}
		return map[string]any{"Items": items, "TotalRecordCount": len(seasons), "StartIndex": p.StartIndex}, nil
	}

	if p.ParentID != "" {
		if episodic, err := e.libraryIsEpisodic(ctx, p.ParentID); err != nil {
			return nil, err
		} else if episodic && !p.Recursive && !containsItemType(p.IncludeItemTypes, "Episode") {
			return e.seriesItemsForLibrary(ctx, p.ParentID, p)
		}
	}

	if containsItemType(p.IncludeItemTypes, "Series") && !containsItemType(p.IncludeItemTypes, "Episode") {
		return e.seriesItemsForLibrary(ctx, p.ParentID, p)
	}
	return e.mediaItems(ctx, p)
}

func (e *EmbyService) mediaItems(ctx context.Context, p ItemsParams) (map[string]any, error) {
	cacheKey := e.embyItemsCacheKey("items", p)
	var cached embyItemsCacheValue
	if e.cache != nil && e.cache.GetJSON(ctx, cacheKey, &cached) {
		return map[string]any{"Items": cached.Items, "TotalRecordCount": cached.TotalRecordCount, "StartIndex": cached.StartIndex}, nil
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{})
	q = e.applyUserMediaVisibility(ctx, q, p.UserID)
	if p.ParentID != "" {
		q = q.Where("library_id IN ? OR series_id = ?", e.mergedLibraryIDs(ctx, p.ParentID), p.ParentID)
	}
	if p.SearchTerm != "" {
		q = q.Where("title LIKE ? OR original_name LIKE ?", "%"+p.SearchTerm+"%", "%"+p.SearchTerm+"%")
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": int64(0), "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins("JOIN favorites ON favorites.media_id = media.id AND favorites.user_id = ? AND favorites.deleted_at IS NULL", p.UserID)
	}
	resumeFilter := containsEmbyFilter(p.Filters, "IsResumable")
	if resumeFilter {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": int64(0), "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins(`JOIN (
			SELECT media_id, MAX(watched_at) AS watched_at
			FROM playback_histories
			WHERE user_id = ? AND completed = ? AND position_ms > 0
			GROUP BY media_id
		) AS resume ON resume.media_id = media.id`, p.UserID, false)
	}
	filterBySeasonNumbers := true
	parentKnownNonEpisodic := false
	if p.ParentID != "" {
		if episodic, err := e.libraryIsEpisodic(ctx, p.ParentID); err == nil && !episodic {
			filterBySeasonNumbers = false
			parentKnownNonEpisodic = true
		}
	}
	if parentKnownNonEpisodic && containsItemType(p.IncludeItemTypes, "Episode") && !containsItemType(p.IncludeItemTypes, "Movie") {
		return emptyItemsEnvelope(p.StartIndex), nil
	}
	if filterBySeasonNumbers && containsItemType(p.IncludeItemTypes, "Movie") && !containsItemType(p.IncludeItemTypes, "Episode") {
		q = e.filterMovieItems(ctx, q)
	}
	if filterBySeasonNumbers && containsItemType(p.IncludeItemTypes, "Episode") && !containsItemType(p.IncludeItemTypes, "Movie") {
		q = e.filterEpisodeItems(ctx, q)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	order := "media.created_at desc"
	switch primarySupportedEmbySort(p.SortBy, resumeFilter) {
	case "sortname", "name":
		order = "media.title"
	case "premieredate", "productionyear":
		order = "media.year"
	case "datecreated":
		order = "media.created_at"
	case "dateplayed":
		order = "resume.watched_at"
	case "communityrating":
		order = "media.rating"
	}
	if strings.EqualFold(firstCSVValue(p.SortOrder), "Descending") {
		if !strings.HasSuffix(order, " desc") {
			order = order + " desc"
		}
	}

	fetchLimit := p.Limit
	fetchOffset := p.StartIndex
	if fetchLimit > 0 && e.shouldCollapseMediaVersions(ctx, p) {
		// Duplicates across merged local/cloud libraries collapse into one Emby
		// item with multiple MediaSources. Fetch a wider window so duplicates do
		// not consume the whole requested page.
		fetchOffset = 0
		fetchLimit = p.StartIndex + maxInt(p.Limit*4, p.Limit)
	}
	var rows []model.Media
	if err := q.Order(order).Offset(fetchOffset).Limit(fetchLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	if e.shouldCollapseMediaVersions(ctx, p) {
		rows = e.collapseMediaVersionRows(ctx, rows)
		rows = pageSlice(rows, p.StartIndex, p.Limit)
	}
	items, err := e.payloadsForMedia(ctx, rows, p.UserID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}
	if e.cache != nil {
		e.cache.SetJSON(ctx, cacheKey, embyItemsCacheValue{Items: items, TotalRecordCount: total, StartIndex: p.StartIndex}, time.Duration(e.mediaCacheTTLSeconds())*time.Second)
	}
	return out, nil
}

type embyItemsCacheValue struct {
	Items            []map[string]any `json:"items"`
	TotalRecordCount int64            `json:"total_record_count"`
	StartIndex       int              `json:"start_index"`
}

type embyLatestCacheValue struct {
	Items []map[string]any `json:"items"`
}

func (e *EmbyService) embyItemsCacheKey(kind string, p ItemsParams) string {
	includeTypes := append([]string(nil), p.IncludeItemTypes...)
	filters := append([]string(nil), p.Filters...)
	ids := append([]string(nil), p.IDs...)
	sort.Strings(includeTypes)
	sort.Strings(filters)
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		kind,
		p.UserID,
		p.ParentID,
		strings.Join(ids, ","),
		p.SearchTerm,
		strings.Join(includeTypes, ","),
		strings.Join(filters, ","),
		strconv.FormatBool(p.Recursive),
		p.SortBy,
		p.SortOrder,
		strconv.Itoa(p.StartIndex),
		strconv.Itoa(p.Limit),
	}, "|")))
	return "media:emby:" + hex.EncodeToString(sum[:])
}

func (e *EmbyService) embyLatestCacheKey(userID, parentID string, limit int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"latest", userID, parentID, strconv.Itoa(limit)}, "|")))
	return "media:emby:" + hex.EncodeToString(sum[:])
}

func (e *EmbyService) mediaCacheTTLSeconds() int {
	if e == nil || e.cfg == nil || e.cfg.Cache.MediaTTLSeconds < 1 {
		return 15
	}
	return e.cfg.Cache.MediaTTLSeconds
}

func (e *EmbyService) episodeItems(ctx context.Context, rows []model.Media, p ItemsParams) (map[string]any, error) {
	rows = e.filterMediaRowsForUser(ctx, rows, p.UserID)
	if p.SearchTerm != "" {
		filtered := rows[:0]
		needle := strings.ToLower(p.SearchTerm)
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Title), needle) || strings.Contains(strings.ToLower(row.OriginalName), needle) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].SeasonNum != rows[j].SeasonNum {
			return rows[i].SeasonNum < rows[j].SeasonNum
		}
		if rows[i].EpisodeNum != rows[j].EpisodeNum {
			return rows[i].EpisodeNum < rows[j].EpisodeNum
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
	total := len(rows)
	items, err := e.payloadsForMedia(ctx, pageSlice(rows, p.StartIndex, p.Limit), p.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

func (e *EmbyService) payloadsForMedia(ctx context.Context, rows []model.Media, userID string) ([]map[string]any, error) {
	rows = e.collapseMediaVersionRows(ctx, rows)
	userFavs := map[string]bool{}
	userPos := map[string]int64{}
	if userID != "" && len(rows) > 0 {
		mediaIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if strings.TrimSpace(row.ID) != "" {
				mediaIDs = append(mediaIDs, row.ID)
			}
		}
		if len(mediaIDs) == 0 {
			mediaIDs = []string{"__none__"}
		}
		var favs []model.Favorite
		favQuery := e.repo.DB.WithContext(ctx).Where("user_id = ?", userID).Where("media_id IN ?", mediaIDs)
		_ = favQuery.Find(&favs).Error
		for _, f := range favs {
			userFavs[f.MediaID] = true
		}
		var hist []model.PlaybackHistory
		histQuery := e.repo.DB.WithContext(ctx).Where("user_id = ?", userID).Where("media_id IN ?", mediaIDs)
		_ = histQuery.Find(&hist).Error
		for _, h := range hist {
			userPos[h.MediaID] = h.PositionMs
		}
	}

	items := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		items = append(items, e.itemPayload(ctx, &m, userFavs[m.ID], userPos[m.ID]))
	}
	return items, nil
}

func (e *EmbyService) shouldCollapseMediaVersions(ctx context.Context, p ItemsParams) bool {
	if containsItemType(p.IncludeItemTypes, "Series") || containsItemType(p.IncludeItemTypes, "Season") {
		return false
	}
	if containsItemType(p.IncludeItemTypes, "Episode") && !containsItemType(p.IncludeItemTypes, "Movie") {
		return true
	}
	if p.ParentID == "" {
		return true
	}
	episodic, err := e.libraryIsEpisodic(ctx, p.ParentID)
	return err == nil && !episodic
}

func (e *EmbyService) collapseMediaVersionRows(ctx context.Context, rows []model.Media) []model.Media {
	if len(rows) < 2 {
		return rows
	}
	out := make([]model.Media, 0, len(rows))
	indexByKey := make(map[string]int, len(rows))
	for _, row := range rows {
		key := e.mediaVersionKey(ctx, &row)
		if key == "" {
			out = append(out, row)
			continue
		}
		if idx, ok := indexByKey[key]; ok {
			if preferMediaVersion(row, out[idx]) {
				out[idx] = row
			}
			continue
		}
		indexByKey[key] = len(out)
		out = append(out, row)
	}
	return out
}

// Item 单条目详情。
func (e *EmbyService) Item(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	if lib, err := e.repo.Library.FindByID(ctx, mediaID); err != nil {
		return nil, err
	} else if lib != nil {
		libs := FilterDisplayCloudLibraries(ctx, e.repo, []model.Library{*lib})
		if len(libs) == 0 {
			return nil, nil
		}
		visibility := e.mediaVisibility(ctx, userID)
		if !e.libraryVisibleFromCachedVisibility(libs[0], visibility) {
			return nil, nil
		}
		return e.libraryAsView(&libs[0]), nil
	}
	if strings.HasPrefix(mediaID, embyVirtualSeasonPrefix) {
		if season, ok, err := e.findSeasonGroup(ctx, mediaID, userID); err != nil {
			return nil, err
		} else if ok {
			return e.seasonPayload(season), nil
		}
	}
	if strings.HasPrefix(mediaID, embyVirtualSeriesPrefix) {
		if series, ok, err := e.findSeriesGroup(ctx, mediaID, userID); err != nil {
			return nil, err
		} else if ok {
			return e.seriesPayload(series), nil
		}
	}
	m, err := e.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		if series, ok, err := e.findSeriesGroup(ctx, mediaID, userID); err != nil {
			return nil, err
		} else if ok {
			return e.seriesPayload(series), nil
		}
		return nil, nil
	}
	if !UserDefaultMediaVisibility(ctx, e.repo, userID).Allows(m) {
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
	return e.itemPayload(ctx, m, fav, pos), nil
}

// LatestItems 最近添加，全库或指定库。
func (e *EmbyService) LatestItems(ctx context.Context, userID, parentID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	cacheKey := e.embyLatestCacheKey(userID, parentID, limit)
	var cached embyLatestCacheValue
	if e.cache != nil && e.cache.GetJSON(ctx, cacheKey, &cached) {
		return cached.Items, nil
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("deleted_at IS NULL")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	if parentID != "" {
		if episodic, err := e.libraryIsEpisodic(ctx, parentID); err == nil && episodic {
			out, err := e.latestSeriesItemsForLibrary(ctx, userID, parentID, limit)
			if err == nil && e.cache != nil {
				e.cache.SetJSON(ctx, cacheKey, embyLatestCacheValue{Items: out}, time.Duration(e.mediaCacheTTLSeconds())*time.Second)
			}
			return out, err
		}
		q = q.Where("library_id IN ?", e.mergedLibraryIDs(ctx, parentID))
	}
	var rows []model.Media
	if err := q.Order("media.created_at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	favs := map[string]bool{}
	if userID != "" && len(rows) > 0 {
		mediaIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if strings.TrimSpace(row.ID) != "" {
				mediaIDs = append(mediaIDs, row.ID)
			}
		}
		if len(mediaIDs) == 0 {
			mediaIDs = []string{"__none__"}
		}
		var fr []model.Favorite
		_ = e.repo.DB.WithContext(ctx).Where("user_id = ? AND media_id IN ?", userID, mediaIDs).Find(&fr).Error
		for _, f := range fr {
			favs[f.MediaID] = true
		}
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, e.itemPayload(ctx, &m, favs[m.ID], 0))
	}
	if e.cache != nil {
		e.cache.SetJSON(ctx, cacheKey, embyLatestCacheValue{Items: out}, time.Duration(e.mediaCacheTTLSeconds())*time.Second)
	}
	return out, nil
}

func (e *EmbyService) latestSeriesItemsForLibrary(ctx context.Context, userID, libraryID string, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rowLimit := limit * 40
	if rowLimit < 200 {
		rowLimit = 200
	}
	if rowLimit > embySeriesGroupingLimit {
		rowLimit = embySeriesGroupingLimit
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("library_id IN ? AND (season_num > 0 OR episode_num > 0)", e.mergedLibraryIDs(ctx, libraryID))
	q = e.applyUserMediaVisibility(ctx, q, userID)
	var rows []model.Media
	if err := q.Order("media.created_at desc").Limit(rowLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	groups := e.seriesGroupsFromMedia(rows)
	sortSeriesGroups(groups, ItemsParams{SortBy: "datecreated", SortOrder: "Descending"})
	if len(groups) > limit {
		groups = groups[:limit]
	}
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, e.seriesPayload(group))
	}
	return items, nil
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
	q := e.repo.DB.WithContext(ctx).Where("id IN ?", ids)
	q = e.applyUserMediaVisibility(ctx, q, userID)
	if err := q.Find(&medias).Error; err != nil {
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
			items = append(items, e.itemPayload(ctx, m, false, posByID[h.MediaID]))
		}
	}
	return map[string]any{"Items": items, "TotalRecordCount": len(items)}, nil
}

func (e *EmbyService) itemPayload(ctx context.Context, m *model.Media, fav bool, posMs int64) map[string]any {
	itemType := "Movie"
	name := m.Title
	parentID := m.LibraryID
	seriesID := m.SeriesID
	seriesName := ""
	seasonID := ""
	if e.mediaBelongsToEpisodicLibrary(ctx, m) && (m.SeasonNum > 0 || m.EpisodeNum > 0) {
		itemType = "Episode"
		seriesID = e.seriesIDForMedia(m)
		seriesName = e.seriesNameForMedia(m)
		seasonID = e.seasonIDForMedia(m)
		parentID = seasonID
		originalName := strings.TrimSpace(m.OriginalName)
		if originalName != "" && !strings.EqualFold(originalName, seriesName) && !strings.EqualFold(originalName, m.Title) {
			name = m.OriginalName
		} else if m.EpisodeNum > 0 {
			name = fmt.Sprintf("第 %d 集", m.EpisodeNum)
		}
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
		"Name":              name,
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
		"ParentId":          parentID,
		"SeasonId":          seasonID,
		"SeasonName":        seasonName(m.SeasonNum),
		"SeriesId":          seriesID,
		"SeriesName":        seriesName,
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
		"MediaSources": e.mediaSourcesForItem(ctx, m, true, false),
	}
}

func (e *EmbyService) seriesItemsForLibrary(ctx context.Context, libraryID string, p ItemsParams) (map[string]any, error) {
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("season_num > 0 OR episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, p.UserID)
	if libraryID != "" {
		q = q.Where("library_id IN ?", e.mergedLibraryIDs(ctx, libraryID))
	}
	if p.SearchTerm != "" {
		q = q.Where("title LIKE ? OR original_name LIKE ?", "%"+p.SearchTerm+"%", "%"+p.SearchTerm+"%")
	}
	if containsEmbyFilter(p.Filters, "IsFavorite") {
		if strings.TrimSpace(p.UserID) == "" {
			return map[string]any{"Items": []map[string]any{}, "TotalRecordCount": 0, "StartIndex": p.StartIndex}, nil
		}
		q = q.Joins("JOIN favorites ON favorites.media_id = media.id AND favorites.user_id = ? AND favorites.deleted_at IS NULL", p.UserID)
	}
	rowLimit := p.StartIndex + maxInt(p.Limit*40, 1000)
	if rowLimit < p.Limit {
		rowLimit = p.Limit
	}
	if rowLimit > embySeriesGroupingLimit {
		rowLimit = embySeriesGroupingLimit
	}
	var rows []model.Media
	if err := q.Order("media.created_at desc").Limit(rowLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	groups := e.seriesGroupsFromMedia(rows)
	sortSeriesGroups(groups, p)
	total := len(groups)
	items := make([]map[string]any, 0, minInt(p.Limit, len(groups)))
	for _, group := range pageSlice(groups, p.StartIndex, p.Limit) {
		items = append(items, e.seriesPayload(group))
	}
	return map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": p.StartIndex}, nil
}

func (e *EmbyService) libraryIsEpisodic(ctx context.Context, libraryID string) (bool, error) {
	if strings.TrimSpace(libraryID) == "" {
		return false, nil
	}
	if lib, err := e.repo.Library.FindByID(ctx, libraryID); err != nil {
		return false, err
	} else if lib != nil {
		return embyLibraryTypeIsEpisodic(lib.Type), nil
	}
	var count int64
	err := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("library_id IN ? AND (season_num > 0 OR episode_num > 0)", e.mergedLibraryIDs(ctx, libraryID)).
		Count(&count).Error
	return count > 0, err
}

func (e *EmbyService) mediaBelongsToEpisodicLibrary(ctx context.Context, m *model.Media) bool {
	if e == nil || m == nil || strings.TrimSpace(m.LibraryID) == "" {
		return false
	}
	lib, err := e.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil || lib == nil {
		return false
	}
	return embyLibraryTypeIsEpisodic(lib.Type)
}

func embyLibraryTypeIsEpisodic(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "tv", "anime", "variety":
		return true
	default:
		return false
	}
}

func (e *EmbyService) filterMovieItems(ctx context.Context, q *gorm.DB) *gorm.DB {
	episodicIDs := e.episodicLibraryIDs(ctx)
	if len(episodicIDs) == 0 {
		return q
	}
	return q.Where("(media.season_num = 0 AND media.episode_num = 0) OR media.library_id NOT IN ?", episodicIDs)
}

func (e *EmbyService) filterEpisodeItems(ctx context.Context, q *gorm.DB) *gorm.DB {
	episodicIDs := e.episodicLibraryIDs(ctx)
	if len(episodicIDs) == 0 {
		return q.Where("1 = 0")
	}
	return q.Where("media.library_id IN ? AND (media.season_num > 0 OR media.episode_num > 0)", episodicIDs)
}

func (e *EmbyService) episodicLibraryIDs(ctx context.Context) []string {
	if e == nil || e.repo == nil || e.repo.DB == nil {
		return nil
	}
	var ids []string
	if err := e.repo.DB.WithContext(ctx).Model(&model.Library{}).
		Where("LOWER(type) IN ?", []string{"tv", "anime", "variety"}).
		Pluck("id", &ids).Error; err != nil {
		return nil
	}
	return ids
}

func (e *EmbyService) rememberSeriesGroup(group embySeriesGroup) {
	if e == nil || strings.TrimSpace(group.ID) == "" {
		return
	}
	expiresAt := time.Now().Add(embyVirtualCacheTTL)
	e.virtualMu.Lock()
	defer e.virtualMu.Unlock()
	if e.virtualSeries == nil {
		e.virtualSeries = make(map[string]embySeriesCacheEntry)
	}
	if e.virtualSeasons == nil {
		e.virtualSeasons = make(map[string]embySeasonCacheEntry)
	}
	if e.virtualArtwork == nil {
		e.virtualArtwork = make(map[string]embyArtworkCacheEntry)
	}
	if len(e.virtualSeries) > 2000 || len(e.virtualSeasons) > 5000 || len(e.virtualArtwork) > 7000 {
		e.virtualSeries = make(map[string]embySeriesCacheEntry)
		e.virtualSeasons = make(map[string]embySeasonCacheEntry)
		e.virtualArtwork = make(map[string]embyArtworkCacheEntry)
	}
	e.virtualSeries[group.ID] = embySeriesCacheEntry{group: group, expiresAt: expiresAt}
	e.virtualArtwork[group.ID] = embyArtworkCacheEntry{primary: group.PosterURL, backdrop: group.BackdropURL, expiresAt: expiresAt}
	e.virtualArtwork[group.ID+"-bd"] = embyArtworkCacheEntry{primary: group.PosterURL, backdrop: group.BackdropURL, expiresAt: expiresAt}
	for _, season := range e.seasonsForSeries(group) {
		e.virtualSeasons[season.ID] = embySeasonCacheEntry{season: season, expiresAt: expiresAt}
		e.virtualArtwork[season.ID] = embyArtworkCacheEntry{primary: season.Series.PosterURL, backdrop: season.Series.BackdropURL, expiresAt: expiresAt}
		e.virtualArtwork[season.ID+"-bd"] = embyArtworkCacheEntry{primary: season.Series.PosterURL, backdrop: season.Series.BackdropURL, expiresAt: expiresAt}
	}
}

func (e *EmbyService) rememberSeasonGroup(season embySeasonGroup) {
	if e == nil || strings.TrimSpace(season.ID) == "" {
		return
	}
	expiresAt := time.Now().Add(embyVirtualCacheTTL)
	e.virtualMu.Lock()
	defer e.virtualMu.Unlock()
	if e.virtualSeasons == nil {
		e.virtualSeasons = make(map[string]embySeasonCacheEntry)
	}
	if e.virtualArtwork == nil {
		e.virtualArtwork = make(map[string]embyArtworkCacheEntry)
	}
	e.virtualSeasons[season.ID] = embySeasonCacheEntry{season: season, expiresAt: expiresAt}
	e.virtualArtwork[season.ID] = embyArtworkCacheEntry{primary: season.Series.PosterURL, backdrop: season.Series.BackdropURL, expiresAt: expiresAt}
	e.virtualArtwork[season.ID+"-bd"] = embyArtworkCacheEntry{primary: season.Series.PosterURL, backdrop: season.Series.BackdropURL, expiresAt: expiresAt}
}

func (e *EmbyService) cachedSeriesGroup(id string) (embySeriesGroup, bool) {
	if e == nil || strings.TrimSpace(id) == "" {
		return embySeriesGroup{}, false
	}
	now := time.Now()
	e.virtualMu.RLock()
	entry, ok := e.virtualSeries[id]
	e.virtualMu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			e.virtualMu.Lock()
			delete(e.virtualSeries, id)
			e.virtualMu.Unlock()
		}
		return embySeriesGroup{}, false
	}
	return entry.group, true
}

func (e *EmbyService) cachedSeasonGroup(id string) (embySeasonGroup, bool) {
	if e == nil || strings.TrimSpace(id) == "" {
		return embySeasonGroup{}, false
	}
	now := time.Now()
	e.virtualMu.RLock()
	entry, ok := e.virtualSeasons[id]
	e.virtualMu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			e.virtualMu.Lock()
			delete(e.virtualSeasons, id)
			e.virtualMu.Unlock()
		}
		return embySeasonGroup{}, false
	}
	return entry.season, true
}

func (e *EmbyService) cachedArtworkURL(id, imageType string) (string, bool) {
	if e == nil || strings.TrimSpace(id) == "" {
		return "", false
	}
	now := time.Now()
	e.virtualMu.RLock()
	entry, ok := e.virtualArtwork[id]
	e.virtualMu.RUnlock()
	if !ok || now.After(entry.expiresAt) {
		if ok {
			e.virtualMu.Lock()
			delete(e.virtualArtwork, id)
			e.virtualMu.Unlock()
		}
		return "", false
	}
	switch strings.ToLower(imageType) {
	case "backdrop", "art":
		if entry.backdrop != "" {
			return entry.backdrop, true
		}
	}
	if entry.primary != "" {
		return entry.primary, true
	}
	return entry.backdrop, entry.backdrop != ""
}

func (e *EmbyService) findSeriesGroup(ctx context.Context, id, userID string) (embySeriesGroup, bool, error) {
	if strings.TrimSpace(id) == "" {
		return embySeriesGroup{}, false, nil
	}
	if strings.HasPrefix(id, embyVirtualSeriesPrefix) {
		if group, ok := e.cachedSeriesGroup(id); ok {
			return group, true, nil
		}
	}
	var rows []model.Media
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("season_num > 0 OR episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	if !strings.HasPrefix(id, embyVirtualSeriesPrefix) {
		q = q.Where("series_id = ?", id)
	}
	if err := q.Order("media.season_num asc, media.episode_num asc, media.created_at asc").Limit(embySeriesGroupingLimit).Find(&rows).Error; err != nil {
		return embySeriesGroup{}, false, err
	}
	for _, group := range e.seriesGroupsFromMedia(rows) {
		if group.ID == id {
			e.rememberSeriesGroup(group)
			return group, true, nil
		}
	}
	if !strings.HasPrefix(id, embyVirtualSeriesPrefix) {
		if series, err := e.repo.Series.FindByID(ctx, id); err != nil {
			return embySeriesGroup{}, false, err
		} else if series != nil {
			return embySeriesGroup{
				ID:          series.ID,
				LibraryID:   series.LibraryID,
				Name:        series.Title,
				PosterURL:   series.PosterURL,
				BackdropURL: series.BackdropURL,
				Overview:    series.Overview,
				Rating:      series.Rating,
				Year:        series.Year,
				TMDbID:      series.TMDbID,
				BangumiID:   series.BangumiID,
				CreatedAt:   series.CreatedAt,
			}, true, nil
		}
	}
	return embySeriesGroup{}, false, nil
}

func (e *EmbyService) findSeasonGroup(ctx context.Context, id, userID string) (embySeasonGroup, bool, error) {
	if strings.TrimSpace(id) == "" || !strings.HasPrefix(id, embyVirtualSeasonPrefix) {
		return embySeasonGroup{}, false, nil
	}
	if season, ok := e.cachedSeasonGroup(id); ok {
		return season, true, nil
	}
	var rows []model.Media
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("season_num > 0 OR episode_num > 0")
	q = e.applyUserMediaVisibility(ctx, q, userID)
	if err := q.
		Order("media.season_num asc, media.episode_num asc, media.created_at asc").
		Limit(embySeriesGroupingLimit).
		Find(&rows).Error; err != nil {
		return embySeasonGroup{}, false, err
	}
	for _, series := range e.seriesGroupsFromMedia(rows) {
		for _, season := range e.seasonsForSeries(series) {
			if season.ID == id {
				e.rememberSeriesGroup(series)
				return season, true, nil
			}
		}
	}
	return embySeasonGroup{}, false, nil
}

func (e *EmbyService) seriesGroupsFromMedia(rows []model.Media) []embySeriesGroup {
	byID := map[string]*embySeriesGroup{}
	order := []string{}
	for _, row := range rows {
		row := row
		seriesID := e.seriesIDForMedia(&row)
		group, ok := byID[seriesID]
		if !ok {
			group = &embySeriesGroup{
				ID:        seriesID,
				LibraryID: row.LibraryID,
				Name:      e.seriesNameForMedia(&row),
				Year:      row.Year,
				TMDbID:    row.TMDbID,
				BangumiID: row.BangumiID,
				CreatedAt: row.CreatedAt,
			}
			byID[seriesID] = group
			order = append(order, seriesID)
		}
		if row.CreatedAt.After(group.CreatedAt) {
			group.CreatedAt = row.CreatedAt
		}
		if group.PosterURL == "" && row.PosterURL != "" {
			group.PosterURL = row.PosterURL
		}
		if group.BackdropURL == "" && row.BackdropURL != "" {
			group.BackdropURL = row.BackdropURL
		}
		if group.Overview == "" && row.Overview != "" {
			group.Overview = row.Overview
		}
		if group.Rating == 0 && row.Rating > 0 {
			group.Rating = row.Rating
		}
		if group.Year == 0 && row.Year > 0 {
			group.Year = row.Year
		}
		group.Episodes = append(group.Episodes, row)
	}
	groups := make([]embySeriesGroup, 0, len(order))
	for _, id := range order {
		group := *byID[id]
		sort.SliceStable(group.Episodes, func(i, j int) bool {
			if group.Episodes[i].SeasonNum != group.Episodes[j].SeasonNum {
				return group.Episodes[i].SeasonNum < group.Episodes[j].SeasonNum
			}
			if group.Episodes[i].EpisodeNum != group.Episodes[j].EpisodeNum {
				return group.Episodes[i].EpisodeNum < group.Episodes[j].EpisodeNum
			}
			return group.Episodes[i].CreatedAt.Before(group.Episodes[j].CreatedAt)
		})
		groups = append(groups, group)
	}
	return groups
}

func (e *EmbyService) seasonsForSeries(series embySeriesGroup) []embySeasonGroup {
	bySeason := map[int]*embySeasonGroup{}
	order := []int{}
	for _, episode := range series.Episodes {
		seasonNum := episode.SeasonNum
		if seasonNum <= 0 {
			seasonNum = 1
		}
		season, ok := bySeason[seasonNum]
		if !ok {
			season = &embySeasonGroup{
				ID:        seasonID(series.ID, seasonNum),
				SeriesID:  series.ID,
				LibraryID: series.LibraryID,
				Name:      seasonName(seasonNum),
				SeasonNum: seasonNum,
				Series:    series,
			}
			bySeason[seasonNum] = season
			order = append(order, seasonNum)
		}
		season.Episodes = append(season.Episodes, episode)
	}
	sort.Ints(order)
	out := make([]embySeasonGroup, 0, len(order))
	for _, seasonNum := range order {
		out = append(out, *bySeason[seasonNum])
	}
	return out
}

func (e *EmbyService) seriesPayload(group embySeriesGroup) map[string]any {
	e.rememberSeriesGroup(group)
	imageTags := map[string]string{}
	backdropTags := []string{}
	if group.PosterURL != "" {
		imageTags["Primary"] = group.ID
	}
	if group.BackdropURL != "" {
		backdropTags = append(backdropTags, group.ID+"-bd")
	}
	return map[string]any{
		"Id":                 group.ID,
		"Name":               group.Name,
		"ServerId":           embyServerID,
		"Type":               "Series",
		"MediaType":          "Video",
		"IsFolder":           true,
		"ParentId":           group.LibraryID,
		"ProductionYear":     group.Year,
		"Overview":           group.Overview,
		"CommunityRating":    group.Rating,
		"RecursiveItemCount": len(group.Episodes),
		"ChildCount":         len(e.seasonsForSeries(group)),
		"DateCreated":        group.CreatedAt,
		"ImageTags":          imageTags,
		"BackdropImageTags":  backdropTags,
		"ProviderIds": map[string]string{
			"Tmdb":    intToStr(group.TMDbID),
			"Bangumi": intToStr(group.BangumiID),
		},
		"UserData": emptyUserData(),
	}
}

func (e *EmbyService) seasonPayload(season embySeasonGroup) map[string]any {
	e.rememberSeasonGroup(season)
	imageTags := map[string]string{}
	backdropTags := []string{}
	if season.Series.PosterURL != "" {
		imageTags["Primary"] = season.ID
	}
	if season.Series.BackdropURL != "" {
		backdropTags = append(backdropTags, season.ID+"-bd")
	}
	return map[string]any{
		"Id":                season.ID,
		"Name":              season.Name,
		"ServerId":          embyServerID,
		"Type":              "Season",
		"MediaType":         "Video",
		"IsFolder":          true,
		"ParentId":          season.SeriesID,
		"SeriesId":          season.SeriesID,
		"SeriesName":        season.Series.Name,
		"IndexNumber":       season.SeasonNum,
		"ChildCount":        len(season.Episodes),
		"ImageTags":         imageTags,
		"BackdropImageTags": backdropTags,
		"UserData":          emptyUserData(),
	}
}

// ImageURL returns artwork for a media/series/season item id.
func (e *EmbyService) ImageURL(ctx context.Context, id, imageType string) (string, error) {
	pick := func(primary, backdrop string) string {
		switch strings.ToLower(imageType) {
		case "backdrop", "art":
			if backdrop != "" {
				return backdrop
			}
		}
		if primary != "" {
			return primary
		}
		return backdrop
	}
	if strings.HasPrefix(id, embyVirtualSeasonPrefix) {
		if raw, ok := e.cachedArtworkURL(id, imageType); ok {
			return raw, nil
		}
		return "", nil
	}
	if strings.HasPrefix(id, embyVirtualSeriesPrefix) {
		if raw, ok := e.cachedArtworkURL(id, imageType); ok {
			return raw, nil
		}
		return "", nil
	}
	m, err := e.repo.Media.FindByID(ctx, id)
	if err == nil && m != nil {
		return pick(m.PosterURL, m.BackdropURL), nil
	}
	if err != nil {
		return "", err
	}
	if series, ok, err := e.findSeriesGroup(ctx, id, ""); err != nil {
		return "", err
	} else if ok {
		return pick(series.PosterURL, series.BackdropURL), nil
	}
	return "", nil
}

func (e *EmbyService) seriesIDForMedia(m *model.Media) string {
	if strings.TrimSpace(m.SeriesID) != "" {
		return m.SeriesID
	}
	return stableEmbyID(embyVirtualSeriesPrefix, m.LibraryID, e.seriesNameForMedia(m))
}

func (e *EmbyService) seasonIDForMedia(m *model.Media) string {
	return seasonID(e.seriesIDForMedia(m), maxInt(m.SeasonNum, 1))
}

func (e *EmbyService) seriesNameForMedia(m *model.Media) string {
	if strings.TrimSpace(m.SeriesID) != "" {
		if series, err := e.repo.Series.FindByID(context.Background(), m.SeriesID); err == nil && series != nil && strings.TrimSpace(series.Title) != "" {
			return series.Title
		}
	}
	if name := inferSeriesNameFromPath(m.Path); name != "" {
		return name
	}
	name := strings.TrimSpace(m.Title)
	name = embyEpisodeTitleRE.ReplaceAllString(name, "")
	name = embyYearSuffixRE.ReplaceAllString(name, "")
	if name == "" {
		name = strings.TrimSpace(m.OriginalName)
	}
	return name
}

func inferSeriesNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if embySeasonDirRE.MatchString(base) {
		dir = filepath.Dir(dir)
		base = filepath.Base(dir)
	}
	base = strings.TrimSpace(embyYearSuffixRE.ReplaceAllString(base, ""))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

func stableEmbyID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(part))))
		_, _ = h.Write([]byte{0})
	}
	return prefix + hex.EncodeToString(h.Sum(nil))[:32]
}

func seasonID(seriesID string, seasonNum int) string {
	return stableEmbyID(embyVirtualSeasonPrefix, seriesID, strconv.Itoa(maxInt(seasonNum, 1)))
}

func seasonName(seasonNum int) string {
	if seasonNum <= 0 {
		seasonNum = 1
	}
	return fmt.Sprintf("第 %d 季", seasonNum)
}

func sortSeriesGroups(groups []embySeriesGroup, p ItemsParams) {
	switch strings.ToLower(p.SortBy) {
	case "sortname", "name":
		sort.SliceStable(groups, func(i, j int) bool {
			if strings.EqualFold(p.SortOrder, "Descending") {
				return groups[i].Name > groups[j].Name
			}
			return groups[i].Name < groups[j].Name
		})
	default:
		sort.SliceStable(groups, func(i, j int) bool {
			if strings.EqualFold(p.SortOrder, "Ascending") {
				return groups[i].CreatedAt.Before(groups[j].CreatedAt)
			}
			return groups[i].CreatedAt.After(groups[j].CreatedAt)
		})
	}
}

func containsItemType(types []string, want string) bool {
	for _, t := range types {
		if strings.EqualFold(strings.TrimSpace(t), want) {
			return true
		}
	}
	return false
}

func containsSupportedEmbyItemType(types []string) bool {
	for _, itemType := range types {
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "movie", "series", "season", "episode", "video", "folder", "collectionfolder":
			return true
		}
	}
	return false
}

func containsOnlyFolderItemTypes(types []string) bool {
	if len(types) == 0 {
		return false
	}
	for _, itemType := range types {
		switch strings.ToLower(strings.TrimSpace(itemType)) {
		case "folder", "collectionfolder":
		default:
			return false
		}
	}
	return true
}

func emptyItemsEnvelope(startIndex int) map[string]any {
	return map[string]any{
		"Items":            []map[string]any{},
		"TotalRecordCount": int64(0),
		"StartIndex":       startIndex,
	}
}

func containsEmbyFilter(filters []string, want string) bool {
	for _, filter := range filters {
		if strings.EqualFold(strings.TrimSpace(filter), want) {
			return true
		}
	}
	return false
}

func firstCSVValue(value string) string {
	if i := strings.Index(value, ","); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func primarySupportedEmbySort(sortBy string, resumeFilter bool) string {
	for _, part := range strings.Split(sortBy, ",") {
		key := strings.ToLower(strings.TrimSpace(part))
		switch key {
		case "sortname", "name", "premieredate", "productionyear", "datecreated", "communityrating":
			return key
		case "dateplayed":
			if resumeFilter {
				return key
			}
		}
	}
	return strings.ToLower(strings.TrimSpace(firstCSVValue(sortBy)))
}

func pageSlice[T any](items []T, start, limit int) []T {
	if start < 0 {
		start = 0
	}
	if limit <= 0 {
		limit = len(items)
	}
	if start >= len(items) {
		return []T{}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func emptyUserData() map[string]any {
	return map[string]any{
		"PlaybackPositionTicks": 0,
		"PlayCount":             0,
		"IsFavorite":            false,
		"Played":                false,
		"PlayedPercentage":      0,
	}
}

func (e *EmbyService) applyUserMediaVisibility(ctx context.Context, q *gorm.DB, userID string) *gorm.DB {
	visibility := e.mediaVisibility(ctx, userID)
	if !visibility.IncludeNSFW {
		q = q.Where("nsfw = ?", false)
		if hidden := visibility.HiddenLibraryIDs; len(hidden) > 0 {
			q = q.Where("library_id NOT IN ?", hidden)
		}
	}
	if len(visibility.AllowedLibraryIDs) > 0 {
		q = q.Where("library_id IN ?", visibility.AllowedLibraryIDs)
	}
	return q
}

func (e *EmbyService) filterMediaRowsForUser(ctx context.Context, rows []model.Media, userID string) []model.Media {
	visibility := e.mediaVisibility(ctx, userID)
	if visibility.IncludeNSFW && len(visibility.AllowedLibraryIDs) == 0 {
		return rows
	}
	allowed := map[string]bool{}
	for _, id := range visibility.AllowedLibraryIDs {
		allowed[id] = true
	}
	hiddenLibraries := map[string]bool{}
	for _, id := range visibility.HiddenLibraryIDs {
		hiddenLibraries[id] = true
	}
	out := rows[:0]
	for _, row := range rows {
		if row.NSFW && !visibility.IncludeNSFW {
			continue
		}
		if hiddenLibraries[row.LibraryID] {
			continue
		}
		if len(allowed) > 0 && !allowed[row.LibraryID] {
			continue
		}
		out = append(out, row)
	}
	return out
}

func (e *EmbyService) mediaVisibility(ctx context.Context, userID string) MediaVisibility {
	if e == nil {
		return MediaVisibility{IncludeNSFW: true}
	}
	key := strings.TrimSpace(userID)
	now := time.Now()
	e.visibilityMu.RLock()
	entry, ok := e.visibilityCache[key]
	e.visibilityMu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return cloneMediaVisibility(entry.visibility)
	}

	visibility := UserDefaultMediaVisibility(ctx, e.repo, userID)
	if !visibility.IncludeNSFW {
		visibility.HiddenLibraryIDs = e.hiddenLibraryIDs(ctx, visibility)
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, e.repo, visibility)
	visibility = cloneMediaVisibility(visibility)

	e.visibilityMu.Lock()
	if e.visibilityCache == nil {
		e.visibilityCache = make(map[string]embyVisibilityCacheEntry)
	}
	if len(e.visibilityCache) > 1000 {
		e.visibilityCache = make(map[string]embyVisibilityCacheEntry)
	}
	e.visibilityCache[key] = embyVisibilityCacheEntry{
		visibility: cloneMediaVisibility(visibility),
		expiresAt:  now.Add(embyVisibilityCacheTTL),
	}
	e.visibilityMu.Unlock()

	return visibility
}

func (e *EmbyService) mergedLibraryIDs(ctx context.Context, libraryID string) []string {
	ids, err := MergedLibraryIDsForLibrary(ctx, e.repo, libraryID)
	if err != nil || len(ids) == 0 {
		return []string{libraryID}
	}
	return ids
}

func cloneMediaVisibility(visibility MediaVisibility) MediaVisibility {
	if visibility.AllowedLibraryIDs != nil {
		visibility.AllowedLibraryIDs = append([]string(nil), visibility.AllowedLibraryIDs...)
	}
	if visibility.HiddenLibraryIDs != nil {
		visibility.HiddenLibraryIDs = append([]string(nil), visibility.HiddenLibraryIDs...)
	}
	return visibility
}

func (e *EmbyService) libraryVisibleFromCachedVisibility(lib model.Library, visibility MediaVisibility) bool {
	if len(visibility.AllowedLibraryIDs) > 0 {
		allowed := false
		for _, id := range visibility.AllowedLibraryIDs {
			if id == lib.ID {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	if visibility.IncludeNSFW {
		return true
	}
	for _, id := range visibility.HiddenLibraryIDs {
		if id == lib.ID {
			return false
		}
	}
	return true
}

func (e *EmbyService) hiddenLibraryIDs(ctx context.Context, visibility MediaVisibility) []string {
	if visibility.IncludeNSFW {
		return nil
	}
	libs, err := e.repo.Library.List(ctx)
	if err != nil {
		return nil
	}
	shadowed := ShadowedCloudLibraryIDSet(libs)
	ids := make([]string, 0)
	for _, lib := range libs {
		if shadowed[lib.ID] || !LibraryVisibleForUser(ctx, e.repo, lib, visibility) {
			ids = append(ids, lib.ID)
		}
	}
	return ids
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ─── Playback ────────────────────────────────────────────────────────────────

// PlaybackInfo returns a PlaybackInfoResponse usable by Emby clients.
func (e *EmbyService) PlaybackInfo(ctx context.Context, mediaID, userID string) (map[string]any, error) {
	m, err := e.playableMedia(ctx, mediaID, userID)
	if err != nil || m == nil {
		return nil, err
	}
	e.ensureCloudTrackMetadata(ctx, m)
	return map[string]any{
		"MediaSources":  e.mediaSourcesForItem(ctx, m, false, e.directPlayOnly(ctx)),
		"PlaySessionId": fmt.Sprintf("%s-%d", m.ID, time.Now().Unix()),
	}, nil
}

// ensureCloudTrackMetadata 在后台补齐云盘媒体的轨道元数据。
//
// 注意必须是异步的：此前这里在 PlaybackInfo 请求路径上同步执行
// CloudResolve + ffprobe(HTTP)（最长 8 秒），既把第三方播放器的起播时间
// 拖长到秒级，又让每一次点开详情/起播都可能触发一次云盘数据下载，是
// Docker 部署下 CPU/带宽长期居高的来源之一。探测结果落库后，下一次
// 请求自然能读到完整元数据。
func (e *EmbyService) ensureCloudTrackMetadata(ctx context.Context, m *model.Media) {
	if e == nil || m == nil || e.storage == nil || e.probe == nil || !mediaTrackMetadataMissing(m) {
		return
	}
	typ, ref, ok := parseCloudMediaPlaybackURL(m.STRMURL)
	if !ok {
		return
	}
	mediaID := m.ID
	e.cloudProbeMu.Lock()
	if e.cloudProbeInFlight == nil {
		e.cloudProbeInFlight = make(map[string]struct{})
	}
	if _, busy := e.cloudProbeInFlight[mediaID]; busy {
		e.cloudProbeMu.Unlock()
		return
	}
	e.cloudProbeInFlight[mediaID] = struct{}{}
	e.cloudProbeMu.Unlock()

	go func() {
		defer func() {
			e.cloudProbeMu.Lock()
			delete(e.cloudProbeInFlight, mediaID)
			e.cloudProbeMu.Unlock()
		}()
		probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		link, err := e.storage.CloudResolve(probeCtx, typ, ref, "")
		if err != nil {
			if e.log != nil {
				e.log.Debug("resolve cloud media for playback probe failed", zap.String("media_id", mediaID), zap.Error(err))
			}
			return
		}
		probe, err := e.probe.ProbeHTTP(probeCtx, link.URL, link.Headers)
		if err != nil {
			if e.log != nil {
				e.log.Debug("playback cloud ffprobe failed", zap.String("media_id", mediaID), zap.Error(err))
			}
			return
		}
		updates := probeResultUpdates(probe)
		if len(updates) == 0 {
			return
		}
		if err := e.repo.DB.WithContext(probeCtx).Model(&model.Media{}).Where("id = ?", mediaID).Updates(updates).Error; err != nil && e.log != nil {
			e.log.Debug("persist playback cloud probe failed", zap.String("media_id", mediaID), zap.Error(err))
		}
	}()
}

func mediaTrackMetadataMissing(m *model.Media) bool {
	return m.DurationSec <= 0 ||
		m.Width <= 0 ||
		m.Height <= 0 ||
		strings.TrimSpace(m.VideoCodec) == "" ||
		strings.TrimSpace(m.AudioCodec) == ""
}

func parseCloudMediaPlaybackURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	const prefix = "api/cloud/play/"
	idx := strings.Index(strings.ToLower(path), prefix)
	if idx < 0 {
		return "", "", false
	}
	typ := strings.TrimSpace(path[idx+len(prefix):])
	ref := strings.TrimSpace(u.Query().Get("ref"))
	return typ, ref, typ != "" && ref != ""
}

func applyProbeResultToMediaValue(m *model.Media, probe *ProbeResult) {
	if m == nil || probe == nil {
		return
	}
	if probe.DurationSec > 0 {
		m.DurationSec = probe.DurationSec
	}
	if probe.Width > 0 {
		m.Width = probe.Width
	}
	if probe.Height > 0 {
		m.Height = probe.Height
	}
	if strings.TrimSpace(probe.VideoCodec) != "" {
		m.VideoCodec = probe.VideoCodec
	}
	if strings.TrimSpace(probe.AudioCodec) != "" {
		m.AudioCodec = probe.AudioCodec
	}
	if strings.TrimSpace(probe.Container) != "" {
		m.Container = probe.Container
	}
}

// directPlayOnly reports whether the admin enabled「客户端直连解码」mode.
// In that mode the host never transcodes; clients must direct-play.
func (e *EmbyService) directPlayOnly(ctx context.Context) bool {
	if e.repo == nil || e.repo.Setting == nil {
		return false
	}
	v, err := e.repo.Setting.Get(ctx, PlaybackDirectOnlySettingKey)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

func (e *EmbyService) playableMedia(ctx context.Context, id, userID string) (*model.Media, error) {
	if season, ok, err := e.findSeasonGroup(ctx, id, userID); err != nil {
		return nil, err
	} else if ok && len(season.Episodes) > 0 {
		return &season.Episodes[0], nil
	}
	if series, ok, err := e.findSeriesGroup(ctx, id, userID); err != nil {
		return nil, err
	} else if ok && len(series.Episodes) > 0 {
		return &series.Episodes[0], nil
	}
	m, err := e.repo.Media.FindByID(ctx, id)
	if err != nil || m == nil {
		return m, err
	}
	if !UserDefaultMediaVisibility(ctx, e.repo, userID).Allows(m) {
		return nil, nil
	}
	return m, nil
}

// mediaSource 是 /Items 与 /PlaybackInfo 共享的 MediaSource 结构。
//
// asEmbedded=true：嵌在 /Items 列表里，不包含完整 stream URL（避免暴露
// 直链给搜索接口）。/PlaybackInfo 走 false 路径，URL 指向 Emby 兼容
// /Videos/{id}/stream（客户端会继续携带 X-Emby-Token 或 append api_key）。
func (e *EmbyService) mediaSource(ctx context.Context, m *model.Media, asEmbedded, directOnly bool) map[string]any {
	container := strings.Trim(strings.ToLower(m.Container), ". ")
	if container == "" {
		container = strings.TrimPrefix(strings.ToLower(filepath.Ext(m.Path)), ".")
	}
	if container == "" && strings.TrimSpace(m.STRMURL) != "" {
		container = "strm"
	}
	isCloud := strings.TrimSpace(m.STRMURL) != ""
	playURL := embyDirectStreamURL(m.ID, container)
	if isCloud {
		switch CloudPlaybackMode(ctx, e.repo) {
		case CloudPlaybackModeSTRM:
			playURL = embySTRMStreamURL(m.ID)
		case CloudPlaybackModeRedirectProxy:
			playURL = embyDirectStreamURL(m.ID, container)
		default:
			playURL = ""
		}
	}
	if isCloud {
		// Cloud/WebDAV media is already a direct/proxy stream. Advertising HLS
		// transcoding makes some Emby clients pick /master.m3u8, forcing this
		// lightweight server to pull remote bytes through ffmpeg and often
		// surfacing as "network/playback failed". Keep cloud media direct-only.
		directOnly = true
	}
	src := map[string]any{
		"Id":                    m.ID,
		"Name":                  m.Title,
		"Path":                  m.Path,
		"Container":             container,
		"Size":                  m.SizeBytes,
		"Protocol":              "Http",
		"Type":                  "Default",
		"IsRemote":              isCloud,
		"RequiresOpening":       false,
		"RequiresClosing":       false,
		"ReadAtNativeFramerate": false,
		"SupportsTranscoding":   !directOnly,
		// 云盘媒体的 Path 在 PlaybackInfo 阶段会被补上 api_key，且最终
		// 302 到云盘直链。Infuse/Emby 官方客户端会优先挑选 DirectPlay
		// 源；如果这里标 false，即使 DirectStreamUrl 可用，也可能被判定
		// 为“没有可播放媒体源”。
		"SupportsDirectStream": !isCloud || playURL != "",
		"SupportsDirectPlay":   !isCloud || playURL != "",
		"SupportsProbing":      true,
		"RunTimeTicks":         int64(m.DurationSec) * 10_000_000,
		"MediaStreams":         e.mediaStreams(m),
	}
	if !asEmbedded && playURL != "" {
		src["DirectStreamUrl"] = playURL
		// 直连解码模式下不下发 TranscodingUrl，迫使客户端本地解码直连，
		// 宿主机不参与转码。
		if !directOnly {
			src["TranscodingUrl"] = "/Videos/" + m.ID + "/master.m3u8"
		}
	}
	if strings.TrimSpace(m.STRMURL) != "" && playURL != "" {
		// STRM / cloud:// media must stay behind a token-aware endpoint. When
		// STRM playback is enabled we expose /api/stream so third-party clients
		// follow the same STRM entry as generated .strm files; when disabled we
		// expose /Videos/{id}/stream so playback uses the Emby 302/proxy path.
		src["IsRemote"] = true
		src["Path"] = playURL
	}
	return src
}

func (e *EmbyService) mediaSourcesForItem(ctx context.Context, m *model.Media, asEmbedded, directOnly bool) []map[string]any {
	siblings := e.mediaVersionSiblings(ctx, m)
	if len(siblings) == 0 {
		return []map[string]any{e.mediaSource(ctx, m, asEmbedded, directOnly)}
	}
	sources := make([]map[string]any, 0, len(siblings))
	for i := range siblings {
		media := siblings[i]
		sources = append(sources, e.mediaSource(ctx, &media, asEmbedded, directOnly))
	}
	return sources
}

func (e *EmbyService) mediaVersionSiblings(ctx context.Context, m *model.Media) []model.Media {
	if e == nil || e.repo == nil || e.repo.DB == nil || m == nil || strings.TrimSpace(m.ID) == "" {
		return nil
	}
	libraryIDs := e.mergedLibraryIDs(ctx, m.LibraryID)
	if len(libraryIDs) == 0 {
		libraryIDs = []string{m.LibraryID}
	}
	q := e.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("library_id IN ?", libraryIDs).
		Where("season_num = ? AND episode_num = ?", m.SeasonNum, m.EpisodeNum)
	if m.TMDbID > 0 {
		q = q.Where("tm_db_id = ?", m.TMDbID)
	} else if m.BangumiID > 0 {
		q = q.Where("bangumi_id = ?", m.BangumiID)
	} else {
		title := strings.TrimSpace(m.Title)
		if title == "" {
			title = strings.TrimSpace(m.OriginalName)
		}
		if title == "" {
			return []model.Media{*m}
		}
		q = q.Where("LOWER(title) = ?", strings.ToLower(title))
		if m.Year > 0 {
			q = q.Where("year = ?", m.Year)
		}
	}
	var rows []model.Media
	if err := q.Find(&rows).Error; err != nil || len(rows) == 0 {
		return []model.Media{*m}
	}
	rows = e.collapseExactPathRows(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].ID == m.ID {
			return true
		}
		if rows[j].ID == m.ID {
			return false
		}
		return preferMediaVersion(rows[i], rows[j])
	})
	return rows
}

func (e *EmbyService) collapseExactPathRows(rows []model.Media) []model.Media {
	if len(rows) < 2 {
		return rows
	}
	out := rows[:0]
	seen := map[string]struct{}{}
	for _, row := range rows {
		path := strings.TrimSpace(row.Path)
		if path != "" {
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
		}
		out = append(out, row)
	}
	return out
}

func (e *EmbyService) mediaVersionKey(ctx context.Context, m *model.Media) string {
	if e == nil || m == nil {
		return ""
	}
	ids := e.mergedLibraryIDs(ctx, m.LibraryID)
	sort.Strings(ids)
	libraryGroup := strings.Join(ids, ",")
	if libraryGroup == "" {
		libraryGroup = strings.TrimSpace(m.LibraryID)
	}
	if m.TMDbID > 0 {
		return fmt.Sprintf("%s|tmdb:%d|s:%d|e:%d", libraryGroup, m.TMDbID, m.SeasonNum, m.EpisodeNum)
	}
	if m.BangumiID > 0 {
		return fmt.Sprintf("%s|bangumi:%d|s:%d|e:%d", libraryGroup, m.BangumiID, m.SeasonNum, m.EpisodeNum)
	}
	title := strings.ToLower(strings.TrimSpace(m.Title))
	if title == "" {
		title = strings.ToLower(strings.TrimSpace(m.OriginalName))
	}
	if title == "" {
		return ""
	}
	return fmt.Sprintf("%s|title:%s|y:%d|s:%d|e:%d", libraryGroup, title, m.Year, m.SeasonNum, m.EpisodeNum)
}

func preferMediaVersion(candidate, current model.Media) bool {
	candidateCloud := strings.TrimSpace(candidate.STRMURL) != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(candidate.Path)), "cloud://")
	currentCloud := strings.TrimSpace(current.STRMURL) != "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(current.Path)), "cloud://")
	if candidateCloud != currentCloud {
		return !candidateCloud
	}
	if candidate.Width != current.Width {
		return candidate.Width > current.Width
	}
	if candidate.SizeBytes != current.SizeBytes {
		return candidate.SizeBytes > current.SizeBytes
	}
	return candidate.CreatedAt.After(current.CreatedAt)
}

func embySTRMStreamURL(mediaID string) string {
	return "/api/stream/" + url.PathEscape(strings.TrimSpace(mediaID))
}

func embyDirectStreamURL(mediaID, container string) string {
	mediaID = strings.TrimSpace(mediaID)
	container = strings.Trim(strings.ToLower(container), ". ")
	if container == "" || container == "strm" {
		return "/Videos/" + mediaID + "/stream"
	}
	return "/Videos/" + mediaID + "/stream." + container
}

func (e *EmbyService) mediaStreams(m *model.Media) []map[string]any {
	streams := []map[string]any{}
	if m.VideoCodec != "" || m.Width > 0 {
		streams = append(streams, map[string]any{
			"Codec":        m.VideoCodec,
			"Type":         "Video",
			"Index":        0,
			"Width":        m.Width,
			"Height":       m.Height,
			"AspectRatio":  "",
			"IsDefault":    true,
			"IsForced":     false,
			"IsExternal":   false,
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
	if len(streams) == 0 {
		streams = append(streams, map[string]any{
			"Codec":        "unknown",
			"Type":         "Video",
			"Index":        0,
			"IsDefault":    true,
			"IsForced":     false,
			"IsExternal":   false,
			"DisplayTitle": "Video",
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

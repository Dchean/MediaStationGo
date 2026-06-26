// Package service — library / media bookkeeping.
package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// MediaService offers high-level CRUD over libraries and media items.
type MediaService struct {
	cfg   *config.Config
	log   *zap.Logger
	repo  *repository.Container
	cache *RuntimeCacheService
}

type MediaVisibility struct {
	IncludeNSFW       bool
	AllowedLibraryIDs []string
	HiddenLibraryIDs  []string
}

const maxMediaSearchLimit = 50000
const maxMediaSearchPageSize = 2000

type LibraryRootInput struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path"`
	Enabled   *bool  `json:"enabled,omitempty"`
	SortOrder *int   `json:"sort_order,omitempty"`
}

func (v MediaVisibility) Allows(media *model.Media) bool {
	if media == nil {
		return false
	}
	if !v.IncludeNSFW && media.NSFW {
		return false
	}
	for _, id := range v.HiddenLibraryIDs {
		if id == media.LibraryID {
			return false
		}
	}
	if len(v.AllowedLibraryIDs) == 0 {
		return true
	}
	for _, id := range v.AllowedLibraryIDs {
		if id == media.LibraryID {
			return true
		}
	}
	return false
}

// NewMediaService is the constructor.
func NewMediaService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *MediaService {
	return &MediaService{cfg: cfg, log: log, repo: repo}
}

func (s *MediaService) SetRuntimeCache(cache *RuntimeCacheService) *MediaService {
	if s != nil {
		s.cache = cache
	}
	return s
}

// CreateLibrary persists a library after validating that its path exists.
func (s *MediaService) CreateLibrary(ctx context.Context, name, path, kind string) (*model.Library, error) {
	return s.CreateLibraryWithRoots(ctx, name, kind, []LibraryRootInput{{Path: path}})
}

func (s *MediaService) CreateLibraryWithRoots(ctx context.Context, name, kind string, inputs []LibraryRootInput) (*model.Library, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("name required")
	}
	roots, err := normalizeLibraryRootInputs(inputs, true)
	if err != nil {
		return nil, err
	}
	kind = inferLibraryKind(name, roots[0].Path, kind)
	lib := &model.Library{Name: strings.TrimSpace(name), Path: roots[0].Path, Type: kind, Enabled: true}
	if err := s.repo.Library.CreateWithRoots(ctx, lib, roots); err != nil {
		return nil, err
	}
	s.invalidateMediaCache(ctx)
	return lib, nil
}

func normalizeLibraryRootInputs(inputs []LibraryRootInput, requirePath bool) ([]model.LibraryRoot, error) {
	roots := make([]model.LibraryRoot, 0, len(inputs))
	seen := map[string]struct{}{}
	for i, input := range inputs {
		rawPath := strings.TrimSpace(input.Path)
		if rawPath == "" {
			if requirePath {
				return nil, errors.New("at least one path required")
			}
			continue
		}
		abs, err := resolveAccessibleLibraryPath(rawPath)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(filepath.Clean(abs))
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate library path: %s", abs)
		}
		seen[key] = struct{}{}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		roots = append(roots, model.LibraryRoot{
			Name:    strings.TrimSpace(input.Name),
			Path:    abs,
			Enabled: enabled,
		})
		if input.SortOrder != nil {
			roots[len(roots)-1].SortOrder = *input.SortOrder
		} else {
			roots[len(roots)-1].SortOrder = i
		}
	}
	if len(roots) == 0 && requirePath {
		return nil, errors.New("at least one path required")
	}
	return roots, nil
}

// ListLibraries returns every library configured on the server.
func (s *MediaService) ListLibraries(ctx context.Context) ([]model.Library, error) {
	return s.repo.Library.List(ctx)
}

func (s *MediaService) ListLibraryRoots(ctx context.Context, libraryID string) ([]model.LibraryRoot, error) {
	if err := s.ensureLibraryRoots(ctx, libraryID); err != nil {
		return nil, err
	}
	return s.repo.Library.ListRoots(ctx, libraryID)
}

func (s *MediaService) AddLibraryRoot(ctx context.Context, libraryID string, input LibraryRootInput) (*model.LibraryRoot, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, errors.New("library not found")
	}
	roots, err := normalizeLibraryRootInputs([]LibraryRootInput{input}, true)
	if err != nil {
		return nil, err
	}
	root := roots[0]
	root.LibraryID = libraryID
	if err := s.ensureLibraryRootPathUnique(ctx, libraryID, "", root.Path); err != nil {
		return nil, err
	}
	if root.SortOrder == 0 {
		existing, _ := s.repo.Library.ListRoots(ctx, libraryID)
		root.SortOrder = len(existing)
	}
	if err := s.repo.Library.CreateRoot(ctx, &root); err != nil {
		return nil, err
	}
	if strings.TrimSpace(lib.Path) == "" {
		_ = s.repo.DB.WithContext(ctx).Model(&model.Library{}).Where("id = ?", libraryID).Update("path", root.Path).Error
	}
	return &root, nil
}

func (s *MediaService) UpdateLibraryRoot(ctx context.Context, libraryID, rootID string, input LibraryRootInput) (*model.LibraryRoot, error) {
	root, err := s.repo.Library.FindRootByID(ctx, libraryID, rootID)
	if err != nil || root == nil {
		return root, err
	}
	updates := map[string]any{}
	if input.Name != "" {
		updates["name"] = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.Path) != "" {
		roots, err := normalizeLibraryRootInputs([]LibraryRootInput{input}, true)
		if err != nil {
			return nil, err
		}
		if err := s.ensureLibraryRootPathUnique(ctx, libraryID, rootID, roots[0].Path); err != nil {
			return nil, err
		}
		updates["path"] = roots[0].Path
		root.Path = roots[0].Path
	}
	if input.Enabled != nil {
		updates["enabled"] = *input.Enabled
		root.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		updates["sort_order"] = *input.SortOrder
		root.SortOrder = *input.SortOrder
	}
	if err := s.repo.Library.UpdateRoot(ctx, root, updates); err != nil {
		return nil, err
	}
	if err := s.syncLibraryPrimaryRoot(ctx, libraryID); err != nil {
		return nil, err
	}
	return s.repo.Library.FindRootByID(ctx, libraryID, rootID)
}

func (s *MediaService) DeleteLibraryRoot(ctx context.Context, libraryID, rootID string) error {
	root, err := s.repo.Library.FindRootByID(ctx, libraryID, rootID)
	if err != nil {
		return err
	}
	if root == nil {
		return errors.New("library root not found")
	}
	roots, err := s.repo.Library.ListRoots(ctx, libraryID)
	if err != nil {
		return err
	}
	if len(roots) <= 1 {
		return errors.New("library must keep at least one path")
	}
	if err := s.repo.Media.DeleteByLibraryRoot(ctx, libraryID, rootID); err != nil {
		return err
	}
	if err := s.repo.Library.DeleteRoot(ctx, libraryID, rootID); err != nil {
		return err
	}
	return s.syncLibraryPrimaryRoot(ctx, libraryID)
}

func (s *MediaService) ensureLibraryRoots(ctx context.Context, libraryID string) error {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil || len(lib.Roots) > 0 || strings.TrimSpace(lib.Path) == "" {
		return err
	}
	root := &model.LibraryRoot{
		LibraryID: libraryID,
		Name:      filepath.Base(filepath.Clean(lib.Path)),
		Path:      lib.Path,
		Enabled:   lib.Enabled,
		SortOrder: 0,
	}
	return s.repo.Library.CreateRoot(ctx, root)
}

func (s *MediaService) syncLibraryPrimaryRoot(ctx context.Context, libraryID string) error {
	roots, err := s.repo.Library.ListRoots(ctx, libraryID)
	if err != nil || len(roots) == 0 {
		return err
	}
	return s.repo.DB.WithContext(ctx).Model(&model.Library{}).Where("id = ?", libraryID).Update("path", roots[0].Path).Error
}

func (s *MediaService) ensureLibraryRootPathUnique(ctx context.Context, libraryID, exceptRootID, pathValue string) error {
	roots, err := s.repo.Library.ListRoots(ctx, libraryID)
	if err != nil {
		return err
	}
	key := strings.ToLower(filepath.Clean(strings.TrimSpace(pathValue)))
	for _, existing := range roots {
		if existing.ID == exceptRootID {
			continue
		}
		if strings.ToLower(filepath.Clean(strings.TrimSpace(existing.Path))) == key {
			return fmt.Errorf("duplicate library path: %s", pathValue)
		}
	}
	return nil
}

// DeleteLibrary removes a library and its media rows. The on-disk files are
// left untouched.
func (s *MediaService) DeleteLibrary(ctx context.Context, id string) error {
	lib, err := s.repo.Library.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if lib != nil {
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			if err := s.repo.Media.PurgeByLibrary(ctx, id); err != nil {
				return err
			}
			_ = s.repo.DB.WithContext(ctx).Where("library_id = ?", id).Delete(&model.LibraryRoot{}).Error
			err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Library{}).Error
			if err == nil {
				s.invalidateMediaCache(ctx)
			}
			return err
		}
	}
	_ = s.repo.DB.WithContext(ctx).Where("library_id = ?", id).Delete(&model.LibraryRoot{}).Error
	if err := s.repo.Media.DeleteByLibrary(ctx, id); err != nil {
		return err
	}
	err = s.repo.Library.Delete(ctx, id)
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}

// ListMedia paginates media items inside a library.
func (s *MediaService) ListMedia(ctx context.Context, libraryID string, page, pageSize int) ([]model.Media, int64, error) {
	return s.ListMediaVisible(ctx, libraryID, page, pageSize, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) ListMediaVisible(ctx context.Context, libraryID string, page, pageSize int, visibility MediaVisibility) ([]model.Media, int64, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 2000 {
		pageSize = 2000
	}
	if page < 1 {
		page = 1
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	libraryIDs, err := MergedLibraryIDsForLibrary(ctx, s.repo, libraryID)
	if err != nil {
		return nil, 0, err
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	cacheKey := s.mediaListCacheKey(libraryID, libraryIDs, page, pageSize, filter)
	var cached mediaListCacheValue
	if s.cache != nil && s.cache.GetJSON(ctx, cacheKey, &cached) {
		s.attachLibraryMetadata(ctx, cached.Items)
		return cached.Items, cached.Total, nil
	}
	items, total, err := s.repo.Media.ListByLibrariesFiltered(ctx, libraryIDs, (page-1)*pageSize, pageSize, filter)
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadata(ctx, items)
	if s.cache != nil {
		s.cache.SetJSON(ctx, cacheKey, mediaListCacheValue{Items: items, Total: total}, time.Duration(s.mediaCacheTTLSeconds())*time.Second)
	}
	return items, total, nil
}

func (s *MediaService) ListMediaVisibleGrouped(ctx context.Context, libraryID string, page, pageSize int, visibility MediaVisibility) ([]MediaItem, int64, error) {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	items, err := s.listMediaVisibleForGrouping(ctx, libraryID, visibility)
	if err != nil {
		return nil, 0, err
	}
	grouped := groupMediaVersions(items)
	return paginateMediaItems(grouped, page, pageSize), int64(len(grouped)), nil
}

func (s *MediaService) listMediaVisibleForGrouping(ctx context.Context, libraryID string, visibility MediaVisibility) ([]model.Media, error) {
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	libraryIDs, err := MergedLibraryIDsForLibrary(ctx, s.repo, libraryID)
	if err != nil {
		return nil, err
	}
	filter := repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	}
	cacheKey := s.mediaListCacheKey(libraryID, libraryIDs, 0, maxMediaSearchLimit, filter) + ":group-source"
	var cached mediaListCacheValue
	if s.cache != nil && s.cache.GetJSON(ctx, cacheKey, &cached) {
		s.attachLibraryMetadata(ctx, cached.Items)
		return cached.Items, nil
	}
	items, total, err := s.repo.Media.ListByLibrariesFiltered(ctx, libraryIDs, 0, maxMediaSearchLimit, filter)
	if err != nil {
		return nil, err
	}
	if total > int64(len(items)) && s.log != nil {
		s.log.Warn("media version grouping truncated by safety limit",
			zap.String("library_id", libraryID),
			zap.Int64("total", total),
			zap.Int("limit", maxMediaSearchLimit))
	}
	s.attachLibraryMetadata(ctx, items)
	if s.cache != nil {
		s.cache.SetJSON(ctx, cacheKey, mediaListCacheValue{Items: items, Total: total}, time.Duration(s.mediaCacheTTLSeconds())*time.Second)
	}
	return items, nil
}

// SearchMedia performs a simple LIKE search across titles.
func (s *MediaService) SearchMedia(ctx context.Context, query string, limit int) ([]model.Media, error) {
	return s.SearchMediaVisible(ctx, query, limit, MediaVisibility{IncludeNSFW: true})
}

func (s *MediaService) SearchMediaVisible(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]model.Media, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	items, err := s.repo.Media.SearchFiltered(ctx, query, limit, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil {
		return nil, err
	}
	s.attachLibraryMetadata(ctx, items)
	return items, nil
}

func (s *MediaService) SearchMediaVisibleGrouped(ctx context.Context, query string, limit int, visibility MediaVisibility) ([]MediaItem, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > maxMediaSearchLimit {
		limit = maxMediaSearchLimit
	}
	items, err := s.SearchMediaVisible(ctx, query, maxMediaSearchLimit, visibility)
	if err != nil {
		return nil, err
	}
	return firstMediaItems(groupMediaVersions(items), limit), nil
}

func (s *MediaService) SearchMediaVisiblePage(ctx context.Context, query string, page, pageSize int, visibility MediaVisibility) ([]model.Media, int64, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > maxMediaSearchPageSize {
		pageSize = maxMediaSearchPageSize
	}
	if page < 1 {
		page = 1
	}
	visibility = ExpandMediaVisibilityForMergedCloudLibraries(ctx, s.repo, visibility)
	items, total, err := s.repo.Media.SearchFilteredPage(ctx, query, (page-1)*pageSize, pageSize, repository.MediaQueryFilter{
		IncludeNSFW:       visibility.IncludeNSFW,
		AllowedLibraryIDs: visibility.AllowedLibraryIDs,
		HiddenLibraryIDs:  visibility.HiddenLibraryIDs,
	})
	if err != nil {
		return nil, 0, err
	}
	s.attachLibraryMetadata(ctx, items)
	return items, total, nil
}

func (s *MediaService) SearchMediaVisiblePageGrouped(ctx context.Context, query string, page, pageSize int, visibility MediaVisibility) ([]MediaItem, int64, error) {
	page, pageSize = normalizeGroupedMediaPage(page, pageSize)
	items, err := s.SearchMediaVisible(ctx, query, maxMediaSearchLimit, visibility)
	if err != nil {
		return nil, 0, err
	}
	grouped := groupMediaVersions(items)
	return paginateMediaItems(grouped, page, pageSize), int64(len(grouped)), nil
}

// GetMedia returns a single media row.
func (s *MediaService) GetMedia(ctx context.Context, id string) (*model.Media, error) {
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil || media == nil {
		return media, err
	}
	items := []model.Media{*media}
	s.attachLibraryMetadata(ctx, items)
	*media = items[0]
	return media, nil
}

const maxRecycleBinRecords = 200

// SoftDelete moves a media row to the recycle bin (gorm soft delete).
// The on-disk file is kept; admins can purge it later.
func (s *MediaService) SoftDelete(ctx context.Context, id string) error {
	media, err := s.repo.Media.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if media != nil && isCloudMediaPath(media.Path) {
		err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
		if err == nil {
			s.invalidateMediaCache(ctx)
		}
		return err
	}
	err = s.repo.DB.WithContext(ctx).Where("id = ?", id).Delete(&model.Media{}).Error
	if err == nil {
		if pruneErr := pruneRecycleBinRows(ctx, s.repo.DB, maxRecycleBinRecords); pruneErr != nil {
			return pruneErr
		}
		s.invalidateMediaCache(ctx)
	}
	return err
}

// RestoreDeleted unsets DeletedAt for a single media row.
func (s *MediaService) RestoreDeleted(ctx context.Context, id string) error {
	err := s.repo.DB.WithContext(ctx).Unscoped().Model(&model.Media{}).
		Where("id = ?", id).Update("deleted_at", nil).Error
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}

// ListRecycleBin returns every soft-deleted row, newest first.
func (s *MediaService) ListRecycleBin(ctx context.Context, limit int) ([]model.Media, error) {
	if err := pruneRecycleBinRows(ctx, s.repo.DB, maxRecycleBinRecords); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxRecycleBinRecords {
		limit = maxRecycleBinRecords
	}
	var rows []model.Media
	err := s.repo.DB.Unscoped().
		Where("deleted_at IS NOT NULL").
		Order("deleted_at desc").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func pruneRecycleBinRows(ctx context.Context, db *gorm.DB, keep int) error {
	if db == nil {
		return nil
	}
	if keep <= 0 {
		keep = maxRecycleBinRecords
	}
	var rows []struct {
		ID string
	}
	if err := db.WithContext(ctx).Unscoped().
		Model(&model.Media{}).
		Select("id").
		Where("deleted_at IS NOT NULL").
		Order("deleted_at desc").
		Limit(100000).
		Offset(keep).
		Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&model.Media{}).Error
}

// PurgeDeleted permanently removes a soft-deleted row from the database.
func (s *MediaService) PurgeDeleted(ctx context.Context, id string) error {
	err := s.repo.DB.WithContext(ctx).Unscoped().Where("id = ?", id).Delete(&model.Media{}).Error
	if err == nil {
		s.invalidateMediaCache(ctx)
	}
	return err
}

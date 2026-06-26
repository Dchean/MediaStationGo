package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// ScanLibrary walks the library root and persists discovered media files.
func (s *ScannerService) ScanLibrary(ctx context.Context, libraryID string) (*ScanResult, error) {
	return s.scanLibrary(ctx, libraryID, true)
}

func (s *ScannerService) ScanLibraryRoot(ctx context.Context, libraryID, rootID string) (*ScanResult, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, errors.New("library not found")
	}
	root, err := s.repo.Library.FindRootByID(ctx, libraryID, rootID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, errors.New("library root not found")
	}
	return s.scanLocalLibraryRoot(ctx, lib, root, true)
}

// ScanLibraryWithoutAutoScrape walks a library without kicking off online
// metadata enrichment. Cloud mounts can contain very large trees; keeping mount
// scans import-only prevents scraper bursts from overwhelming small NAS boxes.
func (s *ScannerService) ScanLibraryWithoutAutoScrape(ctx context.Context, libraryID string) (*ScanResult, error) {
	return s.scanLibrary(ctx, libraryID, false)
}

func (s *ScannerService) TryBeginLocalScan(libraryID string) (func(), bool) {
	if s == nil || strings.TrimSpace(libraryID) == "" {
		return func() {}, true
	}
	s.localScanMu.Lock()
	if s.localScans == nil {
		s.localScans = make(map[string]struct{})
	}
	if _, ok := s.localScans[libraryID]; ok {
		s.localScanMu.Unlock()
		return nil, false
	}
	s.localScans[libraryID] = struct{}{}
	s.localScanMu.Unlock()
	return func() {
		s.localScanMu.Lock()
		delete(s.localScans, libraryID)
		s.localScanMu.Unlock()
	}, true
}

func (s *ScannerService) scanLibrary(ctx context.Context, libraryID string, autoScrape bool) (*ScanResult, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, err
	}
	if mount, ok := ParseCloudLibraryMount(lib.Path); ok {
		return s.scanMountedCloudLibrary(ctx, lib, mount, autoScrape)
	}
	res := &ScanResult{LibraryID: lib.ID}
	writeBatch := newLocalMediaWriteBatch(s, ctx, res, 100)
	existingMedia, err := s.existingLocalMediaSnapshot(ctx, lib.ID)
	if err != nil {
		s.log.Warn("load existing local media snapshot failed", zap.String("library_id", lib.ID), zap.Error(err))
		existingMedia = nil
	}

	roots, err := s.localLibraryScanRoots(ctx, lib)
	if err != nil {
		return res, err
	}
	if len(roots) == 0 {
		return res, errors.New("library has no enabled paths")
	}
	var scanErr error
	scannedRoots := 0
	for i := range roots {
		root := roots[i]
		if err := s.resolveLocalLibraryRootPath(ctx, lib, &root); err != nil {
			addScanError(res, root.Path, err)
			s.log.Warn("library root scan skipped",
				zap.String("library_id", lib.ID),
				zap.String("root_id", root.ID),
				zap.String("path", root.Path),
				zap.Error(err))
			if scanErr == nil {
				scanErr = err
			}
			continue
		}
		seen, walkErr := s.scanLocalLibraryFiles(ctx, lib, &root, existingMedia, writeBatch, res)
		if walkErr != nil {
			addScanError(res, root.Path, walkErr)
			if scanErr == nil {
				scanErr = walkErr
			}
			continue
		}
		scannedRoots++
		removed, err := s.pruneMissingMediaForRoot(ctx, lib.ID, root.ID, root.Path, seen)
		if err != nil {
			s.log.Warn("prune missing media failed", zap.String("library_id", lib.ID), zap.String("root_id", root.ID), zap.Error(err))
		} else {
			res.Removed += removed
		}
	}
	writeBatch.Flush()
	if scanErr != nil && scannedRoots == 0 {
		return res, scanErr
	}

	s.finishLocalLibraryScan(ctx, lib, res, autoScrape)
	return res, nil
}

func (s *ScannerService) scanLocalLibraryRoot(ctx context.Context, lib *model.Library, root *model.LibraryRoot, autoScrape bool) (*ScanResult, error) {
	res := &ScanResult{LibraryID: lib.ID}
	if root == nil || !root.Enabled {
		return res, errors.New("library root disabled or not found")
	}
	if err := s.resolveLocalLibraryRootPath(ctx, lib, root); err != nil {
		return res, err
	}
	writeBatch := newLocalMediaWriteBatch(s, ctx, res, 100)
	existingMedia, err := s.existingLocalMediaSnapshot(ctx, lib.ID)
	if err != nil {
		s.log.Warn("load existing local media snapshot failed", zap.String("library_id", lib.ID), zap.Error(err))
		existingMedia = nil
	}
	seen, walkErr := s.scanLocalLibraryFiles(ctx, lib, root, existingMedia, writeBatch, res)
	writeBatch.Flush()
	if walkErr != nil {
		addScanError(res, root.Path, walkErr)
		return res, walkErr
	}
	removed, err := s.pruneMissingMediaForRoot(ctx, lib.ID, root.ID, root.Path, seen)
	if err != nil {
		s.log.Warn("prune missing media failed", zap.String("library_id", lib.ID), zap.String("root_id", root.ID), zap.Error(err))
	} else {
		res.Removed = removed
	}
	s.finishLocalLibraryScan(ctx, lib, res, autoScrape)
	return res, nil
}

func (s *ScannerService) scanLocalLibraryFiles(ctx context.Context, lib *model.Library, root *model.LibraryRoot, existingMedia map[string]existingLocalMedia, writeBatch *localMediaWriteBatch, res *ScanResult) (map[string]struct{}, error) {
	seen := make(map[string]struct{})
	seenInodes := existingLocalMediaFileIDs(existingMedia)
	walkFn := func(path string, info walkInfo) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if info.isDir {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := videoExtensions[ext]; !ok {
			return nil
		}
		seen[filepath.Clean(path)] = struct{}{}
		s.ingestFile(ctx, lib, root, path, info.size, seenInodes, existingMedia, writeBatch, res)
		return nil
	}
	return seen, walk(root.Path, walkFn)
}

func existingLocalMediaFileIDs(existingMedia map[string]existingLocalMedia) map[string]string {
	seenInodes := make(map[string]string)
	for path, existing := range existingMedia {
		if existing.FileID != "" {
			seenInodes[existing.FileID] = path
		}
	}
	return seenInodes
}

func (s *ScannerService) finishLocalLibraryScan(ctx context.Context, lib *model.Library, res *ScanResult, autoScrape bool) {
	s.hub.Publish("scan", map[string]any{
		"library_id":  lib.ID,
		"finished":    true,
		"visited":     res.Visited,
		"added":       res.Added,
		"updated":     res.Updated,
		"probed":      res.Probed,
		"local_meta":  res.LocalMetadata,
		"removed":     res.Removed,
		"error_count": res.ErrorCount,
		"errors":      res.Errors,
	})
	s.notifyScanFinished(lib, res, nil, false)
	s.invalidateMediaCache(ctx)
	s.maybeGenerateSTRMAfterScan(lib.ID)

	if scanHasImportChanges(res) && autoScrape && s.scraper != nil && s.scraper.AnyEnabled() && s.autoScrapeEnabled(ctx) {
		s.startAutoScrape(ctx, lib.ID)
	}
}

func (s *ScannerService) scanMountedCloudLibrary(ctx context.Context, lib *model.Library, mount CloudMountInfo, autoScrape bool) (*ScanResult, error) {
	if IsDeprecatedNativeCloudProvider(mount.Provider) {
		return &ScanResult{LibraryID: lib.ID, Skipped: 1}, nil
	}
	if CloudLibraryAutoCategory(*lib) {
		res := &ScanResult{LibraryID: lib.ID, Skipped: 1}
		s.log.Info("skip auto category cloud library scan",
			zap.String("library_id", lib.ID),
			zap.String("provider", mount.Provider))
		s.hub.Publish("scan", map[string]any{
			"library_id":    lib.ID,
			"finished":      true,
			"skipped":       res.Skipped,
			"cloud":         true,
			"auto_category": true,
		})
		return res, nil
	}
	if shadow := s.shadowedCloudLibrary(ctx, lib); shadow != nil {
		res := &ScanResult{LibraryID: lib.ID, Skipped: 1}
		s.log.Warn("skip shadowed cloud library scan",
			zap.String("library_id", lib.ID),
			zap.String("shadowed_by", shadow.Library.ID),
			zap.String("provider", mount.Provider))
		s.hub.Publish("scan", map[string]any{
			"library_id": lib.ID,
			"finished":   true,
			"skipped":    res.Skipped,
			"cloud":      true,
			"shadowed":   true,
		})
		return res, nil
	}
	scanCtx, finish, err := s.beginCloudScan(ctx, lib, mount)
	if err != nil {
		if errors.Is(err, ErrCloudScanAlreadyRunning) {
			return &ScanResult{LibraryID: lib.ID, Skipped: 1}, nil
		}
		return nil, err
	}
	release, err := s.acquireCloudScanSlot(scanCtx, lib.ID)
	if err != nil {
		res := &ScanResult{LibraryID: lib.ID}
		if finish != nil {
			finish(res, err)
		}
		return res, err
	}
	defer release()
	res, err := s.scanCloudLibrary(scanCtx, lib, mount, autoScrape)
	if finish != nil {
		finish(res, err)
	}
	return res, err
}

func (s *ScannerService) notifyScanFinished(lib *model.Library, res *ScanResult, err error, cloud bool) {
	if s == nil || s.notify == nil || lib == nil || res == nil {
		return
	}
	if err != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s.notify.Broadcast(ctx, "MediaStationGo 扫描异常", fmt.Sprintf("媒体库：%s\n错误：%s", lib.Name, err.Error()), EventSystemAlert)
		}()
		return
	}
	if res.Added+res.Updated <= 0 {
		return
	}
	source := "本地媒体库"
	if cloud {
		source = "网盘媒体库"
	}
	body := fmt.Sprintf("%s：%s\n新增：%d\n更新：%d\n跳过：%d\n移除：%d", source, lib.Name, res.Added, res.Updated, res.Skipped, res.Removed)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.notify.Broadcast(ctx, "MediaStationGo 入库完成", body, EventLibraryIngest)
	}()
}

// IngestPath ingests a single file into the given library without walking the
// whole tree. Used by the watcher for incremental, event-driven additions so
// adding one new file no longer triggers a full library re-scan (减少硬盘损耗).
// Non-video files and directories are ignored. Returns true if a media row was
// added or updated.
func (s *ScannerService) IngestPath(ctx context.Context, libraryID, path string) (bool, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return false, err
	}
	root, err := s.localLibraryRootForPath(ctx, lib, path)
	if err != nil || root == nil {
		return false, err
	}
	if err := s.resolveLocalLibraryRootPath(ctx, lib, root); err != nil {
		return false, err
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := videoExtensions[ext]; !ok {
		return false, nil
	}
	res := &ScanResult{LibraryID: lib.ID}
	s.ingestFile(ctx, lib, root, path, fi.Size(), make(map[string]string), nil, nil, res)
	if res.Added+res.Updated > 0 {
		s.invalidateMediaCache(ctx)
	}
	return res.Added+res.Updated > 0, nil
}

func (s *ScannerService) resolveLocalLibraryPath(ctx context.Context, lib *model.Library) error {
	if lib == nil || strings.TrimSpace(lib.Path) == "" {
		return nil
	}
	resolved, err := resolveAccessibleLibraryPath(lib.Path)
	if err != nil {
		return err
	}
	if sameLibraryPath(resolved, lib.Path) {
		lib.Path = filepath.Clean(lib.Path)
		return nil
	}
	if s.repo != nil && s.repo.DB != nil {
		if updateErr := s.repo.DB.WithContext(ctx).Model(&model.Library{}).Where("id = ?", lib.ID).Update("path", resolved).Error; updateErr != nil && s.log != nil {
			s.log.Warn("update mapped library path failed",
				zap.String("library_id", lib.ID),
				zap.String("from", lib.Path),
				zap.String("to", resolved),
				zap.Error(updateErr))
		}
	}
	if s.log != nil {
		s.log.Info("mapped library path for scan",
			zap.String("library_id", lib.ID),
			zap.String("from", lib.Path),
			zap.String("to", resolved))
	}
	lib.Path = resolved
	return nil
}

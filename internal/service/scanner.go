// Package service — filesystem scanner.
//
// ScannerService walks the configured library roots looking for video
// files, then upserts a model.Media row per file. Each upsert also runs
// ffprobe (when available) and queues a metadata lookup for newly added
// rows.
//
// When a filename exposes season + episode numbers we store them on the
// Media row for every library type, so variety shows and other episodic
// collections get the same grouping experience as TV/anime.
package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// videoExtensions lists the file extensions treated as media. Matches the
// MediaStation Python defaults.
var videoExtensions = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".m4v":  {},
	".avi":  {},
	".mov":  {},
	".webm": {},
	".ts":   {},
	".rmvb": {},
	".rm":   {},
	".3gp":  {},
	".mpg":  {},
	".mpeg": {},
	".strm": {},
}

// ScannerService walks libraries on disk and upserts model.Media rows.
type ScannerService struct {
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	hub     *Hub
	probe   *FFprobeService
	scraper *ScraperService
	storage *StorageConfigService
}

// NewScannerService is the constructor.
func NewScannerService(
	cfg *config.Config,
	log *zap.Logger,
	repo *repository.Container,
	hub *Hub,
	probe *FFprobeService,
	scraper *ScraperService,
) *ScannerService {
	return &ScannerService{
		cfg: cfg, log: log, repo: repo, hub: hub,
		probe: probe, scraper: scraper,
	}
}

// SetStorageConfig wires cloud-disk storage access into the scanner. It is set
// after service construction because StorageConfigService depends on Crypto,
// while the scanner is needed earlier by watcher/download services.
func (s *ScannerService) SetStorageConfig(storage *StorageConfigService) {
	s.storage = storage
}

// ScanResult summarises a scan run.
type ScanResult struct {
	LibraryID     string `json:"library_id"`
	Visited       int    `json:"visited"`
	Added         int    `json:"added"`
	Updated       int    `json:"updated"`
	Skipped       int    `json:"skipped"`
	Probed        int    `json:"probed"`
	LocalMetadata int    `json:"local_metadata"`
	Removed       int64  `json:"removed"`
}

// ScanLibrary walks the library root and persists discovered media files.
func (s *ScannerService) ScanLibrary(ctx context.Context, libraryID string) (*ScanResult, error) {
	return s.scanLibrary(ctx, libraryID, true)
}

// ScanLibraryWithoutAutoScrape walks a library without kicking off online
// metadata enrichment. Cloud mounts can contain very large trees; keeping mount
// scans import-only prevents scraper bursts from overwhelming small NAS boxes.
func (s *ScannerService) ScanLibraryWithoutAutoScrape(ctx context.Context, libraryID string) (*ScanResult, error) {
	return s.scanLibrary(ctx, libraryID, false)
}

func (s *ScannerService) scanLibrary(ctx context.Context, libraryID string, autoScrape bool) (*ScanResult, error) {
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil || lib == nil {
		return nil, err
	}
	if mount, ok := ParseCloudLibraryMount(lib.Path); ok {
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
		return s.scanCloudLibrary(ctx, lib, mount, autoScrape)
	}
	res := &ScanResult{LibraryID: lib.ID}
	seen := make(map[string]struct{})
	seenInodes := make(map[string]string)

	walkFn := func(path string, info walkInfo) error {
		if info.isDir {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := videoExtensions[ext]; !ok {
			return nil
		}
		seen[filepath.Clean(path)] = struct{}{}
		s.ingestFile(ctx, lib, path, info.size, seenInodes, res)
		return nil
	}

	if err := walk(lib.Path, walkFn); err != nil {
		return res, err
	}
	removed, err := s.pruneMissingMedia(ctx, lib.ID, seen)
	if err != nil {
		s.log.Warn("prune missing media failed", zap.String("library_id", lib.ID), zap.Error(err))
	} else {
		res.Removed = removed
	}

	s.hub.Publish("scan", map[string]any{
		"library_id": lib.ID,
		"finished":   true,
		"visited":    res.Visited,
		"added":      res.Added,
		"updated":    res.Updated,
		"probed":     res.Probed,
		"local_meta": res.LocalMetadata,
		"removed":    res.Removed,
	})

	// Online enrichment is opt-in. Local NFO is always consumed first during
	// the scan, and matched rows are excluded from EnrichLibrary's pending set.
	if autoScrape && s.scraper != nil && s.scraper.AnyEnabled() && s.autoScrapeEnabled(ctx) {
		go func(libID string) {
			if _, err := s.scraper.EnrichLibrary(context.Background(), libID); err != nil {
				s.log.Warn("scraper enrich failed", zap.Error(err))
			}
		}(lib.ID)
	}
	return res, nil
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
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := videoExtensions[ext]; !ok {
		return false, nil
	}
	res := &ScanResult{LibraryID: lib.ID}
	s.ingestFile(ctx, lib, path, fi.Size(), make(map[string]string), res)
	return res.Added+res.Updated > 0, nil
}

func (s *ScannerService) scanCloudLibrary(ctx context.Context, lib *model.Library, mount CloudMountInfo, autoScrape bool) (*ScanResult, error) {
	res := &ScanResult{LibraryID: lib.ID}
	if s.storage == nil {
		return res, fmt.Errorf("cloud storage service unavailable")
	}
	typ := mount.Provider
	rootDir := mount.ScanDir
	rootDisplayDir := mount.DisplayDir
	type cloudCandidate struct {
		ref       string
		name      string
		size      int64
		path      string
		localMeta *LocalMetadata
	}
	seen := make(map[string]struct{})
	seenRefs := make(map[string]struct{})
	candidates := make([]cloudCandidate, 0, 256)
	candidateByKey := make(map[string]int)
	visitedDirs := map[string]struct{}{}
	startedAt := time.Now()
	lastProgress := time.Time{}
	dirsVisited := 0
	filesDiscovered := 0
	publishProgress := func(stage string, force bool) {
		if s.hub == nil {
			return
		}
		if !force && time.Since(lastProgress) < 2*time.Second {
			return
		}
		lastProgress = time.Now()
		elapsed := time.Since(startedAt)
		filesPerSecond := 0.0
		processed := filesDiscovered
		if res.Visited > processed {
			processed = res.Visited
		}
		if elapsed.Seconds() > 0 {
			filesPerSecond = float64(processed) / elapsed.Seconds()
		}
		s.hub.Publish("scan", map[string]any{
			"library_id":       lib.ID,
			"cloud":            true,
			"stage":            stage,
			"dirs":             dirsVisited,
			"discovered":       filesDiscovered,
			"visited":          res.Visited,
			"added":            res.Added,
			"updated":          res.Updated,
			"skipped":          res.Skipped,
			"elapsed_seconds":  int(elapsed.Seconds()),
			"files_per_second": filesPerSecond,
			"estimate_message": "云盘接口不提供总文件数，剩余时间会随目录大小和网盘响应速度变化",
		})
	}
	publishProgress("listing", true)
	var walkCloud func(dirID, displayDir string, inheritedMeta *LocalMetadata) error
	walkCloud = func(dirID, displayDir string, inheritedMeta *LocalMetadata) error {
		if _, ok := visitedDirs[dirID]; ok {
			return nil
		}
		visitedDirs[dirID] = struct{}{}
		entries, err := s.storage.CloudList(ctx, typ, dirID)
		if err != nil {
			if dirID != rootDir {
				res.Skipped++
				s.log.Warn("skip inaccessible cloud directory",
					zap.String("library_id", lib.ID),
					zap.String("provider", typ),
					zap.String("dir", dirID),
					zap.Error(err))
				return nil
			}
			return err
		}
		dirsVisited++
		publishProgress("listing", dirsVisited == 1 || dirsVisited%20 == 0)
		sidecars := newCloudSidecarSet(typ, entries)
		dirMeta := s.cloudDirectoryMetadata(ctx, typ, displayDir, sidecars, inheritedMeta)
		for _, entry := range entries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if entry.IsDir {
				if strings.TrimSpace(entry.ID) != "" {
					if err := walkCloud(entry.ID, joinCloudDisplayPath(displayDir, entry.Name), dirMeta); err != nil {
						return err
					}
				}
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name))
			if _, ok := videoExtensions[ext]; !ok {
				continue
			}
			ref := cloudEntryRef(typ, entry.ID, entry.PickCode)
			if ref == "" {
				res.Skipped++
				continue
			}
			if _, ok := seenRefs[ref]; ok {
				res.Skipped++
				continue
			}
			seenRefs[ref] = struct{}{}
			filesDiscovered++
			publishProgress("listing", filesDiscovered%100 == 0)
			displayPath := joinCloudDisplayPath(displayDir, entry.Name)
			path := cloudMediaPath(typ, displayPath)
			candidate := cloudCandidate{
				ref:       ref,
				name:      entry.Name,
				size:      entry.Size,
				path:      path,
				localMeta: s.cloudFileMetadata(ctx, typ, displayPath, entry.Name, sidecars, dirMeta, librarySupportsSeasons(lib)),
			}
			key := cloudMediaDedupeKey(lib, displayDir, entry.Name, entry.Size)
			if key != "" {
				if prevIndex, ok := candidateByKey[key]; ok {
					res.Skipped++
					if candidate.size > candidates[prevIndex].size {
						candidates[prevIndex] = candidate
					}
					continue
				}
				candidateByKey[key] = len(candidates)
			}
			candidates = append(candidates, candidate)
		}
		return nil
	}
	if err := walkCloud(rootDir, rootDisplayDir, nil); err != nil {
		return res, err
	}
	for _, candidate := range candidates {
		seen[candidate.path] = struct{}{}
		s.ingestCloudFile(ctx, lib, typ, candidate.ref, candidate.path, candidate.name, candidate.size, candidate.localMeta, res)
		publishProgress("importing", res.Visited == 1 || res.Visited%100 == 0)
	}
	removed, err := s.pruneMissingCloudMedia(ctx, lib.ID, seen)
	if err != nil {
		s.log.Warn("prune missing cloud media failed", zap.String("library_id", lib.ID), zap.Error(err))
	} else {
		res.Removed = removed
	}
	s.hub.Publish("scan", map[string]any{
		"library_id":      lib.ID,
		"finished":        true,
		"visited":         res.Visited,
		"added":           res.Added,
		"updated":         res.Updated,
		"removed":         res.Removed,
		"discovered":      filesDiscovered,
		"dirs":            dirsVisited,
		"elapsed_seconds": int(time.Since(startedAt).Seconds()),
		"cloud":           true,
	})
	if autoScrape && s.scraper != nil && s.scraper.AnyEnabled() && s.autoScrapeEnabled(ctx) {
		go func(libID string) {
			if _, err := s.scraper.EnrichLibrary(context.Background(), libID); err != nil {
				s.log.Warn("scraper enrich failed", zap.Error(err))
			}
		}(lib.ID)
	}
	return res, nil
}

func (s *ScannerService) shadowedCloudLibrary(ctx context.Context, lib *model.Library) *CloudMountConflict {
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		s.log.Warn("list libraries for cloud shadow check failed", zap.String("library_id", lib.ID), zap.Error(err))
		return nil
	}
	return CloudLibraryShadowed(libs, *lib)
}

func (s *ScannerService) ingestCloudFile(ctx context.Context, lib *model.Library, typ, ref, path, name string, size int64, localMeta *LocalMetadata, res *ScanResult) {
	res.Visited++
	ext := strings.ToLower(filepath.Ext(name))
	title, year := CleanQuery(name)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(name), ext)
	}
	if title == "" {
		title = ref
	}
	parsedSeason, parsedEpisode := ParseEpisode(path)
	if librarySupportsSeasons(lib) || parsedSeason > 0 || parsedEpisode > 0 {
		if seriesTitle, seriesYear := cloudSeriesTitleFromMediaPath(path); seriesTitle != "" {
			title = seriesTitle
			if seriesYear > 0 {
				year = seriesYear
			}
		}
	}
	isNewMedia := !s.mediaPathExists(ctx, path)
	m := &model.Media{
		LibraryID:    lib.ID,
		Title:        title,
		Year:         year,
		Path:         path,
		SizeBytes:    size,
		Container:    strings.TrimPrefix(ext, "."),
		STRMURL:      "/api/cloud/play/" + typ + "?ref=" + url.QueryEscape(ref),
		ScrapeStatus: "pending",
	}
	if ext == ".strm" {
		if targetURL, err := s.resolveCloudSTRMTarget(ctx, typ, ref); err == nil && targetURL != "" {
			m.STRMURL = targetURL
		} else if err != nil {
			s.log.Debug("read cloud strm failed", zap.String("ref", ref), zap.Error(err))
		}
	}
	m.SeasonNum = parsedSeason
	m.EpisodeNum = parsedEpisode
	if localMeta != nil {
		applyLocalMetadata(m, localMeta)
		res.LocalMetadata++
	}
	if err := s.repo.Media.Upsert(ctx, m); err != nil {
		s.log.Warn("upsert cloud media failed", zap.String("path", path), zap.Error(err))
		return
	}
	if isNewMedia {
		res.Added++
	} else {
		res.Updated++
	}
	s.hub.Publish("scan", map[string]any{
		"library_id": lib.ID,
		"path":       path,
		"visited":    res.Visited,
		"added":      res.Added,
		"updated":    res.Updated,
		"cloud":      true,
	})
}

func cloudSeriesTitleFromMediaPath(mediaPath string) (string, int) {
	displayPath := strings.TrimSpace(mediaPath)
	if strings.HasPrefix(strings.ToLower(displayPath), "cloud://") {
		rest := strings.TrimPrefix(displayPath, "cloud://")
		if idx := strings.Index(rest, "/"); idx >= 0 {
			displayPath = rest[idx+1:]
		} else {
			return "", 0
		}
	}
	displayPath = strings.Trim(strings.ReplaceAll(displayPath, "\\", "/"), "/")
	if displayPath == "" {
		return "", 0
	}
	parts := strings.Split(displayPath, "/")
	if len(parts) < 2 {
		return "", 0
	}
	dirs := parts[:len(parts)-1]
	if len(dirs) == 0 {
		return "", 0
	}
	base := strings.TrimSpace(dirs[len(dirs)-1])
	usedSeasonFolder := false
	if seasonFromDir(base) > 0 {
		usedSeasonFolder = true
		dirs = dirs[:len(dirs)-1]
		if len(dirs) == 0 {
			return "", 0
		}
		base = strings.TrimSpace(dirs[len(dirs)-1])
	}
	if base == "" || (!usedSeasonFolder && len(dirs) < 2) {
		return "", 0
	}
	title, year := CleanQuery(base)
	if title == "" {
		title = base
	}
	return strings.TrimSpace(title), year
}

// RemovePath deletes the media row for a path that has disappeared from disk
// (incremental delete used by the watcher on Remove/Rename events).
func (s *ScannerService) RemovePath(ctx context.Context, path string) (int64, error) {
	if _, err := os.Stat(path); err == nil {
		return 0, nil // still exists; nothing to remove
	}
	res := s.repo.DB.WithContext(ctx).
		Where("path = ?", path).
		Delete(&model.Media{})
	return res.RowsAffected, res.Error
}

// ingestFile upserts a single media file. seenInodes dedups hardlinks within a
// single scan; pass a fresh map for one-off ingests. It mutates res counters.
func (s *ScannerService) ingestFile(ctx context.Context, lib *model.Library, path string, size int64, seenInodes map[string]string, res *ScanResult) {
	res.Visited++
	ext := strings.ToLower(filepath.Ext(path))

	// Hardlink dedup: a seeding source kept by keep_seeding shares its inode
	// with the organized hardlink. Importing both would create duplicate rows
	// and double-count storage, so skip any file whose identity we've already
	// taken (within this scan or via an existing DB row pointing elsewhere).
	fileID, hasID := fileIdentity(path)
	if hasID {
		if first, ok := seenInodes[fileID]; ok && first != path {
			res.Skipped++
			s.log.Debug("scan skip hardlink duplicate",
				zap.String("path", path), zap.String("primary", first))
			return
		}
		if other, ok := s.duplicateByFileID(ctx, fileID, path); ok {
			res.Skipped++
			s.log.Debug("scan skip hardlink duplicate (existing)",
				zap.String("path", path), zap.String("primary", other))
			return
		}
		seenInodes[fileID] = path
	}

	isNewMedia := !s.mediaPathExists(ctx, path)

	title, year := CleanQuery(path)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), ext)
	}

	m := &model.Media{
		LibraryID: lib.ID,
		Title:     title,
		Year:      year,
		Path:      path,
		SizeBytes: size,
		Container: strings.TrimPrefix(ext, "."),
		FileID:    fileID,
	}

	parsedSeason, parsedEpisode := ParseEpisode(path)
	m.SeasonNum = parsedSeason
	m.EpisodeNum = parsedEpisode

	if local, err := ReadLocalMetadata(path, lib.Path, librarySupportsSeasons(lib) || parsedSeason > 0 || parsedEpisode > 0); err == nil && local != nil {
		applyLocalMetadata(m, local)
		res.LocalMetadata++
	} else if err != nil {
		s.log.Warn("read local metadata failed", zap.String("path", path), zap.Error(err))
	}

	// Best-effort ffprobe; failure does not abort the file.
	if s.probe != nil {
		if probe, err := s.probe.Probe(ctx, path); err == nil && probe != nil {
			m.DurationSec = probe.DurationSec
			m.Width = probe.Width
			m.Height = probe.Height
			m.VideoCodec = probe.VideoCodec
			m.AudioCodec = probe.AudioCodec
			if probe.Container != "" {
				m.Container = probe.Container
			}
			res.Probed++
		} else if err != nil {
			s.log.Debug("ffprobe failed", zap.String("path", path), zap.Error(err))
		}
	}

	if err := s.repo.Media.Upsert(ctx, m); err != nil {
		s.log.Warn("upsert media failed", zap.String("path", path), zap.Error(err))
		return
	}
	if isNewMedia {
		res.Added++
	} else {
		res.Updated++
	}
	s.hub.Publish("scan", map[string]any{
		"library_id": lib.ID,
		"path":       path,
		"visited":    res.Visited,
		"added":      res.Added,
		"updated":    res.Updated,
		"probed":     res.Probed,
		"local_meta": res.LocalMetadata,
	})
}

// duplicateByFileID reports an existing media path that shares the given inode
// identity but lives at a different path and still exists on disk.
func (s *ScannerService) duplicateByFileID(ctx context.Context, fileID, path string) (string, bool) {
	if fileID == "" {
		return "", false
	}
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Where("file_id = ? AND path <> ?", fileID, path).
		Limit(8).Find(&rows).Error; err != nil {
		return "", false
	}
	for _, r := range rows {
		if r.Path == "" {
			continue
		}
		if _, err := os.Stat(r.Path); err == nil {
			return r.Path, true
		}
	}
	return "", false
}

func (s *ScannerService) mediaPathExists(ctx context.Context, path string) bool {
	var count int64
	err := s.repo.DB.WithContext(ctx).Unscoped().Model(&model.Media{}).
		Where("path = ?", path).Count(&count).Error
	return err == nil && count > 0
}

func (s *ScannerService) pruneMissingMedia(ctx context.Context, libraryID string, seen map[string]struct{}) (int64, error) {
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Where("library_id = ?", libraryID).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	var removed int64
	for _, row := range rows {
		if row.Path == "" {
			continue
		}
		if _, ok := seen[filepath.Clean(row.Path)]; ok {
			continue
		}
		if _, err := os.Stat(row.Path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			continue
		}
		res := s.repo.DB.WithContext(ctx).
			Where("id = ?", row.ID).
			Delete(&model.Media{})
		if res.Error != nil {
			return removed, res.Error
		}
		removed += res.RowsAffected
	}
	return removed, nil
}

func (s *ScannerService) pruneMissingCloudMedia(ctx context.Context, libraryID string, seen map[string]struct{}) (int64, error) {
	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Where("library_id = ? AND path LIKE ?", libraryID, "cloud://%").
		Find(&rows).Error; err != nil {
		return 0, err
	}
	var removed int64
	for _, row := range rows {
		if _, ok := seen[row.Path]; ok {
			continue
		}
		res := s.repo.DB.WithContext(ctx).
			Where("id = ?", row.ID).
			Delete(&model.Media{})
		if res.Error != nil {
			return removed, res.Error
		}
		removed += res.RowsAffected
	}
	return removed, nil
}

func parseCloudLibraryPath(raw string) (typ, dirID string, ok bool) {
	info, ok := ParseCloudLibraryMount(raw)
	if !ok {
		return "", "", false
	}
	return info.Provider, info.ScanDir, true
}

func cloudEntryRef(typ, id, pickCode string) string {
	if typ == "cloud115" && strings.TrimSpace(pickCode) != "" {
		return strings.TrimSpace(pickCode)
	}
	return strings.TrimSpace(id)
}

func cloudMediaPath(typ, ref string) string {
	return "cloud://" + strings.TrimSpace(typ) + "/" + strings.TrimLeft(strings.TrimSpace(ref), "/")
}

func cloudMediaDedupeKey(lib *model.Library, dirID, name string, size int64) string {
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if base == "" {
		return ""
	}
	season, episode := ParseEpisode(name)
	title, year := CleanQuery(name)
	title = normalizeCloudDedupeText(title)
	if (season > 0 || episode > 0) && title != "" {
		return fmt.Sprintf("episode:%s:%s:%d:%d:%d", strings.ToLower(strings.TrimSpace(lib.Type)), title, year, season, episode)
	}
	if (season > 0 || episode > 0) && title == "" {
		return fmt.Sprintf("episode-dir:%s:%s:%d:%d:%d", strings.ToLower(strings.TrimSpace(lib.Type)), normalizeCloudDedupeText(dirID), season, episode, size)
	}
	return fmt.Sprintf("file:%s:%d", normalizeCloudDedupeText(base), size)
}

func normalizeCloudDedupeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case '.', '_', '-', ' ', '\t', '/', '\\', '[', ']', '(', ')':
			return true
		default:
			return false
		}
	})
	return strings.Join(fields, " ")
}

func (s *ScannerService) resolveCloudSTRMTarget(ctx context.Context, typ, ref string) (string, error) {
	if s.storage == nil {
		return "", nil
	}
	content, err := s.storage.CloudReadText(ctx, typ, ref, 64<<10)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(content, "\n") {
		candidate := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		u, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "webdav", "davs", "alist", "alists", "openlist", "openlists":
			return candidate, nil
		}
	}
	return "", nil
}

func applyLocalMetadata(m *model.Media, local *LocalMetadata) {
	if local.Title != "" {
		m.Title = local.Title
	}
	if local.OriginalName != "" {
		m.OriginalName = local.OriginalName
	}
	if local.AdultCode != "" {
		m.OriginalName = local.AdultCode
	}
	if local.Year > 0 {
		m.Year = local.Year
	}
	if local.Overview != "" {
		m.Overview = local.Overview
	}
	if local.Rating > 0 {
		m.Rating = local.Rating
	}
	if local.PosterURL != "" {
		m.PosterURL = local.PosterURL
	}
	if local.BackdropURL != "" {
		m.BackdropURL = local.BackdropURL
	}
	if local.TMDbID > 0 {
		m.TMDbID = local.TMDbID
	}
	if local.SeasonNum > 0 {
		m.SeasonNum = local.SeasonNum
	}
	if local.EpisodeNum > 0 {
		m.EpisodeNum = local.EpisodeNum
	}
	if local.Genres != "" {
		m.Genres = local.Genres
	}
	if local.Countries != "" {
		m.Countries = local.Countries
	}
	if local.Languages != "" {
		m.Languages = local.Languages
	}
	if local.NSFW {
		m.NSFW = true
	}
	if local.HasNFO || localHasDescriptiveMetadata(local) {
		m.ScrapeStatus = "matched"
	}
}

func localHasDescriptiveMetadata(local *LocalMetadata) bool {
	if local == nil {
		return false
	}
	return local.Title != "" ||
		local.OriginalName != "" ||
		local.AdultCode != "" ||
		local.Year > 0 ||
		local.Overview != "" ||
		local.Rating > 0 ||
		local.TMDbID > 0 ||
		local.Genres != "" ||
		local.Countries != "" ||
		local.Languages != ""
}

func (s *ScannerService) autoScrapeEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	value, err := s.repo.Setting.Get(ctx, "scrape.auto_on_scan")
	if err != nil {
		s.log.Warn("read scrape.auto_on_scan failed", zap.Error(err))
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

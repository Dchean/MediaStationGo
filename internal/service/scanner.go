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
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

	imageProxy *ImageProxy

	cloudScanMu             sync.Mutex
	cloudScans              map[string]*cloudScanEntry
	cloudSlots              chan struct{}
	cloudImagePrefetchOnce  sync.Once
	cloudImagePrefetchQueue chan cloudImagePrefetchTask
	cloudImagePrefetchMu    sync.Mutex
	cloudImagePrefetching   map[string]struct{}
	cloudMediaProbeOnce     sync.Once
	cloudMediaProbeQueue    chan cloudMediaProbeTask
	cloudMediaProbeMu       sync.Mutex
	cloudMediaProbing       map[string]struct{}
	cloudMediaProbeBackoff  map[string]time.Time
	cloudMediaProbeWarnMu   sync.Mutex
	cloudMediaProbeLastWarn time.Time
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
		probe:                   probe,
		scraper:                 scraper,
		cloudScans:              make(map[string]*cloudScanEntry),
		cloudSlots:              make(chan struct{}, 1),
		cloudImagePrefetchQueue: make(chan cloudImagePrefetchTask, 256),
		cloudImagePrefetching:   make(map[string]struct{}),
		cloudMediaProbeQueue:    make(chan cloudMediaProbeTask, 1024),
		cloudMediaProbing:       make(map[string]struct{}),
		cloudMediaProbeBackoff:  make(map[string]time.Time),
	}
}

// SetStorageConfig wires cloud-disk storage access into the scanner. It is set
// after service construction because StorageConfigService depends on Crypto,
// while the scanner is needed earlier by watcher/download services.
func (s *ScannerService) SetStorageConfig(storage *StorageConfigService) {
	s.storage = storage
	if storage != nil && s.probe != nil {
		s.cloudMediaProbeOnce.Do(func() {
			workers := normalizeFFprobeMaxConcurrent(s.cfg.App.FFprobeMaxConcurrent)
			for i := 0; i < workers; i++ {
				go s.cloudMediaProbeWorker()
			}
		})
	}
}

// SetImageProxy lets cloud scans warm sidecar poster/backdrop files into the
// local image cache. This keeps library opening fast without forcing the UI or
// Emby clients to resolve/download every cloud poster on demand.
func (s *ScannerService) SetImageProxy(imageProxy *ImageProxy) {
	s.imageProxy = imageProxy
	if imageProxy != nil {
		s.cloudImagePrefetchOnce.Do(func() {
			go s.cloudImagePrefetchWorker()
		})
	}
}

func (s *ScannerService) cloudImagePrefetchWorker() {
	for task := range s.cloudImagePrefetchQueue {
		s.prefetchCloudImage(task)
	}
}

func (s *ScannerService) queueCloudArtworkPrefetch(raw string) {
	if s == nil || s.storage == nil || s.imageProxy == nil {
		return
	}
	typ, ref, ok := parseCloudImagePlaybackURL(raw)
	if !ok {
		return
	}
	stableKey := typ + ":" + ref
	if s.imageProxy.CloudImageCached(stableKey) {
		return
	}
	s.cloudImagePrefetchMu.Lock()
	if _, ok := s.cloudImagePrefetching[stableKey]; ok {
		s.cloudImagePrefetchMu.Unlock()
		return
	}
	s.cloudImagePrefetching[stableKey] = struct{}{}
	s.cloudImagePrefetchMu.Unlock()

	task := cloudImagePrefetchTask{typ: typ, ref: ref, stableKey: stableKey}
	select {
	case s.cloudImagePrefetchQueue <- task:
	default:
		s.cloudImagePrefetchMu.Lock()
		delete(s.cloudImagePrefetching, stableKey)
		s.cloudImagePrefetchMu.Unlock()
		if s.log != nil {
			s.log.Debug("cloud artwork prefetch queue full", zap.String("provider", typ), zap.String("ref", ref))
		}
	}
}

func (s *ScannerService) prefetchCloudImage(task cloudImagePrefetchTask) {
	defer func() {
		s.cloudImagePrefetchMu.Lock()
		delete(s.cloudImagePrefetching, task.stableKey)
		s.cloudImagePrefetchMu.Unlock()
	}()
	if s == nil || s.storage == nil || s.imageProxy == nil || s.imageProxy.CloudImageCached(task.stableKey) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	link, err := s.storage.CloudResolve(ctx, task.typ, task.ref, "")
	if err != nil {
		if s.log != nil {
			s.log.Debug("resolve cloud artwork for prefetch failed", zap.String("provider", task.typ), zap.String("ref", task.ref), zap.Error(err))
		}
		return
	}
	if err := s.imageProxy.PrefetchCloudResolved(ctx, task.stableKey, link); err != nil && s.log != nil {
		s.log.Debug("prefetch cloud artwork failed", zap.String("provider", task.typ), zap.String("ref", task.ref), zap.Error(err))
	}
}

func (s *ScannerService) cacheCloudArtworkNow(ctx context.Context, raw string) {
	if s == nil || s.storage == nil || s.imageProxy == nil {
		return
	}
	typ, ref, ok := parseCloudImagePlaybackURL(raw)
	if !ok {
		return
	}
	stableKey := typ + ":" + ref
	if s.imageProxy.CloudImageCached(stableKey) {
		return
	}
	cacheCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	link, err := s.storage.CloudResolve(cacheCtx, typ, ref, "")
	if err != nil {
		if s.log != nil {
			s.log.Debug("resolve cloud artwork for priority cache failed", zap.String("provider", typ), zap.String("ref", ref), zap.Error(err))
		}
		s.queueCloudArtworkPrefetch(raw)
		return
	}
	if err := s.imageProxy.PrefetchCloudResolved(cacheCtx, stableKey, link); err != nil {
		if s.log != nil {
			s.log.Debug("priority cache cloud artwork failed", zap.String("provider", typ), zap.String("ref", ref), zap.Error(err))
		}
		s.queueCloudArtworkPrefetch(raw)
	}
}

func (s *ScannerService) cacheCloudMetadataArtworkNow(ctx context.Context, meta *LocalMetadata) {
	if meta == nil {
		return
	}
	s.cacheCloudArtworkNow(ctx, meta.PosterURL)
	s.cacheCloudArtworkNow(ctx, meta.BackdropURL)
}

func parseCloudImagePlaybackURL(raw string) (string, string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	const prefix = "api/cloud/play/"
	if !strings.HasPrefix(strings.ToLower(path), prefix) {
		return "", "", false
	}
	typ := strings.TrimSpace(path[len(prefix):])
	ref := strings.TrimSpace(u.Query().Get("ref"))
	if typ == "" || ref == "" || !isCloudArtworkRef(ref) {
		return "", "", false
	}
	return typ, ref, true
}

func isCloudArtworkRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, suffix := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp"} {
		if strings.HasSuffix(ref, suffix) {
			return true
		}
	}
	return false
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

var ErrCloudScanAlreadyRunning = errors.New("cloud scan already running")

const maxCloudMediaProbeQueuePerScan = 32

const cloudMediaProbeFailureBackoff = 6 * time.Hour

// cloudMediaProbeQueueFullBackoff 是探测队列饱和时给单个文件挂的短退避，
// 防止后续扫描轮次对同一批文件反复尝试入队。
const cloudMediaProbeQueueFullBackoff = 30 * time.Minute

// CloudScanStatus is the operator-facing state for long-running cloud scans.
type CloudScanStatus struct {
	LibraryID      string    `json:"library_id"`
	Provider       string    `json:"provider"`
	Stage          string    `json:"stage"`
	State          string    `json:"state"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	Dirs           int       `json:"dirs"`
	Discovered     int       `json:"discovered"`
	Visited        int       `json:"visited"`
	Added          int       `json:"added"`
	Updated        int       `json:"updated"`
	Skipped        int       `json:"skipped"`
	Removed        int64     `json:"removed"`
	Error          string    `json:"error,omitempty"`
	ResumeHint     string    `json:"resume_hint,omitempty"`
	Estimate       string    `json:"estimate_message,omitempty"`
	FilesPerSecond float64   `json:"files_per_second,omitempty"`
}

type cloudScanEntry struct {
	status CloudScanStatus
	cancel context.CancelFunc
}

type cloudImagePrefetchTask struct {
	typ       string
	ref       string
	stableKey string
}

type cloudMediaProbeTask struct {
	typ  string
	ref  string
	path string
}

type existingCloudMedia struct {
	SizeBytes   int64
	DurationSec int
	Width       int
	Height      int
	VideoCodec  string
	AudioCodec  string
	Container   string
	PosterURL   string
	BackdropURL string
	STRMURL     string
}

func (s *ScannerService) cloudMediaProbeWorker() {
	for task := range s.cloudMediaProbeQueue {
		s.probeCloudMediaAsync(task)
	}
}

func (s *ScannerService) queueCloudMediaProbe(typ, ref, path string) bool {
	if s == nil || s.storage == nil || s.probe == nil {
		return false
	}
	typ = strings.TrimSpace(typ)
	ref = strings.TrimSpace(ref)
	path = strings.TrimSpace(path)
	if typ == "" || ref == "" || path == "" {
		return false
	}
	s.cloudMediaProbeMu.Lock()
	if until, ok := s.cloudMediaProbeBackoff[path]; ok {
		if time.Now().Before(until) {
			s.cloudMediaProbeMu.Unlock()
			return false
		}
		delete(s.cloudMediaProbeBackoff, path)
	}
	if _, ok := s.cloudMediaProbing[path]; ok {
		s.cloudMediaProbeMu.Unlock()
		return false
	}
	s.cloudMediaProbing[path] = struct{}{}
	s.cloudMediaProbeMu.Unlock()

	task := cloudMediaProbeTask{typ: typ, ref: ref, path: path}
	select {
	case s.cloudMediaProbeQueue <- task:
		return true
	default:
		s.cloudMediaProbeMu.Lock()
		delete(s.cloudMediaProbing, path)
		// 队列满说明探测工人已饱和；给该文件挂一个短退避，避免下一轮
		// 扫描立刻重复尝试同一批文件。
		if s.cloudMediaProbeBackoff == nil {
			s.cloudMediaProbeBackoff = make(map[string]time.Time)
		}
		s.cloudMediaProbeBackoff[path] = time.Now().Add(cloudMediaProbeQueueFullBackoff)
		s.cloudMediaProbeMu.Unlock()
		if s.log != nil {
			// 限速告警：队列满在大库扫描中是常态而非异常，逐条 WARN 会
			// 在几小时内产生数万行日志（真实环境出现过 41165 条）。
			now := time.Now()
			s.cloudMediaProbeWarnMu.Lock()
			shouldWarn := now.Sub(s.cloudMediaProbeLastWarn) >= time.Minute
			if shouldWarn {
				s.cloudMediaProbeLastWarn = now
			}
			s.cloudMediaProbeWarnMu.Unlock()
			if shouldWarn {
				s.log.Warn("cloud media probe queue full; deferring remaining probes (logged at most once per minute)",
					zap.String("provider", typ), zap.String("path", path))
			} else {
				s.log.Debug("cloud media probe queue full", zap.String("provider", typ), zap.String("path", path))
			}
		}
		return false
	}
}

func (s *ScannerService) queueCloudMediaProbeWithBudget(typ, ref, path string, budget *int) bool {
	if budget != nil {
		if *budget <= 0 {
			return false
		}
		// 预算按「尝试」扣减而不是按「成功入队」扣减。否则当探测队列被
		// 其他扫描填满时，本次扫描会对剩下的每一个文件都尝试入队并各打
		// 一条日志——真实环境里曾因此产生过 4 万多条 "queue full" WARN，
		// 这本身就是一笔可观的 CPU/磁盘开销。
		*budget--
	}
	return s.queueCloudMediaProbe(typ, ref, path)
}

func (s *ScannerService) beginCloudScan(ctx context.Context, lib *model.Library, mount CloudMountInfo) (context.Context, func(*ScanResult, error), error) {
	if s == nil || lib == nil {
		return ctx, func(*ScanResult, error) {}, nil
	}
	s.cloudScanMu.Lock()
	if s.cloudScans == nil {
		s.cloudScans = make(map[string]*cloudScanEntry)
	}
	if entry := s.cloudScans[lib.ID]; entry != nil && (entry.status.State == "running" || entry.status.State == "canceling") {
		s.cloudScanMu.Unlock()
		return ctx, nil, ErrCloudScanAlreadyRunning
	}
	runCtx, cancel := context.WithCancel(ctx)
	now := time.Now()
	entry := &cloudScanEntry{
		status: CloudScanStatus{
			LibraryID:  lib.ID,
			Provider:   mount.Provider,
			Stage:      "listing",
			State:      "running",
			StartedAt:  now,
			UpdatedAt:  now,
			ResumeHint: "中断后再次点击扫描会从头遍历，但已入库媒体会去重更新，只补齐缺失项。",
			Estimate:   "小目录通常几十秒；几万文件的大目录可能需要数分钟到数小时，取决于网盘接口速度。",
		},
		cancel: cancel,
	}
	s.cloudScans[lib.ID] = entry
	s.cloudScanMu.Unlock()

	finish := func(res *ScanResult, err error) {
		s.cloudScanMu.Lock()
		defer s.cloudScanMu.Unlock()
		current := s.cloudScans[lib.ID]
		if current == nil {
			return
		}
		now := time.Now()
		if res != nil {
			current.status.Visited = res.Visited
			current.status.Added = res.Added
			current.status.Updated = res.Updated
			current.status.Skipped = res.Skipped
			current.status.Removed = res.Removed
		}
		current.status.UpdatedAt = now
		current.status.FinishedAt = now
		current.cancel = nil
		switch {
		case errors.Is(err, context.Canceled):
			current.status.State = "canceled"
			current.status.Stage = "canceled"
			current.status.Error = ""
		case errors.Is(err, context.DeadlineExceeded):
			current.status.State = "error"
			current.status.Stage = "error"
			current.status.Error = "扫描超时：" + err.Error()
		case err != nil:
			current.status.State = "error"
			current.status.Stage = "error"
			current.status.Error = err.Error()
		default:
			current.status.State = "finished"
			current.status.Stage = "finished"
			current.status.Error = ""
		}
		if s.hub != nil {
			s.hub.Publish("scan", map[string]any{
				"library_id": lib.ID,
				"provider":   mount.Provider,
				"cloud":      true,
				"finished":   true,
				"state":      current.status.State,
				"stage":      current.status.Stage,
				"error":      current.status.Error,
				"visited":    current.status.Visited,
				"added":      current.status.Added,
				"updated":    current.status.Updated,
				"skipped":    current.status.Skipped,
				"removed":    current.status.Removed,
			})
		}
	}
	return runCtx, finish, nil
}

func (s *ScannerService) updateCloudScanProgress(libraryID, stage string, dirs, discovered, visited, added, updated, skipped int, removed int64, filesPerSecond float64) {
	if s == nil {
		return
	}
	s.cloudScanMu.Lock()
	defer s.cloudScanMu.Unlock()
	entry := s.cloudScans[libraryID]
	if entry == nil {
		return
	}
	entry.status.Stage = stage
	entry.status.UpdatedAt = time.Now()
	entry.status.Dirs = dirs
	entry.status.Discovered = discovered
	entry.status.Visited = visited
	entry.status.Added = added
	entry.status.Updated = updated
	entry.status.Skipped = skipped
	entry.status.Removed = removed
	entry.status.FilesPerSecond = filesPerSecond
}

func (s *ScannerService) acquireCloudScanSlot(ctx context.Context, libraryID string) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	s.cloudScanMu.Lock()
	if s.cloudSlots == nil {
		s.cloudSlots = make(chan struct{}, 1)
	}
	slots := s.cloudSlots
	if entry := s.cloudScans[libraryID]; entry != nil {
		entry.status.Stage = "queued"
		entry.status.UpdatedAt = time.Now()
	}
	s.cloudScanMu.Unlock()

	select {
	case slots <- struct{}{}:
		s.cloudScanMu.Lock()
		if entry := s.cloudScans[libraryID]; entry != nil && entry.status.State == "running" {
			entry.status.Stage = "listing"
			entry.status.UpdatedAt = time.Now()
		}
		s.cloudScanMu.Unlock()
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CloudScanStatuses returns the current or most recent status per cloud library.
func (s *ScannerService) CloudScanStatuses() []CloudScanStatus {
	if s == nil {
		return nil
	}
	s.cloudScanMu.Lock()
	defer s.cloudScanMu.Unlock()
	out := make([]CloudScanStatus, 0, len(s.cloudScans))
	for _, entry := range s.cloudScans {
		out = append(out, entry.status)
	}
	return out
}

func (s *ScannerService) CancelCloudScan(libraryID string) bool {
	if s == nil || strings.TrimSpace(libraryID) == "" {
		return false
	}
	s.cloudScanMu.Lock()
	defer s.cloudScanMu.Unlock()
	entry := s.cloudScans[libraryID]
	if entry == nil || entry.cancel == nil || (entry.status.State != "running" && entry.status.State != "canceling") {
		return false
	}
	entry.status.State = "canceling"
	entry.status.Stage = "canceling"
	entry.status.UpdatedAt = time.Now()
	entry.cancel()
	return true
}

func (s *ScannerService) CancelAllCloudScans() int {
	if s == nil {
		return 0
	}
	s.cloudScanMu.Lock()
	defer s.cloudScanMu.Unlock()
	cancelled := 0
	for _, entry := range s.cloudScans {
		if entry == nil || entry.cancel == nil || (entry.status.State != "running" && entry.status.State != "canceling") {
			continue
		}
		entry.status.State = "canceling"
		entry.status.Stage = "canceling"
		entry.status.UpdatedAt = time.Now()
		entry.cancel()
		cancelled++
	}
	return cancelled
}

func (s *ScannerService) CancelCloudScansForProvider(provider string) int {
	if s == nil {
		return 0
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return 0
	}
	s.cloudScanMu.Lock()
	defer s.cloudScanMu.Unlock()
	cancelled := 0
	for _, entry := range s.cloudScans {
		if entry == nil || entry.status.Provider != provider || entry.cancel == nil || (entry.status.State != "running" && entry.status.State != "canceling") {
			continue
		}
		entry.status.State = "canceling"
		entry.status.Stage = "canceling"
		entry.status.UpdatedAt = time.Now()
		entry.cancel()
		cancelled++
	}
	return cancelled
}

func (s *ScannerService) StartCloudLibraryScan(libraryID string, autoScrape bool) (CloudScanStatus, bool, error) {
	if s == nil {
		return CloudScanStatus{}, false, errors.New("scanner unavailable")
	}
	lib, err := s.repo.Library.FindByID(context.Background(), libraryID)
	if err != nil {
		return CloudScanStatus{}, false, err
	}
	if lib == nil {
		return CloudScanStatus{}, false, errors.New("library not found")
	}
	mount, ok := ParseCloudLibraryMount(lib.Path)
	if !ok {
		return CloudScanStatus{}, false, errors.New("library is not a cloud mount")
	}
	s.cloudScanMu.Lock()
	if entry := s.cloudScans[libraryID]; entry != nil && (entry.status.State == "running" || entry.status.State == "canceling") {
		status := entry.status
		s.cloudScanMu.Unlock()
		return status, false, nil
	}
	s.cloudScanMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
		defer cancel()
		if autoScrape {
			_, err = s.ScanLibrary(ctx, libraryID)
		} else {
			_, err = s.ScanLibraryWithoutAutoScrape(ctx, libraryID)
		}
		if err != nil && !errors.Is(err, ErrCloudScanAlreadyRunning) && s.log != nil {
			s.log.Warn("cloud library background scan failed", zap.String("library_id", libraryID), zap.Error(err))
		}
	}()
	status := CloudScanStatus{
		LibraryID:  libraryID,
		Provider:   mount.Provider,
		Stage:      "queued",
		State:      "queued",
		StartedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		ResumeHint: "中断后再次点击扫描会从头遍历，但已入库媒体会去重更新，只补齐缺失项。",
		Estimate:   "小目录通常几十秒；几万文件的大目录可能需要数分钟到数小时，取决于网盘接口速度。",
	}
	return status, true, nil
}

func (s *ScannerService) StartAllCloudLibraryScans() ([]CloudScanStatus, error) {
	if s == nil {
		return nil, errors.New("scanner unavailable")
	}
	libs, err := s.repo.Library.List(context.Background())
	if err != nil {
		return nil, err
	}
	libs = FilterScannableCloudLibraries(context.Background(), s.repo, libs)
	statuses := make([]CloudScanStatus, 0, len(libs))
	for _, lib := range libs {
		if !lib.Enabled {
			continue
		}
		if _, ok := ParseCloudLibraryMount(lib.Path); !ok {
			continue
		}
		status, _, err := s.StartCloudLibraryScan(lib.ID, false)
		if err != nil {
			status = CloudScanStatus{LibraryID: lib.ID, State: "error", Error: err.Error(), UpdatedAt: time.Now()}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
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
	s.maybeGenerateSTRMAfterScan(lib.ID)

	// Online enrichment is opt-in. Local NFO is always consumed first during
	// the scan, and matched rows are excluded from EnrichLibrary's pending set.
	if autoScrape && s.scraper != nil && s.scraper.AnyEnabled() && s.autoScrapeEnabled(ctx) {
		s.startAutoScrape(ctx, lib.ID)
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

	// 验证存储配置是否存在且已启用
	cfg, err := s.repo.StorageConfig.Get(ctx, mount.Provider)
	if err != nil || cfg == nil {
		return res, fmt.Errorf("storage config not found: %s", mount.Provider)
	}
	if !cfg.Enabled {
		return res, fmt.Errorf("storage %s is disabled", mount.Provider)
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
		s.updateCloudScanProgress(lib.ID, stage, dirsVisited, filesDiscovered, res.Visited, res.Added, res.Updated, res.Skipped, res.Removed, filesPerSecond)
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
		s.cacheCloudMetadataArtworkNow(ctx, dirMeta)
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
			localMeta := s.cloudFileMetadata(ctx, typ, displayPath, entry.Name, sidecars, dirMeta, librarySupportsSeasons(lib))
			// 每个文件的海报/背景图改走后台预取队列。此前这里是同步
			// CloudResolve+下载（每张最多 20s 超时），几千个文件的云盘库
			// 扫描会变成持续数小时的串行下载，把 CPU/带宽长期吃满。
			if localMeta != nil {
				s.queueCloudArtworkPrefetch(localMeta.PosterURL)
				s.queueCloudArtworkPrefetch(localMeta.BackdropURL)
			}
			candidate := cloudCandidate{
				ref:       ref,
				name:      entry.Name,
				size:      entry.Size,
				path:      path,
				localMeta: localMeta,
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
	existingMedia, err := s.existingCloudMediaSnapshot(ctx, lib.ID)
	if err != nil {
		s.log.Warn("load existing cloud media snapshot failed", zap.String("library_id", lib.ID), zap.Error(err))
		existingMedia = nil
	}
	if existingMedia != nil {
		priority := func(candidate cloudCandidate) int {
			existing, ok := existingMedia[candidate.path]
			if !ok {
				return 2
			}
			if cloudTrackMetadataMissing(existing) || cloudMetadataNeedsRefresh(existing, candidate.localMeta) {
				return 0
			}
			return 1
		}
		sort.SliceStable(candidates, func(i, j int) bool {
			return priority(candidates[i]) < priority(candidates[j])
		})
	}
	probeBudget := maxCloudMediaProbeQueuePerScan
	for _, candidate := range candidates {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		seen[candidate.path] = struct{}{}
		s.ingestCloudFile(ctx, lib, typ, candidate.ref, candidate.path, candidate.name, candidate.size, candidate.localMeta, existingMedia, &probeBudget, res)
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
	s.maybeGenerateSTRMAfterScan(lib.ID)
	if autoScrape && s.scraper != nil && s.scraper.AnyEnabled() && s.autoScrapeEnabled(ctx) {
		s.startAutoScrape(ctx, lib.ID)
	}
	return res, nil
}

func (s *ScannerService) startAutoScrape(ctx context.Context, libraryID string) {
	scrapeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
	go func() {
		defer cancel()
		if _, err := s.scraper.EnrichLibrary(scrapeCtx, libraryID); err != nil {
			s.log.Warn("scraper enrich failed", zap.Error(err))
		}
	}()
}

func (s *ScannerService) existingCloudMediaSnapshot(ctx context.Context, libraryID string) (map[string]existingCloudMedia, error) {
	var rows []struct {
		Path        string
		SizeBytes   int64
		DurationSec int
		Width       int
		Height      int
		VideoCodec  string
		AudioCodec  string
		Container   string
		PosterURL   string
		BackdropURL string
		STRMURL     string
	}
	if err := s.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("path, size_bytes, duration_sec, width, height, video_codec, audio_codec, container, poster_url, backdrop_url, strm_url").
		Where("library_id = ? AND path LIKE ?", libraryID, "cloud://%").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]existingCloudMedia, len(rows))
	for _, row := range rows {
		if row.Path != "" {
			out[row.Path] = existingCloudMedia{
				SizeBytes:   row.SizeBytes,
				DurationSec: row.DurationSec,
				Width:       row.Width,
				Height:      row.Height,
				VideoCodec:  row.VideoCodec,
				AudioCodec:  row.AudioCodec,
				Container:   row.Container,
				PosterURL:   row.PosterURL,
				BackdropURL: row.BackdropURL,
				STRMURL:     row.STRMURL,
			}
		}
	}
	return out, nil
}

func (s *ScannerService) shadowedCloudLibrary(ctx context.Context, lib *model.Library) *CloudMountConflict {
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		s.log.Warn("list libraries for cloud shadow check failed", zap.String("library_id", lib.ID), zap.Error(err))
		return nil
	}
	visible := FilterScannableCloudLibraries(ctx, s.repo, libs)
	for _, kept := range visible {
		if kept.ID == lib.ID {
			return nil
		}
	}
	current, ok := ParseCloudLibraryMount(lib.Path)
	if ok {
		currentKey, _ := cloudLibraryDisplayKey(*lib)
		for _, kept := range visible {
			info, ok := ParseCloudLibraryMount(kept.Path)
			if !ok || info.Provider != current.Provider {
				continue
			}
			keptKey, _ := cloudLibraryDisplayKey(kept)
			exact := currentKey != "" && currentKey == keptKey
			return &CloudMountConflict{
				Library:            kept,
				Exact:              exact,
				Nested:             !exact,
				ExistingIsAncestor: cloudMountAncestor(info.DisplayDir, current.DisplayDir),
			}
		}
	}
	return CloudLibraryShadowed(libs, *lib)
}

func (s *ScannerService) ingestCloudFile(ctx context.Context, lib *model.Library, typ, ref, path, name string, size int64, localMeta *LocalMetadata, existingMedia map[string]existingCloudMedia, probeBudget *int, res *ScanResult) {
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
	expectedSTRMURL := BuildPublicAPIURL(ctx, s.repo, s.cfg, "/api/cloud/play/"+typ, url.Values{"ref": []string{ref}})
	isNewMedia := false
	needsTrackProbe := true
	if existingMedia != nil {
		existing, exists := existingMedia[path]
		isNewMedia = !exists
		needsTrackProbe = !exists || cloudTrackMetadataMissing(existing)
		if exists && existing.SizeBytes == size && existing.STRMURL == expectedSTRMURL && !cloudMetadataNeedsRefresh(existing, localMeta) {
			if needsTrackProbe && ext != ".strm" {
				s.queueCloudMediaProbeWithBudget(typ, ref, path, probeBudget)
			}
			res.Skipped++
			return
		}
	} else {
		isNewMedia = !s.mediaPathExists(ctx, path)
	}
	m := &model.Media{
		LibraryID:    lib.ID,
		Title:        title,
		Year:         year,
		Path:         path,
		SizeBytes:    size,
		Container:    strings.TrimPrefix(ext, "."),
		STRMURL:      expectedSTRMURL,
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
		s.queueCloudArtworkPrefetch(localMeta.PosterURL)
		s.queueCloudArtworkPrefetch(localMeta.BackdropURL)
	}
	if err := s.repo.Media.Upsert(ctx, m); err != nil {
		s.log.Warn("upsert cloud media failed", zap.String("path", path), zap.Error(err))
		return
	}
	if needsTrackProbe && ext != ".strm" {
		s.queueCloudMediaProbeWithBudget(typ, ref, path, probeBudget)
	}
	if isNewMedia {
		res.Added++
	} else {
		res.Updated++
	}
	if s.hub != nil && (res.Visited == 1 || res.Visited%100 == 0) {
		s.hub.Publish("scan", map[string]any{
			"library_id": lib.ID,
			"path":       path,
			"visited":    res.Visited,
			"added":      res.Added,
			"updated":    res.Updated,
			"cloud":      true,
		})
	}
}

func (s *ScannerService) probeCloudMediaAsync(task cloudMediaProbeTask) {
	defer func() {
		s.cloudMediaProbeMu.Lock()
		delete(s.cloudMediaProbing, task.path)
		s.cloudMediaProbeMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	probe, err := s.probeCloudFileMetadata(ctx, task.typ, task.ref)
	if err != nil {
		if s.log != nil {
			s.log.Debug("cloud media async probe failed", zap.String("provider", task.typ), zap.String("path", task.path), zap.Error(err))
		}
		s.cloudMediaProbeMu.Lock()
		if s.cloudMediaProbeBackoff == nil {
			s.cloudMediaProbeBackoff = make(map[string]time.Time)
		}
		s.cloudMediaProbeBackoff[task.path] = time.Now().Add(cloudMediaProbeFailureBackoff)
		s.cloudMediaProbeMu.Unlock()
		return
	}
	updates := probeResultUpdates(probe)
	if len(updates) == 0 {
		return
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).Where("path = ?", task.path).Updates(updates).Error; err != nil {
		if s.log != nil {
			s.log.Debug("update cloud media track metadata failed", zap.String("path", task.path), zap.Error(err))
		}
		return
	}
	s.cloudMediaProbeMu.Lock()
	delete(s.cloudMediaProbeBackoff, task.path)
	s.cloudMediaProbeMu.Unlock()
	if s.hub != nil {
		s.hub.Publish("scan", map[string]any{
			"path":          task.path,
			"cloud":         true,
			"track_probed":  true,
			"duration_sec":  probe.DurationSec,
			"video_codec":   probe.VideoCodec,
			"audio_codec":   probe.AudioCodec,
			"width":         probe.Width,
			"height":        probe.Height,
			"probe_message": "云盘媒体轨道元数据已后台补齐",
		})
	}
}

func (s *ScannerService) probeCloudFileMetadata(ctx context.Context, typ, ref string) (*ProbeResult, error) {
	if s == nil || s.probe == nil || s.storage == nil {
		return nil, errors.New("cloud probe unavailable")
	}
	link, err := s.storage.CloudResolve(ctx, typ, ref, "")
	if err != nil {
		return nil, err
	}
	return s.probe.ProbeHTTP(ctx, link.URL, link.Headers)
}

func probeResultUpdates(probe *ProbeResult) map[string]any {
	updates := map[string]any{}
	if probe == nil {
		return updates
	}
	if probe.DurationSec > 0 {
		updates["duration_sec"] = probe.DurationSec
	}
	if probe.Width > 0 {
		updates["width"] = probe.Width
	}
	if probe.Height > 0 {
		updates["height"] = probe.Height
	}
	if strings.TrimSpace(probe.VideoCodec) != "" {
		updates["video_codec"] = probe.VideoCodec
	}
	if strings.TrimSpace(probe.AudioCodec) != "" {
		updates["audio_codec"] = probe.AudioCodec
	}
	if probe.Container != "" {
		updates["container"] = probe.Container
	}
	return updates
}

func cloudMetadataNeedsRefresh(existing existingCloudMedia, localMeta *LocalMetadata) bool {
	if localMeta == nil {
		return false
	}
	if strings.TrimSpace(localMeta.PosterURL) != "" && strings.TrimSpace(existing.PosterURL) == "" {
		return true
	}
	if strings.TrimSpace(localMeta.BackdropURL) != "" && strings.TrimSpace(existing.BackdropURL) == "" {
		return true
	}
	return false
}

func cloudTrackMetadataMissing(existing existingCloudMedia) bool {
	return existing.DurationSec <= 0 ||
		existing.Width <= 0 ||
		existing.Height <= 0 ||
		strings.TrimSpace(existing.VideoCodec) == "" ||
		strings.TrimSpace(existing.AudioCodec) == ""
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
	if ext == ".strm" {
		m.Container = "strm"
		if targetURL, err := readLocalSTRMTarget(path); err == nil && targetURL != "" {
			m.STRMURL = targetURL
		} else if err != nil {
			s.log.Debug("read local strm failed", zap.String("path", path), zap.Error(err))
		}
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
	// 只取 id/path，并把删除按批提交：此前整表载入完整 Media 结构体、
	// 每行一条 DELETE，大库 prune 既费内存又长期占用写锁。
	var rows []struct {
		ID   string
		Path string
	}
	if err := s.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("id, path").
		Where("library_id = ?", libraryID).
		Find(&rows).Error; err != nil {
		return 0, err
	}
	stale := make([]string, 0)
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
		stale = append(stale, row.ID)
	}
	return s.deleteMediaByIDs(ctx, stale, false)
}

// deleteMediaByIDs removes media rows in fixed-size batches so each write
// transaction stays short and the global write gate is released frequently.
func (s *ScannerService) deleteMediaByIDs(ctx context.Context, ids []string, hard bool) (int64, error) {
	const batch = 500
	var removed int64
	for i := 0; i < len(ids); i += batch {
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		q := s.repo.DB.WithContext(ctx)
		if hard {
			q = q.Unscoped()
		}
		res := q.Where("id IN ?", ids[i:end]).Delete(&model.Media{})
		if res.Error != nil {
			return removed, res.Error
		}
		removed += res.RowsAffected
	}
	return removed, nil
}

func (s *ScannerService) pruneMissingCloudMedia(ctx context.Context, libraryID string, seen map[string]struct{}) (int64, error) {
	var rows []struct {
		ID   string
		Path string
	}
	if err := s.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("id, path").
		Where("library_id = ? AND path LIKE ?", libraryID, "cloud://%").
		Find(&rows).Error; err != nil {
		return 0, err
	}
	stale := make([]string, 0)
	for _, row := range rows {
		if _, ok := seen[row.Path]; ok {
			continue
		}
		stale = append(stale, row.ID)
	}
	return s.deleteMediaByIDs(ctx, stale, true)
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

func readLocalSTRMTarget(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is a discovered .strm file under the configured library root.
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		candidate := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		if strings.HasPrefix(candidate, "/api/") || strings.HasPrefix(candidate, "/Videos/") || strings.HasPrefix(candidate, "/videos/") {
			return candidate, nil
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

func (s *ScannerService) maybeGenerateSTRMAfterScan(libraryID string) {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return
	}
	value, err := s.repo.Setting.Get(context.Background(), "strm.auto_generate_enabled")
	if err != nil || !parseBoolSetting(value, false) {
		return
	}
	go func() {
		strmSvc := NewSTRMService(s.log, s.repo, s.cfg)
		if _, err := strmSvc.GenerateForLibrary(context.Background(), GenerateSTRMOptions{
			LibraryID:    libraryID,
			Enabled:      true,
			IncludeLocal: true,
		}); err != nil && s.log != nil {
			s.log.Warn("auto generate strm failed", zap.String("library_id", libraryID), zap.Error(err))
		}
	}()
}

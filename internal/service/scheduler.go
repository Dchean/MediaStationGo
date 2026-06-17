// Package service — periodic scheduled jobs.
//
// SchedulerService runs five recurring background jobs that keep the
// library up-to-date without operator intervention:
//
//	library_scan      every 60 min — re-scan every enabled library so
//	                                  newly-copied files are picked up.
//	subscription_pull every 30 min — re-poll RSS feeds (in addition to
//	                                  the existing SubscriptionService
//	                                  internal timer).
//	download_sync     every 30 s   — refresh the qBittorrent torrent
//	                                  list (already covered by the
//	                                  download poller, kept here as a
//	                                  watchdog).
//	transcode_cleanup every 24 h   — purge HLS transcode artefacts
//	                                  older than 24 h.
//	recycle_purge     every 24 h   — empty the recycle bin of rows
//	                                  soft-deleted more than 30 days
//	                                  ago.
//
// Each job runs at most once at a time (an in-flight run blocks the
// next tick). All work happens on a long-lived background context so
// the operator can keep clicking around the UI while the watchdog runs.
package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// SchedulerService runs the periodic jobs.
type SchedulerService struct {
	log              *zap.Logger
	repo             *repository.Container
	scanner          *ScannerService
	transcoder       *TranscoderService
	organizer        *OrganizerService
	organizePipeline *OrganizePipelineService
	storageCfg       *StorageConfigService
	hub              *Hub
	tasks            *TaskTrackerService
	cacheDir         string
	now              func() time.Time

	mu     sync.Mutex
	stopCh chan struct{}
	jobs   []*scheduledJob
}

var (
	ErrSchedulerJobNotFound       = errors.New("scheduled job not found")
	ErrSchedulerJobAlreadyRunning = errors.New("scheduled job already running")
)

func (s *SchedulerService) SetTaskTracker(tasks *TaskTrackerService) {
	s.tasks = tasks
}

func (s *SchedulerService) SetOrganizePipeline(pipeline *OrganizePipelineService) {
	s.organizePipeline = pipeline
}

// scheduledJob is one recurring task.
type scheduledJob struct {
	name     string
	interval time.Duration
	run      func(ctx context.Context) error
	lastRun  time.Time
	lastErr  string
	running  bool
	started  time.Time
}

type schedulerManualRunKey struct{}

const (
	cloudAutoSyncEnabledKey        = "cloud.auto_sync_enabled"
	cloudSyncIntervalSecondsKey    = "cloud.sync_interval_seconds"
	cloudLastAutoSyncDateKey       = "cloud.last_auto_sync_date"
	cloudAutoSyncWindowStartHour   = 19
	cloudAutoSyncWindowEndHour     = 21
	cloudAutoSyncCompletedDateForm = "2006-01-02"
)

// NewSchedulerService is the constructor.
func NewSchedulerService(
	log *zap.Logger,
	repo *repository.Container,
	scanner *ScannerService,
	transcoder *TranscoderService,
	organizer *OrganizerService,
	storageCfg *StorageConfigService,
	hub *Hub,
	cacheDir string,
) *SchedulerService {
	return &SchedulerService{
		log:        log,
		repo:       repo,
		scanner:    scanner,
		transcoder: transcoder,
		organizer:  organizer,
		storageCfg: storageCfg,
		hub:        hub,
		cacheDir:   cacheDir,
		now:        time.Now,
		stopCh:     make(chan struct{}),
	}
}

// Start kicks off every job in its own goroutine and returns immediately.
func (s *SchedulerService) Start(ctx context.Context) {
	s.jobs = []*scheduledJob{
		{
			name:     "library_scan",
			interval: 60 * time.Minute,
			run:      s.jobScanLibraries,
		},
		{
			name:     "cloud_sync",
			interval: s.cloudSyncInterval(ctx),
			run:      s.jobSyncCloudLibraries,
		},
		{
			name:     "cloud_upload",
			interval: s.cloudUploadInterval(ctx),
			run:      s.jobUploadLocalToCloud,
		},
		{
			name:     "organize_source",
			interval: s.organizeSourceInterval(ctx),
			run:      s.jobOrganizeSource,
		},
		{
			name:     "transcode_cleanup",
			interval: 24 * time.Hour,
			run:      s.jobCleanTranscodeCache,
		},
		{
			name:     "recycle_purge",
			interval: 24 * time.Hour,
			run:      s.jobPurgeRecycleBin,
		},
	}
	for _, j := range s.jobs {
		initialDelay := 15 * time.Second
		if j.name == "library_scan" || j.name == "organize_source" {
			// 重启后不立即整库重扫/整理下载目录：更新窗口恰是登录高峰，
			// 15 秒即全量 walk + ffprobe 曾把 CPU/磁盘打满导致无法登录。
			// 首轮等满一个完整周期再跑，平时节奏不变。
			initialDelay = j.interval
		}
		go s.loopWithInitialDelay(ctx, j, initialDelay)
	}
}

// Stop signals every job loop to exit on the next tick.
func (s *SchedulerService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stopCh:
		// already closed
	default:
		close(s.stopCh)
	}
}

// JobStatus is a snapshot suitable for the admin UI.
type JobStatus struct {
	Name     string    `json:"name"`
	Interval string    `json:"interval"`
	LastRun  time.Time `json:"last_run,omitempty"`
	LastErr  string    `json:"last_err,omitempty"`
	Running  bool      `json:"running,omitempty"`
	Started  time.Time `json:"started_at,omitempty"`
}

// Status returns the current state of every registered job.
func (s *SchedulerService) Status() []JobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobStatus, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, JobStatus{
			Name:     j.name,
			Interval: j.interval.String(),
			LastRun:  j.lastRun,
			LastErr:  j.lastErr,
			Running:  j.running,
			Started:  j.started,
		})
	}
	return out
}

// RunNow triggers a single run of the named job synchronously.
func (s *SchedulerService) RunNow(ctx context.Context, name string) error {
	j := s.jobByName(name)
	if j == nil {
		return ErrSchedulerJobNotFound
	}
	return s.runOnce(context.WithValue(ctx, schedulerManualRunKey{}, true), j)
}

// RunNowAsync triggers a named job in the background and returns immediately.
// The job is detached from the HTTP request cancellation so a browser timeout,
// route change, or reverse-proxy disconnect cannot kill long organize/scan work.
func (s *SchedulerService) RunNowAsync(ctx context.Context, name string) error {
	j := s.jobByName(name)
	if j == nil {
		return ErrSchedulerJobNotFound
	}
	runCtx := context.Background()
	if ctx != nil {
		runCtx = context.WithoutCancel(ctx)
	}
	runCtx = context.WithValue(runCtx, schedulerManualRunKey{}, true)
	if err := s.beginRun(j); err != nil {
		return err
	}
	go func() {
		if err := s.runReserved(runCtx, j); err != nil && s.log != nil {
			s.log.Warn("manual scheduled job failed", zap.String("name", name), zap.Error(err))
		}
	}()
	return nil
}

func (s *SchedulerService) loop(ctx context.Context, j *scheduledJob) {
	s.loopWithInitialDelay(ctx, j, 15*time.Second)
}

func (s *SchedulerService) loopWithInitialDelay(ctx context.Context, j *scheduledJob, initialDelay time.Duration) {
	delay := initialDelay
	for {
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-s.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
		if err := s.runOnce(ctx, j); err != nil {
			s.log.Warn("scheduled job failed",
				zap.String("name", j.name), zap.Error(err))
		}
		delay = j.interval
	}
}

func (s *SchedulerService) runOnce(ctx context.Context, j *scheduledJob) error {
	if err := s.beginRun(j); err != nil {
		return err
	}
	return s.runReserved(ctx, j)
}

func (s *SchedulerService) jobByName(name string) *scheduledJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.name == name {
			return j
		}
	}
	return nil
}

func (s *SchedulerService) beginRun(j *scheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.running {
		return ErrSchedulerJobAlreadyRunning
	}
	j.running = true
	j.started = s.currentTime()
	return nil
}

func (s *SchedulerService) runReserved(ctx context.Context, j *scheduledJob) error {
	err := j.run(ctx)
	s.mu.Lock()
	j.lastRun = s.currentTime()
	if err != nil {
		j.lastErr = err.Error()
	} else {
		j.lastErr = ""
	}
	j.running = false
	j.started = time.Time{}
	lastErr := j.lastErr
	s.mu.Unlock()
	if s.hub != nil {
		s.hub.Publish("scheduler", map[string]any{
			"name":  j.name,
			"ok":    err == nil,
			"error": lastErr,
		})
	}
	return err
}

// jobScanLibraries re-walks every enabled library.
//
// 默认关闭：文件变更由 WatcherService 增量入库，无需周期性全量重扫。
// 仅当用户在设置中显式开启 scan.periodic_enabled 时才执行整库重扫，
// 避免对硬盘的高频反复读取造成损伤（用户明确要求）。
func (s *SchedulerService) jobScanLibraries(ctx context.Context) error {
	if !s.periodicScanEnabled(ctx) {
		return nil
	}
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return err
	}
	for _, l := range libs {
		if !l.Enabled {
			continue
		}
		if _, ok := ParseCloudLibraryMount(l.Path); ok {
			// 云盘库由 cloud_sync 任务在夜间窗口低频同步；周期性整库
			// 重扫只面向本地磁盘库。否则十几个云盘库每小时全量遍历
			// 会把 CPU/网络长期吃满，还会占住唯一的云扫描槽位，让
			// 手动扫描看起来一直"卡死"在排队。
			continue
		}
		if _, err := s.scanner.ScanLibrary(ctx, l.ID); err != nil {
			s.log.Warn("scheduled scan failed",
				zap.String("library", l.ID), zap.Error(err))
		}
	}
	return nil
}

// jobUploadLocalToCloud copies local media files into the configured external
// storage backend. It is opt-in and never deletes the local source files.
func (s *SchedulerService) jobUploadLocalToCloud(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	if s.storageCfg == nil || (!manual && !s.autoCloudUploadEnabled(ctx)) {
		return nil
	}
	input := s.cloudUploadInput(ctx)
	if strings.TrimSpace(input.Type) == "" || strings.TrimSpace(input.SourcePath) == "" {
		return nil
	}
	res, err := s.storageCfg.UploadLocal(ctx, input)
	if s.log != nil && res != nil {
		s.log.Info("cloud upload finished",
			zap.String("type", input.Type),
			zap.String("source", res.SourcePath),
			zap.String("dest", res.DestPath),
			zap.Int("uploaded", res.Uploaded),
			zap.Int("skipped", res.Skipped),
			zap.Int64("bytes", res.Bytes),
			zap.Int("errors", len(res.Errors)),
		)
	}
	return err
}

func (s *SchedulerService) cloudUploadInput(ctx context.Context) CloudUploadInput {
	get := func(key string) string {
		if s.repo == nil || s.repo.Setting == nil {
			return ""
		}
		v, _ := s.repo.Setting.Get(ctx, key)
		return strings.TrimSpace(v)
	}
	return CloudUploadInput{
		Type:            get(CloudUploadProviderKey),
		SourcePath:      get(CloudUploadSourceDirKey),
		DestPath:        get(CloudUploadDestPathKey),
		Recursive:       parseBoolSetting(get(CloudUploadRecursiveKey), true),
		IncludeSidecars: parseBoolSetting(get(CloudUploadSidecarsKey), true),
		Overwrite:       parseBoolSetting(get(CloudUploadOverwriteKey), false),
		TransferMode:    get(CloudUploadTransferModeKey),
	}
}

func (s *SchedulerService) autoCloudUploadEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, CloudUploadAutoEnabledKey)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

func (s *SchedulerService) cloudUploadInterval(ctx context.Context) time.Duration {
	const fallback = time.Hour
	if s.repo == nil || s.repo.Setting == nil {
		return fallback
	}
	v, err := s.repo.Setting.Get(ctx, CloudUploadIntervalSecondsKey)
	if err != nil {
		return fallback
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || seconds <= 0 {
		return fallback
	}
	if seconds < 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

// jobSyncCloudLibraries keeps mounted cloud:// libraries refreshed without
// enabling full disk scans. It imports remote cloud files as STRM-backed media
// rows; the actual bytes stay on the provider and playback continues through
// /api/cloud/play 302/proxy.
func (s *SchedulerService) jobSyncCloudLibraries(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	if s.scanner == nil || (!manual && !s.autoCloudSyncDue(ctx, s.currentTime())) {
		return nil
	}
	libs, err := s.repo.Library.List(ctx)
	if err != nil {
		return err
	}
	libs = FilterScannableCloudLibraries(ctx, s.repo, libs)
	var firstErr error
	for _, l := range libs {
		if !l.Enabled {
			continue
		}
		if _, ok := ParseCloudLibraryMount(l.Path); !ok {
			continue
		}
		if _, err := s.scanner.ScanLibraryWithoutAutoScrape(ctx, l.ID); err != nil {
			s.log.Warn("cloud sync failed", zap.String("library", l.ID), zap.Error(err))
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if !manual {
		_ = s.markCloudAutoSyncCompleted(ctx, s.currentTime())
	}
	return nil
}

func (s *SchedulerService) autoCloudSyncEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, cloudAutoSyncEnabledKey)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

func (s *SchedulerService) autoCloudSyncDue(ctx context.Context, now time.Time) bool {
	if !s.autoCloudSyncEnabled(ctx) || !cloudAutoSyncInWindow(now) {
		return false
	}
	if s.repo == nil || s.repo.Setting == nil {
		return true
	}
	last, err := s.repo.Setting.Get(ctx, cloudLastAutoSyncDateKey)
	if err != nil {
		return true
	}
	return strings.TrimSpace(last) != now.Format(cloudAutoSyncCompletedDateForm)
}

func cloudAutoSyncInWindow(now time.Time) bool {
	hour := now.In(time.Local).Hour()
	return hour >= cloudAutoSyncWindowStartHour && hour < cloudAutoSyncWindowEndHour
}

func (s *SchedulerService) markCloudAutoSyncCompleted(ctx context.Context, now time.Time) error {
	if s.repo == nil || s.repo.Setting == nil {
		return nil
	}
	return s.repo.Setting.Set(ctx, cloudLastAutoSyncDateKey, now.Format(cloudAutoSyncCompletedDateForm))
}

func (s *SchedulerService) cloudSyncInterval(ctx context.Context) time.Duration {
	const fallback = 30 * time.Minute
	if s.repo == nil || s.repo.Setting == nil {
		return fallback
	}
	v, err := s.repo.Setting.Get(ctx, cloudSyncIntervalSecondsKey)
	if err != nil {
		return fallback
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || seconds <= 0 {
		return fallback
	}
	if seconds < 300 {
		seconds = 300
	}
	return time.Duration(seconds) * time.Second
}

func (s *SchedulerService) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// periodicScanEnabled reports whether the operator opted into periodic full
// library re-scans. Defaults to false so the incremental watcher is the only
// thing touching the disk under normal operation.
func (s *SchedulerService) periodicScanEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, "scan.periodic_enabled")
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

// jobOrganizeSource periodically organizes the configured staging/download
// source directory into the configured media destination. It is intentionally
// opt-in: manual file management remains available, but background disk walking
// only starts after the operator enables organize.auto.
func (s *SchedulerService) jobOrganizeSource(ctx context.Context) error {
	manual, _ := ctx.Value(schedulerManualRunKey{}).(bool)
	if s.organizer == nil || (!manual && !s.autoOrganizeSourceEnabled(ctx)) {
		return nil
	}
	taskName := "自动整理重命名刮削入库"
	if manual {
		taskName = "手动触发自动整理重命名刮削入库"
	}
	resWrap, err := s.ensureOrganizePipeline().Run(ctx, OrganizePipelineRequest{
		Scope:    OrganizeScopeDirectory,
		Trigger:  OrganizeTriggerScheduled,
		TaskName: taskName,
	})
	if err != nil {
		return err
	}
	res := resWrap.Result
	if res == nil {
		res = &OrganizeResult{}
	}
	if s.log != nil && res != nil {
		s.log.Info("scheduled source organize finished",
			zap.String("source", res.SourcePath),
			zap.String("dest", res.DestPath),
			zap.Int("organized", res.Organized),
			zap.Int("replaced", res.Replaced),
			zap.Int("skipped", res.Skipped),
			zap.Int("scrapes", len(res.Scrapes)),
			zap.Int("errors", len(res.Errors)),
		)
	}
	return nil
}

func (s *SchedulerService) ensureOrganizePipeline() *OrganizePipelineService {
	if s.organizePipeline != nil {
		return s.organizePipeline
	}
	return NewOrganizePipelineService(s.log, s.repo, s.organizer, s.scanner, s.tasks)
}

func (s *SchedulerService) startScheduledOrganizeTask(ctx context.Context, manual bool) *TaskHandle {
	if s == nil || s.tasks == nil {
		return nil
	}
	name := "自动整理重命名入库"
	message := "正在执行计划自动整理/重命名/入库"
	if manual {
		name = "手动触发自动整理重命名入库"
		message = "正在执行手动触发的自动整理/重命名/入库"
	}
	return s.tasks.Start(TaskKindOrganize, name, TaskUpdate{
		Stage:      "organize",
		SourcePath: s.organizer.defaultSourceRoot(ctx, ""),
		DestPath:   s.organizer.defaultDestRoot(ctx, ""),
		Message:    message,
	})
}

func (s *SchedulerService) autoOrganizeSourceEnabled(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, "organize.auto")
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

func (s *SchedulerService) organizeSourceInterval(ctx context.Context) time.Duration {
	const fallback = 5 * time.Minute
	if s.repo == nil || s.repo.Setting == nil {
		return fallback
	}
	v, err := s.repo.Setting.Get(ctx, "organize.interval_seconds")
	if err != nil {
		return fallback
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || seconds <= 0 {
		return fallback
	}
	if seconds < 60 {
		seconds = 60
	}
	return time.Duration(seconds) * time.Second
}

// jobCleanTranscodeCache deletes HLS artefacts older than 24h.
func (s *SchedulerService) jobCleanTranscodeCache(ctx context.Context) error {
	if s.cacheDir == "" {
		return nil
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	return walkAndPrune(s.cacheDir+"/hls", cutoff)
}

// jobPurgeRecycleBin permanently deletes media rows soft-deleted >30 days
// ago. The on-disk file is left untouched (delete is operator-driven).
func (s *SchedulerService) jobPurgeRecycleBin(ctx context.Context) error {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	res := s.repo.DB.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).
		Delete(&model.Media{})
	if res.Error != nil && !isMissingTableErr(res.Error) {
		return res.Error
	}
	return pruneRecycleBinRows(ctx, s.repo.DB, maxRecycleBinRecords)
}

// isMissingTableErr lets the test harness ignore "no such table" errors
// that show up before AutoMigrate has run.
func isMissingTableErr(err error) bool {
	if err == nil {
		return false
	}
	return err == gorm.ErrInvalidDB
}

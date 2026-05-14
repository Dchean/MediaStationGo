// Package service — statistics aggregator.
//
// StatsService computes the dashboard numbers for the admin / home page:
//   - total libraries, media items, users
//   - total disk size and durations
//   - top recently-watched media
//   - process metadata (CPU / memory) via gopsutil
package service

import (
	"context"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// StatsService computes aggregate stats.
type StatsService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewStatsService is the constructor.
func NewStatsService(log *zap.Logger, repo *repository.Container) *StatsService {
	return &StatsService{log: log, repo: repo}
}

// Snapshot is the JSON returned by /api/stats.
type Snapshot struct {
	Libraries      int64        `json:"libraries"`
	MediaCount     int64        `json:"media_count"`
	UsersCount     int64        `json:"users_count"`
	TotalSizeBytes int64        `json:"total_size_bytes"`
	TotalSeconds   int64        `json:"total_seconds"`
	RecentlyAdded  []model.Media `json:"recently_added"`
	Hardware       Hardware     `json:"hardware"`
	GeneratedAt    time.Time    `json:"generated_at"`
}

// Hardware is the live CPU / memory / disk readings.
type Hardware struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskTotal   uint64  `json:"disk_total"`
	GoVersion   string  `json:"go_version"`
	Goroutines  int     `json:"goroutines"`
}

// Compute builds a fresh snapshot.
func (s *StatsService) Compute(ctx context.Context, dataDir string) (*Snapshot, error) {
	snap := &Snapshot{GeneratedAt: time.Now()}
	if err := s.repo.DB.Model(&model.Library{}).Count(&snap.Libraries).Error; err != nil {
		return nil, err
	}
	if err := s.repo.DB.Model(&model.Media{}).Count(&snap.MediaCount).Error; err != nil {
		return nil, err
	}
	if err := s.repo.DB.Model(&model.User{}).Count(&snap.UsersCount).Error; err != nil {
		return nil, err
	}
	type sumRow struct {
		Size    int64
		Seconds int64
	}
	var sum sumRow
	if err := s.repo.DB.Model(&model.Media{}).
		Select("COALESCE(SUM(size_bytes),0) as size, COALESCE(SUM(duration_sec),0) as seconds").
		Scan(&sum).Error; err != nil {
		return nil, err
	}
	snap.TotalSizeBytes = sum.Size
	snap.TotalSeconds = sum.Seconds

	if err := s.repo.DB.Model(&model.Media{}).
		Order("created_at desc").Limit(12).
		Find(&snap.RecentlyAdded).Error; err != nil {
		return nil, err
	}

	snap.Hardware = readHardware(dataDir)
	return snap, nil
}

func readHardware(dataDir string) Hardware {
	hw := Hardware{
		GoVersion:  runtime.Version(),
		Goroutines: runtime.NumGoroutine(),
	}
	if usage, err := cpu.Percent(0, false); err == nil && len(usage) > 0 {
		hw.CPUPercent = usage[0]
	}
	if v, err := mem.VirtualMemory(); err == nil {
		hw.MemoryUsed = v.Used
		hw.MemoryTotal = v.Total
	}
	if dataDir == "" {
		dataDir = "/"
	}
	if d, err := disk.Usage(dataDir); err == nil {
		hw.DiskUsed = d.Used
		hw.DiskTotal = d.Total
	}
	return hw
}

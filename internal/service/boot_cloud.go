package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// BootCloudLibraries 在系统启动后自动扫描所有云盘媒体库，使媒体对所有用户立即可见。
// 避免每个用户首次访问时都触发扫描。
func (c *Container) BootCloudLibraries(ctx context.Context) {
	if c == nil || c.Repo == nil || c.Scan == nil {
		return
	}
	libs, err := c.Repo.Library.List(ctx)
	if err != nil {
		c.Log.Warn("boot cloud libraries: list failed", zap.Error(err))
		return
	}
	cloudLibs := make([]model.Library, 0)
	for _, lib := range libs {
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			cloudLibs = append(cloudLibs, lib)
		}
	}
	if len(cloudLibs) == 0 {
		return
	}
	c.Log.Info("boot: scheduling cloud library scans", zap.Int("count", len(cloudLibs)))
	// 延迟3秒后启动，避免和系统初始化任务冲突
	time.AfterFunc(3*time.Second, func() {
		for _, lib := range cloudLibs {
			libID := lib.ID
			libName := lib.Name
			go func() {
				scanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
				defer cancel()
				c.Log.Info("boot: scanning cloud library", zap.String("id", libID), zap.String("name", libName))
				if _, err := c.Scan.ScanLibraryWithoutAutoScrape(scanCtx, libID); err != nil {
					c.Log.Warn("boot: cloud library scan failed", zap.String("id", libID), zap.String("name", libName), zap.Error(err))
				} else {
					c.Log.Info("boot: cloud library scan completed", zap.String("id", libID), zap.String("name", libName))
				}
			}()
		}
	})
}

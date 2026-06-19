// Package handler — batch repair+rescrape endpoint.
package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// repairAndRescrapeAllHandler 触发"全库修复+重刮"流程:先从媒体路径中的
// {tmdb-N}/{bangumi-N} 等占位符回填缺失的外部 ID, 再对所有媒体库重刮一遍。
//
// 路由: POST /api/admin/media/repair-rescrape (需 admin)
// 异步执行, 立即返回 202;通过 WS hub "scrape" topic 推送进度。
func repairAndRescrapeAllHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		task := startScrapeHTTPTask(svc, "全库修复并重刮", "", "")
		go func() {
			result, err := svc.RepairAndRescrapeAllLibraries(context.Background())
			metrics := map[string]int64{
				"repaired":  int64(result.Repaired),
				"libraries": int64(result.Libraries),
				"matched":   int64(result.Matched),
			}
			stage := "completed"
			message := "全库修复并重刮完成"
			if err != nil {
				stage = "scrape"
				message = "全库修复并重刮失败"
			}
			finishHTTPTask(task, err, stage, message, metrics, nil)
		}()
		c.JSON(http.StatusAccepted, gin.H{"status": "started"})
	}
}

// repairAndRescrapeLibraryHandler 触发"单库修复+重刮":只对路径参数指定的
// 媒体库回填占位符外部 ID 并重刮, 不影响其它库。
//
// 路由: POST /api/admin/libraries/:id/repair-rescrape (需 admin)
// 异步执行, 立即返回 202;通过 WS hub "scrape" topic 推送进度。
func repairAndRescrapeLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libraryID := c.Param("id")
		task := startScrapeHTTPTask(svc, "媒体库修复并重刮", "", "")
		go func() {
			result, err := svc.RepairAndRescrapeLibrary(context.Background(), libraryID)
			metrics := map[string]int64{
				"repaired":  int64(result.Repaired),
				"libraries": int64(result.Libraries),
				"matched":   int64(result.Matched),
			}
			stage := "completed"
			message := "媒体库修复并重刮完成"
			if err != nil {
				stage = "scrape"
				message = "媒体库修复并重刮失败"
			}
			finishHTTPTask(task, err, stage, message, metrics, nil)
		}()
		c.JSON(http.StatusAccepted, gin.H{"status": "started"})
	}
}

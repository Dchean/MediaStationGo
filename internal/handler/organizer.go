// Package handler — media file organizer endpoints.
package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// organizeReq carries optional per-request overrides. 留空则沿用系统设置。
//
// source_path = 源目录（待整理），dest_path = 目的地目录（整理输出）。
// target_path 为 dest_path 的向后兼容别名。
type organizeReq struct {
	SourcePath    string `json:"source_path"`
	DestPath      string `json:"dest_path"`
	TargetPath    string `json:"target_path"` // deprecated alias for dest_path
	TransferMode  string `json:"transfer_mode"`
	MediaType     string `json:"media_type"`
	MediaCategory string `json:"media_category"`
	ScanAfter     *bool  `json:"scan_after"`
	ScrapeAfter   *bool  `json:"scrape_after"`
	LibraryID     string `json:"library_id"`
	DryRun        bool   `json:"dry_run"`
}

// bindOrganizeOptions parses the optional JSON body into OrganizeOptions.
// A missing/empty body is fine — it means "use the configured defaults".
func bindOrganizeOptions(c *gin.Context) service.OrganizeOptions {
	var req organizeReq
	_ = c.ShouldBindJSON(&req)
	return organizeOptionsFromReq(req)
}

func organizeOptionsFromReq(req organizeReq) service.OrganizeOptions {
	dest := strings.TrimSpace(req.DestPath)
	if dest == "" {
		dest = strings.TrimSpace(req.TargetPath)
	}
	opts := service.OrganizeOptions{
		SourcePath:    strings.TrimSpace(req.SourcePath),
		DestPath:      dest,
		MediaType:     strings.TrimSpace(req.MediaType),
		MediaCategory: strings.TrimSpace(req.MediaCategory),
		DryRun:        req.DryRun,
	}
	if m := strings.TrimSpace(req.TransferMode); m != "" {
		opts.TransferMode = service.TransferMode(m)
	}
	return opts
}

func organizeMediaHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req organizeReq
		_ = c.ShouldBindJSON(&req)
		opts := organizeOptionsFromReq(req)
		task := startOrganizeHTTPTask(svc, "手动整理媒体", opts)
		dst, err := svc.Organizer.OrganizeMediaWithOptions(c.Request.Context(), c.Param("id"), opts)
		if err != nil {
			finishHTTPTask(task, err, "organize", "手动整理媒体失败", nil)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		payload := gin.H{"path": dst}
		if organizeScanAfter(req.ScanAfter) && !req.DryRun && svc.Scan != nil {
			updateHTTPTask(task, "scan_scrape", "正在扫描入库并按设置刮削", nil)
			scans, scrapes := scanAndScrapeAfterOrganize(c, svc, dst, strings.TrimSpace(req.LibraryID), req.ScrapeAfter)
			payload["scans"] = scans
			payload["scrapes"] = scrapes
		}
		finishHTTPTask(task, nil, "completed", "手动整理媒体结束", nil)
		c.JSON(http.StatusOK, payload)
	}
}

func organizeLibraryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req organizeReq
		_ = c.ShouldBindJSON(&req)
		opts := organizeOptionsFromReq(req)
		task := startOrganizeHTTPTask(svc, "手动整理媒体库", opts)
		res, err := svc.Organizer.OrganizeLibraryWithOptions(c.Request.Context(), c.Param("id"), opts)
		if err != nil {
			finishHTTPTask(task, err, "organize", "手动整理媒体库失败", nil)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if organizeScanAfter(req.ScanAfter) && !req.DryRun && svc.Scan != nil {
			updateHTTPTask(task, "scan_scrape", "正在扫描入库并按设置刮削", nil)
			res.Scans, res.Scrapes = scanAndScrapeAfterOrganize(c, svc, res.DestPath, c.Param("id"), req.ScrapeAfter)
		}
		finishHTTPTask(task, nil, "completed", "手动整理媒体库结束", service.OrganizeTaskMetrics(res))
		c.JSON(http.StatusOK, res)
	}
}

// organizeSourcesHandler lists selectable organize source directories (download
// dir + media dir) so the UI can offer them alongside registered libraries.
func organizeSourcesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"sources": svc.Organizer.OrganizeSourceCandidates(c.Request.Context())})
	}
}

// organizeDirectoryHandler organizes an arbitrary source directory (e.g. the
// download directory) into the destination with dedup + 洗版.
func organizeDirectoryHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req organizeReq
		_ = c.ShouldBindJSON(&req)
		opts := organizeOptionsFromReq(req)
		task := startOrganizeHTTPTask(svc, "手动整理入库", opts)
		res, err := svc.Organizer.OrganizeDirectory(c.Request.Context(), opts)
		if err != nil {
			finishHTTPTask(task, err, "organize", "手动整理入库失败", nil)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updateHTTPTask(task, "organize", "手动整理完成，准备扫描入库", service.OrganizeTaskMetrics(res))
		if organizeScanAfter(req.ScanAfter) && !req.DryRun && svc.Scan != nil {
			updateHTTPTask(task, "scan_scrape", "正在扫描入库并按设置刮削", service.OrganizeTaskMetrics(res))
			res.Scans, res.Scrapes = scanAndScrapeAfterOrganize(c, svc, res.DestPath, strings.TrimSpace(req.LibraryID), req.ScrapeAfter)
		}
		finishHTTPTask(task, nil, "completed", "手动整理入库结束", service.OrganizeTaskMetrics(res))
		c.JSON(http.StatusOK, res)
	}
}

func startOrganizeHTTPTask(svc *service.Container, name string, opts service.OrganizeOptions) *service.TaskHandle {
	if svc == nil || svc.Tasks == nil {
		return nil
	}
	message := "正在整理/重命名/入库"
	if opts.DryRun {
		message = "正在预览整理/重命名"
	}
	return svc.Tasks.Start(service.TaskKindOrganize, name, service.TaskUpdate{
		Stage:      "organize",
		SourcePath: opts.SourcePath,
		DestPath:   opts.DestPath,
		Message:    message,
	})
}

func updateHTTPTask(task *service.TaskHandle, stage, message string, metrics map[string]int64) {
	if task == nil {
		return
	}
	task.Update(service.TaskUpdate{Stage: stage, Message: message, Metrics: metrics})
}

func finishHTTPTask(task *service.TaskHandle, err error, stage, message string, metrics map[string]int64) {
	if task == nil {
		return
	}
	task.Finish(err, service.TaskUpdate{Stage: stage, Message: message, Metrics: metrics})
}

func scanAndScrapeAfterOrganize(c *gin.Context, svc *service.Container, destRoot, preferredLibraryID string, scrapeOverride *bool) ([]service.OrganizeScanSummary, []service.OrganizeScrapeSummary) {
	scrapeAfter := service.OrganizeScrapeAfterEnabled(c.Request.Context(), svc.Repo)
	if scrapeOverride != nil {
		scrapeAfter = *scrapeOverride
	}
	return svc.Scan.ScanAndScrapeLibrariesForPath(c.Request.Context(), destRoot, preferredLibraryID, scrapeAfter)
}

func organizeScanAfter(value *bool) bool {
	return value == nil || *value
}

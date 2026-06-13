// Package handler — cloud-disk (网盘) endpoints: directory browsing, QR-code
// login, media import and 302 playback redirects.
package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

// cloudListHandler browses a configured cloud disk directory.
func cloudListHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		dir := c.Query("dir")
		entries, err := svc.StorageCfg.CloudList(c.Request.Context(), typ, dir)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"error": err.Error(), "items": []any{}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": entries})
	}
}

// cloudImportHandler turns a cloud file into a playable 302-backed media item.
func cloudImportHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		var in struct {
			Ref  string `json:"ref" binding:"required"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		m, err := svc.StorageCfg.CloudImport(c.Request.Context(), typ, in.Ref, in.Name, in.Size)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, m)
	}
}

// cloudMountHandler creates or reuses a cloud:// media library for a cloud
// directory, then queues a recursive import scan. The scan runs outside the
// request so large 115/OpenList folders do not make the UI report a timeout.
func cloudMountHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		var in struct {
			Dir       string `json:"dir"`
			DirPath   string `json:"dir_path"`
			Name      string `json:"name"`
			MediaType string `json:"media_type"`
		}
		_ = c.ShouldBindJSON(&in)
		if !cloud.IsCloudType(typ) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported cloud provider"})
			return
		}
		if _, err := svc.StorageCfg.CloudProvider(c.Request.Context(), typ); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		path := service.BuildCloudLibraryPath(typ, in.Dir, in.DirPath)
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = cloudMountLibraryName(typ, strings.TrimSpace(in.Dir), strings.TrimSpace(in.DirPath))
		}
		mediaType := strings.TrimSpace(in.MediaType)
		if mediaType == "" || strings.EqualFold(mediaType, "auto") {
			displayDir := strings.TrimSpace(in.DirPath)
			if displayDir == "" {
				displayDir = strings.TrimSpace(in.Dir)
			}
			mediaType = service.InferCloudMountMediaType(displayDir, name)
		}
		libs, err := svc.Repo.Library.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var lib *model.Library
		alreadyMounted := false
		if conflict := service.FindCloudMountConflict(libs, typ, in.Dir, in.DirPath); conflict != nil {
			lib = &conflict.Library
			alreadyMounted = conflict.Exact
			if conflict.Nested {
				c.JSON(http.StatusOK, gin.H{
					"library":          lib,
					"skipped":          true,
					"reason":           "cloud mount overlaps an existing mounted parent/child directory",
					"conflict_library": conflict.Library,
				})
				return
			}
		}
		if lib == nil {
			lib = &model.Library{Name: name, Path: path, Type: mediaType, Enabled: true}
			if err := svc.Repo.Library.Create(c.Request.Context(), lib); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		} else if alreadyMounted {
			updates := map[string]any{}
			if path != "" && path != lib.Path {
				updates["path"] = path
				lib.Path = path
			}
			if mediaType != "" && mediaType != lib.Type {
				updates["type"] = mediaType
				lib.Type = mediaType
			}
			currentDisplayName, _ := service.CloudLibraryDisplayName(*lib)
			if name != "" && name != lib.Name && (currentDisplayName == "" || currentDisplayName != name || strings.Contains(lib.Name, " · ")) {
				updates["name"] = name
				lib.Name = name
			}
			if len(updates) > 0 {
				if err := svc.Repo.DB.WithContext(c.Request.Context()).Model(&model.Library{}).Where("id = ?", lib.ID).Updates(updates).Error; err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
			}
		}
		if svc.Scan != nil {
			libID := lib.ID
			if svc.WSHub != nil {
				svc.WSHub.Publish("scan", gin.H{
					"library_id":       libID,
					"cloud":            true,
					"queued":           true,
					"stage":            "queued",
					"message":          "云盘扫描已加入后台队列，会递归扫描并自动加入媒体库",
					"estimate_message": "小目录通常几十秒；几万文件的大目录可能需要数分钟到数小时，取决于网盘接口速度",
				})
			}
			_, _, _ = svc.Scan.StartCloudLibraryScan(libID, false)
		}
		c.JSON(http.StatusAccepted, gin.H{
			"library":          lib,
			"already_mounted":  alreadyMounted,
			"scan_queued":      svc.Scan != nil,
			"message":          "挂载后会后台递归扫描，发现的媒体会自动加入当前媒体库",
			"estimate_message": "小目录通常几十秒；几万文件的大目录可能需要数分钟到数小时，取决于网盘接口速度",
		})
	}
}

func cloudScanAllHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc.Scan == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scanner unavailable"})
			return
		}
		statuses, err := svc.Scan.StartAllCloudLibraryScans()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{
			"items":            statuses,
			"scan_queued":      true,
			"message":          "已开始扫描所有启用的网盘媒体库",
			"resume_message":   "中断后再次点击扫描会重新遍历，但已入库媒体会去重更新，只补齐缺失项。",
			"estimate_message": "小目录通常几十秒；几万文件的大目录可能需要数分钟到数小时，取决于网盘接口速度",
		})
	}
}

func cloudScanCancelHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc.Scan == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scanner unavailable"})
			return
		}
		libraryID := strings.TrimSpace(c.Query("library_id"))
		provider := strings.TrimSpace(c.Query("provider"))
		cancelled := 0
		if libraryID != "" {
			if svc.Scan.CancelCloudScan(libraryID) {
				cancelled = 1
			}
		} else if provider != "" {
			cancelled = svc.Scan.CancelCloudScansForProvider(provider)
		} else {
			cancelled = svc.Scan.CancelAllCloudScans()
		}
		c.JSON(http.StatusOK, gin.H{
			"cancelled": cancelled,
			"message":   "已发送中断信号；正在等待当前网盘请求返回后停止",
		})
	}
}

func cloudScanStatusHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc.Scan == nil {
			c.JSON(http.StatusOK, gin.H{"items": []service.CloudScanStatus{}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": svc.Scan.CloudScanStatuses()})
	}
}

func cloudMountLibraryName(typ, dir, displayDir string) string {
	base := service.CloudMountProviderLabel(typ)
	displayDir = strings.Trim(strings.TrimSpace(strings.ReplaceAll(displayDir, "\\", "/")), "/")
	if displayDir != "" {
		parts := strings.Split(displayDir, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if part := strings.TrimSpace(parts[i]); part != "" {
				return part
			}
		}
	}
	if dir == "" || dir == "0" {
		return base
	}
	dir = strings.Trim(strings.TrimSpace(strings.ReplaceAll(dir, "\\", "/")), "/")
	if dir == "" {
		return base
	}
	parts := strings.Split(dir, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if part := strings.TrimSpace(parts[i]); part != "" {
			return part
		}
	}
	return base
}

// cloud115QRStartHandler begins a 115 QR-code login and returns the session +
// QR image URL for the frontend to render.
func cloud115QRStartHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Param("type") != cloud.Type115 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "qr login is only supported for 115"})
			return
		}
		sess, err := cloud.QRStart(c.Request.Context(), nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, sess)
	}
}

// cloud115QRPollHandler polls a 115 QR session; on confirmation it returns the
// session cookie so the frontend can save it as the storage credential.
func cloud115QRPollHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Param("type") != cloud.Type115 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "qr login is only supported for 115"})
			return
		}
		var sess cloud.QRSession
		if err := c.ShouldBindJSON(&sess); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		st, err := cloud.QRPoll(c.Request.Context(), nil, &sess)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, st)
	}
}

// cloudPlayHandler resolves a cloud file to its direct link and either issues a
// 302 redirect (true offload — host does not stream the bytes) or, when the
// provider requires authenticated headers, reverse-proxies the response.
func cloudPlayHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		typ := c.Param("type")
		ref := c.Query("ref")
		if ref == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ref required"})
			return
		}
		serveCloudResolvedLink(svc, c, typ, ref)
	}
}

func serveCloudResolvedLink(svc *service.Container, c *gin.Context, typ, ref string) {
	if isCloudImageRef(ref) && svc != nil && svc.ImageProxy != nil {
		if svc.ImageProxy.ServeCloudCached(c.Writer, c.Request, typ+":"+ref) {
			return
		}
	}
	if svc == nil || svc.StorageCfg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cloud storage service unavailable"})
		return
	}
	resolveStart := time.Now()
	link, err := svc.StorageCfg.CloudResolve(c.Request.Context(), typ, ref, c.Request.UserAgent())
	resolveDur := time.Since(resolveStart)
	if err != nil {
		logCloudPlayback(svc, "cloud playback resolve failed",
			append(cloudPlaybackLogFields(typ, ref, nil, resolveDur), zap.Error(err))...)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if isCloudImageRef(ref) && svc.ImageProxy != nil {
		if err := svc.ImageProxy.ServeCloudResolved(c.Request.Context(), c.Writer, c.Request, typ+":"+ref, link); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		}
		return
	}
	if isCloudImageRef(ref) {
		c.Header("Cache-Control", "public, max-age=2592000, immutable")
	}
	if !link.Proxy {
		// Pure offload: send the client straight to the cloud CDN.
		setRedirectNoStoreHeaders(c)
		logCloudPlayback(svc, "cloud playback redirect",
			append(cloudPlaybackLogFields(typ, ref, link, resolveDur),
				zap.String("mode", "redirect"),
				zap.Int("status", http.StatusFound),
				zap.String("method", c.Request.Method),
				zap.String("range", c.GetHeader("Range")),
			)...)
		c.Redirect(http.StatusFound, link.URL)
		return
	}
	// Proxy mode: the direct link needs auth headers the browser cannot
	// carry. Stream through with Range forwarding.
	method := c.Request.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, link.URL, nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	for k, v := range link.Headers {
		req.Header.Set(k, v)
	}
	if rng := c.GetHeader("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	if accept := c.GetHeader("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}
	if c.GetHeader("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	upstreamStart := time.Now()
	resp, err := http.DefaultClient.Do(req)
	upstreamHeaderDur := time.Since(upstreamStart)
	if err != nil {
		logCloudPlayback(svc, "cloud playback proxy upstream failed",
			append(cloudPlaybackLogFields(typ, ref, link, resolveDur),
				zap.String("mode", "proxy"),
				zap.String("method", method),
				zap.String("range", c.GetHeader("Range")),
				zap.Int64("upstream_header_ms", durationMilliseconds(upstreamHeaderDur)),
				zap.Error(err),
			)...)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			c.Header(h, v)
		}
	}
	if c.Writer.Header().Get("Accept-Ranges") == "" {
		c.Header("Accept-Ranges", "bytes")
	}
	if resp.StatusCode >= 400 {
		c.Header("Cache-Control", "no-store")
	}
	c.Status(resp.StatusCode)
	var copied int64
	var copyErr error
	streamStart := time.Now()
	if c.Request.Method != http.MethodHead {
		copied, copyErr = io.Copy(c.Writer, resp.Body)
	}
	fields := append(cloudPlaybackLogFields(typ, ref, link, resolveDur),
		zap.String("mode", "proxy"),
		zap.String("method", method),
		zap.String("range", c.GetHeader("Range")),
		zap.Int("status", resp.StatusCode),
		zap.String("content_range", resp.Header.Get("Content-Range")),
		zap.String("content_length", resp.Header.Get("Content-Length")),
		zap.Int64("upstream_header_ms", durationMilliseconds(upstreamHeaderDur)),
		zap.Int64("stream_ms", durationMilliseconds(time.Since(streamStart))),
		zap.Int64("total_ms", durationMilliseconds(time.Since(resolveStart))),
		zap.Int64("bytes", copied),
	)
	if copyErr != nil {
		logCloudPlayback(svc, "cloud playback proxy copy failed", append(fields, zap.Error(copyErr))...)
		return
	}
	logCloudPlayback(svc, "cloud playback proxy finished", fields...)
}

func isCloudImageRef(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	for _, suffix := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp"} {
		if strings.HasSuffix(ref, suffix) {
			return true
		}
	}
	return false
}

func logCloudPlayback(svc *service.Container, msg string, fields ...zap.Field) {
	if svc == nil || svc.Log == nil {
		return
	}
	svc.Log.Info(msg, fields...)
}

func cloudPlaybackLogFields(typ, ref string, link *cloud.DirectLink, resolveDur time.Duration) []zap.Field {
	refHash, refExt := cloudPlaybackRefFingerprint(ref)
	fields := []zap.Field{
		zap.String("provider", strings.TrimSpace(typ)),
		zap.String("ref_hash", refHash),
		zap.String("ref_ext", refExt),
		zap.Int64("resolve_ms", durationMilliseconds(resolveDur)),
	}
	if link != nil {
		fields = append(fields,
			zap.String("target_host", cloudPlaybackLinkHost(link.URL)),
			zap.Bool("headers_required", len(link.Headers) > 0),
			zap.Strings("header_names", cloudPlaybackHeaderNames(link.Headers)),
		)
	}
	return fields
}

func cloudPlaybackRefFingerprint(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	sum := sha256.Sum256([]byte(ref))
	ext := strings.ToLower(path.Ext(strings.Trim(strings.ReplaceAll(ref, "\\", "/"), "/")))
	return hex.EncodeToString(sum[:])[:12], ext
}

func cloudPlaybackLinkHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func cloudPlaybackHeaderNames(headers map[string]string) []string {
	if len(headers) == 0 {
		return nil
	}
	out := make([]string, 0, len(headers))
	for key := range headers {
		if key = strings.TrimSpace(key); key != "" {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func durationMilliseconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return d.Milliseconds()
}

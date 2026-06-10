// Package handler — cloud-disk (网盘) endpoints: directory browsing, QR-code
// login, media import and 302 playback redirects.
package handler

import (
	"context"
	"io"
	"net/http"
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
			name = cloudMountLibraryName(typ, strings.TrimSpace(in.Dir))
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
			if name != "" && name != lib.Name && !strings.Contains(lib.Name, " · ") {
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
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
				defer cancel()
				res, err := svc.Scan.ScanLibraryWithoutAutoScrape(ctx, libID)
				if err != nil {
					if svc.Log != nil {
						svc.Log.Warn("cloud mount background scan failed", zap.String("library_id", libID), zap.Error(err))
					}
					if svc.WSHub != nil {
						svc.WSHub.Publish("scan", gin.H{
							"library_id": libID,
							"cloud":      true,
							"finished":   true,
							"error":      err.Error(),
						})
					}
					return
				}
				if svc.Log != nil {
					svc.Log.Info("cloud mount background scan finished",
						zap.String("library_id", libID),
						zap.Int("visited", res.Visited),
						zap.Int("added", res.Added),
						zap.Int("updated", res.Updated),
						zap.Int("skipped", res.Skipped),
						zap.Int64("removed", res.Removed))
				}
			}()
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

func cloudMountLibraryName(typ, dir string) string {
	base := typ
	switch typ {
	case cloud.TypeQuark:
		base = "夸克网盘"
	case cloud.Type115:
		base = "115 网盘"
	case cloud.TypeCloudDrive2:
		base = "CloudDrive2"
	case cloud.TypeOpenList:
		base = "OpenList"
	}
	if dir == "" || dir == "0" {
		return base
	}
	return base + " · " + dir
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
		link, err := svc.StorageCfg.CloudResolve(c.Request.Context(), typ, ref, c.Request.UserAgent())
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if !link.Proxy {
			// Pure offload: send the client straight to the cloud CDN.
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
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
			if v := resp.Header.Get(h); v != "" {
				c.Header(h, v)
			}
		}
		c.Status(resp.StatusCode)
		if c.Request.Method != http.MethodHead {
			_, _ = io.Copy(c.Writer, resp.Body)
		}
	}
}

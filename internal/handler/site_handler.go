// Package handler — PT 站点管理 HTTP 处理。
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// SiteHandler 站点管理 CRUD。
type SiteHandler struct {
	svc *service.Container
}

// NewSiteHandler 创建站点管理 Handler。
func NewSiteHandler(svc *service.Container) *SiteHandler {
	return &SiteHandler{svc: svc}
}

// ListSites 列出所有站点。
func (h *SiteHandler) ListSites(c *gin.Context) {
	sites, err := h.svc.Site.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if sites == nil {
		sites = []model.Site{}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": sites})
}

// GetSite 获取单个站点详情。
func (h *SiteHandler) GetSite(c *gin.Context) {
	site, err := h.svc.Site.FindByID(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if site == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 1, "message": "site not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": site})
}

// CreateSite 创建站点。
func (h *SiteHandler) CreateSite(c *gin.Context) {
	var body map[string]any
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}

	var site model.Site
	// 手动映射字段（因为敏感字段有 json:"-" 标签，不会自动反序列化）
	if v, ok := body["name"].(string); ok {
		site.Name = v
	}
	if v, ok := body["url"].(string); ok {
		site.URL = v
	}
	if v, ok := body["type"].(string); ok {
		site.Type = v
	}
	if v, ok := body["auth_type"].(string); ok {
		site.AuthType = v
	}
	if v, ok := body["api_key"].(string); ok {
		site.APIKey = v
	}
	if v, ok := body["cookie"].(string); ok {
		site.Cookie = v
	}
	if v, ok := body["auth_header"].(string); ok {
		site.AuthHeader = v
	}
	if v, ok := body["enabled"].(bool); ok {
		site.Enabled = v
	}
	if v, ok := body["is_default"].(bool); ok {
		site.IsDefault = v
	}
	if v, ok := body["extra"].(string); ok {
		site.Extra = v
	}
	// 高级设置字段
	if v, ok := body["user_agent"].(string); ok {
		site.UserAgent = v
	}
	if v, ok := body["rss_url"].(string); ok {
		site.RSSURL = v
	}
	if v, ok := body["timeout"].(float64); ok {
		site.Timeout = int(v)
	}
	if v, ok := body["priority"].(float64); ok {
		site.Priority = int(v)
	}
	if v, ok := body["use_proxy"].(bool); ok {
		site.UseProxy = v
	}
	if v, ok := body["rate_limit"].(bool); ok {
		site.RateLimit = v
	}
	if v, ok := body["browser_emulation"].(bool); ok {
		site.BrowserEmulation = v
	}
	if v, ok := body["downloader"].(string); ok {
		site.Downloader = v
	}

	if err := h.svc.Site.Create(c.Request.Context(), &site); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": 0, "message": "ok", "data": site})
}

// UpdateSite 更新站点。
func (h *SiteHandler) UpdateSite(c *gin.Context) {
	patch := make(map[string]any)
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	// 字段名映射：前端使用蛇形命名，GORM 会正确映射到列名
	// 无需额外处理，Updates 直接使用 patch 中的 key-value
	if err := h.svc.Site.Update(c.Request.Context(), c.Param("id"), patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// DeleteSite 删除站点。
func (h *SiteHandler) DeleteSite(c *gin.Context) {
	if err := h.svc.Site.Delete(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok"})
}

// TestSite 测试站点连通性。
func (h *SiteHandler) TestSite(c *gin.Context) {
	ok, msg, err := h.svc.Site.TestConnection(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "message": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": msg})
}

// GetSiteTypes 返回支持的站点类型列表。
func (h *SiteHandler) GetSiteTypes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": model.SiteTypes()})
}

// GetAuthTypes 返回支持的认证方式列表。
func (h *SiteHandler) GetAuthTypes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": model.AuthTypes()})
}

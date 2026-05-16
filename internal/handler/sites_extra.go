// Package handler — extra site endpoints used by the Vue UI:
//
//	GET /sites/:id/resource → keyword search scoped to one site
//	GET /sites/:id/userdata → cookie-derived user info (stubbed)
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// siteResourceHandler runs a search restricted to a single site.
//
// We reuse the full SiteService.Search() and post-filter by site_id;
// it's not the hottest path so we trade simplicity for speed here.
func siteResourceHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		keyword := c.Query("keyword")
		if keyword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "keyword required"})
			return
		}
		all, err := svc.Site.Search(c.Request.Context(), keyword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		want := c.Param("id")
		filtered := make([]service.SearchResult, 0, len(all))
		for _, r := range all {
			if r.SiteID == want {
				filtered = append(filtered, r)
			}
		}
		c.JSON(http.StatusOK, gin.H{"items": filtered})
	}
}

// siteUserdataHandler returns whatever the site exposes about the
// authenticated user (upload/download stats, ratio, etc.). This is a
// stub: we report the cookie length so the UI can confirm a login is
// present, but full per-site parsing is out of scope here.
func siteUserdataHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		s, err := svc.Site.FindByID(c.Request.Context(), c.Param("id"))
		if err != nil || s == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "site not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"site_id":      s.ID,
			"name":         s.Name,
			"cookie_set":   len(s.Cookie) > 0,
			"login_status": s.LoginStatus,
			"note":         "userdata parsing not implemented; stub",
		})
	}
}

// Package handler — TV series endpoints.
//
// These return episode lists grouped by season number for a library that
// holds TV episodes. Series rows are distinct from Movies — the front
// end uses /api/libraries/:id/seasons to render a season selector.
package handler

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// seasonGroup is the JSON returned to the React UI per season.
type seasonGroup struct {
	Season   int           `json:"season"`
	Episodes []model.Media `json:"episodes"`
}

func listSeasonsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		libID := c.Param("id")
		var rows []model.Media
		err := svc.Repo.DB.Where(&model.Media{LibraryID: libID}).
			Order("season_num asc, episode_num asc").
			Find(&rows).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		buckets := make(map[int][]model.Media)
		for _, r := range rows {
			buckets[r.SeasonNum] = append(buckets[r.SeasonNum], r)
		}
		out := make([]seasonGroup, 0, len(buckets))
		for s, items := range buckets {
			out = append(out, seasonGroup{Season: s, Episodes: items})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Season < out[j].Season })
		c.JSON(http.StatusOK, gin.H{"seasons": out})
	}
}

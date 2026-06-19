package service

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

// RepairCloudPathMetadata backfills external IDs from media paths such as
// "Movie (2025) {tmdb-123}" so existing placeholder rows can be scraped
// without requiring another successful filesystem or cloud provider traversal.
//
// 传入 libraryID 时只修复该媒体库的行;为空则修复全库。
func (c *Container) RepairCloudPathMetadata(ctx context.Context, libraryID ...string) (int, error) {
	if c == nil || c.Repo == nil || c.Repo.DB == nil {
		return 0, nil
	}
	var repaired int
	var rows []model.Media
	query := c.Repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("id, title, path, year, season_num, episode_num, scrape_status, tm_db_id, bangumi_id, douban_id, thetvdb_id").
		Where("("+strings.Join([]string{
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
			"LOWER(path) LIKE ?",
		}, " OR ")+")",
			"%tmdb%", "%tmdbid%", "%douban%", "%db%", "%bangumi%", "%bgm%", "%thetvdb%", "%tvdb%")
	if len(libraryID) > 0 && strings.TrimSpace(libraryID[0]) != "" {
		query = query.Where("library_id = ?", strings.TrimSpace(libraryID[0]))
	}

	err := query.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, row := range rows {
			meta, hints := pathHintMetadata(row.Path, row.SeasonNum > 0 || row.EpisodeNum > 0)
			if meta == nil || !hints.useful() {
				continue
			}
			updates := map[string]any{}
			status := strings.TrimSpace(row.ScrapeStatus)
			enrichable := status == "" || status == "pending" || status == "no_match"
			changedExternalID := false
			if meta.TMDbID > 0 && row.TMDbID != meta.TMDbID {
				updates["tm_db_id"] = meta.TMDbID
				changedExternalID = true
			}
			if meta.BangumiID > 0 && row.BangumiID != meta.BangumiID {
				updates["bangumi_id"] = meta.BangumiID
				changedExternalID = true
			}
			if strings.TrimSpace(meta.DoubanID) != "" && strings.TrimSpace(row.DoubanID) != strings.TrimSpace(meta.DoubanID) {
				updates["douban_id"] = strings.TrimSpace(meta.DoubanID)
				changedExternalID = true
			}
			if strings.TrimSpace(meta.TheTVDBID) != "" && strings.TrimSpace(row.TheTVDBID) != strings.TrimSpace(meta.TheTVDBID) {
				updates["thetvdb_id"] = strings.TrimSpace(meta.TheTVDBID)
				changedExternalID = true
			}
			if meta.Year > 0 && row.Year <= 0 {
				updates["year"] = meta.Year
			}
			if enrichable && strings.TrimSpace(meta.Title) != "" && cloudPathRepairShouldReplaceTitle(row.Title, meta.Title) {
				updates["title"] = strings.TrimSpace(meta.Title)
			}
			if changedExternalID && (status == "" || status == "no_match" || status == "matched") {
				updates["scrape_status"] = "pending"
			}
			if len(updates) == 0 {
				continue
			}
			if err := c.Repo.DB.WithContext(ctx).Model(&model.Media{}).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
				return err
			}
			repaired++
		}
		return nil
	}).Error
	if err != nil {
		return repaired, err
	}
	if repaired > 0 && c.Log != nil {
		c.Log.Info("cloud path metadata repaired", zap.Int("media_count", repaired))
	}
	return repaired, nil
}

func cloudPathRepairShouldReplaceTitle(current, hinted string) bool {
	current = strings.TrimSpace(current)
	hinted = strings.TrimSpace(hinted)
	if hinted == "" || strings.EqualFold(current, hinted) {
		return false
	}
	if current == "" {
		return true
	}
	noise := []string{"web-dl", "bluray", "hdtv", "2160p", "1080p", "720p", "ddp", "aac", "h.264", "h.265", "x264", "x265", "adweb", "mweb", "cmctv", "bit"}
	lower := strings.ToLower(current)
	for _, token := range noise {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return len([]rune(current)) > len([]rune(hinted))*2
}


// RepairAndRescrapeResult 汇总一次「全库修复+重刮」的结果。
type RepairAndRescrapeResult struct {
	Repaired  int `json:"repaired"`  // 从路径占位符回填外部 ID 的媒体数
	Libraries int `json:"libraries"` // 参与重刮的媒体库数
	Matched   int `json:"matched"`   // 重刮后成功匹配的媒体数
}

// RepairAndRescrapeAllLibraries 修复并重刮所有媒体库:先从媒体路径中的
// {tmdb-123}/{bangumi-456} 等占位符回填缺失或错误的外部 ID(回填后会把相关
// 行的 scrape_status 重置为 pending),随后逐个媒体库重刮(含 no_match 重试),
// 让此前因空 ID / 脏 ID 无法刮削的媒体重新匹配到正确数据。
func (c *Container) RepairAndRescrapeAllLibraries(ctx context.Context) (RepairAndRescrapeResult, error) {
	var result RepairAndRescrapeResult
	if c == nil || c.Repo == nil || c.Repo.DB == nil {
		return result, nil
	}
	repaired, err := c.RepairCloudPathMetadata(ctx)
	if err != nil {
		return result, err
	}
	result.Repaired = repaired

	if c.Scraper == nil || c.Repo.Library == nil {
		return result, nil
	}
	libraries, err := c.Repo.Library.List(ctx)
	if err != nil {
		return result, err
	}
	for i := range libraries {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		lib := libraries[i]
		if !lib.Enabled {
			continue
		}
		result.Libraries++
		// retryNoMatch=true:连之前匹配失败的也再试一次,因为这次可能已回填到正确 ID。
		matched, err := c.Scraper.EnrichLibrary(ctx, lib.ID, true)
		if err != nil {
			if c.Log != nil {
				c.Log.Warn("repair rescrape library failed", zap.String("library", lib.ID), zap.Error(err))
			}
			continue
		}
		result.Matched += matched
	}
	if c.Log != nil {
		c.Log.Info("repair and rescrape all libraries done",
			zap.Int("repaired", result.Repaired),
			zap.Int("libraries", result.Libraries),
			zap.Int("matched", result.Matched))
	}
	return result, nil
}

// RepairAndRescrapeLibrary 修复并重刮单个媒体库:先从该库媒体路径中的占位符
// 回填缺失/错误的外部 ID(重置相关行 scrape_status=pending),再对该库重刮
// (含 no_match 重试)。用于「按媒体库」单独触发修复,不影响其它库。
func (c *Container) RepairAndRescrapeLibrary(ctx context.Context, libraryID string) (RepairAndRescrapeResult, error) {
	var result RepairAndRescrapeResult
	libraryID = strings.TrimSpace(libraryID)
	if c == nil || c.Repo == nil || c.Repo.DB == nil || libraryID == "" {
		return result, nil
	}
	repaired, err := c.RepairCloudPathMetadata(ctx, libraryID)
	if err != nil {
		return result, err
	}
	result.Repaired = repaired

	if c.Scraper == nil {
		return result, nil
	}
	result.Libraries = 1
	// retryNoMatch=true:连之前匹配失败的也再试一次,因为这次可能已回填到正确 ID。
	matched, err := c.Scraper.EnrichLibrary(ctx, libraryID, true)
	if err != nil {
		return result, err
	}
	result.Matched = matched
	if c.Log != nil {
		c.Log.Info("repair and rescrape library done",
			zap.String("library", libraryID),
			zap.Int("repaired", result.Repaired),
			zap.Int("matched", result.Matched))
	}
	return result, nil
}

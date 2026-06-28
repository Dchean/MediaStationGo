package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (o *OrganizerService) lookupOrganizeMetadata(ctx context.Context, src, sourceRoot, mediaType, title string, year, season, episode int, cache map[string]*Match) *Match {
	normalizedType := normalizeOrganizeMediaType(mediaType)
	lookupSeason, lookupEpisode := season, episode
	if normalizedType == "movie" {
		lookupSeason, lookupEpisode = 0, 0
	}
	seriesLike := isSeriesLibraryType(mediaType) || (normalizedType != "movie" && (season > 0 || episode > 0))
	if local, err := ReadLocalMetadata(src, sourceRoot, seriesLike); err == nil && local != nil {
		if match := organizeMatchFromLocalMetadata(local); match != nil {
			return match
		}
	} else if err != nil && o.log != nil {
		o.log.Debug("organize read local metadata before rename failed", zap.String("path", src), zap.Error(err))
	}
	if match := o.lookupOrganizeAdultMetadata(ctx, src, mediaType, title); match != nil {
		return match
	}
	if o == nil || o.scraper == nil || !o.scraper.AnyEnabled() {
		return nil
	}
	libType := normalizeOrganizeMediaType(mediaType)
	if libType == "" {
		libType = organizeLibraryModelType(mediaType)
	}
	lib := &model.Library{Path: sourceRoot, Type: libType, Enabled: true}
	media := &model.Media{
		Title:      title,
		Year:       year,
		Path:       src,
		SeasonNum:  lookupSeason,
		EpisodeNum: lookupEpisode,
	}
	for _, candidate := range scrapeQueryCandidatesWithRecognition(ctx, o.repo, media, lib) {
		key := organizeMetadataCacheKey(lib.Type, candidate, year)
		if cache != nil {
			if cached, ok := cache[key]; ok {
				if cached != nil {
					return cached
				}
				continue
			}
		}
		match := o.scraper.lookup(ctx, lib, media, candidate, year)
		if match != nil && strings.TrimSpace(match.Title) != "" {
			if !organizeMetadataMatchTrusted(candidate, year, match) {
				if cache != nil {
					cache[key] = nil
				}
				if o.log != nil {
					o.log.Warn("organize metadata match rejected before rename",
						zap.String("source", src),
						zap.String("query", candidate),
						zap.String("title", match.Title),
						zap.String("media_type", match.MediaType),
						zap.Int("source_year", year),
						zap.Int("match_year", match.Year),
						zap.Int("tmdb_id", match.TMDbID),
						zap.Int("bangumi_id", match.BangumiID),
						zap.String("douban_id", match.DoubanID),
						zap.String("thetvdb_id", match.TheTVDBID))
				}
				continue
			}
			preferLocalizedSearchTitle(candidate, match)
			if cache != nil {
				cache[key] = match
			}
			if o.log != nil {
				o.log.Info("organize metadata matched before rename",
					zap.String("source", src),
					zap.String("query", candidate),
					zap.String("title", match.Title),
					zap.String("media_type", match.MediaType),
					zap.Int("year", match.Year),
					zap.Int("tmdb_id", match.TMDbID),
					zap.Int("bangumi_id", match.BangumiID),
					zap.String("douban_id", match.DoubanID),
					zap.String("thetvdb_id", match.TheTVDBID))
			}
			return match
		}
		if cache != nil {
			cache[key] = nil
		}
	}
	return nil
}

func (o *OrganizerService) lookupOrganizeAdultMetadata(ctx context.Context, src, mediaType, title string) *Match {
	if o == nil || o.scraper == nil || o.scraper.adult == nil || !o.scraper.adult.Enabled() {
		return nil
	}
	isAdult := normalizeOrganizeMediaType(mediaType) == "adult"
	candidates := []string{src, filepath.Base(src), title}
	outCodes := make([]string, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		code := normalizeAdultCode(candidate)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		outCodes = append(outCodes, code)
	}
	if !isAdult && len(outCodes) == 0 {
		return nil
	}
	for _, code := range outCodes {
		match, err := o.scraper.adult.Search(ctx, code)
		if err != nil {
			if o.log != nil {
				o.log.Debug("organize adult metadata search failed", zap.String("source", src), zap.String("code", code), zap.Error(err))
			}
			continue
		}
		if match != nil && strings.TrimSpace(match.Title) != "" {
			if o.log != nil {
				o.log.Info("organize adult metadata matched before rename",
					zap.String("source", src),
					zap.String("code", code),
					zap.String("title", match.Title))
			}
			return match
		}
	}
	return nil
}

func organizeMetadataMatchTrusted(query string, sourceYear int, match *Match) bool {
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return false
	}
	if unsafeAutomaticEpisodeQuery(query) {
		return false
	}
	if sourceYear > 0 && match.Year > 0 {
		diff := sourceYear - match.Year
		if diff < 0 {
			diff = -diff
		}
		if diff > 1 {
			return false
		}
	}
	return automaticMetadataTitleTrusted(query, match)
}

func organizeMatchFromLocalMetadata(local *LocalMetadata) *Match {
	if local == nil || strings.TrimSpace(local.Title) == "" {
		return nil
	}
	match := &Match{
		Title:        strings.TrimSpace(local.Title),
		OriginalName: strings.TrimSpace(local.OriginalName),
		Overview:     local.Overview,
		PosterURL:    local.PosterURL,
		BackdropURL:  local.BackdropURL,
		Year:         local.Year,
		Rating:       local.Rating,
		TMDbID:       local.TMDbID,
		DoubanID:     local.DoubanID,
		TheTVDBID:    local.TheTVDBID,
		NSFW:         local.NSFW,
	}
	if local.Genres != "" {
		match.Genres = splitNFOList(local.Genres)
	}
	if local.Countries != "" {
		match.Countries = splitNFOList(local.Countries)
	}
	if local.Languages != "" {
		match.Languages = splitNFOList(local.Languages)
	}
	return match
}

func (o *OrganizerService) lookupOrganizeSourceMedia(ctx context.Context, path string) *model.Media {
	if o == nil || o.repo == nil || o.repo.DB == nil {
		return nil
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil
	}
	var media model.Media
	if err := o.repo.DB.WithContext(ctx).
		Where("path = ? AND deleted_at IS NULL", path).
		Limit(1).
		Take(&media).Error; err != nil {
		return nil
	}
	return &media
}

func organizeMatchFromMedia(media *model.Media) *Match {
	if media == nil || strings.TrimSpace(media.Title) == "" {
		return nil
	}
	return &Match{
		TMDbID:       media.TMDbID,
		BangumiID:    media.BangumiID,
		DoubanID:     strings.TrimSpace(media.DoubanID),
		TheTVDBID:    strings.TrimSpace(media.TheTVDBID),
		Title:        strings.TrimSpace(media.Title),
		OriginalName: strings.TrimSpace(media.OriginalName),
		Overview:     media.Overview,
		PosterURL:    media.PosterURL,
		BackdropURL:  media.BackdropURL,
		Year:         media.Year,
		Rating:       media.Rating,
		Languages:    parseCommaList(media.Languages),
		Countries:    parseCommaList(media.Countries),
		Genres:       parseCommaList(media.Genres),
		NSFW:         media.NSFW,
	}
}

func applyOrganizeMetadataMatch(match *Match, title, parsedTitle *string, year *int) {
	if match == nil {
		return
	}
	if matchedTitle := sanitizeFilename(strings.TrimSpace(match.Title)); matchedTitle != "" {
		*title = matchedTitle
		*parsedTitle = strings.TrimSpace(match.Title)
	}
	if match.Year > 0 {
		*year = match.Year
	}
}

func organizeMetadataCacheKey(mediaType, query string, year int) string {
	return strings.ToLower(strings.TrimSpace(mediaType)) + "|" + fmt.Sprint(year) + "|" + strings.ToLower(strings.TrimSpace(query))
}

func (o *OrganizerService) smartClassifySourceFile(ctx context.Context, src, sourceRoot, mediaType, title, parsedTitle string, metadataMatch *Match) string {
	if o == nil || !o.isSmartClassifyEnabled(ctx) {
		return ""
	}
	seriesLike := isSeriesLibraryType(mediaType)
	input := mediaClassifyInput{
		MediaType: mediaType,
		Title:     strings.Join([]string{title, parsedTitle, filepath.Base(src)}, " "),
		Category:  strings.Join(organizeDirectoryCategoryCandidates(src, sourceRoot), " "),
	}
	if metadataMatch != nil {
		input.Title = strings.Join([]string{
			metadataMatch.OriginalName,
			title,
			parsedTitle,
			filepath.Base(src),
		}, " ")
		input.Languages = metadataMatch.Languages
		input.Countries = metadataMatch.Countries
		input.Genres = metadataMatch.Genres
		if metadataMatch.NSFW {
			input.MediaType = "adult"
		}
	}
	if meta, err := ReadLocalMetadata(src, sourceRoot, seriesLike); err == nil && meta != nil && meta.HasNFO {
		input.Title = strings.Join([]string{meta.Title, meta.OriginalName, title, parsedTitle, filepath.Base(src)}, " ")
		input.Languages = parseCommaList(meta.Languages)
		input.Countries = parseCommaList(meta.Countries)
		input.Genres = parseCommaList(meta.Genres)
		if meta.NSFW {
			input.MediaType = "adult"
		}
	}
	return sanitizeFilename(classifyMediaCategory(input, o.categoryMap()))
}

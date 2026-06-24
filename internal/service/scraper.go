// Package service — scraper orchestrator.
//
// ScraperService takes a Media row and tries to enrich it with metadata from
// local NFO first, then TMDb -> Douban -> Bangumi -> TheTVDB. Fanart.tv is
// artwork-only and upgrades poster/backdrop after a metadata match.
package service

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ScraperService coordinates metadata enrichment across providers.
type ScraperService struct {
	cfg     *config.Config
	log     *zap.Logger
	repo    *repository.Container
	tmdb    *TMDbProvider
	bangumi *BangumiProvider
	thetvdb *TheTVDBProvider
	douban  *DoubanProvider
	fanart  *FanartProvider
	adult   *AdultProvider
	hub     *Hub
	notify  *NotifyChannelService
	cache   *RuntimeCacheService
	images  *ImageProxy
}

type ScrapeOptions struct {
	RetryNoMatch        bool
	IncludeMatched      bool
	EpisodeArtwork      *bool
	DeferEpisodeDetails bool
}

func (o ScrapeOptions) episodeArtworkEnabled() bool {
	return o.EpisodeArtwork == nil || *o.EpisodeArtwork
}

func skipEpisodeArtworkOptions(retryNoMatch bool) ScrapeOptions {
	episodeArtwork := false
	return ScrapeOptions{RetryNoMatch: retryNoMatch, EpisodeArtwork: &episodeArtwork}
}

// NewScraperService is the constructor.
func NewScraperService(
	cfg *config.Config,
	log *zap.Logger,
	repo *repository.Container,
	tmdb *TMDbProvider,
	bangumi *BangumiProvider,
	thetvdb *TheTVDBProvider,
	fanart *FanartProvider,
	hub *Hub,
	adult ...*AdultProvider,
) *ScraperService {
	var adultProvider *AdultProvider
	if len(adult) > 0 {
		adultProvider = adult[0]
	}
	return &ScraperService{
		cfg: cfg, log: log, repo: repo,
		tmdb: tmdb, bangumi: bangumi, thetvdb: thetvdb, fanart: fanart, adult: adultProvider, hub: hub,
	}
}

func (s *ScraperService) SetDouban(douban *DoubanProvider) {
	s.douban = douban
}

func (s *ScraperService) SetNotifyChannels(notify *NotifyChannelService) {
	if s != nil {
		s.notify = notify
	}
}

func (s *ScraperService) SetRuntimeCache(cache *RuntimeCacheService) *ScraperService {
	if s != nil {
		s.cache = cache
	}
	return s
}

func (s *ScraperService) SetImageProxy(images *ImageProxy) *ScraperService {
	if s != nil {
		s.images = images
	}
	return s
}

var tmdbDetailsTimeout = 8 * time.Second

const (
	defaultScrapeDelayMinMS = 250
	defaultScrapeDelayMaxMS = 500
	maxScrapeDelayMS        = 5 * 60 * 1000
)

// EnrichOne runs the provider chain for a single media row.
func (s *ScraperService) EnrichOne(ctx context.Context, m *model.Media) error {
	return s.EnrichOneWithOptions(ctx, m, ScrapeOptions{})
}

func (s *ScraperService) EnrichOneWithOptions(ctx context.Context, m *model.Media, options ScrapeOptions) error {
	lib, err := s.repo.Library.FindByID(ctx, m.LibraryID)
	if err != nil {
		return err
	}

	seriesLike := mediaIsEpisodic(m, lib)
	cloudMedia := isCloudMediaPath(m.Path) || (lib != nil && isCloudMediaPath(lib.Path))
	var local *LocalMetadata
	if !cloudMedia {
		if found, err := ReadLocalMetadata(m.Path, lib.Path, seriesLike); err == nil && found != nil {
			local = found
		} else if err != nil {
			s.log.Warn("read local metadata before scrape failed", zap.String("media_id", m.ID), zap.Error(err))
		}
	}
	if hinted, _ := pathHintMetadata(m.Path, seriesLike); hinted != nil {
		local = mergeScrapePathHintMetadata(local, hinted)
	}
	if local != nil {
		applyLocalMetadata(m, local)
	}

	year := mediaYearHint(m)

	if s.adult != nil && s.adult.Enabled() {
		if code := firstText(localAdultCode(local), AdultCodeFromMediaPath(m.Path), normalizeAdultCode(m.OriginalName), normalizeAdultCode(m.Title)); code != "" {
			if adultMatch, err := s.adult.Search(ctx, code); err == nil && adultMatch != nil {
				mergeLocalMetadataIntoMatch(adultMatch, local)
				return s.applyProviderMatchWithOptions(ctx, m, lib, adultMatch, options)
			} else if err != nil {
				s.log.Debug("adult metadata search failed", zap.String("media_id", m.ID), zap.String("code", code), zap.Error(err))
			}
		}
	}

	if match := s.matchFromMediaExternalIDs(ctx, m, lib); match != nil {
		s.applyFanartArtwork(ctx, match)
		mergeLocalMetadataIntoMatch(match, local)
		return s.applyProviderMatchWithOptions(ctx, m, lib, match, options)
	}

	candidates := scrapeQueryCandidates(m, lib)
	var query string
	match := (*Match)(nil)
	for _, candidate := range candidates {
		query = candidate
		candidateMatch := s.lookup(ctx, lib, m, candidate, year)
		if candidateMatch == nil {
			continue
		}
		if !organizeMetadataMatchTrusted(candidate, year, candidateMatch) {
			s.log.Warn("metadata scrape match rejected",
				zap.String("media_id", m.ID),
				zap.String("query", candidate),
				zap.String("title", candidateMatch.Title),
				zap.Int("source_year", year),
				zap.Int("match_year", candidateMatch.Year),
				zap.Int("tmdb_id", candidateMatch.TMDbID),
				zap.Int("bangumi_id", candidateMatch.BangumiID),
				zap.String("douban_id", candidateMatch.DoubanID),
				zap.String("thetvdb_id", candidateMatch.TheTVDBID))
			continue
		}
		match = candidateMatch
		if match != nil {
			break
		}
	}
	if match == nil {
		if local != nil && !local.PathHint {
			return s.applyLocalMetadataMatch(ctx, m, local)
		}
		_ = s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).
			Update("scrape_status", "no_match").Error
		s.invalidateMediaCache(ctx)
		s.log.Info("metadata scrape no match",
			zap.String("media_id", m.ID),
			zap.String("query", query),
			zap.String("library_type", lib.Type))
		return nil
	}
	s.applyFanartArtwork(ctx, match)
	mergeLocalMetadataIntoMatch(match, local)

	return s.applyProviderMatchWithOptions(ctx, m, lib, match, options)
}

func (s *ScraperService) matchFromMediaExternalIDs(ctx context.Context, m *model.Media, lib *model.Library) *Match {
	if s == nil || m == nil {
		return nil
	}
	mediaType := ""
	if lib != nil {
		mediaType = lib.Type
	}
	if m.TMDbID > 0 {
		if mediaIsEpisodic(m, lib) {
			mediaType = "tv"
		}
		if match := s.manualTMDbMatchByID(ctx, m.TMDbID, normalizeMediaType(mediaType, m.Title, "")); match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "tmdb") {
				return match
			}
		}
	}
	if strings.TrimSpace(m.DoubanID) != "" && s.douban != nil && s.douban.Enabled() {
		if match, err := s.douban.GetMatchByID(ctx, strings.TrimSpace(m.DoubanID)); err == nil && match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "douban") {
				return match
			}
		} else if err != nil {
			s.log.Debug("douban id lookup failed", zap.String("media_id", m.ID), zap.String("douban_id", m.DoubanID), zap.Error(err))
		}
	}
	if m.BangumiID > 0 && s.bangumi != nil && s.bangumi.Enabled() {
		if match, err := s.bangumi.GetSubject(ctx, m.BangumiID); err == nil && match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "bangumi") {
				return match
			}
		} else if err != nil {
			s.log.Debug("bangumi id lookup failed", zap.String("media_id", m.ID), zap.Int("bangumi_id", m.BangumiID), zap.Error(err))
		}
	}
	if strings.TrimSpace(m.TheTVDBID) != "" && s.thetvdb != nil && s.thetvdb.Enabled() {
		if match, err := s.thetvdb.GetSeriesMatchByID(ctx, strings.TrimSpace(m.TheTVDBID)); err == nil && match != nil {
			if s.mediaExternalIDMatchTrusted(m, lib, match, "thetvdb") {
				return match
			}
		} else if err != nil {
			s.log.Debug("thetvdb id lookup failed", zap.String("media_id", m.ID), zap.String("thetvdb_id", m.TheTVDBID), zap.Error(err))
		}
	}
	return nil
}

func (s *ScraperService) mediaExternalIDMatchTrusted(m *model.Media, lib *model.Library, match *Match, source string) bool {
	if match == nil || strings.TrimSpace(match.Title) == "" {
		return false
	}
	if !mediaIsEpisodic(m, lib) {
		return true
	}
	for _, candidate := range scrapeQueryCandidates(m, lib) {
		if unsafeAutomaticEpisodeQuery(candidate) {
			continue
		}
		if automaticMetadataTitleTrusted(candidate, match) {
			return true
		}
	}
	if s != nil && s.log != nil && m != nil {
		s.log.Warn("episode external id match rejected",
			zap.String("media_id", m.ID),
			zap.String("source", source),
			zap.String("title", match.Title),
			zap.String("original_name", match.OriginalName),
			zap.Int("tmdb_id", match.TMDbID),
			zap.Int("bangumi_id", match.BangumiID),
			zap.String("douban_id", match.DoubanID),
			zap.String("thetvdb_id", match.TheTVDBID))
	}
	return false
}

func (s *ScraperService) applyFanartArtwork(ctx context.Context, match *Match) {
	if s == nil || s.fanart == nil || !s.fanart.Enabled() || match == nil {
		return
	}
	apply := func(a *Artwork) {
		if a == nil {
			return
		}
		if a.Poster != "" {
			match.PosterURL = a.Poster
		}
		if a.Backdrop != "" {
			match.BackdropURL = a.Backdrop
		}
	}
	if match.TMDbID > 0 {
		if a, err := s.fanart.MovieArtwork(ctx, match.TMDbID); err == nil {
			apply(a)
		} else {
			s.log.Debug("fanart movie artwork failed", zap.Int("tmdb_id", match.TMDbID), zap.Error(err))
		}
	}
	if strings.TrimSpace(match.TheTVDBID) != "" {
		if a, err := s.fanart.TVArtwork(ctx, strings.TrimSpace(match.TheTVDBID)); err == nil {
			apply(a)
		} else {
			s.log.Debug("fanart tv artwork failed", zap.String("thetvdb_id", match.TheTVDBID), zap.Error(err))
		}
	}
}

func mediaYearHint(m *model.Media) int {
	if m == nil {
		return 0
	}
	if m.SeasonNum > 0 || m.EpisodeNum > 0 {
		return seriesPathYearHint(m.Path)
	}
	if season, episode := ParseEpisode(m.Path); season > 0 || episode > 0 {
		return seriesPathYearHint(m.Path)
	}
	if m.Year > 0 {
		return m.Year
	}
	if _, year := CleanQuery(filepath.Base(m.Path)); year > 0 {
		return year
	}
	return yearFromText(m.Path)
}

func seriesPathYearHint(path string) int {
	if showDir := showDirFromEpisodePath(path); showDir != "" {
		if _, year := CleanQuery(showDir); year > 0 {
			return year
		}
	}
	return 0
}

func yearFromText(raw string) int {
	if raw == "" {
		return 0
	}
	matches := yearPattern.FindStringSubmatch(strings.ToLower(raw))
	if len(matches) < 2 {
		return 0
	}
	year, _ := strconv.Atoi(matches[1])
	return year
}

func localAdultCode(local *LocalMetadata) string {
	if local == nil {
		return ""
	}
	return local.AdultCode
}

func mergeLocalMetadataIntoMatch(match *Match, local *LocalMetadata) {
	if match == nil || local == nil {
		return
	}
	if local.PathHint {
		mergePathHintIDsIntoMatch(match, local)
		return
	}
	if local.Title != "" {
		match.Title = local.Title
	}
	if local.OriginalName != "" {
		match.OriginalName = local.OriginalName
	}
	if local.AdultCode != "" {
		match.OriginalName = local.AdultCode
		match.NSFW = true
	}
	if local.Overview != "" {
		match.Overview = local.Overview
	}
	if local.PosterURL != "" {
		match.PosterURL = local.PosterURL
	}
	if local.BackdropURL != "" {
		match.BackdropURL = local.BackdropURL
	}
	if local.Rating > 0 {
		match.Rating = local.Rating
	}
	if local.Year > 0 {
		match.Year = local.Year
	}
	if local.TMDbID > 0 {
		match.TMDbID = local.TMDbID
	}
	if local.BangumiID > 0 {
		match.BangumiID = local.BangumiID
	}
	if local.DoubanID != "" {
		match.DoubanID = local.DoubanID
	}
	if local.TheTVDBID != "" {
		match.TheTVDBID = local.TheTVDBID
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
	if local.NSFW {
		match.NSFW = true
	}
}

func mergePathHintIDsIntoMatch(match *Match, local *LocalMetadata) {
	if match == nil || local == nil {
		return
	}
	if local.TMDbID > 0 {
		match.TMDbID = local.TMDbID
	}
	if local.BangumiID > 0 {
		match.BangumiID = local.BangumiID
	}
	if local.DoubanID != "" {
		match.DoubanID = local.DoubanID
	}
	if local.TheTVDBID != "" {
		match.TheTVDBID = local.TheTVDBID
	}
	if match.Year <= 0 && local.Year > 0 {
		match.Year = local.Year
	}
}

func (s *ScraperService) applyProviderMatch(ctx context.Context, m *model.Media, lib *model.Library, match *Match) error {
	return s.applyProviderMatchWithOptions(ctx, m, lib, match, ScrapeOptions{})
}

func (s *ScraperService) applyProviderMatchWithOptions(ctx context.Context, m *model.Media, lib *model.Library, match *Match, options ScrapeOptions) error {
	posterCandidate := match.PosterURL
	backdropCandidate := match.BackdropURL
	posterURL, removePoster := s.prepareScrapedArtworkURL(ctx, m.ID, "poster_url", m.PosterURL, posterCandidate)
	backdropURL, removeBackdrop := s.prepareScrapedArtworkURL(ctx, m.ID, "backdrop_url", m.BackdropURL, backdropCandidate)
	updates := map[string]any{
		"title":         match.Title,
		"overview":      match.Overview,
		"poster_url":    posterURL,
		"backdrop_url":  backdropURL,
		"rating":        match.Rating,
		"year":          match.Year,
		"scrape_status": "matched",
	}
	if match.OriginalName != "" {
		updates["original_name"] = match.OriginalName
	}
	if strings.TrimSpace(m.EpisodeTitle) != "" {
		updates["episode_title"] = strings.TrimSpace(m.EpisodeTitle)
	}
	if match.TMDbID > 0 {
		updates["tm_db_id"] = match.TMDbID
	}
	if match.BangumiID > 0 {
		updates["bangumi_id"] = match.BangumiID
	}
	if match.DoubanID != "" {
		updates["douban_id"] = match.DoubanID
	}
	if match.TheTVDBID != "" {
		updates["thetvdb_id"] = match.TheTVDBID
	}
	if match.NSFW {
		updates["nsfw"] = true
	}
	if len(match.Genres) > 0 {
		updates["genres"] = strings.Join(match.Genres, ",")
	}
	if len(match.Countries) > 0 {
		updates["countries"] = strings.Join(match.Countries, ",")
	}
	if len(match.Languages) > 0 {
		updates["languages"] = strings.Join(match.Languages, ",")
	}

	if err := s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).
		Updates(updates).Error; err != nil {
		return err
	}
	s.removeCachedScrapedArtwork(removePoster, removeBackdrop)

	// Fetch extended metadata after the selected match is already saved.
	// Manual cloud/batch applies must not fail just because an optional provider
	// details request is slow or unavailable.
	if match.TMDbID > 0 && s.tmdb != nil && s.tmdb.Enabled() {
		mediaType := s.determineMediaTypeForMedia(lib, m, match)
		s.fetchAndSaveTMDbExtendedMetadata(ctx, m.ID, match.TMDbID, mediaType)
		if mediaType == "tv" && !options.DeferEpisodeDetails {
			s.fetchAndSaveTMDbEpisodeDetails(ctx, m, match.TMDbID, match.Year, options)
		}
	}
	if !(options.DeferEpisodeDetails && m != nil && m.EpisodeNum > 0) {
		s.writeMediaNFOAfterScrape(ctx, m, lib)
	}
	s.invalidateMediaCache(ctx)
	s.hub.Publish("scrape", map[string]any{
		"media_id":   m.ID,
		"title":      match.Title,
		"tmdb_id":    match.TMDbID,
		"bangumi_id": match.BangumiID,
		"douban_id":  match.DoubanID,
		"thetvdb_id": match.TheTVDBID,
		"source":     map[bool]string{true: "adult"}[match.NSFW],
	})
	return nil
}

func mergeScrapePathHintMetadata(dst, src *LocalMetadata) *LocalMetadata {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneLocalMetadata(src)
	}
	hasLocalMetadata := localMetadataMarksMatched(dst)
	if dst.Title == "" && src.Title != "" {
		dst.Title = src.Title
	}
	if dst.Year <= 0 && src.Year > 0 {
		dst.Year = src.Year
	}
	if src.TMDbID > 0 {
		dst.TMDbID = src.TMDbID
	}
	if src.BangumiID > 0 {
		dst.BangumiID = src.BangumiID
	}
	if strings.TrimSpace(src.DoubanID) != "" {
		dst.DoubanID = strings.TrimSpace(src.DoubanID)
	}
	if strings.TrimSpace(src.TheTVDBID) != "" {
		dst.TheTVDBID = strings.TrimSpace(src.TheTVDBID)
	}
	if !hasLocalMetadata {
		dst.PathHint = dst.PathHint || src.PathHint
	}
	return dst
}

func isCloudMediaPath(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "cloud://")
}

func (s *ScraperService) applyLocalMetadataMatch(ctx context.Context, m *model.Media, local *LocalMetadata) error {
	next := *m
	applyLocalMetadata(&next, local)
	status := "matched"
	if !localMetadataMarksMatched(local) {
		status = "pending"
	}
	updates := map[string]any{
		"title":         next.Title,
		"scrape_status": status,
	}
	if next.OriginalName != "" {
		updates["original_name"] = next.OriginalName
	}
	if next.EpisodeTitle != "" {
		updates["episode_title"] = next.EpisodeTitle
	}
	if next.Overview != "" {
		updates["overview"] = next.Overview
	}
	if next.PosterURL != "" {
		updates["poster_url"] = next.PosterURL
	}
	if next.BackdropURL != "" {
		updates["backdrop_url"] = next.BackdropURL
	}
	if next.Rating > 0 {
		updates["rating"] = next.Rating
	}
	if next.Year > 0 {
		updates["year"] = next.Year
	}
	if next.TMDbID > 0 {
		updates["tm_db_id"] = next.TMDbID
	}
	if next.BangumiID > 0 {
		updates["bangumi_id"] = next.BangumiID
	}
	if next.DoubanID != "" {
		updates["douban_id"] = next.DoubanID
	}
	if next.TheTVDBID != "" {
		updates["thetvdb_id"] = next.TheTVDBID
	}
	if next.SeasonNum > 0 || next.EpisodeNum > 0 {
		updates["season_num"] = next.SeasonNum
	}
	if next.EpisodeNum > 0 {
		updates["episode_num"] = next.EpisodeNum
	}
	if next.Genres != "" {
		updates["genres"] = next.Genres
	}
	if next.Countries != "" {
		updates["countries"] = next.Countries
	}
	if next.Languages != "" {
		updates["languages"] = next.Languages
	}
	if next.NSFW {
		updates["nsfw"] = true
	}
	if err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("id = ?", m.ID).Updates(updates).Error; err != nil {
		return err
	}
	s.invalidateMediaCache(ctx)
	s.hub.Publish("scrape", map[string]any{
		"media_id":  m.ID,
		"title":     next.Title,
		"tmdb_id":   next.TMDbID,
		"douban_id": next.DoubanID,
		"source":    "local_nfo",
	})
	return nil
}

func (s *ScraperService) invalidateMediaCache(ctx context.Context) {
	if s != nil && s.cache != nil {
		s.cache.DeletePrefix(ctx, "media:")
		s.cache.DeletePrefix(ctx, "stats:")
	}
}

// AnyEnabled reports whether at least one provider can run.
func (s *ScraperService) AnyEnabled() bool {
	if s.tmdb != nil && s.tmdb.Enabled() {
		return true
	}
	if s.bangumi != nil && s.bangumi.Enabled() {
		return true
	}
	if s.thetvdb != nil && s.thetvdb.Enabled() {
		return true
	}
	if s.adult != nil && s.adult.Enabled() {
		return true
	}
	if s.douban != nil && s.douban.Enabled() {
		return true
	}
	return false
}

// determineMediaType returns "tv" for TV shows and "movie" for movies.
// It uses the library type as the primary signal.
func (s *ScraperService) determineMediaType(lib *model.Library, match *Match) string {
	return s.determineMediaTypeForMedia(lib, nil, match)
}

func (s *ScraperService) determineMediaTypeForMedia(lib *model.Library, media *model.Media, match *Match) string {
	if media != nil && mediaIsEpisodic(media, lib) {
		return "tv"
	}
	if lib != nil {
		switch lib.Type {
		case "tv", "anime", "variety", "show", "shows":
			return "tv"
		}
	}
	// Fallback: if Bangumi ID is present, treat as TV/anime
	if match != nil && match.BangumiID > 0 {
		return "tv"
	}
	return "movie"
}

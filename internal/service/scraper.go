// Package service — scraper orchestrator.
//
// ScraperService takes a Media row and tries to enrich it with metadata from
// local NFO first, then TMDb -> Douban -> Bangumi -> TheTVDB. Fanart.tv is
// artwork-only and upgrades poster/backdrop after a metadata match.
package service

import (
	"context"
	"path/filepath"
	"regexp"
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

// yearPattern extracts a 4-digit year (1900-2099).
var yearPattern = regexp.MustCompile(`(?:^|[^\d])(19\d{2}|20\d{2})(?:[^\d]|$)`)

var tmdbDetailsTimeout = 8 * time.Second

// noiseTokens are stripped before search.
var noiseTokens = []string{
	// 视频规格
	"1080p", "2160p", "4k", "720p", "480p", "uhd", "ds4k", "fhd",
	"bd", "bdrip", "brrip", "dvd", "dvdrip", "hdtv", "pdtv", "webdl",
	"hdrip", "bluray", "blu-ray", "webrip", "web-dl", "web",
	"x264", "x265", "h264", "h265", "hevc", "avc", "10bit", "8bit", "hi10p", "hi10",
	"hdr", "hdr10", "sdr", "dts", "ddp", "ddp5", "dd5", "dd2", "eac3", "truehd",
	"dovi", "atmos", "aac", "ac3", "flac",
	"remux", "extended", "uncut", "remastered", "repack", "proper", "internal",
	"limited", "imax", "directors-cut", "directors_cut",
	"hkfree", "yify", "rarbg", "ettv", "fgt", "tgx", "ctrlhd", "ntb", "flux",

	// 流媒体平台 / 字幕组 / 国家版本（动漫常见）
	"netflix", "nf", "amzn", "hulu", "disney", "max", "hbo",
	"linetv", "ourtv", "iqiyi", "youku", "bilibili", "qiyi", "krj",
	"crunchyroll", "funimation", "anidb", "horriblesubs", "subsplease",
	"erai-raws", "judas", "asw", "smcat", "leopard-raws", "ohys-raws", "colortv",

	// 中文字幕标记
	"zm", "zw", "ch", "chs", "cht", "cn", "tc", "sc",
	"中字", "繁字", "简中", "繁中", "国语", "粤语", "日语",

	// 季数前缀残留 — ParseEpisode 已抽取过
	"season", "264", "265",
}

var noiseTokenSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(noiseTokens)+1)
	for _, token := range noiseTokens {
		set[token] = struct{}{}
	}
	set["dl"] = struct{}{}
	return set
}()

var releaseBoundaryTokenSet = map[string]struct{}{
	"1080p": {}, "2160p": {}, "4k": {}, "720p": {}, "480p": {}, "uhd": {}, "fhd": {},
	"bd": {}, "bdrip": {}, "brrip": {}, "dvd": {}, "dvdrip": {}, "hdtv": {}, "pdtv": {},
	"webdl": {}, "hdrip": {}, "bluray": {}, "webrip": {}, "web": {}, "remux": {},
	"x264": {}, "x265": {}, "h264": {}, "h265": {}, "hevc": {}, "avc": {},
}
var strictSeasonFolderPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:s|season)\.?\s*(\d{1,2})$`),
	regexp.MustCompile(`^第\s*([0-9一二三四五六七八九十百零两]+)\s*季$`),
}

// bracketedTag matches "[anything]", "(anything)" or "{anything}" segments.
var bracketedTag = regexp.MustCompile(`[\[\(\{][^\]\)\}]*[\]\)\}]`)
var multiWordNoise = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bweb[\s._-]*dl\b`),
	regexp.MustCompile(`(?i)\bblu[\s._-]*ray\b`),
	regexp.MustCompile(`(?i)\bdirectors[\s._-]*cut\b`),
	regexp.MustCompile(`(?i)\berai[\s._-]*raws\b`),
	regexp.MustCompile(`(?i)\bohys[\s._-]*raws\b`),
}

const (
	defaultScrapeDelayMinMS = 250
	defaultScrapeDelayMaxMS = 500
	maxScrapeDelayMS        = 5 * 60 * 1000
)

// CleanQuery converts a filename like "Inception.2010.1080p.BluRay.x264.mkv"
// into a TMDb-friendly title plus an optional year hint.
func CleanQuery(raw string) (title string, year int) {
	name := strings.TrimSuffix(filepath.Base(raw), filepath.Ext(raw))
	lower := strings.ToLower(name)

	if m := yearPattern.FindStringSubmatch(lower); len(m) >= 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			year = v
			lower = strings.ReplaceAll(lower, m[1], " ")
		}
	}

	lower = bracketedTag.ReplaceAllString(lower, " ")

	lower = patSEnE.ReplaceAllString(lower, " ")
	lower = patNxE.ReplaceAllString(lower, " ")
	lower = patEP.ReplaceAllString(lower, " ")
	lower = patCN.ReplaceAllString(lower, " ")
	// 去掉中文季/部标记（如「第二季」「第2部」），避免残留在标题里既污染
	// 搜索查询又导致整理后的目录名重复季信息。
	lower = patSeasonOnly.ReplaceAllString(lower, " ")
	lower = patCNSeason.ReplaceAllString(lower, " ")

	for _, pat := range multiWordNoise {
		lower = pat.ReplaceAllString(lower, " ")
	}
	for _, sep := range []string{".", "_", "-", "[", "]", "(", ")", "×"} {
		lower = strings.ReplaceAll(lower, sep, " ")
	}
	// 拆分后丢掉过短（≤1）且全为 ASCII 数字 / 字母的"碎片"，避免
	// 「2」「0」「v」之类残留干扰 TMDb 搜索。中文字符不算碎片。
	out := make([]string, 0, 8)
	seenReleaseBoundary := false
	for _, w := range strings.Fields(lower) {
		if _, ok := noiseTokenSet[w]; ok {
			if _, boundary := releaseBoundaryTokenSet[w]; boundary {
				seenReleaseBoundary = true
			}
			continue
		}
		if seenReleaseBoundary && isASCIIWord(w) {
			continue
		}
		if len(w) <= 1 {
			r := []rune(w)
			if len(r) == 1 && r[0] < 128 {
				continue
			}
		}
		out = append(out, w)
	}
	title = strings.TrimSpace(strings.Join(out, " "))
	return title, year
}

func isASCIIWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 128 {
			return false
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// EnrichOne runs the provider chain for a single media row.
func (s *ScraperService) EnrichOne(ctx context.Context, m *model.Media) error {
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
			applyLocalMetadata(m, local)
		} else if err != nil {
			s.log.Warn("read local metadata before scrape failed", zap.String("media_id", m.ID), zap.Error(err))
		}
	} else if hinted, _ := pathHintMetadata(m.Path, seriesLike); hinted != nil {
		local = mergeCloudMetadata(local, hinted)
		applyLocalMetadata(m, local)
	}

	year := mediaYearHint(m)

	if s.adult != nil && s.adult.Enabled() {
		if code := firstText(localAdultCode(local), AdultCodeFromMediaPath(m.Path), normalizeAdultCode(m.OriginalName), normalizeAdultCode(m.Title)); code != "" {
			if adultMatch, err := s.adult.Search(ctx, code); err == nil && adultMatch != nil {
				mergeLocalMetadataIntoMatch(adultMatch, local)
				return s.applyProviderMatch(ctx, m, lib, adultMatch)
			} else if err != nil {
				s.log.Debug("adult metadata search failed", zap.String("media_id", m.ID), zap.String("code", code), zap.Error(err))
			}
		}
	}

	if match := s.matchFromMediaExternalIDs(ctx, m, lib); match != nil {
		s.applyFanartArtwork(ctx, match)
		mergeLocalMetadataIntoMatch(match, local)
		return s.applyProviderMatch(ctx, m, lib, match)
	}

	candidates := scrapeQueryCandidates(m, lib)
	var query string
	match := (*Match)(nil)
	for _, candidate := range candidates {
		query = candidate
		candidateMatch := s.lookup(ctx, lib, candidate, year)
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
		if local != nil {
			return s.applyLocalMetadataMatch(ctx, m, local)
		}
		_ = s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).
			Update("scrape_status", "no_match").Error
		s.log.Info("metadata scrape no match",
			zap.String("media_id", m.ID),
			zap.String("query", query),
			zap.String("library_type", lib.Type))
		return nil
	}
	s.applyFanartArtwork(ctx, match)
	mergeLocalMetadataIntoMatch(match, local)

	return s.applyProviderMatch(ctx, m, lib, match)
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
			return match
		}
	}
	if strings.TrimSpace(m.DoubanID) != "" && s.douban != nil && s.douban.Enabled() {
		if match, err := s.douban.GetMatchByID(ctx, strings.TrimSpace(m.DoubanID)); err == nil && match != nil {
			return match
		} else if err != nil {
			s.log.Debug("douban id lookup failed", zap.String("media_id", m.ID), zap.String("douban_id", m.DoubanID), zap.Error(err))
		}
	}
	if m.BangumiID > 0 && s.bangumi != nil && s.bangumi.Enabled() {
		if match, err := s.bangumi.GetSubject(ctx, m.BangumiID); err == nil && match != nil {
			return match
		} else if err != nil {
			s.log.Debug("bangumi id lookup failed", zap.String("media_id", m.ID), zap.Int("bangumi_id", m.BangumiID), zap.Error(err))
		}
	}
	if strings.TrimSpace(m.TheTVDBID) != "" && s.thetvdb != nil && s.thetvdb.Enabled() {
		if match, err := s.thetvdb.GetSeriesMatchByID(ctx, strings.TrimSpace(m.TheTVDBID)); err == nil && match != nil {
			return match
		} else if err != nil {
			s.log.Debug("thetvdb id lookup failed", zap.String("media_id", m.ID), zap.String("thetvdb_id", m.TheTVDBID), zap.Error(err))
		}
	}
	return nil
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
	if m.Year > 0 {
		return m.Year
	}
	if _, year := CleanQuery(filepath.Base(m.Path)); year > 0 {
		return year
	}
	return yearFromText(m.Path)
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

func (s *ScraperService) applyProviderMatch(ctx context.Context, m *model.Media, lib *model.Library, match *Match) error {
	updates := map[string]any{
		"title":         match.Title,
		"overview":      match.Overview,
		"poster_url":    match.PosterURL,
		"backdrop_url":  match.BackdropURL,
		"rating":        match.Rating,
		"year":          match.Year,
		"scrape_status": "matched",
	}
	if match.OriginalName != "" {
		updates["original_name"] = match.OriginalName
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

	// Fetch extended metadata after the selected match is already saved.
	// Manual cloud/batch applies must not fail just because an optional provider
	// details request is slow or unavailable.
	if match.TMDbID > 0 && s.tmdb != nil && s.tmdb.Enabled() {
		mediaType := s.determineMediaTypeForMedia(lib, m, match)
		detailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tmdbDetailsTimeout)
		details, err := s.tmdb.GetDetails(detailCtx, match.TMDbID, mediaType)
		cancel()
		if err != nil {
			s.log.Warn("failed to get details from tmdb",
				zap.Int("tmdb_id", match.TMDbID),
				zap.String("type", mediaType),
				zap.Error(err))
		} else if details != nil {
			detailUpdates := map[string]any{}
			if len(details.Languages) > 0 {
				detailUpdates["languages"] = strings.Join(details.Languages, ",")
			}
			if len(details.Countries) > 0 {
				detailUpdates["countries"] = strings.Join(details.Countries, ",")
			}
			if len(details.Genres) > 0 {
				detailUpdates["genres"] = strings.Join(details.Genres, ",")
			}
			if len(detailUpdates) > 0 {
				if err := s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).
					Updates(detailUpdates).Error; err != nil {
					s.log.Warn("failed to save tmdb extended metadata",
						zap.String("media_id", m.ID),
						zap.Int("tmdb_id", match.TMDbID),
						zap.Error(err))
				}
			}
			s.log.Debug("enrich: saved extended metadata",
				zap.String("media_id", m.ID),
				zap.Strings("languages", details.Languages),
				zap.Strings("countries", details.Countries),
				zap.Strings("genres", details.Genres))
		}
		if mediaType == "tv" && m != nil && m.EpisodeNum > 0 {
			episodeCtx, episodeCancel := context.WithTimeout(context.WithoutCancel(ctx), tmdbDetailsTimeout)
			episode, err := s.tmdb.GetTVEpisodeDetails(episodeCtx, match.TMDbID, m.SeasonNum, m.EpisodeNum)
			episodeCancel()
			if err != nil {
				s.log.Debug("failed to get tmdb episode details",
					zap.String("media_id", m.ID),
					zap.Int("tmdb_id", match.TMDbID),
					zap.Int("season", m.SeasonNum),
					zap.Int("episode", m.EpisodeNum),
					zap.Error(err))
			} else if episode != nil {
				episodeUpdates := map[string]any{}
				if strings.TrimSpace(episode.Name) != "" {
					episodeUpdates["original_name"] = strings.TrimSpace(episode.Name)
				}
				if strings.TrimSpace(episode.Overview) != "" {
					episodeUpdates["overview"] = strings.TrimSpace(episode.Overview)
				}
				if strings.TrimSpace(episode.StillURL) != "" {
					episodeUpdates["backdrop_url"] = strings.TrimSpace(episode.StillURL)
				}
				if episode.Rating > 0 {
					episodeUpdates["rating"] = episode.Rating
				}
				if episode.AirYear > 0 && match.Year <= 0 {
					episodeUpdates["year"] = episode.AirYear
				}
				if episode.Runtime > 0 && m.DurationSec <= 0 {
					episodeUpdates["duration_sec"] = episode.Runtime * 60
				}
				if len(episodeUpdates) > 0 {
					if err := s.repo.DB.Model(&model.Media{}).Where("id = ?", m.ID).
						Updates(episodeUpdates).Error; err != nil {
						s.log.Warn("failed to save tmdb episode metadata",
							zap.String("media_id", m.ID),
							zap.Int("tmdb_id", match.TMDbID),
							zap.Int("season", m.SeasonNum),
							zap.Int("episode", m.EpisodeNum),
							zap.Error(err))
					}
				}
			}
		}
	}
	cloudMedia := isCloudMediaPath(m.Path) || (lib != nil && isCloudMediaPath(lib.Path))
	if !cloudMedia {
		if refreshed, err := s.repo.Media.FindByID(ctx, m.ID); err == nil && refreshed != nil {
			if path, err := WriteMediaNFO(refreshed); err != nil {
				s.log.Warn("write nfo after scrape failed", zap.String("media_id", m.ID), zap.Error(err))
			} else {
				s.log.Debug("write nfo after scrape", zap.String("media_id", m.ID), zap.String("path", path))
			}
		}
	}
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
	s.hub.Publish("scrape", map[string]any{
		"media_id":  m.ID,
		"title":     next.Title,
		"tmdb_id":   next.TMDbID,
		"douban_id": next.DoubanID,
		"source":    "local_nfo",
	})
	return nil
}

func scrapeQueryCandidates(m *model.Media, lib *model.Library) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		cleaned, _ := CleanQuery(raw)
		if cleaned == "" {
			cleaned = strings.TrimSpace(raw)
		}
		for _, candidate := range titleCandidates(cleaned) {
			key := strings.ToLower(candidate)
			if _, ok := seen[key]; ok || candidate == "" {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, candidate)
		}
	}
	if lib != nil && mediaIsEpisodic(m, lib) {
		add(seriesFolderTitle(m.Path, lib.Path))
	}
	add(m.Title)
	add(m.Path)
	if len(out) == 0 {
		out = append(out, strings.TrimSuffix(filepath.Base(m.Path), filepath.Ext(m.Path)))
	}
	return out
}

func seriesFolderTitle(mediaPath, libraryRoot string) string {
	dir := filepath.Dir(mediaPath)
	if strictSeasonFolderMatched(filepath.Base(dir)) {
		dir = filepath.Dir(dir)
	}
	if libraryRoot != "" && samePath(dir, filepath.Clean(libraryRoot)) {
		return ""
	}
	base := filepath.Base(dir)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	if isGenericMediaCategoryFolder(base) {
		return ""
	}
	return base
}

func isGenericMediaCategoryFolder(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.Trim(key, `\/`)
	switch key {
	case "",
		"电影", "movies", "movie",
		"电视剧", "剧集", "tv", "shows", "series",
		"动漫", "动画", "anime", "bangumi",
		"国产剧", "国剧", "大陆剧", "国产电视剧",
		"欧美剧", "欧美电视剧",
		"日韩剧", "日剧", "韩剧",
		"华语电影", "国产电影", "大陆电影",
		"外语电影", "欧美电影", "日韩电影",
		"动画电影", "动漫电影",
		"国漫", "国产动漫", "日番", "日漫", "日本动漫", "日本动画",
		"综艺", "真人秀",
		"纪录片", "纪录",
		"儿童", "少儿",
		"成人", "番号", "9kg",
		"未分类", "uncategorized":
		return true
	default:
		return false
	}
}

func strictSeasonFolder(name string) int {
	if season, ok := seasonFromDir(name); ok {
		return season
	}
	return 0
}

func strictSeasonFolderMatched(name string) bool {
	_, ok := seasonFromDir(name)
	return ok
}

func titleCandidates(title string) []string {
	title = strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
	if title == "" {
		return nil
	}
	out := make([]string, 0, 2)
	if cjk := cjkTitleOnly(title); cjk != "" {
		out = append(out, cjk)
		if cjk != title {
			return out
		}
	}
	out = append(out, title)
	return out
}

func cjkTitleOnly(title string) string {
	parts := make([]string, 0, 4)
	for _, field := range strings.Fields(title) {
		if containsCJK(field) {
			parts = append(parts, field)
		}
	}
	return strings.Join(parts, " ")
}

func containsCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= '\u3400' && r <= '\u4dbf':
			return true
		case r >= '\u4e00' && r <= '\u9fff':
			return true
		case r >= '\uf900' && r <= '\ufaff':
			return true
		}
	}
	return false
}

func mediaIsEpisodic(m *model.Media, lib *model.Library) bool {
	if m != nil && (m.SeasonNum > 0 || m.EpisodeNum > 0) {
		return true
	}
	return librarySupportsSeasons(lib)
}

func librarySupportsSeasons(lib *model.Library) bool {
	if lib == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(lib.Type)) {
	case "tv", "anime", "variety", "show", "shows":
		return true
	default:
		return false
	}
}

// lookup runs the provider chain after local NFO has been considered:
// TMDb -> Douban -> Bangumi -> TheTVDB. Douban and Bangumi do not require API
// keys; providers that are unavailable or return an error are skipped.
func (s *ScraperService) lookup(ctx context.Context, lib *model.Library, query string, year int) *Match {
	kind := ""
	if lib != nil {
		kind = lib.Type
	}
	if s.tmdb != nil && s.tmdb.Enabled() {
		// anime / tv 先用 TMDb /search/tv（剧名通常是 TV 类目）。
		if kind == "anime" || kind == "tv" || kind == "variety" || kind == "show" || kind == "shows" {
			if m, err := s.tmdb.SearchTV(ctx, query, year); err == nil && m != nil {
				return m
			} else if err != nil {
				s.log.Debug("tmdb tv search failed", zap.String("query", query), zap.Error(err))
			}
		}
		if m, err := s.tmdb.SearchMovie(ctx, query, year); err == nil && m != nil {
			return m
		} else if err != nil {
			s.log.Debug("tmdb movie search failed", zap.String("query", query), zap.Error(err))
		}
	}
	if s.douban != nil && s.douban.Enabled() {
		if m, err := s.douban.SearchMatch(ctx, query); err == nil && m != nil {
			return m
		} else if err != nil {
			s.log.Debug("douban search failed", zap.String("query", query), zap.Error(err))
		}
	}
	if s.bangumi != nil && s.bangumi.Enabled() {
		if m, err := s.bangumi.Search(ctx, query); err == nil && m != nil {
			return m
		} else if err != nil {
			s.log.Debug("bangumi search failed", zap.String("query", query), zap.Error(err))
		}
	}
	if (kind == "anime" || kind == "tv" || kind == "variety" || kind == "show" || kind == "shows") && s.thetvdb != nil && s.thetvdb.Enabled() {
		if m, err := s.thetvdb.SearchSeries(ctx, query); err == nil && m != nil {
			return m
		} else if err != nil {
			s.log.Debug("thetvdb search failed", zap.String("query", query), zap.Error(err))
		}
	}
	return nil
}

// EnrichLibrary runs the provider chain for every pending media in a library.
// When retryNoMatch is true it also retries rows previously marked no_match,
// which is the expected behaviour for a manual "重新刮削" action. Scanner-driven
// automatic enrichment keeps the default false path to avoid repeated scraping.
//
// Pending status includes both the canonical "pending" string and the
// empty / NULL values, because MediaRepository.Upsert can wipe the GORM
// default when re-running a scan over an already-existing row.
func (s *ScraperService) EnrichLibrary(ctx context.Context, libraryID string, retryNoMatch ...bool) (int, error) {
	var rows []model.Media
	statusFilter := "scrape_status IS NULL OR scrape_status = '' OR scrape_status = ?"
	statusArgs := []any{"pending"}
	if len(retryNoMatch) > 0 && retryNoMatch[0] {
		statusFilter += " OR scrape_status = ?"
		statusArgs = append(statusArgs, "no_match")
	}
	q := s.repo.DB.Where(statusFilter, statusArgs...)
	if libraryID != "" {
		q = q.Where("library_id = ?", libraryID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return 0, err
	}
	matched := 0
	processed := 0
	for i := range rows {
		select {
		case <-ctx.Done():
			return matched, ctx.Err()
		default:
		}
		if err := s.EnrichOne(ctx, &rows[i]); err != nil {
			s.log.Warn("enrich failed", zap.String("media", rows[i].ID), zap.Error(err))
			s.notifyScrapeFailed(rows[i], err)
			continue
		}
		processed++
		if s.mediaIsMatched(ctx, rows[i].ID) {
			matched++
		}
		if i < len(rows)-1 {
			if delay := s.scrapeDelay(ctx); delay > 0 {
				select {
				case <-ctx.Done():
					return matched, ctx.Err()
				case <-time.After(delay):
				}
			}
		}
	}
	s.hub.Publish("scrape", map[string]any{
		"library_id": libraryID,
		"finished":   true,
		"matched":    matched,
		"processed":  processed,
	})
	return matched, nil
}

func (s *ScraperService) notifyScrapeFailed(m model.Media, err error) {
	if s == nil || s.notify == nil || err == nil {
		return
	}
	body := strings.TrimSpace(m.Title)
	if body == "" {
		body = m.Path
	}
	body = "媒体：" + body + "\n错误：" + err.Error()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s.notify.Broadcast(ctx, "MediaStationGo 刮削失败", body, EventScrapeFailed)
	}()
}

func (s *ScraperService) scrapeDelay(ctx context.Context) time.Duration {
	minMS := s.scrapeDelaySetting(ctx, "scrape.delay_min_ms", defaultScrapeDelayMinMS)
	maxMS := s.scrapeDelaySetting(ctx, "scrape.delay_max_ms", defaultScrapeDelayMaxMS)
	if minMS < 0 {
		minMS = 0
	}
	if maxMS < 0 {
		maxMS = 0
	}
	if minMS > maxScrapeDelayMS {
		minMS = maxScrapeDelayMS
	}
	if maxMS > maxScrapeDelayMS {
		maxMS = maxScrapeDelayMS
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	if maxMS == 0 {
		return 0
	}
	if maxMS == minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+secureRandomIntn(maxMS-minMS+1)) * time.Millisecond
}

func (s *ScraperService) scrapeDelaySetting(ctx context.Context, key string, fallback int) int {
	if s == nil || s.repo == nil || s.repo.Setting == nil {
		return fallback
	}
	value, err := s.repo.Setting.Get(ctx, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return fallback
	}
	return parseIntSettingDefault(strings.TrimSpace(value), fallback)
}

func (s *ScraperService) mediaIsMatched(ctx context.Context, mediaID string) bool {
	var status string
	err := s.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Select("scrape_status").
		Where("id = ?", mediaID).
		Scan(&status).Error
	return err == nil && status == "matched"
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

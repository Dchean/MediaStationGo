package service

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type SeriesCard struct {
	Key       string      `json:"key"`
	Rep       model.Media `json:"rep"`
	LinkMedia model.Media `json:"linkMedia"`
	Count     int         `json:"count"`
}

func (s *MediaService) ListLibrarySeriesCards(ctx context.Context, libraryID string, visibility MediaVisibility) ([]SeriesCard, int64, error) {
	rows, _, err := s.listAllMediaVisible(ctx, libraryID, visibility)
	if err != nil {
		return nil, 0, err
	}
	cards := groupMediaSeriesCards(rows)
	return cards, int64(len(cards)), nil
}

func (s *MediaService) ListLibrarySeriesEpisodes(ctx context.Context, libraryID, key string, visibility MediaVisibility) ([]model.Media, error) {
	rows, _, err := s.listAllMediaVisible(ctx, libraryID, visibility)
	if err != nil {
		return nil, err
	}
	out := make([]model.Media, 0)
	for _, row := range rows {
		if mediaSeriesKey(row) == key {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SeasonNum != out[j].SeasonNum {
			return out[i].SeasonNum < out[j].SeasonNum
		}
		if out[i].EpisodeNum != out[j].EpisodeNum {
			return out[i].EpisodeNum < out[j].EpisodeNum
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MediaService) listAllMediaVisible(ctx context.Context, libraryID string, visibility MediaVisibility) ([]model.Media, int64, error) {
	const pageSize = 2000
	var all []model.Media
	var total int64
	for page := 1; ; page++ {
		rows, n, err := s.ListMediaVisible(ctx, libraryID, page, pageSize, visibility)
		if err != nil {
			return nil, 0, err
		}
		if page == 1 {
			total = n
			all = make([]model.Media, 0, minInt64(n, pageSize))
		}
		all = append(all, rows...)
		if int64(len(all)) >= n || len(rows) < pageSize {
			break
		}
	}
	return all, total, nil
}

func groupMediaSeriesCards(items []model.Media) []SeriesCard {
	if len(items) == 0 {
		return nil
	}
	cards := make([]SeriesCard, 0)
	byKey := make(map[string]int, len(items))
	for _, item := range items {
		key := mediaSeriesKey(item)
		if key == "" {
			continue
		}
		if idx, ok := byKey[key]; ok {
			card := &cards[idx]
			card.Count++
			if betterSeriesLinkMedia(item, card.LinkMedia) {
				card.LinkMedia = item
			}
			currentArtwork := seriesArtworkScore(item)
			representativeArtwork := seriesArtworkScore(card.Rep)
			if currentArtwork > representativeArtwork {
				card.Rep = item
			} else if currentArtwork == representativeArtwork {
				cur := item.SeasonNum*10000 + item.EpisodeNum
				rep := card.Rep.SeasonNum*10000 + card.Rep.EpisodeNum
				if cur > 0 && (rep == 0 || cur < rep) {
					card.Rep = item
				}
			}
			continue
		}
		byKey[key] = len(cards)
		cards = append(cards, SeriesCard{Key: key, Rep: item, LinkMedia: item, Count: 1})
	}
	return cards
}

var episodicPathRE = regexp.MustCompile(`(?i)[\\/](?:电视剧|剧集|国产剧|欧美剧|日韩剧|日剧|韩剧|综艺|纪录片|动漫|番剧|国漫|日番|儿童|tv|series|shows?|season[\s._-]*\d|s\d{1,2}(?:[\s._-]|[\\/])|specials?|sp|ova|oad|extra|extras|特别篇|特別篇|番外|特典)[\\/]`)

func mediaSeriesKey(media model.Media) string {
	return compactSeriesKey(mediaSeriesRawKey(media))
}

func mediaSeriesRawKey(media model.Media) string {
	fromPath := seriesTitleFromMediaPath(media.Path)
	if media.SeasonNum > 0 || media.EpisodeNum > 0 || episodicPathRE.MatchString(media.Path+" "+media.DisplayLibraryPath+" "+media.LibraryPath) {
		if fromPath != "" {
			return seriesFingerprint("library-path", mediaTargetLibraryID(media), fromPath)
		}
		if media.TMDbID > 0 {
			return fmt.Sprintf("tmdb:%d", media.TMDbID)
		}
		if media.BangumiID > 0 {
			return fmt.Sprintf("bgm:%d", media.BangumiID)
		}
		if strings.TrimSpace(media.DoubanID) != "" {
			return "douban:" + strings.TrimSpace(media.DoubanID)
		}
		if strings.TrimSpace(media.TheTVDBID) != "" {
			return "thetvdb:" + strings.TrimSpace(media.TheTVDBID)
		}
		if strings.TrimSpace(media.SeriesID) != "" {
			return "series:" + strings.TrimSpace(media.SeriesID)
		}
		return seriesFingerprint("library-title", mediaTargetLibraryID(media), normalizeSeriesTitle(seriesDisplayTitle(media)))
	}
	if strings.TrimSpace(media.SeriesID) != "" {
		return "series:" + strings.TrimSpace(media.SeriesID)
	}
	if media.TMDbID > 0 {
		return fmt.Sprintf("tmdb:%d", media.TMDbID)
	}
	if media.BangumiID > 0 {
		return fmt.Sprintf("bgm:%d", media.BangumiID)
	}
	if fromPath != "" {
		return seriesFingerprint("library-path", media.LibraryID, fromPath)
	}
	return seriesFingerprint("library-title", media.LibraryID, normalizeSeriesTitle(media.Title))
}

func seriesFingerprint(parts ...string) string {
	return strings.Join(parts, "\x1f")
}

func compactSeriesKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var hash uint32 = 2166136261
	for _, b := range []byte(raw) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return fmt.Sprintf("series:%08x", hash)
}

var (
	seriesYearRE          = regexp.MustCompile(`\s*\((?:19|20)\d{2}\)\s*`)
	seriesIDRE            = regexp.MustCompile(`(?i)\s*\[(?:tmdb|tmdbid)[=-]\d+\]\s*`)
	seriesBraceRE         = regexp.MustCompile(`(?i)\s*\{(?:tmdb|tmdbid|douban|bangumi|bgm|thetvdb|tvdb)[\s:=#-]*[a-z0-9_-]+\}\s*`)
	seriesSpacerRE        = regexp.MustCompile(`[\s._-]+`)
	seriesSpecialSuffixRE = regexp.MustCompile(`(?i)(?:\s+(?:specials?|sp|ova|oad|extra|extras)|\s*(?:特别篇|特別篇|番外|特典|外传|外傳))$`)
)

func normalizeSeriesTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = seriesYearRE.ReplaceAllString(value, " ")
	value = seriesIDRE.ReplaceAllString(value, " ")
	value = seriesBraceRE.ReplaceAllString(value, " ")
	value = seriesSpacerRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func normalizeSeriesPathTitle(value string) string {
	title := normalizeSeriesTitle(value)
	stripped := strings.TrimSpace(seriesSpecialSuffixRE.ReplaceAllString(title, ""))
	if stripped != "" {
		return stripped
	}
	return title
}

func seriesTitleFromMediaPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) < 2 {
		return ""
	}
	dirIndex := len(parts) - 2
	for dirIndex >= 0 && strictSeasonFolderMatched(filepath.Base(parts[dirIndex])) {
		dirIndex--
	}
	if dirIndex < 0 {
		return ""
	}
	return normalizeSeriesPathTitle(parts[dirIndex])
}

func seriesDisplayTitle(media model.Media) string {
	if fromPath := seriesTitleFromMediaPath(media.Path); fromPath != "" {
		return fromPath
	}
	if media.Title != "" {
		return media.Title
	}
	if media.OriginalName != "" {
		return media.OriginalName
	}
	return "未命名节目"
}

func mediaTargetLibraryID(media model.Media) string {
	if strings.TrimSpace(media.DisplayLibraryID) != "" {
		return media.DisplayLibraryID
	}
	return media.LibraryID
}

func betterSeriesLinkMedia(candidate, current model.Media) bool {
	candidateScore := librarySpecificityScore(candidate)
	currentScore := librarySpecificityScore(current)
	if candidateScore != currentScore {
		return candidateScore > currentScore
	}
	return seriesArtworkScore(candidate) > seriesArtworkScore(current)
}

func librarySpecificityScore(media model.Media) int {
	rawPath := strings.TrimSpace(firstNonEmpty(media.DisplayLibraryPath, media.LibraryPath))
	if rawPath == "" {
		return 0
	}
	normalized := strings.TrimRight(strings.ReplaceAll(rawPath, "\\", "/"), "/")
	lower := strings.ToLower(normalized)
	if strings.HasPrefix(lower, "cloud://") {
		rest := normalized[len("cloud://"):]
		slash := strings.Index(rest, "/")
		if slash < 0 || slash == len(rest)-1 {
			return 0
		}
		return 100 + len(nonEmptySlashParts(rest[slash+1:]))
	}
	return 200 + len(nonEmptySlashParts(normalized))
}

func nonEmptySlashParts(value string) []string {
	parts := strings.Split(value, "/")
	out := parts[:0]
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

var (
	posterArtworkRE = regexp.MustCompile(`(poster|folder|cover|movie|show|pl)(?:[._-]|\.[a-z0-9]+$|$)`)
	badArtworkRE    = regexp.MustCompile(`(actor|actress|cast|avatar|sample|screenshot|screen|still|scene|fanart|backdrop|background|landscape|banner|logo|disc)`)
)

func seriesArtworkScore(media model.Media) int {
	poster := strings.ToLower(media.PosterURL)
	backdrop := strings.ToLower(media.BackdropURL)
	if poster == "" {
		if backdrop != "" {
			return 5
		}
		return 0
	}
	if posterArtworkRE.MatchString(poster) {
		return 40
	}
	if badArtworkRE.MatchString(poster) {
		return 10
	}
	if strings.Contains(poster, "thumb") {
		return 20
	}
	return 30
}

func minInt64(a int64, b int) int {
	if a <= 0 {
		return 0
	}
	if a > int64(b) {
		return b
	}
	return int(a)
}

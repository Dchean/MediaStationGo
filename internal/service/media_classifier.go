package service

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

var (
	classifierEpisodeRE = regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}\b|第\s*\d+\s*[集期]|(?:^|[\s._-])E\d{1,3}(?:[\s._-]|$)`)
	classifierSeasonRE  = regexp.MustCompile(`(?i)\bS\d{1,2}\b|第\s*\d+\s*季`)
)

type mediaClassifyInput struct {
	MediaType string
	Title     string
	Languages []string
	Countries []string
	Genres    []string
	Category  string
}

func classifyMediaCategory(input mediaClassifyInput, categories map[string]string) string {
	mediaType := normalizeMediaType(input.MediaType, input.Title, input.Category)
	genres := normalizeTokens(input.Genres...)
	countries := normalizeTokens(input.Countries...)
	languages := normalizeTokens(input.Languages...)
	text := strings.ToLower(input.Title + " " + input.Category + " " + strings.Join(input.Genres, " "))

	isChinese := hasAny(languages, "ZH", "ZH-CN", "ZH-TW", "CN") || hasAny(countries, "CN", "TW", "HK", "MO")
	isJapanese := hasAny(languages, "JA", "JP") || hasAny(countries, "JP") || strings.Contains(text, "日番")
	isKorean := hasAny(languages, "KO", "KR") || hasAny(countries, "KR", "KP")
	isEastAsian := isJapanese || isKorean || hasAny(countries, "TH", "IN", "SG")
	isWestern := hasAny(countries,
		"US", "GB", "UK", "FR", "DE", "CA", "AU", "NZ", "IE", "NL", "SE", "NO", "DK",
		"FI", "ES", "IT", "PT", "AT", "CH", "BE", "RU",
	)

	hasGenre := func(values ...string) bool {
		for _, value := range values {
			if hasAny(genres, strings.ToUpper(value)) || strings.Contains(text, strings.ToLower(value)) {
				return true
			}
		}
		return false
	}

	switch mediaType {
	case "movie":
		if hasGenre("16", "ANIMATION", "动画", "动漫") {
			return categoryName(categories, "animation_movie", "动画电影")
		}
		if isChinese {
			return categoryName(categories, "chinese_movie", "华语电影")
		}
		if isEastAsian {
			return categoryName(categories, "jk_movie", "日韩电影")
		}
		if isWestern {
			return categoryName(categories, "euus_movie", "欧美电影")
		}
		return categoryName(categories, "foreign_movie", "外语电影")
	case "anime":
		if isChinese {
			return categoryName(categories, "cn_anime", "国漫")
		}
		return categoryName(categories, "jp_anime", "日番")
	case "variety":
		return categoryName(categories, "variety", "综艺")
	case "tv":
		if hasGenre("10764", "10767", "REALITY", "TALK", "综艺", "真人秀", "脱口秀") {
			return categoryName(categories, "variety", "综艺")
		}
		if hasGenre("99", "DOCUMENTARY", "纪录", "纪录片") {
			return categoryName(categories, "documentary", "纪录片")
		}
		if hasGenre("10762", "KIDS", "儿童") {
			return categoryName(categories, "children", "儿童")
		}
		if hasGenre("16", "ANIMATION", "动画", "动漫") {
			if isChinese {
				return categoryName(categories, "cn_anime", "国漫")
			}
			return categoryName(categories, "jp_anime", "日番")
		}
		if isChinese {
			return categoryName(categories, "domestic_tv", "国产剧")
		}
		if isEastAsian {
			return categoryName(categories, "jk_tv", "日韩剧")
		}
		if isWestern {
			return categoryName(categories, "euus_tv", "欧美剧")
		}
		return categoryName(categories, "uncategorized_tv", "未分类")
	}
	return ""
}

func normalizeMediaType(mediaType, title, category string) string {
	raw := strings.ToLower(strings.TrimSpace(mediaType))
	switch raw {
	case "movie", "film":
		return "movie"
	case "tv", "series", "show", "drama":
		return "tv"
	case "anime", "animation":
		return "anime"
	case "variety":
		return "variety"
	}
	text := strings.ToLower(title + " " + category)
	switch {
	case strings.Contains(text, "movie") || strings.Contains(text, "电影"):
		return "movie"
	case strings.Contains(text, "anime") || strings.Contains(text, "bangumi") || strings.Contains(text, "动漫") || strings.Contains(text, "动画"):
		return "anime"
	case strings.Contains(text, "variety") || strings.Contains(text, "综艺") || strings.Contains(text, "真人秀"):
		return "variety"
	case classifierEpisodeRE.MatchString(text) || classifierSeasonRE.MatchString(text) || strings.Contains(text, "tv") || strings.Contains(text, "剧集") || strings.Contains(text, "电视剧"):
		return "tv"
	default:
		return "movie"
	}
}

func normalizeTokens(values ...string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '/' || r == '|' || r == ';'
		}) {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part != "" {
				out[part] = struct{}{}
			}
		}
	}
	return out
}

func hasAny(values map[string]struct{}, needles ...string) bool {
	for _, needle := range needles {
		if _, ok := values[strings.ToUpper(needle)]; ok {
			return true
		}
	}
	return false
}

func categoryName(categories map[string]string, key, fallback string) string {
	if categories != nil {
		if name := strings.TrimSpace(categories[key]); name != "" {
			return name
		}
	}
	return fallback
}

func (o *OrganizerService) categoryMap() map[string]string {
	if o == nil || o.cfg == nil || o.cfg.Organizer.Categories == nil {
		return nil
	}
	return o.cfg.Organizer.Categories
}

func (o *OrganizerService) classifyMedia(ctx context.Context, m *model.Media, mediaType string) string {
	if m == nil {
		return ""
	}
	if m.Languages == "" && m.Countries == "" && m.Genres == "" && o != nil && o.repo != nil && o.repo.Media != nil {
		if fresh, err := o.repo.Media.FindByID(ctx, m.ID); err == nil && fresh != nil {
			m = fresh
		}
	}
	return classifyMediaCategory(mediaClassifyInput{
		MediaType: mediaType,
		Title:     m.Title + " " + m.OriginalName,
		Languages: parseCommaList(m.Languages),
		Countries: parseCommaList(m.Countries),
		Genres:    parseCommaList(m.Genres),
	}, o.categoryMap())
}

func (s *SubscriptionService) classifySubscriptionItem(ctx context.Context, sub *model.Subscription, title, sourceCategory string) (string, string) {
	mediaType := normalizeMediaType(sub.MediaType, title+" "+sub.Name+" "+sub.Filter, sourceCategory)
	category := strings.TrimSpace(sub.MediaCategory)
	if category == "" {
		category = classifyMediaCategory(mediaClassifyInput{
			MediaType: mediaType,
			Title:     title + " " + sub.Name + " " + sub.Filter,
			Category:  sourceCategory,
		}, s.categoryMap())
	}
	return mediaType, category
}

func (s *SubscriptionService) categoryMap() map[string]string {
	if s == nil || s.cfg == nil || s.cfg.Organizer.Categories == nil {
		return nil
	}
	return s.cfg.Organizer.Categories
}

func (s *SubscriptionService) resolveSubscriptionSavePath(ctx context.Context, sub *model.Subscription, mediaType, category string) string {
	if sub == nil {
		return ""
	}
	base := strings.TrimSpace(sub.SavePath)
	if base == "" && s.repo != nil && s.repo.Setting != nil {
		base, _ = s.repo.Setting.Get(ctx, "qbittorrent.savepath")
	}
	if base == "" {
		return ""
	}
	if !s.isSmartClassifyEnabled(ctx) || category == "" {
		return base
	}
	return filepath.Join(base, sanitizeFilename(category))
}

func (s *SubscriptionService) isSmartClassifyEnabled(ctx context.Context) bool {
	if s != nil && s.repo != nil && s.repo.Setting != nil {
		val, err := s.repo.Setting.Get(ctx, "organizer.smart_classify")
		if err == nil && val != "" {
			return val == "true" || val == "1" || val == "on"
		}
	}
	return s != nil && s.cfg != nil && s.cfg.Organizer.SmartClassify
}

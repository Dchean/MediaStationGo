package handler

import (
	"context"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

var metadataNoiseRE = regexp.MustCompile(`(?i)(自动订阅|订阅|全集|合集|complete|batch|season\s*\d+|s\d{1,2}|s\d{1,2}e\d{1,3}|第\s*\d+\s*季|第\s*\d+\s*[集话話期]|2160p|1080p|720p|4k|uhd|bluray|blu-ray|web-?dl|hdtv|remux|x26[45]|h\.?26[45]|hevc|avc|hdr10?\+?|dovi|dv|atmos|aac|ddp?5\.1|truehd|flac)`)
var metadataYearRE = regexp.MustCompile(`(?:19|20)\d{2}`)

var displayMetadataCache sync.Map

type cachedDisplayMetadata struct {
	value     displayMetadata
	expiresAt time.Time
}

func enrichSubscriptionArtwork(ctx context.Context, svc *service.Container, sub *model.Subscription) {
	if svc == nil || sub == nil || (strings.TrimSpace(sub.PosterURL) != "" && strings.TrimSpace(sub.BackdropURL) != "") {
		return
	}
	meta := lookupDisplayMetadata(ctx, svc, sub.Name, sub.Filter, sub.MediaType)
	if meta.Title == "" && meta.PosterURL == "" && meta.BackdropURL == "" && meta.Overview == "" {
		return
	}
	if strings.TrimSpace(sub.Source) == "" {
		sub.Source = meta.Source
	}
	if strings.TrimSpace(sub.PosterURL) == "" {
		sub.PosterURL = meta.PosterURL
	}
	if strings.TrimSpace(sub.BackdropURL) == "" {
		sub.BackdropURL = meta.BackdropURL
	}
	if strings.TrimSpace(sub.Overview) == "" {
		sub.Overview = meta.Overview
	}
}

func enrichAndPersistSubscriptions(ctx context.Context, svc *service.Container, items []model.Subscription) {
	for i := range items {
		before := items[i]
		enrichSubscriptionArtwork(ctx, svc, &items[i])
		if before.Source == items[i].Source &&
			before.PosterURL == items[i].PosterURL &&
			before.BackdropURL == items[i].BackdropURL &&
			before.Overview == items[i].Overview {
			continue
		}
		updates := map[string]any{}
		if before.Source != items[i].Source {
			updates["source"] = items[i].Source
		}
		if before.PosterURL != items[i].PosterURL {
			updates["poster_url"] = items[i].PosterURL
		}
		if before.BackdropURL != items[i].BackdropURL {
			updates["backdrop_url"] = items[i].BackdropURL
		}
		if before.Overview != items[i].Overview {
			updates["overview"] = items[i].Overview
		}
		if len(updates) == 0 {
			continue
		}
		if err := svc.Repo.DB.WithContext(ctx).Model(&model.Subscription{}).Where("id = ?", items[i].ID).Updates(updates).Error; err != nil {
			svc.Log.Debug("subscription artwork backfill failed", zap.String("id", items[i].ID), zap.Error(err))
		}
	}
}

func enrichAndPersistDownloadRows(ctx context.Context, svc *service.Container, rows []model.DownloadTask) {
	for i := range rows {
		before := rows[i]
		title := strings.TrimSpace(rows[i].Title)
		if title == "" {
			title = downloadDisplayTitle(rows[i].URL)
			rows[i].Title = title
		}
		if strings.TrimSpace(rows[i].PosterURL) == "" || strings.TrimSpace(rows[i].BackdropURL) == "" {
			meta := enrichDownloadTaskMeta(ctx, svc, service.DownloadTaskMeta{
				Title:       rows[i].Title,
				PosterURL:   rows[i].PosterURL,
				BackdropURL: rows[i].BackdropURL,
				Overview:    rows[i].Overview,
			}, firstNonEmptyString(rows[i].Title, rows[i].URL), "")
			rows[i].Title = firstNonEmptyString(rows[i].Title, meta.Title)
			rows[i].PosterURL = meta.PosterURL
			rows[i].BackdropURL = meta.BackdropURL
			rows[i].Overview = meta.Overview
		}
		if before.Title == rows[i].Title &&
			before.PosterURL == rows[i].PosterURL &&
			before.BackdropURL == rows[i].BackdropURL &&
			before.Overview == rows[i].Overview {
			continue
		}
		updates := map[string]any{}
		if before.Title != rows[i].Title {
			updates["title"] = rows[i].Title
		}
		if before.PosterURL != rows[i].PosterURL {
			updates["poster_url"] = rows[i].PosterURL
		}
		if before.BackdropURL != rows[i].BackdropURL {
			updates["backdrop_url"] = rows[i].BackdropURL
		}
		if before.Overview != rows[i].Overview {
			updates["overview"] = rows[i].Overview
		}
		if len(updates) == 0 {
			continue
		}
		if err := svc.Repo.DB.WithContext(ctx).Model(&model.DownloadTask{}).Where("id = ?", rows[i].ID).Updates(updates).Error; err != nil {
			svc.Log.Debug("download artwork backfill failed", zap.String("id", rows[i].ID), zap.Error(err))
		}
	}
}

func enrichDownloadTorrentViews(ctx context.Context, svc *service.Container, views []service.DownloadTorrentView) {
	cache := map[string]displayMetadata{}
	for i := range views {
		if strings.TrimSpace(views[i].PosterURL) != "" && strings.TrimSpace(views[i].BackdropURL) != "" {
			continue
		}
		query := firstNonEmptyString(views[i].Title, views[i].Name)
		cacheKey, _ := metadataSearchQuery(query)
		meta, ok := cache[cacheKey]
		if !ok {
			meta = lookupDisplayMetadata(ctx, svc, query, "", "")
			cache[cacheKey] = meta
		}
		if strings.TrimSpace(views[i].PosterURL) == "" {
			views[i].PosterURL = meta.PosterURL
		}
		if strings.TrimSpace(views[i].BackdropURL) == "" {
			views[i].BackdropURL = meta.BackdropURL
		}
		if strings.TrimSpace(views[i].Overview) == "" {
			views[i].Overview = meta.Overview
		}
		if (views[i].Title == "" || views[i].Title == views[i].Name) && meta.Title != "" {
			views[i].Title = meta.Title
		}
	}
}

func enrichDownloadTaskMeta(ctx context.Context, svc *service.Container, meta service.DownloadTaskMeta, fallbackTitle, mediaType string) service.DownloadTaskMeta {
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = strings.TrimSpace(downloadDisplayTitle(fallbackTitle))
	}
	if strings.TrimSpace(meta.PosterURL) != "" && strings.TrimSpace(meta.BackdropURL) != "" {
		return meta
	}
	found := lookupDisplayMetadata(ctx, svc, meta.Title, fallbackTitle, mediaType)
	if strings.TrimSpace(meta.Title) == "" {
		meta.Title = found.Title
	}
	if strings.TrimSpace(meta.PosterURL) == "" {
		meta.PosterURL = found.PosterURL
	}
	if strings.TrimSpace(meta.BackdropURL) == "" {
		meta.BackdropURL = found.BackdropURL
	}
	if strings.TrimSpace(meta.Overview) == "" {
		meta.Overview = found.Overview
	}
	return meta
}

func downloadDisplayTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		if dn := strings.TrimSpace(u.Query().Get("dn")); dn != "" {
			if decoded, err := url.QueryUnescape(dn); err == nil && strings.TrimSpace(decoded) != "" {
				return strings.TrimSpace(decoded)
			}
			return dn
		}
		if u.Host != "" {
			base := path.Base(u.Path)
			if base != "." && base != "/" && base != "" {
				base = strings.TrimSuffix(base, path.Ext(base))
				return strings.TrimSpace(base)
			}
		}
	}
	return raw
}

type displayMetadata struct {
	Source      string
	Title       string
	PosterURL   string
	BackdropURL string
	Overview    string
}

func lookupDisplayMetadata(ctx context.Context, svc *service.Container, title, fallback, mediaType string) displayMetadata {
	if svc == nil {
		return displayMetadata{}
	}
	query, year := metadataSearchQuery(title, fallback)
	if query == "" {
		return displayMetadata{}
	}
	searchType := normalizeMetadataMediaType(mediaType)
	cacheKey := searchType + ":" + strconv.Itoa(year) + ":" + strings.ToLower(query)
	if cached, ok := displayMetadataCache.Load(cacheKey); ok {
		entry := cached.(cachedDisplayMetadata)
		if time.Now().Before(entry.expiresAt) {
			return entry.value
		}
		displayMetadataCache.Delete(cacheKey)
	}
	searchCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	results := service.SearchExternalMedia(searchCtx, query, year, searchType, svc.TMDb, svc.Douban, svc.Bangumi)
	if len(results) == 0 {
		displayMetadataCache.Store(cacheKey, cachedDisplayMetadata{expiresAt: time.Now().Add(30 * time.Minute)})
		return displayMetadata{}
	}
	best := results[0]
	for _, item := range results {
		if searchType != "" && item.MediaType != "" && item.MediaType != searchType && !(searchType == "tv" && item.MediaType == "anime") {
			continue
		}
		if strings.TrimSpace(item.PosterURL) != "" {
			best = item
			break
		}
	}
	meta := displayMetadata{
		Source:      best.Source,
		Title:       best.Title,
		PosterURL:   best.PosterURL,
		BackdropURL: best.BackdropURL,
		Overview:    best.Overview,
	}
	displayMetadataCache.Store(cacheKey, cachedDisplayMetadata{value: meta, expiresAt: time.Now().Add(12 * time.Hour)})
	return meta
}

func metadataSearchQuery(values ...string) (string, int) {
	for _, value := range values {
		query := strings.TrimSpace(value)
		if query == "" || strings.Contains(query, "://") {
			continue
		}
		year := 0
		if yearLoc := metadataYearRE.FindStringIndex(query); len(yearLoc) == 2 {
			yearText := query[yearLoc[0]:yearLoc[1]]
			year, _ = strconv.Atoi(yearText)
			query = query[:yearLoc[1]]
		}
		query = stripBracketNoise(query)
		query = metadataNoiseRE.ReplaceAllString(query, " ")
		query = strings.NewReplacer("_", " ", ".", " ", "-", " ", "|", " ", "/", " ").Replace(query)
		query = strings.Join(strings.Fields(query), " ")
		query = strings.TrimSpace(query)
		if query != "" {
			return query, year
		}
	}
	return "", 0
}

func stripBracketNoise(value string) string {
	replacer := strings.NewReplacer("[", " ", "]", " ", "【", " ", "】", " ", "(", " ", ")", " ")
	return replacer.Replace(value)
}

func normalizeMetadataMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "movie":
		return "movie"
	case "anime":
		return "anime"
	case "tv", "series", "show", "variety", "综艺":
		return "tv"
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

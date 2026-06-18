package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

type ManualScrapeRequest struct {
	Source       string   `json:"source"`
	MediaType    string   `json:"media_type"`
	Title        string   `json:"title"`
	OriginalName string   `json:"original_name"`
	Overview     string   `json:"overview"`
	PosterURL    string   `json:"poster_url"`
	BackdropURL  string   `json:"backdrop_url"`
	Year         int      `json:"year"`
	Rating       float32  `json:"rating"`
	TMDbID       int      `json:"tmdb_id"`
	BangumiID    int      `json:"bangumi_id"`
	DoubanID     string   `json:"douban_id"`
	TheTVDBID    string   `json:"thetvdb_id"`
	Languages    []string `json:"languages"`
	Countries    []string `json:"countries"`
	Genres       []string `json:"genres"`
	NSFW         bool     `json:"nsfw"`
}

func (s *ScraperService) ManualSearch(ctx context.Context, media *model.Media, query, provider, mediaType string) ([]ExternalMediaResult, error) {
	if s == nil || media == nil {
		return nil, errors.New("media required")
	}
	lib, _ := s.repo.Library.FindByID(ctx, media.LibraryID)
	query = strings.TrimSpace(query)
	if query == "" {
		query = firstText(media.Title, media.OriginalName)
	}
	if query == "" {
		query, _ = CleanQuery(media.Path)
	}
	if query == "" {
		return nil, errors.New("search query required")
	}
	if mediaType == "" && lib != nil {
		mediaType = lib.Type
	}
	mediaType = normalizeMediaType(mediaType, query, "")
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" || provider == "all" {
		provider = "all"
	}
	year := mediaYearHint(media)
	if year <= 0 {
		_, year = CleanQuery(query)
	}

	out := make([]ExternalMediaResult, 0, 6)
	add := func(source, typ string, match *Match) {
		if match == nil || strings.TrimSpace(match.Title) == "" {
			return
		}
		out = append(out, ExternalMediaResult{
			Source:           source,
			MediaType:        typ,
			Title:            match.Title,
			OriginalName:     match.OriginalName,
			Overview:         match.Overview,
			PosterURL:        match.PosterURL,
			BackdropURL:      match.BackdropURL,
			Year:             match.Year,
			Rating:           match.Rating,
			TMDbID:           match.TMDbID,
			BangumiID:        match.BangumiID,
			DoubanID:         match.DoubanID,
			TheTVDBID:        match.TheTVDBID,
			SubscribeKeyword: buildSubscribeKeyword(match.Title, match.Year),
			Languages:        match.Languages,
			Countries:        match.Countries,
			Genres:           match.Genres,
			NSFW:             match.NSFW,
		})
	}

	if provider == "all" || provider == "adult" {
		for _, match := range s.manualAdultMatches(ctx, media, query) {
			add("adult", "adult", match)
		}
	}
	if provider == "all" || provider == "tmdb" {
		for _, match := range s.manualTMDbMatches(ctx, query, year, mediaType) {
			typ := mediaType
			if typ == "" {
				typ = "movie"
			}
			if match.TMDbID > 0 && isTVLikeTMDbMatch(match, mediaType) {
				typ = "tv"
			}
			add("tmdb", typ, match)
		}
	}
	if provider == "all" || provider == "douban" {
		if match := s.manualDoubanMatch(ctx, query); match != nil {
			add("douban", normalizeMediaType(mediaType, query, ""), match)
		}
	}
	if provider == "all" || provider == "bangumi" {
		if match := s.manualBangumiMatch(ctx, query); match != nil {
			add("bangumi", "anime", match)
		}
	}
	if provider == "all" || provider == "thetvdb" {
		if match := s.manualTheTVDBMatch(ctx, query); match != nil {
			add("thetvdb", "tv", match)
		}
	}
	return dedupeExternalMedia(out), nil
}

func (s *ScraperService) ApplyManualMatch(ctx context.Context, mediaID string, req ManualScrapeRequest) (*model.Media, error) {
	media, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil || media == nil {
		return nil, errors.New("media not found")
	}
	lib, _ := s.repo.Library.FindByID(ctx, media.LibraryID)
	match, err := s.manualRequestMatch(ctx, req)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(match.Title) == "" {
		return nil, errors.New("manual match title required")
	}
	if err := s.applyProviderMatch(ctx, media, lib, match); err != nil {
		return nil, err
	}
	return s.repo.Media.FindByID(ctx, mediaID)
}

func (s *ScraperService) manualRequestMatch(ctx context.Context, req ManualScrapeRequest) (*Match, error) {
	source := strings.ToLower(strings.TrimSpace(req.Source))
	mediaType := normalizeMediaType(req.MediaType, req.Title, "")
	fallback := func() (*Match, error) {
		match := mergeManualRequestIntoMatch(&Match{}, req)
		if strings.TrimSpace(match.Title) == "" {
			return nil, errors.New("manual match title required")
		}
		return match, nil
	}
	switch {
	case req.TMDbID > 0 && (source == "" || source == "tmdb"):
		if match := s.manualTMDbMatchByID(ctx, req.TMDbID, mediaType); match != nil {
			return mergeManualRequestIntoMatch(match, req), nil
		}
	case req.BangumiID > 0 && (source == "" || source == "bangumi"):
		if s.bangumi != nil {
			match, err := s.bangumi.GetSubject(ctx, req.BangumiID)
			if err == nil && match != nil {
				return mergeManualRequestIntoMatch(match, req), nil
			}
		}
	case strings.TrimSpace(req.TheTVDBID) != "" && (source == "" || source == "thetvdb"):
		if s.thetvdb != nil {
			match, err := s.thetvdb.GetSeriesMatchByID(ctx, req.TheTVDBID)
			if err == nil && match != nil {
				return mergeManualRequestIntoMatch(match, req), nil
			}
		}
	case strings.TrimSpace(req.DoubanID) != "" && (source == "" || source == "douban"):
		if s.douban != nil {
			match, err := s.douban.GetMatchByID(ctx, req.DoubanID)
			if err == nil && match != nil {
				return mergeManualRequestIntoMatch(match, req), nil
			}
		}
	case source == "adult":
		if match := s.manualAdultMatch(ctx, firstText(req.OriginalName, req.Title)); match != nil {
			return mergeManualRequestIntoMatch(match, req), nil
		}
	}
	return fallback()
}

func (s *ScraperService) manualTMDbMatches(ctx context.Context, query string, year int, mediaType string) []*Match {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		return nil
	}
	if id, ok := parsePositiveInt(query); ok {
		if match := s.manualTMDbMatchByID(ctx, id, mediaType); match != nil {
			return []*Match{match}
		}
	}
	out := make([]*Match, 0, 2)
	if mediaType == "" || mediaType == "movie" {
		if matches, err := s.tmdb.SearchMovieCandidates(ctx, query, year); err == nil {
			out = append(out, matches...)
		}
	}
	if mediaType == "" || mediaType == "tv" || mediaType == "anime" || mediaType == "variety" {
		if matches, err := s.tmdb.SearchTVCandidates(ctx, query, year); err == nil {
			out = append(out, matches...)
		}
	}
	return out
}

func (s *ScraperService) manualAdultMatches(ctx context.Context, media *model.Media, query string) []*Match {
	candidates := []string{query}
	if media != nil {
		candidates = append(candidates, media.Path, media.OriginalName, media.Title)
	}
	out := make([]*Match, 0, 1)
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
		if match := s.manualAdultMatch(ctx, code); match != nil {
			out = append(out, match)
		}
	}
	return out
}

func (s *ScraperService) manualAdultMatch(ctx context.Context, code string) *Match {
	if s.adult == nil || !s.adult.Enabled() {
		return nil
	}
	match, err := s.adult.Search(ctx, code)
	if err != nil || match == nil {
		return nil
	}
	return match
}

func (s *ScraperService) manualTMDbMatchByID(ctx context.Context, id int, mediaType string) *Match {
	if s.tmdb == nil || !s.tmdb.Enabled() || id <= 0 {
		return nil
	}
	if mediaType == "tv" || mediaType == "anime" || mediaType == "variety" {
		if match, err := s.tmdb.GetTVMatch(ctx, id); err == nil && match != nil {
			return match
		}
	}
	if match, err := s.tmdb.GetMovieMatch(ctx, id); err == nil && match != nil {
		return match
	}
	if match, err := s.tmdb.GetTVMatch(ctx, id); err == nil && match != nil {
		return match
	}
	return nil
}

func (s *ScraperService) manualDoubanMatch(ctx context.Context, query string) *Match {
	if s.douban == nil || !s.douban.Enabled() {
		return nil
	}
	if _, ok := parsePositiveInt(query); ok {
		if match, err := s.douban.GetMatchByID(ctx, query); err == nil && match != nil {
			return match
		}
	}
	match, err := s.douban.SearchMatch(ctx, query)
	if err != nil {
		return nil
	}
	return match
}

func (s *ScraperService) manualBangumiMatch(ctx context.Context, query string) *Match {
	if s.bangumi == nil || !s.bangumi.Enabled() {
		return nil
	}
	if id, ok := parsePositiveInt(query); ok {
		if match, err := s.bangumi.GetSubject(ctx, id); err == nil && match != nil {
			return match
		}
	}
	match, err := s.bangumi.Search(ctx, query)
	if err != nil {
		return nil
	}
	return match
}

func (s *ScraperService) manualTheTVDBMatch(ctx context.Context, query string) *Match {
	if s.thetvdb == nil || !s.thetvdb.Enabled() {
		return nil
	}
	if _, ok := parsePositiveInt(normalizeTheTVDBSeriesID(query)); ok {
		if match, err := s.thetvdb.GetSeriesMatchByID(ctx, query); err == nil && match != nil {
			return match
		}
	}
	match, err := s.thetvdb.SearchSeries(ctx, query)
	if err != nil {
		return nil
	}
	return match
}

func mergeManualRequestIntoMatch(match *Match, req ManualScrapeRequest) *Match {
	if match == nil {
		match = &Match{}
	}
	if req.Title != "" {
		match.Title = req.Title
	}
	if req.OriginalName != "" {
		match.OriginalName = req.OriginalName
	}
	if req.Overview != "" {
		match.Overview = req.Overview
	}
	if req.PosterURL != "" {
		match.PosterURL = req.PosterURL
	}
	if req.BackdropURL != "" {
		match.BackdropURL = req.BackdropURL
	}
	if req.Year > 0 {
		match.Year = req.Year
	}
	if req.Rating > 0 {
		match.Rating = req.Rating
	}
	if req.TMDbID > 0 {
		match.TMDbID = req.TMDbID
	}
	if req.BangumiID > 0 {
		match.BangumiID = req.BangumiID
	}
	if req.DoubanID != "" {
		match.DoubanID = req.DoubanID
	}
	if req.TheTVDBID != "" {
		match.TheTVDBID = req.TheTVDBID
	}
	if len(req.Genres) > 0 {
		match.Genres = req.Genres
	}
	if len(req.Countries) > 0 {
		match.Countries = req.Countries
	}
	if len(req.Languages) > 0 {
		match.Languages = req.Languages
	}
	if req.NSFW {
		match.NSFW = true
	}
	return match
}

func isTVLikeTMDbMatch(match *Match, mediaType string) bool {
	return mediaType == "tv" || mediaType == "anime" || mediaType == "variety"
}

func parsePositiveInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, ":") {
		value = value[strings.LastIndex(value, ":")+1:]
	}
	id, err := strconv.Atoi(strings.TrimSpace(value))
	return id, err == nil && id > 0
}

func manualScrapeBatchName(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return fmt.Sprintf("%d 个媒体", len(ids))
}

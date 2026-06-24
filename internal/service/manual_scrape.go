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
	Source         string   `json:"source"`
	MediaType      string   `json:"media_type"`
	Title          string   `json:"title"`
	OriginalName   string   `json:"original_name"`
	Overview       string   `json:"overview"`
	PosterURL      string   `json:"poster_url"`
	BackdropURL    string   `json:"backdrop_url"`
	Year           int      `json:"year"`
	Rating         float32  `json:"rating"`
	TMDbID         int      `json:"tmdb_id"`
	BangumiID      int      `json:"bangumi_id"`
	DoubanID       string   `json:"douban_id"`
	TheTVDBID      string   `json:"thetvdb_id"`
	Languages      []string `json:"languages"`
	Countries      []string `json:"countries"`
	Genres         []string `json:"genres"`
	NSFW           bool     `json:"nsfw"`
	EpisodeArtwork *bool    `json:"episode_artwork,omitempty"`
	EpisodeImages  *bool    `json:"episode_images,omitempty"`
}

func (r ManualScrapeRequest) EpisodeArtworkOption() *bool {
	if r.EpisodeImages != nil {
		return r.EpisodeImages
	}
	return r.EpisodeArtwork
}

func (s *ScraperService) ManualSearch(ctx context.Context, media *model.Media, query, provider, mediaType string) ([]ExternalMediaResult, error) {
	if s == nil || media == nil {
		return nil, errors.New("media required")
	}
	lib, _ := s.repo.Library.FindByID(ctx, media.LibraryID)
	queries := manualSearchQueries(media, lib, query)
	if len(queries) == 0 {
		return nil, errors.New("search query required")
	}
	if mediaType == "" {
		if mediaIsEpisodic(media, lib) {
			mediaType = "tv"
		} else if lib != nil {
			mediaType = lib.Type
		}
	}
	mediaType = normalizeMediaType(mediaType, queries[0], "")
	providers := manualSearchProviderSet(provider)
	year := mediaYearHint(media)
	if year <= 0 {
		_, year = CleanQuery(queries[0])
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
			SubscribeAliases: buildSubscribeAliases(match.Title, match.OriginalName, match.Year),
			Languages:        match.Languages,
			Countries:        match.Countries,
			Genres:           match.Genres,
			NSFW:             match.NSFW,
		})
	}

	if providers.want("adult") {
		for _, candidateQuery := range queries {
			for _, match := range s.manualAdultMatches(ctx, media, candidateQuery) {
				add("adult", "adult", match)
			}
		}
	}
	if providers.want("tmdb") {
		for _, candidateQuery := range queries {
			for _, candidate := range s.manualTMDbCandidates(ctx, candidateQuery, year, mediaType) {
				add("tmdb", candidate.MediaType, candidate.Match)
			}
		}
	}
	if providers.want("douban") {
		for _, candidateQuery := range queries {
			if match := s.manualDoubanMatch(ctx, candidateQuery); match != nil {
				add("douban", normalizeMediaType(mediaType, candidateQuery, ""), match)
			}
		}
	}
	if providers.want("bangumi") {
		for _, candidateQuery := range queries {
			if match := s.manualBangumiMatch(ctx, candidateQuery); match != nil {
				add("bangumi", "anime", match)
			}
		}
	}
	if providers.want("thetvdb") {
		for _, candidateQuery := range queries {
			if match := s.manualTheTVDBMatch(ctx, candidateQuery); match != nil {
				add("thetvdb", "tv", match)
			}
		}
	}
	return dedupeExternalMedia(out), nil
}

type manualSearchProviders map[string]struct{}

func manualSearchProviderSet(provider string) manualSearchProviders {
	out := manualSearchProviders{}
	for _, field := range strings.FieldsFunc(provider, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' '
	}) {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == "" || field == "all" {
			return nil
		}
		out[field] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (p manualSearchProviders) want(provider string) bool {
	if len(p) == 0 {
		return true
	}
	_, ok := p[provider]
	return ok
}

func manualSearchQueries(media *model.Media, lib *model.Library, query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(value string) {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}

	add(query)
	if strings.TrimSpace(query) == "" && media != nil {
		add(firstText(media.Title, media.OriginalName))
	}
	if media != nil {
		for _, candidate := range scrapeQueryCandidates(media, lib) {
			add(candidate)
		}
		if len(out) == 0 {
			title, _ := CleanQuery(media.Path)
			add(title)
		}
	}
	return out
}

func (s *ScraperService) ApplyManualMatch(ctx context.Context, mediaID string, req ManualScrapeRequest) (*model.Media, error) {
	return s.ApplyManualMatchWithOptions(ctx, mediaID, req, ScrapeOptions{EpisodeArtwork: req.EpisodeArtworkOption()})
}

func (s *ScraperService) ApplyManualMatchWithOptions(ctx context.Context, mediaID string, req ManualScrapeRequest, options ScrapeOptions) (*model.Media, error) {
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
	if err := s.applyProviderMatchWithOptions(ctx, media, lib, match, options); err != nil {
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

type manualTMDbCandidate struct {
	MediaType string
	Match     *Match
}

func (s *ScraperService) manualTMDbCandidates(ctx context.Context, query string, year int, mediaType string) []manualTMDbCandidate {
	if s.tmdb == nil || !s.tmdb.Enabled() {
		return nil
	}
	if id, ok := parsePositiveInt(query); ok {
		out := make([]manualTMDbCandidate, 0, 2)
		for _, typ := range manualTMDbIDSearchTypes(mediaType) {
			if match := s.manualTMDbMatchByIDForType(ctx, id, typ); match != nil {
				out = append(out, manualTMDbCandidate{MediaType: typ, Match: match})
			}
		}
		return out
	}
	out := make([]manualTMDbCandidate, 0, 4)
	for _, typ := range manualTMDbSearchTypes(mediaType) {
		switch typ {
		case "movie":
			if matches, err := s.tmdb.SearchMovieCandidates(ctx, query, year); err == nil {
				for _, match := range matches {
					out = append(out, manualTMDbCandidate{MediaType: "movie", Match: match})
				}
			}
		case "tv":
			if matches, err := s.tmdb.SearchTVCandidates(ctx, query, year); err == nil {
				for _, match := range matches {
					out = append(out, manualTMDbCandidate{MediaType: "tv", Match: match})
				}
			}
		}
	}
	return out
}

func (s *ScraperService) manualTMDbMatches(ctx context.Context, query string, year int, mediaType string) []*Match {
	candidates := s.manualTMDbCandidates(ctx, query, year, mediaType)
	out := make([]*Match, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Match)
	}
	return out
}

func manualTMDbIDSearchTypes(mediaType string) []string {
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		return []string{"tv", "movie"}
	case "movie", "adult":
		return []string{"movie", "tv"}
	default:
		return []string{"movie", "tv"}
	}
}

func manualTMDbSearchTypes(mediaType string) []string {
	if strings.TrimSpace(mediaType) == "" {
		return []string{"movie", "tv"}
	}
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		return []string{"tv", "movie"}
	case "movie", "adult":
		return []string{"movie"}
	default:
		return []string{"movie", "tv"}
	}
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

func (s *ScraperService) manualTMDbMatchByIDForType(ctx context.Context, id int, mediaType string) *Match {
	if s.tmdb == nil || !s.tmdb.Enabled() || id <= 0 {
		return nil
	}
	switch normalizeMediaType(mediaType, "", "") {
	case "tv", "anime", "variety":
		if match, err := s.tmdb.GetTVMatch(ctx, id); err == nil && match != nil {
			return match
		}
	case "movie", "adult":
		if match, err := s.tmdb.GetMovieMatch(ctx, id); err == nil && match != nil {
			return match
		}
	}
	return nil
}

func (s *ScraperService) manualDoubanMatch(ctx context.Context, query string) *Match {
	if s.douban == nil || !s.douban.Enabled() {
		return nil
	}
	if id, ok := parsePositiveIDString(query); ok {
		if match, err := s.douban.GetMatchByID(ctx, id); err == nil && match != nil {
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
	if id, ok := parsePositiveIDString(normalizeTheTVDBSeriesID(query)); ok {
		if match, err := s.thetvdb.GetSeriesMatchByID(ctx, id); err == nil && match != nil {
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

func parsePositiveIDString(value string) (string, bool) {
	id, ok := parsePositiveInt(value)
	if !ok {
		return "", false
	}
	return strconv.Itoa(id), true
}

func manualScrapeBatchName(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return fmt.Sprintf("%d 个媒体", len(ids))
}

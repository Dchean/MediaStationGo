package service

import (
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalMetadata contains metadata read from Kodi/Jellyfin sidecar NFO files.
type LocalMetadata struct {
	Title        string
	OriginalName string
	AdultCode    string
	Year         int
	Overview     string
	Rating       float32
	PosterURL    string
	BackdropURL  string
	TMDbID       int
	BangumiID    int
	DoubanID     string
	TheTVDBID    string
	SeasonNum    int
	EpisodeNum   int
	Genres       string
	Countries    string
	Languages    string
	NSFW         bool
	HasNFO       bool
	HasArtwork   bool
	PathHint     bool
}

type nfoUniqueID struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type nfoFanart struct {
	Value  string   `xml:",chardata"`
	Thumbs []string `xml:"thumb"`
}

type nfoThumb struct {
	Aspect string `xml:"aspect,attr"`
	Value  string `xml:",chardata"`
}

type nfoArt struct {
	Poster     string `xml:"poster"`
	Thumb      string `xml:"thumb"`
	Fanart     string `xml:"fanart"`
	Backdrop   string `xml:"backdrop"`
	Background string `xml:"background"`
	Banner     string `xml:"banner"`
	Landscape  string `xml:"landscape"`
}

type nfoDocument struct {
	XMLName       xml.Name      `xml:""`
	Title         string        `xml:"title"`
	ShowTitle     string        `xml:"showtitle"`
	OriginalTitle string        `xml:"originaltitle"`
	SortTitle     string        `xml:"sorttitle"`
	Num           string        `xml:"num"`
	Year          nfoInt        `xml:"year"`
	Premiered     string        `xml:"premiered"`
	ReleaseDate   string        `xml:"releasedate"`
	Release       string        `xml:"release"`
	Aired         string        `xml:"aired"`
	Plot          string        `xml:"plot"`
	Outline       string        `xml:"outline"`
	OriginalPlot  string        `xml:"originalplot"`
	Rating        nfoFloat      `xml:"rating"`
	Poster        string        `xml:"poster"`
	Thumbs        []nfoThumb    `xml:"thumb"`
	Fanart        nfoFanart     `xml:"fanart"`
	Art           nfoArt        `xml:"art"`
	TMDbID        nfoInt        `xml:"tmdbid"`
	UniqueIDs     []nfoUniqueID `xml:"uniqueid"`
	Season        nfoInt        `xml:"season"`
	Episode       nfoInt        `xml:"episode"`
	Genres        []string      `xml:"genre"`
	Tags          []string      `xml:"tag"`
	Countries     []string      `xml:"country"`
	Languages     []string      `xml:"language"`
	Studio        string        `xml:"studio"`
	Maker         string        `xml:"maker"`
	Publisher     string        `xml:"publisher"`
	Label         string        `xml:"label"`
	Directors     []string      `xml:"director"`
	Actors        []nfoActor    `xml:"actor"`
}

type nfoInt int

func (n *nfoInt) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw string
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	raw = cleanXMLText(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "null") || strings.EqualFold(raw, "nan") {
		*n = 0
		return nil
	}
	if v, err := strconv.Atoi(raw); err == nil {
		*n = nfoInt(v)
		return nil
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		*n = nfoInt(int(f))
	}
	return nil
}

type nfoFloat float32

func (n *nfoFloat) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw string
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	raw = cleanXMLText(raw)
	if raw == "" || strings.EqualFold(raw, "none") || strings.EqualFold(raw, "null") || strings.EqualFold(raw, "nan") {
		*n = 0
		return nil
	}
	if v, err := strconv.ParseFloat(raw, 32); err == nil {
		*n = nfoFloat(v)
	}
	return nil
}

type nfoActor struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
}

// ReadLocalMetadata reads sidecar NFO files for a media path. For TV/anime it
// merges show-level tvshow.nfo with episode-level sidecar metadata.
func ReadLocalMetadata(mediaPath, libraryRoot string, seriesLike bool) (*LocalMetadata, error) {
	if seriesLike {
		return readSeriesMetadata(mediaPath, libraryRoot)
	}
	doc, path, err := findMovieNFO(mediaPath, libraryRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return metadataFromArtwork(mediaPath, ""), nil
		}
		return nil, err
	}
	meta := metadataFromDoc(doc, filepath.Dir(path), false)
	mergeArtworkMetadata(meta, mediaPath, filepath.Dir(path))
	return meta, nil
}

func findMovieNFO(mediaPath, libraryRoot string) (*nfoDocument, string, error) {
	mediaDir := filepath.Dir(mediaPath)
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	adultCode := AdultCodeFromMediaPath(mediaPath)
	names := []string{
		base + ".nfo",
		"movie.nfo",
		filepath.Base(mediaDir) + ".nfo",
	}
	if adultCode != "" {
		names = append([]string{adultCode + ".nfo", strings.ReplaceAll(adultCode, "-", "") + ".nfo"}, names...)
	}
	seen := map[string]struct{}{}
	for _, name := range names {
		if name == ".nfo" || name == "" {
			continue
		}
		path := filepath.Join(mediaDir, name)
		key := strings.ToLower(filepath.Clean(path))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if doc, _, err := readNFO(path); err == nil {
			return doc, path, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, "", err
		}
	}
	if libraryRoot == "" || !samePath(mediaDir, filepath.Clean(libraryRoot)) {
		matches, _ := filepath.Glob(filepath.Join(mediaDir, "*.nfo"))
		if adultCode != "" {
			codeKey := strings.ToLower(strings.ReplaceAll(adultCode, "-", ""))
			for _, match := range matches {
				baseKey := strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(filepath.Base(match), filepath.Ext(match)), "-", ""))
				if strings.Contains(baseKey, codeKey) || strings.Contains(codeKey, baseKey) {
					if doc, _, err := readNFO(match); err == nil {
						return doc, match, nil
					} else if err != nil && !errors.Is(err, os.ErrNotExist) {
						return nil, "", err
					}
				}
			}
		}
		if len(matches) == 1 {
			if doc, _, err := readNFO(matches[0]); err == nil {
				return doc, matches[0], nil
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, "", err
			}
		}
	}
	return nil, "", os.ErrNotExist
}

func readSeriesMetadata(mediaPath, libraryRoot string) (*LocalMetadata, error) {
	var meta *LocalMetadata
	showBaseDir := ""
	if showDoc, showPath, err := findShowNFO(mediaPath, libraryRoot); err == nil && showDoc != nil {
		showBaseDir = filepath.Dir(showPath)
		meta = metadataFromDoc(showDoc, showBaseDir, true)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if episodeDoc, episodePath, err := readNFO(nfoPath(mediaPath)); err == nil {
		episodeMeta := metadataFromDoc(episodeDoc, filepath.Dir(episodePath), true)
		if meta == nil {
			meta = &LocalMetadata{}
		}
		mergeEpisodeMetadata(meta, episodeMeta, episodeDoc)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if meta == nil {
		meta = metadataFromArtwork(mediaPath, showBaseDir)
	} else {
		mergeArtworkMetadata(meta, mediaPath, showBaseDir)
	}
	return meta, nil
}

func readNFO(path string) (*nfoDocument, string, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- path is a discovered NFO sidecar under the configured library root.
	if err != nil {
		return nil, "", err
	}
	var doc nfoDocument
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, "", err
	}
	return &doc, path, nil
}

func findShowNFO(mediaPath, libraryRoot string) (*nfoDocument, string, error) {
	dir := filepath.Dir(mediaPath)
	root := filepath.Clean(libraryRoot)
	for {
		names := []string{"tvshow.nfo", "series.nfo"}
		base := filepath.Base(dir)
		if _, ok := seasonFromDir(base); ok {
			parentBase := filepath.Base(filepath.Dir(dir))
			names = append(names, parentBase+".nfo")
		}
		names = append(names, base+".nfo")
		for _, name := range names {
			path := filepath.Join(dir, name)
			if doc, _, err := readNFO(path); err == nil {
				return doc, path, nil
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, "", err
			}
		}
		if samePath(dir, root) {
			return nil, "", os.ErrNotExist
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, "", os.ErrNotExist
		}
		dir = parent
	}
}

func metadataFromDoc(doc *nfoDocument, baseDir string, seriesLike bool) *LocalMetadata {
	if doc == nil {
		return nil
	}
	meta := &LocalMetadata{
		Title:        cleanXMLText(doc.Title),
		OriginalName: cleanXMLText(doc.OriginalTitle),
		AdultCode:    normalizeAdultCode(doc.Num),
		Year:         int(doc.Year),
		Overview:     firstText(doc.Plot, doc.Outline, doc.OriginalPlot),
		Rating:       float32(doc.Rating),
		PosterURL:    firstRemoteURL(baseDir, nfoPosterValues(doc)...),
		BackdropURL:  firstRemoteURL(baseDir, nfoBackdropValues(doc)...),
		TMDbID:       int(doc.TMDbID),
		BangumiID:    mustAtoi(externalIDFromUniqueIDs(doc.UniqueIDs, "bangumi", "bgm")),
		DoubanID:     externalIDFromUniqueIDs(doc.UniqueIDs, "douban"),
		TheTVDBID:    externalIDFromUniqueIDs(doc.UniqueIDs, "thetvdb", "tvdb"),
		SeasonNum:    int(doc.Season),
		EpisodeNum:   int(doc.Episode),
		Genres:       joinNFOValues(adultAwareGenres(doc)),
		Countries:    joinNFOValues(doc.Countries),
		Languages:    joinNFOValues(doc.Languages),
		HasNFO:       true,
	}
	if meta.AdultCode == "" {
		meta.AdultCode = normalizeAdultCode(firstText(doc.OriginalTitle, doc.SortTitle, doc.Title))
	}
	if meta.AdultCode != "" {
		meta.NSFW = true
		if meta.OriginalName == "" || strings.EqualFold(meta.OriginalName, meta.Title) {
			meta.OriginalName = meta.AdultCode
		}
	}
	if seriesLike && cleanXMLText(doc.ShowTitle) != "" {
		meta.Title = cleanXMLText(doc.ShowTitle)
		if cleanXMLText(doc.Title) != "" {
			meta.OriginalName = cleanXMLText(doc.Title)
		}
	}
	if meta.Year == 0 {
		meta.Year = yearFromDate(firstText(doc.Premiered, doc.ReleaseDate, doc.Release, doc.Aired))
	}
	if meta.TMDbID == 0 {
		meta.TMDbID = tmdbIDFromUniqueIDs(doc.UniqueIDs)
	}
	return meta
}

func adultAwareGenres(doc *nfoDocument) []string {
	if doc == nil {
		return nil
	}
	values := make([]string, 0, len(doc.Genres)+len(doc.Tags)+len(doc.Actors)+4)
	values = append(values, doc.Genres...)
	values = append(values, doc.Tags...)
	for _, value := range []string{doc.Studio, doc.Maker, doc.Publisher, doc.Label} {
		if cleanXMLText(value) != "" {
			values = append(values, cleanXMLText(value))
		}
	}
	for _, value := range doc.Directors {
		if cleanXMLText(value) != "" {
			values = append(values, cleanXMLText(value))
		}
	}
	for _, actor := range doc.Actors {
		if cleanXMLText(actor.Name) != "" {
			values = append(values, cleanXMLText(actor.Name))
		} else if cleanXMLText(actor.Role) != "" {
			values = append(values, cleanXMLText(actor.Role))
		}
	}
	return values
}

func metadataFromArtwork(mediaPath, showBaseDir string) *LocalMetadata {
	meta := &LocalMetadata{}
	mergeArtworkMetadata(meta, mediaPath, showBaseDir)
	if meta.PosterURL == "" && meta.BackdropURL == "" {
		return nil
	}
	return meta
}

func mergeArtworkMetadata(meta *LocalMetadata, mediaPath, showBaseDir string) {
	if meta == nil {
		return
	}
	mediaDir := filepath.Dir(mediaPath)
	if localPoster := firstLocalPoster(mediaPath, showBaseDir); localPoster != "" {
		meta.PosterURL = localPoster
	} else if meta.PosterURL == "" {
		meta.PosterURL = firstAdultLooseImage(mediaDir, "poster")
	}
	dirs := []string{mediaDir, showBaseDir}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if img := firstExistingImage(dir, localBackdropCandidates(mediaPath)...); img != "" {
			meta.BackdropURL = img
			break
		}
		if meta.BackdropURL == "" {
			meta.BackdropURL = firstAdultLooseImage(dir, "backdrop")
		}
	}
	if !isLocalPath(meta.PosterURL) {
		if dmmPoster := adultDMMPosterFromSampleURL(meta.BackdropURL); dmmPoster != "" {
			meta.PosterURL = dmmPoster
		}
	}
	if meta.PosterURL != "" || meta.BackdropURL != "" {
		meta.HasArtwork = true
	}
}

func mergeEpisodeMetadata(dst, episode *LocalMetadata, doc *nfoDocument) {
	showTitle := cleanXMLText(doc.ShowTitle)
	episodeTitle := cleanXMLText(doc.Title)
	if showTitle != "" {
		dst.Title = showTitle
		if episodeTitle != "" {
			dst.OriginalName = episodeTitle
		}
	} else if dst.Title != "" && episodeTitle != "" && episodeTitle != dst.Title {
		dst.OriginalName = episodeTitle
	} else if dst.Title == "" && episodeTitle != "" {
		dst.Title = episodeTitle
	}
	if dst.OriginalName == "" && episode.OriginalName != "" {
		dst.OriginalName = episode.OriginalName
	}
	if episode.Year > 0 {
		dst.Year = episode.Year
	}
	if episode.Overview != "" {
		dst.Overview = episode.Overview
	}
	if episode.Rating > 0 {
		dst.Rating = episode.Rating
	}
	if episode.PosterURL != "" {
		dst.PosterURL = episode.PosterURL
	}
	if episode.BackdropURL != "" {
		dst.BackdropURL = episode.BackdropURL
	}
	if episode.TMDbID > 0 {
		dst.TMDbID = episode.TMDbID
	}
	if episode.BangumiID > 0 {
		dst.BangumiID = episode.BangumiID
	}
	if episode.DoubanID != "" {
		dst.DoubanID = episode.DoubanID
	}
	if episode.TheTVDBID != "" {
		dst.TheTVDBID = episode.TheTVDBID
	}
	if episode.SeasonNum > 0 || episode.EpisodeNum > 0 {
		dst.SeasonNum = episode.SeasonNum
	}
	if episode.EpisodeNum > 0 {
		dst.EpisodeNum = episode.EpisodeNum
	}
	if episode.Genres != "" {
		dst.Genres = episode.Genres
	}
	if episode.Countries != "" {
		dst.Countries = episode.Countries
	}
	if episode.Languages != "" {
		dst.Languages = episode.Languages
	}
}

func tmdbIDFromUniqueIDs(ids []nfoUniqueID) int {
	value := externalIDFromUniqueIDs(ids, "tmdb")
	if value == "" {
		return 0
	}
	v, _ := strconv.Atoi(value)
	return v
}

func externalIDFromUniqueIDs(ids []nfoUniqueID, types ...string) string {
	for _, id := range ids {
		idType := strings.TrimSpace(id.Type)
		for _, typ := range types {
			if strings.EqualFold(idType, typ) {
				return strings.TrimSpace(id.Value)
			}
		}
	}
	return ""
}

func firstRemoteURL(baseDir string, values ...string) string {
	for _, value := range values {
		value = cleanXMLText(value)
		if value == "" {
			continue
		}
		if isHTTPURL(value) {
			return value
		}
		if filepath.IsAbs(value) && fileExists(value) {
			return filepath.Clean(value)
		}
		if baseDir != "" {
			local := filepath.Join(baseDir, filepath.FromSlash(value))
			if fileExists(local) {
				return filepath.Clean(local)
			}
		}
	}
	return ""
}

func localPosterCandidates(mediaPath string) []string {
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	names := []string{
		base + "-poster",
		base + ".poster",
		"poster",
		"folder",
		"cover",
		"movie",
		"show",
		base + "-cover",
		base + ".cover",
		base,
		base + "-thumb",
		base + ".thumb",
		"thumb",
	}
	return append(adultArtworkNameCandidates(mediaPath, "poster"), names...)
}

func localBackdropCandidates(mediaPath string) []string {
	base := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))
	names := []string{
		base + "-fanart",
		base + ".fanart",
		base + "-backdrop",
		base + ".backdrop",
		base + "-background",
		"fanart",
		"backdrop",
		"background",
		"landscape",
		"banner",
		"clearart",
	}
	return append(adultArtworkNameCandidates(mediaPath, "backdrop"), names...)
}

func adultArtworkNameCandidates(mediaPath, kind string) []string {
	code := AdultCodeFromMediaPath(mediaPath)
	if code == "" {
		return nil
	}
	compact := strings.ReplaceAll(code, "-", "")
	bases := []string{code, compact}
	bases = append(bases, adultDMMNameCandidates(code)...)
	out := make([]string, 0, len(bases)*6)
	for _, base := range bases {
		if base == "" {
			continue
		}
		if kind == "poster" {
			out = append(out, base, base+"-poster", base+".poster", base+"-cover", base+".cover", base+"-thumb", base+".thumb", base+"pl", base+"-pl")
		} else {
			out = append(out, base+"-fanart", base+".fanart", base+"-backdrop", base+".backdrop", base+"-background", base+"-landscape", base+"jp", base+"jp-1")
		}
	}
	return out
}

func adultDMMNameCandidates(code string) []string {
	parts := adultStandardPattern.FindStringSubmatch(code)
	if len(parts) < 3 {
		return nil
	}
	prefix := strings.ToLower(parts[1])
	num := strings.TrimLeft(parts[2], "0")
	if num == "" {
		num = "0"
	}
	padded := num
	for len(padded) < 5 {
		padded = "0" + padded
	}
	return []string{prefix + padded}
}

func firstExistingImage(dir string, names ...string) string {
	if dir == "" {
		return ""
	}
	for _, name := range names {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			path := filepath.Join(dir, name+ext)
			if fileExists(path) {
				return filepath.Clean(path)
			}
		}
	}
	return ""
}

func nfoPosterValues(doc *nfoDocument) []string {
	if doc == nil {
		return nil
	}
	values := []string{doc.Poster, doc.Art.Poster}
	for _, thumb := range doc.Thumbs {
		aspect := strings.ToLower(strings.TrimSpace(thumb.Aspect))
		if aspect == "" || aspect == "poster" || aspect == "cover" || aspect == "default" {
			values = append(values, thumb.Value)
		}
	}
	values = append(values, doc.Art.Thumb)
	return values
}

func nfoBackdropValues(doc *nfoDocument) []string {
	if doc == nil {
		return nil
	}
	values := []string{doc.Fanart.Value, doc.Art.Fanart, doc.Art.Backdrop, doc.Art.Background, doc.Art.Landscape, doc.Art.Banner}
	for _, thumb := range doc.Thumbs {
		aspect := strings.ToLower(strings.TrimSpace(thumb.Aspect))
		if aspect == "fanart" || aspect == "backdrop" || aspect == "background" || aspect == "landscape" {
			values = append(values, thumb.Value)
		}
	}
	values = append(values, doc.Fanart.Thumbs...)
	return values
}

func firstLocalPoster(mediaPath, showBaseDir string) string {
	mediaDir := filepath.Dir(mediaPath)
	dirs := []string{}
	if showBaseDir != "" && !samePath(showBaseDir, mediaDir) {
		dirs = append(dirs, showBaseDir)
	}
	dirs = append(dirs, mediaDir)
	for _, dir := range dirs {
		if localPoster := firstExistingPosterImage(dir, localPosterCandidates(mediaPath)...); localPoster != "" {
			return localPoster
		}
	}
	return ""
}

func firstExistingPosterImage(dir string, names ...string) string {
	if dir == "" {
		return ""
	}
	for _, name := range names {
		if isRejectedPosterName(name) {
			continue
		}
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			path := filepath.Join(dir, name+ext)
			if fileExists(path) && likelyPosterImage(path) {
				return filepath.Clean(path)
			}
		}
	}
	return ""
}

func firstAdultLooseImage(dir, kind string) string {
	if dir == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	preferred := []string{}
	fallback := []string{}
	for _, path := range matches {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), ext))
		if kind == "poster" {
			if isRejectedPosterName(name) {
				continue
			}
			if strings.Contains(name, "poster") || strings.Contains(name, "cover") || strings.Contains(name, "folder") || strings.Contains(name, "movie") || strings.HasSuffix(name, "pl") {
				preferred = append(preferred, path)
			}
		} else if strings.Contains(name, "fanart") || strings.Contains(name, "backdrop") || strings.Contains(name, "background") || strings.Contains(name, "landscape") || strings.Contains(name, "jp") {
			preferred = append(preferred, path)
		}
		if kind != "poster" || likelyPosterImage(path) {
			fallback = append(fallback, path)
		}
	}
	if len(preferred) > 0 {
		return filepath.Clean(preferred[0])
	}
	if kind == "poster" && len(fallback) == 1 {
		return filepath.Clean(fallback[0])
	}
	return ""
}

func isRejectedPosterName(name string) bool {
	name = strings.ToLower(name)
	rejected := []string{
		"actor", "actors", "actress", "cast", "avatar", "portrait", "person",
		"sample", "screenshot", "screen", "still", "scene", "extrafanart", "extrathumb",
		"fanart", "backdrop", "background", "landscape", "banner", "clearlogo", "clearart", "logo", "disc",
	}
	for _, token := range rejected {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func likelyPosterImage(path string) bool {
	file, err := os.Open(path) // #nosec G304 -- path is a discovered artwork sidecar under the configured library root.
	if err != nil {
		return false
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return true
	}
	return cfg.Height >= cfg.Width
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func isLocalPath(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw != "" && !isHTTPURL(raw)
}

func firstText(values ...string) string {
	for _, value := range values {
		if text := cleanXMLText(value); text != "" {
			return text
		}
	}
	return ""
}

func cleanXMLText(value string) string {
	return strings.TrimSpace(value)
}

func joinNFOValues(values []string) string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = cleanXMLText(part)
			if part == "" {
				continue
			}
			key := strings.ToLower(part)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, part)
		}
	}
	return strings.Join(out, ",")
}

func yearFromDate(value string) int {
	if len(value) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(value[:4])
	return year
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

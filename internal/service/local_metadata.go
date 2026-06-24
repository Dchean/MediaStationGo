package service

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalMetadata contains metadata read from Kodi/Jellyfin sidecar NFO files.
type LocalMetadata struct {
	Title        string
	OriginalName string
	EpisodeTitle string
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
	if nfoIsEpisodeDetails(doc) {
		meta.EpisodeTitle = cleanXMLText(doc.Title)
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

// mergeEpisodeMetadata 把单集 sidecar NFO(<episodedetails>)合并进整剧元数据
// dst(通常来自 tvshow.nfo)。
//
// 关键约束:「整剧级」字段(Title/OriginalName/TMDbID/BangumiID/DoubanID/TheTVDBID)
// 是合集分组键的依据,必须保证「同一部剧的各集一致」。而单集 NFO 里的
// <uniqueid type="tmdb"> 是【单集 episode id】(如 4375419)、<title> 是【单集名】
// (如「九龙拉棺」),都是单集级数据 —— 一旦写进整剧字段,同剧每集的 id/原名互不
// 相同,会被前端 getSeriesKey / Emby seriesGroupsFromMedia 拆成多张卡(每集一卡)。
// 因此:
//   - 单集外部 id(tmdb/bangumi/douban/thetvdb)一律【不写入】整剧外部 id;
//     整剧 id 只认 tvshow.nfo(已在 dst);无则留空,交由路径剧名分组兜底。
//   - 单集名【不写入】OriginalName(整剧原名);整剧原名只来自 tvshow.nfo。
//   - 仅 overview/rating/剧照/季集号等【单集级】字段按集回填(不影响分组)。
func mergeEpisodeMetadata(dst, episode *LocalMetadata, doc *nfoDocument) {
	showTitle := cleanXMLText(doc.ShowTitle)
	// 整剧标题: 优先 <showtitle>(MoviePilot 在单集 NFO 里也会写整剧名);
	// 其次保留 dst 已有(来自 tvshow.nfo)。不要把单集 <title> 当整剧标题,
	// 否则会把"第 11 集/第几期"整理成整剧目录并污染后续 TMDb 查询。
	if showTitle != "" {
		dst.Title = showTitle
	}
	// 注意: 不要把单集名 / 单集 originaltitle 写进 OriginalName(整剧原名,分组键)。
	if episodeTitle := firstText(episode.EpisodeTitle, doc.Title); episodeTitle != "" && !strings.EqualFold(episodeTitle, showTitle) {
		dst.EpisodeTitle = episodeTitle
	}

	// 单集级展示字段: 每个媒体行本就对应一集,这些可安全按集回填。
	if dst.Year == 0 && episode.Year > 0 {
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
	// 整剧外部 id: 单集 NFO 的 id 都是单集级,绝不写入整剧字段(见上方说明)。
	if episode.SeasonNum > 0 {
		dst.SeasonNum = episode.SeasonNum
	}
	if episode.EpisodeNum > 0 {
		dst.EpisodeNum = episode.EpisodeNum
	}
	// 题材/地区/语言为整剧级,单集 NFO 偶尔携带时仅在整剧未提供时回填。
	if dst.Genres == "" && episode.Genres != "" {
		dst.Genres = episode.Genres
	}
	if dst.Countries == "" && episode.Countries != "" {
		dst.Countries = episode.Countries
	}
	if dst.Languages == "" && episode.Languages != "" {
		dst.Languages = episode.Languages
	}
}

func nfoIsEpisodeDetails(doc *nfoDocument) bool {
	if doc == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(doc.XMLName.Local), "episodedetails")
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

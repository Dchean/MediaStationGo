// Package service — organize an arbitrary source directory (e.g. the download
// directory) into the destination library with dedup + 洗版 (resolution
// replacement).
//
// Unlike OrganizeLibraryWithOptions, which only touches model.Media rows that
// already belong to a registered library, OrganizeDirectory walks the source
// directory on disk directly. This lets operators organize the whole download
// directory (/downloads or a NAS direct-read path configured by the operator)
// even though it is not a registered library.
//
// Two protections requested by operators:
//
//   - 去重：目的地已存在同一媒体时不再从来源整理过去（避免重复 / 多倍占用存储）。
//   - 洗版：若来源分辨率高于目的地已存在的版本，则用高分辨率替换低分辨率。
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const (
	organizeSkipAlreadyOrganized = "already organized"
	organizeSkipDuplicateLibrary = "duplicate in library"
	organizeSkipTargetExists     = "target file exists"
	organizeSkipSampleClip       = "sample/trailer clip"
)

// OrganizeSourceCandidate is a selectable organize source directory surfaced to
// the UI so operators can organize an arbitrary directory (such as the download
// directory) and not only registered libraries.
type OrganizeSourceCandidate struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Kind  string `json:"kind"` // "download" | "media"
}

// OrganizeSourceCandidates returns the configured directories that are valid
// organize sources (download dir + media dir). It uses the container-visible
// paths; in NAS direct-read mode those equal the host paths the operator sees.
func (o *OrganizerService) OrganizeSourceCandidates(ctx context.Context) []OrganizeSourceCandidate {
	out := []OrganizeSourceCandidate{}
	seen := map[string]struct{}{}
	add := func(label, path, kind string) {
		path = strings.TrimSpace(path)
		if path == "" || path == "." || strings.HasPrefix(path, ".") {
			return
		}
		clean := filepath.Clean(path)
		if !isAccessibleDir(clean) {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		out = append(out, OrganizeSourceCandidate{Label: label, Path: clean, Kind: kind})
	}
	add("默认整理源", o.settingValue(ctx, "organize.source_dir"), "source")
	add("下载器保存目录", o.settingValue(ctx, "qbittorrent.savepath"), "download")
	add("下载目录", envOrDefault("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "/downloads"), "download")
	add("媒体目录", envOrDefault("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media"), "media")
	return out
}

func (o *OrganizerService) settingValue(ctx context.Context, key string) string {
	if o.repo == nil || o.repo.Setting == nil {
		return ""
	}
	if v, err := o.repo.Setting.Get(ctx, key); err == nil {
		return strings.TrimSpace(v)
	}
	return ""
}

// defaultSourceRoot resolves the source root for a directory organize:
// explicit override → organize.source_dir setting → qB default save path →
// download container dir.
func (o *OrganizerService) defaultSourceRoot(ctx context.Context, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if v := o.settingValue(ctx, "organize.source_dir"); v != "" {
		return v
	}
	if v := o.settingValue(ctx, "qbittorrent.savepath"); v != "" {
		return v
	}
	return envOrDefault("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "/downloads")
}

// defaultDestRoot resolves the destination root for a directory organize:
// explicit override → organize.target_dir setting → media container dir.
func (o *OrganizerService) defaultDestRoot(ctx context.Context, override string) string {
	if r := strings.TrimSpace(override); r != "" {
		return r
	}
	if o.repo != nil && o.repo.Setting != nil {
		if v, err := o.repo.Setting.Get(ctx, "organize.target_dir"); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return envOrDefault("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media")
}

// OrganizeDirectory organizes every video file found under opts.SourcePath into
// the destination root, applying dedup + 洗版 (resolution replacement).
func (o *OrganizerService) OrganizeDirectory(ctx context.Context, opts OrganizeOptions) (*OrganizeResult, error) {
	requestedSource := strings.TrimSpace(o.defaultSourceRoot(ctx, opts.SourcePath))
	if requestedSource == "" {
		return nil, errors.New("source path required")
	}
	source, info, statErr := resolveAccessibleMappedPath(requestedSource)
	if statErr != nil {
		return nil, fmt.Errorf("source directory not accessible: %s", filepath.Clean(requestedSource))
	}
	dest := resolveMappedDestinationPath(o.defaultDestRoot(ctx, opts.DestPath))
	if dest == "" || dest == "." {
		return nil, errors.New("destination path required")
	}
	mode := o.resolveTransferMode(ctx, opts.TransferMode)
	res := &OrganizeResult{SourcePath: source, DestPath: dest, DryRun: opts.DryRun}
	if !info.IsDir() {
		ext := strings.ToLower(filepath.Ext(source))
		if _, ok := videoExtensions[ext]; !ok {
			return nil, fmt.Errorf("source is not a supported video file: %s", source)
		}
		if skipped, reason := shouldSkipOrganizeSourceVideo(source, filepath.Dir(source)); skipped {
			res.Skipped++
			res.Items = append(res.Items, OrganizePreviewItem{Source: source, Action: "skip", Reason: reason})
			o.log.Info("organize file finished",
				zap.String("source", source),
				zap.String("dest", dest),
				zap.String("mode", string(mode)),
				zap.Int("organized", res.Organized),
				zap.Int("replaced", res.Replaced),
				zap.Int("skipped", res.Skipped),
				zap.Any("skip_reasons", OrganizeSkipReasonCounts(res)),
			)
			return res, nil
		}
		if err := o.organizeSourceFile(ctx, source, filepath.Dir(source), dest, mode, opts.MediaType, opts.MediaCategory, opts.DryRun, opts.AllowReplaceExisting, res); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", filepath.Base(source), err.Error()))
			res.Items = append(res.Items, OrganizePreviewItem{Source: source, Action: "error", Reason: err.Error()})
		}
		o.log.Info("organize file finished",
			zap.String("source", source),
			zap.String("dest", dest),
			zap.String("mode", string(mode)),
			zap.Int("organized", res.Organized),
			zap.Int("replaced", res.Replaced),
			zap.Int("skipped", res.Skipped),
			zap.Any("skip_reasons", OrganizeSkipReasonCounts(res)),
		)
		return res, nil
	}
	walkErr := walk(source, func(path string, wi walkInfo) error {
		if wi.isDir {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := videoExtensions[ext]; !ok {
			return nil
		}
		if skipped, reason := shouldSkipOrganizeSourceVideo(path, source); skipped {
			res.Skipped++
			res.Items = append(res.Items, OrganizePreviewItem{Source: path, Action: "skip", Reason: reason})
			return nil
		}
		if err := o.organizeSourceFile(ctx, path, source, dest, mode, opts.MediaType, opts.MediaCategory, opts.DryRun, opts.AllowReplaceExisting, res); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", filepath.Base(path), err.Error()))
			res.Items = append(res.Items, OrganizePreviewItem{Source: path, Action: "error", Reason: err.Error()})
		}
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}
	o.log.Info("organize directory finished",
		zap.String("source", source),
		zap.String("dest", dest),
		zap.String("mode", string(mode)),
		zap.Int("organized", res.Organized),
		zap.Int("replaced", res.Replaced),
		zap.Int("skipped", res.Skipped),
		zap.Any("skip_reasons", OrganizeSkipReasonCounts(res)),
	)
	return res, nil
}

type organizeDirectoryLayout struct {
	MediaType string
	Category  string
}

// organizeSourceFile organizes a single video file from the source directory
// into destRoot, applying dedup + 洗版.
func (o *OrganizerService) organizeSourceFile(ctx context.Context, src, sourceRoot, destRoot string, mode TransferMode, mediaTypeOverride, mediaCategoryOverride string, dryRun bool, allowReplaceExisting bool, res *OrganizeResult) error {
	ext := filepath.Ext(src)
	title, year := CleanQuery(src)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(src), ext)
	}
	// CleanQuery lowercases the parsed title; title-case it so organized output
	// matches typical library casing (and stays consistent for dedup).
	parsedTitle := title
	title = sanitizeFilename(titleCaseWords(title))
	if title == "" {
		title = "Unknown"
	}
	season, episode := ParseEpisode(src)
	layout := o.inferOrganizeDirectoryLayout(src, sourceRoot)
	if forced := normalizeOrganizeMediaType(mediaTypeOverride); forced != "" {
		if layout.Category != "" && layout.MediaType != "" && layout.MediaType != forced {
			layout.Category = ""
		}
		layout.MediaType = forced
	}
	if layout.MediaType == "" {
		layout.MediaType = o.inferMediaTypeForSourceFile(src, title, season, episode)
	}
	if category := strings.TrimSpace(mediaCategoryOverride); category != "" {
		layout.Category = sanitizeFilename(category)
	} else if layout.Category == "" {
		layout.Category = o.smartClassifySourceFile(ctx, src, sourceRoot, layout.MediaType, title, parsedTitle)
	}
	layoutRoot, matchedLibrary := o.organizeLibraryRootForLayout(ctx, destRoot, layout.MediaType, layout.Category)
	if !matchedLibrary && layout.MediaType != "" {
		layoutRoot = o.organizeRoot(destRoot, layout.MediaType, layout.Category)
	}
	if !matchedLibrary && layout.Category != "" {
		layoutRoot = categoryRoot(layoutRoot, sanitizeFilename(layout.Category))
	}
	if !matchedLibrary && !dryRun {
		o.ensureOrganizeLibraryForRoot(ctx, layoutRoot, layout.MediaType, layout.Category)
	}

	var destDir, dst, episodeTag string
	isSeries := season > 0 || episode > 0
	if layout.MediaType != "" {
		isSeries = isSeriesLibraryType(layout.MediaType) && (season > 0 || episode > 0)
	}
	if isSeries {
		// TV/动漫/综艺等剧集：{destRoot}/{Title}/Season XX/{Title} - SxxExx.ext
		episodeTag = fmt.Sprintf("S%02dE%02d", season, episode)
		destDir = filepath.Join(layoutRoot, title, fmt.Sprintf("Season %02d", season))
		dst = filepath.Join(destDir, fmt.Sprintf("%s - %s%s", title, episodeTag, ext))
	} else {
		// 电影：{destRoot}/{Title} ({Year})/{Title} ({Year}).ext
		folder := title
		if year > 0 {
			folder = fmt.Sprintf("%s (%d)", title, year)
		}
		destDir = filepath.Join(layoutRoot, folder)
		dst = filepath.Join(destDir, folder+ext)
	}

	// 源文件已经位于目标位置：无需处理。
	if filepath.Clean(src) == filepath.Clean(dst) {
		res.Skipped++
		res.Items = append(res.Items, OrganizePreviewItem{
			Source: src, Target: dst, Action: "skip", Reason: organizeSkipAlreadyOrganized,
			MediaType: layout.MediaType, Category: layout.Category, Title: title,
		})
		return nil
	}

	// 去重候选：合并「目的地媒体库已扫描入库的同一媒体（按标题/年份/季集匹配，
	// 不受目录大小写或布局影响）」与「目标文件夹内已存在的同名视频文件」。
	identityExisting := o.existingByIdentity(ctx, destRoot, parsedTitle, year, season, episode)
	folderExisting := o.existingByFolder(destDir, episodeTag)
	existing := mergeExistingVersionPaths(identityExisting, folderExisting)
	if len(existing) > 0 {
		srcArea := o.resolutionArea(ctx, src)
		bestArea := 0
		for _, e := range existing {
			if a := o.resolutionArea(ctx, e); a > bestArea {
				bestArea = a
			}
		}
		// 洗版：仅当来源与已存在版本的分辨率都可判定、且来源更高时才替换；
		// 任一方分辨率未知时保守跳过，绝不删除无法判定的已存在文件。
		if allowReplaceExisting && srcArea > 0 && bestArea > 0 && srcArea > bestArea {
			res.Items = append(res.Items, OrganizePreviewItem{
				Source: src, Target: dst, Action: "replace", Reason: "higher resolution",
				MediaType: layout.MediaType, Category: layout.Category, Title: title,
			})
			if dryRun {
				res.Replaced++
				return nil
			}
			if err := o.replaceVersions(ctx, src, existing, dst, mode); err != nil {
				return err
			}
			o.log.Info("organize replaced lower-resolution media",
				zap.String("from", src),
				zap.String("to", dst),
				zap.Int("src_area", srcArea),
				zap.Int("existing_area", bestArea),
			)
			res.Replaced++
			return nil
		}
		// 去重：目的地已存在同一媒体且不低于来源分辨率，跳过不再整理过去。
		reason := organizeSkipTargetExists
		if len(identityExisting) > 0 || o.allExistingPathsInDB(ctx, existing) {
			reason = organizeSkipDuplicateLibrary
		}
		o.log.Debug("organize skip duplicate",
			zap.String("src", src), zap.String("dest_dir", destDir), zap.String("reason", reason))
		res.Skipped++
		res.Items = append(res.Items, OrganizePreviewItem{
			Source: src, Target: dst, Action: "skip", Reason: reason,
			MediaType: layout.MediaType, Category: layout.Category, Title: title,
		})
		return nil
	}

	res.Items = append(res.Items, OrganizePreviewItem{
		Source: src, Target: dst, Action: "organize",
		MediaType: layout.MediaType, Category: layout.Category, Title: title,
	})
	if dryRun {
		res.Organized++
		return nil
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil { // #nosec G301 -- organized media directories must remain readable by NAS/player users.
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		res.Skipped++
		if len(res.Items) > 0 {
			res.Items[len(res.Items)-1].Action = "skip"
			res.Items[len(res.Items)-1].Reason = organizeSkipTargetExists
		}
		return nil
	}
	if err := transferFile(src, dst, mode); err != nil {
		return err
	}
	if err := transferSidecarNFO(src, dst, mode); err != nil {
		o.log.Warn("organize sidecar nfo failed",
			zap.String("from", src), zap.String("to", dst), zap.Error(err))
	}
	res.Organized++
	return nil
}

func shouldSkipOrganizeSourceVideo(path, sourceRoot string) (bool, string) {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(sourceRoot)
	if rel, err := filepath.Rel(cleanRoot, cleanPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		dir := filepath.Dir(rel)
		if dir != "." {
			for _, part := range strings.Split(dir, string(os.PathSeparator)) {
				switch normalizeOrganizeCategoryKey(part) {
				case "sample", "samples", "trailer", "trailers", "preview", "previews", "teaser", "teasers":
					return true, organizeSkipSampleClip
				}
			}
		}
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(cleanPath), filepath.Ext(cleanPath)))
	normalized := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(base)
	fields := strings.Fields(normalized)
	if len(fields) == 0 {
		return false, ""
	}
	if len(fields) == 1 && strings.HasPrefix(fields[0], "sample") {
		return true, organizeSkipSampleClip
	}
	last := fields[len(fields)-1]
	switch last {
	case "sample", "trailer", "preview", "teaser":
		return true, organizeSkipSampleClip
	}
	return false, ""
}

func normalizeOrganizeMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "movie", "film":
		return "movie"
	case "tv", "series", "show", "drama":
		return "tv"
	case "anime", "animation":
		return "anime"
	case "variety":
		return "variety"
	case "adult", "nsfw":
		return "adult"
	default:
		return ""
	}
}

func (o *OrganizerService) inferMediaTypeForSourceFile(src, title string, season, episode int) string {
	if season > 0 || episode > 0 {
		return "tv"
	}
	return normalizeMediaType("", title, src)
}

func (o *OrganizerService) smartClassifySourceFile(ctx context.Context, src, sourceRoot, mediaType, title, parsedTitle string) string {
	if o == nil || !o.isSmartClassifyEnabled(ctx) {
		return ""
	}
	seriesLike := isSeriesLibraryType(mediaType)
	input := mediaClassifyInput{
		MediaType: mediaType,
		Title:     strings.Join([]string{title, parsedTitle, filepath.Base(src)}, " "),
		Category:  strings.Join(organizeDirectoryCategoryCandidates(src, sourceRoot), " "),
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

func (o *OrganizerService) inferOrganizeDirectoryLayout(src, sourceRoot string) organizeDirectoryLayout {
	for _, name := range organizeDirectoryCategoryCandidates(src, sourceRoot) {
		if mediaType, category := o.mediaTypeForDirectoryCategory(name); mediaType != "" && category != "" {
			return organizeDirectoryLayout{MediaType: mediaType, Category: category}
		}
	}
	return organizeDirectoryLayout{}
}

func organizeDirectoryCategoryCandidates(src, sourceRoot string) []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || value == "." || value == string(os.PathSeparator) {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}

	cleanSourceRoot := filepath.Clean(sourceRoot)
	add(filepath.Base(cleanSourceRoot))
	rel, err := filepath.Rel(cleanSourceRoot, filepath.Clean(src))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return out
	}
	dir := filepath.Dir(rel)
	if dir == "." {
		return out
	}
	for _, part := range strings.Split(dir, string(os.PathSeparator)) {
		add(part)
	}
	return out
}

func (o *OrganizerService) mediaTypeForDirectoryCategory(name string) (string, string) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return "", ""
	}
	if hit, ok := o.directoryCategoryTypes()[key]; ok {
		return hit.MediaType, hit.Category
	}
	return "", ""
}

func (o *OrganizerService) directoryCategoryTypes() map[string]organizeDirectoryLayout {
	categories := o.categoryMap()
	out := map[string]organizeDirectoryLayout{}
	add := func(category, mediaType string) {
		category = strings.TrimSpace(category)
		if category == "" {
			return
		}
		out[strings.ToLower(category)] = organizeDirectoryLayout{
			MediaType: mediaType,
			Category:  category,
		}
	}
	addConfigured := func(key, fallback, mediaType string) {
		add(fallback, mediaType)
		add(categoryName(categories, key, fallback), mediaType)
	}
	addConfigured("animation_movie", "动画电影", "movie")
	addConfigured("chinese_movie", "华语电影", "movie")
	addConfigured("jk_movie", "日韩电影", "movie")
	addConfigured("euus_movie", "欧美电影", "movie")
	addConfigured("foreign_movie", "外语电影", "movie")
	addConfigured("domestic_tv", "国产剧", "tv")
	addConfigured("euus_tv", "欧美剧", "tv")
	addConfigured("jk_tv", "日韩剧", "tv")
	addConfigured("cn_anime", "国漫", "anime")
	addConfigured("jp_anime", "日番", "anime")
	addConfigured("variety", "综艺", "variety")
	addConfigured("documentary", "纪录片", "tv")
	addConfigured("children", "儿童", "tv")
	addConfigured("uncategorized_tv", "未分类", "tv")
	addConfigured("adult", "成人", "adult")
	addConfigured("adult_9kg", "9KG", "adult")
	addConfigured("adult_jav", "番号", "adult")
	return out
}

func (o *OrganizerService) organizeLibraryRootForLayout(ctx context.Context, destRoot, mediaType, category string) (string, bool) {
	if o == nil || o.repo == nil || o.repo.Library == nil {
		return "", false
	}
	libraries, err := o.repo.Library.List(ctx)
	if err != nil {
		if o.log != nil {
			o.log.Debug("list libraries for organize target failed", zap.Error(err))
		}
		return "", false
	}
	destRoot = filepath.Clean(strings.TrimSpace(destRoot))
	mediaType = normalizeOrganizeMediaType(mediaType)
	aliases := o.organizeCategoryAliases(mediaType, category)

	bestPath := ""
	bestScore := -1
	bestDepth := -1
	for _, lib := range libraries {
		if !lib.Enabled || strings.TrimSpace(lib.Path) == "" {
			continue
		}
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			continue
		}
		if destRoot != "" && destRoot != "." && !pathWithin(lib.Path, destRoot) && !pathWithin(destRoot, lib.Path) {
			continue
		}
		categoryMatch := len(aliases) > 0 && libraryMatchesOrganizeCategory(lib, aliases)
		typeScore := organizeLibraryTypeScore(mediaType, lib.Type)
		if len(aliases) > 0 {
			if !categoryMatch {
				continue
			}
		} else if typeScore <= 0 {
			continue
		}
		score := typeScore
		if categoryMatch {
			score += 20
		}
		depth := pathDepth(lib.Path)
		if score > bestScore || (score == bestScore && depth > bestDepth) {
			bestScore = score
			bestDepth = depth
			bestPath = lib.Path
		}
	}
	if bestPath == "" {
		return "", false
	}
	return filepath.Clean(bestPath), true
}

func (o *OrganizerService) ensureOrganizeLibraryForRoot(ctx context.Context, root, mediaType, category string) {
	if o == nil || o.repo == nil || o.repo.Library == nil {
		return
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return
	}
	if _, ok := ParseCloudLibraryMount(root); ok {
		return
	}
	libraries, err := o.repo.Library.List(ctx)
	if err != nil {
		if o.log != nil {
			o.log.Debug("list libraries before organize auto-create failed", zap.Error(err))
		}
		return
	}
	for _, lib := range libraries {
		if !lib.Enabled || strings.TrimSpace(lib.Path) == "" {
			continue
		}
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			continue
		}
		if pathWithin(root, lib.Path) {
			return
		}
	}
	name := strings.TrimSpace(category)
	if name == "" {
		name = filepath.Base(root)
	}
	if name == "" || name == "." || name == string(os.PathSeparator) {
		name = organizeLibraryTypeName(mediaType)
	}
	lib := model.Library{
		Name:    name,
		Path:    root,
		Type:    organizeLibraryModelType(mediaType),
		Enabled: true,
	}
	if err := o.repo.Library.Create(ctx, &lib); err != nil {
		if o.log != nil {
			o.log.Warn("organize auto-create library failed",
				zap.String("path", root),
				zap.String("type", lib.Type),
				zap.String("name", lib.Name),
				zap.Error(err))
		}
		return
	}
	if o.log != nil {
		o.log.Info("organize auto-created missing library",
			zap.String("path", root),
			zap.String("type", lib.Type),
			zap.String("name", lib.Name))
	}
}

func organizeLibraryModelType(mediaType string) string {
	switch normalizeOrganizeMediaType(mediaType) {
	case "tv", "anime", "variety":
		return "tv"
	case "adult", "movie":
		return "movie"
	default:
		return "movie"
	}
}

func organizeLibraryTypeName(mediaType string) string {
	switch normalizeOrganizeMediaType(mediaType) {
	case "tv":
		return "电视剧"
	case "anime":
		return "动漫"
	case "variety":
		return "综艺"
	case "adult":
		return "成人"
	default:
		return "电影"
	}
}

func (o *OrganizerService) organizeCategoryAliases(mediaType, category string) map[string]struct{} {
	aliases := map[string]struct{}{}
	add := func(values ...string) {
		for _, value := range values {
			key := normalizeOrganizeCategoryKey(value)
			if key != "" {
				aliases[key] = struct{}{}
			}
		}
	}
	categories := o.categoryMap()
	add(category)
	switch normalizeOrganizeCategoryKey(category) {
	case normalizeOrganizeCategoryKey(categoryName(categories, "jp_anime", "日番")), "日番", "日漫", "日本动漫", "日本動畫", "日本动画":
		add("日番", "日漫", "日本动漫", "日本动画")
	case normalizeOrganizeCategoryKey(categoryName(categories, "cn_anime", "国漫")), "国漫", "国产动漫", "國漫":
		add("国漫", "国产动漫")
	case normalizeOrganizeCategoryKey(categoryName(categories, "domestic_tv", "国产剧")), "国产剧", "国剧", "大陆剧", "国产电视剧":
		add("国产剧", "国剧", "大陆剧", "国产电视剧")
	case normalizeOrganizeCategoryKey(categoryName(categories, "euus_tv", "欧美剧")), "欧美剧", "欧美电视剧":
		add("欧美剧", "欧美电视剧")
	case normalizeOrganizeCategoryKey(categoryName(categories, "jk_tv", "日韩剧")), "日韩剧", "日剧", "韩剧":
		add("日韩剧", "日剧", "韩剧")
	case normalizeOrganizeCategoryKey(categoryName(categories, "variety", "综艺")), "综艺", "真人秀":
		add("综艺", "真人秀")
	case normalizeOrganizeCategoryKey(categoryName(categories, "documentary", "纪录片")), "纪录片", "纪录":
		add("纪录片", "纪录")
	case normalizeOrganizeCategoryKey(categoryName(categories, "children", "儿童")), "儿童", "少儿":
		add("儿童", "少儿")
	case normalizeOrganizeCategoryKey(categoryName(categories, "chinese_movie", "华语电影")), "华语电影", "国产电影", "大陆电影":
		add("华语电影", "国产电影", "大陆电影")
	case normalizeOrganizeCategoryKey(categoryName(categories, "foreign_movie", "外语电影")), "外语电影":
		add("外语电影")
	case normalizeOrganizeCategoryKey(categoryName(categories, "animation_movie", "动画电影")), "动画电影", "动漫电影":
		add("动画电影", "动漫电影")
	case normalizeOrganizeCategoryKey(categoryName(categories, "adult", "成人")), "成人":
		add("成人")
	case normalizeOrganizeCategoryKey(categoryName(categories, "adult_9kg", "9KG")), "9kg":
		add("9KG")
	case normalizeOrganizeCategoryKey(categoryName(categories, "adult_jav", "番号")), "番号", "jav":
		add("番号", "JAV")
	}
	return aliases
}

func libraryMatchesOrganizeCategory(lib model.Library, aliases map[string]struct{}) bool {
	for _, value := range []string{lib.Name, filepath.Base(filepath.Clean(lib.Path))} {
		if _, ok := aliases[normalizeOrganizeCategoryKey(value)]; ok {
			return true
		}
	}
	return false
}

func organizeLibraryTypeScore(mediaType, libraryType string) int {
	libraryType = normalizeOrganizeMediaType(libraryType)
	if mediaType == "" || libraryType == "" {
		return 1
	}
	if mediaType == libraryType {
		return 8
	}
	if mediaType == "anime" && libraryType == "tv" {
		return 5
	}
	if mediaType == "variety" && libraryType == "tv" {
		return 5
	}
	return 0
}

func normalizeOrganizeCategoryKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func pathDepth(path string) int {
	path = filepath.Clean(path)
	if path == "." || path == string(os.PathSeparator) {
		return 0
	}
	return len(strings.Split(path, string(os.PathSeparator)))
}

// existingVersionPaths returns existing destination files that represent the
// same media, combining two strategies and de-duplicating by path:
//
//  1. DB identity: media rows already scanned into the destination root whose
//     title (case-insensitive) + year [or + season/episode] match the source.
//     This is robust to directory case/layout differences.
//  2. Filesystem: video files inside the computed destination folder (matching
//     the SxxExx tag for episodes). Covers destinations that were not scanned.
func (o *OrganizerService) existingVersionPaths(ctx context.Context, destRoot, destDir, title, episodeTag string, year, season, episode int) []string {
	return mergeExistingVersionPaths(
		o.existingByIdentity(ctx, destRoot, title, year, season, episode),
		o.existingByFolder(destDir, episodeTag),
	)
}

func mergeExistingVersionPaths(groups ...[]string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		c := filepath.Clean(p)
		if _, ok := seen[c]; ok {
			return
		}
		if _, err := os.Stat(c); err != nil {
			return
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	for _, group := range groups {
		for _, p := range group {
			add(p)
		}
	}
	return out
}

func (o *OrganizerService) allExistingPathsInDB(ctx context.Context, paths []string) bool {
	if o == nil || o.repo == nil || o.repo.DB == nil || len(paths) == 0 {
		return false
	}
	cleaned := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		cleaned = append(cleaned, path)
	}
	if len(cleaned) == 0 {
		return false
	}
	var count int64
	if err := o.repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Where("path IN ?", cleaned).
		Count(&count).Error; err != nil {
		return false
	}
	return count == int64(len(cleaned))
}

// existingByIdentity finds scanned destination media matching the parsed
// identity (case-insensitive title + year for movies; title + season/episode
// for episodes), located under destRoot.
func (o *OrganizerService) existingByIdentity(ctx context.Context, destRoot, title string, year, season, episode int) []string {
	if o.repo == nil || o.repo.DB == nil {
		return nil
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	q := o.repo.DB.WithContext(ctx).Model(&model.Media{}).
		Where("deleted_at IS NULL").
		Where("LOWER(title) = ?", strings.ToLower(title))
	if season > 0 || episode > 0 {
		q = q.Where("season_num = ? AND episode_num = ?", season, episode)
	} else if year > 0 {
		q = q.Where("year = ?", year)
	}
	var rows []model.Media
	if err := q.Find(&rows).Error; err != nil {
		return nil
	}
	var out []string
	for _, r := range rows {
		if r.Path != "" && pathWithin(r.Path, destRoot) {
			out = append(out, r.Path)
		}
	}
	return out
}

// existingByFolder returns video files already present in destDir that
// represent the same media. For an episode (episodeTag != "") it matches files
// carrying the same SxxExx tag; for a movie it matches every video file in the
// movie folder.
func (o *OrganizerService) existingByFolder(destDir, episodeTag string) []string {
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return nil
	}
	tag := strings.ToLower(episodeTag)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if _, ok := videoExtensions[strings.ToLower(filepath.Ext(name))]; !ok {
			continue
		}
		if tag != "" && !strings.Contains(strings.ToLower(name), tag) {
			continue
		}
		out = append(out, filepath.Join(destDir, name))
	}
	return out
}

// titleCaseWords upper-cases the first letter of each ASCII word; CJK and other
// non-ASCII leading characters are left untouched. Roman numerals (ii, iii, iv,
// …) are fully upper-cased so sequels like "Wandering Earth II" keep their
// canonical casing instead of becoming "Ii".
func titleCaseWords(s string) string {
	fields := strings.Fields(s)
	for i, w := range fields {
		if isRomanNumeral(w) {
			fields[i] = strings.ToUpper(w)
			continue
		}
		r := []rune(w)
		if len(r) > 0 && r[0] < 128 {
			r[0] = unicode.ToUpper(r[0])
			fields[i] = string(r)
		}
	}
	return strings.Join(fields, " ")
}

// sequelNumerals is a conservative whitelist of multi-letter Roman numerals
// used for movie/series sequels. A whitelist avoids false positives on normal
// English words that happen to be valid numerals (e.g. "mix", "civ", "mi").
var sequelNumerals = map[string]struct{}{
	"ii": {}, "iii": {}, "iv": {}, "vi": {}, "vii": {}, "viii": {},
	"ix": {}, "xi": {}, "xii": {}, "xiii": {}, "xiv": {}, "xv": {},
}

func isRomanNumeral(w string) bool {
	_, ok := sequelNumerals[strings.ToLower(w)]
	return ok
}

// replaceVersions removes the existing lower-resolution files (and their NFO
// sidecars + DB rows) and transfers src into dst.
func (o *OrganizerService) replaceVersions(ctx context.Context, src string, existing []string, dst string, mode TransferMode) error {
	for _, e := range existing {
		if err := os.Remove(e); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove existing %s: %w", e, err)
		}
		if nfo := nfoPath(e); nfo != "" {
			_ = os.Remove(nfo)
		}
		if o.repo != nil && o.repo.DB != nil {
			_ = o.repo.DB.WithContext(ctx).Where("path = ?", e).Delete(&model.Media{}).Error
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { // #nosec G301 -- organized media directories must remain readable by NAS/player users.
		return err
	}
	if err := transferFile(src, dst, mode); err != nil {
		return err
	}
	if err := transferSidecarNFO(src, dst, mode); err != nil {
		o.log.Warn("organize sidecar nfo failed",
			zap.String("from", src), zap.String("to", dst), zap.Error(err))
	}
	return nil
}

// resolutionArea returns the pixel area (width*height) of a video file for 洗版
// comparison. It prefers ffprobe; when unavailable it falls back to a
// resolution token in the filename (2160p/1080p/720p). Returns 0 when the
// resolution cannot be determined, in which case the caller treats the file as
// "unknown" and never performs a destructive replace.
func (o *OrganizerService) resolutionArea(ctx context.Context, path string) int {
	// Prefer a scanned media row's stored dimensions. The destination library
	// is normally scanned with ffprobe, so its files have accurate Width/Height
	// even after organize stripped the resolution token from the filename.
	if o.repo != nil && o.repo.DB != nil {
		var m model.Media
		if err := o.repo.DB.WithContext(ctx).
			Select("width", "height").
			Where("path = ?", path).
			Limit(1).Take(&m).Error; err == nil && m.Width > 0 && m.Height > 0 {
			return m.Width * m.Height
		}
	}
	if o.probe != nil {
		if pr, err := o.probe.Probe(ctx, path); err == nil && pr != nil && pr.Width > 0 && pr.Height > 0 {
			return pr.Width * pr.Height
		}
	}
	switch detectResolutionScore(strings.ToLower(filepath.Base(path))) {
	case 4:
		return 3840 * 2160
	case 3:
		return 1920 * 1080
	case 2:
		return 1280 * 720
	default:
		return 0
	}
}

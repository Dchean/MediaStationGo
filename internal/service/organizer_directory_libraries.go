package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func (o *OrganizerService) organizeLibraryRootForLayout(ctx context.Context, destRoot, mediaType, category string) (string, bool) {
	lib, ok := o.organizeLibraryForLayout(ctx, destRoot, mediaType, category)
	if !ok {
		return "", false
	}
	return filepath.Clean(lib.Path), true
}

func (o *OrganizerService) organizeLibraryForLayout(ctx context.Context, destRoot, mediaType, category string) (model.Library, bool) {
	if o == nil || o.repo == nil || o.repo.Library == nil {
		return model.Library{}, false
	}
	libraries, err := o.repo.Library.List(ctx)
	if err != nil {
		if o.log != nil {
			o.log.Debug("list libraries for organize target failed", zap.Error(err))
		}
		return model.Library{}, false
	}
	destRoot = filepath.Clean(strings.TrimSpace(destRoot))
	mediaType = normalizeOrganizeMediaType(mediaType)
	aliases := o.organizeCategoryAliases(mediaType, category)

	var best model.Library
	bestScore := -1
	bestDepth := -1
	for _, lib := range libraries {
		if !lib.Enabled || strings.TrimSpace(lib.Path) == "" {
			continue
		}
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			continue
		}
		if isOrganizeStagingDir(lib.Path) {
			// "手动整理"等暂存库不作为入库目标,避免把媒体留在暂存目录里。
			continue
		}
		scopeScore, inScope := o.organizeLibraryTargetScopeScore(lib.Path, destRoot, mediaType, category)
		if !inScope {
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
		score += scopeScore
		depth := pathDepth(lib.Path)
		if score > bestScore || (score == bestScore && depth > bestDepth) {
			bestScore = score
			bestDepth = depth
			best = lib
		}
	}
	if strings.TrimSpace(best.Path) == "" {
		return model.Library{}, false
	}
	best.Path = filepath.Clean(best.Path)
	return best, true
}

func (o *OrganizerService) organizeLibraryTargetScopeScore(libPath, destRoot, mediaType, category string) (int, bool) {
	libPath = filepath.Clean(strings.TrimSpace(libPath))
	destRoot = filepath.Clean(strings.TrimSpace(destRoot))
	if destRoot == "" || destRoot == "." {
		return 0, true
	}
	if libPath == "" || libPath == "." {
		return 0, false
	}
	collectionRoot := organizeMediaCollectionRoot(destRoot)
	if collectionRoot != "" && pathWithin(libPath, collectionRoot) && !o.organizeLibraryMatchesExpectedPhysicalRoot(libPath, collectionRoot, mediaType, category) {
		return 0, false
	}
	if pathWithin(libPath, destRoot) || pathWithin(destRoot, libPath) {
		return 8, true
	}
	if _, destCategory := o.mediaTypeForDirectoryCategory(filepath.Base(destRoot)); destCategory == "" {
		if collectionRoot != "" && pathWithin(libPath, collectionRoot) {
			return o.organizeLibraryPhysicalRootScore(libPath, collectionRoot, mediaType, category), true
		}
		return 0, false
	}
	if strings.EqualFold(filepath.Dir(libPath), filepath.Dir(destRoot)) {
		return 4, true
	}
	if collectionRoot != "" && pathWithin(libPath, collectionRoot) {
		return o.organizeLibraryPhysicalRootScore(libPath, collectionRoot, mediaType, category), true
	}
	return 0, false
}

func (o *OrganizerService) organizeLibraryMatchesExpectedPhysicalRoot(libPath, collectionRoot, mediaType, category string) bool {
	if strings.TrimSpace(category) == "" {
		return true
	}
	physicalRoot := o.categoryPhysicalRootDir(category)
	if physicalRoot == "" {
		physicalRoot = mediaTypeRootDir(mediaType)
	}
	if physicalRoot == "" {
		return true
	}
	return pathHasDirectChild(libPath, collectionRoot, physicalRoot)
}

func (o *OrganizerService) organizeLibraryPhysicalRootScore(libPath, collectionRoot, mediaType, category string) int {
	physicalRoot := o.categoryPhysicalRootDir(category)
	if physicalRoot == "" {
		physicalRoot = mediaTypeRootDir(mediaType)
	}
	if physicalRoot == "" {
		return 1
	}
	if pathHasDirectChild(libPath, collectionRoot, physicalRoot) {
		return 14
	}
	return 1
}

func organizeMediaCollectionRoot(path string) string {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." {
		return ""
	}
	if isGenericMediaRoot(clean) {
		return clean
	}
	base := filepath.Base(clean)
	parent := filepath.Dir(clean)
	if isPhysicalMediaRootDir(base) {
		return parent
	}
	if isPhysicalMediaRootDir(filepath.Base(parent)) {
		return filepath.Dir(parent)
	}
	return ""
}

func isPhysicalMediaRootDir(name string) bool {
	switch normalizeOrganizeCategoryKey(name) {
	case normalizeOrganizeCategoryKey("电影"), normalizeOrganizeCategoryKey("电视剧"), normalizeOrganizeCategoryKey("动漫"), normalizeOrganizeCategoryKey("成人"):
		return true
	default:
		return false
	}
}

func pathHasDirectChild(path, root, child string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) == 0 {
		return false
	}
	return strings.EqualFold(parts[0], child)
}

func (o *OrganizerService) ensureOrganizeLibraryForRoot(ctx context.Context, root, mediaType, category string) (model.Library, bool) {
	if o == nil || o.repo == nil || o.repo.Library == nil {
		return model.Library{}, false
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return model.Library{}, false
	}
	if _, ok := ParseCloudLibraryMount(root); ok {
		return model.Library{}, false
	}
	libraries, err := o.repo.Library.List(ctx)
	if err != nil {
		if o.log != nil {
			o.log.Debug("list libraries before organize auto-create failed", zap.Error(err))
		}
		return model.Library{}, false
	}
	var containingLibrary model.Library
	hasContainingLibrary := false
	for _, lib := range libraries {
		if !lib.Enabled || strings.TrimSpace(lib.Path) == "" {
			continue
		}
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			continue
		}
		lib.Path = filepath.Clean(lib.Path)
		if strings.EqualFold(lib.Path, root) {
			return lib, true
		}
		if pathWithin(root, lib.Path) {
			containingLibrary = lib
			hasContainingLibrary = true
		}
	}
	if strings.TrimSpace(category) == "" && hasContainingLibrary {
		return containingLibrary, true
	}
	if !o.autoAddLibraryEnabled(ctx) {
		if o.log != nil {
			o.log.Debug("organize skipped missing library auto-create",
				zap.String("path", root),
				zap.String("media_type", mediaType),
				zap.String("category", category))
		}
		return model.Library{}, false
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
	if err := o.repo.Library.CreateWithRoots(ctx, &lib, []model.LibraryRoot{{
		Name:      name,
		Path:      root,
		Enabled:   true,
		SortOrder: 0,
	}}); err != nil {
		if o.log != nil {
			o.log.Warn("organize auto-create library failed",
				zap.String("path", root),
				zap.String("type", lib.Type),
				zap.String("name", lib.Name),
				zap.Error(err))
		}
		return model.Library{}, false
	}
	if o.log != nil {
		o.log.Info("organize auto-created missing library",
			zap.String("path", root),
			zap.String("type", lib.Type),
			zap.String("name", lib.Name))
	}
	return lib, true
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
	case normalizeOrganizeCategoryKey(categoryName(categories, "euus_anime", "欧美动漫")), "欧美动漫", "欧美动画", "西方动画":
		add("欧美动漫", "欧美动画", "西方动画")
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

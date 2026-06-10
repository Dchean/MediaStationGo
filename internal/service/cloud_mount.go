package service

import (
	"net/url"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

// CloudMountInfo is the canonical identity of a mounted cloud library. ScanDir
// is the provider id/path used for listing. DisplayDir is a hierarchical path
// used to prevent mounting both a parent and its child as separate libraries.
type CloudMountInfo struct {
	Provider   string
	DisplayDir string
	ScanDir    string
	Path       string
}

type CloudMountConflict struct {
	Library            model.Library `json:"library"`
	Exact              bool          `json:"exact"`
	Nested             bool          `json:"nested"`
	ExistingIsAncestor bool          `json:"existing_is_ancestor"`
}

func BuildCloudLibraryPath(provider, scanDir, displayDir string) string {
	provider = strings.TrimSpace(provider)
	scanDir = normalizeCloudMountDir(provider, scanDir)
	displayDir = normalizeCloudMountDir(provider, firstNonEmpty(displayDir, scanDir))
	if provider == "" {
		return ""
	}
	base := "cloud://" + provider
	if displayDir == "" {
		if scanDir != "" {
			return base + "?dir=" + url.QueryEscape(scanDir)
		}
		return base
	}
	path := base + "/" + url.PathEscape(displayDir)
	if scanDir != "" && scanDir != displayDir {
		path += "?dir=" + url.QueryEscape(scanDir)
	}
	return path
}

func ParseCloudLibraryMount(raw string) (CloudMountInfo, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "cloud://") {
		return CloudMountInfo{}, false
	}
	u, err := url.Parse(raw)
	if err != nil || strings.ToLower(u.Scheme) != "cloud" {
		return CloudMountInfo{}, false
	}
	provider := strings.TrimSpace(u.Host)
	if provider == "" {
		return CloudMountInfo{}, false
	}
	displayDir := strings.Trim(strings.TrimSpace(u.Path), "/")
	if decoded, err := url.PathUnescape(displayDir); err == nil {
		displayDir = decoded
	}
	scanDir := displayDir
	if qDir := strings.TrimSpace(u.Query().Get("dir")); qDir != "" {
		if decoded, err := url.QueryUnescape(qDir); err == nil {
			qDir = decoded
		}
		scanDir = qDir
	}
	displayDir = normalizeCloudMountDir(provider, displayDir)
	scanDir = normalizeCloudMountDir(provider, scanDir)
	return CloudMountInfo{
		Provider:   provider,
		DisplayDir: displayDir,
		ScanDir:    scanDir,
		Path:       raw,
	}, true
}

func FindCloudMountConflict(libs []model.Library, provider, scanDir, displayDir string) *CloudMountConflict {
	candidate := CloudMountInfo{
		Provider:   strings.TrimSpace(provider),
		DisplayDir: normalizeCloudMountDir(provider, firstNonEmpty(displayDir, scanDir)),
		ScanDir:    normalizeCloudMountDir(provider, scanDir),
	}
	for _, lib := range libs {
		existing, ok := ParseCloudLibraryMount(lib.Path)
		if !ok || existing.Provider != candidate.Provider {
			continue
		}
		if existing.DisplayDir == candidate.DisplayDir {
			return &CloudMountConflict{Library: lib, Exact: true}
		}
		if existing.ScanDir != "" && candidate.ScanDir != "" && existing.ScanDir == candidate.ScanDir {
			return &CloudMountConflict{Library: lib, Exact: true}
		}
		if cloudMountAncestor(candidate.DisplayDir, existing.DisplayDir) {
			return &CloudMountConflict{Library: lib, Nested: true}
		}
	}
	return nil
}

func CloudLibraryShadowed(libs []model.Library, lib model.Library) *CloudMountConflict {
	current, ok := ParseCloudLibraryMount(lib.Path)
	if !ok {
		return nil
	}
	for _, existing := range libs {
		if existing.ID == lib.ID || !existing.Enabled {
			continue
		}
		info, ok := ParseCloudLibraryMount(existing.Path)
		if !ok || info.Provider != current.Provider {
			continue
		}
		if info.DisplayDir == current.DisplayDir && existing.CreatedAt.Before(lib.CreatedAt) {
			return &CloudMountConflict{Library: existing, Exact: true}
		}
		if cloudMountAncestor(current.DisplayDir, info.DisplayDir) {
			return &CloudMountConflict{Library: existing, Nested: true}
		}
	}
	return nil
}

func FilterShadowedCloudLibraries(libs []model.Library) []model.Library {
	out := make([]model.Library, 0, len(libs))
	for _, lib := range libs {
		if CloudLibraryShadowed(libs, lib) == nil {
			out = append(out, lib)
		}
	}
	return out
}

func ShadowedCloudLibraryIDSet(libs []model.Library) map[string]bool {
	out := make(map[string]bool)
	for _, lib := range libs {
		if CloudLibraryShadowed(libs, lib) != nil {
			out[lib.ID] = true
		}
	}
	return out
}

func InferCloudMountMediaType(dir, name string) string {
	text := strings.ToLower(dir + " " + name)
	switch {
	case strings.Contains(text, "成人") || strings.Contains(text, "adult") || strings.Contains(text, "jav") || strings.Contains(text, "9kg"):
		return "adult"
	case containsAny(text, "动画电影", "华语电影", "外语电影", "欧美电影", "日韩电影", "韩国电影", "日本电影", "港台电影", "香港电影", "台湾电影", "大陆电影", "国产电影", "纪录片", "演唱会", "电影", "movie", "movies", "film", "films", "documentary", "concert"):
		return "movie"
	case containsAny(text, "综艺", "真人秀", "脱口秀", "晚会", "variety"):
		return "variety"
	case containsAny(text, "国漫", "日漫", "日番", "番剧", "动漫", "欧美动漫", "动画剧集", "anime"):
		return "anime"
	case containsAny(text, "国产剧", "大陆剧", "华语剧", "欧美剧", "日韩剧", "韩剧", "日剧", "港剧", "台剧", "泰剧", "英剧", "美剧", "短剧", "电视剧", "剧集", "连续剧", "series", "tv", "shows"):
		return "tv"
	default:
		return "movie"
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func cloudMountAncestor(parent, child string) bool {
	parent = strings.Trim(parent, "/")
	child = strings.Trim(child, "/")
	if parent == child {
		return false
	}
	if parent == "" {
		return child != ""
	}
	return strings.HasPrefix(child, parent+"/")
}

func normalizeCloudMountDir(provider, value string) string {
	value = strings.TrimSpace(value)
	if decoded, err := url.PathUnescape(value); err == nil {
		value = decoded
	}
	if decoded, err := url.QueryUnescape(value); err == nil {
		value = decoded
	}
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "." || ((provider == cloud.Type115 || provider == cloud.TypeQuark) && value == "0") {
		return ""
	}
	return value
}

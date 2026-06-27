package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func FilterDisplayCloudLibraries(ctx context.Context, repo *repository.Container, libs []model.Library) []model.Library {
	if len(libs) == 0 {
		return libs
	}
	libs = FilterDeprecatedNativeCloudLibraries(libs)
	libs = FilterInternalCloudAutoCategoryLibraries(libs)
	counts := cloudLibraryMediaCounts(ctx, repo, libs)
	collapsed := make([]model.Library, 0, len(libs))
	byKey := make(map[string]int, len(libs))
	for _, lib := range libs {
		key, ok := cloudLibraryDisplayKey(lib)
		if !ok {
			collapsed = append(collapsed, lib)
			continue
		}
		if prevIndex, exists := byKey[key]; exists {
			if betterDisplayCloudLibrary(lib, collapsed[prevIndex], counts) {
				collapsed[prevIndex] = lib
			}
			continue
		}
		byKey[key] = len(collapsed)
		collapsed = append(collapsed, lib)
	}
	collapsed = FilterShadowedCloudLibraries(collapsed)
	return mergeDisplayCloudLibraries(collapsed)
}

func FilterInternalCloudAutoCategoryLibraries(libs []model.Library) []model.Library {
	if len(libs) == 0 {
		return libs
	}
	out := make([]model.Library, 0, len(libs))
	for _, lib := range libs {
		if CloudLibraryAutoCategory(lib) {
			continue
		}
		out = append(out, lib)
	}
	return out
}

func FilterScannableCloudLibraries(ctx context.Context, repo *repository.Container, libs []model.Library) []model.Library {
	if len(libs) == 0 {
		return libs
	}
	counts := cloudLibraryMediaCounts(ctx, repo, libs)
	collapsed := make([]model.Library, 0, len(libs))
	byKey := make(map[string]int, len(libs))
	for _, lib := range libs {
		if CloudLibraryAutoCategory(lib) {
			continue
		}
		if info, ok := ParseCloudLibraryMount(lib.Path); ok && IsDeprecatedNativeCloudProvider(info.Provider) {
			continue
		}
		key, ok := cloudLibraryDisplayKey(lib)
		if !ok {
			collapsed = append(collapsed, lib)
			continue
		}
		if prevIndex, exists := byKey[key]; exists {
			if betterDisplayCloudLibrary(lib, collapsed[prevIndex], counts) {
				collapsed[prevIndex] = lib
			}
			continue
		}
		byKey[key] = len(collapsed)
		collapsed = append(collapsed, lib)
	}
	return FilterShadowedCloudLibraries(collapsed)
}

func FilterDeprecatedNativeCloudLibraries(libs []model.Library) []model.Library {
	if len(libs) == 0 {
		return libs
	}
	out := make([]model.Library, 0, len(libs))
	for _, lib := range libs {
		info, ok := ParseCloudLibraryMount(lib.Path)
		if ok && IsDeprecatedNativeCloudProvider(info.Provider) {
			continue
		}
		out = append(out, lib)
	}
	return out
}

func NormalizeCloudLibraryDisplayNames(libs []model.Library) []model.Library {
	out := make([]model.Library, 0, len(libs))
	for _, lib := range libs {
		if displayName, ok := CloudLibraryDisplayName(lib); ok && displayName != "" {
			lib.Name = displayName
		}
		out = append(out, lib)
	}
	return out
}

func cloudLibraryMediaCounts(ctx context.Context, repo *repository.Container, libs []model.Library) map[string]int64 {
	counts := make(map[string]int64, len(libs))
	if repo == nil || repo.DB == nil || len(libs) == 0 {
		return counts
	}
	ids := make([]string, 0, len(libs))
	for _, lib := range libs {
		if _, ok := ParseCloudLibraryMount(lib.Path); ok {
			ids = append(ids, lib.ID)
		}
	}
	if len(ids) == 0 {
		return counts
	}
	var rows []struct {
		LibraryID string
		Count     int64
	}
	if err := repo.DB.WithContext(ctx).
		Model(&model.Media{}).
		Select("library_id, COUNT(*) AS count").
		Where("library_id IN ? AND deleted_at IS NULL", ids).
		Group("library_id").
		Scan(&rows).Error; err != nil {
		return counts
	}
	for _, row := range rows {
		counts[row.LibraryID] = row.Count
	}
	return counts
}

func cloudLibraryDisplayKey(lib model.Library) (string, bool) {
	info, ok := ParseCloudLibraryMount(lib.Path)
	if !ok {
		return "", false
	}
	dir := firstNonEmpty(info.DisplayDir, info.ScanDir)
	return info.Provider + "\x00" + dir, true
}

func betterDisplayCloudLibrary(candidate, current model.Library, counts map[string]int64) bool {
	candidateCount := counts[candidate.ID]
	currentCount := counts[current.ID]
	if (candidateCount > 0) != (currentCount > 0) {
		return candidateCount > 0
	}
	if candidate.Enabled != current.Enabled {
		return candidate.Enabled
	}
	candidateCanonical := cloudLibraryPathIsCanonical(candidate)
	currentCanonical := cloudLibraryPathIsCanonical(current)
	if candidateCanonical != currentCanonical {
		return candidateCanonical
	}
	if !candidate.CreatedAt.Equal(current.CreatedAt) {
		return candidate.CreatedAt.After(current.CreatedAt)
	}
	return candidate.ID > current.ID
}

func cloudLibraryPathIsCanonical(lib model.Library) bool {
	info, ok := ParseCloudLibraryMount(lib.Path)
	if !ok {
		return false
	}
	return BuildCloudLibraryPath(info.Provider, info.ScanDir, info.DisplayDir) == strings.TrimSpace(lib.Path)
}

func mergeDisplayCloudLibraries(libs []model.Library) []model.Library {
	if len(libs) == 0 {
		return libs
	}
	localByKey := make(map[string]struct{}, len(libs))
	for _, lib := range libs {
		if _, ok := ParseCloudLibraryMount(lib.Path); ok || !lib.Enabled {
			continue
		}
		if key, ok := CloudLibraryMergeKey(lib); ok {
			localByKey[key] = struct{}{}
		}
	}
	out := make([]model.Library, 0, len(libs))
	for _, lib := range libs {
		if displayName, ok := CloudLibraryDisplayName(lib); ok && displayName != "" {
			lib.Name = displayName
			if key, ok := CloudLibraryMergeKey(lib); ok {
				if _, exists := localByKey[key]; exists {
					continue
				}
			}
		}
		out = append(out, lib)
	}
	return out
}

func CloudLibraryDisplayName(lib model.Library) (string, bool) {
	info, ok := ParseCloudLibraryMount(lib.Path)
	if !ok {
		return "", false
	}
	name := stripCloudProviderDisplayPrefix(strings.TrimSpace(lib.Name), info.Provider)
	dir := firstNonEmpty(info.DisplayDir, info.ScanDir)
	if name == "" || strings.EqualFold(name, CloudMountProviderLabel(info.Provider)) {
		if base := cloudMountDirBase(dir); base != "" {
			name = base
		}
	}
	if name == "" {
		name = CloudMountProviderLabel(info.Provider)
	}
	return name, true
}

func CloudLibraryMergeKey(lib model.Library) (string, bool) {
	name := strings.TrimSpace(lib.Name)
	if displayName, ok := CloudLibraryDisplayName(lib); ok {
		name = displayName
	}
	name = normalizeLibraryMergeName(name)
	if name == "" {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(lib.Type)) + "\x00" + name, true
}

package service

import (
	"context"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func MergedLibraryIDsForLibrary(ctx context.Context, repo *repository.Container, libraryID string) ([]string, error) {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" || repo == nil || repo.Library == nil {
		return []string{libraryID}, nil
	}
	lib, err := repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return []string{libraryID}, nil
	}
	libs, err := repo.Library.List(ctx)
	if err != nil {
		return nil, err
	}
	return MergedLibraryIDs(libs, *lib), nil
}

func MergedLibraryIDs(libs []model.Library, lib model.Library) []string {
	ids := []string{}
	seen := map[string]struct{}{}
	add := func(more ...string) {
		for _, id := range more {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	add(lib.ID)
	if rootAutoIDs := cloudRootAutoCategoryLibraryIDs(libs, lib); len(rootAutoIDs) > 0 {
		add(rootAutoIDs...)
	}
	key, ok := CloudLibraryMergeKey(lib)
	if !ok {
		return ids
	}
	_, libIsCloud := ParseCloudLibraryMount(lib.Path)
	for _, candidate := range libs {
		if candidate.ID == lib.ID || !candidate.Enabled {
			continue
		}
		candidateKey, ok := CloudLibraryMergeKey(candidate)
		if !ok || candidateKey != key {
			continue
		}
		_, candidateIsCloud := ParseCloudLibraryMount(candidate.Path)
		if !libIsCloud && !candidateIsCloud {
			continue
		}
		add(candidate.ID)
	}
	return ids
}

func cloudRootAutoCategoryLibraryIDs(libs []model.Library, lib model.Library) []string {
	mount, ok := ParseCloudLibraryMount(lib.Path)
	if !ok || !cloudRootMountNeedsAutoCategory(mount) {
		return nil
	}
	ids := make([]string, 0)
	for _, candidate := range libs {
		if candidate.ID == lib.ID || !candidate.Enabled || !CloudLibraryAutoCategory(candidate) {
			continue
		}
		info, ok := ParseCloudLibraryMount(candidate.Path)
		if ok && info.Provider == mount.Provider {
			ids = appendUniqueLibraryIDs(ids, candidate.ID)
		}
	}
	return ids
}

func ExpandMediaVisibilityForMergedCloudLibraries(ctx context.Context, repo *repository.Container, visibility MediaVisibility) MediaVisibility {
	if repo == nil || repo.Library == nil {
		return visibility
	}
	libs, err := repo.Library.List(ctx)
	if err != nil {
		return visibility
	}
	if len(visibility.AllowedLibraryIDs) > 0 {
		visibility.AllowedLibraryIDs = expandMergedLibraryIDsFromLibraries(libs, visibility.AllowedLibraryIDs)
	}
	if len(visibility.HiddenLibraryIDs) > 0 {
		visibility.HiddenLibraryIDs = expandMergedLibraryIDsFromLibraries(libs, visibility.HiddenLibraryIDs)
	}
	visibility.HiddenLibraryIDs = appendUniqueLibraryIDs(visibility.HiddenLibraryIDs, DeprecatedNativeCloudLibraryIDs(libs)...)
	return visibility
}

func expandMergedLibraryIDs(ctx context.Context, repo *repository.Container, ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	libs, err := repo.Library.List(ctx)
	if err != nil {
		return ids
	}
	return expandMergedLibraryIDsFromLibraries(libs, ids)
}

func expandMergedLibraryIDsFromLibraries(libs []model.Library, ids []string) []string {
	byID := make(map[string]model.Library, len(libs))
	for _, lib := range libs {
		byID[lib.ID] = lib
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		lib, ok := byID[id]
		if !ok {
			out = appendUniqueLibraryIDs(out, id)
			continue
		}
		for _, mergedID := range MergedLibraryIDs(libs, lib) {
			out = appendUniqueLibraryIDs(out, mergedID)
		}
	}
	return out
}

func DeprecatedNativeCloudLibraryIDs(libs []model.Library) []string {
	ids := make([]string, 0)
	for _, lib := range libs {
		info, ok := ParseCloudLibraryMount(lib.Path)
		if ok && IsDeprecatedNativeCloudProvider(info.Provider) {
			ids = appendUniqueLibraryIDs(ids, lib.ID)
		}
	}
	return ids
}

func appendUniqueLibraryIDs(ids []string, more ...string) []string {
	for _, id := range more {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		found := false
		for _, existing := range ids {
			if existing == id {
				found = true
				break
			}
		}
		if !found {
			ids = append(ids, id)
		}
	}
	return ids
}

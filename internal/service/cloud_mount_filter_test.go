package service

import (
	"slices"
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

func TestFilterDisplayCloudLibrariesPrefersPopulatedCanonicalDuplicate(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	now := time.Now()
	oldEmpty := model.Library{
		Base:    model.Base{ID: "old-empty", CreatedAt: now.Add(-time.Hour)},
		Name:    "OpenList · 国产剧",
		Path:    "cloud://openlist/%2F国产剧",
		Type:    "tv",
		Enabled: true,
	}
	newPopulated := model.Library{
		Base:    model.Base{ID: "new-populated", CreatedAt: now},
		Name:    "OpenList · 国产剧",
		Path:    BuildCloudLibraryPath("openlist", "/国产剧", "/国产剧"),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &oldEmpty); err != nil {
		t.Fatal(err)
	}
	if err := repos.Library.Create(t.Context(), &newPopulated); err != nil {
		t.Fatal(err)
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID: newPopulated.ID,
		Title:     "剧集",
		Path:      "cloud://openlist/国产剧/剧集.mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}

	filtered := FilterDisplayCloudLibraries(t.Context(), repos, []model.Library{oldEmpty, newPopulated})
	if len(filtered) != 1 || filtered[0].ID != newPopulated.ID {
		t.Fatalf("filtered = %#v, want only populated canonical duplicate", filtered)
	}

	scanner := NewScannerService(nil, zap.NewNop(), repos, nil, nil, nil)
	if conflict := scanner.shadowedCloudLibrary(t.Context(), &oldEmpty); conflict == nil || conflict.Library.ID != newPopulated.ID {
		t.Fatalf("old duplicate scan conflict = %#v, want populated canonical library", conflict)
	}
}

func TestFilterDisplayCloudLibrariesMergesCloudMountIntoExistingLibrary(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	local := model.Library{Name: "国产剧", Path: "/media/国产剧", Type: "tv", Enabled: true}
	cloud := model.Library{Name: "OpenList · 国产剧", Path: BuildCloudLibraryPath("openlist", "/国产剧", "/国产剧"), Type: "tv", Enabled: true}
	movieCloud := model.Library{Name: "OpenList · 国产剧", Path: BuildCloudLibraryPath("openlist", "/电影/国产剧", "/电影/国产剧"), Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&local, &cloud, &movieCloud} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}

	filtered := FilterDisplayCloudLibraries(t.Context(), repos, []model.Library{local, cloud, movieCloud})
	if got := libraryNames(filtered); !slices.Equal(got, []string{"国产剧", "国产剧"}) {
		t.Fatalf("filtered names = %#v, want local tv plus stripped movie cloud", got)
	}
	if filtered[0].ID != local.ID {
		t.Fatalf("first filtered library = %s, want existing local library %s", filtered[0].ID, local.ID)
	}
	if filtered[1].ID != movieCloud.ID {
		t.Fatalf("movie cloud library should stay separate when type differs: %#v", filtered)
	}

	merged := MergedLibraryIDs([]model.Library{local, cloud, movieCloud}, local)
	if !slices.Equal(merged, []string{local.ID, cloud.ID}) {
		t.Fatalf("merged ids = %#v, want local+same-type cloud", merged)
	}
}

func TestFilterDeprecatedNativeCloudLibrariesHidesPopulatedHistory(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	emptyQuark := model.Library{Name: "旧 Quark 空库", Path: "cloud://quark/0", Type: "movie", Enabled: true}
	populatedQuark := model.Library{Name: "旧 Quark 有数据", Path: "cloud://quark/archive", Type: "movie", Enabled: true}
	openList := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&emptyQuark, &populatedQuark, &openList} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID: populatedQuark.ID,
		Title:     "历史媒体",
		Path:      "cloud://quark/archive/movie.mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}

	filtered := FilterDeprecatedNativeCloudLibraries([]model.Library{emptyQuark, populatedQuark, openList})
	if got := libraryNames(filtered); !slices.Equal(got, []string{"OpenList"}) {
		t.Fatalf("filtered names = %#v, want only supported cloud libraries", got)
	}

	displayed := FilterDisplayCloudLibraries(t.Context(), repos, []model.Library{emptyQuark, populatedQuark, openList})
	if got := libraryNames(displayed); !slices.Equal(got, []string{"OpenList"}) {
		t.Fatalf("display names = %#v, want deprecated cloud hidden", got)
	}
}

func TestListMediaVisibleIncludesMergedCloudLibraryItems(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	local := model.Library{Name: "国产剧", Path: "/media/国产剧", Type: "tv", Enabled: true}
	cloud := model.Library{Name: "OpenList · 国产剧", Path: BuildCloudLibraryPath("openlist", "/国产剧", "/国产剧"), Type: "tv", Enabled: true}
	other := model.Library{Name: "欧美剧", Path: BuildCloudLibraryPath("openlist", "/欧美剧", "/欧美剧"), Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&local, &cloud, &other} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.DB.Create(&[]model.Media{
		{LibraryID: local.ID, Title: "本地剧", Path: "/media/国产剧/local.mkv"},
		{LibraryID: cloud.ID, Title: "云盘剧", Path: "cloud://openlist/国产剧/cloud.mkv"},
		{LibraryID: other.ID, Title: "其他剧", Path: "cloud://openlist/欧美剧/other.mkv"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	items, total, err := svc.ListMediaVisible(t.Context(), local.ID, 1, 20, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want merged local+cloud items", total)
	}
	if got := mediaTitles(items); !slices.Equal(got, []string{"云盘剧", "本地剧"}) {
		t.Fatalf("items = %#v, want local+cloud only", got)
	}
	if cloudItem := mediaByTitle(items, "云盘剧"); cloudItem == nil || cloudItem.DisplayLibraryID != local.ID {
		t.Fatalf("cloud item display library = %#v, want merged local library %s", cloudItem, local.ID)
	}

	items, total, err = svc.ListMediaVisible(t.Context(), local.ID, 1, 20, MediaVisibility{
		IncludeNSFW:       true,
		AllowedLibraryIDs: []string{local.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || !slices.Equal(mediaTitles(items), []string{"云盘剧", "本地剧"}) {
		t.Fatalf("profile-limited merged list total=%d items=%#v", total, mediaTitles(items))
	}

	searchItems, err := svc.SearchMediaVisible(t.Context(), "剧", 20, MediaVisibility{
		IncludeNSFW:       true,
		AllowedLibraryIDs: []string{local.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mediaTitles(searchItems); !slices.Equal(got, []string{"云盘剧", "本地剧"}) {
		t.Fatalf("profile-limited merged search items=%#v, want local+hidden cloud", got)
	}
	if cloudItem := mediaByTitle(searchItems, "云盘剧"); cloudItem == nil || cloudItem.DisplayLibraryID != local.ID {
		t.Fatalf("search cloud item display library = %#v, want merged local library %s", cloudItem, local.ID)
	}
}

func TestListMediaVisibleUsesSpecificCloudChildLibraryAsDisplayTarget(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	root := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "tv", Enabled: true}
	child := model.Library{Name: "OpenList · 国产剧", Path: BuildCloudLibraryPath("openlist", "/国产剧", "/国产剧"), Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&root, &child} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID: root.ID,
		Title:     "折腰",
		Path:      "cloud://openlist/国产剧/折腰 (2025)/Season 1/折腰.S01E01.mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	items, total, err := svc.ListMediaVisible(t.Context(), root.ID, 1, 20, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("items total=%d len=%d, want one root cloud item", total, len(items))
	}
	if items[0].DisplayLibraryID != child.ID {
		t.Fatalf("display library = %q, want child cloud library %q", items[0].DisplayLibraryID, child.ID)
	}
	if items[0].DisplayLibraryPath != child.Path {
		t.Fatalf("display library path = %q, want %q", items[0].DisplayLibraryPath, child.Path)
	}
}

func TestStartAllCloudLibraryScansIncludesMergedCloudMounts(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	local := model.Library{Name: "国产剧", Path: "/media/国产剧", Type: "tv", Enabled: true}
	cloud := model.Library{Name: "OpenList · 国产剧", Path: BuildCloudLibraryPath("openlist", "/国产剧", "/国产剧"), Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&local, &cloud} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)

	statuses, err := scanner.StartAllCloudLibraryScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].LibraryID != cloud.ID {
		t.Fatalf("scan-all statuses = %#v, want merged cloud library queued", statuses)
	}
}

func TestAutoCategoryCloudLibrariesDoNotShadowRootOrScan(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	root := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	auto := model.Library{Name: "欧美剧", Path: BuildCloudAutoCategoryLibraryPath("openlist", "电视剧/欧美剧"), Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&root, &auto} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}

	libs, err := repos.Library.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if shadow := CloudLibraryShadowed(libs, root); shadow != nil {
		t.Fatalf("auto category should not shadow root scan: %#v", shadow)
	}
	display := FilterDisplayCloudLibraries(t.Context(), repos, libs)
	if len(display) != 1 || display[0].ID != root.ID {
		t.Fatalf("display libraries = %#v, want only user-mounted root", display)
	}
	scannable := FilterScannableCloudLibraries(t.Context(), repos, libs)
	if len(scannable) != 1 || scannable[0].ID != root.ID {
		t.Fatalf("scannable libraries = %#v, want only root", scannable)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)
	statuses, err := scanner.StartAllCloudLibraryScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].LibraryID != root.ID {
		t.Fatalf("scan-all statuses = %#v, want only root queued", statuses)
	}
}

func TestRootCloudLibraryIncludesHiddenAutoCategoryMedia(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	root := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	auto := model.Library{Name: "欧美剧", Path: BuildCloudAutoCategoryLibraryPath("openlist", "电视剧/欧美剧"), Type: "tv", Enabled: true}
	for _, lib := range []*model.Library{&root, &auto} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	if err := repos.DB.Create(&model.Media{
		LibraryID: auto.ID,
		Title:     "The Show",
		Path:      "cloud://openlist/电视剧/欧美剧/The Show/The.Show.S01E01.mkv",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewMediaService(&config.Config{}, zap.NewNop(), repos)

	items, total, err := svc.ListMediaVisible(t.Context(), root.ID, 1, 20, MediaVisibility{IncludeNSFW: true})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("root cloud items total=%d len=%d, want hidden auto-category media", total, len(items))
	}
	if items[0].LibraryName != root.Name || items[0].LibraryPath != root.Path {
		t.Fatalf("media library metadata = (%q, %q), want user-mounted root", items[0].LibraryName, items[0].LibraryPath)
	}
	if items[0].DisplayLibraryID != root.ID || items[0].DisplayLibraryPath != root.Path {
		t.Fatalf("display library = (%q, %q), want user-mounted root", items[0].DisplayLibraryID, items[0].DisplayLibraryPath)
	}
}

func TestStartAllCloudLibraryScansSkipsDeprecatedQuarkMounts(t *testing.T) {
	db := newServiceTestDB(t, &model.Library{}, &model.Media{})
	repos := repository.New(db)
	quark := model.Library{Name: "旧 Quark", Path: "cloud://quark/0", Type: "movie", Enabled: true}
	openList := model.Library{Name: "OpenList", Path: "cloud://openlist", Type: "movie", Enabled: true}
	for _, lib := range []*model.Library{&quark, &openList} {
		if err := repos.Library.Create(t.Context(), lib); err != nil {
			t.Fatal(err)
		}
	}
	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, nil)

	statuses, err := scanner.StartAllCloudLibraryScans()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Provider != "openlist" {
		t.Fatalf("scan-all statuses = %#v, want only openlist", statuses)
	}
}

func libraryNames(libs []model.Library) []string {
	out := make([]string, 0, len(libs))
	for _, lib := range libs {
		out = append(out, lib.Name)
	}
	return out
}

func mediaTitles(items []model.Media) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Title)
	}
	slices.Sort(out)
	return out
}

func mediaByTitle(items []model.Media, title string) *model.Media {
	for i := range items {
		if items[i].Title == title {
			return &items[i]
		}
	}
	return nil
}

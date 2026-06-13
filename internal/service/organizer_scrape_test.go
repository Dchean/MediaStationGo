package service

import (
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
)

func TestOrganizeDirectoryScanAndScrapeAfter(t *testing.T) {
	scraper, repos, closeServer := newTestScraper(t)
	defer closeServer()
	if err := repos.DB.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	src := filepath.Join(root, "downloads")
	dest := filepath.Join(root, "media")
	sourceFile := filepath.Join(src, "Spy.x.Family.S01E01.2022.1080p.mkv")
	writeOrgFile(t, sourceFile, "episode")

	lib := model.Library{
		Name:    "剧集",
		Path:    filepath.Join(dest, "电视剧"),
		Type:    "tv",
		Enabled: true,
	}
	if err := repos.Library.Create(t.Context(), &lib); err != nil {
		t.Fatal(err)
	}

	organizer := NewOrganizerService(&config.Config{}, zap.NewNop(), repos)
	res, err := organizer.OrganizeDirectory(t.Context(), OrganizeOptions{
		SourcePath:   src,
		DestPath:     dest,
		TransferMode: TransferCopy,
		MediaType:    "tv",
	})
	if err != nil {
		t.Fatalf("organize directory: %v", err)
	}
	if res.Organized != 1 {
		t.Fatalf("organized = %d, want 1", res.Organized)
	}

	scanner := NewScannerService(&config.Config{}, zap.NewNop(), repos, NewHub(zap.NewNop()), nil, scraper)
	scans, scrapes := scanner.ScanAndScrapeLibrariesForPath(t.Context(), res.DestPath, "", true)
	if len(scans) != 1 || scans[0].Added != 1 {
		t.Fatalf("scans = %#v, want one scan with added=1", scans)
	}
	if len(scrapes) != 1 || scrapes[0].Matched != 1 || scrapes[0].Error != "" || scrapes[0].Skipped {
		t.Fatalf("scrapes = %#v, want one successful matched scrape", scrapes)
	}

	var media model.Media
	if err := repos.DB.Where("path LIKE ?", "%Spy Family - S01E01.mkv").First(&media).Error; err != nil {
		t.Fatal(err)
	}
	if media.ScrapeStatus != "matched" || media.TMDbID != 12345 {
		t.Fatalf("media scrape status=%q tmdb=%d, want matched/12345", media.ScrapeStatus, media.TMDbID)
	}
	if _, err := os.Stat(media.Path); err != nil {
		t.Fatalf("organized file missing at %q: %v", media.Path, err)
	}
}

func TestOrganizeScrapeAfterEnabledDefaultsOn(t *testing.T) {
	if !OrganizeScrapeAfterEnabled(t.Context(), nil) {
		t.Fatalf("organize scrape-after should default on without a repo")
	}
	repos := newOrganizerTestRepo(t)
	if !OrganizeScrapeAfterEnabled(t.Context(), repos) {
		t.Fatalf("organize scrape-after should default on when setting is absent")
	}
	if err := repos.Setting.Set(t.Context(), "organize.scrape_after", "false"); err != nil {
		t.Fatal(err)
	}
	if OrganizeScrapeAfterEnabled(t.Context(), repos) {
		t.Fatalf("explicit organize.scrape_after=false should be respected")
	}
}

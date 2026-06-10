package service

import (
	"context"
	"encoding/xml"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ShukeBta/MediaStationGo/internal/service/cloud"
)

type cloudSidecarSet struct {
	nfoByName   map[string]string
	nfoByBase   map[string]string
	imageByBase map[string]string
}

func newCloudSidecarSet(typ string, entries []cloud.FileEntry) cloudSidecarSet {
	set := cloudSidecarSet{
		nfoByName:   make(map[string]string),
		nfoByBase:   make(map[string]string),
		imageByBase: make(map[string]string),
	}
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		ref := cloudEntryRef(typ, entry.ID, entry.PickCode)
		if ref == "" {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		ext := strings.ToLower(filepath.Ext(name))
		base := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, ext)))
		if name == "" || base == "" {
			continue
		}
		switch ext {
		case ".nfo":
			set.nfoByName[strings.ToLower(name)] = ref
			set.nfoByBase[base] = ref
		case ".jpg", ".jpeg", ".png", ".webp":
			set.imageByBase[base] = ref
		}
	}
	return set
}

func (s *ScannerService) cloudDirectoryMetadata(ctx context.Context, typ, displayDir string, sidecars cloudSidecarSet, inherited *LocalMetadata) *LocalMetadata {
	meta := cloneLocalMetadata(inherited)
	for _, name := range cloudShowNFOCandidates(displayDir) {
		ref := sidecars.nfoByName[strings.ToLower(name)]
		if ref == "" {
			ref = sidecars.nfoByBase[strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))]
		}
		if ref == "" {
			continue
		}
		if local, _, err := s.readCloudNFO(ctx, typ, ref, true); err == nil && local != nil {
			meta = mergeCloudMetadata(meta, local)
			break
		}
	}
	meta = applyCloudDirectoryArtwork(typ, sidecars, meta)
	if !cloudMetadataUseful(meta) {
		return nil
	}
	return meta
}

func (s *ScannerService) cloudFileMetadata(ctx context.Context, typ, displayPath, fileName string, sidecars cloudSidecarSet, inherited *LocalMetadata, seriesLike bool) *LocalMetadata {
	season, episode := ParseEpisode(displayPath)
	seriesLike = seriesLike || season > 0 || episode > 0
	meta := cloneLocalMetadata(inherited)
	base := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(fileName, filepath.Ext(fileName))))
	if ref := sidecars.nfoByBase[base]; ref != "" {
		if local, doc, err := s.readCloudNFO(ctx, typ, ref, seriesLike); err == nil && local != nil {
			if seriesLike && doc != nil {
				if meta == nil {
					meta = &LocalMetadata{}
				}
				mergeEpisodeMetadata(meta, local, doc)
				meta.HasNFO = true
			} else {
				meta = mergeCloudMetadata(meta, local)
			}
		}
	}
	meta = applyCloudFileArtwork(typ, sidecars, base, meta)
	if !cloudMetadataUseful(meta) {
		return nil
	}
	return meta
}

func (s *ScannerService) readCloudNFO(ctx context.Context, typ, ref string, seriesLike bool) (*LocalMetadata, *nfoDocument, error) {
	if s.storage == nil {
		return nil, nil, nil
	}
	body, err := s.storage.CloudReadText(ctx, typ, ref, 512<<10)
	if err != nil {
		return nil, nil, err
	}
	var doc nfoDocument
	if err := xml.Unmarshal([]byte(body), &doc); err != nil {
		return nil, nil, err
	}
	meta := metadataFromDoc(&doc, "", seriesLike)
	return meta, &doc, nil
}

func applyCloudDirectoryArtwork(typ string, sidecars cloudSidecarSet, meta *LocalMetadata) *LocalMetadata {
	if meta == nil {
		meta = &LocalMetadata{}
	}
	if meta.PosterURL == "" {
		if ref := firstCloudImageRef(sidecars, "poster", "folder", "cover", "show", "tvshow"); ref != "" {
			meta.PosterURL = cloudPlaybackURL(typ, ref)
			meta.HasArtwork = true
		}
	}
	if meta.BackdropURL == "" {
		if ref := firstCloudImageRef(sidecars, "fanart", "backdrop", "background", "landscape"); ref != "" {
			meta.BackdropURL = cloudPlaybackURL(typ, ref)
			meta.HasArtwork = true
		}
	}
	return meta
}

func applyCloudFileArtwork(typ string, sidecars cloudSidecarSet, base string, meta *LocalMetadata) *LocalMetadata {
	if meta == nil {
		meta = &LocalMetadata{}
	}
	if meta.PosterURL == "" {
		if ref := firstCloudImageRef(sidecars,
			base+"-poster", base+".poster", base+"-cover", base+".cover", base+"-thumb", base+".thumb",
			"poster", "folder", "cover", "movie", "show", "thumb",
		); ref != "" {
			meta.PosterURL = cloudPlaybackURL(typ, ref)
			meta.HasArtwork = true
		}
	}
	if meta.BackdropURL == "" {
		if ref := firstCloudImageRef(sidecars,
			base+"-fanart", base+".fanart", base+"-backdrop", base+".backdrop", base+"-background", base+".background",
			"fanart", "backdrop", "background", "landscape",
		); ref != "" {
			meta.BackdropURL = cloudPlaybackURL(typ, ref)
			meta.HasArtwork = true
		}
	}
	return meta
}

func firstCloudImageRef(sidecars cloudSidecarSet, names ...string) string {
	for _, name := range names {
		if ref := sidecars.imageByBase[strings.ToLower(strings.TrimSpace(name))]; ref != "" {
			return ref
		}
	}
	return ""
}

func cloudShowNFOCandidates(displayDir string) []string {
	names := []string{"tvshow.nfo", "series.nfo", "show.nfo"}
	base := strings.TrimSpace(pathBaseSlash(displayDir))
	if base != "" {
		names = append(names, base+".nfo")
	}
	return names
}

func mergeCloudMetadata(dst, src *LocalMetadata) *LocalMetadata {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneLocalMetadata(src)
	}
	if src.Title != "" {
		dst.Title = src.Title
	}
	if src.OriginalName != "" {
		dst.OriginalName = src.OriginalName
	}
	if src.AdultCode != "" {
		dst.AdultCode = src.AdultCode
	}
	if src.Year > 0 {
		dst.Year = src.Year
	}
	if src.Overview != "" {
		dst.Overview = src.Overview
	}
	if src.Rating > 0 {
		dst.Rating = src.Rating
	}
	if src.PosterURL != "" {
		dst.PosterURL = src.PosterURL
	}
	if src.BackdropURL != "" {
		dst.BackdropURL = src.BackdropURL
	}
	if src.TMDbID > 0 {
		dst.TMDbID = src.TMDbID
	}
	if src.SeasonNum > 0 {
		dst.SeasonNum = src.SeasonNum
	}
	if src.EpisodeNum > 0 {
		dst.EpisodeNum = src.EpisodeNum
	}
	if src.Genres != "" {
		dst.Genres = src.Genres
	}
	if src.Countries != "" {
		dst.Countries = src.Countries
	}
	if src.Languages != "" {
		dst.Languages = src.Languages
	}
	dst.NSFW = dst.NSFW || src.NSFW
	dst.HasNFO = dst.HasNFO || src.HasNFO
	dst.HasArtwork = dst.HasArtwork || src.HasArtwork
	return dst
}

func cloneLocalMetadata(src *LocalMetadata) *LocalMetadata {
	if src == nil {
		return nil
	}
	cp := *src
	return &cp
}

func cloudMetadataUseful(meta *LocalMetadata) bool {
	return meta != nil && (meta.HasNFO || meta.HasArtwork || localHasDescriptiveMetadata(meta))
}

func cloudPlaybackURL(typ, ref string) string {
	return "/api/cloud/play/" + typ + "?ref=" + url.QueryEscape(ref)
}

func joinCloudDisplayPath(parent, child string) string {
	parent = strings.Trim(strings.ReplaceAll(strings.TrimSpace(parent), "\\", "/"), "/")
	child = strings.Trim(strings.ReplaceAll(strings.TrimSpace(child), "\\", "/"), "/")
	switch {
	case parent == "":
		return child
	case child == "":
		return parent
	default:
		return parent + "/" + child
	}
}

func pathBaseSlash(value string) string {
	value = strings.Trim(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/")
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	return parts[len(parts)-1]
}

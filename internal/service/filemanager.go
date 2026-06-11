// Package service — server-side file browser and safe local file operations.
//
// FileManagerService exposes a strict, allow-listed view of the server's
// filesystem. The design follows MoviePilot's StorageChain boundary: callers
// work with file-item-like records, while this service owns path validation,
// local storage operations, and mutation safety.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// FileManagerService browses and mutates the server-side filesystem.
type FileManagerService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewFileManagerService is the constructor.
func NewFileManagerService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *FileManagerService {
	return &FileManagerService{cfg: cfg, log: log, repo: repo}
}

// Entry is one file or directory shown in the browser.
type Entry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Ext      string `json:"ext,omitempty"`
}

// Listing describes the contents of a directory plus navigation hints.
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent,omitempty"`
	Roots   []Root  `json:"roots,omitempty"`
	Entries []Entry `json:"entries"`
}

// Root is the entry-point label shown when no path is given.
type Root struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

type FileOperationResult struct {
	Path string `json:"path"`
}

// ErrPathOutOfBounds is returned when path falls outside every allowed root.
var ErrPathOutOfBounds = errors.New("path is outside the allowed roots")

// ErrRootMutation protects configured roots such as /media and /downloads.
var ErrRootMutation = errors.New("refusing to mutate an allowed root")

// List enumerates a directory under one of the allowed roots, returning up to
// maxEntries items sorted by (dir-first, path). Recursive listing is capped by
// maxEntries to avoid accidentally walking huge NAS trees from the UI.
func (s *FileManagerService) List(path string, maxEntries int, recursive ...bool) (*Listing, error) {
	if maxEntries <= 0 || maxEntries > 5000 {
		maxEntries = 1000
	}
	roots, rootList, err := s.allowedRootList()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return &Listing{Path: "", Roots: rootList}, nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !s.withinAllowed(abs, roots) {
		return nil, ErrPathOutOfBounds
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return &Listing{Path: abs, Roots: rootList, Entries: []Entry{s.entryFromInfo(abs, info)}}, nil
	}

	out := &Listing{Path: abs, Roots: rootList, Entries: []Entry{}}
	parent := filepath.Dir(abs)
	if parent != abs && s.withinAllowed(parent, roots) {
		out.Parent = parent
	}
	if len(recursive) > 0 && recursive[0] {
		if err := s.walkEntries(abs, maxEntries, out); err != nil {
			return nil, err
		}
		sortFileEntries(out.Entries)
		return out, nil
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if len(out.Entries) >= maxEntries {
			break
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(abs, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		out.Entries = append(out.Entries, s.entryFromInfo(full, info))
	}
	sortFileEntries(out.Entries)
	return out, nil
}

func (s *FileManagerService) CreateFolder(parent, name string) (*FileOperationResult, error) {
	parentPath, roots, err := s.requireAllowedPath(parent, false)
	if err != nil {
		return nil, err
	}
	cleanName := sanitizeFilename(name)
	if strings.TrimSpace(name) == "" || cleanName == "" || strings.ContainsAny(name, `/\`) {
		return nil, errors.New("invalid folder name")
	}
	dst := filepath.Join(parentPath, cleanName)
	if !s.withinAllowed(dst, roots) {
		return nil, ErrPathOutOfBounds
	}
	if err := os.MkdirAll(dst, 0o755); err != nil { // #nosec G301 -- user-created media directories must remain readable by NAS/player users.
		return nil, err
	}
	return &FileOperationResult{Path: dst}, nil
}

func (s *FileManagerService) Rename(path, name string) (*FileOperationResult, error) {
	src, roots, err := s.requireAllowedPath(path, true)
	if err != nil {
		return nil, err
	}
	cleanName := sanitizeFilename(name)
	if strings.TrimSpace(name) == "" || cleanName == "" || strings.ContainsAny(name, `/\`) {
		return nil, errors.New("invalid name")
	}
	dst := filepath.Join(filepath.Dir(src), cleanName)
	if !s.withinAllowed(dst, roots) {
		return nil, ErrPathOutOfBounds
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("target already exists: %s", dst)
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, err
	}
	return &FileOperationResult{Path: dst}, nil
}

func (s *FileManagerService) Delete(path string) error {
	target, _, err := s.requireAllowedPath(path, true)
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}

func (s *FileManagerService) Transfer(sourcePath, destDir string, mode TransferMode) (*FileOperationResult, error) {
	src, roots, err := s.requireAllowedPath(sourcePath, true)
	if err != nil {
		return nil, err
	}
	dstDir, _, err := s.requireAllowedPath(destDir, false)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, errors.New("directory transfer is not supported yet")
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if !s.withinAllowed(dst, roots) {
		return nil, ErrPathOutOfBounds
	}
	if _, err := os.Stat(dst); err == nil {
		return nil, fmt.Errorf("target already exists: %s", dst)
	}
	if mode == "" {
		mode = TransferCopy
	}
	if err := transferFile(src, dst, mode); err != nil {
		return nil, err
	}
	return &FileOperationResult{Path: dst}, nil
}

func (s *FileManagerService) walkEntries(root string, maxEntries int, out *Listing) error {
	count := 0
	return filepath.WalkDir(root, func(full string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if full == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if count >= maxEntries {
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out.Entries = append(out.Entries, s.entryFromInfo(full, info))
		count++
		return nil
	})
}

func (s *FileManagerService) requireAllowedPath(path string, forbidRoot bool) (string, map[string]string, error) {
	roots, _, err := s.allowedRootList()
	if err != nil {
		return "", nil, err
	}
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", nil, err
	}
	if !s.withinAllowed(abs, roots) {
		return "", nil, ErrPathOutOfBounds
	}
	if forbidRoot && s.isAllowedRoot(abs, roots) {
		return "", nil, ErrRootMutation
	}
	return abs, roots, nil
}

func (s *FileManagerService) entryFromInfo(path string, info os.FileInfo) Entry {
	return Entry{
		Name:     filepath.Base(path),
		Path:     path,
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		Modified: info.ModTime().Unix(),
		Ext:      strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
}

func sortFileEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Path) < strings.ToLower(entries[j].Path)
	})
}

// allowedRootList returns the union of configured storage roots as
// label → absolute-path plus a sorted UI list.
func (s *FileManagerService) allowedRootList() (map[string]string, []Root, error) {
	roots, err := s.allowedRoots()
	if err != nil {
		return nil, nil, err
	}
	rootList := make([]Root, 0, len(roots))
	seen := map[string]struct{}{}
	for label, p := range roots {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		rootList = append(rootList, Root{Label: label, Path: p})
	}
	sort.Slice(rootList, func(i, j int) bool { return rootList[i].Label < rootList[j].Label })
	return roots, rootList, nil
}

func (s *FileManagerService) allowedRoots() (map[string]string, error) {
	roots := map[string]string{}
	add := func(label, p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if _, err := os.Stat(abs); err != nil {
			return
		}
		roots[label] = abs
	}
	add("data", s.cfg.App.DataDir)
	add("cache", s.cfg.Cache.CacheDir)
	add("movies", s.cfg.Media.MoviesDir)
	add("tv", s.cfg.Media.TVDir)
	add("anime", s.cfg.Media.AnimeDir)
	add("downloads", envOrDefault("MEDIASTATION_DOWNLOAD_CONTAINER_DIR", "/downloads"))
	add("media", envOrDefault("MEDIASTATION_MEDIA_CONTAINER_DIR", "/media"))
	if s.repo != nil && s.repo.Setting != nil {
		addSetting := func(label, key string) {
			if value, err := s.repo.Setting.Get(context.Background(), key); err == nil {
				add(label, strings.TrimSpace(value))
			}
		}
		addSetting("organize-source", "organize.source_dir")
		addSetting("organize-target", "organize.target_dir")
		addSetting("qb-savepath", "qbittorrent.savepath")
	}
	if s.repo != nil && s.repo.Library != nil {
		libs, err := s.repo.Library.List(context.Background())
		if err == nil {
			for _, l := range libs {
				add("library:"+l.Name, l.Path)
			}
		}
	}
	return roots, nil
}

func (s *FileManagerService) withinAllowed(path string, roots map[string]string) bool {
	for _, root := range roots {
		if pathWithin(path, root) {
			return true
		}
	}
	return false
}

func (s *FileManagerService) isAllowedRoot(path string, roots map[string]string) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		if strings.EqualFold(path, filepath.Clean(root)) {
			return true
		}
	}
	return false
}

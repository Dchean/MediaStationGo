// Package service — filesystem watcher.
//
// WatcherService observes every enabled library root with fsnotify and
// debounces incoming events into per-library re-scans. New / renamed
// files become Media rows; deletes remove them.
//
// The watcher runs in the background and is started after migrations
// complete. It survives library add / delete via Refresh().
package service

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// WatcherService is a thin orchestrator on top of fsnotify.
type WatcherService struct {
	log     *zap.Logger
	repo    *repository.Container
	scanner *ScannerService

	mu      sync.Mutex
	watcher *fsnotify.Watcher
	watched map[string]string // dir -> libraryID
	pending map[string]time.Time
	stop    chan struct{}
}

// NewWatcherService is the constructor.
func NewWatcherService(log *zap.Logger, repo *repository.Container, scanner *ScannerService) *WatcherService {
	return &WatcherService{
		log:     log,
		repo:    repo,
		scanner: scanner,
		watched: make(map[string]string),
		pending: make(map[string]time.Time),
		stop:    make(chan struct{}),
	}
}

// Start initialises the underlying fsnotify watcher and registers every
// library root currently in the database.
func (w *WatcherService) Start(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = fw
	if err := w.Refresh(ctx); err != nil {
		w.log.Warn("watcher refresh failed", zap.Error(err))
	}
	go w.loop(ctx)
	go w.debouncer(ctx)
	return nil
}

// Stop tears down the watcher (called on graceful shutdown).
func (w *WatcherService) Stop() {
	close(w.stop)
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

// Refresh reads the library list and adjusts the set of watched
// directories. Idempotent — safe to call after every CRUD.
func (w *WatcherService) Refresh(ctx context.Context) error {
	libs, err := w.repo.Library.List(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	current := make(map[string]string)
	for _, l := range libs {
		if !l.Enabled {
			continue
		}
		current[l.Path] = l.ID
	}
	// Remove disappeared paths.
	for path := range w.watched {
		if _, ok := current[path]; !ok {
			_ = w.watcher.Remove(path)
			delete(w.watched, path)
		}
	}
	// Add new ones (top-level only — fsnotify is non-recursive).
	for path, id := range current {
		if _, ok := w.watched[path]; ok {
			continue
		}
		if err := w.watcher.Add(path); err != nil {
			w.log.Warn("watch add failed", zap.String("path", path), zap.Error(err))
			continue
		}
		w.watched[path] = id
	}
	return nil
}

// loop drains fsnotify events and pushes the affected library into the
// pending map. The actual rescan happens in the debouncer goroutine.
func (w *WatcherService) loop(ctx context.Context) {
	if w.watcher == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Write) == 0 {
				continue
			}
			lib := w.findLibrary(ev.Name)
			if lib == "" {
				continue
			}
			w.mu.Lock()
			w.pending[lib] = time.Now()
			w.mu.Unlock()
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn("watcher error", zap.Error(err))
		}
	}
}

// findLibrary maps a path back to the watching library ID, taking the
// shortest matching prefix.
func (w *WatcherService) findLibrary(path string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	dir := filepath.Dir(path)
	for {
		if id, ok := w.watched[dir]; ok {
			return id
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// debouncer drains the pending set every 5 s and triggers a rescan per
// library. Coalescing avoids storming the disk on bulk operations
// (mass-rename, large copies).
func (w *WatcherService) debouncer(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-t.C:
		}
		w.mu.Lock()
		due := make([]string, 0, len(w.pending))
		now := time.Now()
		for id, ts := range w.pending {
			if now.Sub(ts) >= 5*time.Second {
				due = append(due, id)
				delete(w.pending, id)
			}
		}
		w.mu.Unlock()
		for _, id := range due {
			w.log.Info("watcher triggered rescan", zap.String("library_id", id))
			if _, err := w.scanner.ScanLibrary(ctx, id); err != nil {
				w.log.Warn("watcher rescan failed", zap.Error(err))
			}
		}
	}
}

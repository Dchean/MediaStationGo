// Package service — generic on-disk cleanup helper used by the
// scheduler. Public so handlers can call it for "purge transcode cache
// now" buttons.
package service

import (
	"os"
	"path/filepath"
	"time"
)

// walkAndPrune recursively deletes every file under root whose mtime is
// older than cutoff. Empty directories left behind are removed too.
// Best-effort: per-file errors are ignored so a single permission denial
// doesn't abort the cleanup.
func walkAndPrune(root string, cutoff time.Time) error {
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); err != nil {
		return nil // nothing to clean
	}
	dirs := []string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if path != root {
				dirs = append(dirs, path)
			}
			return nil
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(path) // #nosec G122 -- cache pruning is best-effort under the configured cache root.
		}
		return nil
	})
	// Remove emptied directories from deepest to shallowest.
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	return nil
}

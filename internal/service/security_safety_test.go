package service

import (
	"path/filepath"
	"testing"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

func TestValidateSTRMProxyURLBlocksPrivateTargets(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/video.mkv",
		"http://192.168.1.2/video.mkv",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	}
	for _, raw := range blocked {
		if _, err := validateSTRMProxyURL(raw); err == nil {
			t.Fatalf("validateSTRMProxyURL(%q) allowed unsafe target", raw)
		}
	}
}

func TestValidateSTRMProxyURLAllowsPublicHTTP(t *testing.T) {
	for _, raw := range []string{"https://example.com/video.mkv", "http://8.8.8.8/video.mkv"} {
		if _, err := validateSTRMProxyURL(raw); err != nil {
			t.Fatalf("validateSTRMProxyURL(%q) = %v, want nil", raw, err)
		}
	}
}

func TestBackupFilePathRejectsTraversal(t *testing.T) {
	svc := &BackupService{cfg: &config.Config{}}
	svc.cfg.App.DataDir = t.TempDir()
	for _, name := range []string{"../evil.db", `..\evil.db`, "nested/evil.db", "evil.sqlite"} {
		if _, err := svc.backupFilePath(name); err == nil {
			t.Fatalf("backupFilePath(%q) allowed traversal or non-backup file", name)
		}
	}
	path, err := svc.backupFilePath("mediastation_20260611_010203.db")
	if err != nil {
		t.Fatalf("backupFilePath(valid) = %v", err)
	}
	if filepath.Dir(path) != filepath.Join(svc.cfg.App.DataDir, "backups") {
		t.Fatalf("backupFilePath(valid) dir = %q", filepath.Dir(path))
	}
}

func TestSafeZipTargetRejectsZipSlip(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../evil.exe", `..\evil.exe`, "/tmp/evil.exe"} {
		if _, err := safeZipTarget(root, name); err == nil {
			t.Fatalf("safeZipTarget(%q) allowed zip-slip path", name)
		}
	}
	if _, err := safeZipTarget(root, "ffmpeg/bin/ffmpeg.exe"); err != nil {
		t.Fatalf("safeZipTarget(valid) = %v", err)
	}
}

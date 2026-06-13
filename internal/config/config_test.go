package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDefaults asserts that a Load on a clean working directory yields
// usable, normalized defaults.
func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.App.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.App.Port)
	}
	if cfg.App.MaxCPUThreads != 2 {
		t.Fatalf("expected default MaxCPUThreads 2, got %d", cfg.App.MaxCPUThreads)
	}
	if cfg.Database.DBPath == "" {
		t.Fatalf("expected non-empty DBPath")
	}
	if cfg.Database.MaxOpenConns != defaultDatabaseMaxOpenConns {
		t.Fatalf("expected default MaxOpenConns %d, got %d", defaultDatabaseMaxOpenConns, cfg.Database.MaxOpenConns)
	}
	if cfg.Database.MaxIdleConns != defaultDatabaseMaxIdleConns {
		t.Fatalf("expected default MaxIdleConns %d, got %d", defaultDatabaseMaxIdleConns, cfg.Database.MaxIdleConns)
	}
	if cfg.Secrets.JWTSecret == "" {
		t.Fatalf("expected auto-generated JWT secret")
	}
	if !cfg.Organizer.SmartClassify {
		t.Fatalf("expected organizer smart classify enabled by default")
	}
	// Re-loading must reuse the persisted secret on disk.
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("second Load() error: %v", err)
	}
	if cfg.Secrets.JWTSecret != cfg2.Secrets.JWTSecret {
		t.Fatalf("expected JWT secret to persist across Load() calls")
	}
	if _, err := os.Stat(filepath.Join(cfg.App.DataDir, ".jwt_secret")); err != nil {
		t.Fatalf("expected jwt secret file: %v", err)
	}
}

// TestEnvOverride checks that MEDIASTATION_* env vars override the defaults.
func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	t.Setenv("MEDIASTATION_APP_PORT", "9090")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.App.Port != 9090 {
		t.Fatalf("expected port 9090 from env, got %d", cfg.App.Port)
	}
}

func TestLoadAllowsSingleConnectionDatabaseConfig(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.WriteFile("config.yaml", []byte("database:\n  max_open_conns: 1\n  max_idle_conns: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Database.MaxOpenConns != defaultDatabaseMaxOpenConns {
		t.Fatalf("expected MaxOpenConns=1 to remain at safe default %d, got %d", defaultDatabaseMaxOpenConns, cfg.Database.MaxOpenConns)
	}
}

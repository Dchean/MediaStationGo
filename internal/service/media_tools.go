package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func executableCandidates(configured, name string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 12)
	add := func(path string) {
		path = strings.Trim(path, `" `)
		if path == "" {
			return
		}
		if !strings.EqualFold(filepath.Ext(path), ".exe") && runtime.GOOS == "windows" && !strings.ContainsAny(path, `\/`) {
			path += ".exe"
		}
		if resolved, err := exec.LookPath(path); err == nil {
			path = resolved
		}
		if stat, err := os.Stat(path); err != nil || stat.IsDir() {
			return
		}
		clean, err := filepath.Abs(path)
		if err == nil {
			path = clean
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}

	if configured != "" {
		add(configured)
	}
	add(name)

	for _, path := range localExecutableCandidates(name) {
		add(path)
	}
	return out
}

func resolveLocalExecutable(configured, name string) (string, error) {
	for _, path := range executableCandidates(configured, name) {
		return path, nil
	}
	return "", fmt.Errorf("%s not found in PATH or common local app directories", name)
}

func localExecutableCandidates(name string) []string {
	exe := name
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(exe), ".exe") {
		exe += ".exe"
	}

	out := make([]string, 0, 24)
	add := func(path string) {
		if path != "" {
			out = append(out, path)
		}
	}
	addGlob := func(pattern string) {
		matches, _ := filepath.Glob(pattern)
		out = append(out, matches...)
	}

	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "tools", "ffmpeg", "bin", exe))
		add(filepath.Join(wd, "bin", exe))
	}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		add(filepath.Join(exeDir, "tools", "ffmpeg", "bin", exe))
		add(filepath.Join(exeDir, exe))
	}

	appData := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")
	userProfile := os.Getenv("USERPROFILE")
	programData := os.Getenv("ProgramData")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")

	add(filepath.Join(appData, "bilibili", "ffmpeg", exe))
	add(filepath.Join(userProfile, "scoop", "shims", exe))
	add(filepath.Join(userProfile, "scoop", "apps", "ffmpeg", "current", "bin", exe))
	add(filepath.Join(programData, "chocolatey", "bin", exe))
	add(filepath.Join("C:\\", "ffmpeg", "bin", exe))
	add(filepath.Join("C:\\", "tools", "ffmpeg", "bin", exe))
	add(filepath.Join(programFiles, "ffmpeg", "bin", exe))
	add(filepath.Join(programFilesX86, "ffmpeg", "bin", exe))

	addGlob(filepath.Join(localAppData, "JianyingPro", "Apps", "*", exe))
	addGlob(filepath.Join(localAppData, "Programs", "ffmpeg", "bin", exe))
	addGlob(filepath.Join(localAppData, "*", "ffmpeg", "bin", exe))
	return out
}

func commandOutput(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...) // #nosec G204 -- callers pass paths resolved by resolveLocalExecutable.
	out, err := cmd.CombinedOutput()
	if cmdCtx.Err() != nil {
		return out, cmdCtx.Err()
	}
	return out, err
}

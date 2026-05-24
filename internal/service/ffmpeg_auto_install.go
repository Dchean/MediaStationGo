package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// AutoInstallFFmpeg 在启动时检测并自动安装 ffmpeg/ffprobe
func AutoInstallFFmpeg(log *zap.Logger, cfg *config.Config) (ffprobePath, ffmpegPath string) {
	// 1. 检查配置中是否已指定路径
	if cfg.App.FFprobePath != "" {
		if _, err := os.Stat(cfg.App.FFprobePath); err == nil {
			log.Info("使用配置的 ffprobe", zap.String("path", cfg.App.FFprobePath))
			return cfg.App.FFprobePath, cfg.App.FFmpegPath
		}
	}

	// 2. 检查系统 PATH
	if path, err := exec.LookPath("ffprobe"); err == nil {
		log.Info("在 PATH 中找到 ffprobe", zap.String("path", path))
		if ffmpegPath, err := exec.LookPath("ffmpeg"); err == nil {
			return path, ffmpegPath
		}
		return path, ""
	}

	// 3. 检查默认安装位置
	defaultDir := getDefaultInstallDir()
	ffprobeDefault := filepath.Join(defaultDir, "bin", "ffprobe.exe")
	ffmpegDefault := filepath.Join(defaultDir, "bin", "ffmpeg.exe")

	if _, err := os.Stat(ffprobeDefault); err == nil {
		log.Info("在默认位置找到 ffprobe", zap.String("path", ffprobeDefault))
		return ffprobeDefault, ffmpegDefault
	}

	// 4. 尝试自动安装
	log.Warn("未找到 ffmpeg/ffprobe，尝试自动安装...")
	installed, err := tryAutoInstall(log, defaultDir)
	if err != nil {
		log.Error("自动安装失败，请手动安装 ffmpeg", zap.Error(err))
		return "", ""
	}

	if installed {
		if _, err := os.Stat(ffprobeDefault); err == nil {
			log.Info("自动安装成功", zap.String("path", ffprobeDefault))
			// 更新配置
			updateConfigPaths(cfg, ffprobeDefault, ffmpegDefault)
			return ffprobeDefault, ffmpegDefault
		}
	}

	return "", ""
}

// getDefaultInstallDir 返回默认安装目录
func getDefaultInstallDir() string {
	if wd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(wd, "scripts", "install-ffmpeg.ps1")); err == nil {
			return filepath.Join(wd, "tools", "ffmpeg")
		}
	}
	exePath, err := os.Executable()
	if err != nil {
		return "./tools/ffmpeg"
	}
	exeDir := filepath.Dir(exePath)
	return filepath.Join(exeDir, "tools", "ffmpeg")
}

// tryAutoInstall 尝试自动下载并安装 ffmpeg
func tryAutoInstall(log *zap.Logger, installDir string) (bool, error) {
	if runtime.GOOS == "windows" {
		return downloadFFmpegWindows(log, installDir)
	}

	return false, fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
}

// downloadFFmpegWindows 下载 Windows 版本的 ffmpeg
func downloadFFmpegWindows(log *zap.Logger, installDir string) (bool, error) {
	log.Info("开始下载 ffmpeg...")

	// 创建安装目录
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return false, fmt.Errorf("创建安装目录失败: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "mediastationgo-ffmpeg-*")
	if err != nil {
		return false, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// 下载 URL (使用 gyani.org 的静态构建)
	arch := "win64"
	if !is64Bit() {
		arch = "win32"
	}

	// 先尝试从 gyan.dev 下载（更可靠）
	downloadURL := fmt.Sprintf("https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip")

	log.Info("下载 ffmpeg", zap.String("url", downloadURL))

	// 使用 Go 下载
	zipPath := filepath.Join(tempDir, "ffmpeg.zip")
	if err := downloadFile(log, downloadURL, zipPath); err != nil {
		// 尝试备用 URL
		backupURL := fmt.Sprintf("https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-%s-gpl.zip", arch)
		log.Info("尝试备用下载地址", zap.String("url", backupURL))
		if err2 := downloadFile(log, backupURL, zipPath); err2 != nil {
			return false, fmt.Errorf("下载失败: %v, %v", err, err2)
		}
	}

	// 解压
	log.Info("解压 ffmpeg...")
	extractDir := filepath.Join(tempDir, "extract")
	if err := unzip(log, zipPath, extractDir); err != nil {
		return false, fmt.Errorf("解压失败: %w", err)
	}

	packageRoot, err := findFFmpegPackageRoot(extractDir)
	if err != nil {
		return false, err
	}
	if err := copyDirContents(packageRoot, installDir); err != nil {
		return false, fmt.Errorf("复制 ffmpeg 文件失败: %w", err)
	}

	ffmpegBin := filepath.Join(installDir, "bin", "ffmpeg.exe")
	ffprobeBin := filepath.Join(installDir, "bin", "ffprobe.exe")
	if _, err := os.Stat(ffmpegBin); err != nil {
		return false, fmt.Errorf("安装后未找到 ffmpeg: %w", err)
	}
	if _, err := os.Stat(ffprobeBin); err != nil {
		return false, fmt.Errorf("安装后未找到 ffprobe: %w", err)
	}

	log.Info("ffmpeg 安装完成", zap.String("dir", installDir))
	return true, nil
}

// downloadFile 下载文件
func downloadFile(log *zap.Logger, url, filepath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP 状态码: %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// unzip 解压 ZIP 文件 (简化版，实际应该使用 archive/zip)
func unzip(log *zap.Logger, zipPath, destDir string) error {
	// Windows 使用 PowerShell 解压
	if runtime.GOOS == "windows" {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		cmd := exec.Command("powershell", "-Command", fmt.Sprintf("Expand-Archive -Path '%s' -DestinationPath '%s' -Force", zipPath, destDir))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("不支持的操作系统")
}

func findFFmpegPackageRoot(root string) (string, error) {
	var ffmpegPath string
	var ffprobePath string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		switch strings.ToLower(d.Name()) {
		case "ffmpeg.exe":
			ffmpegPath = path
		case "ffprobe.exe":
			ffprobePath = path
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("扫描解压目录失败: %w", err)
	}
	if ffmpegPath == "" || ffprobePath == "" {
		return "", fmt.Errorf("解压后未找到 ffmpeg/ffprobe 可执行文件")
	}

	return filepath.Dir(filepath.Dir(ffmpegPath)), nil
}

func copyDirContents(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		if err := copyTree(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(srcPath, dstPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return err
	}

	if info.IsDir() {
		if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
			return err
		}
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(srcPath, entry.Name()), filepath.Join(dstPath, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}

	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}

	return out.Close()
}

// updateConfigPaths 更新配置文件中的路径
func updateConfigPaths(cfg *config.Config, ffprobePath, ffmpegPath string) {
	cfg.App.FFprobePath = ffprobePath
	cfg.App.FFmpegPath = ffmpegPath

	// 保存到配置文件
	// 这里需要调用 config 包的保存函数
	log := zap.L().Named("config")
	log.Info("已更新 ffmpeg 路径配置",
		zap.String("ffprobe", ffprobePath),
		zap.String("ffmpeg", ffmpegPath))
}

// is64Bit 检查是否为 64 位系统
func is64Bit() bool {
	return true // 简化处理，假设为 64 位
}

// CheckFFmpegStatus 检查 ffmpeg/ffprobe 状态 (供 API 使用)
func CheckFFmpegStatus(ffprobePath, ffmpegPath string) map[string]interface{} {
	status := map[string]interface{}{
		"ffprobe_installed": false,
		"ffmpeg_installed":  false,
		"auto_installable":  runtime.GOOS == "windows",
	}

	if ffprobePath != "" {
		if _, err := os.Stat(ffprobePath); err == nil {
			status["ffprobe_installed"] = true
			status["ffprobe_path"] = ffprobePath

			// 获取版本
			cmd := exec.Command(ffprobePath, "-version")
			out, err := cmd.Output()
			if err == nil {
				// 提取版本信息（第一行）
				lines := bytes.Split(out, []byte("\n"))
				if len(lines) > 0 {
					status["ffprobe_version"] = string(bytes.TrimSpace(lines[0]))
				}
			}
		}
	}

	if ffmpegPath != "" {
		if _, err := os.Stat(ffmpegPath); err == nil {
			status["ffmpeg_installed"] = true
			status["ffmpeg_path"] = ffmpegPath
		}
	}

	return status
}

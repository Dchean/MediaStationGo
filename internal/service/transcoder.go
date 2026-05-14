// Package service — HLS on-demand transcoder.
//
// TranscoderService spawns ffmpeg processes that segment a source media file
// into HLS (.m3u8 + .ts). The output lives under cache.cache_dir/hls/<id>.
// The HTTP layer serves these files directly with a normal http.FileServer.
//
// Concurrency model:
//   - Each Media has at most one active ffmpeg job.
//   - jobs[mediaID] tracks the running goroutine + cancel func.
//   - Calling Start while a job already exists is a no-op.
//   - When the playlist file appears on disk we consider the job "ready"
//     and unblock the HTTP handler that was waiting on it.
//
// The transcode profile is intentionally conservative: a single 720p/1.5M
// bitrate, AAC stereo audio, MPEG-TS segments. Hardware acceleration
// (NVENC / QSV / VAAPI) is left as a future config-driven extension point.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// TranscoderService orchestrates background ffmpeg transcodes.
type TranscoderService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
	hub  *Hub

	mu   sync.Mutex
	jobs map[string]*hlsJob
}

// hlsJob holds the live state of one ffmpeg run.
type hlsJob struct {
	mediaID    string
	outputDir  string
	cancel     context.CancelFunc
	startedAt  time.Time
	playlistOK bool
}

// NewTranscoderService is the constructor.
func NewTranscoderService(cfg *config.Config, log *zap.Logger, repo *repository.Container, hub *Hub) *TranscoderService {
	return &TranscoderService{
		cfg:  cfg,
		log:  log,
		repo: repo,
		hub:  hub,
		jobs: make(map[string]*hlsJob),
	}
}

// HLSDir is the per-media directory that holds index.m3u8 + segment files.
func (t *TranscoderService) HLSDir(mediaID string) string {
	return filepath.Join(t.cfg.Cache.CacheDir, "hls", mediaID)
}

// PlaylistPath returns the absolute path of the m3u8 playlist for a media.
func (t *TranscoderService) PlaylistPath(mediaID string) string {
	return filepath.Join(t.HLSDir(mediaID), "index.m3u8")
}

// EnsureJob makes sure a transcode is running for mediaID. The function is
// non-blocking: it returns the playlist path immediately. The caller is
// expected to poll until WaitReady reports true.
func (t *TranscoderService) EnsureJob(ctx context.Context, mediaID string) (string, error) {
	m, err := t.repo.Media.FindByID(ctx, mediaID)
	if err != nil {
		return "", err
	}
	if m == nil {
		return "", ErrMediaNotFound
	}
	if _, err := os.Stat(m.Path); err != nil {
		return "", ErrMediaNotFound
	}

	t.mu.Lock()
	if _, ok := t.jobs[mediaID]; ok {
		t.mu.Unlock()
		return t.PlaylistPath(mediaID), nil
	}

	outDir := t.HLSDir(mediaID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.mu.Unlock()
		return "", err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	job := &hlsJob{
		mediaID:   mediaID,
		outputDir: outDir,
		cancel:    cancel,
		startedAt: time.Now(),
	}
	t.jobs[mediaID] = job
	t.mu.Unlock()

	go t.runFFmpeg(jobCtx, job, m.Path)
	return t.PlaylistPath(mediaID), nil
}

// WaitReady blocks (with a deadline) until the playlist file shows up on
// disk. Returns true on success.
func (t *TranscoderService) WaitReady(ctx context.Context, mediaID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(t.PlaylistPath(mediaID)); err == nil {
			t.mu.Lock()
			if j, ok := t.jobs[mediaID]; ok {
				j.playlistOK = true
			}
			t.mu.Unlock()
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// StopJob cancels a running ffmpeg process for mediaID, if any.
func (t *TranscoderService) StopJob(mediaID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if j, ok := t.jobs[mediaID]; ok {
		j.cancel()
		delete(t.jobs, mediaID)
	}
}

// StopAll terminates every running transcode (called on graceful shutdown).
func (t *TranscoderService) StopAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, j := range t.jobs {
		j.cancel()
		delete(t.jobs, id)
	}
}

func (t *TranscoderService) runFFmpeg(ctx context.Context, job *hlsJob, source string) {
	bin := t.cfg.App.FFmpegPath
	if bin == "" {
		bin = "ffmpeg"
	}

	playlist := filepath.Join(job.outputDir, "index.m3u8")
	segments := filepath.Join(job.outputDir, "seg_%05d.ts")

	args := []string{
		"-y",
		"-fflags", "+genpts",
		"-i", source,
		// Single 720p H.264 video rendition.
		"-map", "0:v:0?",
		"-map", "0:a:0?",
		"-vf", "scale=-2:min(720\\,ih)",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-profile:v", "main",
		"-level", "4.0",
		"-pix_fmt", "yuv420p",
		"-b:v", "1500k",
		"-maxrate", "1800k",
		"-bufsize", "3000k",
		"-c:a", "aac",
		"-ar", "48000",
		"-b:a", "128k",
		"-ac", "2",
		"-force_key_frames", "expr:gte(t,n_forced*4)",
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "0",
		"-hls_segment_type", "mpegts",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", segments,
		playlist,
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = os.Stderr

	t.log.Info("transcode started",
		zap.String("media_id", job.mediaID),
		zap.String("source", source),
	)
	t.hub.Publish("transcode", map[string]any{
		"media_id": job.mediaID,
		"status":   "started",
	})

	if err := cmd.Run(); err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		t.log.Warn("ffmpeg exited",
			zap.String("media_id", job.mediaID),
			zap.Error(err),
		)
	}

	t.mu.Lock()
	delete(t.jobs, job.mediaID)
	t.mu.Unlock()

	t.hub.Publish("transcode", map[string]any{
		"media_id": job.mediaID,
		"status":   "stopped",
		"duration": time.Since(job.startedAt).Seconds(),
	})
}

// HumanFFmpegProfile is exposed for the admin UI / settings view.
func (t *TranscoderService) HumanFFmpegProfile() string {
	return fmt.Sprintf("ffmpeg=%s, output=%s",
		t.cfg.App.FFmpegPath, filepath.Join(t.cfg.Cache.CacheDir, "hls"))
}

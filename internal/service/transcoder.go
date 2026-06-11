// Package service — HLS on-demand transcoder.
//
// TranscoderService spawns ffmpeg processes that segment a source media file
// into HLS (.m3u8 + .ts). The output lives under cache.cache_dir/hls/<id>.
// The HTTP layer serves these files directly with a normal http.FileServer.
//
// Encoder selection (read once at startup from the config):
//
//	transcoder.encoder = "" | "nvenc" | "qsv" | "vaapi"
//
//	""      software libx264 (default; runs anywhere)
//	nvenc   h264_nvenc      (NVIDIA GPU, requires --gpus all on Docker)
//	qsv     h264_qsv        (Intel iGPU, requires /dev/dri:/dev/dri)
//	vaapi   h264_vaapi      (Mesa/Intel VAAPI, requires /dev/dri:/dev/dri
//	                         plus the kernel module loaded)
//
// Concurrency model:
//   - Each Media has at most one active ffmpeg job.
//   - jobs[mediaID] tracks the running goroutine + cancel func.
//   - Calling Start while a job already exists is a no-op.
//   - When the playlist file appears on disk we consider the job "ready"
//     and unblock the HTTP handler that was waiting on it.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	lastAccess time.Time
	playlistOK bool
	encoder    string
}

var (
	// ErrTranscodeDisabled is returned when HLS transcoding is globally disabled.
	ErrTranscodeDisabled = errors.New("transcode disabled")
	// ErrTranscodeBusy is returned when the server has reached its configured
	// ffmpeg concurrency limit.
	ErrTranscodeBusy = errors.New("transcode concurrency limit reached")
)

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
	if !t.cfg.Transcoder.Enabled {
		return "", ErrTranscodeDisabled
	}
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
	if _, err := t.resolveFFmpegPath(); err != nil {
		return "", err
	}

	t.mu.Lock()
	if _, ok := t.jobs[mediaID]; ok {
		t.touchJobLocked(mediaID)
		t.mu.Unlock()
		return t.PlaylistPath(mediaID), nil
	}
	if max := t.maxConcurrent(); max > 0 && len(t.jobs) >= max {
		t.mu.Unlock()
		return "", ErrTranscodeBusy
	}

	outDir := t.HLSDir(mediaID)
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.mu.Unlock()
		return "", err
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	job := &hlsJob{
		mediaID:    mediaID,
		outputDir:  outDir,
		cancel:     cancel,
		startedAt:  time.Now(),
		lastAccess: time.Now(),
		encoder:    t.effectiveEncoder(),
	}
	t.jobs[mediaID] = job
	t.mu.Unlock()

	go t.monitorIdle(jobCtx, job)
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

// TouchJob records client activity for the HLS playlist or segment. The idle
// watchdog uses it to stop ffmpeg soon after the player is closed or switches
// back to direct play.
func (t *TranscoderService) TouchJob(mediaID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.touchJobLocked(mediaID)
}

func (t *TranscoderService) touchJobLocked(mediaID string) {
	if j, ok := t.jobs[mediaID]; ok {
		j.lastAccess = time.Now()
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

// ActiveJob is the JSON shape exposed to the React Tasks panel.
type ActiveJob struct {
	MediaID    string    `json:"media_id"`
	Encoder    string    `json:"encoder"`
	StartedAt  time.Time `json:"started_at"`
	PlaylistOK bool      `json:"playlist_ok"`
}

// Active returns a snapshot of the currently running transcode jobs.
func (t *TranscoderService) Active() []ActiveJob {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]ActiveJob, 0, len(t.jobs))
	for _, j := range t.jobs {
		out = append(out, ActiveJob{
			MediaID:    j.mediaID,
			Encoder:    j.encoder,
			StartedAt:  j.startedAt,
			PlaylistOK: j.playlistOK,
		})
	}
	return out
}

func (t *TranscoderService) maxConcurrent() int {
	if t.cfg.Transcoder.MaxConcurrent <= 0 {
		return 1
	}
	return t.cfg.Transcoder.MaxConcurrent
}

func (t *TranscoderService) idleTimeout() time.Duration {
	if t.cfg.Transcoder.IdleTimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(t.cfg.Transcoder.IdleTimeoutSeconds) * time.Second
}

func (t *TranscoderService) monitorIdle(ctx context.Context, job *hlsJob) {
	timeout := t.idleTimeout()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			current, ok := t.jobs[job.mediaID]
			if !ok {
				t.mu.Unlock()
				return
			}
			idleFor := time.Since(current.lastAccess)
			t.mu.Unlock()
			if idleFor >= timeout {
				t.log.Info("transcode idle timeout",
					zap.String("media_id", job.mediaID),
					zap.Duration("idle_for", idleFor),
					zap.Duration("timeout", timeout),
				)
				t.StopJob(job.mediaID)
				return
			}
		}
	}
}

func (t *TranscoderService) runFFmpeg(ctx context.Context, job *hlsJob, source string) {
	bin, err := t.resolveFFmpegPath()
	if err != nil {
		t.log.Warn("ffmpeg unavailable", zap.String("media_id", job.mediaID), zap.Error(err))
		t.mu.Lock()
		delete(t.jobs, job.mediaID)
		t.mu.Unlock()
		t.hub.Publish("transcode", map[string]any{
			"media_id": job.mediaID,
			"status":   "error",
			"error":    err.Error(),
		})
		return
	}

	playlist := filepath.Join(job.outputDir, "index.m3u8")
	segments := filepath.Join(job.outputDir, "seg_%05d.ts")

	args := buildFFmpegArgs(t.cfg, source, playlist, segments)

	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- bin is resolved by resolveFFmpegPath and args are passed without a shell.
	cmd.Stderr = os.Stderr

	t.log.Info("transcode started",
		zap.String("media_id", job.mediaID),
		zap.String("encoder", job.encoder),
		zap.String("source", source),
	)
	t.hub.Publish("transcode", map[string]any{
		"media_id": job.mediaID,
		"encoder":  job.encoder,
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

func (t *TranscoderService) resolveFFmpegPath() (string, error) {
	var lastErr error
	for _, bin := range executableCandidates(strings.TrimSpace(t.cfg.App.FFmpegPath), "ffmpeg") {
		if err := validateFFmpegForTranscode(context.Background(), bin, t.effectiveEncoder()); err != nil {
			lastErr = err
			continue
		}
		t.cfg.App.FFmpegPath = bin
		return bin, nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("no usable ffmpeg found for HLS transcode: %w", lastErr)
	}
	return "", fmt.Errorf("ffmpeg not found in PATH or common local app directories; configure app.ffmpeg_path to an existing local ffmpeg")
}

func (t *TranscoderService) effectiveEncoder() string {
	if !t.cfg.Transcoder.HardwareAccel {
		return ""
	}
	return normalizedHardwareEncoder(t.cfg.Transcoder.Encoder)
}

func normalizedHardwareEncoder(encoder string) string {
	switch strings.ToLower(strings.TrimSpace(encoder)) {
	case "nvenc", "qsv", "vaapi":
		return strings.ToLower(strings.TrimSpace(encoder))
	default:
		return ""
	}
}

func validateFFmpegForTranscode(ctx context.Context, bin, encoder string) error {
	required := requiredVideoEncoder(encoder)
	out, err := commandOutput(ctx, 8*time.Second, bin, "-hide_banner", "-encoders")
	if err != nil {
		return fmt.Errorf("%s cannot list encoders: %w", bin, err)
	}
	if !hasFFmpegListEntry(string(out), required) {
		return fmt.Errorf("%s does not provide required encoder %s", bin, required)
	}

	out, err = commandOutput(ctx, 8*time.Second, bin, "-hide_banner", "-muxers")
	if err != nil || !hasFFmpegListEntry(string(out), "hls") {
		out, err = commandOutput(ctx, 8*time.Second, bin, "-hide_banner", "-formats")
		if err != nil {
			return fmt.Errorf("%s cannot list muxers/formats: %w", bin, err)
		}
	}
	if !hasFFmpegListEntry(string(out), "hls") {
		return fmt.Errorf("%s does not provide hls muxer", bin)
	}
	return nil
}

func requiredVideoEncoder(encoder string) string {
	switch encoder {
	case "nvenc":
		return "h264_nvenc"
	case "qsv":
		return "h264_qsv"
	case "vaapi":
		return "h264_vaapi"
	default:
		return "libx264"
	}
}

func hasFFmpegListEntry(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for _, field := range fields {
			if field == name {
				return true
			}
		}
	}
	return false
}

// buildFFmpegArgs assembles the ffmpeg command line for the configured
// encoder. The function is package-level so the unit test can pin its
// behaviour without spawning a real ffmpeg process.
func buildFFmpegArgs(cfg *config.Config, source, playlist, segments string) []string {
	enc := ""
	if cfg.Transcoder.HardwareAccel {
		enc = normalizedHardwareEncoder(cfg.Transcoder.Encoder)
	}
	bitrate := cfg.Transcoder.VideoBitrate
	if bitrate == "" {
		bitrate = "1500k"
	}
	maxrate := cfg.Transcoder.MaxRate
	if maxrate == "" {
		maxrate = "1800k"
	}
	bufsize := cfg.Transcoder.BufSize
	if bufsize == "" {
		bufsize = "3000k"
	}
	preset := cfg.Transcoder.Preset
	if preset == "" {
		preset = "veryfast"
	}
	height := cfg.Transcoder.MaxHeight
	if height <= 0 {
		height = 720
	}
	segDur := cfg.Transcoder.SegmentSeconds
	if segDur <= 0 {
		segDur = 4
	}

	// Hardware-accel arguments differ in three places:
	//   - Optional input flags (-hwaccel + device init)
	//   - Optional input pixel-format upload filter
	//   - The actual -c:v encoder name + preset/quality flag
	var pre, vf, vcodec, vpreset string
	switch enc {
	case "nvenc":
		pre = "-hwaccel cuda -hwaccel_output_format cuda"
		vf = fmt.Sprintf("scale_cuda=-2:min(%d\\,ih)", height)
		vcodec = "h264_nvenc"
		vpreset = "p4"
	case "qsv":
		pre = "-hwaccel qsv -hwaccel_output_format qsv"
		vf = fmt.Sprintf("scale_qsv=-1:min(%d\\,ih)", height)
		vcodec = "h264_qsv"
		vpreset = preset
	case "vaapi":
		device := cfg.App.VAAPIDevice
		if device == "" {
			device = "/dev/dri/renderD128"
		}
		pre = fmt.Sprintf("-hwaccel vaapi -vaapi_device %s -hwaccel_output_format vaapi", device)
		vf = fmt.Sprintf("scale_vaapi=-2:min(%d\\,ih),format=nv12|vaapi,hwupload", height)
		vcodec = "h264_vaapi"
		vpreset = ""
	default:
		// software
		pre = ""
		vf = fmt.Sprintf("scale=-2:min(%d\\,ih)", height)
		vcodec = "libx264"
		vpreset = preset
	}

	args := []string{"-y", "-hide_banner", "-nostdin", "-fflags", "+genpts"}
	for _, p := range splitNonEmptyArgs(pre) {
		args = append(args, p)
	}
	if cfg.Transcoder.Realtime {
		args = append(args, "-re")
	}
	args = append(args, "-i", source, "-map", "0:v:0?", "-map", "0:a:0?", "-vf", vf, "-c:v", vcodec)
	if cfg.Transcoder.Threads > 0 && vcodec == "libx264" {
		args = append(args, "-threads", strconv.Itoa(cfg.Transcoder.Threads))
	}
	if vpreset != "" {
		args = append(args, "-preset", vpreset)
	}
	args = append(args,
		"-pix_fmt", "yuv420p",
		"-b:v", bitrate,
		"-maxrate", maxrate,
		"-bufsize", bufsize,
		"-c:a", "aac",
		"-ar", "48000",
		"-b:a", "128k",
		"-ac", "2",
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segDur),
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", segDur),
		"-hls_list_size", "0",
		"-hls_segment_filename", segments,
		playlist,
	)
	return args
}

// splitNonEmptyArgs is a tiny helper that mirrors strings.Fields for the
// pre-input flag string without dragging the strings import into the hot
// path of every call to buildFFmpegArgs.
func splitNonEmptyArgs(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, 4)
	field := make([]rune, 0, 16)
	flush := func() {
		if len(field) > 0 {
			out = append(out, string(field))
			field = field[:0]
		}
	}
	for _, r := range s {
		if r == ' ' || r == '\t' {
			flush()
			continue
		}
		field = append(field, r)
	}
	flush()
	return out
}

// HumanFFmpegProfile is exposed for the admin UI / settings view.
func (t *TranscoderService) HumanFFmpegProfile() string {
	return fmt.Sprintf("ffmpeg=%s, encoder=%s, output=%s",
		t.cfg.App.FFmpegPath, t.cfg.Transcoder.Encoder, filepath.Join(t.cfg.Cache.CacheDir, "hls"))
}

// Package service — ffprobe wrapper.
//
// FFprobeService shells out to the `ffprobe` binary configured in
// app.ffprobe_path and parses its JSON output into a typed struct. It is
// intentionally minimal: we only extract the fields needed to populate
// model.Media (duration, resolution, video / audio codec) so a fresh scan
// can show meaningful metadata even before the TMDb scraper has run.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// FFprobeService wraps the external ffprobe binary.
type FFprobeService struct {
	cfg     *config.Config
	log     *zap.Logger
	mu      sync.RWMutex
	limiter chan struct{}
}

// NewFFprobeService is the constructor.
func NewFFprobeService(cfg *config.Config, log *zap.Logger) *FFprobeService {
	maxConcurrent := normalizeFFprobeMaxConcurrent(cfg.App.FFprobeMaxConcurrent)
	return &FFprobeService{cfg: cfg, log: log, limiter: make(chan struct{}, maxConcurrent)}
}

func normalizeFFprobeMaxConcurrent(n int) int {
	if n <= 0 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

func (f *FFprobeService) SetMaxConcurrent(n int) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limiter = make(chan struct{}, normalizeFFprobeMaxConcurrent(n))
}

// ProbeResult is the subset of ffprobe output consumed by the scanner.
type ProbeResult struct {
	DurationSec int
	Width       int
	Height      int
	VideoCodec  string
	AudioCodec  string
	Container   string
}

// Probe runs ffprobe against path and returns a typed result. A 30s timeout
// is applied so a single broken file does not hang the scanner.
func (f *FFprobeService) Probe(ctx context.Context, path string) (*ProbeResult, error) {
	if f == nil {
		return nil, errors.New("ffprobe service nil")
	}
	token, err := f.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer f.release(token)
	if bin, err := resolveLocalExecutable(f.cfg.App.FFprobePath, "ffprobe"); err == nil {
		f.cfg.App.FFprobePath = bin
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(probeCtx, bin, // #nosec G204 -- bin is resolved by resolveLocalExecutable before execution.
			"-v", "error",
			"-print_format", "json",
			"-show_format",
			"-show_streams",
			path,
		)
		out, err := cmd.Output()
		if err == nil {
			return parseProbeJSON(out)
		}
		if f.log != nil {
			f.log.Debug("ffprobe failed, trying ffmpeg fallback", zap.String("path", path), zap.Error(err))
		}
	}
	return f.probeWithFFmpeg(ctx, path)
}

func (f *FFprobeService) acquire(ctx context.Context) (chan struct{}, error) {
	f.mu.RLock()
	limiter := f.limiter
	f.mu.RUnlock()
	if limiter == nil {
		return nil, nil
	}
	select {
	case limiter <- struct{}{}:
		return limiter, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *FFprobeService) release(limiter chan struct{}) {
	if limiter == nil {
		return
	}
	select {
	case <-limiter:
	default:
	}
}

func (f *FFprobeService) probeWithFFmpeg(ctx context.Context, path string) (*ProbeResult, error) {
	bin, err := resolveLocalExecutable(f.cfg.App.FFmpegPath, "ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffprobe/ffmpeg unavailable: %w", err)
	}
	f.cfg.App.FFmpegPath = bin
	out, _ := commandOutput(ctx, 30*time.Second, bin, "-hide_banner", "-i", path)
	res := parseFFmpegProbeText(string(out))
	if res.VideoCodec == "" && res.AudioCodec == "" && res.DurationSec == 0 {
		return nil, fmt.Errorf("ffmpeg probe %s: no stream metadata parsed", path)
	}
	return res, nil
}

// rawProbe mirrors the relevant fields of `ffprobe -show_format -show_streams`.
type rawProbe struct {
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

func parseProbeJSON(data []byte) (*ProbeResult, error) {
	var raw rawProbe
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse ffprobe json: %w", err)
	}
	res := &ProbeResult{Container: raw.Format.FormatName}
	if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
		res.DurationSec = int(d)
	}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if res.VideoCodec == "" {
				res.VideoCodec = s.CodecName
				res.Width = s.Width
				res.Height = s.Height
			}
		case "audio":
			if res.AudioCodec == "" {
				res.AudioCodec = s.CodecName
			}
		}
	}
	return res, nil
}

var (
	ffmpegDurationRE = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	ffmpegInputRE    = regexp.MustCompile(`Input #\d+,\s*(.+?),\s*from`)
	ffmpegVideoRE    = regexp.MustCompile(`Video:\s*([^,\s]+).*?(\d{2,5})x(\d{2,5})`)
	ffmpegAudioRE    = regexp.MustCompile(`Audio:\s*([^,\s]+)`)
)

func parseFFmpegProbeText(text string) *ProbeResult {
	res := &ProbeResult{}
	if match := ffmpegInputRE.FindStringSubmatch(text); len(match) == 2 {
		res.Container = strings.TrimSpace(match[1])
	}
	if match := ffmpegDurationRE.FindStringSubmatch(text); len(match) == 4 {
		hours, _ := strconv.Atoi(match[1])
		minutes, _ := strconv.Atoi(match[2])
		seconds, _ := strconv.ParseFloat(match[3], 64)
		res.DurationSec = hours*3600 + minutes*60 + int(seconds)
	}
	for _, line := range strings.Split(text, "\n") {
		if res.VideoCodec == "" {
			if match := ffmpegVideoRE.FindStringSubmatch(line); len(match) == 4 {
				res.VideoCodec = strings.TrimSpace(match[1])
				res.Width, _ = strconv.Atoi(match[2])
				res.Height, _ = strconv.Atoi(match[3])
			}
		}
		if res.AudioCodec == "" {
			if match := ffmpegAudioRE.FindStringSubmatch(line); len(match) == 2 {
				res.AudioCodec = strings.TrimSpace(match[1])
			}
		}
	}
	return res
}

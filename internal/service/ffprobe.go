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
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
)

// FFprobeService wraps the external ffprobe binary.
type FFprobeService struct {
	cfg *config.Config
	log *zap.Logger
}

// NewFFprobeService is the constructor.
func NewFFprobeService(cfg *config.Config, log *zap.Logger) *FFprobeService {
	return &FFprobeService{cfg: cfg, log: log}
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
	bin := f.cfg.App.FFprobePath
	if bin == "" {
		bin = "ffprobe"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	return parseProbeJSON(out)
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

// Package service — direct-play / HLS streaming.
//
// StreamService exposes two flavours of playback:
//
//   - Direct play: the original file is served with HTTP Range support.
//     Works for browser-friendly containers (mp4 / webm / m4v), no ffmpeg
//     involved, zero CPU overhead.
//   - HLS: when the client opts in (or the source codec / container is
//     not browser-friendly), the TranscoderService runs ffmpeg in the
//     background and we serve the resulting .m3u8 + .ts files directly.
//
// The HTTP layer decides which mode to use based on the request path:
//
//	GET /api/stream/:id              → direct play
//	GET /api/hls/:id/index.m3u8      → HLS playlist
//	GET /api/hls/:id/seg_NNNNN.ts    → HLS segment
package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	STRMEnabledSettingKey                  = "strm.enabled"
	CloudPlaybackModeSettingKey            = "cloud.playback_mode"
	CloudPlaybackSTRMEnabledSettingKey     = "cloud.playback_strm_enabled"
	CloudPlaybackRedirectEnabledSettingKey = "cloud.playback_redirect_proxy_enabled"

	CloudPlaybackModeSTRM          = "strm"
	CloudPlaybackModeRedirectProxy = "redirect_proxy"
)

type CloudPlaybackOptions struct {
	STRMEnabled          bool
	RedirectProxyEnabled bool
	PreferredMode        string
}

// StreamService serves media files with proper Range support so browsers can
// seek into the stream.
type StreamService struct {
	cfg        *config.Config
	log        *zap.Logger
	repo       *repository.Container
	transcoder *TranscoderService
}

// NewStreamService is the constructor.
func NewStreamService(cfg *config.Config, log *zap.Logger, repo *repository.Container, transcoder *TranscoderService) *StreamService {
	return &StreamService{
		cfg:        cfg,
		log:        log,
		repo:       repo,
		transcoder: transcoder,
	}
}

// ErrMediaNotFound is returned when the media row or its file is missing.
var ErrMediaNotFound = errors.New("media not found")

// ErrCloudPlaybackUnavailable 表示媒体行存在但属于云盘媒体、且当前无法
// 构造可用的播放重定向（通常是 STRMURL 缺失，需要重新扫描媒体库）。
// 调用方应把它与「媒体不存在」区分开，避免把配置类故障当成 404 返回给播放器。
var ErrCloudPlaybackUnavailable = errors.New("cloud media playback unavailable: media missing play url; re-scan the library")

var ErrCloudPlaybackDisabled = errors.New("cloud media playback disabled by admin settings")

// normalizeCloudPlayTarget 把存库的云盘播放 URL 规范化为相对路径。
//
// STRMURL 是扫描时根据当时的 server_url/请求地址生成并固化进数据库的。
// 在 Windows 开发机上扫描、再部署到 Docker（或更换了内网 IP/域名）后，
// 这些绝对 URL 会指向已失效的旧地址，第三方播放器跟随 302 就会拿到
// 连接失败/404。这里只要能从 URL 中解析出 provider+ref，就重建为相对
// /api/cloud/play 路径，由 absoluteInternalRedirect 基于「当前请求」补全
// host，从而对历史脏数据免疫。
func normalizeCloudPlayTarget(raw string) string {
	typ, ref, ok := parseCloudMediaPlaybackURL(raw)
	if !ok {
		return raw
	}
	return BuildRelativeCloudPlayURL(typ, ref)
}

// BuildRelativeCloudPlayURL 构造相对的云盘播放 API 路径。
func BuildRelativeCloudPlayURL(typ, ref string) string {
	return "/api/cloud/play/" + url.PathEscape(strings.TrimSpace(typ)) + "?" + url.Values{"ref": []string{ref}}.Encode()
}

// directPlayOnly reports whether the admin enabled「客户端直连解码」mode,
// in which the host never transcodes (HLS is refused) and all playback is
// handled by the client (direct play / 302 redirect).
func (s *StreamService) directPlayOnly(ctx context.Context) bool {
	if s.repo == nil || s.repo.Setting == nil {
		return false
	}
	v, err := s.repo.Setting.Get(ctx, PlaybackDirectOnlySettingKey)
	if err != nil {
		return false
	}
	return parseBoolSetting(v, false)
}

// withAuthToken propagates the caller's auth token onto an internal redirect
// target. A browser <video> element cannot send Authorization headers or
// cookies when it follows a 302, so the cloud 302 chain
// (/api/stream?token=… → /api/cloud/play → CDN) would otherwise hit
// /api/cloud/play unauthenticated and 401. We only attach the token to our
// own relative API endpoints — never to an absolute external direct link —
// so the JWT is never leaked off-site (e.g. to the cloud CDN).
func withAuthToken(target string, r *http.Request) string {
	return withAuthTokenForInternalRedirect(target, r, "")
}

func withAuthTokenForInternalRedirect(target string, r *http.Request, publicBase string) string {
	if r == nil {
		return target
	}
	if strings.HasPrefix(target, "//") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	if u.IsAbs() && !isInternalAPIURL(u, r, publicBase) {
		return target
	}
	if !strings.HasPrefix(strings.ToLower(u.Path), "/api/") {
		return target
	}
	tok := requestToken(r)
	if tok == "" {
		return target
	}
	q := u.Query()
	if q.Get("token") == "" {
		q.Set("token", tok)
	}
	if q.Get("media_id") == "" && strings.HasPrefix(strings.ToLower(u.Path), "/api/cloud/play/") {
		if mediaID := playbackMediaIDFromRequestPath(r.URL.Path); mediaID != "" {
			q.Set("media_id", mediaID)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func playbackMediaIDFromRequestPath(pathValue string) string {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return ""
	}
	segments := strings.Split(strings.Trim(pathValue, "/"), "/")
	lower := make([]string, len(segments))
	for i, segment := range segments {
		lower[i] = strings.ToLower(segment)
	}
	var mediaID string
	switch {
	case len(segments) >= 3 && lower[0] == "api" && lower[1] == "stream":
		mediaID = segments[2]
	case len(segments) >= 4 && lower[0] == "emby" && lower[1] == "api" && lower[2] == "stream":
		mediaID = segments[3]
	case len(segments) >= 3 && lower[0] == "videos":
		mediaID = segments[1]
	}
	if decoded, err := url.PathUnescape(mediaID); err == nil {
		mediaID = decoded
	}
	return strings.TrimSpace(mediaID)
}

func absoluteInternalRedirect(target string, r *http.Request) string {
	if r == nil || target == "" || strings.HasPrefix(target, "//") {
		return target
	}
	u, err := url.Parse(target)
	if err != nil || u.IsAbs() || !strings.HasPrefix(target, "/") {
		return target
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return target
	}
	u.Scheme = scheme
	u.Host = host
	return u.String()
}

func isInternalAPIURL(u *url.URL, r *http.Request, publicBase string) bool {
	if u == nil || !strings.HasPrefix(strings.ToLower(u.Path), "/api/") {
		return false
	}
	targetHost := strings.ToLower(strings.TrimSpace(u.Host))
	if targetHost == "" {
		return true
	}
	if r != nil {
		if host := strings.ToLower(strings.TrimSpace(r.Host)); host != "" && targetHost == host {
			return true
		}
		if host := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))); host != "" && targetHost == host {
			return true
		}
	}
	if publicBase != "" {
		if base, err := url.Parse(publicBase); err == nil && strings.EqualFold(strings.TrimSpace(base.Host), targetHost) {
			return true
		}
	}
	return false
}

// requestToken extracts the bearer JWT from the incoming request the same way
// the auth middleware does (Authorization header, Emby token headers, or the
// token / api_key query params used by <video>.src).
func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	for _, hk := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if v := strings.TrimSpace(r.Header.Get(hk)); v != "" {
			return v
		}
	}
	for _, hk := range []string{"X-Emby-Authorization", "X-MediaBrowser-Authorization"} {
		if token := streamTokenFromAuthHeader(r.Header.Get(hk)); token != "" {
			return token
		}
	}
	if token := streamTokenFromAuthHeader(r.Header.Get("Authorization")); token != "" {
		return token
	}
	for _, k := range []string{"token", "api_key", "apiKey", "ApiKey"} {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func streamTokenFromAuthHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, prefix := range []string{"Bearer ", "Emby "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	if strings.HasPrefix(value, "MediaBrowser ") || strings.Contains(value, "Token=") {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "MediaBrowser "))
			if !strings.HasPrefix(part, "Token=") {
				continue
			}
			token := strings.TrimSpace(strings.TrimPrefix(part, "Token="))
			return strings.Trim(token, `"`)
		}
		return ""
	}
	return value
}

func CloudPlaybackSettings(ctx context.Context, repo *repository.Container) CloudPlaybackOptions {
	opts := CloudPlaybackOptions{
		STRMEnabled:          false,
		RedirectProxyEnabled: true,
		PreferredMode:        CloudPlaybackModeRedirectProxy,
	}
	if repo == nil || repo.Setting == nil {
		return opts
	}
	modeRaw, hasMode := settingValue(ctx, repo, CloudPlaybackModeSettingKey)
	if mode := normalizeCloudPlaybackMode(modeRaw); mode != "" {
		opts.PreferredMode = mode
	}
	legacySTRM, hasLegacySTRM := settingValue(ctx, repo, STRMEnabledSettingKey)
	legacySTRMEnabled := hasLegacySTRM && parseBoolSetting(legacySTRM, false)
	if !hasMode && legacySTRMEnabled {
		opts.PreferredMode = CloudPlaybackModeSTRM
	}
	if raw, ok := settingValue(ctx, repo, CloudPlaybackSTRMEnabledSettingKey); ok {
		opts.STRMEnabled = parseBoolSetting(raw, false)
	} else if hasLegacySTRM {
		opts.STRMEnabled = legacySTRMEnabled
	} else if hasMode && opts.PreferredMode == CloudPlaybackModeSTRM {
		opts.STRMEnabled = true
	}
	if raw, ok := settingValue(ctx, repo, CloudPlaybackRedirectEnabledSettingKey); ok {
		opts.RedirectProxyEnabled = parseBoolSetting(raw, true)
	} else if hasMode && opts.PreferredMode == CloudPlaybackModeRedirectProxy {
		opts.RedirectProxyEnabled = true
	}
	if opts.PreferredMode == CloudPlaybackModeSTRM && !opts.STRMEnabled && opts.RedirectProxyEnabled {
		opts.PreferredMode = CloudPlaybackModeRedirectProxy
	}
	if opts.PreferredMode == CloudPlaybackModeRedirectProxy && !opts.RedirectProxyEnabled && opts.STRMEnabled {
		opts.PreferredMode = CloudPlaybackModeSTRM
	}
	return opts
}

func CloudPlaybackMode(ctx context.Context, repo *repository.Container) string {
	opts := CloudPlaybackSettings(ctx, repo)
	switch opts.PreferredMode {
	case CloudPlaybackModeSTRM:
		if opts.STRMEnabled {
			return CloudPlaybackModeSTRM
		}
		if opts.RedirectProxyEnabled {
			return CloudPlaybackModeRedirectProxy
		}
	case CloudPlaybackModeRedirectProxy:
		if opts.RedirectProxyEnabled {
			return CloudPlaybackModeRedirectProxy
		}
		if opts.STRMEnabled {
			return CloudPlaybackModeSTRM
		}
	}
	return ""
}

func STRMPlaybackEnabled(ctx context.Context, repo *repository.Container) bool {
	return CloudPlaybackSettings(ctx, repo).STRMEnabled
}

func cloudPlaybackModeEnabled(ctx context.Context, repo *repository.Container, mode string) bool {
	opts := CloudPlaybackSettings(ctx, repo)
	switch normalizeCloudPlaybackMode(mode) {
	case CloudPlaybackModeSTRM:
		return opts.STRMEnabled
	case CloudPlaybackModeRedirectProxy:
		return opts.RedirectProxyEnabled
	default:
		return opts.STRMEnabled || opts.RedirectProxyEnabled
	}
}

func settingValue(ctx context.Context, repo *repository.Container, key string) (string, bool) {
	if repo == nil || repo.Setting == nil {
		return "", false
	}
	v, err := repo.Setting.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}

func normalizeCloudPlaybackMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strm", "strmurl", "strm_url", "api_stream", "api-stream":
		return CloudPlaybackModeSTRM
	case "302", "proxy", "reverse_proxy", "redirect", "redirect_proxy", "302_proxy", "302-proxy", "cloud":
		return CloudPlaybackModeRedirectProxy
	default:
		return ""
	}
}

// ServeFile streams the file backing the given media ID using
// http.ServeContent so HEAD / Range / If-Modified-Since are handled for free.
//
// When the media row has a STRMURL set we redirect (302) to that URL
// instead of opening a local file. This lets WebDAV / Alist / S3 / HTTP
// direct links flow through the rest of the player UI unchanged.
func (s *StreamService) ServeFile(w http.ResponseWriter, r *http.Request, mediaID string) error {
	return s.ServeFileWithCloudMode(w, r, mediaID, "")
}

func (s *StreamService) ServeFileWithCloudMode(w http.ResponseWriter, r *http.Request, mediaID, cloudMode string) error {
	m, err := s.repo.Media.FindByID(r.Context(), mediaID)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrMediaNotFound
	}
	if strmURL := strings.TrimSpace(m.STRMURL); strmURL != "" && (isCloudPlaybackTarget(strmURL) || STRMPlaybackEnabled(r.Context(), s.repo)) {
		if !cloudPlaybackModeEnabled(r.Context(), s.repo, cloudMode) {
			return ErrCloudPlaybackDisabled
		}
		// 云盘播放 URL 先规范化为相对路径，免疫扫描时固化的旧 host。
		target := normalizeCloudPlayTarget(strmURL)
		target = withAuthTokenForInternalRedirect(target, r, PublicServerURL(r.Context(), s.repo, s.cfg))
		setCloudRedirectNoStore(w)
		http.Redirect(w, r, absoluteInternalRedirect(target, r), http.StatusFound)
		return nil
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.Path)), "cloud://") {
		// 云盘媒体没有本地文件可回退；走到这里说明 STRM 播放被关闭或
		// STRMURL 缺失。返回明确错误而不是笼统的「文件不存在」，
		// 处理器据此回 502 + 原因，方便用户在播放器/日志里定位。
		return ErrCloudPlaybackUnavailable
	}
	f, err := os.Open(m.Path)
	if err != nil {
		return ErrMediaNotFound
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}

func setCloudRedirectNoStore(w http.ResponseWriter) {
	if w == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func isCloudPlaybackTarget(raw string) bool {
	_, _, ok := parseCloudMediaPlaybackURL(raw)
	return ok
}

// ServeHLSPlaylist makes sure a transcode is running and writes the m3u8.
// We block (with a 30s timeout) until the playlist file shows up.
func (s *StreamService) ServeHLSPlaylist(w http.ResponseWriter, r *http.Request, mediaID string) error {
	// 「客户端直连解码」模式下宿主机不提供转码，HLS 一律拒绝，
	// 迫使播放器走 direct play 本地解码。
	if s.directPlayOnly(r.Context()) {
		return ErrTranscodeDisabled
	}
	if _, err := s.transcoder.EnsureJob(r.Context(), mediaID); err != nil {
		return err
	}
	s.transcoder.TouchJob(mediaID)
	if !s.transcoder.WaitReady(r.Context(), mediaID, 30*time.Second) {
		return errors.New("hls playlist not ready")
	}
	playlist := s.transcoder.PlaylistPath(mediaID)
	f, err := os.Open(playlist) // #nosec G304 -- playlist path is generated under the transcoder cache directory for this media ID.
	if err != nil {
		return err
	}
	defer f.Close()
	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Disposition", "inline")
	if r.URL.RawQuery != "" {
		data, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		playlist := appendQueryToHLSSegments(string(data), r.URL.RawQuery)
		_, err = io.WriteString(w, playlist)
		return err
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}

func appendQueryToHLSSegments(playlist, rawQuery string) string {
	if strings.TrimSpace(rawQuery) == "" {
		return playlist
	}
	lines := strings.SplitAfter(playlist, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.Contains(trimmed, "?") {
			continue
		}
		if strings.HasSuffix(strings.ToLower(trimmed), ".ts") {
			lineEnding := ""
			if strings.HasSuffix(line, "\r\n") {
				lineEnding = "\r\n"
			} else if strings.HasSuffix(line, "\n") {
				lineEnding = "\n"
			}
			lines[i] = strings.TrimRight(line, "\r\n") + "?" + rawQuery + lineEnding
		}
	}
	return strings.Join(lines, "")
}

// ServeHLSSegment writes a single .ts segment from the on-disk cache.
func (s *StreamService) ServeHLSSegment(w http.ResponseWriter, r *http.Request, mediaID, segment string) error {
	s.transcoder.TouchJob(mediaID)
	// Only allow segments that look like seg_NNNNN.ts so we cannot be tricked
	// into reading arbitrary files via path traversal.
	if !strings.HasPrefix(segment, "seg_") || !strings.HasSuffix(segment, ".ts") {
		return errors.New("bad segment")
	}
	full := filepath.Join(s.transcoder.HLSDir(mediaID), segment)
	abs, err := filepath.Abs(full)
	if err != nil {
		return err
	}
	dir, _ := filepath.Abs(s.transcoder.HLSDir(mediaID))
	if !pathWithin(abs, dir) {
		return errors.New("path escape")
	}
	f, err := os.Open(abs) // #nosec G304 -- abs is constrained to the HLS cache directory with pathWithin.
	if err != nil {
		return err
	}
	defer f.Close()
	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	return nil
}

// Probe re-runs ffprobe against an existing media row and refreshes the
// extracted metadata. Used by the admin UI's "rescan" button.
func (s *StreamService) Probe(ctx context.Context, mediaID string, probe *FFprobeService) error {
	m, err := s.repo.Media.FindByID(ctx, mediaID)
	if err != nil || m == nil {
		return ErrMediaNotFound
	}
	res, err := probe.Probe(ctx, m.Path)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"duration_sec": res.DurationSec,
		"width":        res.Width,
		"height":       res.Height,
		"video_codec":  res.VideoCodec,
		"audio_codec":  res.AudioCodec,
	}
	if res.Container != "" {
		updates["container"] = res.Container
	}
	return s.repo.DB.Model(m).Updates(updates).Error
}

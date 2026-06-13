// Package service — STRM 文件管理服务。
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// STRM 错误定义。
var (
	ErrSTRMNotFound        = errors.New("strm record not found")
	ErrSTRMProtocolInvalid = errors.New("invalid strm protocol")
	ErrSTRMURLInvalid      = errors.New("invalid strm url")
)

// STRMService STRM 文件管理服务。
type STRMService struct {
	log  *zap.Logger
	repo *repository.Container
	cfg  *config.Config
}

type GenerateSTRMOptions struct {
	LibraryID     string `json:"library_id"`
	OutputDir     string `json:"output_dir"`
	BaseURL       string `json:"base_url,omitempty"`
	Enabled       bool   `json:"enabled"`
	Overwrite     bool   `json:"overwrite"`
	IncludeLocal  bool   `json:"include_local"`
	PlaybackToken string `json:"-"`
}

type GenerateSTRMResult struct {
	LibraryID string             `json:"library_id"`
	OutputDir string             `json:"output_dir"`
	Generated int                `json:"generated"`
	Updated   int                `json:"updated"`
	Skipped   int                `json:"skipped"`
	Errors    []string           `json:"errors,omitempty"`
	Items     []GenerateSTRMItem `json:"items,omitempty"`
}

type GenerateSTRMItem struct {
	MediaID  string `json:"media_id"`
	Title    string `json:"title"`
	FilePath string `json:"file_path"`
	URL      string `json:"url,omitempty"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
}

// NewSTRMService 创建 STRM 服务。
func NewSTRMService(log *zap.Logger, repo *repository.Container, cfg *config.Config) *STRMService {
	return &STRMService{log: log, repo: repo, cfg: cfg}
}

func (s *STRMService) GenerateForLibrary(ctx context.Context, opts GenerateSTRMOptions) (*GenerateSTRMResult, error) {
	if s == nil || s.repo == nil || s.repo.DB == nil {
		return nil, errors.New("strm service unavailable")
	}
	libraryID := strings.TrimSpace(opts.LibraryID)
	if libraryID == "" {
		return nil, errors.New("library_id required")
	}
	lib, err := s.repo.Library.FindByID(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if lib == nil {
		return nil, errors.New("library not found")
	}
	outputDir := resolveMappedDestinationPath(strings.TrimSpace(opts.OutputDir))
	if (outputDir == "" || outputDir == ".") && s.repo.Setting != nil {
		if saved, err := s.repo.Setting.Get(ctx, "strm.output_dir"); err == nil {
			outputDir = resolveMappedDestinationPath(strings.TrimSpace(saved))
		}
	}
	if outputDir == "" || outputDir == "." {
		outputDir = s.defaultOutputDir(lib)
	}
	if outputDir == "" || outputDir == "." {
		return nil, errors.New("output_dir required")
	}
	if strings.TrimSpace(opts.BaseURL) != "" && s.repo.Setting != nil {
		baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
		_ = s.repo.Setting.Set(ctx, "app.server_url", baseURL)
		_ = s.repo.Setting.Set(ctx, "strm.base_url", baseURL)
	}
	if s.repo.Setting != nil {
		_ = s.repo.Setting.Set(ctx, "strm.auto_generate_enabled", strconv.FormatBool(opts.Enabled))
		_ = s.repo.Setting.Set(ctx, "strm.output_dir", outputDir)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil { // #nosec G301 -- STRM output directories must stay readable by NAS/player users.
		return nil, err
	}

	var rows []model.Media
	if err := s.repo.DB.WithContext(ctx).
		Where("library_id = ?", libraryID).
		Order("title asc, season_num asc, episode_num asc, created_at asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	res := &GenerateSTRMResult{LibraryID: libraryID, OutputDir: outputDir}
	for _, media := range rows {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		default:
		}
		item := s.generateOne(ctx, *lib, media, outputDir, opts)
		res.Items = append(res.Items, item)
		switch item.Action {
		case "generated":
			res.Generated++
		case "updated":
			res.Updated++
		case "skipped":
			res.Skipped++
		case "error":
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %s", item.Title, item.Reason))
		}
	}
	return res, nil
}

func (s *STRMService) defaultOutputDir(lib *model.Library) string {
	if s != nil && s.cfg != nil && strings.TrimSpace(s.cfg.App.DataDir) != "" {
		return filepath.Join(s.cfg.App.DataDir, "strm", sanitizeFilename(lib.Name))
	}
	return filepath.Join("data", "strm", sanitizeFilename(lib.Name))
}

func (s *STRMService) generateOne(ctx context.Context, lib model.Library, media model.Media, outputDir string, opts GenerateSTRMOptions) GenerateSTRMItem {
	item := GenerateSTRMItem{MediaID: media.ID, Title: media.Title}
	playURL := s.strmPlaybackURL(ctx, media, opts.BaseURL, opts.PlaybackToken)
	if playURL == "" {
		item.Action = "skipped"
		item.Reason = "no playable strm target"
		return item
	}
	if strings.TrimSpace(media.STRMURL) == "" && !opts.IncludeLocal {
		item.Action = "skipped"
		item.Reason = "local media skipped"
		return item
	}
	rel := s.strmRelativePath(lib, media)
	if rel == "" {
		item.Action = "skipped"
		item.Reason = "cannot build file name"
		return item
	}
	filePath := filepath.Join(outputDir, rel)
	item.FilePath = filePath
	item.URL = playURL
	if _, err := os.Stat(filePath); err == nil && !opts.Overwrite {
		item.Action = "skipped"
		item.Reason = "target exists"
		return item
	}
	action := "generated"
	if _, err := os.Stat(filePath); err == nil {
		action = "updated"
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil { // #nosec G301 -- STRM output directories must stay readable by NAS/player users.
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	if err := os.WriteFile(filePath, []byte(playURL+"\n"), 0o644); err != nil { // #nosec G306 -- STRM files are media sidecars intended to be readable by players.
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	if err := s.upsertGeneratedRecord(ctx, media, filePath, playURL, lib.Type); err != nil {
		item.Action = "error"
		item.Reason = err.Error()
		return item
	}
	item.Action = action
	return item
}

func (s *STRMService) strmPlaybackURL(ctx context.Context, media model.Media, baseURL, playbackToken string) string {
	if media.ID == "" {
		return ""
	}
	query := url.Values{}
	token := strings.TrimSpace(playbackToken)
	if token == "" {
		token = s.defaultSTRMPlaybackToken(ctx)
	}
	if token != "" {
		query.Set("token", token)
	}
	return buildAbsoluteSTRMAPIURL(firstNonEmpty(baseURL, PublicServerURL(ctx, s.repo, s.cfg)), "/api/stream/"+url.PathEscape(media.ID), query)
}

func (s *STRMService) defaultSTRMPlaybackToken(ctx context.Context) string {
	if s == nil || s.repo == nil || s.repo.User == nil || s.cfg == nil || strings.TrimSpace(s.cfg.Secrets.JWTSecret) == "" {
		return ""
	}
	admin, err := s.repo.User.FirstAdmin(ctx)
	if err != nil || admin == nil {
		if err != nil && s.log != nil {
			s.log.Warn("generate strm playback token failed", zap.Error(err))
		}
		return ""
	}
	token, err := signSTRMPlaybackToken(admin, s.cfg.Secrets.JWTSecret)
	if err != nil {
		if s.log != nil {
			s.log.Warn("sign strm playback token failed", zap.Error(err))
		}
		return ""
	}
	return token
}

func signSTRMPlaybackToken(u *model.User, secret string) (string, error) {
	if u == nil || strings.TrimSpace(u.ID) == "" || strings.TrimSpace(secret) == "" {
		return "", ErrSTRMURLInvalid
	}
	claims := Claims{
		UserID: u.ID,
		Role:   u.Role,
		Tier:   u.Tier,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(EmbyTokenDuration)),
			Issuer:    "mediastationgo",
			Subject:   u.ID,
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
}

func (s *STRMService) strmRelativePath(lib model.Library, media model.Media) string {
	title := strings.TrimSpace(media.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(media.Path), filepath.Ext(media.Path))
	}
	if title == "" {
		return ""
	}
	seriesLike := isSeriesLibraryType(lib.Type) || media.SeasonNum > 0 || media.EpisodeNum > 0
	if seriesLike {
		show := inferSeriesNameFromPath(media.Path)
		if show == "" {
			show = title
		}
		season := media.SeasonNum
		if season <= 0 {
			season = 1
		}
		name := title
		if media.EpisodeNum > 0 {
			name = fmt.Sprintf("%s - S%02dE%02d", show, season, media.EpisodeNum)
		}
		return filepath.Join(sanitizeFilename(show), fmt.Sprintf("Season %02d", season), sanitizeFilename(name)+".strm")
	}
	folder := title
	if media.Year > 0 && !strings.Contains(folder, strconv.Itoa(media.Year)) {
		folder = fmt.Sprintf("%s (%d)", folder, media.Year)
	}
	safe := sanitizeFilename(folder)
	return filepath.Join(safe, safe+".strm")
}

func (s *STRMService) upsertGeneratedRecord(ctx context.Context, media model.Media, filePath, playURL, mediaType string) error {
	protocol := ""
	if u, err := url.Parse(playURL); err == nil {
		protocol = strings.ToLower(u.Scheme)
	}
	if protocol == "" {
		protocol = "http"
	}
	record := model.STRMRecord{
		Title:      media.Title,
		URL:        playURL,
		FilePath:   filePath,
		Protocol:   protocol,
		MediaID:    media.ID,
		MediaType:  mediaType,
		SeasonNum:  media.SeasonNum,
		EpisodeNum: media.EpisodeNum,
	}
	var existing model.STRMRecord
	err := s.repo.DB.WithContext(ctx).Where("media_id = ? AND file_path = ?", media.ID, filePath).First(&existing).Error
	if err == nil {
		existing.Title = record.Title
		existing.URL = record.URL
		existing.Protocol = record.Protocol
		existing.MediaType = record.MediaType
		existing.SeasonNum = record.SeasonNum
		existing.EpisodeNum = record.EpisodeNum
		return s.repo.DB.WithContext(ctx).Save(&existing).Error
	}
	return s.repo.DB.WithContext(ctx).Create(&record).Error
}

func absolutizeSTRMURL(raw, baseURL string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "//") {
		return raw
	}
	u, err := url.Parse(raw)
	if err == nil && u.IsAbs() {
		return raw
	}
	return buildAbsoluteSTRMAPIURL(baseURL, raw, nil)
}

func buildAbsoluteSTRMAPIURL(baseURL, apiPath string, query url.Values) string {
	apiPath = "/" + strings.TrimLeft(strings.TrimSpace(apiPath), "/")
	if query != nil && len(query) > 0 {
		apiPath += "?" + query.Encode()
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return apiPath
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return apiPath
	}
	target, err := url.Parse(apiPath)
	if err != nil {
		return apiPath
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(target.Path, "/")
	base.RawQuery = target.RawQuery
	base.Fragment = ""
	return base.String()
}

// Create 创建 STRM 记录。
func (s *STRMService) Create(ctx context.Context, record *model.STRMRecord) (*model.STRMRecord, error) {
	if err := s.validateSTRM(record); err != nil {
		return nil, err
	}

	if err := s.repo.STRM.Create(ctx, record); err != nil {
		s.log.Error("create strm failed", zap.Error(err))
		return nil, err
	}

	return record, nil
}

// CreateBatch 批量创建 STRM 记录。
func (s *STRMService) CreateBatch(ctx context.Context, records []model.STRMRecord) (int, error) {
	created := 0
	for i := range records {
		if err := s.validateSTRM(&records[i]); err != nil {
			s.log.Warn("skip invalid strm record",
				zap.String("title", records[i].Title),
				zap.Error(err),
			)
			continue
		}
		created++
	}

	validRecords := make([]model.STRMRecord, 0, created)
	for _, r := range records {
		if model.IsAllowedProtocol(r.Protocol) && r.URL != "" {
			validRecords = append(validRecords, r)
		}
	}

	if len(validRecords) == 0 {
		return 0, nil
	}

	if err := s.repo.STRM.CreateBatch(ctx, validRecords); err != nil {
		s.log.Error("batch create strm failed", zap.Error(err))
		return 0, err
	}

	return len(validRecords), nil
}

// GetByID 获取 STRM 记录。
func (s *STRMService) GetByID(ctx context.Context, id string) (*model.STRMRecord, error) {
	record, err := s.repo.STRM.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrSTRMNotFound
	}
	return record, nil
}

// List 列出 STRM 记录（支持筛选和分页）。
func (s *STRMService) List(ctx context.Context, filters map[string]string, page, pageSize int) ([]model.STRMRecord, int64, error) {
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	records, total, err := s.repo.STRM.List(ctx, filters, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// Update 更新 STRM 记录。
func (s *STRMService) Update(ctx context.Context, record *model.STRMRecord) (*model.STRMRecord, error) {
	existing, err := s.repo.STRM.FindByID(ctx, record.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrSTRMNotFound
	}

	if record.Protocol != "" {
		if !model.IsAllowedProtocol(record.Protocol) {
			return nil, ErrSTRMProtocolInvalid
		}
	}

	if err := s.repo.STRM.Update(ctx, record); err != nil {
		s.log.Error("update strm failed", zap.Error(err))
		return nil, err
	}

	return record, nil
}

// Delete 删除 STRM 记录。
func (s *STRMService) Delete(ctx context.Context, id string) error {
	existing, err := s.repo.STRM.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrSTRMNotFound
	}
	return s.repo.STRM.Delete(ctx, id)
}

// GetProtocols 获取支持的协议列表。
func (s *STRMService) GetProtocols() []string {
	return model.AllowedSTRMProtocols
}

// ProxySTRM 代理访问 STRM 资源。
// 支持 Range 请求（206 Partial Content）。
func (s *STRMService) ProxySTRM(ctx context.Context, id string, req *http.Request, w http.ResponseWriter) error {
	record, err := s.repo.STRM.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if record == nil {
		return ErrSTRMNotFound
	}

	if !model.IsAllowedProtocol(record.Protocol) {
		return ErrSTRMProtocolInvalid
	}

	targetURL, err := validateSTRMProxyURL(record.URL)
	if err != nil {
		return err
	}

	// 创建代理请求
	proxyReq, err := http.NewRequestWithContext(ctx, req.Method, targetURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create proxy request: %w", err)
	}

	// 复制 Range 等关键请求头
	for _, header := range []string{
		"Range", "If-Range", "If-Match", "If-None-Match",
		"If-Modified-Since", "If-Unmodified-Since",
		"Accept", "Accept-Encoding", "Accept-Language",
	} {
		if v := req.Header.Get(header); v != "" {
			proxyReq.Header.Set(header, v)
		}
	}

	// 对 alist/webdav 协议可能需要特殊处理认证
	if record.Protocol == "alist" || record.Protocol == "alists" {
		// alist 协议可以直接访问，无需额外认证
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(proxyReq) // #nosec G107,G704 -- STRM proxy target is validated by validateSTRMProxyURL before request creation.
	if err != nil {
		return fmt.Errorf("proxy request failed: %w", err)
	}
	defer resp.Body.Close()

	// 复制响应头
	for _, header := range []string{
		"Content-Type", "Content-Length", "Content-Range",
		"Accept-Ranges", "Last-Modified", "ETag",
		"Cache-Control", "Content-Disposition",
	} {
		if v := resp.Header.Get(header); v != "" {
			w.Header().Set(header, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	return err
}

func validateSTRMProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, ErrSTRMURLInvalid
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, ErrSTRMProtocolInvalid
	}
	if isPrivateHost(u.Hostname()) {
		return nil, ErrSTRMURLInvalid
	}
	return u, nil
}

// validateSTRM 验证 STRM 记录。
func (s *STRMService) validateSTRM(record *model.STRMRecord) error {
	if record.Title == "" {
		return errors.New("title is required")
	}
	if record.URL == "" {
		return ErrSTRMURLInvalid
	}
	if !model.IsAllowedProtocol(record.Protocol) {
		return ErrSTRMProtocolInvalid
	}

	// 标准化协议名
	record.Protocol = strings.ToLower(record.Protocol)

	return nil
}

// ListByMediaID 获取关联到指定媒体的 STRM 记录。
func (s *STRMService) ListByMediaID(ctx context.Context, mediaID string) ([]model.STRMRecord, error) {
	return s.repo.STRM.FindByMediaID(ctx, mediaID)
}

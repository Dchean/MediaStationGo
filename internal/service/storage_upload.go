package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	CloudUploadAutoEnabledKey      = "cloud.upload_auto_enabled"
	CloudUploadProviderKey         = "cloud.upload_provider"
	CloudUploadSourceDirKey        = "cloud.upload_source_dir"
	CloudUploadDestPathKey         = "cloud.upload_dest_path"
	CloudUploadRecursiveKey        = "cloud.upload_recursive"
	CloudUploadSidecarsKey         = "cloud.upload_sidecars"
	CloudUploadOverwriteKey        = "cloud.upload_overwrite"
	CloudUploadIntervalSecondsKey  = "cloud.upload_interval_seconds"
	CloudUploadUnsupportedProvider = "本地文件直传目前支持 Alist / OpenList / WebDAV / CloudDrive2；115/夸克原生上传需要各自的分片上传私有接口，建议先用 CloudDrive2、OpenList 或 Alist 桥接后转存。"
)

type CloudUploadInput struct {
	Type            string `json:"type"`
	SourcePath      string `json:"source_path"`
	DestPath        string `json:"dest_path"`
	Recursive       bool   `json:"recursive"`
	IncludeSidecars bool   `json:"include_sidecars"`
	Overwrite       bool   `json:"overwrite"`
}

type CloudUploadResult struct {
	SourcePath string                  `json:"source_path"`
	DestPath   string                  `json:"dest_path"`
	Uploaded   int                     `json:"uploaded"`
	Skipped    int                     `json:"skipped"`
	Bytes      int64                   `json:"bytes"`
	Errors     []string                `json:"errors,omitempty"`
	Items      []CloudUploadResultItem `json:"items,omitempty"`
}

type CloudUploadResultItem struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Action string `json:"action"` // upload / skip / error
	Size   int64  `json:"size,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type storageUploader interface {
	ensureDir(ctx context.Context, remoteDir string) error
	exists(ctx context.Context, remotePath string) (bool, error)
	upload(ctx context.Context, localPath, remotePath string, size int64) error
}

var cloudUploadSidecarExtensions = map[string]struct{}{
	".nfo": {}, ".jpg": {}, ".jpeg": {}, ".png": {}, ".webp": {},
	".srt": {}, ".ass": {}, ".ssa": {}, ".vtt": {}, ".sub": {}, ".idx": {},
}

// UploadLocal copies local media files into an external storage backend. It is
// intentionally conservative: it never deletes local files, skips existing
// remote targets unless Overwrite is set, and only uploads video + common
// sidecar metadata files.
func (s *StorageConfigService) UploadLocal(ctx context.Context, in CloudUploadInput) (*CloudUploadResult, error) {
	in.Type = strings.TrimSpace(in.Type)
	in.SourcePath = strings.TrimSpace(in.SourcePath)
	in.DestPath = normalizeRemotePath(in.DestPath)
	if in.SourcePath == "" {
		return nil, errors.New("source_path required")
	}
	uploader, err := s.uploader(ctx, in.Type)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(in.SourcePath)
	if err != nil {
		return nil, fmt.Errorf("source path not accessible: %w", err)
	}
	result := &CloudUploadResult{SourcePath: in.SourcePath, DestPath: in.DestPath}
	if !info.IsDir() {
		s.uploadOne(ctx, uploader, in, in.SourcePath, filepath.Base(in.SourcePath), info.Size(), result)
		return result, firstUploadError(result)
	}
	root := filepath.Clean(in.SourcePath)
	walkFn := func(localPath string, entryInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			addUploadError(result, localPath, "", walkErr)
			return nil
		}
		if entryInfo == nil || entryInfo.IsDir() {
			if !in.Recursive && filepath.Clean(localPath) != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !eligibleCloudUploadFile(localPath, in.IncludeSidecars) {
			return nil
		}
		rel, err := filepath.Rel(root, localPath)
		if err != nil {
			addUploadError(result, localPath, "", err)
			return nil
		}
		s.uploadOne(ctx, uploader, in, localPath, filepath.ToSlash(rel), entryInfo.Size(), result)
		return nil
	}
	if err := filepath.Walk(in.SourcePath, walkFn); err != nil {
		return result, err
	}
	return result, firstUploadError(result)
}

func (s *StorageConfigService) uploader(ctx context.Context, typ string) (storageUploader, error) {
	view, err := s.Get(ctx, typ)
	if err != nil {
		return nil, err
	}
	if view == nil || !view.Enabled {
		return nil, fmt.Errorf("%s storage not configured", typ)
	}
	switch typ {
	case "alist":
		return newAlistUploader(view.Config), nil
	case "openlist":
		if strings.TrimSpace(strr(view.Config["server"])) != "" {
			return newNamedAlistUploader("openlist", view.Config), nil
		}
		return newWebDAVUploader(view.Config), nil
	case "webdav":
		return newWebDAVUploader(view.Config), nil
	case "clouddrive2":
		return newWebDAVUploader(view.Config), nil
	case "s3":
		return nil, errors.New("s3 local upload is not implemented yet")
	case "cloud115", "quark":
		return nil, errors.New(CloudUploadUnsupportedProvider)
	default:
		return nil, fmt.Errorf("unsupported storage type %q", typ)
	}
}

func (s *StorageConfigService) uploadOne(ctx context.Context, uploader storageUploader, in CloudUploadInput, localPath, rel string, size int64, result *CloudUploadResult) {
	remotePath := joinRemotePath(in.DestPath, rel)
	if err := uploader.ensureDir(ctx, path.Dir(remotePath)); err != nil {
		addUploadError(result, localPath, remotePath, err)
		return
	}
	if !in.Overwrite {
		exists, err := uploader.exists(ctx, remotePath)
		if err != nil {
			addUploadError(result, localPath, remotePath, err)
			return
		}
		if exists {
			result.Skipped++
			addUploadItem(result, CloudUploadResultItem{Source: localPath, Target: remotePath, Action: "skip", Size: size, Reason: "remote exists"})
			return
		}
	}
	if err := uploader.upload(ctx, localPath, remotePath, size); err != nil {
		addUploadError(result, localPath, remotePath, err)
		return
	}
	result.Uploaded++
	result.Bytes += size
	addUploadItem(result, CloudUploadResultItem{Source: localPath, Target: remotePath, Action: "upload", Size: size})
}

func eligibleCloudUploadFile(localPath string, includeSidecars bool) bool {
	ext := strings.ToLower(filepath.Ext(localPath))
	if _, ok := videoExtensions[ext]; ok {
		return true
	}
	if includeSidecars {
		_, ok := cloudUploadSidecarExtensions[ext]
		return ok
	}
	return false
}

func addUploadError(result *CloudUploadResult, source, target string, err error) {
	result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", source, err))
	addUploadItem(result, CloudUploadResultItem{Source: source, Target: target, Action: "error", Reason: err.Error()})
}

func addUploadItem(result *CloudUploadResult, item CloudUploadResultItem) {
	if len(result.Items) < 200 {
		result.Items = append(result.Items, item)
	}
}

func firstUploadError(result *CloudUploadResult) error {
	if result.Uploaded > 0 || len(result.Errors) == 0 {
		return nil
	}
	return errors.New(result.Errors[0])
}

func normalizeRemotePath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" || p == "." {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func joinRemotePath(base, rel string) string {
	parts := []string{normalizeRemotePath(base)}
	for _, part := range strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/") {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return path.Clean(path.Join(parts...))
}

type alistUploader struct {
	name   string
	server string
	token  string
	client *http.Client
}

func newAlistUploader(cfg map[string]any) *alistUploader {
	return newNamedAlistUploader("alist", cfg)
}

func newNamedAlistUploader(name string, cfg map[string]any) *alistUploader {
	return &alistUploader{
		name:   name,
		server: strings.TrimRight(strr(cfg["server"]), "/"),
		token:  strr(cfg["token"]),
		client: &http.Client{},
	}
}

func (a *alistUploader) ensureDir(ctx context.Context, remoteDir string) error {
	if a.server == "" {
		return fmt.Errorf("%s missing server", a.name)
	}
	remoteDir = normalizeRemotePath(remoteDir)
	if remoteDir == "/" {
		return nil
	}
	current := ""
	for _, part := range strings.Split(strings.Trim(remoteDir, "/"), "/") {
		current = normalizeRemotePath(path.Join(current, part))
		payload, _ := json.Marshal(map[string]string{"path": current})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.server+"/api/fs/mkdir", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		a.auth(req)
		req.Header.Set("Content-Type", "application/json")
		resp, err := a.client.Do(req)
		if err != nil {
			return decorateStorageTransportError(a.name, a.server, err)
		}
		err = a.checkJSON(resp, "alist mkdir")
		if err != nil && !isAlreadyExistsMessage(err.Error()) {
			return err
		}
	}
	return nil
}

func (a *alistUploader) exists(ctx context.Context, remotePath string) (bool, error) {
	payload, _ := json.Marshal(map[string]string{"path": normalizeRemotePath(remotePath)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.server+"/api/fs/get", bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	a.auth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return false, decorateStorageTransportError(a.name, a.server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode >= 200 && resp.StatusCode < 300 && out.Code == 200, nil
}

func (a *alistUploader) upload(ctx context.Context, localPath, remotePath string, size int64) error {
	f, err := os.Open(localPath) // #nosec G304 -- localPath is selected from configured local media files before upload.
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, a.server+"/api/fs/put", f)
	if err != nil {
		return err
	}
	a.auth(req)
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("File-Path", url.PathEscape(normalizeRemotePath(remotePath)))
	resp, err := a.client.Do(req)
	if err != nil {
		return decorateStorageTransportError(a.name, a.server, err)
	}
	return a.checkJSON(resp, "alist upload")
}

func (a *alistUploader) auth(req *http.Request) {
	if a.token != "" {
		req.Header.Set("Authorization", a.token)
	}
}

func (a *alistUploader) checkJSON(resp *http.Response, op string) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: http %d: %s", op, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	if out.Code != 0 && out.Code != 200 {
		return fmt.Errorf("%s: %s", op, out.Message)
	}
	return nil
}

type webDAVUploader struct {
	base     *url.URL
	username string
	password string
	client   *http.Client
}

func newWebDAVUploader(cfg map[string]any) *webDAVUploader {
	u, _ := url.Parse(strings.TrimRight(strr(cfg["url"]), "/"))
	return &webDAVUploader{
		base:     u,
		username: strr(cfg["username"]),
		password: strr(cfg["password"]),
		client:   &http.Client{},
	}
}

func (w *webDAVUploader) ensureDir(ctx context.Context, remoteDir string) error {
	if w.base == nil || w.base.Scheme == "" || w.base.Host == "" {
		return errors.New("webdav missing url")
	}
	remoteDir = normalizeRemotePath(remoteDir)
	if remoteDir == "/" {
		return nil
	}
	current := ""
	for _, part := range strings.Split(strings.Trim(remoteDir, "/"), "/") {
		current = normalizeRemotePath(path.Join(current, part))
		req, err := http.NewRequestWithContext(ctx, "MKCOL", w.urlFor(current), nil)
		if err != nil {
			return err
		}
		w.auth(req)
		resp, err := w.client.Do(req)
		if err != nil {
			return decorateStorageTransportError("webdav", w.urlFor(current), err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			continue
		}
		if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusConflict {
			continue
		}
		return fmt.Errorf("webdav mkdir %s: http %d", current, resp.StatusCode)
	}
	return nil
}

func (w *webDAVUploader) exists(ctx context.Context, remotePath string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, w.urlFor(remotePath), nil)
	if err != nil {
		return false, err
	}
	w.auth(req)
	resp, err := w.client.Do(req)
	if err != nil {
		return false, decorateStorageTransportError("webdav", w.urlFor(remotePath), err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

func (w *webDAVUploader) upload(ctx context.Context, localPath, remotePath string, size int64) error {
	f, err := os.Open(localPath) // #nosec G304 -- localPath is selected from configured local media files before upload.
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, w.urlFor(remotePath), f)
	if err != nil {
		return err
	}
	w.auth(req)
	req.ContentLength = size
	resp, err := w.client.Do(req)
	if err != nil {
		return decorateStorageTransportError("webdav", w.urlFor(remotePath), err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webdav upload %s: http %d", remotePath, resp.StatusCode)
	}
	return nil
}

func (w *webDAVUploader) auth(req *http.Request) {
	if w.username != "" {
		req.SetBasicAuth(w.username, w.password)
	}
}

func (w *webDAVUploader) urlFor(remotePath string) string {
	u := *w.base
	basePath := strings.TrimRight(u.EscapedPath(), "/")
	segments := make([]string, 0)
	if basePath != "" && basePath != "/" {
		segments = append(segments, strings.Trim(basePath, "/"))
	}
	for _, part := range strings.Split(strings.Trim(normalizeRemotePath(remotePath), "/"), "/") {
		if part != "" {
			segments = append(segments, url.PathEscape(part))
		}
	}
	u.RawPath = ""
	u.Path = "/" + strings.Join(segments, "/")
	return u.String()
}

func isAlreadyExistsMessage(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "exist") || strings.Contains(message, "已存在")
}

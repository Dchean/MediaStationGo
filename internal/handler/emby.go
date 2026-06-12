// Package handler — Emby/Jellyfin compatibility shim.
//
// 路由挂在 /emby/* 和根路径下双前缀。Infuse / Yamby / Hills /
// Senplayer / Kodi 这类客户端会自动尝试 /System/Info 与 /emby/System/Info
// 两种 URL，我们都接住。
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ShukeBta/MediaStationGo/internal/middleware"
	"github.com/ShukeBta/MediaStationGo/internal/service"
)

// embyError 返回 Emby 风格的错误（顶层 Code/Message）。
func embyError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"Code": status, "Message": msg})
}

// embyUserID 从中间件中获取 user id。Emby auth middleware 写入 CtxUserID。
func embyUserID(c *gin.Context) string {
	if uid, ok := c.Get(middleware.CtxUserID); ok {
		if s, ok := uid.(string); ok {
			return s
		}
	}
	return ""
}

// ─── System ──────────────────────────────────────────────────────────────────

func embySystemInfoHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, embyWithRequestAddress(c, svc.Emby.SystemInfo()))
	}
}

func embySystemInfoPublicHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, embyWithRequestAddress(c, svc.Emby.SystemInfoPublic()))
	}
}

func embyRequestBaseURL(c *gin.Context) string {
	proto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
	if proto == "" {
		if c.Request != nil && c.Request.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	if comma := strings.Index(proto, ","); comma >= 0 {
		proto = strings.TrimSpace(proto[:comma])
	}

	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" && c.Request != nil {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host == "" {
		return ""
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func embyWithRequestAddress(c *gin.Context, payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload)+2)
	for key, value := range payload {
		out[key] = value
	}
	if address := embyRequestBaseURL(c); address != "" {
		out["LocalAddress"] = address
		out["WanAddress"] = address
		out["PublishedServerUrl"] = address
	}
	return out
}

func embySystemEndpointHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"IsLocal":     true,
			"IsInNetwork": true,
		})
	}
}

func embyPingHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Emby/Jellyfin 期望 plain text "Emby Server"
		c.String(http.StatusOK, "Emby Server")
	}
}

func embyRootHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, embyPublicSystemInfoPayload(c, svc))
	}
}

func embyPublicSystemInfoPayload(c *gin.Context, svc *service.Container) map[string]any {
	if svc != nil && svc.Emby != nil {
		return embyWithRequestAddress(c, svc.Emby.SystemInfoPublic())
	}
	return embyWithRequestAddress(c, map[string]any{
		"Id":                     "mediastation-go-001",
		"ServerId":               "mediastation-go-001",
		"ServerName":             "MediaStationGo",
		"Version":                "4.8.10.0",
		"ServerVersion":          "4.8.10.0",
		"ProductName":            "Emby Server",
		"OperatingSystem":        "Windows",
		"SupportsHttps":          false,
		"SupportsAutoDiscovery":  true,
		"StartupWizardCompleted": true,
	})
}

// ─── Users / Auth ────────────────────────────────────────────────────────────

type embyAuthByNameReq struct {
	Username     string `json:"Username"`
	Pw           string `json:"Pw"`
	Password     string `json:"Password"`
	PasswordMd5  string `json:"PasswordMd5"`
	PasswordSha1 string `json:"PasswordSha1"`
}

func parseEmbyAuthByNameReq(c *gin.Context) (embyAuthByNameReq, error) {
	req := embyAuthByNameReq{}
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "json") {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
			return req, err
		}
		fillEmbyAuthFromMap(&req, body)
	}

	if req.Username == "" || (req.Pw == "" && req.Password == "" && req.PasswordMd5 == "" && req.PasswordSha1 == "") {
		_ = c.Request.ParseForm()
		if req.Username == "" {
			req.Username = firstFormValue(c, "Username", "username", "Name", "name")
		}
		if req.Pw == "" {
			req.Pw = firstFormValue(c, "Pw", "pw")
		}
		if req.Password == "" {
			req.Password = firstFormValue(c, "Password", "password")
		}
		if req.PasswordMd5 == "" {
			req.PasswordMd5 = firstFormValue(c, "PasswordMd5", "passwordMd5", "password_md5")
		}
		if req.PasswordSha1 == "" {
			req.PasswordSha1 = firstFormValue(c, "PasswordSha1", "passwordSha1", "password_sha1")
		}
	}

	if req.Username == "" {
		req.Username = firstQueryValue(c, "Username", "username", "Name", "name")
	}
	if req.Pw == "" {
		req.Pw = firstQueryValue(c, "Pw", "pw")
	}
	if req.Password == "" {
		req.Password = firstQueryValue(c, "Password", "password")
	}
	if req.PasswordMd5 == "" {
		req.PasswordMd5 = firstQueryValue(c, "PasswordMd5", "passwordMd5", "password_md5")
	}
	if req.PasswordSha1 == "" {
		req.PasswordSha1 = firstQueryValue(c, "PasswordSha1", "passwordSha1", "password_sha1")
	}
	if req.Username == "" || (req.Pw == "" && req.Password == "" && req.PasswordMd5 == "" && req.PasswordSha1 == "") {
		fillEmbyAuthFromRawBody(c, &req)
	}
	return req, nil
}

func fillEmbyAuthFromMap(req *embyAuthByNameReq, body map[string]any) {
	if req.Username == "" {
		req.Username = firstStringFromMap(body, "Username", "username", "UserName", "userName", "Name", "name", "LoginName", "loginName")
	}
	if req.Pw == "" {
		req.Pw = firstStringFromMap(body, "Pw", "pw", "PW")
	}
	if req.Password == "" {
		req.Password = firstStringFromMap(body, "Password", "password", "Pass", "pass", "Pwd", "pwd")
	}
	if req.PasswordMd5 == "" {
		req.PasswordMd5 = firstStringFromMap(body, "PasswordMd5", "passwordMd5", "password_md5")
	}
	if req.PasswordSha1 == "" {
		req.PasswordSha1 = firstStringFromMap(body, "PasswordSha1", "passwordSha1", "password_sha1")
	}
}

func fillEmbyAuthFromRawBody(c *gin.Context, req *embyAuthByNameReq) {
	if c.Request == nil || c.Request.Body == nil {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}
	if bytes.HasPrefix(raw, []byte("{")) {
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err == nil {
			fillEmbyAuthFromMap(req, body)
		}
		return
	}
	if values, err := url.ParseQuery(string(raw)); err == nil {
		fillEmbyAuthFromValues(req, values)
	}
}

func fillEmbyAuthFromValues(req *embyAuthByNameReq, values url.Values) {
	if req.Username == "" {
		req.Username = firstValue(values, "Username", "username", "UserName", "userName", "Name", "name", "LoginName", "loginName")
	}
	if req.Pw == "" {
		req.Pw = firstValue(values, "Pw", "pw", "PW")
	}
	if req.Password == "" {
		req.Password = firstValue(values, "Password", "password", "Pass", "pass", "Pwd", "pwd")
	}
	if req.PasswordMd5 == "" {
		req.PasswordMd5 = firstValue(values, "PasswordMd5", "passwordMd5", "password_md5")
	}
	if req.PasswordSha1 == "" {
		req.PasswordSha1 = firstValue(values, "PasswordSha1", "passwordSha1", "password_sha1")
	}
}

func firstValue(values url.Values, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstStringFromMap(body map[string]any, keys ...string) string {
	if len(body) == 0 {
		return ""
	}
	for _, key := range keys {
		if value, ok := body[key]; ok {
			if s, ok := value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func firstFormValue(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if values, ok := c.Request.PostForm[key]; ok && len(values) > 0 {
			if value := strings.TrimSpace(values[0]); value != "" {
				return value
			}
		}
	}
	return ""
}

func firstQueryValue(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	return ""
}

// embyAuthByNameHandler 处理 POST /Users/AuthenticateByName。
//
// 这是 Emby 客户端登录的唯一入口（Infuse / Yamby / Hills 等都走这里）。
// 用户名+密码 → 调用我们已有的 AuthService.Login → 返回 AccessToken + User。
func embyAuthByNameHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		req, err := parseEmbyAuthByNameReq(c)
		if err != nil {
			embyError(c, http.StatusBadRequest, "invalid body")
			return
		}
		password := req.Pw
		if password == "" {
			password = req.Password
		}
		if strings.TrimSpace(req.Username) == "" || password == "" {
			if req.PasswordMd5 != "" || req.PasswordSha1 != "" {
				embyError(c, http.StatusBadRequest, "plain password required")
				return
			}
			embyError(c, http.StatusBadRequest, "missing username or password")
			return
		}
		resp, err := svc.Auth.Login(c.Request.Context(), req.Username, password)
		if err != nil {
			embyError(c, http.StatusUnauthorized, err.Error())
			return
		}
		// 记录登录设备会话并执行防共享检测（登录客户端数 / 设备指纹）。
		if svc.Device != nil {
			svc.Device.RecordLogin(c.Request.Context(), resp.User.ID,
				c.GetHeader("X-Emby-Device-Id"),
				c.GetHeader("X-Emby-Device-Name"),
				c.GetHeader("X-Emby-Client"),
				c.ClientIP())
		}
		userPayload, _ := svc.Emby.FindUser(c.Request.Context(), resp.User.ID)
		// Emby/Jellyfin 客户端没有 refresh token 机制：它们把这里返回的
		// AccessToken 长期保存并反复使用。若返回 60 分钟的普通 access
		// token，客户端每小时就会掉登录、无法播放、媒体库无法刷新。因此
		// 签发长期令牌（IssueEmbyToken）匹配 Emby 持久化令牌语义。
		accessToken := resp.Tokens.AccessToken
		if longLived, err := svc.Auth.IssueEmbyToken(resp.User); err == nil && longLived != "" {
			accessToken = longLived
		}
		c.JSON(http.StatusOK, gin.H{
			"AccessToken": accessToken,
			"ServerId":    "mediastation-go-001",
			"User":        userPayload,
			"SessionInfo": gin.H{
				"Id":         resp.User.ID,
				"UserId":     resp.User.ID,
				"UserName":   resp.User.Username,
				"Client":     c.GetHeader("X-Emby-Client"),
				"DeviceId":   c.GetHeader("X-Emby-Device-Id"),
				"DeviceName": c.GetHeader("X-Emby-Device-Name"),
			},
		})
	}
}

func embyPublicUsersHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 公开用户列表（Emby Web 客户端登录页拉这个，列出可见用户）。
		users, err := svc.Emby.ListUsers(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusOK, []any{})
			return
		}
		// 公开版本只暴露 Id + Name，不包含 Policy。
		out := make([]map[string]any, 0, len(users))
		for _, u := range users {
			out = append(out, map[string]any{
				"Id":          u["Id"],
				"Name":        u["Name"],
				"ServerId":    u["ServerId"],
				"HasPassword": true,
			})
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyListUsersHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		users, err := svc.Emby.ListUsers(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, users)
	}
}

func embyMeHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		if uid == "" {
			embyError(c, http.StatusUnauthorized, "not authenticated")
			return
		}
		u, err := svc.Emby.FindUser(c.Request.Context(), uid)
		if err != nil || u == nil {
			embyError(c, http.StatusNotFound, "user not found")
			return
		}
		c.JSON(http.StatusOK, u)
	}
}

func embyGetUserByIDHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, err := svc.Emby.FindUser(c.Request.Context(), c.Param("userId"))
		if err == nil && u != nil {
			c.JSON(http.StatusOK, u)
			return
		}
		if authUID := embyUserID(c); authUID != "" && authUID != c.Param("userId") {
			u, err = svc.Emby.FindUser(c.Request.Context(), authUID)
			if err == nil && u != nil {
				c.JSON(http.StatusOK, u)
				return
			}
		}
		c.JSON(http.StatusOK, embyFallbackUser(c.Param("userId")))
	}
}

func embyFallbackUser(id string) gin.H {
	if strings.TrimSpace(id) == "" {
		id = "mediastation-user"
	}
	return gin.H{
		"Id":                        id,
		"Name":                      "MediaStation",
		"ServerId":                  "mediastation-go-001",
		"HasPassword":               true,
		"HasConfiguredPassword":     true,
		"HasConfiguredEasyPassword": false,
		"EnableAutoLogin":           false,
		"Policy": gin.H{
			"IsAdministrator":                 true,
			"EnableContentDeletion":           true,
			"EnableRemoteControlOfOtherUsers": true,
			"EnableSharedDeviceControl":       true,
			"EnableRemoteAccess":              true,
			"EnableAllDevices":                true,
			"EnableAllChannels":               true,
			"EnableAllFolders":                true,
		},
	}
}

// ─── Views / MediaFolders ────────────────────────────────────────────────────

func embyViewsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		out, err := svc.Emby.Views(c.Request.Context(), uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyVirtualFoldersHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		libs, err := svc.Repo.Library.List(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		libs = service.FilterDisplayCloudLibraries(c.Request.Context(), svc.Repo, libs)
		uid := embyUserID(c)
		visibility := service.UserDefaultMediaVisibility(c.Request.Context(), svc.Repo, uid)
		out := make([]gin.H, 0, len(libs))
		for _, lib := range libs {
			if !service.LibraryVisibleForUser(c.Request.Context(), svc.Repo, lib, visibility) {
				continue
			}
			collectionType := "movies"
			switch lib.Type {
			case "tv", "anime", "variety":
				collectionType = "tvshows"
			case "music":
				collectionType = "music"
			}
			out = append(out, gin.H{
				"Name":               lib.Name,
				"Locations":          []string{lib.Path},
				"CollectionType":     collectionType,
				"ItemId":             lib.ID,
				"Id":                 lib.ID,
				"PrimaryImageItemId": lib.ID,
				"RefreshStatus":      "Idle",
				"LibraryOptions":     gin.H{},
			})
		}
		c.JSON(http.StatusOK, out)
	}
}

// ─── Items ───────────────────────────────────────────────────────────────────

func parseEmbyItemsParams(c *gin.Context) service.ItemsParams {
	limit, _ := strconv.Atoi(embyFirstNonEmptyString(firstQueryValue(c, "Limit", "limit"), "50"))
	offset, _ := strconv.Atoi(embyFirstNonEmptyString(firstQueryValue(c, "StartIndex", "startIndex", "startindex"), "0"))
	uid := c.Param("userId")
	if uid == "" {
		uid = embyUserID(c)
	}
	splitOpt := func(s string) []string {
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return service.ItemsParams{
		UserID:           uid,
		ParentID:         firstQueryValue(c, "ParentId", "parentId", "parentid"),
		IDs:              splitOpt(firstQueryValue(c, "Ids", "ids")),
		SearchTerm:       firstQueryValue(c, "SearchTerm", "searchTerm", "searchterm"),
		IncludeItemTypes: splitOpt(firstQueryValue(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")),
		Recursive:        strings.EqualFold(firstQueryValue(c, "Recursive", "recursive"), "true"),
		SortBy:           firstQueryValue(c, "SortBy", "sortBy", "sortby"),
		SortOrder:        firstQueryValue(c, "SortOrder", "sortOrder", "sortorder"),
		Limit:            limit,
		StartIndex:       offset,
	}
}

func embyFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func embyItemsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := svc.Emby.Items(c.Request.Context(), parseEmbyItemsParams(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyItemByIDHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		out, err := svc.Emby.Item(c.Request.Context(), id, uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if out == nil {
			embyError(c, http.StatusNotFound, "item not found")
			return
		}
		embyAttachRequestTokenToPlaybackInfo(c, out)
		c.JSON(http.StatusOK, out)
	}
}

func embyUserItemByIDHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch strings.ToLower(c.Param("id")) {
		case "latest":
			embyLatestItemsHandler(svc)(c)
		case "resume":
			embyResumeItemsHandler(svc)(c)
		default:
			embyItemByIDHandler(svc)(c)
		}
	}
}

func embyLatestItemsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))
		out, err := svc.Emby.LatestItems(c.Request.Context(), uid, c.Query("ParentId"), limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyResumeItemsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("Limit", "20"))
		out, err := svc.Emby.ResumeItems(c.Request.Context(), uid, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyItemsCountsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"MovieCount":   0,
			"SeriesCount":  0,
			"EpisodeCount": 0,
			"ItemCount":    0,
		})
	}
}

// ─── Images ──────────────────────────────────────────────────────────────────

// embyItemImageHandler 把 /Items/{id}/Images/Primary 等请求直接输出为图片。
// Emby 客户端缓存图片 URL 时经常不会继续携带 token；如果重定向到受保护的
// /api/img 会变成 401，所以这里复用 ImageProxy 但不再走 /api 路由。
func embyItemImageHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		req := c.Request.WithContext(ctx)
		id := c.Param("id")
		imgType := strings.ToLower(c.Param("type"))
		raw, err := svc.Emby.ImageURL(ctx, id, imgType)
		if err != nil || raw == "" {
			c.Status(http.StatusNotFound)
			return
		}
		if typ, ref, ok := parseCloudPlayImageURL(raw); ok {
			c.Request = req
			serveCloudResolvedLink(svc, c, typ, ref)
			return
		}
		if svc.ImageProxy == nil {
			c.Status(http.StatusNotFound)
			return
		}
		if err := svc.ImageProxy.Serve(ctx, c.Writer, req, raw); err != nil {
			c.Status(http.StatusNotFound)
		}
	}
}

func parseCloudPlayImageURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	path := strings.Trim(u.Path, "/")
	const prefix = "api/cloud/play/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	typ := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	ref := strings.TrimSpace(u.Query().Get("ref"))
	if typ == "" || ref == "" {
		return "", "", false
	}
	return typ, ref, true
}

func embyShowSeasonsHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		params := service.ItemsParams{
			UserID:   firstQueryValue(c, "UserId", "userId"),
			ParentID: c.Param("id"),
			Limit:    500,
		}
		out, err := svc.Emby.Items(c.Request.Context(), params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func embyShowEpisodesHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		parentID := firstQueryValue(c, "SeasonId", "seasonId")
		if parentID == "" {
			parentID = c.Param("id")
		}
		params := service.ItemsParams{
			UserID:           firstQueryValue(c, "UserId", "userId"),
			ParentID:         parentID,
			IncludeItemTypes: []string{"Episode"},
			Recursive:        true,
			Limit:            500,
		}
		out, err := svc.Emby.Items(c.Request.Context(), params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// ─── Playback ────────────────────────────────────────────────────────────────

func embyPlaybackInfoHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		out, err := svc.Emby.PlaybackInfo(c.Request.Context(), c.Param("id"), uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if out == nil {
			embyError(c, http.StatusNotFound, "not found")
			return
		}
		embyAttachRequestTokenToPlaybackInfo(c, out)
		c.JSON(http.StatusOK, out)
	}
}

func embyAttachRequestTokenToPlaybackInfo(c *gin.Context, out map[string]any) {
	token := embyRequestToken(c)
	if token == "" || out == nil {
		return
	}
	sources, ok := out["MediaSources"].([]map[string]any)
	if !ok {
		return
	}
	for _, source := range sources {
		for _, key := range []string{"DirectStreamUrl", "TranscodingUrl", "Path"} {
			raw, ok := source[key].(string)
			if !ok {
				continue
			}
			source[key] = embyAppendAPIKey(raw, token)
		}
	}
}

func embyRequestToken(c *gin.Context) string {
	if c == nil {
		return ""
	}
	for _, key := range []string{"api_key", "apiKey", "ApiKey", "token"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	for _, header := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			return value
		}
	}
	for _, header := range []string{"Authorization", "X-Emby-Authorization"} {
		if token := embyTokenFromAuthHeader(c.GetHeader(header)); token != "" {
			return token
		}
	}
	return ""
}

func embyTokenFromAuthHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, prefix := range []string{"Bearer ", "Emby "} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(value, prefix))
		}
	}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "MediaBrowser "))
		if !strings.HasPrefix(part, "Token=") {
			continue
		}
		token := strings.TrimSpace(strings.TrimPrefix(part, "Token="))
		return strings.Trim(token, `"`)
	}
	if strings.Contains(value, "Token=") {
		return ""
	}
	return value
}

func embyAppendAPIKey(raw, token string) string {
	raw = strings.TrimSpace(raw)
	token = strings.TrimSpace(token)
	if raw == "" || token == "" {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() {
		return raw
	}
	q := u.Query()
	if q.Get("api_key") == "" && q.Get("apiKey") == "" && q.Get("token") == "" {
		q.Set("api_key", token)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// embyVideoStreamHandler 是 GET /Videos/{id}/stream 的入口，
// 直接代理到我们的 /api/stream/{id}（同一个 ServeFile）。
func embyVideoStreamHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		item, err := svc.Emby.Item(c.Request.Context(), c.Param("id"), uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if item == nil {
			c.Status(http.StatusNotFound)
			return
		}
		// 直接调用 Stream service 写入 response。
		// 此前这里把所有错误一律吞成 404：云盘 Cookie 过期、直链解析失败、
		// STRM 播放被关闭……在第三方播放器上全部表现为「404 不存在」，
		// 无法排查。现在区分：行不存在→404；云盘播放不可用/上游故障→502+原因。
		err = svc.Stream.ServeFile(c.Writer, c.Request, c.Param("id"))
		switch {
		case err == nil:
		case errors.Is(err, service.ErrMediaNotFound):
			c.Status(http.StatusNotFound)
		default:
			if !c.Writer.Written() {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			}
		}
	}
}

func embyVideoHLSPlaylistHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		item, err := svc.Emby.Item(c.Request.Context(), c.Param("id"), uid)
		if err != nil || item == nil || svc.Stream == nil {
			c.Status(http.StatusNotFound)
			return
		}
		err = svc.Stream.ServeHLSPlaylist(c.Writer, c.Request, c.Param("id"))
		if errors.Is(err, service.ErrTranscodeDisabled) {
			c.JSON(http.StatusConflict, gin.H{"error": "transcode disabled"})
			return
		}
		if errors.Is(err, service.ErrTranscodeBusy) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "transcode busy"})
			return
		}
		if err != nil {
			c.Status(http.StatusNotFound)
		}
	}
}

func embyVideoHLSSegmentHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		item, err := svc.Emby.Item(c.Request.Context(), c.Param("id"), uid)
		if err != nil || item == nil || svc.Stream == nil {
			c.Status(http.StatusNotFound)
			return
		}
		if err := svc.Stream.ServeHLSSegment(c.Writer, c.Request, c.Param("id"), c.Param("seg")); err != nil {
			c.Status(http.StatusNotFound)
		}
	}
}

// ─── 播放进度 / 收藏 / 已看 ────────────────────────────────────────────────

type embyPlayingReq struct {
	ItemId        string `json:"ItemId"`
	PositionTicks int64  `json:"PositionTicks"`
	RunTimeTicks  int64  `json:"RunTimeTicks"`
}

func embyPlayingProgressHandler(svc *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := embyUserID(c)
		if uid == "" {
			c.Status(http.StatusUnauthorized)
			return
		}
		var req embyPlayingReq
		_ = c.ShouldBindJSON(&req)
		// 兼容 query 形式（一些客户端在 /Sessions/Playing/* 用 query）
		if req.ItemId == "" {
			req.ItemId = c.Query("ItemId")
		}
		if req.PositionTicks == 0 {
			req.PositionTicks, _ = strconv.ParseInt(c.Query("PositionTicks"), 10, 64)
		}
		if req.RunTimeTicks == 0 {
			req.RunTimeTicks, _ = strconv.ParseInt(c.Query("RunTimeTicks"), 10, 64)
		}
		if req.ItemId == "" {
			c.Status(http.StatusOK) // Emby 期望 2xx；不是关键操作
			return
		}
		// 被「一键踢下线」的设备拒绝继续播放，直到重新登录。
		if svc.Device != nil && svc.Device.IsDeviceKicked(c.Request.Context(), uid, c.GetHeader("X-Emby-Device-Id")) {
			c.Status(http.StatusUnauthorized)
			return
		}
		_ = svc.Emby.RecordProgress(c.Request.Context(), uid, req.ItemId, req.PositionTicks, req.RunTimeTicks)
		// 标记该设备正在播放并执行并发播放防共享检测。
		if svc.Device != nil {
			svc.Device.RecordPlayback(c.Request.Context(), uid,
				c.GetHeader("X-Emby-Device-Id"),
				c.GetHeader("X-Emby-Device-Name"),
				c.GetHeader("X-Emby-Client"))
		}
		c.Status(http.StatusNoContent)
	}
}

func embyFavoriteHandler(svc *service.Container, fav bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		mid := c.Param("itemId")
		if uid == "" || mid == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := svc.Emby.SetFavorite(c.Request.Context(), uid, mid, fav); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Emby 期望返回 UserItemDataDto；最小可工作版本：echo Item 即可。
		out, _ := svc.Emby.Item(c.Request.Context(), mid, uid)
		if out != nil {
			c.JSON(http.StatusOK, out["UserData"])
			return
		}
		c.JSON(http.StatusOK, gin.H{"IsFavorite": fav})
	}
}

func embyMarkPlayedHandler(svc *service.Container, played bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.Param("userId")
		if uid == "" {
			uid = embyUserID(c)
		}
		mid := c.Param("itemId")
		if uid == "" || mid == "" {
			c.Status(http.StatusBadRequest)
			return
		}
		if err := svc.Emby.MarkPlayed(c.Request.Context(), uid, mid, played); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out, _ := svc.Emby.Item(c.Request.Context(), mid, uid)
		if out != nil {
			c.JSON(http.StatusOK, out["UserData"])
			return
		}
		c.JSON(http.StatusOK, gin.H{"Played": played})
	}
}

// ─── Sessions / Branding 占位 ────────────────────────────────────────────────

func embySessionsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []any{})
	}
}

func embyNoContentHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	}
}

func embyServerConfigurationHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"EnableFolderView":                   true,
			"EnableGroupingIntoCollections":      true,
			"EnableExternalContentInSuggestions": false,
			"ImageSavingConvention":              "Compatible",
		})
	}
}

func embyPublicServerConfigurationHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"IsStartupWizardCompleted": true,
			"EnableRemoteAccess":       true,
			"EnableUPnP":               false,
			"EnableHttps":              false,
			"RequireHttps":             false,
			"LocalNetworkSubnets":      []string{},
			"LocalNetworkAddresses":    []string{},
			"RemoteClientBitrateLimit": 0,
		})
	}
}

func embyStartupConfigurationHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"IsStartupWizardCompleted":  true,
			"StartupWizardCompleted":    true,
			"EnableRemoteAccess":        true,
			"UICulture":                 "zh-CN",
			"MetadataCountryCode":       "CN",
			"PreferredMetadataLanguage": "zh-CN",
		})
	}
}

func embyQuickConnectEnabledHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, false)
	}
}

func embyEmptyItemsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"Items": []any{}, "TotalRecordCount": 0})
	}
}

func embyServerDomainsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []any{})
	}
}

func embyDanmuRawHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", nil)
	}
}

func embyBrandingConfigHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"LoginDisclaimer":     "",
			"CustomCss":           "",
			"SplashscreenEnabled": false,
		})
	}
}

func embyBrandingCSSHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(""))
	}
}

func embyLocalizationOptionsHandler(_ *service.Container) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, []map[string]any{
			{"Name": "简体中文", "Value": "zh-CN"},
			{"Name": "English", "Value": "en-US"},
		})
	}
}

// registerEmbyRoutes 在 r 上挂双前缀（"" + "/emby"）的 Emby 兼容路由。
func registerEmbyRoutes(r *gin.Engine, jwtSecret string, svc *service.Container) {
	for _, prefix := range []string{"/emby", ""} {
		grp := r.Group(prefix)
		grp.Use(func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
			c.Next()
		})

		if prefix == "/emby" {
			grp.GET("", embyRootHandler(svc))
			grp.HEAD("", embyRootHandler(svc))
			grp.GET("/", embyRootHandler(svc))
			grp.HEAD("/", embyRootHandler(svc))
		}

		// 公开端点
		for _, path := range []string{"/System/Info/Public", "/system/info/public"} {
			grp.GET(path, embySystemInfoPublicHandler(svc))
			grp.HEAD(path, embySystemInfoPublicHandler(svc))
		}
		for _, path := range []string{"/System/Info", "/system/info"} {
			grp.GET(path, embySystemInfoHandler(svc))
			grp.HEAD(path, embySystemInfoHandler(svc))
		}
		for _, path := range []string{"/System/Endpoint", "/system/endpoint"} {
			grp.GET(path, embySystemEndpointHandler(svc))
		}
		for _, path := range []string{"/System/Ext/ServerDomains", "/system/ext/serverdomains"} {
			grp.GET(path, embyServerDomainsHandler(svc))
			grp.HEAD(path, embyServerDomainsHandler(svc))
		}
		for _, path := range []string{"/System/Configuration/Public", "/system/configuration/public"} {
			grp.GET(path, embyPublicServerConfigurationHandler(svc))
			grp.HEAD(path, embyPublicServerConfigurationHandler(svc))
		}
		for _, path := range []string{"/Startup/Configuration", "/startup/configuration"} {
			grp.GET(path, embyStartupConfigurationHandler(svc))
			grp.HEAD(path, embyStartupConfigurationHandler(svc))
		}
		for _, path := range []string{"/Startup/Complete", "/startup/complete"} {
			grp.POST(path, embyNoContentHandler(svc))
		}
		for _, path := range []string{"/QuickConnect/Enabled", "/quickconnect/enabled"} {
			grp.GET(path, embyQuickConnectEnabledHandler(svc))
			grp.HEAD(path, embyQuickConnectEnabledHandler(svc))
		}
		for _, path := range []string{"/System/Ping", "/system/ping"} {
			grp.GET(path, embyPingHandler(svc))
			grp.HEAD(path, embyPingHandler(svc))
			grp.POST(path, embyPingHandler(svc))
		}
		for _, path := range []string{"/Sessions/Capabilities", "/Sessions/Capabilities/Full", "/sessions/capabilities", "/sessions/capabilities/full"} {
			grp.POST(path, embyNoContentHandler(svc))
		}
		// 30/min per IP: many Emby clients sit behind a single NAT/reverse-proxy
		// IP, so a low limit would throttle legitimate logins into 429s.
		embyLoginLimiter := middleware.NewRateLimiter(30, 1*time.Minute)
		for _, path := range []string{"/Users/AuthenticateByName", "/Users/authenticatebyname", "/users/AuthenticateByName", "/users/authenticatebyname"} {
			grp.POST(path, middleware.RateLimit(embyLoginLimiter), embyAuthByNameHandler(svc))
		}
		for _, path := range []string{"/Users/Public", "/users/public"} {
			grp.GET(path, embyPublicUsersHandler(svc))
		}
		for _, path := range []string{"/Branding/Configuration", "/branding/configuration"} {
			grp.GET(path, embyBrandingConfigHandler(svc))
		}
		for _, path := range []string{"/Branding/Css", "/branding/css"} {
			grp.GET(path, embyBrandingCSSHandler(svc))
			grp.HEAD(path, embyBrandingCSSHandler(svc))
		}
		for _, path := range []string{"/Localization/Options", "/localization/options"} {
			grp.GET(path, embyLocalizationOptionsHandler(svc))
		}

		// 图片公开（Infuse 缓存 URL 时会丢 token）
		grp.GET("/Items/:id/Images/:type", embyItemImageHandler(svc))
		grp.GET("/Items/:id/Images/:type/:index", embyItemImageHandler(svc))
		grp.HEAD("/Items/:id/Images/:type", embyItemImageHandler(svc))
		grp.GET("/items/:id/images/:type", embyItemImageHandler(svc))
		grp.GET("/items/:id/images/:type/:index", embyItemImageHandler(svc))
		grp.HEAD("/items/:id/images/:type", embyItemImageHandler(svc))

		// 鉴权后端点
		auth := grp.Group("", middleware.EmbyAuthRequired(jwtSecret), activeEmbyUserRequired(svc))
		auth.GET("/Users/Me", embyMeHandler(svc))
		auth.GET("/Users", embyListUsersHandler(svc))
		auth.GET("/Users/:userId", embyGetUserByIDHandler(svc))
		auth.GET("/Users/:userId/Views", embyViewsHandler(svc))
		auth.GET("/Library/MediaFolders", embyViewsHandler(svc))
		auth.GET("/Library/VirtualFolders", embyVirtualFoldersHandler(svc))
		auth.GET("/Library/SelectableMediaFolders", embyVirtualFoldersHandler(svc))

		auth.GET("/Items", embyItemsHandler(svc))
		auth.GET("/Users/:userId/Items", embyItemsHandler(svc))
		auth.GET("/Items/Counts", embyItemsCountsHandler(svc))
		auth.GET("/Users/:userId/Items/Counts", embyItemsCountsHandler(svc))
		auth.GET("/Items/:id", embyItemByIDHandler(svc))
		auth.GET("/Users/:userId/Items/:id", embyUserItemByIDHandler(svc))
		auth.GET("/Shows/:id/Seasons", embyShowSeasonsHandler(svc))
		auth.GET("/Shows/:id/Episodes", embyShowEpisodesHandler(svc))
		auth.GET("/Users/:userId/Shows/:id/Seasons", embyShowSeasonsHandler(svc))
		auth.GET("/Users/:userId/Shows/:id/Episodes", embyShowEpisodesHandler(svc))
		auth.GET("/Shows/NextUp", embyEmptyItemsHandler(svc))
		auth.GET("/Users/:userId/Shows/NextUp", embyEmptyItemsHandler(svc))
		auth.GET("/MediaSegments/:id", embyEmptyItemsHandler(svc))
		auth.GET("/Items/:id/Similar", embyEmptyItemsHandler(svc))
		auth.GET("/api/danmu/:id/raw", embyDanmuRawHandler(svc))

		auth.GET("/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))
		auth.POST("/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))
		auth.GET("/Users/:userId/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))
		auth.POST("/Users/:userId/Items/:id/PlaybackInfo", embyPlaybackInfoHandler(svc))

		auth.GET("/Videos/:id/stream", embyVideoStreamHandler(svc))
		auth.HEAD("/Videos/:id/stream", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/stream.:container", embyVideoStreamHandler(svc))
		auth.HEAD("/Videos/:id/stream.:container", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/original", embyVideoStreamHandler(svc))
		auth.HEAD("/Videos/:id/original", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/original.:container", embyVideoStreamHandler(svc))
		auth.HEAD("/Videos/:id/original.:container", embyVideoStreamHandler(svc))
		auth.GET("/Videos/:id/master.m3u8", embyVideoHLSPlaylistHandler(svc))
		auth.HEAD("/Videos/:id/master.m3u8", embyVideoHLSPlaylistHandler(svc))
		auth.GET("/Videos/:id/main.m3u8", embyVideoHLSPlaylistHandler(svc))
		auth.HEAD("/Videos/:id/main.m3u8", embyVideoHLSPlaylistHandler(svc))
		auth.GET("/Videos/:id/:seg", embyVideoHLSSegmentHandler(svc))

		auth.POST("/Sessions/Playing", embyPlayingProgressHandler(svc))
		auth.POST("/Sessions/Playing/Progress", embyPlayingProgressHandler(svc))
		auth.POST("/Sessions/Playing/Stopped", embyPlayingProgressHandler(svc))

		auth.POST("/Users/:userId/FavoriteItems/:itemId", embyFavoriteHandler(svc, true))
		auth.DELETE("/Users/:userId/FavoriteItems/:itemId", embyFavoriteHandler(svc, false))
		auth.POST("/Users/:userId/PlayedItems/:itemId", embyMarkPlayedHandler(svc, true))
		auth.DELETE("/Users/:userId/PlayedItems/:itemId", embyMarkPlayedHandler(svc, false))

		auth.GET("/Sessions", embySessionsHandler(svc))
		auth.GET("/System/Configuration", embyServerConfigurationHandler(svc))
		auth.GET("/DisplayPreferences/:id", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"Id": c.Param("id"), "CustomPrefs": gin.H{}})
		})
		auth.POST("/DisplayPreferences/:id", func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})

		registerLowercaseEmbyAuthRoutes(auth, svc)
	}
}

func registerLowercaseEmbyAuthRoutes(auth *gin.RouterGroup, svc *service.Container) {
	auth.GET("/users/me", embyMeHandler(svc))
	auth.GET("/users", embyListUsersHandler(svc))
	auth.GET("/users/:userId", embyGetUserByIDHandler(svc))
	auth.GET("/users/:userId/views", embyViewsHandler(svc))
	auth.GET("/library/mediafolders", embyViewsHandler(svc))
	auth.GET("/library/virtualfolders", embyVirtualFoldersHandler(svc))
	auth.GET("/library/selectablemediafolders", embyVirtualFoldersHandler(svc))

	auth.GET("/items", embyItemsHandler(svc))
	auth.GET("/users/:userId/items", embyItemsHandler(svc))
	auth.GET("/items/counts", embyItemsCountsHandler(svc))
	auth.GET("/users/:userId/items/counts", embyItemsCountsHandler(svc))
	auth.GET("/items/:id", embyItemByIDHandler(svc))
	auth.GET("/users/:userId/items/:id", embyUserItemByIDHandler(svc))
	auth.GET("/shows/:id/seasons", embyShowSeasonsHandler(svc))
	auth.GET("/shows/:id/episodes", embyShowEpisodesHandler(svc))
	auth.GET("/users/:userId/shows/:id/seasons", embyShowSeasonsHandler(svc))
	auth.GET("/users/:userId/shows/:id/episodes", embyShowEpisodesHandler(svc))
	auth.GET("/shows/nextup", embyEmptyItemsHandler(svc))
	auth.GET("/users/:userId/shows/nextup", embyEmptyItemsHandler(svc))
	auth.GET("/mediasegments/:id", embyEmptyItemsHandler(svc))
	auth.GET("/items/:id/similar", embyEmptyItemsHandler(svc))

	auth.GET("/items/:id/playbackinfo", embyPlaybackInfoHandler(svc))
	auth.POST("/items/:id/playbackinfo", embyPlaybackInfoHandler(svc))
	auth.GET("/users/:userId/items/:id/playbackinfo", embyPlaybackInfoHandler(svc))
	auth.POST("/users/:userId/items/:id/playbackinfo", embyPlaybackInfoHandler(svc))

	auth.GET("/videos/:id/stream", embyVideoStreamHandler(svc))
	auth.HEAD("/videos/:id/stream", embyVideoStreamHandler(svc))
	auth.GET("/videos/:id/stream.:container", embyVideoStreamHandler(svc))
	auth.HEAD("/videos/:id/stream.:container", embyVideoStreamHandler(svc))
	auth.GET("/videos/:id/original", embyVideoStreamHandler(svc))
	auth.HEAD("/videos/:id/original", embyVideoStreamHandler(svc))
	auth.GET("/videos/:id/original.:container", embyVideoStreamHandler(svc))
	auth.HEAD("/videos/:id/original.:container", embyVideoStreamHandler(svc))
	auth.GET("/videos/:id/master.m3u8", embyVideoHLSPlaylistHandler(svc))
	auth.HEAD("/videos/:id/master.m3u8", embyVideoHLSPlaylistHandler(svc))
	auth.GET("/videos/:id/main.m3u8", embyVideoHLSPlaylistHandler(svc))
	auth.HEAD("/videos/:id/main.m3u8", embyVideoHLSPlaylistHandler(svc))
	auth.GET("/videos/:id/:seg", embyVideoHLSSegmentHandler(svc))

	auth.POST("/sessions/playing", embyPlayingProgressHandler(svc))
	auth.POST("/sessions/playing/progress", embyPlayingProgressHandler(svc))
	auth.POST("/sessions/playing/stopped", embyPlayingProgressHandler(svc))

	auth.POST("/users/:userId/favoriteitems/:itemId", embyFavoriteHandler(svc, true))
	auth.DELETE("/users/:userId/favoriteitems/:itemId", embyFavoriteHandler(svc, false))
	auth.POST("/users/:userId/playeditems/:itemId", embyMarkPlayedHandler(svc, true))
	auth.DELETE("/users/:userId/playeditems/:itemId", embyMarkPlayedHandler(svc, false))

	auth.GET("/sessions", embySessionsHandler(svc))
	auth.GET("/system/configuration", embyServerConfigurationHandler(svc))
	auth.GET("/displaypreferences/:id", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"Id": c.Param("id"), "CustomPrefs": gin.H{}})
	})
	auth.POST("/displaypreferences/:id", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
}

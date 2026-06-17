package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type qbitLoginVariant struct {
	name    string
	referer bool
	origin  bool
}

func qbitLogin(ctx context.Context, client *http.Client, baseURL, username, password string) error {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return errors.New("qbittorrent host not configured")
	}
	var err error
	baseURL, err = normalizeDownloadClientEndpoint("qbittorrent", baseURL)
	if err != nil {
		return err
	}
	var lastErr error
	for _, variant := range []qbitLoginVariant{
		{name: "minimal"},
		{name: "referer", referer: true},
		{name: "referer-origin", referer: true, origin: true},
	} {
		err := qbitLoginOnce(ctx, client, baseURL, username, password, variant)
		if err == nil {
			return nil
		}
		lastErr = err
		if errors.Is(err, errQbitBadCredentials) {
			return err
		}
	}
	return lastErr
}

var errQbitBadCredentials = errors.New("qbittorrent: 用户名/密码错误")

func qbitLoginOnce(ctx context.Context, client *http.Client, baseURL, username, password string, variant qbitLoginVariant) error {
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	req, err := newDownloadClientHTTPRequest(ctx, http.MethodPost,
		baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if variant.referer {
		req.Header.Set("Referer", baseURL)
	}
	if variant.origin {
		req.Header.Set("Origin", baseURL)
	}

	resp, err := client.Do(req)
	if err != nil {
		return qbitNetworkError(baseURL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	text := strings.TrimSpace(string(raw))
	switch {
	case resp.StatusCode == http.StatusOK && text == "Ok.":
		return nil
	case resp.StatusCode == http.StatusOK && text == "Ok":
		return nil
	case resp.StatusCode == http.StatusNoContent:
		return nil
	case resp.StatusCode == http.StatusOK && text == "Fails.":
		return errQbitBadCredentials
	case resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("qbittorrent: 403 forbidden during %s login, body=%q; check qBittorrent WebUI bypass/auth settings for the container IP and host %s", variant.name, text, baseURL)
	case resp.StatusCode >= 400:
		return fmt.Errorf("qbittorrent login failed during %s login: status=%d body=%q", variant.name, resp.StatusCode, text)
	default:
		return fmt.Errorf("qbittorrent login unexpected response during %s login: status=%d body=%q", variant.name, resp.StatusCode, text)
	}
}

func qbitNetworkError(baseURL string, err error) error {
	if err == nil {
		return nil
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) || strings.Contains(err.Error(), "Client.Timeout exceeded") {
		return fmt.Errorf("qbittorrent: 连接 %s 超时；容器内无法访问该地址。若 qBittorrent 运行在 NAS 宿主机上，请把下载器地址改为 http://host.docker.internal:端口 或 http://172.17.0.1:端口，并确认 docker-compose.yml 包含 extra_hosts: host.docker.internal:host-gateway: %w", baseURL, err)
	}
	return fmt.Errorf("qbittorrent: 连接 %s 失败：%w", baseURL, err)
}

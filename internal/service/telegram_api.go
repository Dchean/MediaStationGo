package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const defaultTelegramAPIBaseURL = "https://api.telegram.org"

var telegramTokenPattern = regexp.MustCompile(`bot[0-9]+:[^/\s"'?]+`)

func telegramAPIBaseURL(cfg map[string]string) string {
	base := strings.TrimSpace(cfg["api_base_url"])
	if base == "" {
		base = strings.TrimSpace(os.Getenv("MEDIASTATION_TELEGRAM_API_BASE_URL"))
	}
	if base == "" {
		base = defaultTelegramAPIBaseURL
	}
	return strings.TrimRight(base, "/")
}

func telegramMethodURL(cfg map[string]string, botToken, method string) (string, error) {
	botToken = strings.TrimSpace(botToken)
	method = strings.TrimSpace(method)
	if botToken == "" {
		return "", errors.New("telegram bot_token required")
	}
	if method == "" {
		return "", errors.New("telegram method required")
	}
	base := telegramAPIBaseURL(cfg)
	if _, err := url.ParseRequestURI(base); err != nil {
		return "", fmt.Errorf("telegram api_base_url invalid")
	}
	return fmt.Sprintf("%s/bot%s/%s", base, botToken, method), nil
}

func telegramHTTPClient(timeout time.Duration, cfg map[string]string) *http.Client {
	clients := telegramHTTPClients(timeout, cfg)
	return clients[0]
}

func telegramHTTPClients(timeout time.Duration, cfg map[string]string) []*http.Client {
	clients := []*http.Client{}
	seen := map[string]bool{}

	addProxy := func(proxyURL *url.URL) {
		if proxyURL == nil {
			return
		}
		key := proxyURL.String()
		if seen[key] {
			return
		}
		seen[key] = true
		transport := NewExternalTransport()
		transport.Proxy = http.ProxyURL(proxyURL)
		clients = append(clients, &http.Client{Timeout: timeout, Transport: transport})
	}

	for _, proxyRaw := range telegramExplicitProxyCandidates(cfg) {
		proxyURL, err := normalizeProxyURL(proxyRaw, "http")
		if err != nil || proxyURL == nil {
			continue
		}
		addProxy(proxyURL)
	}
	if proxyURL, err := telegramAutoProxyURL(cfg); err == nil {
		addProxy(proxyURL)
	}
	if telegramAPIBaseURL(cfg) == defaultTelegramAPIBaseURL {
		for _, proxyRaw := range telegramFallbackProxyCandidates() {
			proxyURL, err := normalizeProxyURL(proxyRaw, "http")
			if err != nil || proxyURL == nil {
				continue
			}
			addProxy(proxyURL)
		}
	}
	transport := NewExternalTransport()
	transport.Proxy = nil
	clients = append(clients, &http.Client{Timeout: timeout, Transport: transport})
	return clients
}

func telegramExplicitProxyCandidates(cfg map[string]string) []string {
	out := []string{}
	for _, value := range []string{
		cfg["proxy_url"],
		os.Getenv("MEDIASTATION_TELEGRAM_PROXY_URL"),
	} {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func telegramProxyCandidates(cfg map[string]string) []string {
	out := telegramExplicitProxyCandidates(cfg)
	if len(out) > 0 {
		return out
	}
	return telegramFallbackProxyCandidates()
}

func telegramFallbackProxyCandidates() []string {
	// Common local proxy ports used by Clash / v2rayN / v2rayA. Docker on Linux
	// reaches host services through host.docker.internal (when extra_hosts is
	// configured) or the bridge gateway 172.17.0.1. These are only fallbacks:
	// explicit channel/env proxy and environment/system proxy are tried first.
	return []string{
		"http://host.docker.internal:20171",
		"socks5://host.docker.internal:20170",
		"http://172.17.0.1:20171",
		"socks5://172.17.0.1:20170",
		"http://host.docker.internal:7890",
		"http://host.docker.internal:10808",
		"http://172.17.0.1:7890",
		"http://172.17.0.1:10808",
		"http://127.0.0.1:10808",
		"http://127.0.0.1:10809",
		"http://127.0.0.1:7890",
		"http://127.0.0.1:7891",
	}
}

func telegramAutoProxyURL(cfg map[string]string) (*url.URL, error) {
	base := telegramAPIBaseURL(cfg)
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, base, nil)
	if err != nil {
		return nil, err
	}
	return ProxyFromEnvironmentOrSystem(req)
}

func telegramPostForm(ctx context.Context, cfg map[string]string, method string, form url.Values, timeout time.Duration) error {
	apiURL, err := telegramMethodURL(cfg, cfg["bot_token"], method)
	if err != nil {
		return err
	}
	return telegramDoWithFallback(ctx, cfg, http.MethodPost, apiURL, form.Encode(), "application/x-www-form-urlencoded", timeout)
}

func telegramPostJSON(ctx context.Context, cfg map[string]string, method string, payload any, timeout time.Duration) error {
	apiURL, err := telegramMethodURL(cfg, cfg["bot_token"], method)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return telegramDoWithFallback(ctx, cfg, http.MethodPost, apiURL, string(body), "application/json", timeout)
}

func telegramDo(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		return sanitizeTelegramError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("telegram api error %d: %s", resp.StatusCode, sanitizeTelegramText(string(body)))
	}
	return nil
}

func telegramDoWithFallback(ctx context.Context, cfg map[string]string, method, apiURL, body, contentType string, timeout time.Duration) error {
	var lastErr error
	for _, client := range telegramHTTPClients(timeout, cfg) {
		req, err := http.NewRequestWithContext(ctx, method, apiURL, strings.NewReader(body))
		if err != nil {
			return err
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if err := telegramDo(client, req); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("telegram request failed")
}

func telegramPostJSONDecode(ctx context.Context, cfg map[string]string, method string, payload any, timeout time.Duration, out any) error {
	apiURL, err := telegramMethodURL(cfg, cfg["bot_token"], method)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var lastErr error
	for _, client := range telegramHTTPClients(timeout, cfg) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(body)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = sanitizeTelegramError(err)
			continue
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("telegram api error %d: %s", resp.StatusCode, sanitizeTelegramText(string(respBody)))
			continue
		}
		if out != nil {
			return json.Unmarshal(respBody, out)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("telegram request failed")
}

func telegramGetJSONDecode(ctx context.Context, cfg map[string]string, method string, timeout time.Duration, out any) error {
	apiURL, err := telegramMethodURL(cfg, cfg["bot_token"], method)
	if err != nil {
		return err
	}
	var lastErr error
	for _, client := range telegramHTTPClients(timeout, cfg) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = sanitizeTelegramError(err)
			continue
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("telegram api error %d: %s", resp.StatusCode, sanitizeTelegramText(string(respBody)))
			continue
		}
		if out != nil {
			return json.Unmarshal(respBody, out)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("telegram request failed")
}

func telegramStringConfigFromAny(cfg map[string]any) map[string]string {
	out := make(map[string]string, len(cfg))
	for key, value := range cfg {
		out[key] = str(value)
	}
	normalizeTelegramConfig(out)
	return out
}

func sanitizeTelegramError(err error) error {
	if err == nil {
		return nil
	}
	msg := sanitizeTelegramText(err.Error())
	if strings.Contains(msg, "Client.Timeout exceeded") || strings.Contains(msg, "context deadline exceeded") {
		return errors.New("telegram request timeout: 请检查 NAS/Docker 到 Telegram API 的代理、反代或网络连通性")
	}
	return errors.New(msg)
}

func sanitizeTelegramText(text string) string {
	return telegramTokenPattern.ReplaceAllString(text, "bot<redacted>")
}

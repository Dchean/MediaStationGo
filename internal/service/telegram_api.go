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
	transport := NewExternalTransport()
	proxyRaw := strings.TrimSpace(cfg["proxy_url"])
	if proxyRaw == "" {
		proxyRaw = strings.TrimSpace(os.Getenv("MEDIASTATION_TELEGRAM_PROXY_URL"))
	}
	if proxyRaw != "" {
		if proxyURL, err := normalizeProxyURL(proxyRaw, "http"); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func telegramPostForm(ctx context.Context, cfg map[string]string, method string, form url.Values, timeout time.Duration) error {
	apiURL, err := telegramMethodURL(cfg, cfg["bot_token"], method)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return telegramDo(telegramHTTPClient(timeout, cfg), req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return telegramDo(telegramHTTPClient(timeout, cfg), req)
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

func telegramStringConfigFromAny(cfg map[string]any) map[string]string {
	out := make(map[string]string, len(cfg))
	for key, value := range cfg {
		out[key] = str(value)
	}
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

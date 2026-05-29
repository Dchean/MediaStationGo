// Package service — Telegram 通知 Provider。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TelegramProvider 通过 Telegram Bot API 发送通知。
type TelegramProvider struct{}

// Send 发送 Telegram 消息。
func (p *TelegramProvider) Send(ctx context.Context, cfg map[string]string, event NotifyEvent) error {
	botToken := cfg["bot_token"]
	chatID := cfg["chat_id"]
	parseMode := cfg["parse_mode"]
	if parseMode == "" {
		parseMode = "HTML"
	}

	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram: bot_token and chat_id are required")
	}

	text := formatTelegramMessage(event, parseMode)

	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": parseMode,
	}
	return telegramPostJSON(ctx, cfg, "sendMessage", payload, 15*time.Second)
}

// ValidateConfig 验证 Telegram 配置。
func (p *TelegramProvider) ValidateConfig(cfg map[string]string) error {
	if cfg["bot_token"] == "" {
		return fmt.Errorf("telegram: bot_token is required")
	}
	if cfg["chat_id"] == "" {
		return fmt.Errorf("telegram: chat_id is required")
	}
	return nil
}

// formatTelegramMessage 格式化消息内容。
func formatTelegramMessage(event NotifyEvent, parseMode string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n\n", escapeHTML(event.Title)))
	sb.WriteString(escapeHTML(event.Message))

	if len(event.Data) > 0 {
		sb.WriteString("\n\n")
		for k, v := range event.Data {
			sb.WriteString(fmt.Sprintf("• <b>%s</b>: %v\n", escapeHTML(k), v))
		}
	}

	if parseMode != "HTML" {
		// Markdown 模式
		result := sb.String()
		result = strings.ReplaceAll(result, "<b>", "**")
		result = strings.ReplaceAll(result, "</b>", "**")
		result = strings.ReplaceAll(result, "&lt;", "<")
		result = strings.ReplaceAll(result, "&gt;", ">")
		result = strings.ReplaceAll(result, "&amp;", "&")
		return result
	}

	return sb.String()
}

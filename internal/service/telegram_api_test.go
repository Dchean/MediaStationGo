package service

import (
	"errors"
	"strings"
	"testing"
)

func TestTelegramMethodURLUsesCustomAPIBase(t *testing.T) {
	got, err := telegramMethodURL(map[string]string{
		"api_base_url": "https://tg.example.com/",
	}, "123456:ABC-def", "sendMessage")
	if err != nil {
		t.Fatalf("telegramMethodURL returned error: %v", err)
	}
	want := "https://tg.example.com/bot123456:ABC-def/sendMessage"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeTelegramErrorRedactsBotToken(t *testing.T) {
	err := sanitizeTelegramError(errors.New(`Post "https://api.telegram.org/bot123456:SECRET/sendMessage": context deadline exceeded`))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRET") || strings.Contains(msg, "123456:") {
		t.Fatalf("telegram token leaked in error: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Fatalf("expected timeout hint, got: %s", msg)
	}
}

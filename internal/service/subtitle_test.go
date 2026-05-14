package service

import (
	"strings"
	"testing"
)

func TestSrtToVTT(t *testing.T) {
	in := "1\n00:00:01,000 --> 00:00:02,500\nHello world\n\n"
	out := srtToVTT(in)
	if !strings.HasPrefix(out, "WEBVTT") {
		t.Fatalf("missing WEBVTT prefix: %q", out)
	}
	if !strings.Contains(out, "00:00:01.000 --> 00:00:02.500") {
		t.Fatalf("comma timecode not converted: %q", out)
	}
	if !strings.Contains(out, "Hello world") {
		t.Fatalf("dialogue lost: %q", out)
	}
}

func TestStripASSTags(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"{\\an8}hello", "hello"},
		{"plain", "plain"},
		{"a{\\fad(0,500)}b{\\b1}c", "abc"},
	}
	for _, c := range cases {
		got := stripASSTags(c.in)
		if got != c.want {
			t.Errorf("stripASSTags(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

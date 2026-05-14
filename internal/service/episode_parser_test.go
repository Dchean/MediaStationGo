package service

import "testing"

func TestParseEpisode(t *testing.T) {
	cases := []struct {
		in              string
		wantS, wantE    int
	}{
		{"Breaking.Bad.S01E02.1080p.mkv", 1, 2},
		{"breaking.bad.s5e14.mkv", 5, 14},
		{"Friends 1x02.mp4", 1, 2},
		{"Friends 10x24 - The One Where.mkv", 10, 24},
		{"Some Anime - EP05 [1080p].mkv", 1, 5},
		{"Some Anime - E12.mkv", 1, 12},
		{"日剧 第03集.mkv", 1, 3},
		{"日剧 第12话.mkv", 1, 12},
		{"Movie.2020.1080p.mkv", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			s, e := ParseEpisode(tc.in)
			if s != tc.wantS || e != tc.wantE {
				t.Errorf("ParseEpisode(%q) = (%d, %d), want (%d, %d)",
					tc.in, s, e, tc.wantS, tc.wantE)
			}
		})
	}
}

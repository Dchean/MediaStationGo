package service

import "testing"

func TestCleanQuery(t *testing.T) {
	cases := []struct {
		in        string
		wantTitle string
		wantYear  int
	}{
		{"Inception.2010.1080p.BluRay.x264.mkv", "inception", 2010},
		{"The_Matrix_(1999).1080p.WEB-DL.H265.mp4", "the matrix", 1999},
		{"interstellar.2014.4k.hdr.dts.atmos.mkv", "interstellar", 2014},
		{"My Movie 2022 [HDR] (1080p) [TGx].mp4", "my movie", 2022},
		{"NoYearOrTags.mkv", "noyearortags", 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotTitle, gotYear := CleanQuery(tc.in)
			if gotTitle != tc.wantTitle || gotYear != tc.wantYear {
				t.Errorf("CleanQuery(%q) = (%q, %d), want (%q, %d)",
					tc.in, gotTitle, gotYear, tc.wantTitle, tc.wantYear)
			}
		})
	}
}

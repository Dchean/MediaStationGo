// Package service — episode parser for TV series.
//
// Detects season + episode numbers from filenames. Recognised patterns:
//
//   S01E02        / s1e2
//   1x02          / 01x02
//   EP02 / E02
//   第2集         / 第02集
//
// When neither a season nor an episode marker is present, returns (0, 0).
package service

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	patSEnE = regexp.MustCompile(`(?i)s(\d{1,2})e(\d{1,3})`)
	patNxE  = regexp.MustCompile(`(\d{1,2})x(\d{1,3})`)
	patEP   = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:e|ep)\.?\s*(\d{1,3})(?:[^0-9]|$)`)
	patCN   = regexp.MustCompile(`第\s*(\d{1,3})\s*[集话話]`)
)

// ParseEpisode tries to extract (season, episode) from an arbitrary filename.
// Returns (0, 0) when nothing recognisable is found.
func ParseEpisode(path string) (season, episode int) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	if m := patSEnE.FindStringSubmatch(name); len(m) == 3 {
		season = mustAtoi(m[1])
		episode = mustAtoi(m[2])
		return
	}
	if m := patNxE.FindStringSubmatch(name); len(m) == 3 {
		season = mustAtoi(m[1])
		episode = mustAtoi(m[2])
		return
	}
	if m := patEP.FindStringSubmatch(name); len(m) >= 2 {
		season = 1
		episode = mustAtoi(m[1])
		return
	}
	if m := patCN.FindStringSubmatch(name); len(m) >= 2 {
		season = 1
		episode = mustAtoi(m[1])
		return
	}
	return 0, 0
}

func mustAtoi(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

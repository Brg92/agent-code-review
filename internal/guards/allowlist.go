// Package guards holds the checks behind the repository's structural guards.
// The guards themselves are tests; what lives outside test files is only what
// the release tooling reuses.
package guards

import (
	"path"
	"strings"
)

// ParseAllowlist returns the patterns in text, one per line, skipping blank
// lines and # comments.
func ParseAllowlist(text string) []string {
	var patterns []string
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

// DisallowedPaths returns the slash-separated paths that match no pattern.
// A pattern is a path.Match glob, or dir/** for anything below dir. A path
// that is not clean and relative never matches.
func DisallowedPaths(patterns, paths []string) []string {
	var bad []string
	for _, p := range paths {
		if !allowed(patterns, p) {
			bad = append(bad, p)
		}
	}
	return bad
}

func allowed(patterns []string, p string) bool {
	if p == "" || p != path.Clean(p) || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, `\`) {
		return false
	}
	for _, pattern := range patterns {
		if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
			if strings.HasPrefix(p, dir+"/") {
				return true
			}
			continue
		}
		if ok, err := path.Match(pattern, p); err == nil && ok {
			return true
		}
	}
	return false
}

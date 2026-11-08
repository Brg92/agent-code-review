// Package rules loads rules catalogs and runs their mechanical checks on
// the tokens of one file, producing signals for the judgement.
package rules

import (
	"fmt"
	"strings"
)

// Glob is a compiled path glob with forward slashes: '*' matches any run
// of characters within a segment and a '**' segment matches any number of
// segments, none included. Every other character is literal.
type Glob struct {
	text     string
	segments []string
}

// CompileGlob checks and compiles a glob.
func CompileGlob(glob string) (Glob, error) {
	if glob == "" || strings.Contains(glob, `\`) {
		return Glob{}, fmt.Errorf("glob %q: not a relative path with forward slashes", glob)
	}
	segments := strings.Split(glob, "/")
	for _, s := range segments {
		if s == "" {
			return Glob{}, fmt.Errorf("glob %q: empty segment", glob)
		}
	}
	return Glob{text: glob, segments: segments}, nil
}

// String returns the glob as written.
func (g Glob) String() string { return g.text }

// Match reports whether a slash-separated relative path matches the glob.
func (g Glob) Match(path string) bool {
	return matchSegments(g.segments, strings.Split(path, "/"))
}

func matchSegments(glob, path []string) bool {
	for len(glob) > 0 {
		if glob[0] == "**" {
			for skip := 0; skip <= len(path); skip++ {
				if matchSegments(glob[1:], path[skip:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 || !matchSegment(glob[0], path[0]) {
			return false
		}
		glob, path = glob[1:], path[1:]
	}
	return len(path) == 0
}

// matchSegment matches one segment, where '*' matches any run of
// characters.
func matchSegment(glob, s string) bool {
	parts := strings.Split(glob, "*")
	if len(parts) == 1 {
		return glob == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return len(s) >= len(last) && strings.HasSuffix(s, last)
}

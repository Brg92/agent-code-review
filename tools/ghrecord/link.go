package ghrecord

import "strings"

// NextLink returns the target of the rel="next" entry of a Link header, or
// the empty string when there is none.
func NextLink(header string) string {
	rest := header
	for {
		start := strings.IndexByte(rest, '<')
		if start < 0 {
			return ""
		}
		end := strings.IndexByte(rest[start:], '>')
		if end < 0 {
			return ""
		}
		target := rest[start+1 : start+end]
		rest = rest[start+end+1:]
		params := rest
		if next := strings.IndexByte(rest, '<'); next >= 0 {
			params = rest[:next]
		}
		for _, p := range strings.Split(params, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(p), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
				continue
			}
			value = strings.Trim(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(value), ",")), `"`)
			for _, rel := range strings.Fields(value) {
				if strings.EqualFold(rel, "next") {
					return target
				}
			}
		}
	}
}

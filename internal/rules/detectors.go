package rules

import "unicode/utf8"

// lines splits a source into lines the way the lexer counts them: a line
// ends at \n, \r\n or a lone \r, which is not part of it.
func lines(src string) []string {
	var out []string
	start := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\n':
			out = append(out, src[start:i])
			start = i + 1
		case '\r':
			out = append(out, src[start:i])
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(src) {
		out = append(out, src[start:])
	}
	return out
}

// lineLength signals lines longer than max runes, at the first rune past
// the limit.
func lineLength(m *matcher, f File, max int, out *collector) {
	for i, l := range lines(f.Source) {
		if !m.step() {
			return
		}
		if utf8.RuneCountInString(l) > max {
			out.add(i+1, max+1)
		}
	}
}

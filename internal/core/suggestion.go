package core

import "strings"

// SuggestionFits reports whether a suggestion can replace the lines from
// start to line of the file: each of them must be an added or context line
// on the RIGHT side, all in the same hunk. A start of 0 means the single
// line.
func (d FileDiff) SuggestionFits(side Side, start, line int) bool {
	if start == 0 {
		start = line
	}
	if side != Right || start < 1 || start > line {
		return false
	}
	hunk := -1
	for n := start; n <= line; n++ {
		h, _, ok := d.lineAt(Right, n)
		if !ok || hunk >= 0 && h != hunk {
			return false
		}
		hunk = h
	}
	return true
}

// CodeBlock fences text as a Markdown code block, with a fence longer than
// any run of backticks in the text.
func CodeBlock(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "\n" + text + "\n" + fence
}

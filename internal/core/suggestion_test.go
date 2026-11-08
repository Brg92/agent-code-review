package core

import "testing"

func TestSuggestionFits(t *testing.T) {
	// RIGHT 1-4 in the first hunk (2 added), RIGHT 10-11 in the second.
	d := mustDiff(t, FileModified, "@@ -1,3 +1,4 @@\n a\n+b\n c\n d\n@@ -9,2 +10,2 @@\n e\n-f\n+g")
	tests := []struct {
		name        string
		side        Side
		start, line int
		want        bool
	}{
		{"one added line", Right, 0, 2, true},
		{"one context line", Right, 0, 3, true},
		{"added and context lines of one hunk", Right, 1, 4, true},
		{"across two hunks", Right, 4, 10, false},
		{"partly outside the hunks", Right, 3, 6, false},
		{"outside the hunks", Right, 0, 7, false},
		{"left side", Left, 0, 10, false},
		{"start after line", Right, 4, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.SuggestionFits(tt.side, tt.start, tt.line); got != tt.want {
				t.Errorf("SuggestionFits(%s, %d, %d) = %v, want %v", tt.side, tt.start, tt.line, got, tt.want)
			}
		})
	}
	if mustDiff(t, FileAdded, "").SuggestionFits(Right, 0, 1) {
		t.Error("a suggestion fits a file without a patch")
	}
}

func TestSuggestionFitsRecordedS3(t *testing.T) {
	d := diffOf(t, recordedDiffs(t, "S3", "core-lib", 1), "src/main/java/com/example/core/session/SessionExpiry.kt")
	if !d.SuggestionFits(Right, 5, 9) {
		t.Error("S3's five-line suggestion on SessionExpiry.kt lines 5-9 does not fit")
	}
	if d.SuggestionFits(Right, 5, 12) {
		t.Error("a suggestion past the end of the added file fits")
	}
}

func TestCodeBlock(t *testing.T) {
	tests := []struct{ in, want string }{
		{"return x", "```\nreturn x\n```"},
		{"a\nb", "```\na\nb\n```"},
		{"val s = \"```\"", "````\nval s = \"```\"\n````"},
		{"", "```\n\n```"},
	}
	for _, tt := range tests {
		if got := CodeBlock(tt.in); got != tt.want {
			t.Errorf("CodeBlock(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

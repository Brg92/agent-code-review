package core

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// layout writes each line of the hunks as "<kind> <old> <new> <text>",
// with "$" after a line that has no newline at the end.
func layout(hunks []Hunk) []string {
	var out []string
	for _, h := range hunks {
		out = append(out, fmt.Sprintf("@@ %d,%d %d,%d %q", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section))
		for _, l := range h.Lines {
			s := fmt.Sprintf("%s %d %d %s", l.Kind, l.Old, l.New, l.Text)
			if l.NoNewline {
				s += "$"
			}
			out = append(out, s)
		}
	}
	return out
}

func TestParsePatch(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  []string
	}{
		{"empty", "", nil},
		{"two hunks", "@@ -1,3 +1,4 @@ package a\n a\n+b\n c\n-d\n+e\n@@ -10,2 +11,1 @@\n x\n-y",
			[]string{
				`@@ 1,3 1,4 " package a"`,
				"context 1 1 a", "added 0 2 b", "context 2 3 c", "removed 3 0 d", "added 0 4 e",
				`@@ 10,2 11,1 ""`,
				"context 10 11 x", "removed 11 0 y",
			}},
		{"counts default to one", "@@ -5 +5 @@\n-a\n+b",
			[]string{`@@ 5,1 5,1 ""`, "removed 5 0 a", "added 0 5 b"}},
		{"added file", "@@ -0,0 +1,2 @@\n+a\n+b",
			[]string{`@@ 0,0 1,2 ""`, "added 0 1 a", "added 0 2 b"}},
		{"removed file", "@@ -1,2 +0,0 @@\n-a\n-b",
			[]string{`@@ 1,2 0,0 ""`, "removed 1 0 a", "removed 2 0 b"}},
		{"file headers", "diff --git a/x.kt b/x.kt\nindex 1..2 100644\n--- a/x.kt\n+++ b/x.kt\n@@ -1 +1 @@\n-a\n+b\n",
			[]string{`@@ 1,1 1,1 ""`, "removed 1 0 a", "added 0 1 b"}},
		{"no newline on both sides", "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file",
			[]string{`@@ 1,2 1,2 ""`, "context 1 1 a", "removed 2 0 b$", "added 0 2 c$"}},
		{"no newline on context", "@@ -1,2 +1,3 @@\n+a\n b\n c\n\\ No newline at end of file",
			[]string{`@@ 1,2 1,3 ""`, "added 0 1 a", "context 1 2 b", "context 2 3 c$"}},
		{"blank context line without its space", "@@ -1,3 +1,3 @@\n a\n\n-c\n+d",
			[]string{`@@ 1,3 1,3 ""`, "context 1 1 a", "context 2 2 ", "removed 3 0 c", "added 0 3 d"}},
		{"carriage returns", "@@ -1 +1 @@\r\n-a\r\n+b\r\n",
			[]string{`@@ 1,1 1,1 ""`, "removed 1 0 a", "added 0 1 b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hunks, err := ParsePatch(tt.patch)
			if err != nil {
				t.Fatalf("ParsePatch: %v", err)
			}
			if got := layout(hunks); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestParsePatchRejectsMalformedPatches(t *testing.T) {
	tests := []struct{ name, patch, want string }{
		{"bad header", "@@ -a +1 @@\n+x", "line 1: malformed hunk header"},
		{"line before any hunk", "+x", "line 1: outside a hunk"},
		{"line after a full hunk", "@@ -1 +1 @@\n-a\n+b\n+c", "line 4: outside a hunk"},
		{"too many removed lines", "@@ -1,1 +1,2 @@\n-a\n-b\n+c\n+d", "line 3: more removed lines than the hunk header says"},
		{"too many context lines", "@@ -1,1 +1,2 @@\n a\n b", "line 3: more context lines than the hunk header says"},
		{"truncated hunk", "@@ -1,2 +1,2 @@\n a", "line 1: hunk ends early: 1 old and 1 new lines missing"},
		{"marker without a line", "@@ -0,0 +0,0 @@\n\\ No newline at end of file", "line 2: no line before"},
		{"unknown line in a hunk", "@@ -1,2 +1,2 @@\n a\n*b", "line 3: unknown line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePatch(tt.patch)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ParsePatch error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestNewFileDiff(t *testing.T) {
	d, err := NewFileDiff("b/x.kt", "a/x.kt", FileRenamed, "@@ -1 +1 @@\n-a\n+b")
	if err != nil {
		t.Fatal(err)
	}
	if d.Path != "b/x.kt" || d.OldPath != "a/x.kt" || d.Status != FileRenamed || len(d.Hunks) != 1 {
		t.Errorf("NewFileDiff = %+v", d)
	}
	if _, err := NewFileDiff("x.kt", "", FileModified, "+x"); err == nil || !strings.Contains(err.Error(), "x.kt") {
		t.Errorf("error = %v, want one naming the file", err)
	}
	if _, err := NewFileDiff("x.kt", "", "moved", ""); err == nil {
		t.Error("an unknown status is accepted")
	}
	none, err := NewFileDiff("big.kt", "", FileAdded, "")
	if err != nil || none.Hunks != nil || none.Visible() {
		t.Errorf("a file without a patch: %+v %v, want no hunks and nothing visible", none, err)
	}
}

func TestFileDiffLineLookup(t *testing.T) {
	d, err := NewFileDiff("x.kt", "", FileModified, "@@ -1,3 +1,3 @@\n a\n-b\n+c\n d\n@@ -9 +9,2 @@\n e\n+f")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		side Side
		line int
		want string
	}{
		{Right, 1, "0 context a"},
		{Right, 2, "0 added c"},
		{Right, 3, "0 context d"},
		{Right, 10, "1 added f"},
		{Right, 4, "none"},
		{Left, 2, "0 removed b"},
		{Left, 3, "0 context d"},
		{Left, 9, "1 context e"},
		{Left, 10, "none"},
	}
	for _, tt := range tests {
		got := "none"
		if h, l, ok := d.lineAt(tt.side, tt.line); ok {
			got = fmt.Sprintf("%d %s %s", h, l.Kind, l.Text)
		}
		if got != tt.want {
			t.Errorf("lineAt(%s, %d) = %q, want %q", tt.side, tt.line, got, tt.want)
		}
	}
	if !d.Visible() {
		t.Error("a file with hunks has visible lines")
	}
}

package core

import (
	"fmt"
	"testing"
)

func mustDiff(t *testing.T, status FileStatus, patch string) FileDiff {
	t.Helper()
	d, err := NewFileDiff("a/B.kt", "", status, patch)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func show(a *Anchor) string {
	if a == nil {
		return "none"
	}
	s := fmt.Sprintf("%s %s %d", a.Kind, a.Side, a.Line)
	if a.RequestedLine != 0 {
		s += fmt.Sprintf(" for %d", a.RequestedLine)
	}
	return s
}

func TestAnchor(t *testing.T) {
	// RIGHT 10-14 and 30-31, LEFT 10-13 and 26-28.
	two := "@@ -10,4 +10,5 @@\n a\n-b\n+c\n+d\n e\n f\n@@ -26,3 +30,2 @@\n g\n-h\n-i\n+j"
	tests := []struct {
		name  string
		patch string
		side  Side
		line  int
		want  string
	}{
		{"added line", two, Right, 11, "added RIGHT 11"},
		{"context line", two, Right, 13, "context RIGHT 13"},
		{"removed line", two, Left, 11, "removed LEFT 11"},
		{"context line asked on the left", two, Left, 12, "context RIGHT 13"},
		{"above the first hunk", two, Right, 3, "relocated RIGHT 10 for 3"},
		{"between hunks, nearer the first", two, Right, 20, "relocated RIGHT 14 for 20"},
		{"between hunks, nearer the second", two, Right, 25, "relocated RIGHT 30 for 25"},
		{"between hunks, tie goes to the lower line", two, Right, 22, "relocated RIGHT 14 for 22"},
		{"below the last hunk", two, Right, 90, "relocated RIGHT 31 for 90"},
		{"left line outside the hunks", two, Left, 18, "relocated RIGHT 14 for 18"},
		{"left line nearer a context line of the second hunk", two, Left, 20, "relocated RIGHT 30 for 20"},
		{"left line relocated to a removed line", two, Left, 40, "relocated LEFT 28 for 40"},
		{"removed file", "@@ -1,3 +0,0 @@\n-a\n-b\n-c", Left, 2, "removed LEFT 2"},
		{"right line on a diff with only removals", "@@ -5,2 +4,0 @@\n-a\n-b", Right, 4, "relocated LEFT 5 for 4"},
		{"no patch", "", Right, 7, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := mustDiff(t, FileModified, tt.patch)
			if got := show(d.Anchor(tt.side, tt.line)); got != tt.want {
				t.Errorf("Anchor(%s, %d) = %s, want %s", tt.side, tt.line, got, tt.want)
			}
		})
	}
}

func TestAnchorLead(t *testing.T) {
	relocated := Anchor{Kind: AnchorRelocated, Side: Right, Line: 16, RequestedLine: 27}
	if got, want := relocated.Lead("app/ui/HomeViewModel.kt"), "Refers to line 27 of `app/ui/HomeViewModel.kt`, outside the changed lines."; got != want {
		t.Errorf("Lead = %q, want %q", got, want)
	}
	for _, k := range []AnchorKind{AnchorAdded, AnchorContext, AnchorRemoved} {
		if got := (Anchor{Kind: k, Side: Right, Line: 3}).Lead("x.kt"); got != "" {
			t.Errorf("%s anchor Lead = %q, want none", k, got)
		}
	}
}

func TestAnchorRecordedS1(t *testing.T) {
	diffs := recordedDiffs(t, "S1", "sample-app", 2)
	const dir = "app/src/main/java/com/example/sample/"
	tests := []struct {
		path string
		line int
		want string
	}{
		{"MainActivity.kt", 13, "added RIGHT 13"},
		{"data/ProfileStore.kt", 8, "context RIGHT 8"},
		{"data/SessionRepository.kt", 13, "relocated RIGHT 15 for 13"},
		{"ui/HomeViewModel.kt", 27, "relocated RIGHT 16 for 27"},
	}
	for _, tt := range tests {
		if got := show(diffOf(t, diffs, dir+tt.path).Anchor(Right, tt.line)); got != tt.want {
			t.Errorf("%s:%d anchors %s, want %s", tt.path, tt.line, got, tt.want)
		}
	}
	// S2 core-lib: the removed legacyToken() is on the LEFT side.
	core := diffOf(t, recordedDiffs(t, "S2", "core-lib", 3), "src/main/java/com/example/core/session/SessionManager.kt")
	if got := show(core.Anchor(Left, 39)); got != "removed LEFT 39" {
		t.Errorf("SessionManager.kt LEFT 39 anchors %s, want removed LEFT 39", got)
	}
}

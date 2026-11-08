package core

import (
	"fmt"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// recordedDiffs parses the recorded changed files of a sandbox pull
// request.
func recordedDiffs(t *testing.T, scenario, repo string, number int) []FileDiff {
	t.Helper()
	var diffs []FileDiff
	for _, f := range sandbox.PullFiles(t, scenario, repo, number) {
		d, err := recordedDiff(f)
		if err != nil {
			t.Fatalf("%s %s#%d: %v", scenario, repo, number, err)
		}
		diffs = append(diffs, d)
	}
	return diffs
}

func recordedDiff(f sandbox.File) (FileDiff, error) {
	patch := ""
	if f.Patch != nil {
		patch = *f.Patch
	}
	return NewFileDiff(f.Filename, f.PreviousFilename, FileStatus(f.Status), patch)
}

// TestParseRecordedPatches parses every recorded patch: each one counts
// as many added and removed lines as the platform says.
func TestParseRecordedPatches(t *testing.T) {
	lists := sandbox.AllPullFiles(t)
	if len(lists) == 0 {
		t.Fatal("no recorded pull request files")
	}
	for _, l := range lists {
		for _, f := range l.Files {
			name := fmt.Sprintf("%s %s#%d %s", l.Scenario, l.Repo, l.Number, f.Filename)
			d, err := recordedDiff(f)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			added, removed := 0, 0
			for _, h := range d.Hunks {
				for _, line := range h.Lines {
					switch line.Kind {
					case LineAdded:
						added++
					case LineRemoved:
						removed++
					}
				}
			}
			if f.Patch != nil && (added != f.Additions || removed != f.Deletions) {
				t.Errorf("%s: %d added and %d removed lines, the platform counts %d and %d", name, added, removed, f.Additions, f.Deletions)
			}
			if f.Patch == nil && d.Visible() {
				t.Errorf("%s: no patch, but visible lines", name)
			}
		}
	}
}

func diffOf(t *testing.T, diffs []FileDiff, path string) FileDiff {
	t.Helper()
	for _, d := range diffs {
		if d.Path == path {
			return d
		}
	}
	t.Fatalf("no diff for %s", path)
	return FileDiff{}
}

func TestRecordedS3FileChanges(t *testing.T) {
	diffs := recordedDiffs(t, "S3", "core-lib", 1)
	const dir = "src/main/java/com/example/core/"

	rename := diffOf(t, diffs, dir+"session/codec/TokenCodec.kt")
	if rename.Status != FileRenamed || rename.OldPath != dir+"session/TokenCodec.kt" {
		t.Errorf("TokenCodec.kt: status %s, old path %q", rename.Status, rename.OldPath)
	}
	if _, l, ok := rename.lineAt(Left, 7); !ok || l.Kind != LineRemoved || l.Text != `        require(token.isNotBlank()) { "token must not be blank" }` {
		t.Errorf("TokenCodec.kt LEFT 7 = %+v %v, want the removed require()", l, ok)
	}
	if _, l, ok := rename.lineAt(Right, 7); !ok || l.Kind != LineContext || l.Old != 8 {
		t.Errorf("TokenCodec.kt RIGHT 7 = %+v %v, want the context line that was LEFT 8", l, ok)
	}

	removed := diffOf(t, diffs, dir+"session/LegacySessionStore.kt")
	if removed.Status != FileRemoved || len(removed.Hunks) != 1 || len(removed.Hunks[0].Lines) != 27 {
		t.Fatalf("LegacySessionStore.kt = %+v", removed)
	}
	for i, l := range removed.Hunks[0].Lines {
		if l.Kind != LineRemoved || l.Old != i+1 || l.New != 0 {
			t.Errorf("LegacySessionStore.kt line %d = %+v, want removed", i+1, l)
		}
	}

	added := diffOf(t, diffs, dir+"session/SessionExpiry.kt")
	for n := 5; n <= 9; n++ {
		if h, l, ok := added.lineAt(Right, n); !ok || h != 0 || l.Kind != LineAdded {
			t.Errorf("SessionExpiry.kt RIGHT %d = %+v %v, want an added line of the first hunk", n, l, ok)
		}
	}

	for _, path := range []string{dir + "ui/badge/StatusBadgeView.kt", "src/main/res/drawable-nodpi/badge_background.png"} {
		if d := diffOf(t, diffs, path); d.Hunks != nil || d.Visible() {
			t.Errorf("%s has no patch, got hunks %+v", path, d.Hunks)
		}
	}
	if d := diffOf(t, diffs, dir+"ui/badge/StatusBadgeView.kt"); d.Status != FileRenamed || d.OldPath != dir+"ui/StatusBadgeView.kt" {
		t.Errorf("StatusBadgeView.kt: the rename without a patch keeps status %s and old path %q", d.Status, d.OldPath)
	}
}

func TestRecordedS4FileWithoutPatch(t *testing.T) {
	diffs := recordedDiffs(t, "S4", "feature-lib", 1)
	big := diffOf(t, diffs, "src/main/java/com/example/feature/catalog/IconCatalog.kt")
	if big.Status != FileAdded || big.Hunks != nil || big.Visible() {
		t.Errorf("IconCatalog.kt = %+v, want an added file without hunks", big)
	}
	if d := diffOf(t, diffs, "src/main/java/com/example/feature/FeatureToggle.kt"); !d.Visible() {
		t.Error("FeatureToggle.kt has a patch")
	}
}

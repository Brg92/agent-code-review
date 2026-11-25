package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/state"
)

const (
	headA = "aaaaaaaa0000000000000000000000000000000a"
	headB = "bbbbbbbb0000000000000000000000000000000b"
)

var localNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func localKey(t testing.TB, s string) core.Key {
	t.Helper()
	k, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// saveRun writes a run of root in phase at runID, whose root has head, and
// entries as the rest of the cascade.
func saveRun(t testing.TB, work, runID string, root core.Key, head string, phase state.Phase, others ...core.Entry) {
	t.Helper()
	entries := append([]core.Entry{{CR: root, Status: core.Pending, HeadSHA: head}}, others...)
	st, err := state.New(runID, root, core.DefaultLimits(), entries, localNow)
	if err != nil {
		t.Fatal(err)
	}
	st.Phase = phase
	if phase != state.Resolved {
		for i := range st.ChangeRequests {
			if st.ChangeRequests[i].Status != core.Skipped {
				st.ChangeRequests[i].Status = core.Finalized
			}
		}
	}
	dir, err := state.CreateRunDir(work, runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(dir, st); err != nil {
		t.Fatal(err)
	}
}

func TestLocalReviewsFindsTheMostRecentRunOfARoot(t *testing.T) {
	work := t.TempDir()
	app := localKey(t, "github:github.com/acme/app#1")
	lib := localKey(t, "github:github.com/acme/lib#2")
	saveRun(t, work, "20260101T000000Z-00000001", app, headA, state.Finalized)
	saveRun(t, work, "20260102T000000Z-00000002", app, headB, state.Collected)
	// lib is in the cascade of app but is not its root: not a review of lib.
	saveRun(t, work, "20260103T000000Z-00000003", lib, headA, state.Published)

	got, err := ReadLocalReviews(work)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := got.Of(app); !ok || r.RunID != "20260102T000000Z-00000002" || r.Phase != state.Collected || r.Head != headB {
		t.Errorf("Of(app) = %+v, %v, want the later run, collected, at head B", r, ok)
	}
	if r, ok := got.Of(lib); !ok || r.Phase != state.Published || r.Head != headA {
		t.Errorf("Of(lib) = %+v, %v", r, ok)
	}
	if _, ok := got.Of(localKey(t, "github:github.com/acme/other#9")); ok {
		t.Error("Of(other) found a review that never ran")
	}
}

func TestLocalReviewsIgnoresARelatedChangeRequest(t *testing.T) {
	work := t.TempDir()
	app := localKey(t, "github:github.com/acme/app#1")
	lib := localKey(t, "github:github.com/acme/lib#2")
	parent := app
	saveRun(t, work, "20260102T000000Z-00000002", app, headA, state.Finalized,
		core.Entry{CR: lib, Depth: 1, Parent: &parent, Status: core.Finalized, HeadSHA: headB})

	got, err := ReadLocalReviews(work)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Of(lib); ok {
		t.Error("a change request that is only in the cascade counts as reviewed on its own")
	}
}

func TestLocalReviewsMatchesKeysWithoutRegardToCase(t *testing.T) {
	work := t.TempDir()
	saveRun(t, work, "20260102T000000Z-00000002", localKey(t, "github:github.com/Acme/App#1"), headA, state.Finalized)
	got, err := ReadLocalReviews(work)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Of(localKey(t, "github:github.com/acme/app#1")); !ok {
		t.Error("the lookup is case sensitive: GitHub names are not")
	}
}

func TestLocalReviewsSkipsWhatIsNotAReview(t *testing.T) {
	work := t.TempDir()
	app := localKey(t, "github:github.com/acme/app#1")
	// Resolved, never collected: no head, nothing was reviewed.
	saveRun(t, work, "20260104T000000Z-00000004", app, "", state.Resolved)
	saveRun(t, work, "20260101T000000Z-00000001", app, headA, state.Judged)
	runs := filepath.Join(work, "runs")
	// A directory with no state, a damaged state and a file are no run.
	for name, content := range map[string]string{"20260105T000000Z-00000005": "", "20260106T000000Z-00000006": "{", "not-a-run-id!": ""} {
		if err := os.MkdirAll(filepath.Join(runs, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if content != "" {
			if err := os.WriteFile(filepath.Join(runs, name, state.FileName), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(runs, "20260107T000000Z-00000007"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLocalReviews(work)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := got.Of(app); !ok || r.RunID != "20260101T000000Z-00000001" || r.Phase != state.Judged {
		t.Errorf("Of(app) = %+v, %v, want the judged run: the later ones reviewed nothing", r, ok)
	}
	if got.Unreadable != 1 {
		t.Errorf("Unreadable = %d, want 1 (the damaged state)", got.Unreadable)
	}
}

func TestLocalReviewsOfAMissingWorkDirIsEmpty(t *testing.T) {
	got, err := ReadLocalReviews(filepath.Join(t.TempDir(), "nothing"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Of(localKey(t, "github:github.com/acme/app#1")); ok {
		t.Error("found a review in a work directory that is not there")
	}
}

func TestLocalReviewsFailsWhenTheRunsCannotBeListed(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "runs"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadLocalReviews(work)
	if f := asFailure(t, err, KindUnexpected); !strings.Contains(f.Where, "runs") {
		t.Errorf("Where = %q, want the runs directory", f.Where)
	}
}

func TestLocalReviewHeadChangedSince(t *testing.T) {
	r := LocalReview{Head: headA}
	if r.HeadChanged(headA) || !r.HeadChanged(headB) {
		t.Errorf("HeadChanged: same head %v, other head %v, want false, true", r.HeadChanged(headA), r.HeadChanged(headB))
	}
	if r.Head8() != "aaaaaaaa" {
		t.Errorf("Head8() = %q", r.Head8())
	}
}

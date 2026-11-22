package review

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/state"
)

// LocalReview is what the work directory knows of the review of a change
// request: its most recent run that has it as its root and that collected
// it.
type LocalReview struct {
	RunID string
	Phase state.Phase
	// Head is the head commit the run reviewed.
	Head string
}

// Head8 is the first 8 characters of the head, as the work directory names
// it.
func (r LocalReview) Head8() string {
	if len(r.Head) < 8 {
		return r.Head
	}
	return r.Head[:8]
}

// HeadChanged reports whether the change request has a head other than the
// one the run reviewed.
func (r LocalReview) HeadChanged(current string) bool { return !strings.EqualFold(r.Head, current) }

// LocalReviews are the reviews the work directory holds, by change request.
type LocalReviews struct {
	byKey map[string]LocalReview
	// Unreadable is how many runs have a state file that cannot be read:
	// they are left out, since what they reviewed is unknown.
	Unreadable int
}

// Of returns the most recent review of root. Names on the platform do not
// depend on case, and neither does the lookup.
func (l LocalReviews) Of(root core.Key) (LocalReview, bool) {
	r, ok := l.byKey[strings.ToLower(root.String())]
	return r, ok
}

// ReadLocalReviews reads the state of every run in the work directory and
// keeps, for each change request, the most recent run (by run id, which
// sorts by time) that has it as its root and whose root was collected, so
// that it has a head. It reads state files only; a run whose state cannot
// be read is counted, never an error. A work directory with no runs is
// empty.
func ReadLocalReviews(workDir string) (LocalReviews, error) {
	out := LocalReviews{byKey: map[string]LocalReview{}}
	runs := filepath.Join(workDir, "runs")
	entries, err := os.ReadDir(runs)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, &Failure{Kind: KindUnexpected, What: "could not list the runs", Where: runs, Why: err.Error(), Err: err,
			Hint: "check that the work directory can be read"}
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && state.ValidRunID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	// Oldest first, so that a later run replaces an earlier one.
	sort.Strings(ids)
	for _, id := range ids {
		st, err := state.Load(filepath.Join(runs, id))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			out.Unreadable++
			continue
		}
		for _, e := range st.ChangeRequests {
			if e.Status == core.Skipped || !e.CR.Same(st.Root) || e.HeadSHA == "" {
				continue
			}
			out.byKey[strings.ToLower(st.Root.String())] = LocalReview{RunID: id, Phase: st.Phase, Head: e.HeadSHA}
		}
	}
	return out, nil
}

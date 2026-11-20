package review

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// Opened is a run found in the work directory.
type Opened struct {
	ID string
	// Dir is the work directory of the run.
	Dir   string
	State *state.State
}

// OpenRun finds the run the commands after review work on. With a run id
// it is that run, whatever its phase; the id must be a run id. Without
// one it is the most recent run that is not published, by run id, which
// sorts by time: a published run is never chosen by itself, and a
// directory without a state file, left by a review that failed before it
// saved one, is no run. A state file that cannot be read is an error, never
// a reason to fall back to an older run. The run's log is attached to log,
// when there is one, so that the lines of the command are appended to it.
func OpenRun(workDir, runID string, log *runlog.Log) (*Opened, error) {
	if runID != "" {
		return openNamed(workDir, runID, log)
	}
	runs := filepath.Join(workDir, "runs")
	entries, err := os.ReadDir(runs)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, &Failure{Kind: KindUnexpected, What: "could not list the runs", Where: runs, Why: err.Error(), Err: err,
			Hint: "check that the work directory can be read"}
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && state.ValidRunID(e.Name()) {
			ids = append(ids, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	for _, id := range ids {
		o, err := openDir(filepath.Join(runs, id), id)
		var f *Failure
		switch {
		case errors.As(err, &f) && f.Err != nil && errors.Is(f.Err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, err
		case o.State.Phase == state.Published:
			continue
		}
		return attach(o, log)
	}
	return nil, &Failure{Kind: KindState, What: "there is no run to continue", Where: runs,
		Why: "no run that is not published", Hint: "run `agent-code-review review <pull request>` first"}
}

func openNamed(workDir, runID string, log *runlog.Log) (*Opened, error) {
	if !state.ValidRunID(runID) {
		return nil, &Failure{Kind: KindUsage, What: "the run id is not a run id", Where: "--run",
			Why: "a run id is 8 to 64 letters, digits and hyphens", Hint: "use the id that `agent-code-review review` printed"}
	}
	o, err := openDir(filepath.Join(workDir, "runs", runID), runID)
	var f *Failure
	if errors.As(err, &f) && f.Err != nil && errors.Is(f.Err, fs.ErrNotExist) {
		return nil, &Failure{Kind: KindState, What: "there is no run " + runID, Where: filepath.Join(workDir, "runs", runID),
			Why: "no state file", Hint: "run `agent-code-review review <pull request>` first, or give the id of a run that exists"}
	}
	if err != nil {
		return nil, err
	}
	return attach(o, log)
}

// openDir loads the state of the run in dir.
func openDir(dir, id string) (*Opened, error) {
	st, err := state.Load(dir)
	switch {
	case err == nil:
		return &Opened{ID: id, Dir: dir, State: st}, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, &Failure{Kind: KindState, What: "the run has no state file", Where: filepath.Join(dir, state.FileName), Why: "missing", Err: err}
	}
	return nil, &Failure{Kind: KindState, What: "the state of run " + id + " cannot be read", Where: filepath.Join(dir, state.FileName),
		Why: err.Error(), Err: err, Hint: "the run was written by another version or is damaged: run `agent-code-review review <pull request>` again"}
}

func attach(o *Opened, log *runlog.Log) (*Opened, error) {
	if log != nil {
		if err := log.Attach(o.Dir); err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not open the run log", Where: o.Dir, Why: err.Error(), Err: err,
				Hint: "check that the work directory can be written"}
		}
	}
	return o, nil
}

package review

import (
	"os"
	"path/filepath"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/state"
)

// loaded is a reviewed change request of a run with its work package.
type loaded struct {
	Entry core.Entry
	// Dir is its directory in the run.
	Dir string
	WP  core.WorkPackage
}

// damaged is the failure of a run whose files do not hold together.
func damaged(what, where, why string, err error) *Failure {
	return &Failure{Kind: KindState, What: what, Where: where, Why: why, Err: err,
		Hint: "the run is damaged: run `agent-code-review review <pull request>` again"}
}

// loadPackages reads the work package of every change request of the run
// in review order, related first and the root last.
func loadPackages(o *Opened) ([]loaded, error) {
	var out []loaded
	for _, e := range reviewOrder(o.State.ChangeRequests) {
		name, err := dirName(e)
		if err != nil {
			return nil, damaged("the directory of "+e.CR.String()+" cannot be named", e.CR.String()+", "+state.FileName, err.Error(), err)
		}
		dir := filepath.Join(o.Dir, name)
		wp, err := readWorkPackageFile(filepath.Join(dir, state.WorkPackageFile), e.CR)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded{Entry: e, Dir: dir, WP: wp})
	}
	return out, nil
}

// cascadeOf rebuilds the resolved cascade of a run from its state and the
// descriptions in the work packages, which review resolved it from: the
// entries and the review order are in the state; the lines of a Related
// Changes section that were not followed or not understood are found
// again by reading the sections the same way, in the order the walk met
// them. Nothing is read from the platform.
func cascadeOf(o *Opened, loads []loaded, registries []refs.Registry) (cascade.Result, error) {
	reg, err := composeRegistry(registries, o.State.Root.Forge, o.State.Root.String())
	if err != nil {
		f := err.(*Failure)
		f.Kind, f.Hint = KindUnexpected, "report it"
		return cascade.Result{}, f
	}
	entries := o.State.ChangeRequests
	res := cascade.Result{Entries: entries}
	for _, l := range loads {
		res.Order = append(res.Order, l.Entry.CR)
	}
	descriptions := map[string]string{}
	for _, l := range loads {
		descriptions[lower(l.Entry.CR)] = l.WP.Description
	}
	visited := map[string]bool{}
	pos := 1
	var walk func(e core.Entry)
	walk = func(e core.Entry) {
		visited[lower(e.CR)] = true
		for _, line := range refs.Related(descriptions[lower(e.CR)], reg, e.CR) {
			l := cascade.Line{CR: e.CR, Line: line.Line, Text: line.Text}
			switch line.Kind() {
			case refs.Unparsed:
				res.Unparsed = append(res.Unparsed, l)
			case refs.NotFollowed:
				res.NotFollowed = append(res.NotFollowed, l)
			default:
				// Every reference made one entry, found right after the ones
				// before it; a state that disagrees with the description is
				// followed as far as it agrees.
				for _, key := range line.Refs {
					if pos >= len(entries) || !entries[pos].CR.Same(key) {
						continue
					}
					child := entries[pos]
					pos++
					if child.Status != core.Skipped {
						walk(child)
					}
				}
			}
		}
	}
	walk(entries[0])
	// A change request the walk did not reach still has lines to report.
	for _, e := range entries {
		if e.Status != core.Skipped && !visited[lower(e.CR)] {
			walk(e)
		}
	}
	return res, nil
}

// writeAtomic writes data to path, which a reader sees either as it was or
// as it is.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o600)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		os.Remove(name)
	}
	return werr
}

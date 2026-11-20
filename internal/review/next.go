package review

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// servedFile is the file of a change request's directory that counts the
// parts of its work package served. It is not part of the run's state: the
// phase does not change when a part is served.
const servedFile = "served"

// DoneText is what next prints when nothing is left to judge.
const DoneText = "Done: run agent-code-review finalize\n"

// Served is what next gives the agent: the text to print, and which part
// of which change request it is. Done is set when every change request has
// a judgement.
type Served struct {
	Text  string
	Done  bool
	CR    core.Key
	Part  int
	Total int
}

// reviewOrder returns the change requests to review, related first and the
// root last: the post-order of the cascade, which the entries of the state
// hold in discovery order, depth first.
func reviewOrder(entries []core.Entry) []core.Entry {
	var out, open []core.Entry
	for _, e := range entries {
		if e.Status == core.Skipped {
			continue
		}
		for len(open) > 0 && open[len(open)-1].Depth >= e.Depth {
			out = append(out, open[len(open)-1])
			open = open[:len(open)-1]
		}
		open = append(open, e)
	}
	for i := len(open) - 1; i >= 0; i-- {
		out = append(out, open[i])
	}
	return out
}

// Next serves the next part to judge of a collected run: the first change
// request, in review order, without a judgement that parses, and in it the
// part after the last one served; after the last part it starts again, so
// that an agent that lost them can read them again. It reads the run and
// writes only the count of the parts served; the phase does not change.
// Whether a judgement is valid is for finalize to say.
func Next(o *Opened, log *runlog.Log) (Served, error) {
	log.SetCR(core.Key{})
	if o.State.Phase != state.Collected {
		f := &Failure{Kind: KindState, What: "the run is not ready to be judged", Where: o.Dir, Why: "its phase is " + string(o.State.Phase),
			Hint: "next serves a run that is collected and not yet judged; run `agent-code-review review <pull request>` for a new one"}
		if o.State.Phase == state.Resolved {
			f.Hint = "the review of this run did not finish; run `agent-code-review review <pull request>` again"
		}
		return Served{}, f
	}
	for _, e := range reviewOrder(o.State.ChangeRequests) {
		name, err := dirName(e)
		if err != nil {
			return Served{}, &Failure{Kind: KindState, What: "the directory of " + e.CR.String() + " cannot be named", Where: e.CR.String() + ", " + state.FileName, Why: err.Error(), Err: err,
				Hint: "the run is damaged: run `agent-code-review review <pull request>` again"}
		}
		dir := filepath.Join(o.Dir, name)
		wp, err := readWorkPackageFile(filepath.Join(dir, state.WorkPackageFile), e.CR)
		if err != nil {
			return Served{}, err
		}
		if wp.Parts.Total < 1 || len(wp.Parts.Files) != wp.Parts.Total {
			return Served{}, &Failure{Kind: KindState, What: "the work package of " + e.CR.String() + " has no parts to serve", Where: e.CR.String() + ", parts",
				Why: "total " + strconv.Itoa(wp.Parts.Total), Hint: "the run is damaged: run `agent-code-review review <pull request>` again"}
		}
		judgement, err := insideRun(o.Dir, wp.JudgementPath, e.CR)
		if err != nil {
			return Served{}, err
		}
		judged, why := judgementState(judgement)
		if judged {
			continue
		}
		part, err := nextPart(dir, wp.Parts.Total)
		if err != nil {
			return Served{}, err
		}
		brief, err := render.Brief(wp, part)
		if err != nil {
			return Served{}, &Failure{Kind: KindUnexpected, What: "could not write the brief of " + e.CR.String(), Where: e.CR.String() + ", part " + strconv.Itoa(part),
				Why: err.Error(), Err: err, Hint: "the change request holds text the brief cannot carry safely; it is not served"}
		}
		header := fmt.Sprintf("Judge %s part %d of %d — write the judgement to %s", e.CR, part, wp.Parts.Total, judgement)
		if why != "" {
			header += " (" + why + ")"
		}
		log.SetCR(e.CR)
		log.Event(fmt.Sprintf("serve part %d of %d", part, wp.Parts.Total), "ok")
		return Served{Text: header + "\n" + brief, CR: e.CR, Part: part, Total: wp.Parts.Total}, nil
	}
	log.Event("nothing left to judge", "ok")
	return Served{Text: DoneText, Done: true}, nil
}

// readWorkPackageFile reads the work package of a change request of the run.
func readWorkPackageFile(path string, cr core.Key) (core.WorkPackage, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var wp core.WorkPackage
		if wp, err = core.DecodeWorkPackage(data); err == nil {
			return wp, nil
		}
	}
	return core.WorkPackage{}, &Failure{Kind: KindState, What: "the work package of " + cr.String() + " cannot be read", Where: cr.String() + ", " + path,
		Why: err.Error(), Err: err, Hint: "the run is damaged: run `agent-code-review review <pull request>` again"}
}

// insideRun resolves a path the work package gives, relative to the run's
// work directory, and refuses one that leaves it. The path has the form of
// the relativePath of the contract on every system: forward slashes and
// segments that are not empty, "." or "..", and no backslash or colon, which
// Windows reads as a separator and a drive.
func insideRun(runDir, rel string, cr core.Key) (string, error) {
	ok := rel != "" && !strings.ContainsAny(rel, `\:`)
	for _, seg := range strings.Split(rel, "/") {
		ok = ok && seg != "" && seg != "." && seg != ".."
	}
	if !ok {
		return "", &Failure{Kind: KindState, What: "the work package of " + cr.String() + " names a path outside the run", Where: cr.String() + ", judgementPath",
			Why: "path not allowed", Hint: "the run is damaged: run `agent-code-review review <pull request>` again"}
	}
	return filepath.Join(runDir, filepath.FromSlash(rel)), nil
}

// judgementState says whether the judgement at path counts as written: the
// file exists and parses. A file that exists and does not parse is
// explained, so that the agent knows it is replaced.
func judgementState(path string) (judged bool, why string) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, ""
	case err != nil:
		return false, "the file there cannot be read and is replaced"
	}
	if _, err := core.DecodeJudgement(data); err != nil {
		return false, "the file there does not parse and is replaced"
	}
	return true, ""
}

// nextPart returns the part to serve of a change request with total parts
// and counts it as served.
func nextPart(dir string, total int) (int, error) {
	path := filepath.Join(dir, servedFile)
	served := 0
	if data, err := os.ReadFile(path); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && n >= 0 {
			served = n
		}
	}
	part := served%total + 1
	if err := os.WriteFile(path, []byte(strconv.Itoa(part)+"\n"), 0o600); err != nil {
		return 0, &Failure{Kind: KindUnexpected, What: "could not record the part served", Where: path, Why: err.Error(), Err: err,
			Hint: "check that the work directory can be written"}
	}
	return part, nil
}

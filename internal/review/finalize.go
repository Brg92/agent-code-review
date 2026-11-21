package review

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// FinalizeDeps are what finalize needs from the outside: nothing from the
// platform.
type FinalizeDeps struct {
	// Packs are the loaded packs, which define the rules of the work
	// packages.
	Packs *packs.Set
	// Registries are the reference registries, as for review.
	Registries []refs.Registry
	// Version is the version the reports are headed with.
	Version string
	// Now is the clock.
	Now func() time.Time
}

// Finalized is what finalize made.
type Finalized struct {
	// Text is what the command prints: the final message with the paths of
	// the reports, the summary of the cascade and the actions to take.
	Text string
	// Reports are the paths of the report of each change request, in
	// review order.
	Reports []string
}

// problem is something wrong with the judgement of one change request.
type problem struct {
	cr   core.Key
	path string
	text []string
}

// judged is a change request with a valid judgement.
type judged struct {
	loaded
	review    core.Review
	judgement core.Judgement
}

// Finalize turns the judgements of a collected run into the findings and
// the report of each change request. It is all or nothing: it validates
// every judgement first and, if one is missing or invalid, writes nothing,
// leaves the phase as it is and fails with a KindValidation failure that
// lists every problem. Otherwise it writes findings.json and report.txt in
// the directory of each change request, the report made from findings.json
// as shown would make it, and saves the run as finalized in one step. It
// reads nothing from the platform: whether the head has moved is publish's
// to check.
func Finalize(o *Opened, d FinalizeDeps, log *runlog.Log) (*Finalized, error) {
	log.SetCR(core.Key{})
	if err := finalizable(o); err != nil {
		return nil, err
	}
	loads, err := loadPackages(o)
	if err != nil {
		return nil, err
	}
	res, err := cascadeOf(o, loads, d.Registries)
	if err != nil {
		return nil, err
	}

	var ok []judged
	var problems []problem
	for _, l := range loads {
		log.SetCR(l.Entry.CR)
		path, err := insideRun(o.Dir, l.WP.JudgementPath, l.Entry.CR)
		if err != nil {
			return nil, err
		}
		rv, err := core.ReviewOf(l.WP, d.Packs)
		if err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not rebuild the review of " + l.Entry.CR.String(), Where: l.Entry.CR.String() + ", " + state.WorkPackageFile,
				Why: err.Error(), Err: err, Hint: "install the packs the review was made with, next to the executable or in packPaths of the user configuration"}
		}
		j, texts := readJudgement(path, rv, l.Entry.CR)
		if texts != nil {
			problems = append(problems, problem{cr: l.Entry.CR, path: path, text: texts})
			log.Event("validate the judgement", "error")
			continue
		}
		log.Event("validate the judgement", "ok")
		ok = append(ok, judged{loaded: l, review: rv, judgement: j})
	}
	log.SetCR(core.Key{})
	if len(problems) > 0 {
		return nil, validationFailure(problems)
	}

	// Everything is valid: make every document before writing any file.
	type made struct {
		findings, report         []byte
		findingsPath, reportPath string
	}
	var all []made
	var reviewed []render.Reviewed
	for _, j := range ok {
		result, err := core.Finalize(j.review, j.judgement)
		if err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not finalize " + j.Entry.CR.String(), Where: j.Entry.CR.String(), Why: err.Error(), Err: err, Hint: "report it"}
		}
		data, err := core.EncodeDocument(result.Document(j.WP.Header()))
		if err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not write the findings of " + j.Entry.CR.String(), Where: j.Entry.CR.String(), Why: err.Error(), Err: err, Hint: "report it"}
		}
		// The report is made from the findings as they are written.
		doc, err := core.DecodeDocument(data)
		if err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not read back the findings of " + j.Entry.CR.String(), Where: j.Entry.CR.String(), Why: err.Error(), Err: err, Hint: "report it"}
		}
		m := made{findings: data, report: []byte(render.Report(d.Version, doc)),
			findingsPath: filepath.Join(j.Dir, state.FindingsFile), reportPath: filepath.Join(j.Dir, state.ReportFile)}
		all = append(all, m)
		reviewed = append(reviewed, render.Reviewed{Doc: doc, Report: m.reportPath})
	}
	text, err := render.Final(res, reviewed)
	if err != nil {
		return nil, &Failure{Kind: KindUnexpected, What: "could not summarise the cascade", Where: o.State.Root.String(), Why: err.Error(), Err: err, Hint: "report it"}
	}

	st := *o.State
	st.ChangeRequests = slices.Clone(o.State.ChangeRequests)
	for _, to := range []state.Phase{state.Judged, state.Finalized} {
		status := core.Judged
		if to == state.Finalized {
			status = core.Finalized
		}
		for i, e := range st.ChangeRequests {
			if e.Status != core.Skipped {
				st.ChangeRequests[i].Status = status
			}
		}
		if err := st.Advance(to, d.Now()); err != nil {
			return nil, damaged("the run cannot be finalized", o.Dir, err.Error(), err)
		}
	}
	var reports []string
	for i, m := range all {
		log.SetCR(ok[i].Entry.CR)
		for _, w := range []struct {
			path string
			data []byte
		}{{m.findingsPath, m.findings}, {m.reportPath, m.report}} {
			if err := writeAtomic(w.path, w.data); err != nil {
				return nil, &Failure{Kind: KindUnexpected, What: "could not write " + filepath.Base(w.path), Where: w.path, Why: err.Error(), Err: err,
					Hint: "check that the work directory can be written, then run `agent-code-review finalize` again"}
			}
		}
		reports = append(reports, m.reportPath)
		log.Event("finalize", "ok")
	}
	log.SetCR(core.Key{})
	if err := state.Save(o.Dir, &st); err != nil {
		return nil, &Failure{Kind: KindUnexpected, What: "could not save the state of the run", Where: filepath.Join(o.Dir, state.FileName), Why: err.Error(), Err: err,
			Hint: "check that the work directory can be written, then run `agent-code-review finalize` again"}
	}
	log.Event("finalize the run", "ok")
	o.State = &st
	return &Finalized{Text: text, Reports: reports}, nil
}

// finalizable checks the phase of the run.
func finalizable(o *Opened) error {
	switch p := o.State.Phase; p {
	case state.Collected:
		return nil
	case state.Judged, state.Finalized, state.Planned, state.Published:
		return &Failure{Kind: KindState, What: "the run is already finalized", Where: o.Dir, Why: "its phase is " + string(p),
			Hint: "to change a judgement, run `agent-code-review review <pull request>` again; `agent-code-review show` reads the reports"}
	default:
		return &Failure{Kind: KindState, What: "the run is not ready to be finalized", Where: o.Dir, Why: "its phase is " + string(p),
			Hint: "the review of this run did not finish; run `agent-code-review review <pull request>` again"}
	}
}

// readJudgement reads and validates the judgement at path; texts is nil
// when it is valid, else the lines of what is wrong, one per problem.
func readJudgement(path string, rv core.Review, cr core.Key) (core.Judgement, []string) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return core.Judgement{}, []string{fmt.Sprintf("missing judgement for %s, run agent-code-review next", cr)}
	case err != nil:
		return core.Judgement{}, []string{"/: the judgement cannot be read: " + err.Error()}
	}
	j, err := core.DecodeJudgement(data)
	if err != nil {
		return core.Judgement{}, []string{"/: " + err.Error()}
	}
	err = core.ValidateJudgement(rv, j)
	var ve core.ValidationError
	if !errors.As(err, &ve) {
		return j, nil
	}
	texts := make([]string, 0, len(ve))
	for _, f := range ve {
		texts = append(texts, f.Pointer+": "+f.Message)
	}
	return j, texts
}

// validationFailure lists the problems, grouped by change request.
func validationFailure(problems []problem) *Failure {
	f := &Failure{Kind: KindValidation, What: "the judgements are not valid, nothing was finalized"}
	n := 0
	for _, p := range problems {
		f.Details = append(f.Details, fmt.Sprintf("%s — %s", p.cr, p.path))
		for _, t := range p.text {
			f.Details = append(f.Details, "  "+t)
			n++
		}
	}
	f.Where = plural(len(problems), "change request")
	f.Why = plural(n, "problem")
	f.Hint = "fix every problem in the judgement file of its change request, then run `agent-code-review finalize` again"
	return f
}

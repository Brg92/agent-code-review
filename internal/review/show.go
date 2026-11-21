package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// ShowDeps are what show and save need from the outside.
type ShowDeps struct {
	// Registries are the reference registries, as for review.
	Registries []refs.Registry
	// Version is the version the reports are headed with.
	Version string
	// Now is the clock; it dates the saved reports.
	Now func() time.Time
	// HomeDir finds the home directory, where reports are saved when no
	// folder is named; nil is os.UserHomeDir.
	HomeDir func() (string, error)
}

// finished is a finalized run read for showing: the findings of each
// change request and the cascade they belong to.
type finished struct {
	loads    []loaded
	docs     []core.Document
	reviewed []render.Reviewed
	res      cascade.Result
}

// readFinished reads the findings of a run that is finalized or later.
func readFinished(o *Opened, registries []refs.Registry) (*finished, error) {
	switch p := o.State.Phase; p {
	case state.Finalized, state.Planned, state.Published:
	default:
		return nil, &Failure{Kind: KindState, What: "the run has no reports yet", Where: o.Dir, Why: "its phase is " + string(p),
			Hint: "run `agent-code-review finalize` once every change request is judged"}
	}
	loads, err := loadPackages(o)
	if err != nil {
		return nil, err
	}
	res, err := cascadeOf(o, loads, registries)
	if err != nil {
		return nil, err
	}
	f := &finished{loads: loads, res: res}
	for _, l := range loads {
		path := filepath.Join(l.Dir, state.FindingsFile)
		data, err := os.ReadFile(path)
		var doc core.Document
		if err == nil {
			doc, err = core.DecodeDocument(data)
		}
		if err != nil {
			return nil, damaged("the findings of "+l.Entry.CR.String()+" cannot be read", l.Entry.CR.String()+", "+path, err.Error(), err)
		}
		f.docs = append(f.docs, doc)
		f.reviewed = append(f.reviewed, render.Reviewed{Doc: doc, Report: filepath.Join(l.Dir, state.ReportFile)})
	}
	return f, nil
}

// summary is the summary of the cascade.
func (f *finished) summary(o *Opened) (string, error) {
	text, err := render.Cascade(f.res, f.reviewed)
	if err != nil {
		return "", damaged("the cascade of the run cannot be summarised", o.Dir, err.Error(), err)
	}
	return text, nil
}

// pick finds the change request named by ref among those reviewed in the
// run: its key, or <owner>/<repo>#<number> as the summary writes it, in
// any case.
func (f *finished) pick(o *Opened, ref string) (int, error) {
	project, number, ok := strings.Cut(strings.TrimSpace(ref), "#")
	n, err := strconv.Atoi(number)
	if !ok || err != nil || n < 1 || project == "" {
		return 0, &Failure{Kind: KindUsage, What: "not a change request reference", Where: "show", Why: fmt.Sprintf("%q is neither a key nor <owner>/<repo>#<number>", oneLine(ref)),
			Hint: "give a change request of the run, as `agent-code-review show` lists them"}
	}
	want := strings.ToLower(project)
	for i, l := range f.loads {
		k := l.Entry.CR
		if n == k.Number && (want == strings.ToLower(k.Project) || want == strings.ToLower(string(k.Forge)+":"+k.Host+"/"+k.Project) || want == strings.ToLower(k.Host+"/"+k.Project)) {
			return i, nil
		}
	}
	why := "not a change request of this run"
	for _, e := range o.State.ChangeRequests {
		k := e.CR
		if e.Status == core.Skipped && n == k.Number && (strings.EqualFold(want, k.Project) || strings.EqualFold(want, string(k.Forge)+":"+k.Host+"/"+k.Project)) {
			why = "skipped: " + string(e.SkipReason) + ", so it has no report"
			break
		}
	}
	return 0, &Failure{Kind: KindUsage, What: "there is no report for " + oneLine(ref), Where: "run " + o.ID, Why: why,
		Hint: "run `agent-code-review show` for the change requests of the run"}
}

// Show returns what show prints: the summary of the cascade, or with cr
// the full report of that change request, made again from its
// findings.json. It changes nothing.
func Show(o *Opened, d ShowDeps, log *runlog.Log, cr string) (string, error) {
	log.SetCR(core.Key{})
	f, err := readFinished(o, d.Registries)
	if err != nil {
		return "", err
	}
	if cr == "" {
		text, err := f.summary(o)
		if err != nil {
			return "", err
		}
		log.Event("show summary", "ok")
		return text, nil
	}
	i, err := f.pick(o, cr)
	if err != nil {
		return "", err
	}
	log.SetCR(f.loads[i].Entry.CR)
	log.Event("show report", "ok")
	return render.Report(d.Version, f.docs[i]), nil
}

// Save writes a report to a new file in dir, or in the reports folder of
// the home directory when dir is empty, and returns its path. The report
// is the root's, with the summary of the cascade, or that of the change
// request cr. The folder must be outside any git work tree, have no ".."
// and be writable, or it is a KindPath failure.
func Save(o *Opened, d ShowDeps, log *runlog.Log, dir, cr string) (string, error) {
	log.SetCR(core.Key{})
	f, err := readFinished(o, d.Registries)
	if err != nil {
		return "", err
	}
	i := len(f.loads) - 1 // the root is reviewed last
	if cr != "" {
		if i, err = f.pick(o, cr); err != nil {
			return "", err
		}
	}
	dir, err = reportsDir(d, dir)
	if err != nil {
		return "", err
	}
	l, doc := f.loads[i], f.docs[i]
	log.SetCR(l.Entry.CR)
	content := render.Report(d.Version, doc)
	if l.Entry.CR.Same(o.State.Root) {
		summary, err := f.summary(o)
		if err != nil {
			return "", err
		}
		content = state.RootReport(content, summary)
	}
	name, err := state.ReportName(l.Entry.CR, doc.HeadSHA, d.Now())
	if err != nil {
		return "", damaged("the report of "+l.Entry.CR.String()+" cannot be named", l.Entry.CR.String(), err.Error(), err)
	}
	path, err := state.SaveReport(dir, name, content)
	if err != nil {
		var inside *state.InsideGitError
		f := &Failure{Kind: KindPath, What: "could not save the report", Where: dir, Why: err.Error(), Err: err,
			Hint: "name a writable folder outside any git work tree"}
		if errors.As(err, &inside) {
			f.Why = "the folder is inside the git work tree " + inside.WorkTree
		}
		return "", f
	}
	log.Event("save report", "ok")
	return path, nil
}

// reportsDir is the absolute folder reports are saved in.
func reportsDir(d ShowDeps, dir string) (string, error) {
	bad := func(why string) error {
		return &Failure{Kind: KindPath, What: "could not choose the folder for the report", Where: oneLine(dir), Why: why,
			Hint: "name a folder without \"..\" that is outside any git work tree"}
	}
	if dir == "" {
		home := d.HomeDir
		if home == nil {
			home = os.UserHomeDir
		}
		def, err := state.DefaultReportsDir(home)
		if err != nil {
			return "", &Failure{Kind: KindPath, What: "could not find the folder for the report", Where: "home directory", Why: err.Error(), Err: err,
				Hint: "name the folder: agent-code-review show --save <folder>"}
		}
		return def, nil
	}
	for _, seg := range strings.FieldsFunc(dir, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "", bad("it contains \"..\"")
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", bad(err.Error())
	}
	return abs, nil
}

package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

func finalizeDeps(t testing.TB) FinalizeDeps {
	t.Helper()
	return FinalizeDeps{
		Packs:      loadPacks(t),
		Registries: []refs.Registry{github.Registry{}, gitlab.Registry{}},
		Version:    "1.2.3",
		Now:        func() time.Time { return now },
	}
}

// validJudgementOf is a valid judgement of a work package that says
// nothing: it dismisses the signals that need a decision.
func validJudgementOf(wp core.WorkPackage) core.Judgement {
	j := core.Judgement{Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA, Findings: []core.JudgedFinding{}, PreExisting: []core.JudgedFinding{}}
	for _, s := range wp.Signals {
		if s.NeedsJudgement {
			j.Signals = append(j.Signals, core.SignalDecision{ID: s.ID, Decision: core.Dismiss, Reason: "synthetic: not an issue here"})
		}
	}
	return j
}

func writeJudgementOf(t testing.TB, path string, j core.Judgement) {
	t.Helper()
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	writeJudgement(t, path, string(data))
}

// collectedRun is a hand-made collected run with a valid judgement for
// each of its change requests.
func collectedRun(t testing.TB) (madeRun, string) {
	t.Helper()
	work := t.TempDir()
	m := makeRun(t, work, now, state.Collected)
	writeJudgementOf(t, m.AppJudgement, validJudgementOf(m.AppWP))
	writeJudgementOf(t, m.LibJudgement, validJudgementOf(m.LibWP))
	return m, work
}

func finalize(t testing.TB, work, runID string) (*Finalized, *Opened, *runlog.Log, error) {
	t.Helper()
	o, log, _ := open(t, work, runID)
	log.SetCommand("finalize")
	f, err := Finalize(o, finalizeDeps(t), log)
	return f, o, log, err
}

func asFailure(t testing.TB, err error, kind Kind) *Failure {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) || f.Kind != kind {
		t.Fatalf("error %v, want a failure of kind %d", err, kind)
	}
	return f
}

func loadState(t testing.TB, m madeRun) *state.State {
	t.Helper()
	st, err := state.Load(m.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestFinalizeWritesTheFindingsAndTheReports(t *testing.T) {
	m, work := collectedRun(t)
	f, _, _, err := finalize(t, work, "")
	if err != nil {
		t.Fatal(err)
	}
	st := loadState(t, m)
	if st.Phase != state.Finalized {
		t.Errorf("phase %s", st.Phase)
	}
	for _, e := range st.ChangeRequests {
		if e.Status != core.Finalized {
			t.Errorf("%s is %s", e.CR, e.Status)
		}
	}
	for _, c := range []struct {
		judgement string
		wp        core.WorkPackage
	}{{m.AppJudgement, m.AppWP}, {m.LibJudgement, m.LibWP}} {
		dir := filepath.Dir(c.judgement)
		data, err := os.ReadFile(filepath.Join(dir, state.FindingsFile))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := core.DecodeDocument(data)
		if err != nil {
			t.Fatal(err)
		}
		if doc.HeadSHA != c.wp.HeadSHA || doc.Title != c.wp.Title || doc.CI.State != c.wp.CI.State || doc.Verdict != core.OK {
			t.Errorf("findings of %s: head %s, title %q, ci %s, verdict %s", c.wp.CR, doc.HeadSHA, doc.Title, doc.CI.State, doc.Verdict)
		}
		report, err := os.ReadFile(filepath.Join(dir, state.ReportFile))
		if err != nil {
			t.Fatal(err)
		}
		// The report is made from findings.json alone.
		if want := render.Report("1.2.3", doc); string(report) != want {
			t.Errorf("report of %s:\n%s\nwant:\n%s", c.wp.CR, report, want)
		}
		if !strings.Contains(f.Text, filepath.Join(dir, state.ReportFile)) {
			t.Errorf("the final message does not name the report of %s:\n%s", c.wp.CR, f.Text)
		}
	}
	for _, want := range []string{"Review completed. Nothing was written to GitHub.\n", "\nCascade — ", "\nOverall: OK\n", "\n  agent-code-review show <cr> "} {
		if !strings.Contains(f.Text, want) {
			t.Errorf("the final message has no %q:\n%s", want, f.Text)
		}
	}
	if len(f.Reports) != 2 {
		t.Errorf("reports %v", f.Reports)
	}
	if !strings.Contains(readLog(t, m), "\tfinalize\tgithub:github.com/acme/app#7\tfinalize\t-\t-\t-\tok\n") {
		t.Errorf("run.log:\n%s", readLog(t, m))
	}
}

func readLog(t testing.TB, m madeRun) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.Dir, runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestFinalizeIsAllOrNothing: every invalid judgement is listed, grouped
// by change request with the pointer of each field, and nothing is written.
func TestFinalizeIsAllOrNothing(t *testing.T) {
	m, work := collectedRun(t)
	bad := validJudgementOf(m.AppWP)
	bad.HeadSHA = strings.Repeat("0", 40)
	bad.Findings = []core.JudgedFinding{{Rule: "no-such-rule", Severity: core.Major, Path: "app/Nope.kt", Side: core.Right, Line: 3, Title: "t", Why: "w", Fix: "f"}}
	writeJudgementOf(t, m.AppJudgement, bad)
	worse := validJudgementOf(m.LibWP)
	worse.Signals = append(worse.Signals, core.SignalDecision{ID: "s99", Decision: core.Dismiss, Reason: "r"})
	worse.Notes = []string{" "}
	writeJudgementOf(t, m.LibJudgement, worse)

	_, _, _, err := finalize(t, work, "")
	f := asFailure(t, err, KindValidation)
	got := strings.Join(f.Details, "\n")
	for _, want := range []string{
		"github:github.com/acme/app#7", "/headSha:", "/findings/0/rule:", "/findings/0/path:",
		"github:github.com/acme/lib#3", "/signals/", "no signal \"s99\"", "/notes/0:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the errors do not hold %q:\n%s", want, got)
		}
	}
	report := f.Report()
	if !strings.Contains(report, "\n  problems:\n") || !strings.Contains(report, "    github:github.com/acme/lib#3") {
		t.Errorf("report:\n%s", report)
	}
	if st := loadState(t, m); st.Phase != state.Collected {
		t.Errorf("phase %s after a failure", st.Phase)
	}
	for _, j := range []string{m.AppJudgement, m.LibJudgement} {
		for _, name := range []string{state.FindingsFile, state.ReportFile} {
			if _, err := os.Stat(filepath.Join(filepath.Dir(j), name)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s was written: %v", name, err)
			}
		}
	}
}

func TestFinalizeListsAMissingJudgementWithTheOtherErrors(t *testing.T) {
	m, work := collectedRun(t)
	if err := os.Remove(m.LibJudgement); err != nil {
		t.Fatal(err)
	}
	bad := validJudgementOf(m.AppWP)
	bad.Schema = 2
	writeJudgementOf(t, m.AppJudgement, bad)
	_, _, _, err := finalize(t, work, "")
	f := asFailure(t, err, KindValidation)
	got := strings.Join(f.Details, "\n")
	if !strings.Contains(got, "missing judgement for github:github.com/acme/lib#3, run agent-code-review next") || !strings.Contains(got, "/schema:") {
		t.Errorf("errors:\n%s", got)
	}
}

func TestFinalizeListsAJudgementThatDoesNotParse(t *testing.T) {
	m, work := collectedRun(t)
	writeJudgement(t, m.AppJudgement, `{"schema":1,"unknown":true}`)
	writeJudgement(t, m.LibJudgement, `not json`)
	_, _, _, err := finalize(t, work, "")
	f := asFailure(t, err, KindValidation)
	got := strings.Join(f.Details, "\n")
	if strings.Count(got, "/: decode judgement:") != 2 {
		t.Errorf("errors:\n%s", got)
	}
}

func TestFinalizeTwiceAndInTheWrongPhase(t *testing.T) {
	m, work := collectedRun(t)
	if _, _, _, err := finalize(t, work, ""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(m.Dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = finalize(t, work, "")
	f := asFailure(t, err, KindState)
	if !strings.Contains(f.Why, "finalized") || !strings.Contains(f.Hint, "review") {
		t.Errorf("failure %+v", f)
	}
	after, _ := os.ReadFile(filepath.Join(m.Dir, state.FileName))
	if string(after) != string(before) {
		t.Error("the state of a finalized run changed")
	}

	work2 := t.TempDir()
	makeRun(t, work2, now, state.Resolved)
	_, _, _, err = finalize(t, work2, "")
	if f := asFailure(t, err, KindState); !strings.Contains(f.Why, "resolved") {
		t.Errorf("failure %+v", f)
	}
}

func TestFinalizeDamagedRun(t *testing.T) {
	t.Run("work package", func(t *testing.T) {
		m, work := collectedRun(t)
		if err := os.WriteFile(filepath.Join(filepath.Dir(m.AppJudgement), state.WorkPackageFile), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := finalize(t, work, "")
		asFailure(t, err, KindState)
	})
	t.Run("judgement path", func(t *testing.T) {
		m, work := collectedRun(t)
		wp := m.AppWP
		wp.JudgementPath = "../escape/judgement.json"
		data, err := core.EncodeWorkPackage(wp)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(m.AppJudgement), state.WorkPackageFile), data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, _, err = finalize(t, work, "")
		asFailure(t, err, KindState)
	})
	t.Run("a rule no loaded pack defines", func(t *testing.T) {
		_, work := collectedRun(t)
		o, log, _ := open(t, work, "")
		d := finalizeDeps(t)
		d.Packs = &packs.Set{}
		_, err := Finalize(o, d, log)
		if f := asFailure(t, err, KindUnexpected); !strings.Contains(f.Why, "do not define the rules") {
			t.Errorf("failure %+v", f)
		}
	})
	t.Run("no registry for the platform", func(t *testing.T) {
		_, work := collectedRun(t)
		o, log, _ := open(t, work, "")
		d := finalizeDeps(t)
		d.Registries = nil
		_, err := Finalize(o, d, log)
		asFailure(t, err, KindUnexpected)
	})
}

// TestFinalizeRefusesAFindingOnALineTheAgentWasNotShown: a validation
// error like any other, with its pointer and the path and line it names.
func TestFinalizeRefusesAFindingOnALineTheAgentWasNotShown(t *testing.T) {
	m, work := collectedRun(t)
	const path = "src/main/java/com/example/feature/ui/LongScreen.kt"
	j := validJudgementOf(m.LibWP)
	j.Findings = []core.JudgedFinding{
		{Rule: "logic-defect", Severity: core.Major, Path: path, Side: core.Right, Line: 5, Title: "t", Why: "w", Fix: "f", References: []core.Reference{}},
		{Rule: "logic-defect", Severity: core.Major, Path: path, Side: core.Right, StartLine: 3, Line: 380, Title: "t", Why: "w", Fix: "f", References: []core.Reference{}},
		{Rule: "logic-defect", Severity: core.Major, Path: path, Side: core.Right, Line: 381, Title: "t", Why: "w", Fix: "f", References: []core.Reference{}},
	}
	writeJudgementOf(t, m.LibJudgement, j)
	_, _, _, err := finalize(t, work, "")
	f := asFailure(t, err, KindValidation)
	got := strings.Join(f.Details, "\n")
	for _, want := range []string{"/findings/0/line: line 5 of " + path + " was not shown to the agent", "/findings/1/startLine: line 3 of " + path + " was not shown to the agent"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/findings/2/") {
		t.Errorf("a line of a region was refused:\n%s", got)
	}
}

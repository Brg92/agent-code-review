package review

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

func showDeps(home string) ShowDeps {
	return ShowDeps{
		Registries: []refs.Registry{github.Registry{}, gitlab.Registry{}},
		Version:    "1.2.3",
		Now:        func() time.Time { return now },
		HomeDir:    func() (string, error) { return home, nil },
	}
}

// finalizedRun is a hand-made run, finalized.
func finalizedRun(t testing.TB) (madeRun, string) {
	t.Helper()
	m, work := collectedRun(t)
	if _, _, _, err := finalize(t, work, ""); err != nil {
		t.Fatal(err)
	}
	return m, work
}

func openForShow(t testing.TB, work, runID string) (*Opened, *runlog.Log) {
	t.Helper()
	o, log, _ := open(t, work, runID)
	log.SetCommand("show")
	return o, log
}

func reportOf(t testing.TB, judgement string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(judgement), state.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestShowTheSummaryAndAReport(t *testing.T) {
	m, work := finalizedRun(t)
	o, log := openForShow(t, work, "")
	d := showDeps(t.TempDir())

	summary, err := Show(o, d, log, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(summary, "Cascade — acme/app#7 · 2 reviewed · 0 skipped · 0 not followed\n") || !strings.HasSuffix(summary, "Overall: OK\n") ||
		strings.Contains(summary, "Review completed") || strings.Contains(summary, "Next:") {
		t.Errorf("summary:\n%s", summary)
	}
	for _, ref := range []string{"acme/lib#3", "github:github.com/acme/lib#3", "ACME/LIB#3"} {
		got, err := Show(o, d, log, ref)
		if err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
		if got != reportOf(t, m.LibJudgement) {
			t.Errorf("%s: report\n%s\nwant\n%s", ref, got, reportOf(t, m.LibJudgement))
		}
	}
	root, err := Show(o, d, log, "acme/app#7")
	if err != nil || root != reportOf(t, m.AppJudgement) {
		t.Errorf("root report %q, %v", root, err)
	}
	if !strings.Contains(readLog(t, m), "\tshow\tgithub:github.com/acme/lib#3\tshow report\t-\t-\t-\tok\n") ||
		!strings.Contains(readLog(t, m), "\tshow\t-\tshow summary\t-\t-\t-\tok\n") {
		t.Errorf("run.log:\n%s", readLog(t, m))
	}
}

// TestShowMakesTheReportFromTheFindings: the report is regenerated from
// findings.json, not read from report.txt, and showing never changes the
// run.
func TestShowMakesTheReportFromTheFindings(t *testing.T) {
	m, work := finalizedRun(t)
	stateBefore, err := os.ReadFile(filepath.Join(m.Dir, state.FileName))
	if err != nil {
		t.Fatal(err)
	}
	findings := filepath.Join(filepath.Dir(m.LibJudgement), state.FindingsFile)
	data, err := os.ReadFile(findings)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"title": "`+m.LibWP.Title+`"`, `"title": "Edited in findings.json"`, 1)
	if edited == string(data) {
		t.Fatal("the title is not in findings.json")
	}
	if err := os.WriteFile(findings, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	o, log := openForShow(t, work, "")
	got, err := Show(o, showDeps(t.TempDir()), log, "acme/lib#3")
	if err != nil || !strings.Contains(got, `"Edited in findings.json"`) {
		t.Errorf("report %q, %v", got, err)
	}
	if stateAfter, _ := os.ReadFile(filepath.Join(m.Dir, state.FileName)); string(stateAfter) != string(stateBefore) {
		t.Error("show changed the state")
	}
}

func TestShowRefusesWhatItCannotShow(t *testing.T) {
	m, work := finalizedRun(t)
	o, log := openForShow(t, work, "")
	d := showDeps(t.TempDir())

	// Skipped entries and strangers are not in the run's reports.
	st := *o.State
	root := st.ChangeRequests[0].CR
	other := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/gone", Number: 9}
	st.ChangeRequests = append(append([]core.Entry(nil), st.ChangeRequests...),
		core.Entry{CR: other, Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: core.NotFound})
	o.State = &st
	for ref, why := range map[string]string{"acme/nope#1": "not a change request of this run", "acme/gone#9": "skipped: not-found"} {
		_, err := Show(o, d, log, ref)
		if f := asFailure(t, err, KindUsage); !strings.Contains(f.Why, why) {
			t.Errorf("%s: %+v", ref, f)
		}
	}
	for _, ref := range []string{"not a reference", "acme/lib#", "#3", "acme/lib#x"} {
		_, err := Show(o, d, log, ref)
		asFailure(t, err, KindUsage)
	}

	// A run that is not finalized has nothing to show.
	work2 := t.TempDir()
	makeRun(t, work2, now, state.Collected)
	o2, log2 := openForShow(t, work2, "")
	_, err := Show(o2, d, log2, "")
	if f := asFailure(t, err, KindState); !strings.Contains(f.Why, "collected") {
		t.Errorf("failure %+v", f)
	}
	_, err = Save(o2, d, log2, t.TempDir(), "")
	asFailure(t, err, KindState)

	// Findings that are gone or damaged.
	if err := os.Remove(filepath.Join(filepath.Dir(m.LibJudgement), state.FindingsFile)); err != nil {
		t.Fatal(err)
	}
	o3, log3 := openForShow(t, work, "")
	_, err = Show(o3, d, log3, "")
	asFailure(t, err, KindState)
	if err := os.WriteFile(filepath.Join(filepath.Dir(m.LibJudgement), state.FindingsFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Show(o3, d, log3, "acme/lib#3")
	asFailure(t, err, KindState)
}

func TestShowAPublishedRun(t *testing.T) {
	work := t.TempDir()
	m := makeRun(t, work, now, state.Published)
	writeJudgementOf(t, m.AppJudgement, validJudgementOf(m.AppWP))
	writeJudgementOf(t, m.LibJudgement, validJudgementOf(m.LibWP))
	// Make the findings the way finalize does, on a run that moved on.
	collected := t.TempDir()
	cm := makeRun(t, collected, now, state.Collected)
	writeJudgementOf(t, cm.AppJudgement, validJudgementOf(cm.AppWP))
	writeJudgementOf(t, cm.LibJudgement, validJudgementOf(cm.LibWP))
	if _, _, _, err := finalize(t, collected, ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{state.FindingsFile} {
		for _, j := range [][2]string{{cm.AppJudgement, m.AppJudgement}, {cm.LibJudgement, m.LibJudgement}} {
			data, err := os.ReadFile(filepath.Join(filepath.Dir(j[0]), name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(j[1]), name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	o, log := openForShow(t, work, m.ID)
	if got, err := Show(o, showDeps(t.TempDir()), log, ""); err != nil || !strings.Contains(got, "Overall: OK") {
		t.Errorf("show of a published run: %q, %v", got, err)
	}
}

func TestSaveTheReports(t *testing.T) {
	m, work := finalizedRun(t)
	o, log := openForShow(t, work, "")
	home := t.TempDir()
	d := showDeps(home)
	dir := filepath.Join(t.TempDir(), "saved")

	// The root's report holds the summary of the cascade.
	path, err := Save(o, d, log, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "app-7-"+m.AppWP.HeadSHA[:8]+"_2026-11-20_1200.txt"); path != want {
		t.Errorf("path %s, want %s", path, want)
	}
	summary, err := Show(o, d, log, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != state.RootReport(reportOf(t, m.AppJudgement), summary) {
		t.Errorf("content %q, %v", got, err)
	}
	// A second save does not overwrite.
	again, err := Save(o, d, log, dir, "acme/app#7")
	if err != nil || again != strings.TrimSuffix(path, ".txt")+"_2.txt" {
		t.Errorf("second save %s, %v", again, err)
	}
	// Another change request: its report alone.
	lib, err := Save(o, d, log, dir, "acme/lib#3")
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(lib); string(data) != reportOf(t, m.LibJudgement) || filepath.Base(lib) != "lib-3-"+m.LibWP.HeadSHA[:8]+"_2026-11-20_1200.txt" {
		t.Errorf("lib report %s:\n%s", lib, data)
	}
	// The default folder is in the home directory.
	def, err := Save(o, d, log, "", "acme/lib#3")
	if err != nil || filepath.Dir(def) != filepath.Join(home, "agent-code-review", "reports") {
		t.Errorf("default folder: %s, %v", def, err)
	}
	if !strings.Contains(readLog(t, m), "\tsave report\t-\t-\t-\tok\n") {
		t.Errorf("run.log:\n%s", readLog(t, m))
	}
}

func TestSaveRefusesAPathItMayNotUse(t *testing.T) {
	_, work := finalizedRun(t)
	o, log := openForShow(t, work, "")
	d := showDeps(t.TempDir())

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	plainFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(plainFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{
		"inside a git work tree": filepath.Join(repo, "reports"),
		"with ..":                t.TempDir() + string(filepath.Separator) + "a" + string(filepath.Separator) + ".." + string(filepath.Separator) + "b",
		"a file":                 filepath.Join(plainFile, "sub"),
		"relative with ..":       filepath.Join("..", "reports"),
	} {
		_, err := Save(o, d, log, dir, "")
		f := asFailure(t, err, KindPath)
		if f.Where == "" || f.Why == "" {
			t.Errorf("%s: %+v", name, f)
		}
		var inside *state.InsideGitError
		if name == "inside a git work tree" && !errors.As(f, &inside) {
			t.Errorf("%s: %v", name, f.Err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(repo)); len(entries) != 1 {
		t.Errorf("something was written in the work tree: %v", entries)
	}
	if _, err := Save(o, d, log, t.TempDir(), "acme/nope#1"); err == nil {
		t.Error("saved a report of a change request that is not in the run")
	}
	_, err := Save(o, ShowDeps{Registries: d.Registries, Version: "1", Now: d.Now, HomeDir: func() (string, error) { return "", errors.New("no home") }}, log, "", "")
	asFailure(t, err, KindPath)
}

// runlogFor is a run log that writes nowhere.
func runlogFor(t testing.TB) *runlog.Log {
	t.Helper()
	l := runlog.New(func() time.Time { return now }, nil)
	l.SetCommand("show")
	return l
}

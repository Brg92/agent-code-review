package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/reviewreplay"
)

const (
	s1Root  = "acr-plugin-sandbox/sample-app#2"
	s1RunID = "20261120T120000Z-52fdfc07"
)

// reviewedS1 is a collected run of scenario S1, and where the judgement of
// its one change request goes.
func reviewedS1(t *testing.T) (e env, work, runDir, judgement string) {
	t.Helper()
	r := reviewreplay.New(t, "S1")
	reviewreplay.AddBuildFiles(t, r, "S1", "sample-app", 2)
	e, work = reviewEnv(t, r)
	if res := runWith(t, e, "review", s1Root); res.code != exitOK {
		t.Fatalf("review: code %d\n%s", res.code, res.stderr)
	}
	runDir = filepath.Join(work, "runs", s1RunID)
	judgement = filepath.Join(runDir, "acr-plugin-sandbox_sample-app-2-76ae5f13", state.JudgementFile)
	return e, work, runDir, judgement
}

func copyFile(t *testing.T, from []string, to string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, from...)...))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeThenShowOnS1(t *testing.T) {
	e, work, runDir, judgement := reviewedS1(t)
	copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)

	r := runWith(t, e, "finalize")
	if r.code != exitOK || r.stderr != "" {
		t.Fatalf("finalize: code %d\nstderr: %s\nstdout:\n%s", r.code, r.stderr, r.stdout)
	}
	report := filepath.Join(filepath.Dir(judgement), state.ReportFile)
	for _, want := range []string{"Review completed. Nothing was written to GitHub.\n", report + "\n", "BLOCKED", "Overall: BLOCKED\n",
		"agent-code-review show <cr>", "agent-code-review show --save", "agent-code-review publish --plan"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("finalize prints no %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "Verdict:") {
		t.Errorf("finalize prints a report:\n%s", r.stdout)
	}
	noCanary(t, "stdout", r.stdout)

	// A run that is finalized cannot be finalized again.
	if again := runWith(t, e, "finalize"); again.code != exitState || again.stdout != "" || !strings.Contains(again.stderr, "already finalized") {
		t.Errorf("second finalize: code %d\n%s", again.code, again.stderr)
	}

	summary := runWith(t, e, "show")
	if summary.code != exitOK || summary.stderr != "" || !strings.HasPrefix(summary.stdout, "Cascade — acr-plugin-sandbox/sample-app#2 · 1 reviewed") || !strings.HasSuffix(summary.stdout, "Overall: BLOCKED\n") {
		t.Errorf("show: code %d\n%s\n%s", summary.code, summary.stderr, summary.stdout)
	}
	one := runWith(t, e, "show", s1Root)
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if one.code != exitOK || one.stdout != string(data) || !strings.Contains(one.stdout, "Verdict: BLOCKED") {
		t.Errorf("show %s: code %d\n%s", s1Root, one.code, one.stdout)
	}
	// By the id of the run as well, and by the key.
	if named := runWith(t, e, "show", "--run", s1RunID, "github:github.com/"+s1Root); named.code != exitOK || named.stdout != one.stdout {
		t.Errorf("show --run: code %d\n%s", named.code, named.stderr)
	}
	if missing := runWith(t, e, "show", "acr-plugin-sandbox/sample-app#99"); missing.code != exitUsage || missing.stdout != "" || !strings.Contains(missing.stderr, "not a change request of this run") {
		t.Errorf("show of a stranger: code %d\n%s", missing.code, missing.stderr)
	}

	// show never changes the phase, and everything is in run.log.
	st, err := state.Load(runDir)
	if err != nil || st.Phase != state.Finalized {
		t.Errorf("phase %v, %v", st, err)
	}
	log, err := os.ReadFile(filepath.Join(runDir, runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\tfinalize\t", "\tshow\t"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("run.log has no %q line:\n%s", want, log)
		}
	}
	noCanary(t, "run.log", string(log))
	_ = work
}

func TestShowSave(t *testing.T) {
	e, _, _, judgement := reviewedS1(t)
	copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)
	if r := runWith(t, e, "finalize"); r.code != exitOK {
		t.Fatalf("finalize: %d\n%s", r.code, r.stderr)
	}
	dir := filepath.Join(t.TempDir(), "reports")
	r := runWith(t, e, "show", "--save", dir)
	if r.code != exitOK || r.stderr != "" {
		t.Fatalf("show --save: code %d\n%s", r.code, r.stderr)
	}
	saved := strings.TrimSuffix(r.stdout, "\n")
	if filepath.Dir(saved) != dir || !strings.HasPrefix(filepath.Base(saved), "sample-app-2-76ae5f13_2026-11-20_1200") || strings.Contains(saved, "\n") {
		t.Errorf("show --save printed %q", r.stdout)
	}
	data, err := os.ReadFile(saved)
	if err != nil || !strings.Contains(string(data), "Verdict: BLOCKED") || !strings.Contains(string(data), "Cascade — ") {
		t.Errorf("saved report %q, %v", data, err)
	}
	// Named with the change request, in either order, the report alone;
	// the second one the same minute gets a suffix.
	for i, args := range [][]string{{"--save", s1Root, dir}, {"--save", dir, s1Root}} {
		again := runWith(t, e, append([]string{"show"}, args...)...)
		want := strings.TrimSuffix(saved, ".txt") + "_" + string(rune('2'+i)) + ".txt"
		if again.code != exitOK || strings.TrimSuffix(again.stdout, "\n") != want {
			t.Errorf("show %v: code %d, %q, want %s\n%s", args, again.code, again.stdout, want, again.stderr)
		}
	}
	// The default folder is in the home directory.
	home := t.TempDir()
	e.userHomeDir = func() (string, error) { return home, nil }
	def := runWith(t, e, "show", "--save")
	if def.code != exitOK || filepath.Dir(strings.TrimSuffix(def.stdout, "\n")) != filepath.Join(home, "agent-code-review", "reports") {
		t.Errorf("default folder: code %d\n%s\n%s", def.code, def.stdout, def.stderr)
	}
}

func TestShowSaveRefusals(t *testing.T) {
	e, _, _, judgement := reviewedS1(t)
	copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)
	if r := runWith(t, e, "finalize"); r.code != exitOK {
		t.Fatalf("finalize: %d\n%s", r.code, r.stderr)
	}
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{
		"git work tree": filepath.Join(repo, "docs"),
		"with ..":       t.TempDir() + string(filepath.Separator) + ".." + string(filepath.Separator) + "x",
	} {
		r := runWith(t, e, "show", "--save", dir)
		if r.code != exitPath || r.stdout != "" || !strings.Contains(r.stderr, "agent-code-review: could not") {
			t.Errorf("%s: code %d\nstdout %q\n%s", name, r.code, r.stdout, r.stderr)
		}
		failShape(t, r.stderr, true)
	}
	if entries, _ := os.ReadDir(repo); len(entries) != 1 {
		t.Errorf("a file was written in the work tree: %v", entries)
	}
}

func TestFinalizeListsEveryError(t *testing.T) {
	e, _, runDir, judgement := reviewedS1(t)
	// A judgement for another head, with a finding on a file that is not
	// changed, and signals left undecided.
	bad := `{"schema":1,"cr":"github:github.com/acr-plugin-sandbox/sample-app#2","headSha":"0000000000000000000000000000000000000000",
"findings":[{"rule":"logic-defect","severity":"MAJOR","path":"nope.kt","side":"RIGHT","line":1,"title":"t","why":"w","fix":"f","references":[]}],"preExisting":[]}`
	if err := os.WriteFile(judgement, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	r := runWith(t, e, "finalize")
	if r.code != exitValidation || r.stdout != "" {
		t.Fatalf("code %d\nstdout %q\n%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{"/headSha:", "/findings/0/path:", "/signals:", "github:github.com/acr-plugin-sandbox/sample-app#2 — " + judgement, "\n  what to do: fix every problem"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr has no %q:\n%s", want, r.stderr)
		}
	}
	st, err := state.Load(runDir)
	if err != nil || st.Phase != state.Collected {
		t.Errorf("phase after a failure: %v, %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(judgement), state.FindingsFile)); err == nil {
		t.Error("findings.json was written")
	}
}

func TestFinalizeAndShowBeforeTheirTime(t *testing.T) {
	e, _, _, _ := reviewedS1(t)
	// Not judged yet: the judgement is missing, as a validation error.
	r := runWith(t, e, "finalize")
	if r.code != exitValidation || !strings.Contains(r.stderr, "missing judgement for github:github.com/acr-plugin-sandbox/sample-app#2, run agent-code-review next") {
		t.Errorf("finalize: code %d\n%s", r.code, r.stderr)
	}
	// Not finalized: nothing to show.
	for _, args := range [][]string{{"show"}, {"show", s1Root}, {"show", "--save", t.TempDir()}} {
		if r := runWith(t, e, args...); r.code != exitState || r.stdout != "" || !strings.Contains(r.stderr, "its phase is collected") {
			t.Errorf("%v: code %d\n%s", args, r.code, r.stderr)
		}
	}
}

func TestFinalizeAndShowWithoutARun(t *testing.T) {
	e, _ := reviewEnv(t, reviewreplay.New(t, "S1"))
	for _, args := range [][]string{{"finalize"}, {"show"}, {"finalize", "--run", s1RunID}, {"show", "--run", s1RunID}} {
		if r := runWith(t, e, args...); r.code != exitState || r.stdout != "" {
			t.Errorf("%v: code %d\n%s", args, r.code, r.stderr)
		}
	}
}

func TestFinalizeAndShowUsage(t *testing.T) {
	e, _ := reviewEnv(t, reviewreplay.New(t, "S1"))
	for _, tt := range []struct {
		args  []string
		usage string
	}{
		{[]string{"finalize", "extra"}, finalizeUsage},
		{[]string{"finalize", "--frobnicate"}, finalizeUsage},
		{[]string{"finalize", "--part", "1"}, finalizeUsage},
		{[]string{"finalize", "--save"}, finalizeUsage},
		{[]string{"finalize", "--run"}, finalizeUsage},
		{[]string{"finalize", "--run", "x"}, ""},
		{[]string{"show", "a#1", "b#2"}, showUsage},
		{[]string{"show", "/some/dir"}, showUsage},
		{[]string{"show", "--save", "a", "b", "c"}, showUsage},
		{[]string{"show", "--save", "a#1", "b#2"}, showUsage},
		{[]string{"show", "--save", "/a", "/b"}, showUsage},
		{[]string{"show", "--part", "1"}, showUsage},
		{[]string{"show", "--frobnicate"}, showUsage},
	} {
		r := runWith(t, e, tt.args...)
		if r.code != exitUsage || r.stdout != "" || (tt.usage != "" && !strings.HasSuffix(r.stderr, tt.usage)) {
			t.Errorf("%v: code %d\n%s", tt.args, r.code, r.stderr)
		}
	}
}

func TestVerboseOnFinalizeAndShow(t *testing.T) {
	e, work, runDir, judgement := reviewedS1(t)
	copyFile(t, []string{"testdata", "scenarios", "S1", "judgements", "sample-app-2.json"}, judgement)
	f := runWith(t, e, "finalize", "--verbose")
	if f.code != exitOK || !strings.Contains(f.stderr, "\tfinalize\tgithub:github.com/"+s1Root+"\tfinalize\t") {
		t.Fatalf("finalize --verbose: code %d\n%s", f.code, f.stderr)
	}
	s := runWith(t, e, "show", "--verbose", s1Root)
	if s.code != exitOK || !strings.Contains(s.stderr, "\tshow\tgithub:github.com/"+s1Root+"\tshow report\t") {
		t.Fatalf("show --verbose: code %d\n%s", s.code, s.stderr)
	}
	log, err := os.ReadFile(filepath.Join(runDir, runlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(log), f.stderr+s.stderr) {
		t.Errorf("--verbose differs from the end of run.log:\n%s", log)
	}
	// The token reached the platform and nothing else: not what the
	// commands print, not a file they write.
	for label, text := range map[string]string{"finalize stdout": f.stdout, "finalize stderr": f.stderr, "show stdout": s.stdout, "show stderr": s.stderr} {
		noCanary(t, label, text)
	}
	err = filepath.WalkDir(work, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err == nil && strings.Contains(string(data), reviewreplay.Canary) {
			t.Errorf("%s holds the token", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

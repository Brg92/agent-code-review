package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/state"
)

// The binary has no way to point at another server: the only GitHub it reads
// is the real one, so the tests of its commands that read a platform stop
// before the first request (a reference that is refused, a token that is
// missing). The commands are tested against recordings in process, in
// internal/cli and internal/review, with a replay server.

// reviewSetup builds the binary and gives it an empty work directory, the
// packs of the repository and a git that has no token; authSetup also moves
// the home and the user configuration out of the way.
func reviewSetup(t *testing.T) (bin, work string, env []string) {
	t.Helper()
	bin, _, _ = authSetup(t)
	work = t.TempDir()
	packs, err := filepath.Abs(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACR_WORKDIR", work)
	t.Setenv("ACR_PACKS_DIR", packs)
	return bin, work, os.Environ()
}

func TestReviewBinaryRefusesWhatItCannotReview(t *testing.T) {
	bin, work, _ := reviewSetup(t)
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no pull request", []string{"review"}, 2, "missing the pull request"},
		{"not a pull request", []string{"review", "banana"}, 2, "is not a pull request"},
		{"unknown flag", []string{"review", "--frobnicate", "o/r#1"}, 2, "unknown flag"},
		{"another host", []string{"review", "https://ghe.example.com/o/r/pull/1"}, 2, "not supported"},
		{"another platform", []string{"review", "https://gitlab.com/group/project/-/merge_requests/3"}, 2, "not supported"},
		{"no token", []string{"review", "o/r#1"}, 6, "agent-code-review auth login"},
		{"no run", []string{"next"}, 8, "agent-code-review review"},
		{"a run id that is not one", []string{"next", "--run", "../x"}, 2, "run id"},
		{"a run that does not exist", []string{"next", "--run", "20261120T120000Z-deadbeef"}, 8, "no run 20261120T120000Z-deadbeef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runIn(t, bin, t.TempDir(), "", tt.args...)
			if code != tt.code || stdout != "" || !strings.Contains(stderr, tt.stderr) {
				t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
	// A review that stops at the token leaves its run log for the user.
	_, stderr, _ := runIn(t, bin, t.TempDir(), "", "review", "o/r#1")
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	log := strings.TrimPrefix(lines[len(lines)-1], "  log: ")
	if !strings.HasPrefix(lines[len(lines)-1], "  log: ") || !strings.HasPrefix(log, work) {
		t.Fatalf("stderr: %q", stderr)
	}
	data, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), "\treview\t") {
		t.Errorf("run.log: %q, %v", data, err)
	}
}

// writeCollectedRun writes a run that review would have left, with one
// change request whose work package is an example of the contract.
func writeCollectedRun(t *testing.T, work string) (judgement string) {
	t.Helper()
	cr, err := core.ParseKey("github:github.com/acr-plugin-sandbox/sample-app#7")
	if err != nil {
		t.Fatal(err)
	}
	const head = "e9009ec0098be0aa6733ce77c43bc53161ad9c5f"
	const id = "20261120T120000Z-deadbeef"
	dir, err := state.CreateRunDir(work, id)
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.New(id, cr, core.DefaultLimits(), []core.Entry{{CR: cr, Status: core.Pending, Title: "Show when the session expires", HeadSHA: head}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st.ChangeRequests[0].Status = core.Collected
	if err := st.Advance(state.Collected, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(dir, st); err != nil {
		t.Fatal(err)
	}
	crDir, err := state.CreateChangeRequestDir(dir, cr, head)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "workpackage", "valid", "s2-root-with-related-changes.json"))
	if err != nil {
		t.Fatal(err)
	}
	wp, err := core.DecodeWorkPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	wp.RunID, wp.JudgementPath = id, filepath.Base(crDir)+"/"+state.JudgementFile
	if data, err = core.EncodeWorkPackage(wp); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crDir, state.WorkPackageFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(crDir, state.JudgementFile)
}

func TestNextBinaryServesARunAndSaysWhenItIsDone(t *testing.T) {
	bin, work, _ := reviewSetup(t)
	judgement := writeCollectedRun(t, work)

	stdout, stderr, code := runIn(t, bin, t.TempDir(), "", "next")
	first, _, _ := strings.Cut(stdout, "\n")
	want := "Judge github:github.com/acr-plugin-sandbox/sample-app#7 part 1 of 1 — write the judgement to " + judgement
	if code != 0 || stderr != "" || first != want || !strings.Contains(stdout, "Work package github:github.com/acr-plugin-sandbox/sample-app#7") {
		t.Fatalf("next: code %d, stderr %q, stdout:\n%s", code, stderr, stdout)
	}

	if err := os.WriteFile(judgement, []byte(`{"schema":1,"cr":"x","headSha":"e9009ec0098be0aa6733ce77c43bc53161ad9c5f","findings":[],"preExisting":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runIn(t, bin, t.TempDir(), "", "next", "--verbose")
	if code != 0 || stdout != "Done: run agent-code-review finalize\n" || !strings.Contains(stderr, "nothing left to judge") {
		t.Errorf("next after the judgement: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	log, err := os.ReadFile(filepath.Join(filepath.Dir(judgement), "..", "run.log"))
	if err != nil || strings.Count(string(log), "\tnext\t") != 2 {
		t.Errorf("run.log: %q, %v", log, err)
	}
}

func TestNextBinaryWithAStateItCannotRead(t *testing.T) {
	bin, work, _ := reviewSetup(t)
	dir, err := state.CreateRunDir(work, "20261120T120000Z-deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, state.FileName), []byte(`{"schema": 99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runIn(t, bin, t.TempDir(), "", "next")
	if code != 8 || stdout != "" || !strings.Contains(stderr, "unsupported schema version 99") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

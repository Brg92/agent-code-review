package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/state"
)

const (
	sampleCR   = "github:github.com/acr-plugin-sandbox/sample-app#7"
	sampleHead = "e9009ec0098be0aa6733ce77c43bc53161ad9c5f"
)

func TestFinalizeAndShowBinary(t *testing.T) {
	bin, work, _ := reviewSetup(t)
	judgement := writeCollectedRun(t, work)
	cwd := t.TempDir()

	// Nothing is judged yet: every problem is listed, and nothing is written.
	stdout, stderr, code := runIn(t, bin, cwd, "", "finalize")
	if code != 3 || stdout != "" || !strings.Contains(stderr, "missing judgement for "+sampleCR+", run agent-code-review next") {
		t.Fatalf("finalize without judgement: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, _, code := runIn(t, bin, cwd, "", "show"); code != 8 {
		t.Errorf("show before finalize: code %d", code)
	}

	bad := `{"schema":1,"cr":"` + sampleCR + `","headSha":"` + strings.Repeat("0", 40) + `","findings":[],"preExisting":[]}`
	if err := os.WriteFile(judgement, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runIn(t, bin, cwd, "", "finalize")
	if code != 3 || stdout != "" || !strings.Contains(stderr, "/headSha:") {
		t.Fatalf("finalize with a judgement for another head: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	good := `{"schema":1,"cr":"` + sampleCR + `","headSha":"` + sampleHead + `","findings":[],"preExisting":[]}`
	if err := os.WriteFile(judgement, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runIn(t, bin, cwd, "", "finalize", "--verbose")
	report := filepath.Join(filepath.Dir(judgement), state.ReportFile)
	if code != 0 || !strings.Contains(stdout, "Review completed. Nothing was written to GitHub.") || !strings.Contains(stdout, report) ||
		!strings.Contains(stdout, "Overall: OK") || !strings.Contains(stderr, "\tfinalize\t") {
		t.Fatalf("finalize: code %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if _, _, code := runIn(t, bin, cwd, "", "finalize"); code != 8 {
		t.Errorf("second finalize: code %d", code)
	}

	stdout, stderr, code = runIn(t, bin, cwd, "", "show")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "Cascade — acr-plugin-sandbox/sample-app#7 · 1 reviewed") {
		t.Errorf("show: code %d, stderr %q, stdout:\n%s", code, stderr, stdout)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runIn(t, bin, cwd, "", "show", "acr-plugin-sandbox/sample-app#7")
	if code != 0 || stderr != "" || stdout != string(data) {
		t.Errorf("show <cr>: code %d, stderr %q, stdout:\n%s", code, stderr, stdout)
	}
	if _, stderr, code := runIn(t, bin, cwd, "", "show", "acr-plugin-sandbox/sample-app#8"); code != 2 || !strings.Contains(stderr, "not a change request of this run") {
		t.Errorf("show of a stranger: code %d, stderr %q", code, stderr)
	}

	saved := filepath.Join(t.TempDir(), "reports")
	stdout, stderr, code = runIn(t, bin, cwd, "", "show", "--save", saved)
	path := strings.TrimSuffix(stdout, "\n")
	if code != 0 || stderr != "" || filepath.Dir(path) != saved || !strings.HasPrefix(filepath.Base(path), "sample-app-7-e9009ec0_") {
		t.Fatalf("show --save: code %d, stderr %q, stdout %q", code, stderr, stdout)
	}
	if content, err := os.ReadFile(path); err != nil || !strings.Contains(string(content), "Cascade — ") {
		t.Errorf("saved report: %q, %v", content, err)
	}

	// A folder inside a git work tree, or with "..", is refused.
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(repo, "reports"), saved + string(filepath.Separator) + ".." + string(filepath.Separator) + "x"} {
		stdout, stderr, code = runIn(t, bin, cwd, "", "show", "--save", dir)
		if code != 9 || stdout != "" || !strings.Contains(stderr, "agent-code-review: could not") {
			t.Errorf("show --save %s: code %d, stdout %q, stderr %q", dir, code, stdout, stderr)
		}
	}
	if entries, _ := os.ReadDir(repo); len(entries) != 1 {
		t.Errorf("something was written in the work tree: %v", entries)
	}
}

func TestFinalizeAndShowBinaryRefuseWhatTheyCannotDo(t *testing.T) {
	bin, _, _ := reviewSetup(t)
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no run", []string{"finalize"}, 8, "agent-code-review review"},
		{"no run to show", []string{"show"}, 8, "agent-code-review review"},
		{"an argument", []string{"finalize", "extra"}, 2, "takes no argument"},
		{"a run id that is not one", []string{"show", "--run", "../x"}, 2, "run id"},
		{"a folder without --save", []string{"show", "/some/folder"}, 2, "a folder is for --save"},
		{"two change requests", []string{"show", "a/b#1", "c/d#2"}, 2, "give one change request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runIn(t, bin, t.TempDir(), "", tt.args...)
			if code != tt.code || stdout != "" || !strings.Contains(stderr, tt.stderr) {
				t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestUserConfigurationThatIsNotValidExits3(t *testing.T) {
	bin, work, _ := reviewSetup(t)
	judgement := writeCollectedRun(t, work)
	good := `{"schema":1,"cr":"` + sampleCR + `","headSha":"` + sampleHead + `","findings":[],"preExisting":[]}`
	if err := os.WriteFile(judgement, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("HOME", cfgHome)
	t.Setenv("AppData", cfgHome)
	for _, dir := range []string{filepath.Join(cfgHome, "agent-code-review"), filepath.Join(cfgHome, ".config", "agent-code-review"), filepath.Join(cfgHome, "Library", "Application Support", "agent-code-review")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"schema": 1, "keepDays": "many"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := runIn(t, bin, t.TempDir(), "", "finalize")
	if code != 3 || stdout != "" || !strings.Contains(stderr, "/keepDays") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

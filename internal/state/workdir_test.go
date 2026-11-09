package state

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

// env returns a getenv over vars.
func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestWorkDirBase(t *testing.T) {
	cache := t.TempDir()
	cacheDir := func() (string, error) { return cache, nil }
	got, err := WorkDirBase(env(nil), cacheDir)
	if err != nil || got != filepath.Join(cache, "agent-code-review") {
		t.Errorf("default base = %q, %v, want %q", got, err, filepath.Join(cache, "agent-code-review"))
	}
	override := t.TempDir()
	got, err = WorkDirBase(env(map[string]string{WorkDirEnv: override}), cacheDir)
	if err != nil || got != override {
		t.Errorf("%s base = %q, %v, want %q", WorkDirEnv, got, err, override)
	}
	if _, err := WorkDirBase(env(map[string]string{WorkDirEnv: "relative/dir"}), cacheDir); err == nil {
		t.Error("a relative ACR_WORKDIR was accepted")
	}
	noCache := func() (string, error) { return "", errors.New("no cache directory") }
	if _, err := WorkDirBase(env(nil), noCache); err == nil {
		t.Error("no error without a cache directory")
	}
}

const testRunID = "20261109T120000Z-0a1b2c3d"

func TestCreateRunDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "agent-code-review")
	dir, err := CreateRunDir(base, testRunID)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "runs", testRunID); dir != want {
		t.Errorf("run dir = %q, want %q", dir, want)
	}
	checkPrivateDir(t, dir)
	if again, err := CreateRunDir(base, testRunID); err != nil || again != dir {
		t.Errorf("creating it again: %q, %v", again, err)
	}
	for _, bad := range []string{"", "short", "../../escape", "a/b-c-d-e-f", testRunID + "/x"} {
		if _, err := CreateRunDir(base, bad); err == nil {
			t.Errorf("run id %q accepted", bad)
		}
	}
	if got, want := filepath.Join(dir, CascadeFile), filepath.Join(dir, "cascade.txt"); got != want {
		t.Errorf("cascade file %q", got)
	}
}

func TestChangeRequestDirName(t *testing.T) {
	head := "9b96ceb6a1f0e2d3c4b5a6978877665544332211"
	tests := []struct {
		key  string
		want string
	}{
		{"github:github.com/acr-plugin-sandbox/sample-app#53", "acr-plugin-sandbox_sample-app-53-9b96ceb6"},
		{"gitlab:gitlab.example.com/group/sub/lib#7", "group_sub_lib-7-9b96ceb6"},
	}
	for _, tt := range tests {
		k, err := core.ParseKey(tt.key)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := ChangeRequestDirName(k, head); err != nil || got != tt.want {
			t.Errorf("%s: %q, %v, want %q", tt.key, got, err, tt.want)
		}
	}
	k, _ := core.ParseKey("github:github.com/o/r#1")
	for _, bad := range []string{"", "9b96ceb", "9B96CEB6", "zzzzzzzz"} {
		if _, err := ChangeRequestDirName(k, bad); err == nil {
			t.Errorf("head %q accepted", bad)
		}
	}
}

func TestCreateChangeRequestDir(t *testing.T) {
	run, err := CreateRunDir(t.TempDir(), testRunID)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := core.ParseKey("github:github.com/acr-plugin-sandbox/core-lib#2")
	dir, err := CreateChangeRequestDir(run, k, "0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(run, "acr-plugin-sandbox_core-lib-2-01234567"); dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	checkPrivateDir(t, dir)
	names := []string{WorkPackageFile, JudgementFile, FindingsFile, ReportFile}
	if want := []string{"workpackage.json", "judgement.json", "findings.json", "report.txt"}; !slices.Equal(names, want) {
		t.Errorf("file names %q, want %q", names, want)
	}
}

func checkPrivateDir(t *testing.T, dir string) {
	t.Helper()
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s: %v, want a directory", dir, err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("%s has mode %v, want 0700", dir, info.Mode().Perm())
	}
}

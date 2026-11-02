package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/version"
)

const versionVar = "github.com/Brg92/agent-code-review/internal/version.Version"

// buildBinary compiles the command into a temp dir, optionally injecting a version.
func buildBinary(t *testing.T, injected string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agent-code-review")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	args := []string{"build", "-o", bin}
	if injected != "" {
		args = append(args, "-ldflags", "-X "+versionVar+"="+injected)
	}
	args = append(args, ".")
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func run(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errOut.String(), code
}

func TestVersionOutput(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		want     string
	}{
		{"default", "", "0.0.0-dev"},
		{"release", "0.1.0", "0.1.0"},
		{"pre-release", "0.9.0-beta.1", "0.9.0-beta.1"},
		{"build metadata", "0.0.0-ci+abc1234", "0.0.0-ci+abc1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := buildBinary(t, tt.injected)
			for _, arg := range []string{"version", "--version"} {
				stdout, stderr, code := run(t, bin, arg)
				if code != 0 {
					t.Errorf("%s: exit code = %d, want 0", arg, code)
				}
				if stdout != tt.want+"\n" {
					t.Errorf("%s: stdout = %q, want %q", arg, stdout, tt.want+"\n")
				}
				if stderr != "" {
					t.Errorf("%s: stderr = %q, want empty", arg, stderr)
				}
				if got := strings.TrimSuffix(stdout, "\n"); !version.IsValidSemVer(got) {
					t.Errorf("%s: output %q is not valid SemVer", arg, got)
				}
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	bin := buildBinary(t, "")
	tests := []struct {
		name string
		args []string
	}{
		{"no command", nil},
		{"unknown command", []string{"frobnicate"}},
		{"unknown flag", []string{"--frobnicate"}},
		{"extra argument after version", []string{"version", "extra"}},
		{"extra argument after --version", []string{"--version", "extra"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := run(t, bin, tt.args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "usage:") {
				t.Errorf("stderr = %q, want usage", stderr)
			}
		})
	}
}

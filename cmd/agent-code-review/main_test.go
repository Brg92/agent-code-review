package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
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

func TestVersionInjection(t *testing.T) {
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
			var stdout bytes.Buffer
			cmd := exec.Command(buildBinary(t, tt.injected))
			cmd.Stdout = &stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := stdout.String(); got != tt.want+"\n" {
				t.Errorf("stdout = %q, want %q", got, tt.want+"\n")
			}
		})
	}
}

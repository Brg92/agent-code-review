package cli

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/version"
)

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(t.Context(), []string{arg}, nil, strings.NewReader(""), &stdout, &stderr); code != exitOK {
				t.Errorf("Run(%q) = %d, want %d", arg, code, exitOK)
			}
			if want := version.Version + "\n"; stdout.String() != want {
				t.Errorf("stdout = %q, want %q", stdout.String(), want)
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no command", nil},
		{"empty argument", []string{""}},
		{"unknown command", []string{"frobnicate"}},
		{"unknown flag", []string{"--frobnicate"}},
		{"single dash version", []string{"-version"}},
		{"extra argument after version", []string{"version", "extra"}},
		{"extra argument after --version", []string{"--version", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(t.Context(), tt.args, nil, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
				t.Errorf("Run(%q) = %d, want %d", tt.args, code, exitUsage)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if stderr.String() != usage {
				t.Errorf("stderr = %q, want %q", stderr.String(), usage)
			}
		})
	}
}

// A command finds the token in the environment it is given, not in the
// one of the process.
func TestRunReadsTheGivenEnvironment(t *testing.T) {
	t.Setenv("ACR_GITHUB_TOKEN", "")
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), []string{"auth", "status"}, map[string]string{"ACR_GITHUB_TOKEN": testToken}, strings.NewReader(""), &stdout, &stderr)
	if code != exitOK || !strings.Contains(stdout.String(), "environment") {
		t.Fatalf("auth status with a token in the given environment = %d\n%s%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), testToken) {
		t.Errorf("the token is in the output:\n%s%s", stdout.String(), stderr.String())
	}
}

func TestRunStopsWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := Run(ctx, []string{"version"}, nil, strings.NewReader(""), &stdout, &stderr); code != exitInterrupted {
		t.Errorf("Run with a done context = %d, want %d", code, exitInterrupted)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "interrupted") {
		t.Errorf("stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

func TestEnvironmentLookup(t *testing.T) {
	vars := map[string]string{"Path": "p", "ACR_X": "x"}
	tests := []struct {
		name, goos, key, want string
	}{
		{"exact name", "linux", "ACR_X", "x"},
		{"case matters on linux", "linux", "PATH", ""},
		{"case does not matter on windows", "windows", "PATH", "p"},
		{"exact name on windows", "windows", "ACR_X", "x"},
		{"missing", "windows", "NOPE", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lookup(vars, tt.goos)(tt.key); got != tt.want {
				t.Errorf("lookup(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestEnvironmentFromTheProcess(t *testing.T) {
	got := EnvMap([]string{"A=1", "B=x=y", "=C:=C:\\dir", "NOEQUALS", "A=2"})
	want := map[string]string{"A": "2", "B": "x=y", "=C:": "C:\\dir"}
	if len(got) != len(want) {
		t.Fatalf("EnvMap = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("EnvMap[%q] = %q, want %q", k, got[k], v)
		}
	}
	list := environList(map[string]string{"B": "2", "A": "1"})
	if !slices.Equal(list, []string{"A=1", "B=2"}) {
		t.Errorf("environList = %q, want sorted NAME=value entries", list)
	}
}

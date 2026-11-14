package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/version"
)

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run([]string{arg}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
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
			if code := Run(tt.args, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
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

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Brg92/agent-code-review/internal/version"
)

// A host starts bin\agent-code-review.cmd by its absolute path, which holds
// spaces (a user profile, Program Files) and may hold parentheses
// (Program Files (x86)), with arguments that hold spaces: show --save <dir>.
// These tests start it as a shell would, and check that the argument arrives
// unchanged and that the exit code passes through. cmd.exe and PowerShell are
// run here only to play the host; the product never starts a shell.

// hostStart runs a command line the way a host does and returns what the
// launcher printed and how it ended.
type hostStart func(t *testing.T, launcher string, args ...string) (stdout, stderr string, code int)

// inQuotes puts an argument in double quotes, as a shell is told to.
func inQuotes(s string) string { return `"` + s + `"` }

// viaCmd runs cmd.exe with an explicit command line: /d /s /c, and the whole
// command in one more pair of quotes, which /s removes, so that the first and
// last quotes of the launcher and the arguments are kept.
func viaCmd(t *testing.T, launcher string, args ...string) (string, string, int) {
	t.Helper()
	parts := []string{inQuotes(launcher)}
	for _, a := range args {
		parts = append(parts, inQuotes(a))
	}
	system := os.Getenv("SystemRoot")
	cmdExe := filepath.Join(system, "System32", "cmd.exe")
	cmd := exec.Command(cmdExe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: inQuotes(cmdExe) + ` /d /s /c "` + strings.Join(parts, " ") + `"`}
	return runHost(t, cmd)
}

// viaPowerShell runs & "<launcher>" "<arg>", then ends with the exit code of
// the last native command.
func viaPowerShell(t *testing.T, launcher string, args ...string) (string, string, int) {
	t.Helper()
	script := "& " + inQuotes(launcher)
	for _, a := range args {
		script += " " + inQuotes(a)
	}
	script += "; exit $LASTEXITCODE"
	return runHost(t, exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script))
}

func runHost(t *testing.T, cmd *exec.Cmd) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run %v: %v", cmd.Args, err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errOut.String(), code
}

func TestCmdLauncherStartedByAbsolutePathWithSpaces(t *testing.T) {
	for _, install := range []string{"plugin root", "Program Files (x86)"} {
		r := newReleaseIn(t, install)
		if !filepath.IsAbs(r.launcher) || !strings.Contains(r.launcher, " ") {
			t.Fatalf("launcher %q is not an absolute path with spaces", r.launcher)
		}
		for name, host := range map[string]hostStart{"cmd.exe with an explicit command line": viaCmd, "PowerShell": viaPowerShell} {
			t.Run(install+", "+name, func(t *testing.T) {
				t.Run("version", func(t *testing.T) {
					stdout, stderr, code := host(t, r.launcher, "version")
					if code != 0 || strings.TrimSpace(stdout) != version.Version {
						t.Errorf("version: code %d, stdout %q, stderr %q", code, stdout, stderr)
					}
				})
				t.Run("an argument with spaces arrives unchanged", func(t *testing.T) {
					dir := filepath.Join(t.TempDir(), "a folder  with spaces (and parentheses)")
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					stdout, stderr, code := host(t, r.launcher, "check-criteria", dir)
					if code != 0 || !strings.Contains(stdout, "Repository criteria in "+dir+"\n") && !strings.Contains(stdout, "Repository criteria in "+dir+"\r\n") {
						t.Errorf("check-criteria: code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
					}
				})
				t.Run("the exit code passes through", func(t *testing.T) {
					stdout, stderr, code := host(t, r.launcher, "no-such-command", "an argument with spaces")
					if code != 2 || strings.TrimSpace(stdout) != "" || !strings.HasPrefix(stderr, "usage: agent-code-review") {
						t.Errorf("unknown command: code %d, stdout %q, stderr %q", code, stdout, stderr)
					}
				})
			})
		}
	}
}

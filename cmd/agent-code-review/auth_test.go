package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/credhelper"
)

const testToken = "test-token-value-0123"

func TestMain(m *testing.M) {
	credhelper.Main()
	os.Exit(m.Run())
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// runIn runs the binary in dir with stdin, and fails if any output shows
// the token.
func runIn(t *testing.T, bin, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exit.ExitCode()
	}
	for _, s := range []string{out.String(), errOut.String()} {
		if strings.Contains(s, testToken) {
			t.Errorf("%v: output leaks the token: %q", args, s)
		}
	}
	return out.String(), errOut.String(), code
}

// authSetup builds the binary and isolates git with the test helper.
func authSetup(t *testing.T) (bin, dir, store string) {
	t.Helper()
	requireGit(t)
	bin = buildBinary(t, "")
	store, _ = credhelper.Install(t, credhelper.Config{})
	t.Setenv("ACR_GITHUB_TOKEN", "")
	return bin, t.TempDir(), store
}

func TestAuthStatusBinary(t *testing.T) {
	bin, dir, _ := authSetup(t)

	stdout, stderr, code := runIn(t, bin, dir, "", "auth", "status")
	if code != 6 || stdout != "" || !strings.Contains(stderr, "no token") {
		t.Errorf("status without token: code %d stdout %q stderr %q", code, stdout, stderr)
	}

	t.Setenv("ACR_GITHUB_TOKEN", testToken)
	stdout, stderr, code = runIn(t, bin, dir, "", "auth", "status")
	if code != 0 || !strings.Contains(stdout, "ACR_GITHUB_TOKEN") || stderr != "" {
		t.Errorf("status with env token: code %d stdout %q stderr %q", code, stdout, stderr)
	}

	stdout, stderr, code = runIn(t, bin, dir, "", "auth", "status", "--host", "bad host")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "usage:") {
		t.Errorf("status with bad host: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

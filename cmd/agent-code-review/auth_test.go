package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestAuthLoginBinary(t *testing.T) {
	bin, dir, store := authSetup(t)

	stdout, stderr, code := runIn(t, bin, dir, testToken+"\n", "auth", "login")
	if code != 0 || !strings.Contains(stdout, "token stored") {
		t.Fatalf("login: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	entries, _ := credhelper.Read(t, store)
	if len(entries) != 1 || entries[0] != (credhelper.Entry{Host: "github.com", Username: "acr-api", Password: testToken}) {
		t.Errorf("stored entries = %+v", entries)
	}
	stdout, _, code = runIn(t, bin, dir, "", "auth", "status")
	if code != 0 || !strings.Contains(stdout, "git credential helper") {
		t.Errorf("status after login: code %d stdout %q", code, stdout)
	}

	for name, args := range map[string][]string{
		"token argument": {"auth", "login", testToken},
		"token flag":     {"auth", "login", "--token=" + testToken},
	} {
		stdout, stderr, code := runIn(t, bin, dir, "", args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "read from stdin") {
			t.Errorf("%s: code %d stdout %q stderr %q", name, code, stdout, stderr)
		}
	}

	stdout, stderr, code = runIn(t, bin, dir, "", "auth", "login", "--host", "ghe.example.invalid")
	if code != 6 || stdout != "" || !strings.Contains(stderr, "no token") {
		t.Errorf("login with empty stdin: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestAuthLoginBinaryRefusesPlainText(t *testing.T) {
	requireGit(t)
	bin := buildBinary(t, "")
	_, home := credhelper.Install(t, credhelper.Config{Helpers: []string{"store"}})
	stdout, stderr, code := runIn(t, bin, t.TempDir(), testToken+"\n", "auth", "login")
	if code != 10 || stdout != "" || !strings.Contains(stderr, "plain text") {
		t.Errorf("login with store helper: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".git-credentials")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plain-text credentials written: %v", err)
	}
}

func TestAuthLogoutBinary(t *testing.T) {
	bin, dir, store := authSetup(t)
	if _, _, code := runIn(t, bin, dir, testToken+"\n", "auth", "login"); code != 0 {
		t.Fatalf("login: code %d", code)
	}
	stdout, stderr, code := runIn(t, bin, dir, "", "auth", "logout")
	if code != 0 || !strings.Contains(stdout, "token removed") || stderr != "" {
		t.Errorf("logout: code %d stdout %q stderr %q", code, stdout, stderr)
	}
	if entries, _ := credhelper.Read(t, store); len(entries) != 0 {
		t.Errorf("entries after logout = %+v", entries)
	}
	if _, _, code := runIn(t, bin, dir, "", "auth", "status"); code != 6 {
		t.Errorf("status after logout: code %d, want 6", code)
	}
	if _, _, code := runIn(t, bin, dir, "", "auth", "logout"); code != 0 {
		t.Errorf("second logout: code %d, want 0", code)
	}
}

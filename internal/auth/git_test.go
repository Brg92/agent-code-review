package auth

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/credhelper"
)

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

// realResolver runs the real git outside any repository and without the
// developer's environment token.
func realResolver(t *testing.T) Resolver {
	return Resolver{Getenv: func(string) string { return "" }, Git: GitRunner{Dir: t.TempDir()}}
}

func TestRealGitRoundTrip(t *testing.T) {
	requireGit(t)
	store, _ := credhelper.Install(t, credhelper.Config{})
	r := realResolver(t)
	ctx := context.Background()

	check, err := r.CheckHelpers(ctx, "github.com")
	if err != nil {
		t.Fatalf("CheckHelpers: %v", err)
	}
	// Anything but the test helper means the isolation leaks into the
	// developer's or runner's configuration: stop before storing.
	if len(check.Helpers) != 1 || check.Helpers[0].Value != credhelper.Name {
		t.Fatalf("helpers = %v, want only the test helper", check.Helpers)
	}

	if _, err := r.Resolve(ctx, "github.com"); !errors.Is(err, ErrNoToken) {
		t.Fatalf("Resolve before login = %v, want ErrNoToken", err)
	}
	if _, err := r.Save(ctx, "github.com", testToken); err != nil {
		t.Fatalf("Save: %v", err)
	}
	tok, err := r.Resolve(ctx, "github.com")
	if err != nil || tok.Value() != testToken || tok.Source != SourceCredentialHelper {
		t.Fatalf("Resolve after login = %v, %v", tok, err)
	}
	entries, seen := credhelper.Read(t, store)
	if len(entries) != 1 || entries[0] != (credhelper.Entry{Host: "github.com", Username: Username, Password: testToken}) {
		t.Errorf("stored entries = %+v", entries)
	}
	for k, want := range map[string]string{"GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "GCM_INTERACTIVE": "never"} {
		if seen[k] != want {
			t.Errorf("helper saw %s=%q, want %q", k, seen[k], want)
		}
	}

	if err := r.Erase(ctx, "github.com"); err != nil {
		t.Fatalf("Erase: %v", err)
	}
	if _, err := r.Resolve(ctx, "github.com"); !errors.Is(err, ErrNoToken) {
		t.Errorf("Resolve after logout = %v, want ErrNoToken", err)
	}
}

func TestRealGitLeavesUserCredentialsAlone(t *testing.T) {
	requireGit(t)
	credhelper.Install(t, credhelper.Config{})
	ctx := context.Background()
	user := GitRunner{Dir: t.TempDir()}
	own := "protocol=https\nhost=github.com\nusername=octo-user\npassword=user-own-pass-1\n\n"
	if _, _, err := user.Run(ctx, own, "credential", "approve"); err != nil {
		t.Fatal(err)
	}

	r := realResolver(t)
	if _, err := r.Resolve(ctx, "github.com"); !errors.Is(err, ErrNoToken) {
		t.Errorf("Resolve picked up the user's credential: %v", err)
	}
	if _, err := r.Save(ctx, "github.com", testToken); err != nil {
		t.Fatal(err)
	}
	if err := r.Erase(ctx, "github.com"); err != nil {
		t.Fatal(err)
	}
	out, _, err := user.Run(ctx, "protocol=https\nhost=github.com\nusername=octo-user\n\n", "credential", "fill")
	if err != nil || !strings.Contains(out, "password=user-own-pass-1") {
		t.Errorf("user's own credential changed: %v", err)
	}
}

func TestRealGitRefusesUnsafeHelpers(t *testing.T) {
	requireGit(t)
	for name, helpers := range map[string][]string{
		"no helper":   {""},
		"store":       {"store"},
		"store first": {"store", credhelper.Name},
	} {
		t.Run(name, func(t *testing.T) {
			_, home := credhelper.Install(t, credhelper.Config{Helpers: helpers})
			_, err := realResolver(t).Save(context.Background(), "github.com", testToken)
			if !errors.Is(err, ErrNoHelper) && !errors.Is(err, ErrPlainTextHelper) {
				t.Fatalf("Save = %v, want a refusal", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".git-credentials")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("a plain-text credential file was written: %v", err)
			}
		})
	}
}

func TestGitRunnerMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := realResolver(t).Resolve(context.Background(), "github.com")
	if err == nil || errors.Is(err, ErrNoToken) {
		t.Errorf("Resolve without git = %v, want a git error", err)
	}
}

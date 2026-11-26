package auth

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const promptsDisabled = "fatal: could not read Password for 'https://acr-api@github.com': terminal prompts disabled\n"

func TestFillTellsNoCredentialFromHelperFailure(t *testing.T) {
	tests := []struct {
		name       string
		a          answer
		wantNoItem bool
	}{
		{"no credential stored", answer{stderr: promptsDisabled, err: &ExitError{Code: 128}}, true},
		{"no credential, no newline", answer{stderr: strings.TrimSuffix(promptsDisabled, "\n"), err: &ExitError{Code: 128}}, true},
		{"no credential, username prompt", answer{stderr: "fatal: could not read Username for 'https://github.com': terminal prompts disabled\n", err: &ExitError{Code: 128}}, true},
		{"helper complained first", answer{stderr: "error: keychain is locked\n" + promptsDisabled, err: &ExitError{Code: 128}}, false},
		{"helper complained after", answer{stderr: promptsDisabled + "warning: helper crashed\n", err: &ExitError{Code: 128}}, false},
		{"other fatal error", answer{stderr: "fatal: unable to read config file\n", err: &ExitError{Code: 128}}, false},
		{"no message", answer{err: &ExitError{Code: 128}}, false},
		{"other status", answer{stderr: promptsDisabled, err: &ExitError{Code: 1}}, false},
		{"git missing", answer{err: exec.ErrNotFound}, false},
		{"timeout", answer{err: &TimeoutError{After: time.Second}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Getenv: env(nil), Git: &fakeGit{answers: map[string]answer{"credential fill": tt.a}}}
			_, err := r.Resolve(context.Background(), "github.com")
			var he *HelperError
			switch {
			case tt.wantNoItem && !errors.Is(err, ErrNoToken):
				t.Errorf("Resolve = %v, want ErrNoToken", err)
			case tt.wantNoItem && errors.As(err, &he):
				t.Errorf("Resolve = %v, a HelperError for a missing credential", err)
			case !tt.wantNoItem && (!errors.As(err, &he) || errors.Is(err, ErrNoToken)):
				t.Errorf("Resolve = %v, want a *HelperError that is not ErrNoToken", err)
			}
			if he != nil && he.Op != "git credential fill" {
				t.Errorf("Op = %q, want git credential fill", he.Op)
			}
		})
	}
}

func TestHelperErrorNeverQuotesGitOutput(t *testing.T) {
	const echoed = "echoed-by-the-helper"
	git := &fakeGit{answers: map[string]answer{
		"credential fill": {stdout: "password=" + echoed + "\n", stderr: "error: helper got password=" + echoed + "\n", err: &ExitError{Code: 1}},
	}}
	_, err := (Resolver{Getenv: env(nil), Git: git}).Resolve(context.Background(), "github.com")
	if err == nil || strings.Contains(err.Error(), echoed) {
		t.Errorf("Resolve error %v quotes what git printed", err)
	}
}

func TestHelperErrorWrapsTheCause(t *testing.T) {
	cause := &ExitError{Code: 3}
	err := error(&HelperError{Op: "git credential approve", Err: cause})
	var exit *ExitError
	if !errors.As(err, &exit) || exit != cause {
		t.Error("HelperError does not unwrap to its cause")
	}
	if got := err.Error(); !strings.Contains(got, "credential approve") || !strings.Contains(got, "status 3") {
		t.Errorf("Error() = %q", got)
	}
}

func TestOtherGitCallsReturnHelperErrors(t *testing.T) {
	boom := &ExitError{Code: 5}
	tests := map[string]struct {
		git  *fakeGit
		call func(Resolver) error
		op   string
	}{
		"approve": {keychainGitWithApprove(answer{err: boom}), func(r Resolver) error { _, err := r.Save(context.Background(), "github.com", testToken); return err }, "git credential approve"},
		"reject":  {&fakeGit{answers: map[string]answer{"credential reject": {err: boom}}}, func(r Resolver) error { return r.Erase(context.Background(), "github.com") }, "git credential reject"},
		"config":  {&fakeGit{answers: map[string]answer{helperQuery: {err: boom}}}, func(r Resolver) error { _, err := r.CheckHelpers(context.Background(), "github.com"); return err }, "git config (reading the credential helpers)"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.call(Resolver{Getenv: env(nil), Git: tt.git})
			var he *HelperError
			if !errors.As(err, &he) || he.Op != tt.op || !errors.Is(err, boom) {
				t.Errorf("error %v is not a *HelperError for %q wrapping the exit error", err, tt.op)
			}
		})
	}
}

func keychainGitWithApprove(approve answer) *fakeGit {
	g := keychainGit(storedFill(testToken))
	g.answers["credential approve"] = approve
	return g
}

func TestTokensAreNormalizedWhereverTheyComeFrom(t *testing.T) {
	fillWith := func(password string) *fakeGit {
		return &fakeGit{answers: map[string]answer{"credential fill": {stdout: "username=acr-api\npassword=" + password + "\n"}}}
	}
	tests := []struct {
		name    string
		envVal  string
		fill    string
		want    string
		wantErr error
	}{
		{"env trimmed", " " + testToken + "\t\r\n", "", testToken, nil},
		{"env with a space inside", "abc def", "", "", ErrInvalidToken},
		{"env with a newline inside", "abc\nhost=evil", "", "", ErrInvalidToken},
		{"env with a control character", "abc\x01def", "", "", ErrInvalidToken},
		{"env with DEL", "abc\x7fdef", "", "", ErrInvalidToken},
		{"blank env falls back to the helper", " \t", testToken, testToken, nil},
		{"helper trimmed", "", " " + testToken + " ", testToken, nil},
		{"helper with a space inside", "", "abc def", "", ErrInvalidToken},
		{"helper with a control character", "", "abc\x01def", "", ErrInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{Getenv: env(map[string]string{"ACR_GITHUB_TOKEN": tt.envVal}), Git: fillWith(tt.fill)}
			tok, err := r.Resolve(context.Background(), "github.com")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Resolve error = %v, want %v", err, tt.wantErr)
			}
			if tok.Value() != tt.want {
				t.Errorf("Resolve value = %q, want %q", tok.Value(), tt.want)
			}
			if err != nil && (strings.Contains(err.Error(), "def") || strings.Contains(err.Error(), "evil")) {
				t.Errorf("error %q quotes the token", err)
			}
		})
	}
}

func TestGitRunnerTimeout(t *testing.T) {
	t.Setenv(hangEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	g := GitRunner{Dir: t.TempDir(), Timeout: 200 * time.Millisecond, program: exe}
	start := time.Now()
	_, _, err = g.Run(context.Background(), "", "credential", "fill")
	var te *TimeoutError
	if !errors.As(err, &te) || te.After != 200*time.Millisecond {
		t.Fatalf("Run = %v, want a *TimeoutError after 200ms", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("Run took %v to give up", elapsed)
	}
	if !strings.Contains(err.Error(), "200ms") {
		t.Errorf("message %q does not say how long it waited", err)
	}
}

func TestGitRunnerCancelIsNotATimeout(t *testing.T) {
	t.Setenv(hangEnv, "1")
	exe, _ := os.Executable()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	g := GitRunner{Dir: t.TempDir(), Timeout: time.Minute, program: exe}
	_, _, err := g.Run(ctx, "", "credential", "fill")
	var te *TimeoutError
	if err == nil || errors.As(err, &te) || !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
}

func TestGitRunnerDefaultTimeout(t *testing.T) {
	if got := (GitRunner{}).timeout(); got != 30*time.Second || DefaultGitTimeout != 30*time.Second {
		t.Errorf("default timeout = %v (constant %v), want 30s", got, DefaultGitTimeout)
	}
	if got := (GitRunner{Timeout: time.Second}).timeout(); got != time.Second {
		t.Errorf("timeout = %v, want 1s", got)
	}
	if got := (GitRunner{Timeout: -1}).timeout(); got != DefaultGitTimeout {
		t.Errorf("negative timeout = %v, want the default", got)
	}
}

func TestGitRunnerKeepsMessagesInEnglish(t *testing.T) {
	if !containsAll(nonInteractive, "LC_ALL=C") {
		t.Errorf("nonInteractive = %v: git messages must not be translated, the runner reads them", nonInteractive)
	}
}

func containsAll(list []string, items ...string) bool {
	for _, it := range items {
		found := false
		for _, l := range list {
			found = found || l == it
		}
		if !found {
			return false
		}
	}
	return true
}

// envOf runs the test binary as git with the runner g and returns the
// environment it sees, one variable per line.
func envOf(t *testing.T, g GitRunner) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	g.Dir, g.program = t.TempDir(), exe
	stdout, _, err := g.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return stdout
}

// TestGitRunnerEnv checks that the environment of git is the one given, plus
// the variables that keep it from prompting, and that without one it is the
// environment of the process.
func TestGitRunnerEnv(t *testing.T) {
	t.Setenv(echoEnv, "1")
	t.Setenv("ACR_AUTH_TEST_PARENT", "parent")
	given := envOf(t, GitRunner{Env: []string{echoEnv + "=1", "ACR_AUTH_TEST_GIVEN=given"}})
	for _, want := range []string{"ACR_AUTH_TEST_GIVEN=given", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"} {
		if !strings.Contains(given, want) {
			t.Errorf("given environment: %q is missing from\n%s", want, given)
		}
	}
	if strings.Contains(given, "ACR_AUTH_TEST_PARENT") {
		t.Errorf("a variable of the process leaked into the given environment:\n%s", given)
	}
	inherited := envOf(t, GitRunner{})
	for _, want := range []string{"ACR_AUTH_TEST_PARENT=parent", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(inherited, want) {
			t.Errorf("inherited environment: %q is missing from\n%s", want, inherited)
		}
	}
}

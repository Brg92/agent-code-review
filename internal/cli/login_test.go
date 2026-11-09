package cli

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
)

var (
	errExit1 = &auth.ExitError{Code: 1}
	errExit5 = &auth.ExitError{Code: 5}
)

// fakeTerm records how the login command used the terminal.
type fakeTerm struct {
	terminal   bool
	disableErr error
	disabled   int
	restored   int
}

func (f *fakeTerm) isTerminal(io.Reader) bool { return f.terminal }

func (f *fakeTerm) disableEcho(io.Reader) (func() error, error) {
	if f.disableErr != nil {
		return nil, f.disableErr
	}
	f.disabled++
	return func() error { f.restored++; return nil }, nil
}

func loginGit(helper string) *scriptedGit {
	return &scriptedGit{answers: map[string]gitAnswer{
		helperQuery:          {stdout: "credential.helper\n" + helper + "\x00"},
		"credential approve": {},
		fill:                 haveEntry,
	}}
}

const approveInput = "protocol=https\nhost=github.com\nusername=acr-api\npassword=" + testToken + "\n\n"

func TestAuthLoginPiped(t *testing.T) {
	git := loginGit("osxkeychain")
	term := &fakeTerm{}
	r := runWith(t, env{git: git, term: term, stdin: strings.NewReader(testToken + "\nsecond line\n")}, "auth", "login")
	if r.code != exitOK || r.stdout != "github.com: token stored in the git credential helper (username acr-api)\n" || r.stderr != "" {
		t.Errorf("got %+v", r)
	}
	if git.stdin["credential approve"] != approveInput {
		t.Errorf("approve stdin = %q", git.stdin["credential approve"])
	}
	if term.disabled != 0 {
		t.Error("echo was changed for piped input")
	}
}

func TestAuthLoginTerminal(t *testing.T) {
	git := loginGit("osxkeychain")
	term := &fakeTerm{terminal: true}
	r := runWith(t, env{git: git, term: term, stdin: strings.NewReader(testToken + "\r\n")}, "auth", "login")
	if r.code != exitOK || !strings.Contains(r.stderr, "Token for github.com (input hidden): ") {
		t.Errorf("got %+v", r)
	}
	if term.disabled != 1 || term.restored != 1 {
		t.Errorf("echo disabled %d restored %d, want 1 and 1", term.disabled, term.restored)
	}
	if git.stdin["credential approve"] != approveInput {
		t.Errorf("approve stdin = %q", git.stdin["credential approve"])
	}
}

// blockingReader never returns, like a terminal nobody types into.
type blockingReader struct{ done chan struct{} }

func (b blockingReader) Read([]byte) (int, error) { <-b.done; return 0, io.EOF }

func TestAuthLoginInterruptRestoresEcho(t *testing.T) {
	term := &fakeTerm{terminal: true}
	stdin := blockingReader{done: make(chan struct{})}
	defer close(stdin.done)
	notify := func(c chan<- os.Signal) func() {
		c <- os.Interrupt
		return func() {}
	}
	git := loginGit("osxkeychain")
	r := runWith(t, env{git: git, term: term, stdin: stdin, notify: notify}, "auth", "login")
	if r.code != exitInterrupted || !strings.Contains(r.stderr, "interrupted") {
		t.Errorf("got %+v", r)
	}
	if term.disabled != 1 || term.restored != 1 {
		t.Errorf("echo disabled %d restored %d, want 1 and 1", term.disabled, term.restored)
	}
	if _, ran := git.stdin["credential approve"]; ran {
		t.Error("approve ran after an interrupt")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestAuthLoginReadErrorRestoresEcho(t *testing.T) {
	term := &fakeTerm{terminal: true}
	r := runWith(t, env{git: loginGit("osxkeychain"), term: term, stdin: errReader{}}, "auth", "login")
	if r.code != exitUnexpected || term.restored != 1 {
		t.Errorf("got %+v, restored %d", r, term.restored)
	}
	r = runWith(t, env{git: loginGit("osxkeychain"), stdin: errReader{}}, "auth", "login")
	if r.code != exitUnexpected {
		t.Errorf("piped read error: got %+v", r)
	}
}

func TestAuthLoginCannotHideInput(t *testing.T) {
	term := &fakeTerm{terminal: true, disableErr: errors.New("stty failed")}
	git := loginGit("osxkeychain")
	r := runWith(t, env{git: git, term: term, stdin: strings.NewReader(testToken + "\n")}, "auth", "login")
	if r.code != exitUnexpected || !strings.Contains(r.stderr, "cannot hide") {
		t.Errorf("got %+v", r)
	}
	if _, ran := git.stdin["credential approve"]; ran {
		t.Error("approve ran without hidden input")
	}
}

func TestAuthLoginFailures(t *testing.T) {
	tests := []struct {
		name  string
		git   *scriptedGit
		stdin string
		env   map[string]string
		code  int
		part  string
	}{
		{"store helper", loginGit("store"), testToken + "\n", nil, exitUnsafeStorage, "plain text"},
		{"no helper", &scriptedGit{answers: map[string]gitAnswer{helperQuery: {err: errExit1}}}, testToken + "\n", nil, exitUnsafeStorage, "no git credential helper"},
		{"helper query fails", &scriptedGit{answers: map[string]gitAnswer{helperQuery: {err: errExit5}}}, testToken + "\n", nil, exitAuth, "credential helpers"},
		{"empty stdin", loginGit("osxkeychain"), "", nil, exitAuth, "no token"},
		{"blank line", loginGit("osxkeychain"), "  \n", nil, exitAuth, "no token"},
		{"token with space", loginGit("osxkeychain"), "abc def\n", nil, exitAuth, "whitespace"},
		{"too long", loginGit("osxkeychain"), strings.Repeat("a", maxTokenBytes+1) + "\n", nil, exitAuth, "too long"},
		{"approve fails", &scriptedGit{answers: map[string]gitAnswer{
			helperQuery:          {stdout: "credential.helper\nosxkeychain\x00"},
			"credential approve": {err: errExit1},
		}}, testToken + "\n", nil, exitAuth, "approve"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runWith(t, env{git: tt.git, stdin: strings.NewReader(tt.stdin), getenv: func(k string) string { return tt.env[k] }}, "auth", "login")
			if r.code != tt.code || r.stdout != "" || !strings.Contains(r.stderr, tt.part) {
				t.Errorf("got %+v, want code %d and %q", r, tt.code, tt.part)
			}
		})
	}
}

func TestAuthLoginWarnings(t *testing.T) {
	r := runWith(t, env{git: loginGit("acme-vault"), stdin: strings.NewReader(testToken + "\n"),
		getenv: func(k string) string {
			if k == "ACR_GITHUB_TOKEN" {
				return "env-token-value-9"
			}
			return ""
		}}, "auth", "login")
	if r.code != exitOK || !strings.Contains(r.stderr, "warning: unknown git credential helper \"acme-vault\"") ||
		!strings.Contains(r.stderr, "warning: ACR_GITHUB_TOKEN is set and takes precedence") {
		t.Errorf("got %+v", r)
	}
	if strings.Contains(r.stderr, "env-token-value-9") {
		t.Error("stderr leaks the environment token")
	}
}

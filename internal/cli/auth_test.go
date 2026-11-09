package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
)

const testToken = "test-token-value-0123"

// gitAnswer is a scripted git result.
type gitAnswer struct {
	stdout string
	err    error
}

// scriptedGit answers git command lines and records stdin per command.
type scriptedGit struct {
	answers map[string]gitAnswer
	stdin   map[string]string
}

func (g *scriptedGit) Run(_ context.Context, stdin string, args ...string) (string, string, error) {
	key := strings.Join(args, " ")
	if g.stdin == nil {
		g.stdin = map[string]string{}
	}
	g.stdin[key] = stdin
	a, ok := g.answers[key]
	if !ok {
		return "", "", fmt.Errorf("unexpected git %s", key)
	}
	return a.stdout, "", a.err
}

const (
	helperQuery = `config --null --get-regexp ^credential\.(.*\.)?helper$`
	fill        = "credential fill"
)

var (
	noEntry   = gitAnswer{err: &auth.ExitError{Code: 128}}
	haveEntry = gitAnswer{stdout: "username=acr-api\npassword=" + testToken + "\n"}
)

type result struct {
	code           int
	stdout, stderr string
}

func runWith(t *testing.T, e env, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	e.stdout, e.stderr = &stdout, &stderr
	if e.stdin == nil {
		e.stdin = strings.NewReader("")
	}
	if e.getenv == nil {
		e.getenv = func(string) string { return "" }
	}
	code := run(args, e)
	r := result{code, stdout.String(), stderr.String()}
	for _, s := range []string{r.stdout, r.stderr} {
		if strings.Contains(s, testToken) {
			t.Errorf("output leaks the token: %q", s)
		}
	}
	return r
}

func TestAuthStatus(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		env        map[string]string
		fill       gitAnswer
		code       int
		stdout     string
		stderrPart string
	}{
		{name: "environment", env: map[string]string{"ACR_GITHUB_TOKEN": testToken}, code: exitOK,
			stdout: "github.com: token available from the environment variable ACR_GITHUB_TOKEN\n"},
		{name: "credential helper", fill: haveEntry, code: exitOK,
			stdout: "github.com: token available from the git credential helper (username acr-api)\n"},
		{name: "other host", args: []string{"--host", "ghe.example.invalid"}, fill: haveEntry, code: exitOK,
			stdout: "ghe.example.invalid: token available from the git credential helper (username acr-api)\n"},
		{name: "single dash host", args: []string{"-host=ghe.example.invalid"}, fill: haveEntry, code: exitOK,
			stdout: "ghe.example.invalid: token available from the git credential helper (username acr-api)\n"},
		{name: "none", fill: noEntry, code: exitAuth,
			stderrPart: "github.com: no token; run `agent-code-review auth login` or set ACR_GITHUB_TOKEN"},
		{name: "none on other host", args: []string{"--host", "ghe.example.invalid"}, fill: noEntry, code: exitAuth,
			stderrPart: "ghe.example.invalid: no token; run `agent-code-review auth login --host ghe.example.invalid`\n"},
		{name: "git missing", fill: gitAnswer{err: exec.ErrNotFound}, code: exitAuth, stderrPart: "git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			git := &scriptedGit{answers: map[string]gitAnswer{fill: tt.fill}}
			r := runWith(t, env{git: git, getenv: func(k string) string { return tt.env[k] }}, append([]string{"auth", "status"}, tt.args...)...)
			if r.code != tt.code || r.stdout != tt.stdout || !strings.Contains(r.stderr, tt.stderrPart) {
				t.Errorf("got %+v, want code %d stdout %q stderr containing %q", r, tt.code, tt.stdout, tt.stderrPart)
			}
		})
	}
}

func TestAuthUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		part string
	}{
		{"no subcommand", []string{"auth"}, "usage: agent-code-review auth"},
		{"unknown subcommand", []string{"auth", "frob"}, "unknown auth command \"frob\""},
		{"unknown flag", []string{"auth", "status", "--token", "x"}, "flag provided but not defined"},
		{"extra argument", []string{"auth", "status", "extra"}, "unexpected argument"},
		{"invalid host", []string{"auth", "status", "--host", "https://github.com"}, "invalid host"},
		{"missing host value", []string{"auth", "status", "--host"}, "flag needs an argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runWith(t, env{git: &scriptedGit{}}, tt.args...)
			if r.code != exitUsage || r.stdout != "" || !strings.Contains(r.stderr, tt.part) || !strings.Contains(r.stderr, "usage: agent-code-review auth") {
				t.Errorf("got %+v, want usage error with %q", r, tt.part)
			}
		})
	}
}

func TestAuthHelp(t *testing.T) {
	for _, args := range [][]string{{"auth", "--help"}, {"auth", "-h"}, {"auth", "status", "--help"}} {
		r := runWith(t, env{git: &scriptedGit{}}, args...)
		if r.code != exitOK || !strings.Contains(r.stdout, "usage: agent-code-review auth") || r.stderr != "" {
			t.Errorf("%v: got %+v, want usage on stdout", args, r)
		}
	}
}

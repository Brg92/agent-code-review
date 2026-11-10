package auth

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// call is one recorded git invocation.
type call struct {
	stdin string
	args  []string
}

// fakeGit answers each git command line with a scripted result.
type fakeGit struct {
	calls   []call
	answers map[string]answer
}

type answer struct {
	stdout, stderr string
	err            error
}

func (f *fakeGit) Run(_ context.Context, stdin string, args ...string) (string, string, error) {
	f.calls = append(f.calls, call{stdin, args})
	a, ok := f.answers[strings.Join(args, " ")]
	if !ok {
		return "", "", fmt.Errorf("fakeGit: unexpected git %v", args)
	}
	return a.stdout, a.stderr, a.err
}

const fillInput = "protocol=https\nhost=github.com\nusername=acr-api\n\n"

func TestResolveFromCredentialHelper(t *testing.T) {
	git := &fakeGit{answers: map[string]answer{
		"credential fill": {stdout: "protocol=https\nhost=github.com\nusername=acr-api\npassword=" + testToken + "\n"},
	}}
	r := Resolver{Getenv: env(nil), Git: git}
	tok, err := r.Resolve(context.Background(), "github.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tok.Value() != testToken || tok.Source != SourceCredentialHelper {
		t.Errorf("Resolve = %v", tok)
	}
	if len(git.calls) != 1 || git.calls[0].stdin != fillInput {
		t.Errorf("calls = %q, want one fill with %q", git.calls, fillInput)
	}
}

func TestResolveOtherHostSkipsEnvironment(t *testing.T) {
	git := &fakeGit{answers: map[string]answer{
		"credential fill": {stdout: "username=acr-api\npassword=" + testToken + "\n"},
	}}
	r := Resolver{Getenv: env(map[string]string{"ACR_GITHUB_TOKEN": "env-token-value-9"}), Git: git}
	tok, err := r.Resolve(context.Background(), "ghe.example.invalid")
	if err != nil || tok.Value() != testToken {
		t.Fatalf("Resolve = %v, %v", tok, err)
	}
	if want := "protocol=https\nhost=ghe.example.invalid\nusername=acr-api\n\n"; git.calls[0].stdin != want {
		t.Errorf("stdin = %q, want %q", git.calls[0].stdin, want)
	}
}

func TestResolveNotFound(t *testing.T) {
	tests := map[string]answer{
		"git exits non-zero": {stderr: "fatal: could not read Password: terminal prompts disabled", err: &ExitError{Code: 128}},
		"no password":        {stdout: "protocol=https\nhost=github.com\nusername=acr-api\n"},
		"empty password":     {stdout: "username=acr-api\npassword=\n"},
		"other username":     {stdout: "username=someone\npassword=" + testToken + "\n"},
	}
	for name, a := range tests {
		t.Run(name, func(t *testing.T) {
			r := Resolver{Getenv: env(nil), Git: &fakeGit{answers: map[string]answer{"credential fill": a}}}
			tok, err := r.Resolve(context.Background(), "github.com")
			if !errors.Is(err, ErrNoToken) {
				t.Errorf("Resolve = %v, %v; want ErrNoToken", tok, err)
			}
			if err != nil && strings.Contains(err.Error(), testToken) {
				t.Errorf("error leaks the token: %v", err)
			}
		})
	}
}

func TestResolveGitMissing(t *testing.T) {
	r := Resolver{Getenv: env(nil), Git: &fakeGit{answers: map[string]answer{"credential fill": {err: exec.ErrNotFound}}}}
	_, err := r.Resolve(context.Background(), "github.com")
	if err == nil || errors.Is(err, ErrNoToken) || !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("Resolve = %v, want a git error", err)
	}
}

func TestParseCredential(t *testing.T) {
	got := parseCredential("protocol=https\r\nhost=h\nusername=u\npassword=p=q\nbogus\n\nignored=after-blank\n")
	want := map[string]string{"protocol": "https", "host": "h", "username": "u", "password": "p=q"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("parseCredential = %v, want %v", got, want)
	}
}

func TestExitError(t *testing.T) {
	if s := (&ExitError{Code: 128}).Error(); s != "git exited with status 128" {
		t.Errorf("Error() = %q", s)
	}
}

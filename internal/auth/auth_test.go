package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testToken = "test-token-value-0123"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestTokenNeverFormatsItsValue(t *testing.T) {
	tok := Token{value: testToken, Source: SourceEnvironment}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		if s := fmt.Sprintf(verb, tok); strings.Contains(s, testToken) || strings.Contains(s, fmt.Sprintf("%x", testToken)) {
			t.Errorf("Sprintf(%s) = %q leaks the token", verb, s)
		}
		if s := fmt.Sprintf(verb, &tok); strings.Contains(s, testToken) {
			t.Errorf("Sprintf(%s, &tok) = %q leaks the token", verb, s)
		}
	}
	if s := fmt.Sprintf("%v", struct{ T Token }{tok}); strings.Contains(s, testToken) {
		t.Errorf("nested Sprintf = %q leaks the token", s)
	}
	if tok.Value() != testToken {
		t.Errorf("Value() = %q", tok.Value())
	}
}

func TestEnvVar(t *testing.T) {
	tests := map[string]string{"github.com": "ACR_GITHUB_TOKEN", "GitHub.com": "ACR_GITHUB_TOKEN", "ghe.example.invalid": "", "gitlab.com": ""}
	for host, want := range tests {
		if got := EnvVar(host); got != want {
			t.Errorf("EnvVar(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestValidateHost(t *testing.T) {
	for _, ok := range []string{"github.com", "ghe.example.invalid", "git.example.invalid:8443", "localhost", "a1-b.c"} {
		if err := ValidateHost(ok); err != nil {
			t.Errorf("ValidateHost(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "github.com\nusername=x", "github.com/org", "https://github.com", "user@github.com", "-x.com", "x.com-", "x..com", "x.com:", "x.com:abc", "x.com:123456", "exa mple.com", "x.com\x00"} {
		if err := ValidateHost(bad); err == nil {
			t.Errorf("ValidateHost(%q) = nil, want error", bad)
		}
	}
}

func TestResolveFromEnvironment(t *testing.T) {
	r := Resolver{Getenv: env(map[string]string{"ACR_GITHUB_TOKEN": "  " + testToken + "\n"}), Git: failRunner(t)}
	tok, err := r.Resolve(context.Background(), "github.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tok.Value() != testToken || tok.Source != SourceEnvironment || tok.EnvVar != "ACR_GITHUB_TOKEN" {
		t.Errorf("Resolve = %+v (%s)", tok, tok.Source)
	}
}

func TestResolveRejectsInvalidHost(t *testing.T) {
	r := Resolver{Getenv: env(nil), Git: failRunner(t)}
	if _, err := r.Resolve(context.Background(), "bad host"); err == nil {
		t.Error("Resolve(bad host) = nil error")
	}
}

func TestSourceDescription(t *testing.T) {
	if s := (Token{Source: SourceEnvironment, EnvVar: "ACR_GITHUB_TOKEN"}).Describe(); s != "the environment variable ACR_GITHUB_TOKEN" {
		t.Errorf("Describe = %q", s)
	}
	if s := (Token{Source: SourceCredentialHelper}).Describe(); s != "the git credential helper (username acr-api)" {
		t.Errorf("Describe = %q", s)
	}
}

// failRunner fails the test if git is run.
func failRunner(t *testing.T) Runner {
	return RunnerFunc(func(context.Context, string, ...string) (string, string, error) {
		t.Helper()
		t.Error("git was run")
		return "", "", errors.New("unexpected")
	})
}

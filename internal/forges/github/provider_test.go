package github

import (
	"context"
	"errors"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

func TestNewReadsOnlyKnownHosts(t *testing.T) {
	tok := &tokens{token: canary}
	r, err := New("github.com", tok, Options{})
	if err != nil || r == nil {
		t.Fatalf("New(github.com) = %v, %v", r, err)
	}
	if r.Capabilities() != Capabilities() {
		t.Errorf("Capabilities() = %+v, want %+v", r.Capabilities(), Capabilities())
	}
	for _, host := range []string{"ghe.example.com", "github.com.evil.example", "api.github.com", ""} {
		r, err := New(host, tok, Options{})
		var ue *UnsupportedHostError
		if !errors.As(err, &ue) || r != nil {
			t.Errorf("New(%q) = %v, %v, want an UnsupportedHostError", host, r, err)
		}
	}
	if n := tok.asked.Load(); n != 0 {
		t.Errorf("the token was asked for %d times: an unsupported host must not get it", n)
	}
	if !SupportedHost("GitHub.com") || SupportedHost("gitlab.com") {
		t.Error("SupportedHost: github.com in any case yes, gitlab.com no")
	}
}

func TestNoTokenIsForbiddenWithItsCause(t *testing.T) {
	r := newTestProviderWith(t, &tokens{err: auth.ErrNoToken})
	_, err := r.ChangeRequest(context.Background(), key("sample-app", 2))
	if !errors.Is(err, base.ErrForbidden) || !errors.Is(err, auth.ErrNoToken) {
		t.Errorf("error = %v, want forbidden, with auth.ErrNoToken reachable for the exit code", err)
	}
}

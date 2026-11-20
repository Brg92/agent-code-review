package guards

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/forges/github"
)

// askedTokens is a token resolver that counts how often it is asked.
type askedTokens struct{ asked atomic.Int32 }

func (r *askedTokens) Resolve(context.Context, string) (auth.Token, error) {
	r.asked.Add(1)
	return auth.Token{}, errors.New("no token in this test")
}

// TestNewLoopbackRefusesAnyOtherServer: the one way to point the GitHub
// reader at another server may only name a process on this machine, and
// refuses everything else before the token is asked for.
func TestNewLoopbackRefusesAnyOtherServer(t *testing.T) {
	tests := []struct {
		name, host, base string
	}{
		{"the real API", "github.com", "https://api.github.com"},
		{"https on loopback", "github.com", "https://127.0.0.1:8443"},
		{"another host", "github.com", "http://example.com"},
		{"a name that starts like loopback", "github.com", "http://127.0.0.1.example.com"},
		{"a private address", "github.com", "http://10.0.0.1:8080"},
		{"localhost, which a hosts file may point elsewhere", "github.com", "http://localhost:8080"},
		{"user info", "github.com", "http://user:pw@127.0.0.1:8080"},
		{"no host", "github.com", "http://"},
		{"not a URL", "github.com", "://"},
		{"a host the provider does not read", "ghe.example.com", "http://127.0.0.1:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := &askedTokens{}
			if _, err := github.NewLoopback(tt.host, tt.base, tok, github.Options{}); err == nil {
				t.Errorf("NewLoopback(%q, %q) succeeded", tt.host, tt.base)
			}
			if tok.asked.Load() != 0 {
				t.Error("the token was asked for")
			}
		})
	}
	var ue *github.UnsupportedHostError
	if _, err := github.NewLoopback("ghe.example.com", "http://127.0.0.1:8080", nil, github.Options{}); !errors.As(err, &ue) {
		t.Errorf("error = %v, want an UnsupportedHostError", err)
	}
}

func TestNewLoopbackAcceptsLoopbackAddresses(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:8080", "http://[::1]:8080", "http://127.0.0.1"} {
		if _, err := github.NewLoopback("github.com", base, nil, github.Options{}); err != nil {
			t.Errorf("NewLoopback(%q): %v", base, err)
		}
	}
}

// TestNoProductionCodeCallsNewLoopback: only tests may point the reader at
// a local server; the command line reads GitHub and nothing else.
func TestNoProductionCodeCallsNewLoopback(t *testing.T) {
	var callers []string
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(repoRoot, path)
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && (rel == "internal/testutil" || rel == "internal/guards"):
				return filepath.SkipDir
			case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || rel == "internal/forges/github/loopback.go":
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(src), "NewLoopback") {
				callers = append(callers, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(callers) > 0 {
		t.Errorf("production code uses NewLoopback: %v", callers)
	}
}

package github

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/transport"
)

// canary is the token of the tests. No error, log or panic message may
// contain it.
const canary = "ghp_CANARYtoken0123456789abcdefABCDEF"

// tokens is a Resolver that hands out the canary and counts the asks.
type tokens struct {
	asked atomic.Int32
	token string
	err   error
}

func (r *tokens) Resolve(_ context.Context, _ string) (auth.Token, error) {
	r.asked.Add(1)
	if r.err != nil {
		return auth.Token{}, r.err
	}
	return auth.Resolver{Getenv: func(string) string { return r.token }}.Resolve(context.Background(), "github.com")
}

// clock records sleeps and advances a fixed time instead of waiting.
type clock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *clock) Now() time.Time { return c.now }
func (c *clock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return ctx.Err()
}

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// testOptions are the options of the tests: a fixed clock, no waiting.
func testOptions(c *clock) Options {
	return Options{Transport: transport.Config{Now: c.Now, Sleep: c.Sleep}}
}

// onReplay returns a provider that reads the recordings of scenario.
func onReplay(t *testing.T, scenario string) (*Provider, *sandbox.Replay, *tokens) {
	t.Helper()
	r := sandbox.NewReplay(t, scenario)
	// The repository endpoint is not in the recordings: this is the one
	// synthetic response the recorded scenarios need, for the default branch.
	for _, repo := range []string{"sample-app", "core-lib", "kmp-shared", "feature-lib"} {
		r.Add(sandbox.Route{Path: "/repos/" + sandbox.Owner + "/" + repo, Status: 200, Body: `{"default_branch":"main"}`})
	}
	tok := &tokens{token: canary}
	p, err := newProvider("github.com", r.URL(), tok, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	return p, r, tok
}

// key returns the key of pull request number of repo in the sandbox.
func key(repo string, number int) core.Key {
	return core.Key{Forge: core.GitHub, Host: "github.com", Project: sandbox.Owner + "/" + repo, Number: number}
}

// newTestProviderWith returns a provider on the S1 replay with the given
// token resolver.
func newTestProviderWith(t *testing.T, tok Resolver) *Provider {
	t.Helper()
	r := sandbox.NewReplay(t, "S1")
	p, err := newProvider("github.com", r.URL(), tok, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// inProject returns k in another project.
func inProject(k core.Key, project string) core.Key {
	k.Project = project
	return k
}

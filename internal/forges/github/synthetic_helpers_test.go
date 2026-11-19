package github

import (
	"encoding/base64"
	"testing"

	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// synthetic is a provider on a replay with no recordings: every response
// is made up by the test, for what the sandbox cannot produce.
type synthetic struct {
	*Provider
	Replay *sandbox.Replay
	Tokens *tokens
	Clock  *clock
}

// onSynthetic returns a synthetic provider with options set by mod, which
// may be nil.
func onSynthetic(t *testing.T, mod func(*Options)) *synthetic {
	t.Helper()
	s := &synthetic{Replay: sandbox.NewSynthetic(t), Tokens: &tokens{token: canary}, Clock: &clock{now: epoch}}
	opts := testOptions(s.Clock)
	if mod != nil {
		mod(&opts)
	}
	p, err := newProvider("github.com", s.Replay.URL(), s.Tokens, opts)
	if err != nil {
		t.Fatal(err)
	}
	s.Provider = p
	return s
}

// route adds a response for GET path with query.
func (s *synthetic) route(status int, path string, query map[string]string, header map[string]string, body string) {
	s.Replay.Add(sandbox.Route{Path: path, Query: query, Status: status, Header: header, Body: body})
}

// repo is the path of a synthetic repository.
const repo = "/repos/acme/app"

// synthKey is a change request of the synthetic repository.
var synthKey = inProject(key("app", 7), "acme/app")

func encodeBase64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

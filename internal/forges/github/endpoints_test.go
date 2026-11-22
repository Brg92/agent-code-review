package github

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/transport"
)

// TestWriteEndpointsAreExactlyThoseOfTheWriter names every request besides
// a GET the provider may send. Adding one means changing this test on
// purpose; none may merge, approve, close, resolve, push or delete.
func TestWriteEndpointsAreExactlyThoseOfTheWriter(t *testing.T) {
	want := []string{
		"POST /repos/*/*/pulls/*/reviews",
		"POST /repos/*/*/issues/*/comments",
		"POST /repos/*/*/issues",
		"PATCH /repos/*/*/issues/*",
		"POST /repos/*/*/labels",
		"PATCH /repos/*/*/pulls/*",
		"POST /graphql (query only)",
	}
	var got []string
	for _, e := range writeEndpoints {
		s := e.Method + " " + e.Path
		if e.ReadOnlyQuery {
			s += " (query only)"
		}
		got = append(got, s)
		for _, bad := range []string{"merge", "approve", "close", "resolve", "events", "dismiss", "git/", "branches", "refs", "hooks", "lock"} {
			if strings.Contains(e.Path, bad) {
				t.Errorf("the endpoint %s is about %s", s, bad)
			}
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("write endpoints = %v, want %v", got, want)
	}
	if _, err := transport.New(transport.Config{BaseURL: "https://api.github.com", Allow: writeEndpoints}); err != nil {
		t.Errorf("the transport refuses them: %v", err)
	}
}

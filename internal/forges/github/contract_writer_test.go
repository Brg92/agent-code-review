package github

import (
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
)

// TestWriterContract runs the contract of the writes on the provider,
// through the transport, on the synthetic GitHub: there is no recording of a
// write, and none ever reaches the sandbox from a test.
func TestWriterContract(t *testing.T) {
	srv := ghserver.New(t)
	srv.Token = canary
	app := srv.AddRepo("acme/app")
	app.AddPR(7, "author-login", writeHead).Lines = map[string]map[string][]int{"app/A.kt": {"RIGHT": {1, 2, 3}}}
	for _, name := range []string{"acme/locked", "acme/busy", "acme/flaky"} {
		srv.AddRepo(name).AddPR(map[string]int{"acme/locked": 1, "acme/busy": 3, "acme/flaky": 2}[name], "author-login", writeHead)
	}
	srv.Fail("POST", `^/repos/acme/locked/`, ghserver.Fault{Status: 403, Body: `{"message":"Resource not accessible by personal access token"}`,
		Header: map[string]string{"X-Accepted-GitHub-Permissions": "issues=write"}})
	srv.Fail("POST", `^/repos/acme/busy/`, ghserver.Fault{Status: 403, Body: `{"message":"API rate limit exceeded"}`,
		Header: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1099511627776"}})
	srv.Fail("POST", `^/repos/acme/flaky/`, ghserver.Fault{Status: 500, Body: `{}`})

	p, err := newProvider("github.com", srv.URL(), &tokens{token: canary}, contractOptions())
	if err != nil {
		t.Fatal(err)
	}
	key := func(project string, n int) core.Key {
		return core.Key{Forge: core.GitHub, Host: "github.com", Project: project, Number: n}
	}
	forgecontract.RunWriter(t, p, forgecontract.WriterFixture{
		ChangeRequest: key("acme/app", 7),
		Head:          writeHead,
		Project:       key("acme/app", 7),
		Label:         "contract-review",
		Login:         "token-login",
		Refused:       base.ReviewComment{Path: "app/A.kt", Side: core.Right, Line: 99},
		Missing:       key("acme/app", 9),
		NoProject:     key("acme/nowhere", 1),
		Forbidden:     key("acme/locked", 1),
		Busy:          key("acme/busy", 3),
		Broken:        key("acme/flaky", 2),
	})
	for _, w := range srv.Writes() {
		if w.Path == "" {
			t.Error("a write without a path")
		}
	}
}

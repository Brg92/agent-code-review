package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

const otherHead = "1111111111111111111111111111111111111111"

// queueRoute is the queue the recorder saved, served for the query the
// command asks.
func queueRoute(t *testing.T, limit string) sandbox.Route {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "github", "queue", "*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("recorded queue: %v, %v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Body json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return sandbox.Route{Path: "/search/issues", Status: 200, Body: string(rec.Body), Query: map[string]string{
		"q": "is:pr is:open review-requested:@me archived:false", "sort": "updated", "order": "desc", "per_page": limit}}
}

// queueEnv is a review environment whose GitHub is the S1 replay with the
// queue added.
func queueEnv(t *testing.T) (env, string, *sandbox.Replay) {
	t.Helper()
	r := sandbox.NewReplay(t, "S1")
	r.Add(queueRoute(t, "30"))
	e, work := reviewEnv(t, r)
	return e, work, r
}

// saveLocalRun writes a run in the work directory whose root is
// acr-plugin-sandbox/sample-app#2 at head.
func saveLocalRun(t *testing.T, work, head string, phase state.Phase) {
	t.Helper()
	root, err := core.ParseKey("github:github.com/acr-plugin-sandbox/sample-app#2")
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.New("20261119T100000Z-00000001", root, core.DefaultLimits(), []core.Entry{{CR: root, Status: core.Finalized, HeadSHA: head}}, clockNow)
	if err != nil {
		t.Fatal(err)
	}
	st.Phase = phase
	dir, err := state.CreateRunDir(work, st.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(dir, st); err != nil {
		t.Fatal(err)
	}
}

func TestQueueListsTheReviewRequests(t *testing.T) {
	e, _, r := queueEnv(t)
	got := runWith(t, e, "queue")
	if got.code != exitOK || got.stderr != "" {
		t.Fatalf("queue: code %d\nstderr: %s", got.code, got.stderr)
	}
	lines := strings.Split(strings.TrimSuffix(got.stdout, "\n"), "\n")
	if len(lines) != 1+1+13+1+2 {
		t.Fatalf("%d lines:\n%s", len(lines), got.stdout)
	}
	want := "acr-plugin-sandbox/sample-app#7 · <<<UNTRUSTED-"
	if !strings.HasPrefix(lines[2], want) || !strings.Contains(lines[2], " Show when the session expires UNTRUSTED-") ||
		!strings.Contains(lines[2], " · by sandbox-author · ready · updated 2026-11-02T18:23:44Z · local: not reviewed") {
		t.Errorf("first line = %q", lines[2])
	}
	if lines[len(lines)-1] != "Next: agent-code-review review <cr>" {
		t.Errorf("last line = %q", lines[len(lines)-1])
	}
	noCanary(t, "stdout", got.stdout)
	for _, req := range r.Requests() {
		if !strings.HasPrefix(req, "GET ") {
			t.Errorf("request %q is not a read", req)
		}
	}
	if n := r.Count("GET", "/search/issues"); n != 1 || len(r.Requests()) != 1 {
		t.Errorf("requests = %v, want the one search: no change request has a local review", r.Requests())
	}
}

func TestQueueShowsTheLocalReviewState(t *testing.T) {
	tests := []struct {
		name  string
		head  string
		phase state.Phase
		want  string
	}{
		{"same head", s1Head, state.Finalized, "local: reviewed at 76ae5f13 (finalized)\n"},
		{"head changed", otherHead, state.Collected, "local: reviewed at 11111111 (collected), head changed\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, work, r := queueEnv(t)
			saveLocalRun(t, work, tt.head, tt.phase)
			got := runWith(t, e, "queue")
			if got.code != exitOK {
				t.Fatalf("queue: code %d\n%s", got.code, got.stderr)
			}
			var line string
			for _, l := range strings.Split(got.stdout, "\n") {
				if strings.HasPrefix(l, "acr-plugin-sandbox/sample-app#2 ") {
					line = l + "\n"
				}
			}
			if !strings.HasSuffix(line, tt.want) {
				t.Errorf("line = %q, want it to end with %q", line, tt.want)
			}
			if n := r.Count("GET", "/repos/acr-plugin-sandbox/sample-app/pulls/2"); n != 1 {
				t.Errorf("the head was read %d times, want once", n)
			}
		})
	}
}

func TestQueueSaysWhenTheHeadCannotBeChecked(t *testing.T) {
	r := sandbox.NewSynthetic(t)
	r.Add(queueRoute(t, "30"))
	r.Add(sandbox.Route{Path: "/repos/acr-plugin-sandbox/sample-app/pulls/2", Status: 500, Body: `{"message":"boom"}`})
	e, work := reviewEnv(t, r)
	saveLocalRun(t, work, s1Head, state.Finalized)
	got := runWith(t, e, "queue")
	if got.code != exitOK || !strings.Contains(got.stdout, "local: reviewed at 76ae5f13 (finalized), head not checked\n") {
		t.Errorf("queue: code %d\n%s%s", got.code, got.stdout, got.stderr)
	}
}

func TestQueueLimit(t *testing.T) {
	r := sandbox.NewSynthetic(t)
	r.Add(queueRoute(t, "5"))
	e, _ := reviewEnv(t, r)
	got := runWith(t, e, "queue", "--limit", "5")
	if got.code != exitOK {
		t.Fatalf("queue --limit 5: code %d\n%s", got.code, got.stderr)
	}
	if n := strings.Count(got.stdout, " · local: "); n != 5 {
		t.Errorf("%d change requests, want 5:\n%s", n, got.stdout)
	}
	if !strings.Contains(got.stdout, "5 of 13 pull requests") {
		t.Errorf("the output does not say there are more:\n%s", got.stdout)
	}
}

func TestQueueUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"zero", []string{"queue", "--limit", "0"}},
		{"negative", []string{"queue", "--limit", "-3"}},
		{"above the maximum", []string{"queue", "--limit", "101"}},
		{"not a number", []string{"queue", "--limit", "many"}},
		{"no value", []string{"queue", "--limit"}},
		{"unknown flag", []string{"queue", "--all"}},
		{"argument", []string{"queue", "acme/app#1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, r := queueEnv(t)
			got := runWith(t, e, tt.args...)
			if got.code != exitUsage || got.stdout != "" || !strings.Contains(got.stderr, "usage: agent-code-review queue") {
				t.Errorf("code %d\nstdout: %s\nstderr: %s", got.code, got.stdout, got.stderr)
			}
			if len(r.Requests()) != 0 {
				t.Errorf("requests = %v, want none", r.Requests())
			}
		})
	}
}

func TestQueueMaximumLimitIsAccepted(t *testing.T) {
	r := sandbox.NewSynthetic(t)
	r.Add(queueRoute(t, "100"))
	e, _ := reviewEnv(t, r)
	if got := runWith(t, e, "queue", "--limit", "100"); got.code != exitOK {
		t.Errorf("queue --limit 100: code %d\n%s", got.code, got.stderr)
	}
}

func TestQueueErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   int
	}{
		{"refused token", 401, exitAuth},
		{"missing permission", 403, exitAuth},
		{"server error", 500, exitPlatform},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := sandbox.NewSynthetic(t)
			route := queueRoute(t, "30")
			route.Status, route.Body = tt.status, `{"message":"no"}`
			r.Add(route)
			e, _ := reviewEnv(t, r)
			got := runWith(t, e, "queue")
			if got.code != tt.want || got.stdout != "" || !strings.HasPrefix(got.stderr, "agent-code-review: ") {
				t.Errorf("code %d, want %d\nstdout: %s\nstderr: %s", got.code, tt.want, got.stdout, got.stderr)
			}
			noCanary(t, "stderr", got.stderr)
		})
	}
}

func TestQueueFailsWhenTheWorkDirectoryCannotBeRead(t *testing.T) {
	e, work, _ := queueEnv(t)
	if err := os.WriteFile(filepath.Join(work, "runs"), []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runWith(t, e, "queue")
	if got.code != exitUnexpected || !strings.Contains(got.stderr, "could not list the runs") {
		t.Errorf("code %d\n%s", got.code, got.stderr)
	}
}

func TestQueueNeedsAPlatformThatHasOne(t *testing.T) {
	r := sandbox.NewSynthetic(t)
	e, _ := reviewEnv(t, r)
	e.newReader = func(string, github.Resolver, github.Options) (base.ForgeProvider, error) {
		return &forgecontract.Fake{}, nil
	}
	got := runWith(t, e, "queue")
	if got.code != exitUnexpected || !strings.Contains(got.stderr, "review queue") {
		t.Errorf("code %d\n%s", got.code, got.stderr)
	}
}

func TestQueueIsListedInTheUsage(t *testing.T) {
	if !strings.Contains(usage, "queue [--limit N]") {
		t.Error("the usage does not list queue")
	}
}

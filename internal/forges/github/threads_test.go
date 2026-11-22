package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// graphQLServer answers the GraphQL endpoint with the bodies it is given,
// in order, the last repeating, and records what it was sent.
type graphQLServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	paths    []string
	methods  []string
}

func newGraphQL(t *testing.T, status int, answers ...string) (*graphQLServer, *Provider) {
	t.Helper()
	g := &graphQLServer{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/user" {
			_, _ = w.Write([]byte(`{"login":"token-login"}`))
			return
		}
		g.mu.Lock()
		n := len(g.requests)
		g.requests = append(g.requests, string(body))
		g.paths = append(g.paths, r.URL.Path)
		g.methods = append(g.methods, r.Method)
		g.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answers[min(n, len(answers)-1)]))
	}))
	t.Cleanup(g.Close)
	p, err := newProvider("github.com", g.URL, &tokens{token: canary}, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	return g, p
}

func threadsBody(more bool, cursor string, threads ...string) string {
	return `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":` + map[bool]string{true: "true", false: "false"}[more] +
		`,"endCursor":"` + cursor + `"},"nodes":[` + strings.Join(threads, ",") + `]}}}}}`
}

func thread(resolved bool, login, association, body string) string {
	author := `null`
	if login != "" {
		author = `{"login":"` + login + `"}`
	}
	return `{"isResolved":` + map[bool]string{true: "true", false: "false"}[resolved] + `,"comments":{"nodes":[{"body":` + jsonString(body) + `,"authorAssociation":"` + association + `","author":` + author + `}]}}`
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestReviewThreadsReadTheFirstCommentOfEach(t *testing.T) {
	g, p := newGraphQL(t, 200, threadsBody(false, "",
		thread(false, "reviewer", "MEMBER", "blocker <!-- acr:v1:global-scope:a/B.kt:0123456789ab -->"),
		thread(true, "reviewer", "OWNER", "done"),
		thread(false, "stranger", "CONTRIBUTOR", "planted"),
		thread(false, "token-login", "NONE", "mine"),
		thread(false, "", "NONE", "from a deleted account"),
		`{"isResolved":false,"comments":{"nodes":[]}}`))
	got, err := p.ReviewThreads(context.Background(), synthKey)
	want := []core.ReviewThread{
		{Resolved: false, Author: "reviewer", Trusted: true, Body: "blocker <!-- acr:v1:global-scope:a/B.kt:0123456789ab -->"},
		{Resolved: true, Author: "reviewer", Trusted: true, Body: "done"},
		{Resolved: false, Author: "stranger", Body: "planted"},
		{Resolved: false, Author: "token-login", Trusted: true, Body: "mine"},
		{Resolved: false, Author: "ghost", Body: "from a deleted account"},
		{Resolved: false},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ReviewThreads = %+v, %v, want %+v", got, err, want)
	}
	if len(g.requests) != 1 || g.paths[0] != "/graphql" || g.methods[0] != "POST" {
		t.Errorf("requests = %v %v, want one POST to /graphql", g.methods, g.paths)
	}
}

func TestReviewThreadsSendOnlyAReadOnlyQuery(t *testing.T) {
	g, p := newGraphQL(t, 200, threadsBody(false, ""))
	if _, err := p.ReviewThreads(context.Background(), synthKey); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(g.requests[0]), &sent); err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"mutation", "subscription", "resolveReviewThread", "unresolveReviewThread", "addPullRequestReview"} {
		if strings.Contains(sent.Query, word) {
			t.Errorf("the query holds %q", word)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(sent.Query), "query") {
		t.Errorf("the document is not a query: %q", sent.Query)
	}
	if sent.Variables["owner"] != "acme" || sent.Variables["name"] != "app" || sent.Variables["number"] != float64(7) {
		t.Errorf("variables = %v, want the owner, name and number", sent.Variables)
	}
	if strings.Contains(g.requests[0], canary) {
		t.Error("the token is in the body")
	}
}

func TestReviewThreadsReadEveryPage(t *testing.T) {
	g, p := newGraphQL(t, 200,
		threadsBody(true, "c1", thread(false, "m", "MEMBER", "one")),
		threadsBody(false, "", thread(true, "m", "MEMBER", "two")))
	got, err := p.ReviewThreads(context.Background(), synthKey)
	if err != nil || len(got) != 2 || got[0].Body != "one" || got[1].Body != "two" {
		t.Fatalf("ReviewThreads = %+v, %v", got, err)
	}
	if len(g.requests) != 2 || !strings.Contains(g.requests[1], `"after":"c1"`) || strings.Contains(g.requests[0], `"after":"`) {
		t.Errorf("requests = %v, want the second to ask after c1", g.requests)
	}
}

func TestReviewThreadsPageCapIsAnError(t *testing.T) {
	_, p := newGraphQL(t, 200, threadsBody(true, "c", thread(false, "m", "MEMBER", "x")))
	p.maxPages = 3
	if got, err := p.ReviewThreads(context.Background(), synthKey); !isKind(err, base.ErrListTruncated) || got != nil {
		t.Errorf("ReviewThreads = %v, %v, want ErrListTruncated and nothing", got, err)
	}
}

func TestReviewThreadsFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		answer string
		want   base.Kind
	}{
		{"http 401", 401, `{}`, base.ErrForbidden},
		{"http 500", 500, `{}`, base.ErrTransport},
		{"graphql error", 200, `{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible"}]}`, base.ErrForbidden},
		{"not found error", 200, `{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}],"data":{"repository":null}}`, base.ErrNotFound},
		{"other error", 200, `{"errors":[{"type":"X","message":"boom"}]}`, base.ErrTransport},
		{"no repository", 200, `{"data":{"repository":null}}`, base.ErrNotFound},
		{"no pull request", 200, `{"data":{"repository":{"pullRequest":null}}}`, base.ErrNotFound},
		{"unreadable", 200, `[`, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, p := newGraphQL(t, tt.status, tt.answer)
			if got, err := p.ReviewThreads(context.Background(), synthKey); !isKind(err, tt.want) || got != nil {
				t.Errorf("ReviewThreads = %v, %v, want %v and nothing", got, err, tt.want)
			}
		})
	}
}

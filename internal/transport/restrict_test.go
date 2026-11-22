package transport

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type recordingTripper struct{ called bool }

func (r *recordingTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.called = true
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func request(t *testing.T, method, url, body string) *http.Request {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestRestrictRefusesWritesWithoutEndpoints(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace, "get", "PROPFIND"}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			next := &recordingTripper{}
			_, err := (restrict{next: next}).RoundTrip(request(t, m, "https://example.invalid/x", "body"))
			var na *NotAllowedError
			if !errors.Is(err, ErrNotAllowed) || !errors.As(err, &na) || na.Method != m || na.Path != "/x" {
				t.Errorf("RoundTrip(%s) error = %v, want a NotAllowedError for %s /x", m, err, m)
			}
			if next.called {
				t.Errorf("RoundTrip(%s) reached the network", m)
			}
		})
	}
}

func TestRestrictPassesGet(t *testing.T) {
	next := &recordingTripper{}
	if _, err := (restrict{next: next}).RoundTrip(request(t, http.MethodGet, "https://example.invalid/x", "")); err != nil || !next.called {
		t.Errorf("RoundTrip(GET) = %v, called %v", err, next.called)
	}
}

func TestRestrictAllowsOnlyTheEndpointsItWasGiven(t *testing.T) {
	allow := []Endpoint{
		{Method: http.MethodPost, Path: "/repos/*/*/pulls/*/reviews"},
		{Method: http.MethodPatch, Path: "/repos/*/*/issues/*"},
		{Method: http.MethodPost, Path: "/repos/*/*/issues"},
	}
	tests := []struct {
		method, path string
		ok           bool
	}{
		{"POST", "/repos/o/r/pulls/7/reviews", true},
		{"POST", "/repos/o/r/pulls/7/reviews?x=1", true},
		{"PATCH", "/repos/o/r/issues/3", true},
		{"POST", "/repos/o/r/issues", true},
		{"PATCH", "/repos/o/r/pulls/7/reviews", false},
		{"POST", "/repos/o/r/issues/3", false},
		{"POST", "/repos/o/r/pulls/7/reviews/9/events", false},
		{"POST", "/repos/o/pulls/7/reviews", false},
		{"POST", "/repos/o/r/pulls//reviews", false},
		{"POST", "/repos/o/r/pulls/7/reviews/", false},
		{"DELETE", "/repos/o/r/issues/3", false},
		{"PUT", "/repos/o/r/issues/3", false},
		{"POST", "/repos/o/r/merges", false},
		{"PUT", "/repos/o/r/pulls/7/merge", false},
		{"POST", "/graphql", false},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			next := &recordingTripper{}
			_, err := (restrict{next: next, allow: allow}).RoundTrip(request(t, tt.method, "https://example.invalid"+tt.path, "{}"))
			if tt.ok != (err == nil) || tt.ok != next.called {
				t.Errorf("RoundTrip = %v, reached the network %v, want allowed %v", err, next.called, tt.ok)
			}
		})
	}
}

func TestRestrictGraphQLIsReadOnly(t *testing.T) {
	allow := []Endpoint{{Method: http.MethodPost, Path: "/graphql", ReadOnlyQuery: true}}
	tests := []struct {
		name, body string
		ok         bool
	}{
		{"named query", `{"query":"query Threads($n: Int!) { repository { pullRequest(number: $n) { id } } }","variables":{"n":1}}`, true},
		{"anonymous query", `{"query":"{ viewer { login } }"}`, true},
		{"query with a comment", `{"query":"# a note\nquery { viewer { login } }"}`, true},
		{"a field that starts like a mutation", `{"query":"query { viewer { mutationsAllowed } }"}`, true},
		{"mutation", `{"query":"mutation { resolveReviewThread(input: {threadId: \"x\"}) { thread { id } } }"}`, false},
		{"named mutation", `{"query":"mutation Close($id: ID!) { closeIssue(input: {issueId: $id}) { clientMutationId } }"}`, false},
		{"mutation after a query", `{"query":"query A { viewer { login } }\nmutation B { addStar(input: {starrableId: \"x\"}) { clientMutationId } }"}`, false},
		{"subscription", `{"query":"subscription { x }"}`, false},
		{"mutation in uppercase is not a keyword, and a query is still not one", `{"query":"MUTATION { x }"}`, false},
		{"no query", `{"variables":{}}`, false},
		{"empty query", `{"query":"  "}`, false},
		{"not JSON", `query { viewer { login } }`, false},
		{"query is not a string", `{"query":{"a":1}}`, false},
		{"no body", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := &recordingTripper{}
			req := request(t, http.MethodPost, "https://example.invalid/graphql", tt.body)
			_, err := (restrict{next: next, allow: allow}).RoundTrip(req)
			if tt.ok != (err == nil) || tt.ok != next.called {
				t.Errorf("RoundTrip = %v, reached the network %v, want allowed %v", err, next.called, tt.ok)
			}
			if !tt.ok && !errors.Is(err, ErrNotAllowed) {
				t.Errorf("error = %v, want ErrNotAllowed", err)
			}
		})
	}
}

func TestRestrictLeavesTheBodyForTheNetwork(t *testing.T) {
	allow := []Endpoint{{Method: http.MethodPost, Path: "/graphql", ReadOnlyQuery: true}}
	var seen []byte
	next := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen, _ = io.ReadAll(r.Body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	body := `{"query":"{ viewer { login } }"}`
	if _, err := (restrict{next: next, allow: allow}).RoundTrip(request(t, http.MethodPost, "https://example.invalid/graphql", body)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(seen, []byte(body)) {
		t.Errorf("the network saw %q, want the body unchanged", seen)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewRefusesEndpointsThatAreNotPostOrPatch(t *testing.T) {
	for _, e := range []Endpoint{
		{Method: http.MethodDelete, Path: "/x"}, {Method: http.MethodPut, Path: "/x"}, {Method: http.MethodGet, Path: "/x"},
		{Method: "post", Path: "/x"}, {Method: http.MethodPost, Path: "x"}, {Method: http.MethodPost, Path: ""}, {Method: http.MethodPost, Path: "//x"},
		{Method: http.MethodPatch, Path: "/x", ReadOnlyQuery: true},
	} {
		if _, err := New(Config{BaseURL: "https://example.invalid", Allow: []Endpoint{e}}); err == nil {
			t.Errorf("New accepted the endpoint %+v", e)
		}
	}
	if _, err := New(Config{BaseURL: "https://example.invalid", Allow: []Endpoint{{Method: http.MethodPost, Path: "/graphql", ReadOnlyQuery: true}, {Method: http.MethodPatch, Path: "/a/*"}}}); err != nil {
		t.Errorf("New refused valid endpoints: %v", err)
	}
}

// TestClientMethods guards the API of the client: reads are Get and
// GetPages; the only writes are Post and Patch, which take the body as
// bytes, and no method takes an HTTP method.
func TestClientMethods(t *testing.T) {
	typ := reflect.TypeFor[*Client]()
	writes := map[string]bool{}
	for i := range typ.NumMethod() {
		m := typ.Method(i)
		switch {
		case strings.HasPrefix(m.Name, "Get"):
			for j := 1; j < m.Type.NumIn(); j++ {
				if in := m.Type.In(j); in == reflect.TypeFor[io.Reader]() || in == reflect.TypeFor[[]byte]() {
					t.Errorf("Client.%s takes a body", m.Name)
				}
			}
		case m.Name == "Post" || m.Name == "Patch":
			writes[m.Name] = true
		default:
			t.Errorf("Client.%s: exported methods are Get..., Post and Patch", m.Name)
		}
	}
	if len(writes) != 2 {
		t.Errorf("writes = %v, want Post and Patch", writes)
	}
}

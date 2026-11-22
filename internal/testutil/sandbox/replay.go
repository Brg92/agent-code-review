package sandbox

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// apiHost is the host the recorded Link headers point to.
const apiHost = "https://api.github.com"

// Route is a response added to a Replay by hand, for what no recording
// holds. It answers a GET of Path with exactly the query Query.
type Route struct {
	// Method is the HTTP method the route answers; GET when empty. A
	// route of another method answers any query.
	Method string
	Path   string
	Query  map[string]string
	Status int
	Header map[string]string
	Body   string
	// Then are the responses to the requests that follow, one each; the
	// last one repeats. Their Path and Query are ignored.
	Then []Route
}

// Replay is an HTTP server that answers with the recordings of one
// scenario. A request is answered by the recording with the same method,
// path and query, with its status, its ETag and Link headers and its body.
// A request that has no recording, or a route added with Add, fails the
// test and is answered with 501: a missing recording is never an answer
// such as a 404.
type Replay struct {
	t      testing.TB
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]Route
	hits   map[string]int
	log    []string
}

// NewReplay starts a Replay of the recordings of scenario, such as "S1".
// It is closed when the test ends.
func NewReplay(t testing.TB, scenario string) *Replay {
	t.Helper()
	r := &Replay{t: t, routes: map[string]Route{}, hits: map[string]int{}}
	for f, rec := range recordings(t, filepath.Join(scenario, "*.json")) {
		if rec.Method != "" && rec.Method != http.MethodGet {
			continue
		}
		route := Route{Path: rec.Path, Query: rec.Query, Status: rec.Status, Header: rec.Headers, Body: string(rec.Body)}
		key := routeKey(http.MethodGet, rec.Path, toValues(rec.Query))
		if _, dup := r.routes[key]; dup {
			t.Fatalf("%s: recorded twice in %s: %s", f, scenario, key)
		}
		r.routes[key] = route
	}
	if len(r.routes) == 0 {
		t.Fatalf("no recordings of scenario %s", scenario)
	}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

// NewSynthetic starts a Replay with no recordings, for the cases the sandbox
// cannot produce. Every response comes from Add, and each test that uses it
// is synthetic.
func NewSynthetic(t testing.TB) *Replay {
	t.Helper()
	r := &Replay{t: t, routes: map[string]Route{}, hits: map[string]int{}}
	r.srv = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

// URL is the base URL of the server, to give to the client in place of
// the API host.
func (r *Replay) URL() string { return r.srv.URL }

// Add answers a request that no recording holds. It is for synthetic
// responses: tests that use it say so.
func (r *Replay) Add(rt Route) {
	r.mu.Lock()
	defer r.mu.Unlock()
	method := rt.Method
	if method == "" {
		method = http.MethodGet
	}
	r.routes[routeKey(method, rt.Path, toValues(rt.Query))] = rt
}

// Requests returns the requests received, in order, as "METHOD path?query".
func (r *Replay) Requests() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

// Count returns how many requests with the method and path, whatever
// their query, were received.
func (r *Replay) Count(method, path string) int {
	n := 0
	for _, l := range r.Requests() {
		l, _, _ = strings.Cut(l, "?")
		if l == method+" "+path {
			n++
		}
	}
	return n
}

func (r *Replay) serve(w http.ResponseWriter, req *http.Request) {
	line := req.Method + " " + req.URL.Path
	if req.URL.RawQuery != "" {
		line += "?" + req.URL.RawQuery
	}
	r.mu.Lock()
	r.log = append(r.log, line)
	query := req.URL.Query()
	if req.Method != http.MethodGet {
		query = nil
	}
	key := routeKey(req.Method, req.URL.Path, query)
	route, ok := r.routes[key]
	if ok {
		if n := r.hits[key]; n > 0 && len(route.Then) > 0 {
			route = route.Then[min(n-1, len(route.Then)-1)]
		}
		r.hits[key]++
	}
	r.mu.Unlock()
	if !ok {
		r.t.Errorf("replay: no recording for %s", line)
		http.Error(w, "replay: no recording for this request", http.StatusNotImplemented)
		return
	}
	for k, v := range route.Header {
		if strings.EqualFold(k, "Link") {
			v = strings.ReplaceAll(v, apiHost, r.srv.URL)
		}
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(route.Status)
	_, _ = w.Write([]byte(route.Body))
}

func toValues(m map[string]string) url.Values {
	v := url.Values{}
	for k, s := range m {
		v.Set(k, s)
	}
	return v
}

// routeKey is the method, path and query of a request in a form that does
// not depend on the order of the query.
func routeKey(method, path string, q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(method + " " + path)
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			b.WriteString("&" + url.QueryEscape(k) + "=" + url.QueryEscape(v))
		}
	}
	return b.String()
}

package sandbox

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recTB keeps what a replay reports instead of failing the test.
type recTB struct {
	testing.TB
	errs []string
}

func (r *recTB) Helper() {}
func (r *recTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, strings.TrimSpace(sprintf(format, args...)))
}

func get(t *testing.T, url string, header ...string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestReplayServesRecordings(t *testing.T) {
	r := NewReplay(t, "S2")
	repo := "/repos/" + Owner + "/core-lib"

	resp, body := get(t, r.URL()+repo+"/pulls/3")
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == "" || !strings.Contains(body, `"number": 3`) && !strings.Contains(body, `"number":3`) {
		t.Errorf("pulls/3 = %d, ETag %q, body %.60q", resp.StatusCode, resp.Header.Get("ETag"), body)
	}
	head := PullRequest(t, "S2", "core-lib", 3).HeadSHA
	if !strings.Contains(body, head) {
		t.Errorf("pulls/3 body lacks the recorded head %s", head)
	}

	resp, body = get(t, r.URL()+repo+"/pulls/9999")
	if resp.StatusCode != 404 || !strings.Contains(body, "Not Found") {
		t.Errorf("pulls/9999 = %d %.60q, want the recorded 404", resp.StatusCode, body)
	}

	resp, body = get(t, r.URL()+"/search/code?per_page=100&q=legacyToken+repo%3A"+Owner+"%2Fsample-app")
	if resp.StatusCode != 200 || !strings.Contains(body, "incomplete_results") {
		t.Errorf("search = %d %.60q", resp.StatusCode, body)
	}

	got := r.Requests()
	if len(got) != 3 || got[0] != "GET "+repo+"/pulls/3" || !strings.HasPrefix(got[2], "GET /search/code?") {
		t.Errorf("Requests() = %v", got)
	}
	if r.Count("GET", repo+"/pulls/3") != 1 || r.Count("GET", repo+"/pulls/4") != 0 {
		t.Errorf("Count: %d, %d", r.Count("GET", repo+"/pulls/3"), r.Count("GET", repo+"/pulls/4"))
	}
}

func TestReplayNeverInventsAnAnswer(t *testing.T) {
	rec := &recTB{TB: t}
	r := NewReplay(rec, "S2")
	repo := "/repos/" + Owner + "/core-lib"
	tests := []struct{ name, method, path string }{
		{"other path", "GET", repo + "/pulls/12345"},
		{"other query", "GET", repo + "/pulls/3?page=2"},
		{"other scenario", "GET", "/repos/" + Owner + "/sample-app/pulls/8"},
		{"other method", "POST", repo + "/pulls/3"},
	}
	for _, tt := range tests {
		req, _ := http.NewRequest(tt.method, r.URL()+tt.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 404 || resp.StatusCode == 200 {
			t.Errorf("%s: status %d: a missing recording must not look like an answer", tt.name, resp.StatusCode)
		}
	}
	if len(rec.errs) != len(tests) {
		t.Fatalf("%d errors reported, want %d: %v", len(rec.errs), len(tests), rec.errs)
	}
	for i, tt := range tests {
		if !strings.Contains(rec.errs[i], tt.method+" "+tt.path) {
			t.Errorf("error %q does not name the request %s %s", rec.errs[i], tt.method, tt.path)
		}
	}
}

func TestReplayAddedRoutes(t *testing.T) {
	r := NewReplay(t, "S1")
	r.Add(Route{Path: "/repos/x/y", Status: 200, Header: map[string]string{"Link": `<https://api.github.com/repos/x/y?page=2>; rel="next"`}, Body: `{"ok":true}`})
	resp, body := get(t, r.URL()+"/repos/x/y")
	if resp.StatusCode != 200 || body != `{"ok":true}` {
		t.Errorf("added route = %d %q", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Link"); got != `<`+r.URL()+`/repos/x/y?page=2>; rel="next"` {
		t.Errorf("Link = %q, want the platform host replaced by the replay server", got)
	}
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func TestSyntheticReplayHasOnlyItsRoutes(t *testing.T) {
	rec := &recTB{TB: t}
	r := NewSynthetic(rec)
	r.Add(Route{Path: "/repos/x/y", Query: map[string]string{"a": "1"}, Status: 403, Header: map[string]string{"X-Test": "yes"}, Body: `{"message":"no"}`})
	resp, body := get(t, r.URL()+"/repos/x/y?a=1")
	if resp.StatusCode != 403 || resp.Header.Get("X-Test") != "yes" || body != `{"message":"no"}` {
		t.Errorf("synthetic route = %d %v %q", resp.StatusCode, resp.Header, body)
	}
	if resp, _ := get(t, r.URL()+"/repos/x/y"); resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("a query that was not added = %d, want 501", resp.StatusCode)
	}
	if len(rec.errs) != 1 {
		t.Errorf("errors = %v, want one for the request without a route", rec.errs)
	}
}

func TestSyntheticRouteSequence(t *testing.T) {
	r := NewSynthetic(t)
	r.Add(Route{Path: "/x", Status: 503, Then: []Route{{Status: 429, Header: map[string]string{"Retry-After": "1"}}, {Status: 200, Body: "ok"}}})
	var got []int
	for range 5 {
		resp, _ := get(t, r.URL()+"/x")
		got = append(got, resp.StatusCode)
	}
	if want := []int{503, 429, 200, 200, 200}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("statuses = %v, want %v: each request takes the next response and the last one repeats", got, want)
	}
}

func TestSyntheticPostRoute(t *testing.T) {
	r := NewSynthetic(t)
	r.Add(Route{Method: http.MethodPost, Path: "/graphql", Status: 200, Body: `{"data":{}}`})
	resp, err := http.Post(r.URL()+"/graphql", "application/json", strings.NewReader(`{"query":"{ x }"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != `{"data":{}}` {
		t.Errorf("POST /graphql = %d %q", resp.StatusCode, body)
	}
	if n := r.Count("POST", "/graphql"); n != 1 {
		t.Errorf("Count = %d, want 1", n)
	}
	// A GET of the same path is a different route, and has no answer.
	rec := &recTB{TB: t}
	r.t = rec
	if resp, _ := get(t, r.URL()+"/graphql"); resp.StatusCode != http.StatusNotImplemented || len(rec.errs) != 1 {
		t.Errorf("GET /graphql = %d, errors %v, want 501 and one error", resp.StatusCode, rec.errs)
	}
}

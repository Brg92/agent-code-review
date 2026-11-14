package ghrecord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "test-token-value"

// fakeAPI serves a tiny GitHub REST API and remembers every request.
type fakeAPI struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	seen   []string
	routes map[string]string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	api := &fakeAPI{t: t, routes: map[string]string{
		"/repos/o/r/pulls/1": `{"number":1,"user":{"login":"real-author","id":42},` +
			`"head":{"ref":"feat/x","sha":"h1"},"base":{"ref":"main","sha":"b1"}}`,
		"/repos/o/r/pulls/1/files?per_page=100": `[{"filename":"A.kt","status":"added","patch":"@@"}]`,
		"/repos/o/r/pulls/1/files?page=2&per_page=100": `[{"filename":"B.kt","status":"modified","patch":"@@"},` +
			`{"filename":"logo.png","status":"added"}]`,
		"/repos/o/r/contents/A.kt?ref=h1":                             `{"type":"file","content":"YQ=="}`,
		"/repos/o/r/contents/B.kt?ref=h1":                             `{"type":"file","content":"Yg=="}`,
		"/repos/o/r/contents/B.kt?ref=b1":                             `{"type":"file","content":"YQ=="}`,
		"/repos/o/r/issues/9":                                         `{"number":9,"title":"Tracking"}`,
		"/repos/o/r/pulls/1/commits?per_page=100":                     `[]`,
		"/repos/o/r/pulls/1/reviews?per_page=100":                     `[]`,
		"/repos/o/r/pulls/1/comments?per_page=100":                    `[]`,
		"/repos/o/r/issues/1/comments?per_page=100":                   `[{"user":{"login":"RealReviewer","id":7},"body":"ok"}]`,
		"/repos/o/r/commits/h1/check-runs?per_page=100":               `{"total_count":0,"check_runs":[]}`,
		"/repos/o/r/commits/h1/status":                                `{"state":"pending","statuses":[]}`,
		"/search/code?per_page=100&q=legacyToken+repo%3Ao%2Fr":        `{"total_count":2,"incomplete_results":false,"items":[{"path":"src/A.kt","repository":{"full_name":"o/r"}},{"path":"src/B.kt","repository":{"full_name":"o/r"}}]}`,
		"/search/code?per_page=100&q=gone+repo%3Ao%2Fr":               `{"total_count":0,"incomplete_results":true,"items":[]}`,
		"/repos/o/r/contents/src/A.kt?ref=main":                       `{"type":"file","content":"YQ=="}`,
		"/repos/o/r/contents/src/B.kt?ref=main":                       `{"type":"file","content":"Yg=="}`,
		"/repos/o/r/git/trees/h1?recursive=1":                         `{"sha":"h1","tree":[{"path":"A.kt","type":"blob"}],"truncated":true}`,
		"/repos/o/r/contents/.agent-code-review/config.json?ref=main": `{"type":"file","content":"e30="}`,
	}}
	api.server = httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(api.server.Close)
	return api
}

func (a *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.Query().Encode()
	}
	a.mu.Lock()
	a.seen = append(a.seen, r.Method+" "+key)
	a.mu.Unlock()
	if r.Method != http.MethodGet {
		a.t.Errorf("%s %s: only GET is allowed", r.Method, key)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		a.t.Errorf("%s: Authorization = %q", key, got)
	}
	if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		a.t.Errorf("%s: X-GitHub-Api-Version = %q", key, got)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", `"by real-author"`)
	w.Header().Set("X-RateLimit-Remaining", "4999")
	if key == "/repos/o/r/pulls/1/files?per_page=100" {
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/pulls/1/files?per_page=100&page=2>; rel="next"`, a.server.URL))
	}
	body, ok := a.routes[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		body = `{"message":"Not Found"}`
	}
	fmt.Fprint(w, body)
}

func newTestClient(t *testing.T, api *fakeAPI) *Client {
	t.Helper()
	return &Client{
		BaseURL:   api.server.URL,
		Token:     testToken,
		HTTP:      api.server.Client(),
		Sanitizer: testSanitizer(t),
		Dir:       t.TempDir(),
	}
}

func readRecordings(t *testing.T, dir string) map[string]Recording {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	recs := map[string]Recording{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{testToken, "real-author", "RealReviewer", "\r"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("%s contains %q", e.Name(), forbidden)
			}
		}
		var rec Recording
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if want := FileName(rec.Method, rec.Path, rec.Query); e.Name() != want {
			t.Errorf("file %s holds a recording named %s", e.Name(), want)
		}
		recs[rec.Path+"?"+fmt.Sprint(rec.Query)] = rec
	}
	return recs
}

func TestRecordPullRequest(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	if err := c.Record(Target{Owner: "o", Repo: "r", Number: 1}); err != nil {
		t.Fatal(err)
	}
	recs := readRecordings(t, c.Dir)
	if len(recs) != len(api.seen) {
		t.Errorf("%d recordings for %d requests: %v", len(recs), len(api.seen), api.seen)
	}
	page2, ok := recs["/repos/o/r/pulls/1/files?"+fmt.Sprint(map[string]string{"page": "2", "per_page": "100"})]
	if !ok {
		t.Fatalf("second page of files not recorded; requests: %v", api.seen)
	}
	if page2.Status != 200 || page2.Headers["ETag"] != `"by sandbox-author"` {
		t.Errorf("page 2 = status %d, headers %v", page2.Status, page2.Headers)
	}
	if _, ok := page2.Headers["X-Ratelimit-Remaining"]; ok {
		t.Errorf("page 2 keeps unlisted headers: %v", page2.Headers)
	}
	missing, ok := recs["/repos/o/r/contents/.agent-code-review/criteria.md?"+fmt.Sprint(map[string]string{"ref": "main"})]
	if !ok || missing.Status != 404 {
		t.Errorf("missing criteria file: recorded %v, status %d", ok, missing.Status)
	}
	for _, unwanted := range []string{"GET /repos/o/r/contents/logo.png?ref=h1", "GET /repos/o/r/contents/A.kt?ref=b1"} {
		for _, s := range api.seen {
			if s == unwanted {
				t.Errorf("unexpected request %s", s)
			}
		}
	}
}

func TestRecordTreeAtHead(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	if err := c.Record(Target{Owner: "o", Repo: "r", Number: 1}); err != nil {
		t.Fatal(err)
	}
	tree, ok := readRecordings(t, c.Dir)["/repos/o/r/git/trees/h1?"+fmt.Sprint(map[string]string{"recursive": "1"})]
	if !ok || tree.Status != 200 {
		t.Fatalf("tree at head not recorded: %v", ok)
	}
	var body struct {
		Truncated bool
		Tree      []struct{ Path string }
	}
	if err := json.Unmarshal(tree.Body, &body); err != nil || !body.Truncated || len(body.Tree) != 1 {
		t.Errorf("tree body %s: the truncated flag is not kept as returned (%v)", tree.Body, err)
	}
	for _, s := range api.seen {
		if strings.Contains(s, "/git/trees/b1") {
			t.Errorf("tree at base requested: %s", s)
		}
	}
}

func TestRecordFailsWithoutTree(t *testing.T) {
	api := newFakeAPI(t)
	delete(api.routes, "/repos/o/r/git/trees/h1?recursive=1")
	c := newTestClient(t, api)
	if err := c.Record(Target{Owner: "o", Repo: "r", Number: 1}); err == nil || !strings.Contains(err.Error(), "tree") {
		t.Errorf("Record without a tree: %v", err)
	}
}

func TestRecordNumberThatIsNotAPullRequest(t *testing.T) {
	tests := []struct {
		number int
		issue  int
	}{
		{9, 200},
		{9999, 404},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.number), func(t *testing.T) {
			api := newFakeAPI(t)
			c := newTestClient(t, api)
			if err := c.Record(Target{Owner: "o", Repo: "r", Number: tt.number}); err != nil {
				t.Fatal(err)
			}
			recs := readRecordings(t, c.Dir)
			pull := recs[fmt.Sprintf("/repos/o/r/pulls/%d?map[]", tt.number)]
			issue := recs[fmt.Sprintf("/repos/o/r/issues/%d?map[]", tt.number)]
			if len(recs) != 2 || pull.Status != 404 || issue.Status != tt.issue {
				t.Errorf("recordings = %v", recs)
			}
		})
	}
}

func TestGetRejectsForeignNextLink(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request sent to the foreign host: %s", r.URL)
	}))
	defer foreign.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", "<"+foreign.URL+"/x?page=2>; rel=\"next\"")
		fmt.Fprint(w, `[]`)
	}))
	defer api.Close()
	c := &Client{BaseURL: api.URL, Token: testToken, HTTP: api.Client(), Sanitizer: testSanitizer(t), Dir: t.TempDir()}
	if _, _, err := c.Get(Request{Path: "/x"}); err == nil {
		t.Error("Get followed a next link to another host")
	}
}

func TestGetRejectsNonJSONBody(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "diff --git a/x b/x")
	}))
	defer api.Close()
	c := &Client{BaseURL: api.URL, Token: testToken, HTTP: api.Client(), Sanitizer: testSanitizer(t), Dir: t.TempDir()}
	if _, _, err := c.Get(Request{Path: "/x"}); err == nil {
		t.Error("Get accepted a body that is not JSON")
	}
}

func TestRecordSearch(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	var waits int
	c.SearchThrottle = &Throttle{Interval: time.Second, Sleep: func(time.Duration) { waits++ }}
	res, err := c.RecordSearch("o", "r", "legacyToken", "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || res.Incomplete || !reflect.DeepEqual(res.Paths, []string{"src/A.kt", "src/B.kt"}) {
		t.Errorf("result %+v", res)
	}
	recs := readRecordings(t, c.Dir)
	if len(recs) != 3 {
		t.Errorf("%d recordings, want the search and the two files: %v", len(recs), api.seen)
	}
	for _, p := range []string{"src/A.kt", "src/B.kt"} {
		if _, ok := recs["/repos/o/r/contents/"+p+"?"+fmt.Sprint(map[string]string{"ref": "main"})]; !ok {
			t.Errorf("contents of %s at main not recorded", p)
		}
	}
	// The throttle is asked once per search request and never for a file.
	if _, err := c.RecordSearch("o", "r", "gone", "main"); err != nil {
		t.Fatal(err)
	}
	if c.SearchThrottle.last.IsZero() {
		t.Error("the search throttle was not used")
	}
}

func TestRecordSearchReportsIncompleteResults(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	res, err := c.RecordSearch("o", "r", "gone", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Incomplete || res.Total != 0 || len(res.Paths) != 0 {
		t.Errorf("result %+v, want an incomplete empty result recorded as returned", res)
	}
}

func TestRecordSearchFailsOnStatus(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	if _, err := c.RecordSearch("o", "r", "unknown", "main"); err == nil {
		t.Error("RecordSearch accepted a 404")
	}
}

func TestSearchThrottleOnlyAppliesToSearch(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	calls := 0
	c.SearchThrottle = &Throttle{Interval: time.Hour, Sleep: func(time.Duration) { calls++ }}
	if err := c.Record(Target{Owner: "o", Repo: "r", Number: 1}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || !c.SearchThrottle.last.IsZero() {
		t.Errorf("the search throttle was used by %d other requests", calls)
	}
}

func TestRecordTreeOfRecordedPullRequest(t *testing.T) {
	api := newFakeAPI(t)
	c := newTestClient(t, api)
	// A previous run recorded the pull request, which must not be read
	// again, and a number that is an issue.
	pull := Recording{Method: "GET", Path: "/repos/o/r/pulls/1", Status: 200,
		Body: json.RawMessage(`{"head":{"ref":"feat/x","sha":"h1"},"base":{"ref":"main","sha":"b1"}}`)}
	missing := Recording{Method: "GET", Path: "/repos/o/r/pulls/9", Status: 404, Body: json.RawMessage(`{"message":"Not Found"}`)}
	for _, r := range []Recording{pull, missing} {
		data, err := r.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c.Dir, FileName(r.Method, r.Path, r.Query)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int{1, 9} {
		if err := c.RecordTreeOfRecorded(Target{Owner: "o", Repo: "r", Number: n}); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"GET /repos/o/r/git/trees/h1?recursive=1"}; !reflect.DeepEqual(api.seen, want) {
		t.Errorf("requests %v, want only %v", api.seen, want)
	}
	if err := c.RecordTreeOfRecorded(Target{Owner: "o", Repo: "r", Number: 77}); err == nil {
		t.Error("a pull request that was never recorded has no head to read the tree at")
	}
}

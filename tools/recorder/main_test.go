package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/tools/ghrecord"
)

const fakeToken = "fake-token-value"

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

var validEnv = map[string]string{"ACR_RECORDER_TOKEN": fakeToken, "ACR_SANDBOX_LOGINS": "RealReviewer,real-author"}

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"no scenario", nil, validEnv, "usage:"},
		{"unknown scenario", []string{"S99"}, validEnv, `unknown scenario "S99"`},
		{"unknown flag", []string{"-x", "S1"}, validEnv, "usage:"},
		{"no token", []string{"S1"}, map[string]string{"ACR_SANDBOX_LOGINS": "a,b"}, "ACR_RECORDER_TOKEN is not set"},
		{"no logins", []string{"S1"}, map[string]string{"ACR_RECORDER_TOKEN": fakeToken}, "ACR_SANDBOX_LOGINS"},
		{"one login", []string{"S1"}, map[string]string{"ACR_RECORDER_TOKEN": fakeToken, "ACR_SANDBOX_LOGINS": "a"}, "ACR_SANDBOX_LOGINS"},
		{"empty login", []string{"S1"}, map[string]string{"ACR_RECORDER_TOKEN": fakeToken, "ACR_SANDBOX_LOGINS": "a, "}, "ACR_SANDBOX_LOGINS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tt.args, env(tt.env), &stdout, &stderr)
			if code != 2 || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("run(%v) = %d, stderr %q; want 2 and %q", tt.args, code, stderr.String(), tt.want)
			}
			if strings.Contains(stdout.String()+stderr.String(), fakeToken) {
				t.Error("output contains the token")
			}
		})
	}
}

func TestEveryScenarioHasARecording(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "testdata", "scenarios", "S*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no scenario directories: %v", err)
	}
	for _, dir := range dirs {
		name := filepath.Base(dir)
		if s, ok := scenarios[name]; !ok || len(s.targets) == 0 {
			t.Errorf("scenario %s has no targets to record", name)
		}
	}
}

func TestRunRecordsIntoScenarioDirectories(t *testing.T) {
	var (
		mu      sync.Mutex
		methods []string
	)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.Method)
		mu.Unlock()
		if r.URL.Path == "/search/issues" {
			w.Write([]byte(`{"total_count":0,"items":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer api.Close()

	out := t.TempDir()
	stale := filepath.Join(out, "S3", "stale.json")
	keep := filepath.Join(out, "S3", "notes.txt")
	for _, f := range []string{stale, keep} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-api", api.URL, "-out", out, "S3", "queue"}, env(validEnv), &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale recording was not removed: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("non-recording file was removed: %v", err)
	}
	for _, dir := range []string{"S3", "queue"} {
		files, _ := filepath.Glob(filepath.Join(out, dir, "*.json"))
		if len(files) == 0 {
			t.Errorf("no recordings in %s", dir)
		}
	}
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("request with method %s", m)
		}
	}
}

func TestEverySearchIsExplained(t *testing.T) {
	for name, s := range scenarios {
		for _, q := range s.searches {
			if q.Repo == "" || q.Term == "" || q.Rule == "" {
				t.Errorf("scenario %s: search %+v needs a repository, a term and the rule that needs it", name, q)
			}
		}
	}
	// S2 is the scenario of the uses-removed-api candidate: core-lib S2
	// removes legacyToken(), which sample-app still calls on main.
	var found bool
	for _, q := range scenarios["S2"].searches {
		found = found || q.Repo == "sample-app" && q.Term == "legacyToken" && q.Rule == "uses-removed-api"
	}
	if !found {
		t.Errorf("S2 does not search sample-app for legacyToken: %+v", scenarios["S2"].searches)
	}
}

// withScenarios replaces the scenario table for one test.
func withScenarios(t *testing.T, table map[string]scenario) {
	t.Helper()
	old := scenarios
	scenarios = table
	t.Cleanup(func() { scenarios = old })
}

// newTreeAPI serves one pull request with a tree and a code search.
func newTreeAPI(t *testing.T, incomplete bool) (*httptest.Server, *[]string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path+"?"+r.URL.Query().Encode())
		mu.Unlock()
		switch r.URL.Path {
		case "/repos/acr-plugin-sandbox/app/pulls/1":
			w.Write([]byte(`{"head":{"ref":"feat/x","sha":"h1"},"base":{"ref":"main","sha":"b1"},"updated_at":"` + time.Now().Format(time.RFC3339Nano) + `"}`))
		case "/repos/acr-plugin-sandbox/app/pulls/1/files":
			w.Write([]byte(`[]`))
		case "/repos/acr-plugin-sandbox/app/git/trees/h1":
			w.Write([]byte(`{"sha":"h1","tree":[],"truncated":false}`))
		case "/search/code":
			if incomplete {
				w.Write([]byte(`{"total_count":0,"incomplete_results":true,"items":[]}`))
				return
			}
			w.Write([]byte(`{"total_count":1,"incomplete_results":false,"items":[{"path":"A.kt"}]}`))
		case "/repos/acr-plugin-sandbox/app/contents/A.kt":
			w.Write([]byte(`{"type":"file","content":"YQ=="}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		}
	}))
	t.Cleanup(api.Close)
	return api, &seen
}

func TestRunRecordsTreeAndSearch(t *testing.T) {
	withScenarios(t, map[string]scenario{"T1": {
		targets:  []ghrecord.Target{pr("app", 1)},
		searches: []search{{Repo: "app", Term: "oldName", Rule: "uses-removed-api"}},
	}})
	api, seen := newTreeAPI(t, false)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-api", api.URL, "-out", out, "-search-interval", "0", "T1"}, env(validEnv), &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
	files, _ := filepath.Glob(filepath.Join(out, "T1", "*.json"))
	names := strings.Join(files, "\n")
	for _, want := range []string{"git_trees_h1", "search_code", "contents_A.kt_ref_main"} {
		if !strings.Contains(names, want) {
			t.Errorf("no recording named like %s in\n%s", want, names)
		}
	}
	if strings.Contains(stderr.String(), "warning") {
		t.Errorf("unexpected warning: %q", stderr.String())
	}
	if !contains(*seen, "/search/code?per_page=100&q=oldName+repo%3Aacr-plugin-sandbox%2Fapp") {
		t.Errorf("search request not sent: %v", *seen)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestRunWarnsAboutIncompleteSearch(t *testing.T) {
	withScenarios(t, map[string]scenario{"T1": {
		targets:  []ghrecord.Target{pr("app", 1)},
		searches: []search{{Repo: "app", Term: "oldName", Rule: "uses-removed-api"}},
	}})
	api, _ := newTreeAPI(t, true)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-api", api.URL, "-out", t.TempDir(), "-search-interval", "0", "T1"}, env(validEnv), &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `warning: T1: search "oldName" in app returned incomplete results`) {
		t.Errorf("stderr %q has no warning about the incomplete search", stderr.String())
	}
}

func TestRunExtendKeepsExistingRecordings(t *testing.T) {
	withScenarios(t, map[string]scenario{"T1": {
		targets:  []ghrecord.Target{pr("app", 1)},
		searches: []search{{Repo: "app", Term: "oldName", Rule: "uses-removed-api"}},
	}})
	api, seen := newTreeAPI(t, false)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{"-api", api.URL, "-out", out, "-search-interval", "0"}
	if code := run(append(args, "T1"), env(validEnv), &stdout, &stderr); code != 0 {
		t.Fatalf("first run = %d, stderr %q", code, stderr.String())
	}
	before := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(out, "T1", "*.json"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		before[f] = string(data)
	}
	// Drop the tree and the search, as in a scenario recorded before they
	// existed, and extend it.
	for f := range before {
		if strings.Contains(f, "git_trees") || strings.Contains(f, "search_code") || strings.Contains(f, "contents_A.kt") {
			os.Remove(f)
		}
	}
	*seen = nil
	if code := run(append([]string{"-extend"}, append(args, "T1")...), env(validEnv), &stdout, &stderr); code != 0 {
		t.Fatalf("extend run = %d, stderr %q", code, stderr.String())
	}
	for f, data := range before {
		now, err := os.ReadFile(f)
		switch {
		case err != nil && !strings.Contains(f, "git_trees") && !strings.Contains(f, "search_code") && !strings.Contains(f, "contents_A.kt"):
			t.Errorf("%s was removed: %v", f, err)
		case err == nil && string(now) != data:
			t.Errorf("%s changed by -extend", f)
		}
	}
	after, _ := filepath.Glob(filepath.Join(out, "T1", "*.json"))
	if len(after) != len(before) {
		t.Errorf("%d recordings after -extend, %d before", len(after), len(before))
	}
	for _, s := range *seen {
		if strings.Contains(s, "/pulls/") {
			t.Errorf("-extend read %s again", s)
		}
	}
}

func TestScenarioS7(t *testing.T) {
	s := scenarios["S7"]
	want := []ghrecord.Target{pr("sample-app", 8), pr("kmp-shared", 3)}
	if !reflect.DeepEqual(s.targets, want) {
		t.Errorf("S7 targets %v, want %v", s.targets, want)
	}
	// kmp-shared S7 adds an expect without an iOS actual: the detector
	// searches the target branch for the declaration's name.
	if want := []search{{Repo: "kmp-shared", Term: "deviceModel", Rule: "kmp-expect-without-actual"}}; !reflect.DeepEqual(s.searches, want) {
		t.Errorf("S7 searches %v, want %v", s.searches, want)
	}
}

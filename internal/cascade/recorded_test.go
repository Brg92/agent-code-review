package cascade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/state"
)

// recording is the part of a recorded response that the tests read.
type recording struct {
	Path   string          `json:"path"`
	Status int             `json:"status"`
	Raw    json.RawMessage `json:"body"`
	Body   pull            `json:"-"`
}

// pull is the body of a pull request or of an issue.
type pull struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

// recorded is a Source over the recorded GitHub responses of the sandbox
// scenarios. A request with no recording fails the test: the walk may only
// ask for what a real run asks for.
type recorded struct {
	t     *testing.T
	byURL map[string]recording
	asked []string
}

var pullOrIssue = regexp.MustCompile(`^/repos/[^/]+/[^/]+/(?:pulls|issues)/[0-9]+$`)

func loadRecorded(t *testing.T, scenarios ...string) *recorded {
	t.Helper()
	r := &recorded{t: t, byURL: map[string]recording{}}
	for _, s := range scenarios {
		files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "github", s, "GET_repos_*_*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no recordings for %s: %v", s, err)
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var rec recording
			if err := json.Unmarshal(data, &rec); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			if !pullOrIssue.MatchString(rec.Path) {
				continue
			}
			if rec.Status == 200 {
				if err := json.Unmarshal(rec.Raw, &rec.Body); err != nil {
					t.Fatalf("%s: %v", f, err)
				}
			}
			r.byURL[rec.Path] = rec
		}
	}
	return r
}

func (r *recorded) Get(_ context.Context, key core.Key) (Item, error) {
	r.asked = append(r.asked, key.String())
	pull, ok := r.byURL[fmt.Sprintf("/repos/%s/pulls/%d", key.Project, key.Number)]
	if !ok {
		return Item{}, fmt.Errorf("no recording of the pull request %s", key)
	}
	if pull.Status == 200 {
		return Item{Kind: KindChangeRequest, Title: pull.Body.Title, Description: pull.Body.Body, Head: pull.Body.Head.SHA}, nil
	}
	issue, ok := r.byURL[fmt.Sprintf("/repos/%s/issues/%d", key.Project, key.Number)]
	switch {
	case !ok:
		return Item{}, fmt.Errorf("pull request %s is missing and its issue is not recorded", key)
	case issue.Status == 200:
		return Item{Kind: KindIssue, Title: issue.Body.Title}, nil
	}
	return Item{Kind: KindNotFound}, nil
}

const sandbox = "github:github.com/acr-plugin-sandbox/"

func exampleState(t *testing.T, name string) *state.State {
	t.Helper()
	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "state", "valid", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, state.FileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := state.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// sameCascade compares resolved entries with the entries of an example
// state, ignoring how far the example has progressed.
func sameCascade(t *testing.T, got []core.Entry, want []core.Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d entries, want %d:\n%q", len(got), len(want), walk(Result{Entries: got}))
	}
	for i, w := range want {
		g := got[i]
		if g.Status != core.Skipped && w.Status != core.Skipped {
			w.Status = g.Status
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
}

func TestResolveS2(t *testing.T) {
	src := loadRecorded(t, "S2", "S4")
	root := k(t, sandbox+"sample-app#7")
	res, err := Resolve(context.Background(), src, refs.Compose(github.Registry{}, gitlab.Registry{}), root, core.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	check(t, "entries", walk(res), []string{
		"0 sample-app#7 <- pending",
		"1 core-lib#3 <sample-app#7 pending",
		"2 kmp-shared#2 <core-lib#3 pending",
		"3 feature-lib#1 <kmp-shared#2 pending",
		"2 sample-app#7 <core-lib#3 cycle",
		"1 feature-lib#2 <sample-app#7 already-done",
		"1 sample-app#2 <sample-app#7 pending",
		"1 sample-app#1 <sample-app#7 not-a-change-request",
		"1 sample-app#7 <sample-app#7 self-reference",
		"1 sample-app#1 <sample-app#7 other-platform",
		"1 core-lib#9999 <sample-app#7 not-found",
	})
	check(t, "review order", order(res), []string{"feature-lib#1", "kmp-shared#2", "core-lib#3", "sample-app#2", "sample-app#7"})
	if len(res.Order) != 5 {
		t.Errorf("%d change requests reviewed, want 5", len(res.Order))
	}

	// The same cascade as the contract example of a finished S2 run.
	sameCascade(t, res.Entries, exampleState(t, "s2-cascade-finalized.json").ChangeRequests)

	// What is reported besides the entries.
	if len(res.NotFollowed) != 1 || res.NotFollowed[0].Text != "- https://github.com/acr-plugin-sandbox/kmp-shared/pull/1" || short(res.NotFollowed[0].CR) != "sample-app#7" {
		t.Errorf("NotFollowed = %+v, want the kmp-shared/pull/1 line of sample-app#7", res.NotFollowed)
	}
	if len(res.Unparsed) != 0 {
		t.Errorf("Unparsed = %+v", res.Unparsed)
	}
	var causes []string
	for _, c := range res.Causes() {
		causes = append(causes, c.Reason+" "+short(c.CR))
	}
	check(t, "causes", causes, []string{"other-platform sample-app#1", "not-found core-lib#9999"})
	if !res.Incomplete() {
		t.Error("the S2 cascade has a link to another platform and a missing pull request: it is incomplete")
	}

	// Nothing is asked that a ticked entry, a line without a checkbox or a
	// link to another platform would need.
	for _, asked := range src.asked {
		for _, never := range []string{"feature-lib#2", "kmp-shared#1", "gitlab:"} {
			if strings.Contains(asked, never) {
				t.Errorf("requested %s", asked)
			}
		}
	}

	// The result is a state the loader accepts.
	if _, err := state.New("20261102T101500Z-0a1b2c3d", root, core.DefaultLimits(), res.Entries, time.Now()); err != nil {
		t.Error(err)
	}
}

func TestResolveS2WithLowLimits(t *testing.T) {
	src := loadRecorded(t, "S2", "S4")
	root := k(t, sandbox+"sample-app#7")
	limits := core.Limits{MaxDepth: 1, MaxChangeRequests: 2}
	res, err := Resolve(context.Background(), src, refs.Compose(github.Registry{}, gitlab.Registry{}), root, limits)
	if err != nil {
		t.Fatal(err)
	}
	example := exampleState(t, "s2-low-limits.json")
	if example.Cascade != limits {
		t.Fatalf("the example uses %+v", example.Cascade)
	}
	sameCascade(t, res.Entries, example.ChangeRequests)
	check(t, "review order", order(res), []string{"core-lib#3", "sample-app#7"})
	var causes []string
	for _, c := range res.Causes() {
		causes = append(causes, c.Reason+" "+short(c.CR))
	}
	check(t, "causes", causes, []string{"over-depth-limit kmp-shared#2", "over-count-limit sample-app#2", "other-platform sample-app#1", "not-found core-lib#9999"})
}

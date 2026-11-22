package review_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/publishworld"
)

const (
	app  = "sample-app"
	app2 = 2
)

func s1(t *testing.T) *publishworld.World {
	t.Helper()
	return publishworld.New(t, "S1", app, app2)
}

func rulesOf(actions []core.Action) []string {
	var out []string
	for _, a := range actions {
		out = append(out, a.Rules...)
	}
	return out
}

func failure(t *testing.T, err error, kind review.Kind) *review.Failure {
	t.Helper()
	var f *review.Failure
	if err == nil || !asFailure(err, &f) || f.Kind != kind {
		t.Fatalf("error = %v, want a failure of kind %v", err, kind)
	}
	return f
}

func asFailure(err error, f **review.Failure) bool {
	for err != nil {
		if x, ok := err.(*review.Failure); ok {
			*f = x
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestS1PlanThenApplyPostsOneReviewWithTheBlockers: S1 is BLOCKED by three
// BLOCKERs of the change request, one of them on a line outside the hunks.
// Planning writes nothing to the platform; applying posts one review that
// comments, with one inline comment each, and nothing else: the verdict is
// BLOCKED, so there is no note, and the findings of the existing code and
// the MAJOR ones are for the issues.
func TestS1PlanThenApplyPostsOneReviewWithTheBlockers(t *testing.T) {
	w := s1(t)
	planned := w.MustPlan(core.Comments)
	if n := len(w.Server.Requests()); n == 0 {
		t.Error("planning read nothing")
	}
	if got := w.Server.Writes(); len(got) != 0 {
		t.Fatalf("planning wrote %v", got)
	}
	if got := w.State().Phase; got != state.Planned {
		t.Errorf("phase = %s, want planned", got)
	}
	plan := w.ReadPlan(app, app2)
	comments := publishworld.Actions(plan, core.ActionReviewComment)
	if got, want := strings.Join(rulesOf(comments), " "), "global-scope logic-defect run-blocking-prod"; !sameWords(got, want) {
		t.Errorf("inline comments for %q, want %q", got, want)
	}
	if len(plan.Actions) != len(comments) {
		t.Errorf("plan has %d actions besides the %d comments: no note when BLOCKED, no issue for comments", len(plan.Actions), len(comments))
	}
	var relocated core.Action
	for _, c := range comments {
		if strings.HasPrefix(c.Body, "Refers to line 27 of `app/src/main/java/com/example/sample/ui/HomeViewModel.kt`, outside the changed lines.") {
			relocated = c
		}
		if c.Severity != core.Blocker || !strings.HasSuffix(c.Body, core.Embed(c.FindingID)) || c.Side != core.Right {
			t.Errorf("comment %+v", c)
		}
	}
	if relocated.Line != 16 {
		t.Errorf("the finding outside the hunks is anchored on line %d, want 16: %+v", relocated.Line, relocated)
	}
	for _, want := range []string{"plan for", "Nothing was written to GitHub", "3 inline comments", "global-scope", "run-blocking-prod", "logic-defect"} {
		if !strings.Contains(strings.ToLower(planned.Text), strings.ToLower(want)) {
			t.Errorf("the summary lacks %q:\n%s", want, planned.Text)
		}
	}

	applied := w.MustApply()
	if !applied.Done {
		t.Errorf("Done = false:\n%s", applied.Text)
	}
	writes := w.Server.Writes()
	if len(writes) != 1 || writes[0].String() != "POST /repos/acr-plugin-sandbox/sample-app/pulls/2/reviews" {
		t.Fatalf("writes = %v, want one review", writes)
	}
	var sent struct {
		CommitID string `json:"commit_id"`
		Event    string `json:"event"`
		Body     string `json:"body"`
		Comments []struct {
			Path, Body, Side string
			Line             int
		} `json:"comments"`
	}
	if err := json.Unmarshal([]byte(writes[0].Body), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Event != "COMMENT" || sent.Body != "" || len(sent.Comments) != 3 || sent.CommitID != plan.HeadSHA {
		t.Errorf("review = %+v, want 3 comments, the event COMMENT, no text, on the reviewed head", sent)
	}
	if got := w.State().Phase; got != state.Published {
		t.Errorf("phase = %s, want published", got)
	}
	pub, ok := w.Published(app, app2)
	if !ok || len(pub.Outcomes) != 3 {
		t.Fatalf("published = %+v, %v", pub, ok)
	}
	for _, o := range pub.Outcomes {
		if o.Status != core.StatusPosted {
			t.Errorf("outcome %+v", o)
		}
	}
	if w.Server.SawMutation() {
		t.Error("a GraphQL mutation reached the platform")
	}
}

func sameWords(a, b string) bool {
	x, y := strings.Fields(a), strings.Fields(b)
	if len(x) != len(y) {
		return false
	}
	seen := map[string]int{}
	for _, s := range x {
		seen[s]++
	}
	for _, s := range y {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

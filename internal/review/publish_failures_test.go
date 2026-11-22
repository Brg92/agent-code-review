package review_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
	"github.com/Brg92/agent-code-review/internal/testutil/publishworld"
)

func countWrites(w *publishworld.World, method, suffix string) int {
	n := 0
	for _, r := range w.Server.Writes() {
		if r.Method == method && strings.HasSuffix(r.Path, suffix) {
			n++
		}
	}
	return n
}

// TestFailureHalfWayThenResume: the platform fails on the review after the
// label and the issues were written. The apply says what was done and what
// was not, the run stays planned, and applying again writes the review and
// nothing twice.
func TestFailureHalfWayThenResume(t *testing.T) {
	w := s1(t)
	w.MustPlan(core.Both)
	w.Server.Fail("POST", `/pulls/2/reviews$`, ghserver.Fault{Status: 502, Body: `{"message":"Bad Gateway"}`, Times: 1})
	applied, err := w.Apply()
	f := failure(t, err, review.KindPlatform)
	if !strings.Contains(f.What, "post the review") || !strings.Contains(f.Why, "transport") {
		t.Errorf("failure %+v", f)
	}
	if applied == nil || applied.Done || !strings.Contains(applied.Text, "only partly published") || !strings.Contains(applied.Text, "posted  label review") ||
		!strings.Contains(applied.Text, "to do  inline comment") {
		t.Errorf("text = %v", applied)
	}
	if got := w.State().Phase; got != state.Planned {
		t.Errorf("phase %s, want planned: not every action has a final outcome", got)
	}
	pub, _ := w.Published("sample-app", 2)
	statuses := map[core.PlanStatus]int{}
	for _, o := range pub.Outcomes {
		statuses[o.Status]++
	}
	if statuses[core.StatusPosted] != 3 || statuses[core.StatusFailed] != 3 {
		t.Errorf("outcomes = %+v, want the label and two issues posted and three comments failed", pub.Outcomes)
	}
	if strings.Contains(string(mustRead(t, filepath.Join(w.Package("sample-app", 2).Dir, state.PublishedFile))), "Bad Gateway") {
		t.Error("published.json holds the answer of the platform")
	}
	if got := countWrites(w, "POST", "/reviews"); got != 1 {
		t.Errorf("%d reviews sent: a write whose outcome is unknown is not repeated at once", got)
	}

	applied = w.MustApply()
	if !applied.Done || w.State().Phase != state.Published {
		t.Errorf("the second apply did not finish:\n%s", applied.Text)
	}
	if countWrites(w, "POST", "/labels") != 1 || countWrites(w, "POST", "/issues") != 2 {
		t.Errorf("the label and the issues were written again: %v", w.Server.Writes())
	}
	if got := len(w.PR("sample-app", 2).Inlines); got != 3 {
		t.Errorf("%d inline comments on the pull request, want 3", got)
	}
	if got := len(w.Server.Repo("acr-plugin-sandbox/sample-app").Issues); got != 2 {
		t.Errorf("%d issues, want 2", got)
	}
}

// TestLostAnswerIsNotWrittenTwice: the platform wrote the note and the
// answer was lost. The outcome is unknown, so the next apply looks for the
// fingerprint before it writes, and finds it.
func TestLostAnswerIsNotWrittenTwice(t *testing.T) {
	w := s2(t)
	w.MustPlan(core.Comments)
	w.Server.Fail("POST", `/sample-app/issues/7/comments$`, ghserver.Fault{Status: 502, Body: `{}`, Times: 1, Apply: true})
	_, err := w.Apply()
	failure(t, err, review.KindPlatform)
	if got := len(w.PR("sample-app", 7).Comments); got != 1 {
		t.Fatalf("the note is there %d times after the failure", got)
	}
	applied := w.MustApply()
	if got := len(w.PR("sample-app", 7).Comments); got != 1 {
		t.Errorf("the note is there %d times after the second apply", got)
	}
	pub, _ := w.Published("sample-app", 7)
	if len(pub.Outcomes) != 1 || pub.Outcomes[0].Status != core.StatusAlreadyPosted || !applied.Done {
		t.Errorf("outcomes %+v, done %v, want already-posted", pub.Outcomes, applied.Done)
	}
}

// TestResumeWithoutTheOutcomes: published.json is gone, which is what a
// disk that failed or a cleanup does; the platform tells what is posted.
func TestResumeWithoutTheOutcomes(t *testing.T) {
	w := s1(t)
	w.MustPlan(core.Both)
	w.Server.Fail("POST", `/pulls/2/reviews$`, ghserver.Fault{Status: 500, Body: `{}`, Times: 1, Apply: true})
	_, err := w.Apply()
	failure(t, err, review.KindPlatform)
	if err := os.Remove(filepath.Join(w.Package("sample-app", 2).Dir, state.PublishedFile)); err != nil {
		t.Fatal(err)
	}
	w.MustApply()
	if countWrites(w, "POST", "/labels") != 1 || countWrites(w, "POST", "/issues") != 2 || len(w.PR("sample-app", 2).Inlines) != 3 {
		t.Errorf("something was written twice: %v", w.Server.Writes())
	}
	pub, _ := w.Published("sample-app", 2)
	for _, o := range pub.Outcomes {
		if o.Status != core.StatusAlreadyPosted {
			t.Errorf("outcome %+v, want already-posted: the platform has it", o)
		}
	}
}

func TestOutcomesOfAnotherPlanAreStale(t *testing.T) {
	w := s1(t)
	w.MustPlan(core.Comments)
	w.Server.Fail("POST", `/pulls/2/reviews$`, ghserver.Fault{Status: 502, Body: `{}`, Times: 1})
	_, err := w.Apply()
	failure(t, err, review.KindPlatform)
	w.MustPlan(core.Both) // a new plan: the outcomes of the old one do not count
	w.MustApply()
	pub, _ := w.Published("sample-app", 2)
	if len(pub.Outcomes) != len(w.ReadPlan("sample-app", 2).Actions) {
		t.Errorf("outcomes %+v for the new plan", pub.Outcomes)
	}
	if got := len(w.PR("sample-app", 2).Inlines); got != 3 {
		t.Errorf("%d inline comments", got)
	}
}

func TestPlatformFailuresHaveTheirKind(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault ghserver.Fault
		kind  review.Kind
	}{
		{"server error", ghserver.Fault{Status: 503, Body: `{}`}, review.KindPlatform},
		{"dropped connection", ghserver.Fault{Drop: true}, review.KindPlatform},
		{"rate limit beyond the wait", ghserver.Fault{Status: 429, Header: map[string]string{"Retry-After": "86400"}, Body: `{}`}, review.KindPlatform},
		{"no permission", ghserver.Fault{Status: 403, Body: `{"message":"Resource not accessible"}`, Header: map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=write"}}, review.KindAuth},
		{"refused token", ghserver.Fault{Status: 401, Body: `{"message":"Bad credentials"}`}, review.KindAuth},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := s1(t)
			w.MustPlan(core.Comments)
			w.Server.Fail("POST", `/reviews$`, c.fault)
			applied, err := w.Apply()
			f := failure(t, err, c.kind)
			if applied == nil || applied.Done || !strings.Contains(applied.Text, "to do") || !strings.Contains(f.Report(), "what to do:") {
				t.Errorf("text %v, report %q", applied, f.Report())
			}
			if got := w.State().Phase; got != state.Planned {
				t.Errorf("phase %s", got)
			}
			if strings.Contains(f.Report(), "Bad credentials") || strings.Contains(f.Report(), "Retry-After") {
				t.Errorf("the report holds the platform's words: %s", f.Report())
			}
		})
	}
}

func TestReadFailureWhilePlanningWritesNoPlan(t *testing.T) {
	w := s2(t)
	w.Server.Fail("GET", `/sample-app/issues/7/comments$`, ghserver.Fault{Status: 500, Body: `{}`})
	planned, err := w.Plan(core.Comments)
	failure(t, err, review.KindPlatform)
	if planned != nil {
		t.Errorf("planned = %+v", planned)
	}
	if got := w.State().Phase; got != state.Finalized {
		t.Errorf("phase %s: a plan that could not be made leaves the run as it was", got)
	}
	for _, p := range w.Run.Packages {
		if _, err := os.Stat(filepath.Join(p.Dir, state.PlanFile)); err == nil {
			t.Errorf("%s has a plan", p.CR)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestRejectedPositionOfOneInlineComment: the platform refuses one of the
// three comments of the review. When its answer names it, the review is
// sent again without it, once, and the refused comment is recorded as
// rejected, never tried on another line; when it does not, nothing was
// posted and every comment is recorded as rejected.
func TestRejectedPositionOfOneInlineComment(t *testing.T) {
	const viewModel = "app/src/main/java/com/example/sample/ui/HomeViewModel.kt"
	restrict := func(w *publishworld.World) {
		w.PR("sample-app", 2).Lines = map[string]map[string][]int{
			"app/src/main/java/com/example/sample/MainActivity.kt":           {"RIGHT": {13}},
			"app/src/main/java/com/example/sample/data/SessionRepository.kt": {"RIGHT": {19}},
			viewModel: {"RIGHT": {1}},
		}
	}
	t.Run("the platform names it", func(t *testing.T) {
		w := s1(t)
		restrict(w)
		w.Server.NameRejected = true
		w.MustPlan(core.Comments)
		applied := w.MustApply()
		reviews := 0
		for _, r := range w.Server.Writes() {
			if strings.HasSuffix(r.Path, "/reviews") {
				reviews++
			}
		}
		if reviews != 2 || len(w.PR("sample-app", 2).Inlines) != 2 {
			t.Errorf("%d review requests and %d comments posted, want 2 and 2", reviews, len(w.PR("sample-app", 2).Inlines))
		}
		pub, _ := w.Published("sample-app", 2)
		rejected := 0
		for _, o := range pub.Outcomes {
			if o.Status == core.StatusRejected {
				rejected++
				if !strings.Contains(o.Detail, viewModel) || strings.Contains(o.Detail, "outside the changed lines") {
					t.Errorf("detail %q", o.Detail)
				}
			} else if o.Status != core.StatusPosted {
				t.Errorf("outcome %+v", o)
			}
		}
		if rejected != 1 || !applied.Done || !strings.Contains(applied.Text, "rejected") {
			t.Errorf("rejected %d, done %v:\n%s", rejected, applied.Done, applied.Text)
		}
		// The comment was never sent again on another line.
		planned := map[string]bool{}
		for _, a := range w.ReadPlan("sample-app", 2).Actions {
			planned[a.Path+":"+itoa(a.Line)] = true
		}
		for _, r := range w.Server.Writes() {
			var in struct {
				Comments []struct {
					Path string
					Line int
				}
			}
			_ = json.Unmarshal([]byte(r.Body), &in)
			for _, c := range in.Comments {
				if !planned[c.Path+":"+itoa(c.Line)] {
					t.Errorf("a comment was sent on %s:%d, which the plan does not have", c.Path, c.Line)
				}
			}
		}
		if w.State().Phase != state.Published {
			t.Errorf("phase %s: a rejected comment is a final outcome", w.State().Phase)
		}
	})
	t.Run("the platform does not name it", func(t *testing.T) {
		w := s1(t)
		restrict(w)
		w.MustPlan(core.Comments)
		applied := w.MustApply()
		if got := countWrites(w, "POST", "/reviews"); got != 1 {
			t.Errorf("%d review requests, want 1: without a name there is nothing to retry without", got)
		}
		if got := len(w.PR("sample-app", 2).Inlines); got != 0 {
			t.Errorf("%d comments posted", got)
		}
		pub, _ := w.Published("sample-app", 2)
		for _, o := range pub.Outcomes {
			if o.Status != core.StatusRejected || !strings.Contains(o.Detail, "did not say which") {
				t.Errorf("outcome %+v", o)
			}
		}
		if !applied.Done {
			t.Errorf("not published:\n%s", applied.Text)
		}
	})
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// Canaries for the secret patterns, built from pieces so that no line of the
// source is one.
var canaries = map[string]string{
	"github-token":          "gh" + "p_" + strings.Repeat("A1b2", 9),
	"aws-access-key-id":     "AK" + "IA" + "0123456789ABCDEF",
	"private-key":           "-----BEGIN RSA PRIVATE " + "KEY-----",
	"slack-token":           "xo" + "xb-" + "0123456789-abcdefghij",
	"credential-assignment": `password = "` + "hunter2hunter2" + `"`,
}

// TestSecretInATextBlocksOnlyItsAction: a text that matches a pattern is not
// planned and is not written, and the value is in no plan, no output, no log
// and no request; the other actions go ahead.
func TestSecretInATextBlocksOnlyItsAction(t *testing.T) {
	for pattern, canary := range canaries {
		for _, field := range []string{"why", "title"} {
			t.Run(pattern+" in "+field, func(t *testing.T) {
				w := s1(t)
				doc := w.Document("sample-app", 2)
				victim := doc.Findings[0]
				for i, f := range doc.Findings {
					if f.Severity == core.Blocker && i == 0 {
						victim = f
					}
				}
				if field == "why" {
					doc.Findings[0].Why = "leaks " + canary + " here"
				} else {
					doc.Findings[0].Title = "leaks " + canary
				}
				w.SetDocument("sample-app", 2, doc)
				planned := w.MustPlan(core.Both)
				plan := w.ReadPlan("sample-app", 2)
				blocked := publishworld.SkippedWith(plan, core.StatusSecretBlocked)
				if len(blocked) == 0 || !strings.Contains(blocked[0].Pattern, pattern) {
					t.Fatalf("skipped = %+v, want secret-blocked by %s", plan.Skipped, pattern)
				}
				if blocked[0].FindingID != victim.ID && blocked[0].Target == "" {
					t.Errorf("the blocked action is not named: %+v", blocked[0])
				}
				for _, a := range plan.Actions {
					if strings.Contains(a.Body+a.Title, canary) {
						t.Errorf("the plan holds the secret: %+v", a)
					}
				}
				if len(plan.Actions) == 0 {
					t.Error("every action was blocked: the others go ahead")
				}
				applied := w.MustApply()
				everything := planned.Text + applied.Text + string(mustRead(t, w.PlanFile("sample-app", 2))) +
					string(mustRead(t, filepath.Join(w.Package("sample-app", 2).Dir, state.PublishedFile))) + string(mustRead(t, filepath.Join(w.Work, "runs", w.Run.ID, "run.log")))
				for _, r := range w.Server.Requests() {
					everything += r.Path + r.Query + r.Body
				}
				if strings.Contains(everything, canary) || strings.Contains(everything, strings.TrimPrefix(canary, "password = ")) {
					t.Errorf("the secret is in the plan, the output, the log or a request")
				}
				if !strings.Contains(planned.Text, "secret-blocked") {
					t.Errorf("the summary does not say it:\n%s", planned.Text)
				}
				onlyAllowedWrites(t, w)
			})
		}
	}
}

// TestApplyScansAgain: the plan is checked against its hash, and its texts
// are scanned again; a plan whose text matches a pattern, whatever the way
// it got there, is not written.
func TestApplyScansAgain(t *testing.T) {
	w := s1(t)
	w.MustPlan(core.Comments)
	plan := w.ReadPlan("sample-app", 2)
	plan.Actions[0].Body += "\n" + canaries["github-token"]
	data, err := core.EncodePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.PlanFile("sample-app", 2), data, 0o600); err != nil {
		t.Fatal(err)
	}
	st := w.State()
	for i := range st.ChangeRequests {
		if st.ChangeRequests[i].Status != core.Skipped && st.ChangeRequests[i].CR.Number == 2 {
			st.ChangeRequests[i].PlanSHA256 = core.Hash(data)
		}
	}
	w.SetState(st)
	applied := w.MustApply()
	pub, _ := w.Published("sample-app", 2)
	if pub.Outcomes[0].Status != core.StatusSecretBlocked || !strings.Contains(pub.Outcomes[0].Detail, "github-token") {
		t.Errorf("outcomes %+v", pub.Outcomes)
	}
	if got := len(w.PR("sample-app", 2).Inlines); got != 2 {
		t.Errorf("%d comments posted, want the 2 that are not blocked", got)
	}
	var published bytes.Buffer
	published.WriteString(applied.Text)
	published.Write(mustRead(t, filepath.Join(w.Package("sample-app", 2).Dir, state.PublishedFile)))
	for _, r := range w.Server.Requests() {
		published.WriteString(r.Body)
	}
	if strings.Contains(published.String(), canaries["github-token"]) {
		t.Error("the secret was written or printed")
	}
}

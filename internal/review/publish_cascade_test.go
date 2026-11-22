package review_test

import (
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

func s2(t *testing.T) *publishworld.World {
	t.Helper()
	return publishworld.New(t, "S2", "sample-app", 7)
}

// onlyAllowedWrites checks that the platform never saw anything but the
// writes of publish: reviews that comment, comments, issues, labels and the
// description of a pull request.
func onlyAllowedWrites(t *testing.T, w *publishworld.World) {
	t.Helper()
	for _, r := range w.Server.Writes() {
		ok := false
		for _, suffix := range []string{"/reviews", "/comments", "/issues", "/labels"} {
			ok = ok || r.Method == "POST" && strings.HasSuffix(r.Path, suffix)
		}
		ok = ok || r.Method == "PATCH" && (strings.Contains(r.Path, "/pulls/") || strings.Contains(r.Path, "/issues/"))
		if !ok || strings.Contains(r.Path, "merge") || strings.Contains(r.Body, "APPROVE") || strings.Contains(r.Body, "REQUEST_CHANGES") ||
			strings.Contains(r.Body, `"state"`) {
			t.Errorf("the platform saw %s %s", r, r.Body)
		}
	}
	if w.Server.SawMutation() {
		t.Error("a GraphQL mutation reached the platform")
	}
}

// diffBytes counts the bytes that differ between two strings of the same
// length.
func diffBytes(a, b string) int {
	if len(a) != len(b) {
		return -1
	}
	n := 0
	for i := range a {
		if a[i] != b[i] {
			n++
		}
	}
	return n
}

func writesTo(w *publishworld.World, prefix string) []ghserver.Request {
	var out []ghserver.Request
	for _, r := range w.Server.Writes() {
		if strings.HasPrefix(r.Path, "/repos/acr-plugin-sandbox/"+prefix) {
			out = append(out, r)
		}
	}
	return out
}

// TestS2CommentsPostTheNoteAndTickTheOKChangeRequests: the change requests
// of S2 whose verdict is OK get the note that nothing blocks and a tick in
// the description of their parent, which changes one byte; the ones that
// are BLOCKED get no note and no tick.
func TestS2CommentsPostTheNoteAndTickTheOKChangeRequests(t *testing.T) {
	w := s2(t)
	before := map[string]string{
		"core-lib":   w.PR("core-lib", 3).Body,
		"kmp-shared": w.PR("kmp-shared", 2).Body,
	}
	w.MustPlan(core.Comments)
	for _, c := range []struct {
		repo   string
		number int
		note   bool
		tick   string
	}{
		{"feature-lib", 1, true, "kmp-shared#2"},
		{"kmp-shared", 2, true, "core-lib#3"},
		{"core-lib", 3, false, ""},
		{"sample-app", 2, false, ""},
		{"sample-app", 7, true, ""},
	} {
		plan := w.ReadPlan(c.repo, c.number)
		notes, ticks := publishworld.Actions(plan, core.ActionSummaryComment), publishworld.Actions(plan, core.ActionRelatedTick)
		if (len(notes) == 1) != c.note || (len(ticks) == 1) != (c.tick != "") {
			t.Errorf("%s#%d: %d notes and %d ticks, want a note %v and a tick in %q", c.repo, c.number, len(notes), len(ticks), c.note, c.tick)
		}
		if c.note && (notes[0].Body != "Review completed: no blocking issues found!\n\n"+core.Embed(notes[0].Fingerprint) || !strings.HasPrefix(notes[0].Fingerprint, "acr:v1:ok:")) {
			t.Errorf("%s#%d: note %+v", c.repo, c.number, notes[0])
		}
		if c.tick != "" && !strings.HasSuffix(ticks[0].Parent.String(), "/"+c.tick) {
			t.Errorf("%s#%d: tick in %s, want %s", c.repo, c.number, ticks[0].Parent, c.tick)
		}
		if len(publishworld.Actions(plan, core.ActionIssueCreate)) != 0 {
			t.Errorf("%s#%d: an issue is planned for comments", c.repo, c.number)
		}
	}
	if len(w.Server.Writes()) != 0 {
		t.Fatal("planning wrote")
	}
	w.MustApply()
	if got := len(writesTo(w, "feature-lib")); got != 1 {
		t.Errorf("feature-lib got %d writes, want its note", got)
	}
	for repo, body := range before {
		after := w.PR(repo, map[string]int{"core-lib": 3, "kmp-shared": 2}[repo]).Body
		if repo == "kmp-shared" {
			// kmp-shared#2 lists feature-lib#1, ticked by the review.
			if diffBytes(body, after) != 1 && body != after {
				t.Errorf("%s description changed by %d bytes", repo, diffBytes(body, after))
			}
		}
	}
	if got := w.PR("kmp-shared", 2).Body; !strings.Contains(got, "- [x] acr-plugin-sandbox/feature-lib#1") {
		t.Errorf("the entry of feature-lib#1 in kmp-shared#2 is not ticked: %q", got)
	}
	if got := w.PR("core-lib", 3).Body; !strings.Contains(got, "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2") {
		t.Errorf("the entry of kmp-shared#2 in core-lib#3 is not ticked: %q", got)
	}
	if got := w.PR("sample-app", 7).Body; !strings.Contains(got, "- [ ] https://github.com/acr-plugin-sandbox/core-lib/pull/3") {
		t.Errorf("core-lib#3 is BLOCKED, so its entry in sample-app#7 is still empty: %q", got)
	}
	if got := w.PR("sample-app", 7).Comments; len(got) != 1 || got[0].Body != "Review completed: no blocking issues found!\n\n"+core.Embed("acr:v1:ok:e9009ec0") {
		t.Errorf("the root got %+v", got)
	}
	for _, n := range []struct {
		repo string
		n    int
	}{{"core-lib", 3}, {"sample-app", 2}} {
		for _, c := range w.PR(n.repo, n.n).Comments {
			t.Errorf("%s#%d is BLOCKED and got the note %q", n.repo, n.n, c.Body)
		}
	}
	onlyAllowedWrites(t, w)
	if got := w.State().Phase; got != state.Published {
		t.Errorf("phase %s", got)
	}
}

// TestOneByteChangesInTheParent: a tick changes the one byte of the
// checkbox and nothing else of the description.
func TestOneByteChangesInTheParent(t *testing.T) {
	w := s2(t)
	before := w.PR("core-lib", 3).Body
	w.MustPlan(core.Comments)
	w.MustApply()
	if got := diffBytes(before, w.PR("core-lib", 3).Body); got != 1 {
		t.Errorf("the description of core-lib#3 changed by %d bytes", got)
	}
}

func TestS2IssuesAndBoth(t *testing.T) {
	w := s2(t)
	w.MustPlan(core.Issues)
	for _, c := range []struct {
		repo   string
		number int
	}{{"feature-lib", 1}, {"kmp-shared", 2}} {
		if p := w.ReadPlan(c.repo, c.number); len(p.Actions) != 0 {
			t.Errorf("%s#%d: %d actions for issues, want none: it has no findings and issues write no note and no tick", c.repo, c.number, len(p.Actions))
		}
	}
	root := w.ReadPlan("sample-app", 7)
	if got := publishworld.Actions(root, core.ActionLabelCreate); len(got) != 1 || got[0].ID != 1 || got[0].Label.Name != "review" {
		t.Errorf("the label comes first: %+v", got)
	}
	creates := publishworld.Actions(root, core.ActionIssueCreate)
	if len(creates) != 2 || creates[0].Severity != core.Blocker || creates[1].Severity != core.Major || creates[0].Assignee != "sandbox-author" {
		t.Fatalf("issues = %+v, want the BLOCKER one for existing code and the MAJOR one, assigned to the author", creates)
	}
	if !strings.HasPrefix(creates[0].Body, "Review of #7 at `e9009ec0`: BLOCKER findings in existing code.\n\n### Existing code (not written in this change request)") ||
		!strings.HasSuffix(creates[0].Body, "<!-- acr:v1:issue:acr-plugin-sandbox/sample-app#7:blocker -->") {
		t.Errorf("BLOCKER issue body:\n%s", creates[0].Body)
	}
	w.MustApply()
	for _, r := range w.Server.Writes() {
		if strings.HasSuffix(r.Path, "/comments") || strings.HasSuffix(r.Path, "/reviews") || r.Method == "PATCH" {
			t.Errorf("issues wrote %s", r)
		}
	}
	issues := w.Server.Repo("acr-plugin-sandbox/sample-app").Issues
	if len(issues) != 4 {
		t.Fatalf("sample-app has %d issues, want 4 (two for #7, two for #2)", len(issues))
	}
	for _, is := range issues {
		if !is.Open || len(is.Labels) != 1 || is.Labels[0] != "review" || len(is.Assignees) != 1 || is.Assignees[0] != "sandbox-author" {
			t.Errorf("issue %+v", is)
		}
	}
	onlyAllowedWrites(t, w)

	both := s2(t)
	both.MustPlan(core.Both)
	both.MustApply()
	var comments, reviews, patches, issuesPosted int
	for _, r := range both.Server.Writes() {
		switch {
		case strings.HasSuffix(r.Path, "/comments"):
			comments++
		case strings.HasSuffix(r.Path, "/reviews"):
			reviews++
		case r.Method == "PATCH":
			patches++
		case strings.HasSuffix(r.Path, "/issues"):
			issuesPosted++
		}
	}
	if comments != 3 || reviews != 2 || patches != 2 || issuesPosted != 5 {
		t.Errorf("both wrote %d notes, %d reviews, %d ticks and %d issues, want 3, 2, 2 and 5", comments, reviews, patches, issuesPosted)
	}
	onlyAllowedWrites(t, both)
}

// TestHeadChangedBetweenPlanAndApply: a change request with new commits gets
// nothing, the others go ahead, and the command says which to review again.
func TestHeadChangedBetweenPlanAndApply(t *testing.T) {
	w := s2(t)
	w.MustPlan(core.Both)
	w.PR("core-lib", 3).Head = strings.Repeat("c", 40)
	applied, err := w.Apply()
	f := failure(t, err, review.KindHeadChanged)
	if len(f.Details) != 1 || !strings.Contains(f.Details[0], "core-lib#3") || !strings.Contains(f.Details[0], "df06e9a9") || !strings.Contains(f.Details[0], "cccccccc") {
		t.Errorf("details = %v, want core-lib#3 with the two heads", f.Details)
	}
	if applied == nil || !strings.Contains(applied.Text, "nothing was written: head-changed") {
		t.Errorf("text = %v", applied)
	}
	if got := writesTo(w, "core-lib"); len(got) != 0 {
		t.Errorf("core-lib got %v after its head changed", got)
	}
	if got := len(writesTo(w, "feature-lib")); got != 1 {
		t.Errorf("feature-lib got %d writes, want its note: the other change requests go ahead", got)
	}
	if got := len(writesTo(w, "kmp-shared")); got != 2 {
		t.Errorf("kmp-shared got %d writes, want its note and the tick of feature-lib#1: the other change requests go ahead", got)
	}
	if got := len(writesTo(w, "sample-app")); got == 0 {
		t.Error("sample-app got nothing")
	}
	pub, ok := w.Published("core-lib", 3)
	if !ok || pub.Blocked != core.StatusHeadChanged {
		t.Fatalf("published = %+v", pub)
	}
	for _, o := range pub.Outcomes {
		if o.Status != core.StatusHeadChanged {
			t.Errorf("outcome %+v", o)
		}
	}
	// kmp-shared#2 is OK, but its parent core-lib#3 has new commits: its
	// description is not written.
	var tick core.Outcome
	pubKmp, _ := w.Published("kmp-shared", 2)
	for _, o := range pubKmp.Outcomes {
		if o.ID == 2 {
			tick = o
		}
	}
	if tick.Status != core.StatusNotEligible || !strings.Contains(tick.Detail, "head-changed") {
		t.Errorf("the tick of kmp-shared#2 in core-lib#3 = %+v, want not-eligible: the parent changed", tick)
	}
	onlyAllowedWrites(t, w)
}

func TestHeadChangedBeforeThePlan(t *testing.T) {
	w := s2(t)
	w.PR("sample-app", 2).Head = strings.Repeat("d", 40)
	planned, err := w.Plan(core.Both)
	failure(t, err, review.KindHeadChanged)
	if planned == nil || len(planned.Plans) != 5 {
		t.Fatalf("planned = %+v: the plans are written all the same", planned)
	}
	p := w.ReadPlan("sample-app", 2)
	if len(p.Actions) != 0 || len(publishworld.SkippedWith(p, core.StatusHeadChanged)) != 1 {
		t.Errorf("plan = %+v, want no action and head-changed", p)
	}
	if !strings.Contains(planned.Text, "skip   head-changed") {
		t.Errorf("the summary does not say it:\n%s", planned.Text)
	}
	if got := w.State().Phase; got != state.Planned {
		t.Errorf("phase %s, want planned", got)
	}
	// Applying writes the others, and says again which to review.
	if _, err := w.Apply(); err == nil {
		t.Error("apply did not say that the head changed")
	}
	if got := writesTo(w, "sample-app"); len(got) != 4 {
		t.Errorf("sample-app got %d writes, want the 4 of the root only: %v", len(got), got)
	}
}

func TestClosedMergedAndOwnChangeRequestsGetNothing(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(w *publishworld.World)
		want   core.PlanStatus
	}{
		{"closed", func(w *publishworld.World) { w.PR("kmp-shared", 2).State = "closed" }, core.StatusNotOpen},
		{"merged", func(w *publishworld.World) { pr := w.PR("kmp-shared", 2); pr.State, pr.Merged = "closed", true }, core.StatusNotOpen},
		{"own", func(w *publishworld.World) { w.PR("kmp-shared", 2).Author = publishworld.Login }, core.StatusOwnCR},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := s2(t)
			c.change(w)
			w.MustPlan(core.Both)
			plan := w.ReadPlan("kmp-shared", 2)
			if len(plan.Actions) != 0 || len(publishworld.SkippedWith(plan, c.want)) != 1 {
				t.Errorf("plan = %+v, want nothing and %s", plan, c.want)
			}
			// Its child feature-lib#1 is not ticked in it, and it is not
			// ticked in its parent.
			if got := publishworld.Actions(w.ReadPlan("feature-lib", 1), core.ActionRelatedTick); len(got) != 0 {
				t.Errorf("feature-lib#1 is ticked in %s: %v", c.want, got)
			}
			applied := w.MustApply()
			if got := writesTo(w, "kmp-shared"); len(got) != 0 {
				t.Errorf("kmp-shared got %v", got)
			}
			if got := w.PR("core-lib", 3).Body; strings.Contains(got, "[x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2") && !strings.Contains(w.PR("core-lib", 3).Body, "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2") {
				t.Errorf("core-lib#3 = %q", got)
			}
			if !applied.Done {
				t.Errorf("the run is not published:\n%s", applied.Text)
			}
			pub, _ := w.Published("kmp-shared", 2)
			if pub.Blocked != c.want {
				t.Errorf("blocked = %q, want %q", pub.Blocked, c.want)
			}
			if !strings.Contains(applied.Text, "nothing was written: "+string(c.want)) {
				t.Errorf("the report does not say it:\n%s", applied.Text)
			}
			onlyAllowedWrites(t, w)
		})
	}
}

func TestOwnChangeRequestIsNeverTickedInItsParent(t *testing.T) {
	w := s2(t)
	// kmp-shared#2 is OK and listed in core-lib#3; made the account's own it
	// gets nothing, and its entry stays empty.
	w.PR("kmp-shared", 2).Author = publishworld.Login
	before := w.PR("core-lib", 3).Body
	w.MustPlan(core.Comments)
	w.MustApply()
	if got := w.PR("core-lib", 3).Body; got != before {
		t.Errorf("core-lib#3 changed: %q", got)
	}
}

func TestPlanOfTheRunIsRegeneratedAndOldOutcomesAreStale(t *testing.T) {
	w := s2(t)
	w.MustPlan(core.Comments)
	h1 := w.State().ChangeRequests[0].PlanSHA256
	w.MustPlan(core.Both)
	st := w.State()
	if st.Phase != state.Planned || st.ChangeRequests[0].PlanSHA256 == h1 {
		t.Errorf("phase %s, hash kept %v: planned -> planned makes a new plan", st.Phase, st.ChangeRequests[0].PlanSHA256 == h1)
	}
	for _, e := range st.ChangeRequests {
		if (e.Status == core.Skipped) != (e.PlanSHA256 == "") {
			t.Errorf("%s: skipped %v with hash %q", e.CR, e.Status == core.Skipped, e.PlanSHA256)
		}
	}
	w.MustApply()
	if _, err := w.Plan(core.Comments); err == nil {
		t.Error("a published run was planned again")
	} else {
		failure(t, err, review.KindState)
	}
}

func TestApplyNeedsAPlan(t *testing.T) {
	w := s2(t)
	_, err := w.Apply()
	f := failure(t, err, review.KindState)
	if !strings.Contains(f.Why, "finalized") || len(w.Server.Requests()) != 0 {
		t.Errorf("failure %+v, requests %v: nothing is read or written without a plan", f, w.Server.Requests())
	}
	w.MustPlan(core.Comments)
	w.MustApply()
	written := len(w.Server.Writes())
	_, err = w.Apply()
	failure(t, err, review.KindState)
	if got := len(w.Server.Writes()); got != written || written == 0 {
		t.Errorf("%d writes after %d: the second apply wrote again", got, written)
	}
}

// TestTamperedOrMissingPlansWriteNothing: publish --apply executes exactly
// the stored plans. Any plan that is not the one whose hash state.json holds
// stops it before anything is written, for every change request.
func TestTamperedOrMissingPlansWriteNothing(t *testing.T) {
	for _, c := range []struct {
		name   string
		tamper func(t *testing.T, w *publishworld.World)
		why    string
	}{
		{"a body is edited", func(t *testing.T, w *publishworld.World) {
			path := w.PlanFile("feature-lib", 1)
			data, _ := os.ReadFile(path)
			if err := os.WriteFile(path, []byte(strings.Replace(string(data), "no blocking issues", "no blocking issues at all", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not the one that was made"},
		{"an action is added", func(t *testing.T, w *publishworld.World) {
			p := w.ReadPlan("feature-lib", 1)
			p.Actions = append(p.Actions, core.Action{ID: 3, Kind: core.ActionSummaryComment, Body: "extra", Fingerprint: "acr:v1:ok:00000000"})
			data, _ := core.EncodePlan(p)
			if err := os.WriteFile(w.PlanFile("feature-lib", 1), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not the one that was made"},
		{"a plan is deleted", func(t *testing.T, w *publishworld.World) {
			if err := os.Remove(w.PlanFile("sample-app", 7)); err != nil {
				t.Fatal(err)
			}
		}, "missing"},
		{"a plan is garbage", func(t *testing.T, w *publishworld.World) {
			if err := os.WriteFile(w.PlanFile("sample-app", 2), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not the one that was made"},
		{"state.json lost the hash", func(t *testing.T, w *publishworld.World) {
			st := w.State()
			for i := range st.ChangeRequests {
				if st.ChangeRequests[i].Status != core.Skipped {
					st.ChangeRequests[i].PlanSHA256 = ""
					break
				}
			}
			w.SetState(st)
		}, "records no plan"},
		{"a plan of another run is copied over", func(t *testing.T, w *publishworld.World) {
			other := s2(t)
			other.MustPlan(core.Comments)
			data, err := os.ReadFile(other.PlanFile("sample-app", 7))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(w.PlanFile("sample-app", 7), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}, "not the one that was made"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := s2(t)
			w.MustPlan(core.Both)
			c.tamper(t, w)
			asked := len(w.Server.Requests())
			applied, err := w.Apply()
			f := failure(t, err, review.KindState)
			if applied != nil || len(f.Details) == 0 || !strings.Contains(strings.Join(f.Details, "\n"), c.why) {
				t.Errorf("applied %v, details %v, want one that says %q", applied, f.Details, c.why)
			}
			if got := w.Server.Writes(); len(got) != 0 {
				t.Errorf("wrote %v", got)
			}
			if got := w.Server.Requests()[asked:]; len(got) != 0 {
				t.Errorf("the platform was asked %v before the plans were checked", got)
			}
			if got := w.State().Phase; got != state.Planned {
				t.Errorf("phase %s", got)
			}
			if _, err := os.Stat(filepath.Join(w.Package("sample-app", 7).Dir, state.PublishedFile)); err == nil {
				t.Error("published.json exists")
			}
		})
	}
}

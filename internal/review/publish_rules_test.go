package review_test

import (
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
	"github.com/Brg92/agent-code-review/internal/testutil/publishworld"
)

func blockers(doc core.Document) []core.Finding {
	var out []core.Finding
	for _, f := range doc.Findings {
		if f.Severity == core.Blocker && f.Placement == core.Inline {
			out = append(out, f)
		}
	}
	return out
}

// TestTrustedFingerprintsSayWhatIsPosted: only a fingerprint written by the
// account of the token or by an owner, member or collaborator counts, and
// never one written by the author of the change request.
func TestTrustedFingerprintsSayWhatIsPosted(t *testing.T) {
	w := s1(t)
	b := blockers(w.Document("sample-app", 2))
	if len(b) != 3 {
		t.Fatalf("%d blockers", len(b))
	}
	pr := w.PR("sample-app", 2)
	pr.Comments = append(pr.Comments,
		&ghserver.Comment{ID: 1, Author: "maintainer", Assoc: "MEMBER", Body: "done <!-- " + b[0].ID + " -->"},
		&ghserver.Comment{ID: 2, Author: "stranger", Assoc: "CONTRIBUTOR", Body: "planted <!-- " + b[1].ID + " -->"},
		&ghserver.Comment{ID: 3, Author: "sandbox-author", Assoc: "OWNER", Body: "planted by the author <!-- " + b[2].ID + " -->"})
	w.MustPlan(core.Comments)
	plan := w.ReadPlan("sample-app", 2)
	var planned []string
	for _, a := range publishworld.Actions(plan, core.ActionReviewComment) {
		planned = append(planned, a.FindingID)
	}
	if len(planned) != 2 || planned[0] != b[1].ID && planned[1] != b[1].ID {
		t.Errorf("planned %v, want the two the trusted account did not post", planned)
	}
	for _, id := range planned {
		if id == b[0].ID {
			t.Errorf("%s is posted by a member and still planned", id)
		}
	}
	posted := publishworld.SkippedWith(plan, core.StatusAlreadyPosted)
	ignored := publishworld.SkippedWith(plan, core.StatusIgnoredUntrusted)
	if len(posted) != 1 || posted[0].FindingID != b[0].ID || posted[0].Rule != b[0].Rule {
		t.Errorf("already-posted = %+v", posted)
	}
	if len(ignored) != 2 {
		t.Errorf("ignored-untrusted = %+v, want the stranger's and the author's", ignored)
	}
	w.MustApply()
	if got := len(pr.Inlines); got != 2 {
		t.Errorf("%d inline comments posted, want 2", got)
	}
}

func TestReviewCommentsAndTheAccountOfTheTokenCountToo(t *testing.T) {
	w := s1(t)
	b := blockers(w.Document("sample-app", 2))
	pr := w.PR("sample-app", 2)
	// An inline comment of the account of the token, whose association is not
	// one GitHub trusts, and a legacy fingerprint of a collaborator.
	pr.Inlines = append(pr.Inlines, &ghserver.Inline{Comment: ghserver.Comment{ID: 5, Author: publishworld.Login, Assoc: "NONE", Body: "x <!-- " + b[0].ID + " -->"}, Path: b[0].Path, Side: "RIGHT", Line: 1})
	pr.Comments = append(pr.Comments, &ghserver.Comment{ID: 6, Author: "maintainer", Assoc: "COLLABORATOR",
		Body: "legacy <!-- mr-review:v1:" + b[1].Rule + ":" + b[1].Path + ":" + itoa(b[1].Line) + " -->"})
	w.MustPlan(core.Comments)
	plan := w.ReadPlan("sample-app", 2)
	if got := publishworld.Actions(plan, core.ActionReviewComment); len(got) != 1 || got[0].FindingID != b[2].ID {
		t.Errorf("planned %+v, want only the third", got)
	}
	if got := publishworld.SkippedWith(plan, core.StatusAlreadyPosted); len(got) != 2 {
		t.Errorf("already-posted = %+v", got)
	}
}

func TestOKNoteIsPostedOnce(t *testing.T) {
	w := s2(t)
	pr := w.PR("sample-app", 7)
	pr.Comments = append(pr.Comments, &ghserver.Comment{ID: 1, Author: "maintainer", Assoc: "OWNER", Body: "ok " + core.Embed("acr:v1:ok:e9009ec0")})
	w.MustPlan(core.Comments)
	plan := w.ReadPlan("sample-app", 7)
	if len(plan.Actions) != 0 || len(publishworld.SkippedWith(plan, core.StatusAlreadyPosted)) != 1 {
		t.Errorf("plan = %+v", plan)
	}
	// The note of an earlier head does not count for this one, and neither does
	// one of an account that is not trusted.
	w2 := s2(t)
	pr2 := w2.PR("sample-app", 7)
	pr2.Comments = append(pr2.Comments,
		&ghserver.Comment{ID: 1, Author: "maintainer", Assoc: "OWNER", Body: core.Embed("acr:v1:ok:00000000")},
		&ghserver.Comment{ID: 2, Author: "stranger", Assoc: "NONE", Body: core.Embed("acr:v1:ok:e9009ec0")},
		&ghserver.Comment{ID: 3, Author: "maintainer", Assoc: "OWNER", Body: core.Embed("mr-review:v1:ok:e9009ec0")})
	w2.MustPlan(core.Comments)
	if got := w2.ReadPlan("sample-app", 7); len(publishworld.Actions(got, core.ActionSummaryComment)) != 0 {
		t.Errorf("the legacy note of a maintainer is not recognised: %+v", got)
	}
	w3 := s2(t)
	w3.PR("sample-app", 7).Comments = []*ghserver.Comment{{ID: 2, Author: "stranger", Assoc: "NONE", Body: core.Embed("acr:v1:ok:e9009ec0")}}
	w3.MustPlan(core.Comments)
	got := w3.ReadPlan("sample-app", 7)
	if len(publishworld.Actions(got, core.ActionSummaryComment)) != 1 || len(publishworld.SkippedWith(got, core.StatusIgnoredUntrusted)) != 1 {
		t.Errorf("plan = %+v, want the note and ignored-untrusted", got)
	}
}

// issueOf finds the issue of a severity in the plan of a change request.
func issueOf(plan core.Plan, sev core.Severity) (core.Action, bool) {
	for _, a := range plan.Actions {
		if (a.Kind == core.ActionIssueCreate || a.Kind == core.ActionIssueUpdate) && a.Severity == sev {
			return a, true
		}
	}
	return core.Action{}, false
}

func TestIssuesAreRewrittenReplacedOrLeftAlone(t *testing.T) {
	const project = "acr-plugin-sandbox/sample-app"
	fp := func(sev string) string { return core.Embed("acr:v1:issue:" + project + "#2:" + sev) }

	t.Run("an open issue is rewritten, a closed one is replaced", func(t *testing.T) {
		w := s1(t)
		repo := w.Server.Repo(project)
		repo.Labels["review"] = true
		open := repo.AddIssue(publishworld.Login, "COLLABORATOR", "Our title", "old body "+fp("blocker"), "review")
		closed := repo.AddIssue(publishworld.Login, "COLLABORATOR", "Old major", "old body "+fp("major"), "review")
		closed.Open = false
		w.MustPlan(core.Issues)
		plan := w.ReadPlan("sample-app", 2)
		blocker, _ := issueOf(plan, core.Blocker)
		major, _ := issueOf(plan, core.Major)
		if blocker.Kind != core.ActionIssueUpdate || blocker.Number != open.Number || blocker.Title != "Our title" {
			t.Errorf("BLOCKER = %+v, want a rewrite of #%d that keeps its title", blocker, open.Number)
		}
		if major.Kind != core.ActionIssueCreate {
			t.Errorf("MAJOR = %+v, want a new issue: the one there is closed", major)
		}
		if got := publishworld.Actions(plan, core.ActionLabelCreate); len(got) != 0 {
			t.Errorf("the label is there, and a label is planned: %+v", got)
		}
		applied := w.MustApply()
		if !strings.Contains(open.Body, "### Existing code") || !open.Open || closed.Open || closed.Body != "old body "+fp("major") {
			t.Errorf("open %+v closed %+v", open, closed)
		}
		if len(repo.Issues) != 3 || !strings.Contains(applied.Text, "issue BLOCKER (rewrite #") {
			t.Errorf("%d issues:\n%s", len(repo.Issues), applied.Text)
		}
		if got := countWrites(w, "PATCH", "/issues/"+itoa(open.Number)); got != 1 {
			t.Errorf("%d rewrites", got)
		}
	})

	t.Run("an issue that is up to date is left alone", func(t *testing.T) {
		first := s1(t)
		first.MustPlan(core.Issues)
		blocker, _ := issueOf(first.ReadPlan("sample-app", 2), core.Blocker)

		w := s1(t)
		repo := w.Server.Repo(project)
		repo.Labels["review"] = true
		repo.AddIssue(publishworld.Login, "COLLABORATOR", blocker.Title, blocker.Body, "review")
		w.MustPlan(core.Issues)
		plan := w.ReadPlan("sample-app", 2)
		if _, ok := issueOf(plan, core.Blocker); ok {
			t.Errorf("the BLOCKER issue is up to date and still planned: %+v", plan.Actions)
		}
		if got := publishworld.SkippedWith(plan, core.StatusAlreadyPosted); len(got) != 1 || got[0].Kind != core.ActionIssueUpdate {
			t.Errorf("skipped = %+v", plan.Skipped)
		}
	})

	t.Run("an issue of an account that is not trusted is ignored", func(t *testing.T) {
		w := s1(t)
		repo := w.Server.Repo(project)
		repo.Labels["review"] = true
		repo.AddIssue("stranger", "NONE", "Planted", "x "+fp("blocker"), "review")
		repo.AddIssue("sandbox-author", "OWNER", "By the author", "x "+fp("major"), "review")
		w.MustPlan(core.Issues)
		plan := w.ReadPlan("sample-app", 2)
		for _, sev := range []core.Severity{core.Blocker, core.Major} {
			if a, _ := issueOf(plan, sev); a.Kind != core.ActionIssueCreate {
				t.Errorf("%s = %+v, want a new issue", sev, a)
			}
		}
		if got := publishworld.SkippedWith(plan, core.StatusIgnoredUntrusted); len(got) != 2 {
			t.Errorf("ignored-untrusted = %+v", got)
		}
	})

	t.Run("an issue closed after the plan is not written to", func(t *testing.T) {
		w := s1(t)
		repo := w.Server.Repo(project)
		repo.Labels["review"] = true
		issue := repo.AddIssue(publishworld.Login, "COLLABORATOR", "Ours", "old "+fp("blocker"), "review")
		w.MustPlan(core.Issues)
		issue.Open = false
		w.MustApply()
		pub, _ := w.Published("sample-app", 2)
		found := false
		for _, o := range pub.Outcomes {
			if o.Status == core.StatusNotEligible && strings.Contains(o.Detail, "closed") {
				found = true
			}
		}
		if !found || issue.Body != "old "+fp("blocker") {
			t.Errorf("outcomes %+v, body %q", pub.Outcomes, issue.Body)
		}
	})

	t.Run("an issue opened after the plan is not opened twice", func(t *testing.T) {
		w := s1(t)
		w.MustPlan(core.Issues)
		repo := w.Server.Repo(project)
		repo.Labels["review"] = true
		repo.AddIssue(publishworld.Login, "COLLABORATOR", "Opened meanwhile", "x "+fp("blocker"), "review")
		w.MustApply()
		if got := countWrites(w, "POST", "/issues"); got != 1 {
			t.Errorf("%d issues opened, want only the MAJOR one", got)
		}
	})
}

func TestLabelIsAnActionOfThePlan(t *testing.T) {
	w := s1(t)
	w.MustPlan(core.Issues)
	plan := w.ReadPlan("sample-app", 2)
	label := publishworld.Actions(plan, core.ActionLabelCreate)
	if len(label) != 1 || label[0].ID != 1 || label[0].Label.Name != "review" || label[0].Label.Color != "5319e7" {
		t.Fatalf("label = %+v", label)
	}
	// Created by someone after the plan: the apply does not fail.
	w.Server.Repo("acr-plugin-sandbox/sample-app").Labels["review"] = true
	w.MustApply()
	pub, _ := w.Published("sample-app", 2)
	if pub.Outcomes[0].Status != core.StatusAlreadyPosted {
		t.Errorf("outcomes %+v", pub.Outcomes)
	}
	if countWrites(w, "POST", "/labels") != 0 {
		t.Error("the label was created again")
	}
	// Without issues there is no label.
	c := s1(t)
	c.MustPlan(core.Comments)
	if got := publishworld.Actions(c.ReadPlan("sample-app", 2), core.ActionLabelCreate); len(got) != 0 {
		t.Errorf("a label for comments: %+v", got)
	}
}

func TestAuthorWhoCannotBeAssignedGetsAnIssueWithoutAssignee(t *testing.T) {
	w := s1(t)
	w.Server.Repo("acr-plugin-sandbox/sample-app").Assignable = map[string]bool{}
	w.MustPlan(core.Issues)
	applied := w.MustApply()
	pub, _ := w.Published("sample-app", 2)
	n := 0
	for _, o := range pub.Outcomes {
		if o.Status == core.StatusPostedUnassigned {
			n++
			if !strings.Contains(o.Detail, "cannot be assigned") || o.Number == 0 {
				t.Errorf("outcome %+v", o)
			}
		}
	}
	if n != 2 || !strings.Contains(applied.Text, "posted-unassigned") {
		t.Errorf("%d unassigned issues:\n%s", n, applied.Text)
	}
	for _, is := range w.Server.Repo("acr-plugin-sandbox/sample-app").Issues {
		if len(is.Assignees) != 0 {
			t.Errorf("issue %+v is assigned", is)
		}
	}
	for _, r := range w.Server.Writes() {
		if strings.HasSuffix(r.Path, "/issues") && strings.Contains(r.Body, "assignees") {
			t.Errorf("an assignee was sent: %s", r.Body)
		}
	}
}

func TestBodiesLongerThanThePlatformAcceptsAreBlocked(t *testing.T) {
	w := s1(t)
	doc := w.Document("sample-app", 2)
	long := strings.Repeat("é", 65537)
	var major, blocker int
	for i, f := range doc.Findings {
		switch {
		case f.Severity == core.Major && major == 0:
			doc.Findings[i].Why, major = long, 1
		case f.Severity == core.Blocker && blocker == 0:
			doc.Findings[i].Why, blocker = long, 1
		}
	}
	w.SetDocument("sample-app", 2, doc)
	w.MustPlan(core.Both)
	plan := w.ReadPlan("sample-app", 2)
	tooLong := publishworld.SkippedWith(plan, core.StatusTooLong)
	if len(tooLong) != 2 {
		t.Fatalf("too-long = %+v", plan.Skipped)
	}
	for _, s := range tooLong {
		if !strings.Contains(s.Detail, "65536") {
			t.Errorf("%+v", s)
		}
	}
	for _, a := range plan.Actions {
		if len([]rune(a.Body)) > 65536 {
			t.Errorf("a body of %d characters is planned", len([]rune(a.Body)))
		}
	}
	if len(plan.Actions) == 0 {
		t.Error("the other actions were dropped")
	}
	w.MustApply()
}

func addThread(pr *ghserver.PR, author, assoc, body string, resolved bool) {
	pr.Inlines = append(pr.Inlines, &ghserver.Inline{Comment: ghserver.Comment{ID: len(pr.Inlines) + 100, Author: author, Assoc: assoc, Body: body}, Path: "a/B.kt", Side: "RIGHT", Line: 1, Resolved: resolved})
}

const someBlocker = "acr:v1:logic-defect:a/B.kt:0123456789ab"

func tickDetail(plan core.Plan) (core.PlanSkip, bool) {
	for _, s := range plan.Skipped {
		if s.Kind == core.ActionRelatedTick {
			return s, true
		}
	}
	return core.PlanSkip{}, false
}

// TestTickEligibility: the entry of a change request is ticked only when its
// verdict is OK, no unresolved thread of ours starts with a trusted blocker,
// and the checkbox in the parent is still empty.
func TestTickEligibility(t *testing.T) {
	cases := []struct {
		name     string
		change   func(w *publishworld.World)
		eligible bool
		why      string
	}{
		{"nothing in the way", func(w *publishworld.World) {}, true, ""},
		{"an unresolved blocker of ours", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), publishworld.Login, "COLLABORATOR", "x <!-- "+someBlocker+" -->", false)
		}, false, "unresolved review thread"},
		{"an unresolved blocker by a maintainer", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), "maintainer", "MEMBER", "x <!-- "+someBlocker+" -->", false)
		}, false, "unresolved review thread"},
		{"a resolved blocker", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), publishworld.Login, "COLLABORATOR", "x <!-- "+someBlocker+" -->", true)
		}, true, ""},
		{"an unresolved fingerprint planted by a stranger", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), "stranger", "CONTRIBUTOR", "x <!-- "+someBlocker+" -->", false)
		}, true, ""},
		{"an unresolved fingerprint planted by the author", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), "sandbox-author", "OWNER", "x <!-- "+someBlocker+" -->", false)
		}, true, ""},
		{"an unresolved thread without a fingerprint", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), "maintainer", "MEMBER", "Please rename this.", false)
		}, true, ""},
		{"the box is already ticked", func(w *publishworld.World) {
			pr := w.PR("core-lib", 3)
			pr.Body = strings.Replace(pr.Body, "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
		}, false, "already ticked"},
		{"there is no checkbox", func(w *publishworld.World) {
			pr := w.PR("core-lib", 3)
			pr.Body = strings.Replace(pr.Body, "- [ ] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
			pr.Body = strings.Replace(pr.Body, "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
		}, false, "no checkbox"},
		{"the parent is closed", func(w *publishworld.World) { w.PR("core-lib", 3).State = "closed" }, false, "not-open"},
		{"the parent is the account's", func(w *publishworld.World) { w.PR("core-lib", 3).Author = publishworld.Login }, false, "own-cr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := s2(t)
			// The recorded description has the entry ticked: it was reviewed
			// before. Start from an empty box unless the case says otherwise.
			pr := w.PR("core-lib", 3)
			pr.Body = strings.Replace(pr.Body, "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- [ ] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
			if c.name == "the box is already ticked" {
				pr.Body = strings.Replace(pr.Body, "- [ ] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
			}
			c.change(w)
			w.MustPlan(core.Comments)
			plan := w.ReadPlan("kmp-shared", 2)
			ticks := publishworld.Actions(plan, core.ActionRelatedTick)
			skip, skipped := tickDetail(plan)
			if c.eligible && (len(ticks) != 1 || skipped) {
				t.Fatalf("plan = %+v, want the tick", plan)
			}
			if !c.eligible {
				if len(ticks) != 0 || !skipped || skip.Status != core.StatusNotEligible || !strings.Contains(skip.Detail, c.why) {
					t.Fatalf("plan = %+v, want not-eligible: %s", plan, c.why)
				}
			}
		})
	}
}

func TestTickIsCheckedAgainWhenApplying(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(w *publishworld.World)
		status core.PlanStatus
	}{
		{"someone ticked it", func(w *publishworld.World) {
			pr := w.PR("core-lib", 3)
			pr.Body = strings.Replace(pr.Body, "- [ ] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
		}, core.StatusAlreadyPosted},
		{"a blocker was commented", func(w *publishworld.World) {
			addThread(w.PR("kmp-shared", 2), publishworld.Login, "COLLABORATOR", "x <!-- "+someBlocker+" -->", false)
		}, core.StatusNotEligible},
		{"the entry was removed", func(w *publishworld.World) { w.PR("core-lib", 3).Body = "No related changes now." }, core.StatusNotEligible},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := s2(t)
			pr := w.PR("core-lib", 3)
			pr.Body = strings.Replace(pr.Body, "- [x] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", "- [ ] https://github.com/acr-plugin-sandbox/kmp-shared/pull/2", 1)
			w.MustPlan(core.Comments)
			c.change(w)
			before := pr.Body
			w.MustApply()
			pub, _ := w.Published("kmp-shared", 2)
			var got core.Outcome
			for _, o := range pub.Outcomes {
				if o.ID == 2 {
					got = o
				}
			}
			if got.Status != c.status {
				t.Errorf("outcome %+v, want %s", got, c.status)
			}
			if c.status != core.StatusPosted && pr.Body != before {
				t.Errorf("the description changed: %q", pr.Body)
			}
		})
	}
}

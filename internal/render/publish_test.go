package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

func finding(rule string, sev core.Severity, path string, line int) core.Finding {
	return core.Finding{
		ID: core.FindingFingerprint(rule, path, "val x = 1", 0), Rule: rule, Severity: sev, Path: path, Side: core.Right, Line: line,
		Title: "Title of " + rule, Why: "It goes wrong.", Fix: "Do it right.", References: []core.Reference{},
		Placement: core.Inline, Anchor: &core.Anchor{Kind: core.AnchorAdded, Side: core.Right, Line: line},
	}
}

func TestInlineComment(t *testing.T) {
	plain := finding("global-scope", core.Blocker, "app/A.kt", 12)
	want := "**BLOCKER** · `global-scope` — Title of global-scope\n\nIt goes wrong.\n\n**Fix:** Do it right.\n\n<!-- " + plain.ID + " -->"
	if got := InlineComment(plain, "suggestion"); got != want {
		t.Errorf("InlineComment = %q, want %q", got, want)
	}

	relocated := plain
	relocated.Line = 27
	relocated.Anchor = &core.Anchor{Kind: core.AnchorRelocated, Side: core.Right, Line: 16, RequestedLine: 27}
	got := InlineComment(relocated, "suggestion")
	if !strings.HasPrefix(got, "Refers to line 27 of `app/A.kt`, outside the changed lines.\n\n**BLOCKER**") {
		t.Errorf("a relocated finding starts with %q", got[:min(len(got), 100)])
	}

	suggested := plain
	text := "val x = 2\nval y = `3`"
	suggested.Suggestion = &text
	got = InlineComment(suggested, "suggestion")
	if !strings.Contains(got, "**Fix:** Do it right.\n\n```suggestion\nval x = 2\nval y = `3`\n```\n\n<!-- ") {
		t.Errorf("a suggestion is a suggestion block after the fix: %q", got)
	}
	ticks := "a ``` b"
	suggested.Suggestion = &ticks
	if got := InlineComment(suggested, "suggestion"); !strings.Contains(got, "````suggestion\na ``` b\n````\n") {
		t.Errorf("the fence is longer than the backticks of the text: %q", got)
	}
	empty := ""
	suggested.Suggestion = &empty
	if got := InlineComment(suggested, "suggestion"); !strings.Contains(got, "```suggestion\n```\n") {
		t.Errorf("an empty suggestion removes the lines: %q", got)
	}
	suggested.Suggestion = &text
	if got := InlineComment(suggested, ""); strings.Contains(got, "suggestion") || !strings.Contains(got, "```\nval x = 2") {
		t.Errorf("a platform without suggestions gets a plain code block: %q", got)
	}

	withRefs := plain
	withRefs.References = []core.Reference{{Project: "github:github.com/acme/lib", Ref: "main", Path: "src/Session.kt", Line: 30}}
	if got := InlineComment(withRefs, "suggestion"); !strings.Contains(got, "**See also:**\n- `acme/lib` `src/Session.kt:30` on `main`\n\n<!-- ") {
		t.Errorf("references: %q", got)
	}
}

func TestOKNote(t *testing.T) {
	want := "Review completed: no blocking issues found!\n\n<!-- acr:v1:ok:76ae5f13 -->"
	if got := OKNote("76ae5f13ca1575b65fe1567c471e8a7b51a00af4"); got != want {
		t.Errorf("OKNote = %q, want %q", got, want)
	}
}

func TestIssueTitle(t *testing.T) {
	cr := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}
	tests := map[core.Severity]string{
		core.Blocker: "Review of #7: BLOCKER findings in existing code",
		core.Major:   "Review of #7: MAJOR findings",
		core.Minor:   "Review of #7: MINOR findings",
	}
	for s, want := range tests {
		if got := IssueTitle(cr, s); got != want {
			t.Errorf("IssueTitle(%s) = %q, want %q", s, got, want)
		}
	}
}

func TestIssueBody(t *testing.T) {
	cr := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}
	head := "76ae5f13ca1575b65fe1567c471e8a7b51a00af4"
	own := finding("missing-tests", core.Major, "app/A.kt", 3)
	own2 := finding("no-double-bang", core.Major, "app/B.kt", 9)
	own2.Why = "Line one.\nLine two."
	old := finding("global-scope", core.Major, "app/C.kt", 20)
	old.Crash = true

	got := IssueBody(cr, head, core.Major, []core.Finding{own, own2}, []core.Finding{old})
	want := "Review of #7 at `76ae5f13`: MAJOR findings.\n\n" +
		"- **Title of missing-tests** (`missing-tests`) — `app/A.kt:3`\n  It goes wrong.\n  **Fix:** Do it right.\n  <!-- " + core.ItemFingerprint(own.ID) + " -->\n" +
		"- **Title of no-double-bang** (`no-double-bang`) — `app/B.kt:9`\n  Line one.\n  Line two.\n  **Fix:** Do it right.\n  <!-- " + core.ItemFingerprint(own2.ID) + " -->\n\n" +
		"### Existing code (not written in this change request)\n\n" +
		"- **Title of global-scope** (`global-scope`) — `app/C.kt:20` — crash\n  It goes wrong.\n  **Fix:** Do it right.\n  <!-- " + core.ItemFingerprint(old.ID) + " -->\n\n" +
		"<!-- acr:v1:issue:acme/app#7:major -->"
	if got != want {
		t.Errorf("IssueBody =\n%s\nwant\n%s", got, want)
	}

	// The BLOCKER issue is about existing code only: the findings of the
	// change request itself are inline comments.
	got = IssueBody(cr, head, core.Blocker, nil, []core.Finding{old})
	if strings.Contains(got, "- **Title of missing-tests**") || !strings.HasPrefix(got, "Review of #7 at `76ae5f13`: BLOCKER findings in existing code.\n\n### Existing code (not written in this change request)\n\n- **Title of global-scope**") ||
		!strings.HasSuffix(got, "<!-- acr:v1:issue:acme/app#7:blocker -->") {
		t.Errorf("the BLOCKER issue = %q", got)
	}

	// Without existing code there is no such section.
	if got := IssueBody(cr, head, core.Minor, []core.Finding{own}, nil); strings.Contains(got, "Existing code") || !strings.HasSuffix(got, "<!-- acr:v1:issue:acme/app#7:minor -->") {
		t.Errorf("the MINOR issue = %q", got)
	}
}

// TestIssueBodyCarriesTheFingerprintsThatFindItAgain: what publish reads
// back from the body is what it wrote.
func TestIssueBodyCarriesTheFingerprintsThatFindItAgain(t *testing.T) {
	cr := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}
	f := finding("missing-tests", core.Major, "app/My File.kt", 3)
	body := IssueBody(cr, strings.Repeat("a", 40), core.Major, []core.Finding{f}, nil)
	var kinds []core.FingerprintKind
	var finding string
	for _, fp := range core.ParseFingerprints([]core.ReviewNote{{Author: "x", Trusted: true, Body: body}}, "") {
		kinds = append(kinds, fp.Kind)
		if fp.Kind == core.FingerprintItem {
			finding = fp.Finding
		}
	}
	if len(kinds) != 2 || kinds[0] != core.FingerprintItem || kinds[1] != core.FingerprintIssue || finding != f.ID {
		t.Errorf("fingerprints read back = %v, finding %q, want the item of %q and the issue", kinds, finding, f.ID)
	}
}

// TestPublishedTextsMentionNoAutomation: what is posted is written as a
// reviewer would write it.
func TestPublishedTextsMentionNoAutomation(t *testing.T) {
	cr := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}
	f := finding("global-scope", core.Blocker, "app/A.kt", 12)
	f.References = []core.Reference{{Project: "github:github.com/acme/lib", Ref: "main", Path: "a.kt", Line: 1}}
	sugg := "x"
	f.Suggestion = &sugg
	f.Anchor = &core.Anchor{Kind: core.AnchorRelocated, Side: core.Right, Line: 3, RequestedLine: 12}
	texts := []string{
		InlineComment(f, "suggestion"), OKNote(strings.Repeat("a", 40)),
		IssueTitle(cr, core.Blocker), IssueBody(cr, strings.Repeat("a", 40), core.Blocker, []core.Finding{f}, []core.Finding{f}),
	}
	banned := regexp.MustCompile(`(?i)\b(ai|a\.i\.|bot|copilot|agents?|automat\w*|generated|claude|gpt|llm|plugin|assistant)\b`)
	for _, text := range texts {
		if m := banned.FindString(text); m != "" {
			t.Errorf("the text mentions %q: %q", m, text)
		}
		if strings.Contains(text, "\r") || strings.HasSuffix(text, "\n") || strings.Contains(text, " \n") {
			t.Errorf("the text has CR, a trailing newline or a trailing space: %q", text)
		}
	}
}

func planOf(actions []core.Action, skipped ...core.PlanSkip) core.Plan {
	return core.Plan{Schema: 1, Actions: actions, Skipped: skipped}
}

func TestPlanText(t *testing.T) {
	parent := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/lib", Number: 3}
	app := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}
	views := []PlanView{
		{CR: app, Title: "Greet by name", Head: strings.Repeat("a", 40), Path: "/w/app/plan.json", Plan: planOf([]core.Action{
			{ID: 1, Kind: core.ActionLabelCreate, Label: &core.Label{Name: "review"}},
			{ID: 2, Kind: core.ActionIssueCreate, Severity: core.Major, Rules: []string{"missing-tests", "missing-tests", "no-double-bang"}},
			{ID: 3, Kind: core.ActionIssueUpdate, Severity: core.Minor, Number: 12, Rules: []string{"missing-kdoc"}},
			{ID: 4, Kind: core.ActionReviewComment, Rules: []string{"global-scope"}},
			{ID: 5, Kind: core.ActionReviewComment, Rules: []string{"logic-defect"}},
			{ID: 6, Kind: core.ActionSummaryComment},
			{ID: 7, Kind: core.ActionRelatedTick, Parent: &parent},
		}, core.PlanSkip{Status: core.StatusAlreadyPosted, Kind: core.ActionReviewComment, Rule: "no-double-bang", Path: "a/B.kt", Line: 4},
			core.PlanSkip{Status: core.StatusSecretBlocked, Kind: core.ActionIssueCreate, Target: "acr:v1:issue:acme/app#7:blocker", Pattern: "github-token"},
			core.PlanSkip{Status: core.StatusNotEligible, Kind: core.ActionRelatedTick, Target: "x", Detail: "its verdict is BLOCKED"})},
		{CR: parent, Title: "Load", Head: strings.Repeat("b", 40), Path: "/w/lib/plan.json", Plan: planOf(nil), Blocked: ""},
		{CR: core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/gone", Number: 1}, Title: "Old", Head: strings.Repeat("c", 40), Path: "/w/gone/plan.json", Blocked: core.StatusNotOpen,
			Plan: planOf(nil, core.PlanSkip{Status: core.StatusNotOpen, Target: "x", Detail: "the change request is closed"})},
	}
	got := PlanText("20261120T120000Z-52fdfc07", core.Both, core.GitHub, views)
	want := `Plan for run 20261120T120000Z-52fdfc07 (both). Nothing was written to GitHub.

acme/app#7 — Greet by name (head aaaaaaaa)
  write  label review
  write  issue MAJOR (new): missing-tests ×2, no-double-bang
  write  issue MINOR (rewrite #12): missing-kdoc
  write  2 inline comments: global-scope, logic-defect
  write  the note that nothing blocks
  write  a tick in acme/lib#3
  skip   already-posted: no-double-bang a/B.kt:4
  skip   secret-blocked: issue-create (github-token) acr:v1:issue:acme/app#7:blocker
  skip   not-eligible: related-tick — its verdict is BLOCKED
  plan   /w/app/plan.json

acme/lib#3 — Load (head bbbbbbbb)
  nothing to write
  plan   /w/lib/plan.json

acme/gone#1 — Old (head cccccccc)
  skip   not-open: x — the change request is closed
  plan   /w/gone/plan.json

Next:
  agent-code-review publish --apply   write exactly these plans, only after the user said yes
  agent-code-review publish --plan <comments|issues|both>   plan again
`
	if got != want {
		t.Errorf("PlanText =\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, "github-token ") && strings.Contains(got, canaryPattern) {
		t.Error("the text that matched is printed")
	}
	if one := PlanText("r", core.Comments, core.GitLab, nil); !strings.Contains(one, "Nothing was written to GitLab.") {
		t.Errorf("GitLab: %q", one)
	}
}

const canaryPattern = "ghp_"

func TestApplyText(t *testing.T) {
	parent := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/lib", Number: 3}
	app := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}
	plan := planOf([]core.Action{
		{ID: 1, Kind: core.ActionLabelCreate, Label: &core.Label{Name: "review"}},
		{ID: 2, Kind: core.ActionIssueCreate, Severity: core.Major},
		{ID: 3, Kind: core.ActionIssueUpdate, Severity: core.Minor, Number: 12},
		{ID: 4, Kind: core.ActionReviewComment, Rules: []string{"global-scope"}, Path: "a/B.kt", Line: 4},
		{ID: 5, Kind: core.ActionSummaryComment},
		{ID: 6, Kind: core.ActionRelatedTick, Parent: &parent},
		{ID: 7, Kind: core.ActionReviewComment, Rules: []string{"logic-defect"}, Path: "a/C.kt", Line: 9},
	})
	outcomes := map[int]core.Outcome{
		1: {ID: 1, Status: core.StatusPosted},
		2: {ID: 2, Status: core.StatusPostedUnassigned, Number: 4, Detail: "the author cannot be assigned"},
		3: {ID: 3, Status: core.StatusAlreadyPosted, Number: 12},
		4: {ID: 4, Status: core.StatusFailed, Detail: "transport"},
		// 5 and 7 were not tried.
		6: {ID: 6, Status: core.StatusNotEligible, Detail: "its verdict is BLOCKED"},
	}
	views := []ApplyView{
		{CR: app, Title: "Greet by name", Head: strings.Repeat("a", 40), Plan: plan, Outcomes: outcomes},
		{CR: parent, Title: "Load", Head: strings.Repeat("b", 40), Plan: planOf(nil)},
		{CR: core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/gone", Number: 1}, Title: "Old", Head: strings.Repeat("c", 40), Blocked: core.StatusHeadChanged, Detail: "reviewed at cccccccc, now at dddddddd"},
	}
	got := ApplyText("run-1", core.GitHub, views, false)
	want := `Run run-1 is only partly published to GitHub.

acme/app#7 — Greet by name (head aaaaaaaa)
  posted  label review
  posted-unassigned  issue MAJOR (new) (#4) — the author cannot be assigned
  already-posted  issue MINOR (rewrite #12) (#12)
  not-eligible  tick in acme/lib#3 — its verdict is BLOCKED
  to do  inline comment global-scope a/B.kt:4 — failed: transport
  to do  the note that nothing blocks — not tried
  to do  inline comment logic-defect a/C.kt:9 — not tried

acme/lib#3 — Load (head bbbbbbbb)
  nothing to write

acme/gone#1 — Old (head cccccccc)
  nothing was written: head-changed — reviewed at cccccccc, now at dddddddd

Run ` + "`agent-code-review publish --apply`" + ` again to do what is left; what was done is not done twice.
`
	if got != want {
		t.Errorf("ApplyText =\n%s\nwant\n%s", got, want)
	}
	if done := ApplyText("run-1", core.GitLab, views[1:2], true); !strings.HasPrefix(done, "Published run run-1 to GitLab.\n") || strings.Contains(done, "again") {
		t.Errorf("done: %q", done)
	}
}

package render

import (
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

func mustKey(t *testing.T, s string) core.Key {
	t.Helper()
	k, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func conclusion(s string) *string { return &s }

// testDocument is a change request with a finding of every severity but
// MINOR, a pre-existing crash and every line of the footer.
func testDocument(t *testing.T) core.Document {
	t.Helper()
	suggestion := "lifecycleScope.launch { refresh() }"
	return core.Document{
		Schema: 1, CR: mustKey(t, "github:github.com/acr-plugin-sandbox/sample-app#2"), Title: "Prefetch the session",
		HeadSHA: "76ae5f13ca1575b65fe1567c471e8a7b51a00af4", BaseRef: "main", Verdict: core.Blocked,
		Files: []string{"app/A.kt", "app/B.kt", "app/C.kt", "app/D.kt", "app/E.kt"},
		Findings: []core.Finding{
			{Rule: "global-scope", Severity: core.Major, Path: "app/B.kt", Side: core.Right, Line: 20, Title: "Launches in GlobalScope",
				Why: "It leaks.\n\nIt is never cancelled.", Fix: "Use a scope.", Placement: core.Inline,
				Anchor: &core.Anchor{Kind: core.AnchorAdded, Side: core.Right, Line: 20}},
			{Rule: "style.redundant-if", Severity: core.Info, Path: "app/A.kt", Side: core.Right, Line: 3, Title: "Could be shorter",
				Why: "It reads worse.", Fix: "Return the condition.", Placement: core.Inline,
				Anchor: &core.Anchor{Kind: core.AnchorAdded, Side: core.Right, Line: 3}},
			{Rule: "run-blocking-prod", Severity: core.Blocker, Path: "app/A.kt", Side: core.Right, Line: 13, Title: "Blocks the main thread",
				Why: "The UI freezes.", Fix: "Launch it.", Suggestion: &suggestion, Placement: core.Inline,
				Anchor: &core.Anchor{Kind: core.AnchorRelocated, Side: core.Right, Line: 10, RequestedLine: 13}},
		},
		PreExisting: []core.Finding{
			{Rule: "no-double-bang", Severity: core.Blocker, Path: "app/C.kt", Side: core.Right, Line: 8, Title: "Asserts a stored name",
				Why: "It crashes.", Fix: "Return a default.", Crash: true, Placement: core.Summary,
				Anchor: &core.Anchor{Kind: core.AnchorContext, Side: core.Right, Line: 8}},
		},
		NotVerified:      []core.UnverifiedFile{{Path: "app/D.kt", Reason: "generated", Detail: "The platform omitted the diff."}},
		AgentNotVerified: []string{"How the cache is filled."},
		SignalsDismissed: []core.Dismissal{{Rule: "empty-catch", Path: "app/B.kt", Line: 5, Reason: "The catch logs."}},
		Questions:        []core.Question{{Path: "app/A.kt", Line: 4, Text: "Is the cache shared?"}, {Text: "Which consumers pick it up?"}},
		Notes:            []string{"Reviewed with care."},
		CI: core.CI{State: core.CIPassing, Checks: []core.Check{
			{Name: "lint", Status: "completed", Conclusion: conclusion("success")},
			{Name: "test", Status: "completed", Conclusion: conclusion("success")},
		}},
		Criteria: core.Criteria{Source: "overlay", Profile: "app", ProfileReason: "set in config", Packs: []string{"android"},
			Dimensions: map[string]core.DimensionChoice{"ui-toolkit": {Value: "compose", Reason: "found compose"}},
			Notes:      []string{"The overlay changed."}},
	}
}

func TestReportFullForm(t *testing.T) {
	want := `agent-code-review v1.2.3 — acr-plugin-sandbox/sample-app#2 "Prefetch the session"
Packs: android (profile app: set in config · ui-toolkit compose: found compose)
Criteria: overlay .agent-code-review/ ＠main
Scope: 5 changed files · head 76ae5f13

BLOCKER (1) — inline notes on publish
  [BLOCKER] app/A.kt:13 (relocated to 10) — Blocks the main thread  [run-blocking-prod]
            why: The UI freezes.
            fix: Launch it.   (suggestion)

MAJOR (1) — one issue on publish
  [MAJOR]   app/B.kt:20 — Launches in GlobalScope  [global-scope]
            why: It leaks. · It is never cancelled.
            fix: Use a scope.

MINOR (0) — one issue on publish

INFO (1) — report only
  [INFO]    app/A.kt:3 — Could be shorter  [style.redundant-if]
            why: It reads worse.
            fix: Return the condition.

Existing code in touched files (1) — not written in this change request
  [BLOCKER] app/C.kt:8 — Asserts a stored name  [no-double-bang] [crash]
            why: It crashes.
            fix: Return a default.

Questions for the author:
  - app/A.kt:4 — Is the cache shared?
  - Which consumers pick it up?
Signals dismissed:
  - empty-catch app/B.kt:5 — The catch logs.
Not verified:
  - app/D.kt — generated: The platform omitted the diff.
  - How the cache is filled.
no findings: app/E.kt
Pipeline: passing on head (2 checks)
Notes: The overlay changed.
       Reviewed with care.
Verdict: BLOCKED — 1 blocker in the change and 1 crashing defect in existing code to fix, then review again.
`
	if got := Report("1.2.3", testDocument(t)); got != want {
		t.Errorf("got\n%s\nwant\n%s\ndiff at line %d", got, want, firstDiff(got, want))
	}
}

func TestReportOmitsEmptyOptionalLines(t *testing.T) {
	d := testDocument(t)
	d.Findings = d.Findings[:1]
	d.PreExisting = nil
	d.NotVerified, d.AgentNotVerified, d.SignalsDismissed, d.Questions, d.Notes, d.Criteria.Notes = nil, nil, nil, nil, nil, nil
	d.Files = []string{"app/B.kt"}
	got := Report("1.2.3", d)
	for _, absent := range []string{"INFO", "Questions", "Signals dismissed", "Not verified", "no findings", "Notes"} {
		if strings.Contains(got, absent) {
			t.Errorf("%q printed:\n%s", absent, got)
		}
	}
	for _, present := range []string{"\nScope: 1 changed file · ", "\nBLOCKER (0) — ", "\nMINOR (0) — ", "\nExisting code in touched files (0) — ",
		"\nVerdict: OK — nothing blocks; non-blocking findings go to issues on publish.\n"} {
		if !strings.Contains(got, present) {
			t.Errorf("%q missing:\n%s", present, got)
		}
	}
}

func TestReportVerdictCounts(t *testing.T) {
	tests := []struct {
		blockers, crashes int
		want              string
	}{
		{2, 0, "BLOCKED — 2 blockers in the change to fix, then review again."},
		{0, 3, "BLOCKED — 3 crashing defects in existing code to fix, then review again."},
		{2, 3, "BLOCKED — 2 blockers in the change and 3 crashing defects in existing code to fix, then review again."},
	}
	for _, tt := range tests {
		d := testDocument(t)
		d.Findings, d.PreExisting = nil, nil
		for i := 0; i < tt.blockers; i++ {
			f := testDocument(t).Findings[2]
			f.Line = 13 + i
			d.Findings = append(d.Findings, f)
		}
		for i := 0; i < tt.crashes; i++ {
			f := testDocument(t).PreExisting[0]
			f.Line = 8 + i
			d.PreExisting = append(d.PreExisting, f)
		}
		got := Report("1.2.3", d)
		if !strings.Contains(got, "\nVerdict: "+tt.want+"\n") {
			t.Errorf("want verdict %q in\n%s", tt.want, got)
		}
	}
}

func TestCriteriaSources(t *testing.T) {
	for source, want := range map[string]string{"overlay": "overlay .agent-code-review/", "legacy": "legacy .github/skills", "default": "defaults"} {
		d := testDocument(t)
		d.Criteria.Source = source
		if got := Report("1.2.3", d); !strings.Contains(got, "\nCriteria: "+want+" ＠main\n") {
			t.Errorf("%s: no criteria line %q in\n%s", source, want, got)
		}
	}
}

// firstDiff is the 1-based number of the first line where a and b differ.
func firstDiff(a, b string) int {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range la {
		if i >= len(lb) || la[i] != lb[i] {
			return i + 1
		}
	}
	return len(la) + 1
}

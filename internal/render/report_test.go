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
Criteria: overlay .agent-code-review/ @main
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
		if got := Report("1.2.3", d); !strings.Contains(got, "\nCriteria: "+want+" @main\n") {
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

func existing(rule string, s core.Severity, path string, line int, why, fix string, crash bool) core.Finding {
	return core.Finding{Rule: rule, Severity: s, Path: path, Side: core.Right, Line: line, Title: "Asserts a value",
		Why: why, Fix: fix, Crash: crash, Placement: core.Summary}
}

func TestReportGroupsExistingCode(t *testing.T) {
	d := testDocument(t)
	d.PreExisting = []core.Finding{
		existing("no-double-bang", core.Major, "lib/R.kt", 73, "It throws.", "Handle null.", false),
		existing("no-double-bang", core.Major, "lib/R.kt", 69, "It throws.", "Handle null.", false),
		existing("no-double-bang", core.Major, "lib/R.kt", 81, "It throws.", "Return early.", false),
		existing("no-double-bang", core.Blocker, "lib/R.kt", 90, "It crashes.", "Handle null.", true),
		existing("no-double-bang", core.Blocker, "lib/R.kt", 95, "It crashes.", "Handle null.", true),
		existing("no-double-bang", core.Major, "lib/S.kt", 5, "It throws.", "Handle null.", false),
		existing("no-double-bang", core.Major, "lib/R.kt", 101, "It throws.", "Handle null.", false),
	}
	got := Report("1.2.3", d)
	want := `Existing code in touched files (7) — not written in this change request
  [BLOCKER] lib/R.kt:90 — Asserts a value  [no-double-bang] [crash]
            why: It crashes.
            fix: Handle null.
  [BLOCKER] lib/R.kt:95 — Asserts a value  [no-double-bang] [crash]
            why: It crashes.
            fix: Handle null.
  [MAJOR]   lib/R.kt — Asserts a value  [no-double-bang] (3)
            lines: 69, 73, 101
            why: It throws.
            fix: Handle null.
  [MAJOR]   lib/R.kt:81 — Asserts a value  [no-double-bang]
            why: It throws.
            fix: Return early.
  [MAJOR]   lib/S.kt:5 — Asserts a value  [no-double-bang]
            why: It throws.
            fix: Handle null.

`
	if !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant the section\n%s", got, want)
	}
	if !strings.Contains(got, "\nVerdict: BLOCKED — 1 blocker in the change and 2 crashing defects in existing code to fix") {
		t.Errorf("verdict:\n%s", got)
	}
}

func check(name, status, concl string) core.Check {
	c := core.Check{Name: name, Status: status}
	if concl != "" {
		c.Conclusion = conclusion(concl)
	}
	return c
}

func TestPipeline(t *testing.T) {
	tests := []struct {
		ci   core.CI
		want string
	}{
		{core.CI{State: core.CIPassing, Checks: []core.Check{check("lint", "completed", "success")}}, "passing on head (1 check)"},
		{core.CI{State: core.CIFailing, Checks: []core.Check{
			check("lint", "completed", "failure"), check("test", "completed", "success"), check("instrumented", "in_progress", ""),
			check("lint", "completed", "failure"), check("e2e", "completed", "timed_out"),
		}}, "failing on head: lint, e2e"},
		{core.CI{State: core.CIPending, Checks: []core.Check{
			check("lint", "completed", "success"), check("instrumented", "in_progress", ""), check("deploy", "queued", ""),
		}}, "pending on head: instrumented, deploy"},
		{core.CI{State: core.CINone, Checks: []core.Check{}}, "none on head"},
		{core.CI{State: core.CIUnknown, Reason: "forbidden", Detail: "Resource not accessible by integration", Checks: []core.Check{}},
			"not verified (forbidden: Resource not accessible by integration)"},
		{core.CI{State: core.CIUnknown, Reason: "error", Checks: []core.Check{}}, "not verified (error)"},
	}
	for _, tt := range tests {
		if got := pipeline(tt.ci); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.ci.State, got, tt.want)
		}
	}
}

func TestReportShortForm(t *testing.T) {
	d := testDocument(t)
	d.Verdict = core.OK
	d.Findings, d.PreExisting, d.SignalsDismissed, d.Questions = nil, nil, nil, nil
	d.NotVerified, d.AgentNotVerified, d.Notes, d.Criteria.Notes = nil, nil, nil, nil
	want := `agent-code-review v1.2.3 — acr-plugin-sandbox/sample-app#2 "Prefetch the session"
Packs: android (profile app: set in config · ui-toolkit compose: found compose)
No findings on 5 changed files. Pipeline: passing on head (2 checks). Verdict: OK.
`
	if got := Report("1.2.3", d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	// What could not be checked and the notes are never left out.
	d.NotVerified = []core.UnverifiedFile{{Path: "app/D.kt", Reason: "generated"}}
	d.AgentNotVerified = []string{"How the cache is filled."}
	d.Criteria.Notes = []string{"The overlay changed."}
	want += `Not verified:
  - app/D.kt — generated
  - How the cache is filled.
Notes: The overlay changed.
`
	if got := Report("1.2.3", d); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	// Anything else to show gives the full form.
	for name, change := range map[string]func(d *core.Document){
		"finding":      func(d *core.Document) { d.Findings = testDocument(t).Findings[:1] },
		"pre-existing": func(d *core.Document) { d.PreExisting = testDocument(t).PreExisting },
		"dismissed":    func(d *core.Document) { d.SignalsDismissed = testDocument(t).SignalsDismissed },
		"question":     func(d *core.Document) { d.Questions = testDocument(t).Questions },
	} {
		full := d
		change(&full)
		if got := Report("1.2.3", full); !strings.Contains(got, "\nScope: ") {
			t.Errorf("%s: want the full form, got\n%s", name, got)
		}
	}
}

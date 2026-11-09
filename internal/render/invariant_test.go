package render

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
)

// randomDocument is a findings document with few paths, rules and texts,
// so that grouping, sorting and every optional line are exercised.
func randomDocument(t *testing.T, rng *rand.Rand, cr string) core.Document {
	t.Helper()
	pick := func(list ...string) string { return list[rng.IntN(len(list))] }
	paths := []string{"app/A.kt", "app/B.kt", "lib/C.kt"}
	finding := func(pre bool) core.Finding {
		f := core.Finding{
			Rule: pick("no-double-bang", "global-scope", "style.redundant-if"), Severity: severities[rng.IntN(len(severities))],
			Path: pick(paths...), Side: core.Right, Line: 1 + rng.IntN(40), Title: pick("Asserts a value", "Launches in GlobalScope"),
			Why: pick("It throws.", "It leaks.\nAnd stays.", "  It hides the intent.  "), Fix: pick("Handle null.", "Use a scope.\n\n```\nx\n```"),
			Placement: core.Inline, Anchor: &core.Anchor{Kind: core.AnchorAdded, Side: core.Right},
		}
		f.Anchor.Line = f.Line
		if rng.IntN(4) == 0 {
			f.Anchor = &core.Anchor{Kind: core.AnchorRelocated, Side: core.Right, Line: 1 + rng.IntN(40), RequestedLine: f.Line}
		}
		if pre {
			f.Placement = core.Summary
			f.Crash = f.Severity == core.Blocker && rng.IntN(2) == 0
		} else if rng.IntN(3) == 0 {
			s := "x := 1"
			f.Suggestion = &s
		}
		return f
	}
	d := core.Document{
		Schema: 1, CR: mustKey(t, cr), Title: "A change", HeadSHA: strings.Repeat("ab", 20), BaseRef: "main",
		Files:    append([]string{"app/D.kt"}, paths...),
		CI:       core.CI{State: core.CIPassing, Checks: []core.Check{check("lint", "completed", "success")}},
		Criteria: core.Criteria{Source: "default", Profile: "app", ProfileReason: "default profile of android", Packs: []string{"android"}},
	}
	for range rng.IntN(8) {
		d.Findings = append(d.Findings, finding(false))
	}
	for range rng.IntN(12) {
		d.PreExisting = append(d.PreExisting, finding(true))
	}
	if rng.IntN(2) == 0 {
		d.Questions = []core.Question{{Path: "app/A.kt", Line: 3, Text: "Why?\nReally?"}}
	}
	if rng.IntN(2) == 0 {
		d.SignalsDismissed = []core.Dismissal{{Rule: "empty-catch", Path: "app/B.kt", Line: 2, Reason: "Logged."}}
	}
	if rng.IntN(2) == 0 {
		d.NotVerified = []core.UnverifiedFile{{Path: "app/D.kt", Reason: "generated"}}
		d.Notes = []string{"A note.", "Another note."}
	}
	d.Verdict = core.ChangeRequestVerdict(d.Findings, d.PreExisting)
	return d
}

var (
	sectionHeader  = regexp.MustCompile(`^(BLOCKER|MAJOR|MINOR|INFO) \((\d+)\) — `)
	existingHeader = regexp.MustCompile(`^Existing code in touched files \((\d+)\) — `)
	entryLine      = regexp.MustCompile(`^  \[(BLOCKER|MAJOR|MINOR|INFO)\] *\S`)
	groupLine      = regexp.MustCompile(` \((\d+)\)$`)
	verdictLine    = regexp.MustCompile(`^Verdict: (BLOCKED|OK)(?: — (?:(\d+) blockers? in the change)?(?: and )?(?:(\d+) crashing defects? in existing code)?)?`)
)

// reportCounts reads a full report back: the count of each section header,
// the entries and occurrences listed under it, and the verdict.
type reportCounts struct {
	headers, listed map[string]int
	verdict         string
	blockers        int
	crashes         int
}

func readReport(t *testing.T, report string) reportCounts {
	t.Helper()
	c := reportCounts{headers: map[string]int{}, listed: map[string]int{}}
	section := ""
	lines := strings.Split(report, "\n")
	for i, l := range lines {
		if m := sectionHeader.FindStringSubmatch(l); m != nil {
			section = m[1]
			c.headers[section], _ = strconv.Atoi(m[2])
			continue
		}
		if m := existingHeader.FindStringSubmatch(l); m != nil {
			section = "existing"
			c.headers[section], _ = strconv.Atoi(m[1])
			continue
		}
		if l == "" {
			section = ""
			continue
		}
		if section != "" && entryLine.MatchString(l) {
			n := 1
			if m := groupLine.FindStringSubmatch(l); m != nil && section == "existing" {
				n, _ = strconv.Atoi(m[1])
				numbers := strings.Split(strings.TrimPrefix(strings.TrimSpace(lines[i+1]), "lines: "), ", ")
				if !strings.HasPrefix(strings.TrimSpace(lines[i+1]), "lines: ") || len(numbers) != n || n < 2 {
					t.Errorf("group of %d with %q", n, lines[i+1])
				}
				if strings.Contains(l, "[crash]") {
					t.Errorf("grouped crash: %q", l)
				}
			}
			c.listed[section] += n
			if strings.Contains(l, "[crash]") {
				c.crashes++
			}
		}
		if m := verdictLine.FindStringSubmatch(l); m != nil {
			c.verdict = m[1]
			c.blockers, _ = strconv.Atoi(m[2])
		}
	}
	return c
}

func checkText(t *testing.T, name, s string) {
	t.Helper()
	if strings.Contains(s, "\r") || !strings.HasSuffix(s, "\n") {
		t.Errorf("%s: not LF-terminated lines", name)
	}
	for i, l := range strings.Split(s, "\n") {
		if strings.TrimRight(l, " \t") != l {
			t.Errorf("%s: trailing space on line %d: %q", name, i+1, l)
		}
	}
}

func TestReportInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 500 {
		d := randomDocument(t, rng, "github:github.com/acr-plugin-sandbox/sample-app#2")
		got := Report("1.2.3", d)
		name := fmt.Sprintf("document %d", i)
		checkText(t, name, got)
		if again := Report("1.2.3", d); again != got {
			t.Fatalf("%s: two renderings differ", name)
		}
		if short(d) {
			continue
		}
		c := readReport(t, got)
		bySeverity := map[string]int{}
		for _, f := range d.Findings {
			bySeverity[string(f.Severity)]++
		}
		for _, s := range severities {
			if c.headers[string(s)] != c.listed[string(s)] || c.headers[string(s)] != bySeverity[string(s)] {
				t.Errorf("%s: %s header %d, %d listed, %d findings\n%s", name, s, c.headers[string(s)], c.listed[string(s)], bySeverity[string(s)], got)
			}
		}
		if c.headers["existing"] != c.listed["existing"] || c.headers["existing"] != len(d.PreExisting) {
			t.Errorf("%s: existing header %d, %d listed, %d findings\n%s", name, c.headers["existing"], c.listed["existing"], len(d.PreExisting), got)
		}
		if c.verdict != string(d.Verdict) || c.blockers != c.headers["BLOCKER"] {
			t.Errorf("%s: verdict %s with %d blockers, want %s with %d\n%s", name, c.verdict, c.blockers, d.Verdict, c.headers["BLOCKER"], got)
		}
	}
}

var summaryRow = regexp.MustCompile(`^ +\d+\. (\S+) +(OK|BLOCKED) +(\d+) blocker · (\d+) major · (\d+) minor \(\+(\d+) existing(?:, (\d+) crash)?\)$`)

func TestCascadeInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range 200 {
		root := mustKey(t, sandboxKey+"sample-app#1")
		res := cascade.Result{Entries: []core.Entry{{CR: root, Status: core.Finalized}}}
		var reviewed []Reviewed
		n := 1 + rng.IntN(6)
		for j := range n {
			k := root
			if j > 0 {
				k = mustKey(t, fmt.Sprintf("%score-lib#%d", sandboxKey, j))
				res.Entries = append(res.Entries, core.Entry{CR: k, Depth: 1, Parent: &root, Status: core.Finalized})
				res.Order = append(res.Order, k)
			}
			reviewed = append(reviewed, Reviewed{Doc: randomDocument(t, rng, k.String()), Report: k.Project + ".txt"})
		}
		res.Order = append(res.Order, root)
		summary, err := Cascade(res, reviewed)
		if err != nil {
			t.Fatal(err)
		}
		checkText(t, "summary", summary)
		final, err := Final(res, reviewed)
		if err != nil {
			t.Fatal(err)
		}
		checkText(t, "final", final)
		if again, _ := Cascade(res, reviewed); again != summary {
			t.Fatal("two renderings of a summary differ")
		}
		var rows [][]string
		for _, l := range strings.Split(summary, "\n") {
			if m := summaryRow.FindStringSubmatch(l); m != nil {
				rows = append(rows, m)
			}
		}
		if len(rows) != len(res.Order) {
			t.Fatalf("cascade %d: %d rows for %d change requests\n%s", i, len(rows), len(res.Order), summary)
		}
		for j, k := range res.Order {
			var doc core.Document
			for _, r := range reviewed {
				if r.Doc.CR == k {
					doc = r.Doc
				}
			}
			report := Report("1.2.3", doc)
			c := readReport(t, report)
			if short(doc) {
				c = reportCounts{headers: map[string]int{}, verdict: "OK"}
			}
			row := rows[j]
			want := []string{ref(k), c.verdict, fmt.Sprint(c.headers["BLOCKER"]), fmt.Sprint(c.headers["MAJOR"]), fmt.Sprint(c.headers["MINOR"]), fmt.Sprint(c.headers["existing"])}
			crash := ""
			if c.crashes > 0 {
				crash = fmt.Sprint(c.crashes)
			}
			if got := append(row[1:7:7], row[7]); fmt.Sprint(got) != fmt.Sprint(append(want, crash)) {
				t.Errorf("cascade %d row %d: %q, the report says %q\n%s", i, j+1, got, append(want, crash), report)
			}
		}
	}
}

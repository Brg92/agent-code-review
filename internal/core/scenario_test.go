package core_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
)

func finalize(t *testing.T, scen, repo string, number int, change func(j *core.Judgement)) core.Result {
	t.Helper()
	j := scenario.Judgement(t, scen, repo, number)
	if change != nil {
		change(&j)
	}
	res, err := core.Finalize(scenario.Review(t, repo, number, scen), j)
	if err != nil {
		t.Fatalf("%s %s#%d: %v", scen, repo, number, err)
	}
	checkContract(t, res)
	return res
}

// checkContract checks that the findings of a result match the findings
// contract, inside an otherwise minimal findings document.
func checkContract(t *testing.T, res core.Result) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "findings.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := json.Marshal(map[string]any{
		"schema": 1, "cr": res.CR, "headSha": res.HeadSHA, "verdict": res.Verdict,
		"findings": res.Findings, "preExisting": res.PreExisting, "notVerified": []any{},
		"agentNotVerified": []any{}, "signalsDismissed": []any{}, "questions": []any{}, "notes": []any{},
		"ci":       map[string]any{"state": "none", "checks": []any{}},
		"criteria": map[string]any{"source": "default", "profile": "app", "packs": []any{}, "notes": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issues, err := schema.Validate(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Errorf("%s: findings contract: %s", res.CR, i)
	}
}

func find(t *testing.T, fs []core.Finding, rule string, line int) core.Finding {
	t.Helper()
	for _, f := range fs {
		if f.Rule == rule && f.Line == line {
			return f
		}
	}
	t.Fatalf("no %s finding on line %d", rule, line)
	return core.Finding{}
}

func anchor(f core.Finding) string {
	if f.Anchor == nil {
		return string(f.Placement)
	}
	s := fmt.Sprintf("%s %s %s %d", f.Placement, f.Anchor.Kind, f.Anchor.Side, f.Anchor.Line)
	if f.Anchor.RequestedLine != 0 {
		s += fmt.Sprintf(" for %d", f.Anchor.RequestedLine)
	}
	return s
}

const s1Dir = "app/src/main/java/com/example/sample/"

func TestScenarioS1(t *testing.T) {
	res := finalize(t, "S1", "sample-app", 2, nil)
	if res.Verdict != core.Blocked {
		t.Errorf("verdict %s, want BLOCKED", res.Verdict)
	}
	tests := []struct {
		list       []core.Finding
		rule, want string
		line       int
	}{
		{res.Findings, "run-blocking-prod", "inline added RIGHT 13", 13},
		{res.Findings, "global-scope", "inline added RIGHT 19", 19},
		{res.Findings, "no-double-bang", "inline added RIGHT 20", 20},
		{res.Findings, "logic.always-throws", "inline relocated RIGHT 16 for 27", 27},
		{res.PreExisting, "no-double-bang", "summary context RIGHT 8", 8},
		{res.PreExisting, "global-scope", "summary relocated RIGHT 15 for 13", 13},
	}
	for _, tt := range tests {
		if got := anchor(find(t, tt.list, tt.rule, tt.line)); got != tt.want {
			t.Errorf("%s:%d is %s, want %s", tt.rule, tt.line, got, tt.want)
		}
	}
	throws := find(t, res.Findings, "logic.always-throws", 27)
	if got, want := throws.Anchor.Lead(throws.Path), "Refers to line 27 of `"+s1Dir+"ui/HomeViewModel.kt`, outside the changed lines."; got != want {
		t.Errorf("lead %q, want %q", got, want)
	}

	// The fingerprints are those of the findings contract example of S1;
	// the two GlobalScope launches share their text and differ by
	// occurrence index.
	var example struct {
		Findings, PreExisting []core.Finding
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "findings", "valid", "s1-blocked.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &example); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, f := range append(example.Findings, example.PreExisting...) {
		want[fmt.Sprintf("%s %s %d", f.Rule, f.Path, f.Line)] = f.ID
	}
	for _, f := range append(res.Findings, res.PreExisting...) {
		if k := fmt.Sprintf("%s %s %d", f.Rule, f.Path, f.Line); f.ID != want[k] {
			t.Errorf("%s: id %s, the contract example says %s", k, f.ID, want[k])
		}
	}
	if a, b := find(t, res.PreExisting, "global-scope", 13).ID, find(t, res.Findings, "global-scope", 19).ID; a == b {
		t.Errorf("both GlobalScope launches are %s", a)
	}
}

func TestScenarioS1BlockedByThePreExistingCrashAlone(t *testing.T) {
	withoutBlockers := func(j *core.Judgement) {
		var kept []core.JudgedFinding
		for _, f := range j.Findings {
			if f.Severity != core.Blocker {
				kept = append(kept, f)
			}
		}
		j.Findings = kept
	}
	res := finalize(t, "S1", "sample-app", 2, withoutBlockers)
	if len(res.Findings) != 2 || res.Verdict != core.Blocked {
		t.Errorf("%d findings, verdict %s: want BLOCKED by the crash in ProfileStore.kt", len(res.Findings), res.Verdict)
	}
	res = finalize(t, "S1", "sample-app", 2, func(j *core.Judgement) {
		withoutBlockers(j)
		j.PreExisting[0].Crash = false
	})
	if res.Verdict != core.OK {
		t.Errorf("verdict %s without BLOCKERs and crashes, want OK", res.Verdict)
	}
}

func TestScenarioS3(t *testing.T) {
	res := finalize(t, "S3", "core-lib", 1, nil)
	if res.Verdict != core.OK {
		t.Errorf("verdict %s, want OK: a MAJOR does not block", res.Verdict)
	}
	validation := find(t, res.Findings, "logic.removed-validation", 7)
	if validation.Severity != core.Major || anchor(validation) != "inline removed LEFT 7" {
		t.Errorf("removed validation: %s %s, want MAJOR on the removed line", validation.Severity, anchor(validation))
	}
	redundant := find(t, res.Findings, "style.redundant-if", 9)
	if anchor(redundant) != "inline added RIGHT 9" || redundant.StartLine != 5 {
		t.Errorf("redundant if: %s from %d", anchor(redundant), redundant.StartLine)
	}
	if redundant.Suggestion == nil || *redundant.Suggestion != "        return session.expiresAtMillis <= nowMillis" || redundant.Fix != "Return the comparison directly." {
		t.Errorf("the five-line suggestion is not kept: %v, fix %q", redundant.Suggestion, redundant.Fix)
	}
}

func TestScenarioS4(t *testing.T) {
	res := finalize(t, "S4", "feature-lib", 1, nil)
	if res.Verdict != core.OK || len(res.Findings)+len(res.PreExisting) != 0 {
		t.Errorf("verdict %s with %d findings, want OK with none", res.Verdict, len(res.Findings))
	}
	// A finding on the generated file, whose patch the platform omitted,
	// can only go to the summary.
	res = finalize(t, "S4", "feature-lib", 1, func(j *core.Judgement) {
		j.Findings = []core.JudgedFinding{{
			Rule: "global-scope", Severity: core.Minor, Path: "src/main/java/com/example/feature/catalog/IconCatalog.kt", Side: core.Right, Line: 3,
			Title: "t", Why: "w", Fix: "f", References: []core.Reference{},
		}}
	})
	if got := anchor(res.Findings[0]); got != "summary" {
		t.Errorf("finding on IconCatalog.kt is %s, want summary", got)
	}
}

func TestScenariosWithoutBlockers(t *testing.T) {
	tests := []struct {
		scen, repo string
		number     int
		findings   int
	}{
		{"S5", "sample-app", 3, 0},
		{"S5", "sample-app", 4, 0},
		{"S6", "sample-app", 5, 1},
		{"S6", "core-lib", 2, 0},
		{"S6", "kmp-shared", 1, 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %s#%d", tt.scen, tt.repo, tt.number), func(t *testing.T) {
			res := finalize(t, tt.scen, tt.repo, tt.number, nil)
			if res.Verdict != core.OK || len(res.Findings) != tt.findings || len(res.PreExisting) != 0 {
				t.Errorf("verdict %s, %d findings, %d pre-existing: want OK with %d findings", res.Verdict, len(res.Findings), len(res.PreExisting), tt.findings)
			}
		})
	}
	initials := find(t, finalize(t, "S6", "sample-app", 5, nil).Findings, "no-double-bang", 10)
	if initials.Severity != core.Major || anchor(initials) != "inline added RIGHT 10" {
		t.Errorf("S6 A: %s %s, want a MAJOR on the added line", initials.Severity, anchor(initials))
	}
}

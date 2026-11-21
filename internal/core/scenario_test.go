package core_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
	"github.com/Brg92/agent-code-review/internal/testutil/productflow"
)

// finalize finalizes a change request as the product does: the review is
// the one built from the work package that review wrote on the recordings,
// and the judgement is the scenario's, after change has edited it.
func finalize(t *testing.T, scen, repo string, number int, change func(j *core.Judgement)) core.Result {
	t.Helper()
	res := productflow.Collect(t, scen, repo, number).Of(t, repo, number).Finalize(t, change)
	checkContract(t, res)
	return res
}

// checkContract checks that the findings document of a result, with a
// minimal header, matches the findings contract.
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
	doc, err := json.Marshal(res.Document(core.Header{
		Title: "t", BaseRef: "main", CI: core.CI{State: core.CINone},
		Criteria: core.Criteria{Source: "default", Profile: "app", ProfileReason: "r"},
	}))
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
		{res.Findings, "logic-defect", "inline relocated RIGHT 16 for 27", 27},
		{res.PreExisting, "no-double-bang", "summary context RIGHT 8", 8},
		{res.PreExisting, "global-scope", "summary relocated RIGHT 15 for 13", 13},
	}
	for _, tt := range tests {
		if got := anchor(find(t, tt.list, tt.rule, tt.line)); got != tt.want {
			t.Errorf("%s:%d is %s, want %s", tt.rule, tt.line, got, tt.want)
		}
	}
	throws := find(t, res.Findings, "logic-defect", 27)
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
	// The example is the findings the scenario describes; the two automatic
	// missing-tests findings that the product adds are not in it.
	seen := map[string]bool{}
	for _, f := range append(res.Findings, res.PreExisting...) {
		k := fmt.Sprintf("%s %s %d", f.Rule, f.Path, f.Line)
		if id, ok := want[k]; ok {
			seen[k] = true
			if f.ID != id {
				t.Errorf("%s: id %s, the contract example says %s", k, f.ID, id)
			}
		} else if f.Rule != "missing-tests" {
			t.Errorf("%s is not in the contract example", k)
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("%s is in the contract example and not in the result", k)
		}
	}
	if a, b := find(t, res.PreExisting, "global-scope", 13).ID, find(t, res.Findings, "global-scope", 19).ID; a == b {
		t.Errorf("both GlobalScope launches are %s", a)
	}
}

func TestScenarioS1BlockedByThePreExistingCrashAlone(t *testing.T) {
	// No BLOCKER of the change request is left: the hand-written one is
	// dropped and the signals the product raises as BLOCKERs, run-blocking-prod
	// (s1) and global-scope (s7), are dismissed.
	withoutBlockers := func(j *core.Judgement) {
		var kept []core.JudgedFinding
		for _, f := range j.Findings {
			if f.Severity != core.Blocker {
				kept = append(kept, f)
			}
		}
		j.Findings = kept
		for i, d := range j.Signals {
			if d.ID == "s1" || d.ID == "s7" {
				j.Signals[i] = core.SignalDecision{ID: d.ID, Decision: core.Dismiss, Reason: "not part of this test"}
			}
		}
	}
	res := finalize(t, "S1", "sample-app", 2, withoutBlockers)
	for _, f := range res.Findings {
		if f.Severity == core.Blocker {
			t.Errorf("%s on %s:%d is a BLOCKER", f.Rule, f.Path, f.Line)
		}
	}
	if len(res.Findings) == 0 || res.Verdict != core.Blocked {
		t.Errorf("%d findings, verdict %s: want BLOCKED by the crash in ProfileStore.kt", len(res.Findings), res.Verdict)
	}
	res = finalize(t, "S1", "sample-app", 2, func(j *core.Judgement) {
		withoutBlockers(j)
		for i, d := range j.Signals {
			if d.ID == "s2" {
				j.Signals[i] = core.SignalDecision{ID: d.ID, Decision: core.Confirm, Severity: core.Major}
			}
		}
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
	validation := find(t, res.Findings, "logic-defect", 7)
	if validation.Severity != core.Major || anchor(validation) != "inline removed LEFT 7" {
		t.Errorf("removed validation: %s %s, want MAJOR on the removed line", validation.Severity, anchor(validation))
	}
	redundant := find(t, res.Findings, "expression-body", 9)
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
	// The agent was shown no line of the generated file, whose patch the
	// platform omitted: a finding on it is refused, with its pointer.
	r := productflow.Collect(t, "S4", "feature-lib", 1).Of(t, "feature-lib", 1)
	j := r.Judgement
	const path = "src/main/java/com/example/feature/catalog/IconCatalog.kt"
	j.Findings = []core.JudgedFinding{{
		Rule: "global-scope", Severity: core.Blocker, Path: path, Side: core.Right, Line: 3,
		Title: "t", Why: "w", Fix: "f", References: []core.Reference{},
	}}
	var verr core.ValidationError
	if err := core.ValidateJudgement(r.Review, j); !errors.As(err, &verr) || len(verr) != 1 || verr[0].Pointer != "/findings/0/line" ||
		!strings.Contains(verr[0].Message, "line 3 of "+path+" was not shown to the agent") {
		t.Errorf("errors: %v", err)
	}
}

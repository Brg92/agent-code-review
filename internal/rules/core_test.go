package rules

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCore(t *testing.T) {
	c := Core()
	var got []string
	for _, r := range c.Rules {
		if r.Matched() && !r.OnChangeRequest() {
			t.Errorf("%s has a match on one file; core checks are not file-level", r.ID)
		}
		got = append(got, r.ID+" "+r.Severity)
	}
	want := "logic-defect BLOCKER, variant-asymmetry MAJOR, dependency-mr-undeclared MAJOR, uses-removed-api BLOCKER, missing-required-argument BLOCKER, contract-misuse MAJOR"
	if c.Pack != CorePack || strings.Join(got, ", ") != want {
		t.Errorf("core %q: %s", c.Pack, strings.Join(got, ", "))
	}
}

func TestMerge(t *testing.T) {
	legacy, err := LoadCatalog(readFile(t, filepath.Join(contractDir, "valid", "legacy-signals.json")))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := Merge(Core(), legacy)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 6+18 || rules[0].ID != "logic-defect" || rules[6].ID != "strict-version-pin" {
		t.Errorf("merged %d rules, first %s, seventh %s", len(rules), rules[0].ID, rules[6].ID)
	}
	example, err := LoadCatalog(readFile(t, filepath.Join(contractDir, "valid", "android.json")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Merge(Core(), example)
	if err == nil || !strings.Contains(err.Error(), `"logic-defect"`) || !strings.Contains(err.Error(), "core") || !strings.Contains(err.Error(), "android") {
		t.Errorf("Merge with a duplicate id: error %v, want one naming the rule and both packs", err)
	}
}

// TestCoreLogicDefect checks the rule a judgement applies to a defect in
// the logic of the change that no more specific rule names: it is only
// judged, starts as a BLOCKER and tells the agent what to write.
func TestCoreLogicDefect(t *testing.T) {
	var rule *Rule
	for _, r := range Core().Rules {
		if r.ID == "logic-defect" {
			rule = r
		}
	}
	if rule == nil {
		t.Fatal("no logic-defect rule in the core catalog")
	}
	if rule.Matched() || !rule.NeedsJudgement || rule.Severity != "BLOCKER" || rule.Group != "Logic" {
		t.Errorf("logic-defect = %+v, want a judged BLOCKER in group Logic without a match", rule)
	}
	if rule.Why == "" || rule.Fix == "" {
		t.Errorf("logic-defect has why %q and fix %q, want both", rule.Why, rule.Fix)
	}
	for _, example := range []string{"wrong result", "always throws", "validation", "unreachable"} {
		if !strings.Contains(rule.Description, example) {
			t.Errorf("logic-defect description does not mention %q: %s", example, rule.Description)
		}
	}
}

func TestCoreVariantAsymmetry(t *testing.T) {
	var rule *Rule
	for _, r := range Core().Rules {
		if r.ID == "variant-asymmetry" {
			rule = r
		}
	}
	if rule == nil || !rule.OnChangeRequest() || !rule.NeedsJudgement || rule.Why == "" || rule.Fix == "" {
		t.Fatalf("variant-asymmetry = %+v, want a change-request rule that needs judgement, with why and fix", rule)
	}
	free := ChangedFile{File: File{Path: "app/src/free/res/values/strings.xml", Source: "<a/>\n", Added: map[int]bool{1: true}}}
	tree := fakeTree{"app/src/free/res/values/strings.xml", "app/src/paid/res/values/strings.xml"}
	tests := []struct {
		name     string
		variants []string
		want     string
	}{
		{"no variants", nil, ""},
		{"one variant", []string{"free"}, ""},
		{"two variants", []string{"free", "paid"}, "app/src/free/res/values/strings.xml:1:1"},
	}
	for _, tt := range tests {
		sigs := RunChangeRequest([]*Rule{rule}, ChangeRequest{Files: []ChangedFile{free}, Tree: tree, Variants: tt.variants}).Signals
		if got := signalsAt(sigs); got != tt.want {
			t.Errorf("%s: signals %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCoreSymbolRules(t *testing.T) {
	byID := map[string]*Rule{}
	for _, r := range Core().Rules {
		byID[r.ID] = r
	}
	parent := Ancestor{Project: sampleApp, Head: "e9009e", Target: "main"}
	tests := []struct {
		id, head string
		hits     map[string][]Match
		want     []string
	}{
		{"uses-removed-api", managerHead, map[string][]Match{sampleApp + " main legacyToken": {{Path: repoPath, Line: 5}}},
			[]string{"LEFT uses-removed-api " + managerPath + ":7:9 -> sample-app@main:" + repoPath + ":5"}},
		{"missing-required-argument", strings.Replace(managerBase, "(force: Boolean)", "(force: Boolean, reason: String)", 1),
			map[string][]Match{sampleApp + " main refresh": {{Path: "app/Sync.kt", Line: 4}}},
			[]string{"RIGHT missing-required-argument " + managerPath + ":11:9 -> sample-app@main:app/Sync.kt:4"}},
	}
	for _, tt := range tests {
		r := byID[tt.id]
		if r == nil || !r.OnChangeRequest() || !r.NeedsJudgement || r.Severity != "BLOCKER" || r.Why == "" || r.Fix == "" {
			t.Fatalf("%s = %+v, want a BLOCKER change-request rule that needs judgement, with why and fix", tt.id, r)
		}
		cr := ChangeRequest{
			Files:   []ChangedFile{symbolFile(t, managerPath, managerBase, tt.head, nil, nil)},
			Project: coreLib, Target: "main", Search: &fakeSearch{hits: tt.hits}, Ancestors: []Ancestor{parent},
		}
		if got := signalsWithRefs(RunChangeRequest([]*Rule{r}, cr).Signals); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: signals %q, want %q", tt.id, got, tt.want)
		}
	}
}

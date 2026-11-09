package rules

import (
	"path/filepath"
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
	want := "variant-asymmetry MAJOR, dependency-mr-undeclared MAJOR, uses-removed-api BLOCKER, missing-required-argument BLOCKER, contract-misuse MAJOR"
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
	if len(rules) != 5+18 || rules[0].ID != "variant-asymmetry" || rules[5].ID != "strict-version-pin" {
		t.Errorf("merged %d rules, first %s, sixth %s", len(rules), rules[0].ID, rules[5].ID)
	}
	example, err := LoadCatalog(readFile(t, filepath.Join(contractDir, "valid", "android.json")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Merge(Core(), example)
	if err == nil || !strings.Contains(err.Error(), `"uses-removed-api"`) || !strings.Contains(err.Error(), "core") || !strings.Contains(err.Error(), "android") {
		t.Errorf("Merge with a duplicate id: error %v, want one naming the rule and both packs", err)
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

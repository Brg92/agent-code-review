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
		if r.Matched() {
			t.Errorf("%s has a match; its checks are not file-level", r.ID)
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

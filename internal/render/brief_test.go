package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// briefCatalog is a synthetic pack catalog for the briefs.
const briefCatalog = `{"schema":1,"pack":"mobile","rules":[
 {"id":"mobile-blocking","title":"Blocking call on the main thread","severity":"BLOCKER","description":"D","why":"It freezes the screen.","fix":"Move it off the main thread.","languages":[],"tags":[],"group":"Threads",
  "match":{"patterns":[{"tokens":[{"kind":"identifier","text":"blockingLoad"}]}]}},
 {"id":"mobile-assert","title":"Not-null assertion","severity":"MAJOR","needsJudgement":true,"description":"D","languages":[],"tags":[],"group":"Errors"},
 {"id":"mobile-lines","title":"Line over {max} characters","severity":"MINOR","description":"At most {max}.","why":"Long lines.","fix":"Wrap within {max}.","languages":[],"tags":[],"group":"Style",
  "match":{"detector":"line-length","max":100,"lines":"added"},"options":{"max":{"min":60,"max":200}}}
]}`

func briefKey(t *testing.T, project string, n int) core.Key {
	t.Helper()
	return mustKey(t, fmt.Sprintf("github:github.com/acr-plugin-sandbox/%s#%d", project, n))
}

func briefRuleset(t *testing.T, crit core.RepoCriteria) core.Ruleset {
	t.Helper()
	c, err := rules.LoadCatalog([]byte(briefCatalog))
	if err != nil {
		t.Fatal(err)
	}
	all, err := rules.Merge(rules.Core(), c)
	if err != nil {
		t.Fatal(err)
	}
	p := &packs.Pack{ID: "mobile", Rules: c}
	return core.EffectiveRules(core.RulesetInput{Set: &packs.Set{Packs: []*packs.Pack{p}, Rules: all}, Active: []*packs.Pack{p}, Profile: "app", Criteria: crit})
}

// screenFile is a synthetic Kotlin file of n lines whose diff replaces
// each of the given lines.
func screenFile(t *testing.T, path string, n int, changed ...int) core.PackageFileInput {
	t.Helper()
	head := make([]string, n)
	for i := range head {
		head[i] = fmt.Sprintf("    val value%d = %d", i+1, i+1)
	}
	head[0] = "package com.example.screens"
	var patch strings.Builder
	for _, l := range changed {
		fmt.Fprintf(&patch, "@@ -%d,1 +%d,1 @@ class Screen {\n-    val old%d = 0\n+%s\n", l, l, l, head[l-1])
	}
	d, err := core.NewFileDiff(path, "", core.FileModified, patch.String())
	if err != nil {
		t.Fatal(err)
	}
	return core.PackageFileInput{File: core.ChangedFile{Diff: d, Head: head, Base: head}, Language: "kotlin"}
}

func briefInput(t *testing.T, files ...core.PackageFileInput) core.WorkPackageInput {
	t.Helper()
	crit := core.RepoCriteria{
		Source: core.SourceOverlay, Packs: []string{"mobile"}, Files: []string{core.ConfigFile, core.CriteriaFile},
		DisabledRules: []string{"mobile-assert"}, RuleOptions: map[string]map[string]int{"mobile-lines": {"max": 120}},
		Rules: []core.RepositoryRule{{ID: "screen-names", Source: core.RepositorySource, Severity: core.Minor, Text: "Screens end in Screen.", File: core.CriteriaFile, Line: 4}},
	}
	return core.WorkPackageInput{
		RunID: "20261109-140000-brief", CR: briefKey(t, "sample-app", 12), URL: "https://github.com/acr-plugin-sandbox/sample-app/pull/12",
		Title: "Load the screens", Description: "Loads every screen at start.\nKeeps the old loader for now.",
		HeadSHA: strings.Repeat("c", 40), BaseSHA: strings.Repeat("d", 40), BaseRef: "main",
		Files: files, Ruleset: briefRuleset(t, crit), Profile: "app", ProfileReason: "set in .agent-code-review/config.json",
		Dimensions:     map[string]core.DimensionChoice{"ui-toolkit": {Value: "compose", Reason: "androidx.compose in app/build.gradle.kts"}},
		Criteria:       crit,
		Ancestors:      []core.Ancestor{{CR: briefKey(t, "core-lib", 4), Title: "Split the loader"}},
		RelatedChanges: []core.RelatedChange{{CR: briefKey(t, "core-lib", 4), Title: "Split the loader", WorkPackage: "acr-plugin-sandbox_core-lib-4-eeeeeeee/workpackage.json"}},
		JudgementPath:  "acr-plugin-sandbox_sample-app-12-cccccccc/judgement.json",
	}
}

func briefSignals(in *core.WorkPackageInput) {
	in.FileSignals = []rules.Signal{
		{RuleID: "mobile-blocking", Severity: "BLOCKER", Path: "app/src/main/java/com/example/screens/HomeScreen.kt", Line: 3, Column: 18, Origin: "mr"},
		{RuleID: "mobile-assert", NeedsJudgement: true, Path: "app/src/main/java/com/example/screens/HomeScreen.kt", Line: 2, Column: 9, Origin: "existing"},
	}
	in.ChangeRequestSignals = []rules.Signal{{RuleID: "uses-removed-api", NeedsJudgement: true, Path: "app/src/main/java/com/example/screens/HomeScreen.kt",
		Side: rules.SideLeft, Line: 3, Column: 5, Origin: "mr",
		References: []rules.Reference{{Project: "github:github.com/acr-plugin-sandbox/core-lib", Ref: "main", Path: "src/main/java/com/example/core/Loader.kt", Line: 8}}}}
}

func TestBriefSmall(t *testing.T) {
	in := briefInput(t, screenFile(t, "app/src/main/java/com/example/screens/HomeScreen.kt", 6, 3))
	in.Criteria.Ignored = []core.IgnoredItem{{Item: "`Bad Id` (.agent-code-review/criteria.md:9)", Reason: "the id is not kebab-case"}}
	in.Ruleset = briefRuleset(t, in.Criteria)
	in.NotVerified = []core.UnverifiedFile{{Path: "app/src/main/res/drawable/logo.png", Reason: "unsupported-language", Detail: "no pack covers .png files"}}
	logo, err := core.NewFileDiff("app/src/main/res/drawable/logo.png", "", core.FileAdded, "")
	if err != nil {
		t.Fatal(err)
	}
	in.Files = append(in.Files, core.PackageFileInput{File: core.ChangedFile{Diff: logo}})
	briefSignals(&in)
	wp, _ := core.BuildWorkPackage(in)
	got, err := Brief(wp, 1)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "brief-small", got)
	checkBrief(t, wp, 1, got)
}

func TestBriefParts(t *testing.T) {
	in := briefInput(t,
		screenFile(t, "app/src/main/java/com/example/screens/HomeScreen.kt", 6, 3),
		screenFile(t, "app/src/main/java/com/example/screens/ListScreen.kt", 330, 2, 200, 230),
		screenFile(t, "app/src/main/java/com/example/screens/EmptyScreen.kt", 4, 2),
	)
	briefSignals(&in)
	in.FileSignals = append(in.FileSignals, rules.Signal{RuleID: "mobile-lines", Severity: "MINOR", Path: "app/src/main/java/com/example/screens/ListScreen.kt", Line: 200, Column: 121, Origin: "mr"})
	in.PartBudget = 2000
	wp, _ := core.BuildWorkPackage(in)
	if wp.Parts.Total != 3 {
		t.Fatalf("%d parts, want 3: %v", wp.Parts.Total, wp.Parts.Files)
	}
	for n := 1; n <= 2; n++ {
		got, err := Brief(wp, n)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, fmt.Sprintf("brief-parts-%d", n), got)
		checkBrief(t, wp, n, got)
	}
	last, err := Brief(wp, 3)
	if err != nil {
		t.Fatal(err)
	}
	checkBrief(t, wp, 3, last)
	for _, n := range []int{0, 4} {
		if _, err := Brief(wp, n); err == nil || !strings.Contains(err.Error(), "3 parts") {
			t.Errorf("part %d: error %v", n, err)
		}
	}
}

// checkBrief checks what every brief must hold: its part, each signal it
// serves with whether it needs a decision, one pointer to the judgement
// contract, and the invariants of rendered text.
func checkBrief(t *testing.T, wp core.WorkPackage, n int, got string) {
	t.Helper()
	if again, _ := Brief(wp, n); again != got {
		t.Error("the same work package gives a different brief")
	}
	if !strings.Contains(got, fmt.Sprintf("part %d of %d", n, wp.Parts.Total)) {
		t.Errorf("part %d: the brief does not say which part it is", n)
	}
	if c := strings.Count(got, "schemas/judgement.schema.json"); c != 1 {
		t.Errorf("part %d: %d pointers to the judgement schema, want 1", n, c)
	}
	part, _ := wp.Part(n)
	for _, s := range append(part.Signals, part.ChangeRequestSignals...) {
		mark := "  " + s.ID + " [" + string(s.Severity) + "]"
		if s.NeedsJudgement {
			mark = "  " + s.ID + " [DECIDE]"
		}
		if !strings.Contains(got, mark) {
			t.Errorf("part %d: signal %s is not listed as %q", n, s.ID, mark)
		}
	}
	for _, f := range part.Files {
		if !strings.Contains(got, "== "+f.Path) {
			t.Errorf("part %d: no file %s", n, f.Path)
		}
	}
	for i, l := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if l != strings.TrimRight(l, " \t") || strings.Contains(l, "\r") {
			t.Errorf("part %d: line %d has trailing space or CR: %q", n, i+1, l)
		}
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("part %d: the brief does not end with exactly one line break", n)
	}
}

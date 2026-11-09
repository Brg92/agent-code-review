package core

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/lexer"
	"github.com/Brg92/agent-code-review/internal/rules"
)

func TestAddedLines(t *testing.T) {
	d, err := NewFileDiff("a.kt", "", FileModified, "@@ -1,3 +1,4 @@\n a\n-b\n+B\n+C\n c\n@@ -10,1 +11,2 @@\n x\n+y\n")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d.AddedLines(), map[int]bool{2: true, 3: true, 12: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("added %v, want %v", got, want)
	}
	if got := (FileDiff{Path: "b.png", Status: FileAdded}).AddedLines(); len(got) != 0 {
		t.Errorf("a diff without hunks adds %v", got)
	}
}

func TestRulesFile(t *testing.T) {
	d, err := NewFileDiff("app/src/main/B.kt", "app/src/main/A.kt", FileRenamed, "@@ -1,1 +1,2 @@\n a\n+b\n")
	if err != nil {
		t.Fatal(err)
	}
	assigned := rules.File{Path: "ignored", Language: "kotlin", Test: true, Source: "ignored"}
	got := ChangedFile{Diff: d, Head: []string{"a", "b"}}.RulesFile(assigned)
	want := rules.ChangedFile{
		File:    rules.File{Path: "app/src/main/B.kt", Language: "kotlin", Test: true, Source: "a\nb\n", Added: map[int]bool{2: true}},
		OldPath: "app/src/main/A.kt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RulesFile = %+v, want %+v", got, want)
	}
	gone := ChangedFile{Diff: FileDiff{Path: "app/src/main/C.kt", Status: FileRemoved}, Base: []string{"x"}}.RulesFile(rules.File{})
	if !gone.Removed || gone.Source != "" || gone.Path != "app/src/main/C.kt" || len(gone.Added) != 0 {
		t.Errorf("removed file = %+v", gone)
	}
}

func TestNumberSignals(t *testing.T) {
	files := []ChangedFile{{Diff: FileDiff{Path: "z/B.kt"}}, {Diff: FileDiff{Path: "a/A.kt"}}}
	fileSignals := []rules.Signal{
		{RuleID: "empty-catch", Severity: "BLOCKER", Path: "z/B.kt", Line: 9, Column: 3, Origin: rules.OriginMR},
		{RuleID: "no-double-bang", NeedsJudgement: true, Path: "a/A.kt", Line: 2, Column: 5, Origin: rules.OriginExisting},
		{RuleID: "line-too-long", Severity: "MINOR", Path: "z/B.kt", Line: 4, Column: 101, Origin: rules.OriginMR},
	}
	changeRequestSignals := []rules.Signal{
		{RuleID: "missing-tests", Severity: "MAJOR", Path: "z/B.kt", Line: 4, Column: 101, Origin: rules.OriginMR},
		{RuleID: "kmp-source-set-asymmetry", NeedsJudgement: true, Path: "a/A.kt", Line: 1, Column: 1, Origin: rules.OriginMR},
	}
	got := NumberSignals(files, fileSignals, changeRequestSignals)
	want := []Signal{
		{ID: "s1", Rule: "line-too-long", Severity: Minor, Path: "z/B.kt", Line: 4, Column: 101, Origin: OriginMR},
		{ID: "s2", Rule: "missing-tests", Severity: Major, Path: "z/B.kt", Line: 4, Column: 101, Origin: OriginMR},
		{ID: "s3", Rule: "empty-catch", Severity: Blocker, Path: "z/B.kt", Line: 9, Column: 3, Origin: OriginMR},
		{ID: "s4", Rule: "kmp-source-set-asymmetry", Path: "a/A.kt", Line: 1, Column: 1, Origin: OriginMR, NeedsJudgement: true},
		{ID: "s5", Rule: "no-double-bang", Path: "a/A.kt", Line: 2, Column: 5, Origin: OriginExisting, NeedsJudgement: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NumberSignals =\n%+v\nwant\n%+v", got, want)
	}
	stray := rules.Signal{RuleID: "r", Severity: "INFO", Path: "elsewhere.kt", Line: 1, Column: 1, Origin: rules.OriginMR}
	got = NumberSignals(files, []rules.Signal{stray}, fileSignals[:1])
	if len(got) != 2 || got[0].Path != "z/B.kt" || got[1].Path != "elsewhere.kt" || got[1].ID != "s2" {
		t.Errorf("a signal outside the changed files comes last: %+v", got)
	}
	if got := NumberSignals(files); got == nil || len(got) != 0 {
		t.Errorf("no signals: %#v, want an empty list", got)
	}
}

type memTree []string

func (t memTree) List(prefix string) []string {
	var out []string
	for _, p := range t {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}

// TestChangeRequestSignalsBecomeFindings runs the change-request rules of
// the core and android catalogs on a review, numbers their signals and
// finalizes them like file signals.
func TestChangeRequestSignalsBecomeFindings(t *testing.T) {
	read := func(parts ...string) []byte {
		data, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	android, err := rules.LoadCatalog(read("packs", "android", "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	kotlin, err := lexer.LoadTable(read("packs", "android", "lexer", "kotlin.json"))
	if err != nil {
		t.Fatal(err)
	}
	all, err := rules.Merge(rules.Core(), android)
	if err != nil {
		t.Fatal(err)
	}
	r := testReview(t)
	ads := mustFile(t, "app/src/free/a/Ads.kt", FileModified, "@@ -1,2 +1,3 @@\n a\n+b\n c")
	ads.Head = []string{"a", "b", "c"}
	repo := mustFile(t, "app/src/main/a/AdsRepository.kt", FileAdded, "@@ -0,0 +1,2 @@\n+package a\n+class AdsRepository")
	repo.Head = []string{"package a", "class AdsRepository"}
	r.Files, r.Rules = []ChangedFile{ads, repo}, RulesOf(rules.Core(), android)
	kt := rules.File{Language: "kotlin", Table: kotlin}
	cr := rules.ChangeRequest{
		Files:    []rules.ChangedFile{ads.RulesFile(kt), repo.RulesFile(kt)},
		Tree:     memTree{"app/src/free/a/Ads.kt", "app/src/paid/a/Ads.kt", "app/src/main/a/AdsRepository.kt"},
		Variants: []string{"free", "paid"},
	}
	r.Signals = NumberSignals(r.Files, nil, rules.RunChangeRequest(all, cr))
	want := []Signal{
		{ID: "s1", Rule: "variant-asymmetry", Path: "app/src/free/a/Ads.kt", Line: 2, Column: 1, Origin: OriginMR, NeedsJudgement: true},
		{ID: "s2", Rule: "missing-tests", Severity: Major, Path: "app/src/main/a/AdsRepository.kt", Line: 2, Column: 7, Origin: OriginMR},
	}
	if !reflect.DeepEqual(r.Signals, want) {
		t.Fatalf("signals %+v, want %+v", r.Signals, want)
	}
	j := Judgement{Schema: 1, CR: testCR, HeadSHA: testHead, Findings: []JudgedFinding{}, PreExisting: []JudgedFinding{},
		Signals: []SignalDecision{{ID: "s1", Decision: Confirm, Severity: Major}}}
	res, err := Finalize(r, j)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range res.Findings {
		got = append(got, fmt.Sprintf("%s %s %s:%d %s", f.Rule, f.Severity, f.Path, f.Line, f.Placement))
		if f.Why == "" || f.Fix == "" {
			t.Errorf("%s: no why or fix", f.Rule)
		}
	}
	wantFindings := []string{
		"variant-asymmetry MAJOR app/src/free/a/Ads.kt:2 " + string(Inline),
		"missing-tests MAJOR app/src/main/a/AdsRepository.kt:2 " + string(Inline),
	}
	if !reflect.DeepEqual(got, wantFindings) {
		t.Errorf("findings %v, want %v", got, wantFindings)
	}
	j.Signals = []SignalDecision{{ID: "s1", Decision: Dismiss, Reason: "the paid variant has no ads"}, {ID: "s2", Decision: Dismiss, Reason: "no logic to test"}}
	if res, err = Finalize(r, j); err != nil || len(res.Findings) != 0 || len(res.Dismissed) != 2 {
		t.Errorf("dismissed: %d findings, %d dismissed, %v", len(res.Findings), len(res.Dismissed), err)
	}
}

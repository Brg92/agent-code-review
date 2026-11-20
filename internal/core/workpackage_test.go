package core_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/jsonschema"
)

// checkWorkPackage checks a work package against its contract.
func checkWorkPackage(t *testing.T, wp core.WorkPackage) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "workpackage.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	schema, err := jsonschema.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := json.Marshal(wp)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := schema.Validate(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range issues {
		t.Errorf("%s: work package contract: %s", wp.CR, i)
	}
	return doc
}

// numbered returns n lines named after their number.
func numbered(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("line %d", i+1)
	}
	return out
}

// modified is a modified file of n lines at head whose diff changes each
// of the given lines.
func modified(t *testing.T, path string, n int, changed ...int) core.PackageFileInput {
	t.Helper()
	head := numbered(n)
	var patch strings.Builder
	for _, l := range changed {
		fmt.Fprintf(&patch, "@@ -%d,1 +%d,1 @@\n-old %d\n+%s\n", l, l, l, head[l-1])
	}
	d, err := core.NewFileDiff(path, "", core.FileModified, patch.String())
	if err != nil {
		t.Fatal(err)
	}
	return core.PackageFileInput{File: core.ChangedFile{Diff: d, Head: head, Base: head}, Language: "kotlin"}
}

func fileDiff(t *testing.T, path, old string, status core.FileStatus, patch string) core.FileDiff {
	t.Helper()
	d, err := core.NewFileDiff(path, old, status, patch)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

var testCR = core.Key{Forge: core.GitHub, Host: "github.com", Project: "acr-plugin-sandbox/sample-app", Number: 9}

// packageInput is a work package input around files, with the synthetic
// rule set.
func packageInput(t *testing.T, files ...core.PackageFileInput) core.WorkPackageInput {
	t.Helper()
	set, active := syntheticSet(t)
	crit := repoCriteria(repoA)
	crit.Files = []string{core.CriteriaFile}
	return core.WorkPackageInput{
		RunID: "20261109-120000-test", CR: testCR, URL: "https://github.com/acr-plugin-sandbox/sample-app/pull/9",
		Title: "Tidy the screens", Description: "Synthetic.", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40), BaseRef: "main",
		Files:   files,
		Ruleset: core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: "app", Criteria: crit}),
		Profile: "app", ProfileReason: "set in .agent-code-review/config.json",
		Dimensions:    map[string]core.DimensionChoice{"ui-toolkit": {Value: "compose", Reason: "set in .agent-code-review/config.json"}},
		Criteria:      crit,
		CI:            core.CI{State: core.CINone, Checks: []core.Check{}},
		JudgementPath: "acr-plugin-sandbox_sample-app-9-aaaaaaaa/judgement.json",
	}
}

func regions(f core.PackageFile) string {
	if f.Code == nil {
		return "no code"
	}
	var out []string
	for _, r := range f.Code.Regions {
		out = append(out, fmt.Sprintf("%d-%d", r.Start, r.Start+len(r.Lines)-1))
	}
	s := strings.Join(out, " ")
	if f.Code.Whole {
		s = "whole " + s
	}
	return s
}

func TestBuildWorkPackageCode(t *testing.T) {
	tests := []struct {
		name string
		file core.PackageFileInput
		in   func(in *core.WorkPackageInput)
		want string
	}{
		{"a file of 300 lines is whole", modified(t, "A.kt", 300, 150), nil, "whole 1-300"},
		{"a file of 301 lines is in regions", modified(t, "A.kt", 301, 150), nil, "130-170"},
		{"regions stop at the ends of the file", modified(t, "A.kt", 400, 5, 395), nil, "1-25 375-400"},
		{"overlapping regions merge", modified(t, "A.kt", 400, 100, 130), nil, "80-150"},
		{"adjacent regions merge", modified(t, "A.kt", 400, 100, 141), nil, "80-161"},
		{"regions a line apart stay apart", modified(t, "A.kt", 400, 100, 142), nil, "80-120 122-162"},
		{"the limits are parameters", modified(t, "A.kt", 50, 25), func(in *core.WorkPackageInput) { in.WholeFileLines, in.ContextLines = 40, 2 }, "23-27"},
		{"a file without a language has no code", core.PackageFileInput{File: modified(t, "A.kt", 10, 5).File}, nil, "no code"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := packageInput(t, tt.file)
			if tt.in != nil {
				tt.in(&in)
			}
			wp, _ := buildPackage(t, in)
			checkWorkPackage(t, wp)
			f := wp.Files[0]
			if got := regions(f); got != tt.want {
				t.Errorf("code %s, want %s", got, tt.want)
			}
			if f.Code != nil {
				for _, r := range f.Code.Regions {
					for i, l := range r.Lines {
						if want := fmt.Sprintf("line %d", r.Start+i); l != want {
							t.Fatalf("line %d is %q", r.Start+i, l)
						}
					}
				}
			}
			if f.Placement != "inline" || !strings.HasPrefix(f.Diff, "@@ -") {
				t.Errorf("placement %s, diff %q", f.Placement, f.Diff)
			}
		})
	}
}

func TestBuildWorkPackageDeletionHunk(t *testing.T) {
	d := fileDiff(t, "A.kt", "", core.FileModified, "@@ -200,2 +199,0 @@ fun a() {\n-gone 1\n-gone 2\n\\ No newline at end of file\n")
	wp, _ := buildPackage(t, packageInput(t, core.PackageFileInput{File: core.ChangedFile{Diff: d, Head: numbered(400)}, Language: "kotlin"}))
	checkWorkPackage(t, wp)
	if got := regions(wp.Files[0]); got != "179-219" {
		t.Errorf("code %s, want the lines around the removal", got)
	}
	if want := "@@ -200,2 +199,0 @@ fun a() {\n-gone 1\n-gone 2\n\\ No newline at end of file\n"; wp.Files[0].Diff != want {
		t.Errorf("diff %q, want %q", wp.Files[0].Diff, want)
	}
}

func TestBuildWorkPackageFilesWithoutCode(t *testing.T) {
	removed := fileDiff(t, "Old.kt", "", core.FileRemoved, "@@ -1,2 +0,0 @@\n-a\n-b\n")
	renamed := fileDiff(t, "new/Name.kt", "old/Name.kt", core.FileRenamed, "")
	copied := fileDiff(t, "copy/Name.kt", "old/Name.kt", core.FileCopied, "")
	generated := fileDiff(t, "Icons.kt", "", core.FileAdded, "")
	binary := fileDiff(t, "logo.png", "", core.FileAdded, "")
	in := packageInput(t,
		core.PackageFileInput{File: core.ChangedFile{Diff: removed, Base: []string{"a", "b"}}, Language: "kotlin"},
		core.PackageFileInput{File: core.ChangedFile{Diff: renamed, Head: []string{"x"}, Base: []string{"x"}}, Language: "kotlin"},
		core.PackageFileInput{File: core.ChangedFile{Diff: copied, Head: []string{"x"}}, Language: "kotlin"},
		core.PackageFileInput{File: core.ChangedFile{Diff: generated, Head: []string{"x"}}, Language: "kotlin"},
		core.PackageFileInput{File: core.ChangedFile{Diff: binary}},
	)
	in.NotVerified = []core.UnverifiedFile{{Path: "logo.png", Reason: "binary"}}
	wp, review := buildPackage(t, in)
	checkWorkPackage(t, wp)
	want := []core.PackageFile{
		{Path: "Old.kt", Status: core.FileRemoved, Language: "kotlin", Placement: core.Inline},
		{Path: "new/Name.kt", PreviousPath: "old/Name.kt", Status: core.FileRenamed, Language: "kotlin", Placement: core.Summary, Reason: "renamed-without-changes"},
		{Path: "copy/Name.kt", PreviousPath: "old/Name.kt", Status: core.FileCopied, Language: "kotlin", Placement: core.Summary, Reason: "copied-without-changes"},
		{Path: "Icons.kt", Status: core.FileAdded, Language: "kotlin", Placement: core.Summary, Reason: "patch-unavailable"},
		{Path: "logo.png", Status: core.FileAdded, Placement: core.Summary, Reason: "binary"},
	}
	if !reflect.DeepEqual(wp.Files, want) {
		t.Errorf("files =\n%+v\nwant\n%+v", wp.Files, want)
	}
	if len(review.Files) != len(want) || !reflect.DeepEqual(wp.NotVerified, in.NotVerified) {
		t.Errorf("review files %d, not verified %+v", len(review.Files), wp.NotVerified)
	}
}

// sizes returns the size of each file entry of a work package.
func sizes(t *testing.T, wp core.WorkPackage) []int {
	t.Helper()
	var out []int
	for _, f := range wp.Files {
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, len(data))
	}
	return out
}

func TestBuildWorkPackageParts(t *testing.T) {
	files := []core.PackageFileInput{modified(t, "A.kt", 20, 3), modified(t, "B.kt", 20, 4), modified(t, "C.kt", 20, 5)}
	all, _ := buildPackage(t, packageInput(t, files...))
	n := sizes(t, all)
	if n[0] != n[1] || n[1] != n[2] {
		t.Fatalf("sizes %v, want three files of the same size", n)
	}
	size := n[0]
	tests := []struct {
		name   string
		budget int
		want   string
	}{
		{"everything fits", 3 * size, "A.kt B.kt C.kt"},
		{"exactly at budget", 2 * size, "A.kt B.kt | C.kt"},
		{"one byte over", 2*size - 1, "A.kt | B.kt | C.kt"},
		{"every file is over the budget", size - 1, "A.kt | B.kt | C.kt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := packageInput(t, files...)
			in.PartBudget = tt.budget
			wp, _ := buildPackage(t, in)
			checkWorkPackage(t, wp)
			var parts []string
			for _, p := range wp.Parts.Files {
				parts = append(parts, strings.Join(p, " "))
			}
			if got := strings.Join(parts, " | "); got != tt.want || wp.Parts.Total != len(wp.Parts.Files) {
				t.Errorf("parts %q (total %d), want %q", got, wp.Parts.Total, tt.want)
			}
		})
	}
	big := modified(t, "Big.kt", 200, 100)
	in := packageInput(t, files[0], big, files[1])
	in.PartBudget = 2*size + 10
	wp, _ := buildPackage(t, in)
	if got := fmt.Sprint(wp.Parts.Files); got != "[[A.kt] [Big.kt] [B.kt]]" {
		t.Errorf("parts %s, want the large file alone, and no file split", got)
	}
	if got := sizes(t, wp); got[1] <= in.PartBudget {
		t.Fatalf("Big.kt is %d bytes, not over the budget %d", got[1], in.PartBudget)
	}
	def, _ := buildPackage(t, packageInput(t, files...))
	if def.Parts.Total != 1 || core.DefaultPartBudget != 40000 || core.DefaultWholeFileLines != 300 || core.DefaultContextLines != 20 {
		t.Errorf("defaults: %d parts, budget %d, whole %d, context %d", def.Parts.Total, core.DefaultPartBudget, core.DefaultWholeFileLines, core.DefaultContextLines)
	}
}

func TestBuildWorkPackageSignals(t *testing.T) {
	files := []core.PackageFileInput{modified(t, "A.kt", 20, 3), modified(t, "B.kt", 20, 4)}
	in := packageInput(t, files...)
	in.FileSignals = []rules.Signal{
		{RuleID: "mobile-plain", Severity: "MAJOR", Path: "B.kt", Line: 4, Column: 1, Origin: "mr"},
		{RuleID: "mobile-judged", NeedsJudgement: true, Path: "A.kt", Line: 3, Column: 2, Origin: "mr"},
	}
	in.ChangeRequestSignals = []rules.Signal{
		{RuleID: "uses-removed-api", NeedsJudgement: true, Path: "A.kt", Side: rules.SideLeft, Line: 3, Column: 1, Origin: "mr",
			References: []rules.Reference{{Project: "github:github.com/acr-plugin-sandbox/core-lib", Ref: "main", Path: "X.kt", Line: 2}}},
		{RuleID: "variant-asymmetry", Severity: "MAJOR", Path: "elsewhere/Gone.kt", Line: 1, Column: 1, Origin: "mr"},
	}
	in.PartBudget = 1
	wp, review := buildPackage(t, in)
	checkWorkPackage(t, wp)
	var ids []string
	for _, s := range wp.Signals {
		ids = append(ids, s.ID+" "+s.Rule+" "+s.Path)
	}
	want := []string{"s1 uses-removed-api A.kt", "s2 mobile-judged A.kt", "s3 mobile-plain B.kt", "s4 variant-asymmetry elsewhere/Gone.kt"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("signals %q, want %q", ids, want)
	}
	if got := wp.Parts.ChangeRequestSignals; !reflect.DeepEqual(got, []string{"s1", "s4"}) {
		t.Errorf("change-request signals %q, want s1 and s4", got)
	}
	if !reflect.DeepEqual(review.Signals, wp.Signals) || review.CR != wp.CR || review.HeadSHA != wp.HeadSHA {
		t.Errorf("the review and the work package disagree")
	}
	part, ok := wp.Part(2)
	if !ok || len(part.Files) != 1 || part.Files[0].Path != "B.kt" || len(part.Signals) != 1 || part.Signals[0].ID != "s3" || len(part.ChangeRequestSignals) != 0 {
		t.Errorf("part 2 %+v", part)
	}
	part, _ = wp.Part(1)
	if len(part.Signals) != 1 || part.Signals[0].ID != "s2" || len(part.ChangeRequestSignals) != 2 {
		t.Errorf("part 1 signals %+v, change-request signals %+v", part.Signals, part.ChangeRequestSignals)
	}
	if _, ok := wp.Part(3); ok {
		t.Error("part 3 of 2")
	}
	if _, ok := wp.Part(0); ok {
		t.Error("part 0")
	}
}

func TestBuildWorkPackageHeader(t *testing.T) {
	in := packageInput(t, modified(t, "A.kt", 20, 3))
	in.Ancestors = []core.Ancestor{{CR: core.Key{Forge: core.GitHub, Host: "github.com", Project: "acr-plugin-sandbox/sample-app", Number: 7}, Title: "Root"}}
	in.RelatedChanges = []core.RelatedChange{{CR: in.Ancestors[0].CR, Title: "Root", WorkPackage: "x/workpackage.json"}}
	in.Criteria.Ignored = []core.IgnoredItem{{Item: "`x` (a)", Reason: "r"}}
	set, _ := syntheticSet(t)
	in.Ruleset = core.EffectiveRules(core.RulesetInput{Set: set, Profile: "app", Criteria: in.Criteria})
	wp, review := buildPackage(t, in)
	checkWorkPackage(t, wp)
	if wp.Schema != 1 || wp.Criteria.Source != core.SourceOverlay || wp.Criteria.ProfileReason != in.ProfileReason ||
		!reflect.DeepEqual(wp.Criteria.Files, []string{core.CriteriaFile}) || !reflect.DeepEqual(wp.Criteria.Ignored, in.Criteria.Ignored) {
		t.Errorf("criteria %+v", wp.Criteria)
	}
	if !reflect.DeepEqual(wp.Cascade.Ancestors, in.Ancestors) || !reflect.DeepEqual(wp.RelatedChanges, in.RelatedChanges) {
		t.Errorf("cascade %+v, related %+v", wp.Cascade, wp.RelatedChanges)
	}
	if got := wp.Checklist.Repository; !reflect.DeepEqual(got, []string{"repo-a"}) {
		t.Errorf("repository checklist %q", got)
	}
	if len(review.Rules) != len(wp.Rules) || review.Rules["repo-a"] == nil {
		t.Errorf("review rules %d, work package rules %d", len(review.Rules), len(wp.Rules))
	}
}

func TestBuildWorkPackageIsDeterministic(t *testing.T) {
	build := func() []byte {
		in := packageInput(t, modified(t, "A.kt", 400, 10, 200), modified(t, "B.kt", 20, 3))
		in.Dimensions["platform-free"] = core.DimensionChoice{Value: "x", Reason: "y"}
		in.Dimensions["a-first"] = core.DimensionChoice{Value: "x", Reason: "y"}
		wp, _ := buildPackage(t, in)
		data, err := json.Marshal(wp)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := build()
	for i := 0; i < 20; i++ {
		if !bytes.Equal(build(), first) {
			t.Fatal("the same input gives different bytes")
		}
	}
	if data := checkWorkPackage(t, func() core.WorkPackage { wp, _ := buildPackage(t, packageInput(t)); return wp }()); bytes.Contains(data, []byte("null")) {
		t.Errorf("a work package without files has null lists: %s", data)
	}
}

// TestBuildWorkPackageRefusesControlCharactersInPaths checks that a file
// name cannot forge a line of the brief: a path with a control character,
// a newline or a tab included, is refused wherever the work package would
// carry it.
func TestBuildWorkPackageRefusesControlCharactersInPaths(t *testing.T) {
	file := func(path, oldPath string, status core.FileStatus) core.PackageFileInput {
		d, err := core.NewFileDiff(path, oldPath, status, "@@ -1,1 +1,1 @@\n-a\n+b\n")
		if err != nil {
			t.Fatal(err)
		}
		return core.PackageFileInput{File: core.ChangedFile{Diff: d, Head: []string{"b"}, Base: []string{"a"}}, Language: "kotlin"}
	}
	hostile := []string{"a\nb.kt", "a\tb.kt", "a\rb.kt", "a\x00b.kt", "a\x1bb.kt", "a\x7fb.kt", "a\u0085b.kt", "\n== forged.kt"}
	for _, bad := range hostile {
		for name, mutate := range map[string]func(*core.WorkPackageInput){
			"path": func(in *core.WorkPackageInput) { in.Files = []core.PackageFileInput{file(bad, "", core.FileModified)} },
			"previous path": func(in *core.WorkPackageInput) {
				in.Files = []core.PackageFileInput{file("ok.kt", bad, core.FileRenamed)}
			},
			"not verified path": func(in *core.WorkPackageInput) { in.NotVerified = []core.UnverifiedFile{{Path: bad, Reason: "binary"}} },
			"criteria file":     func(in *core.WorkPackageInput) { in.Criteria.Files = append(in.Criteria.Files, bad) },
			"file signal": func(in *core.WorkPackageInput) {
				in.FileSignals = []rules.Signal{{RuleID: "uses-removed-api", Path: bad, Line: 1, Column: 1, Origin: "mr"}}
			},
			"change request signal": func(in *core.WorkPackageInput) {
				in.ChangeRequestSignals = []rules.Signal{{RuleID: "uses-removed-api", Path: bad, Line: 1, Column: 1, Origin: "mr"}}
			},
			"signal reference": func(in *core.WorkPackageInput) {
				in.ChangeRequestSignals = []rules.Signal{{RuleID: "uses-removed-api", Path: "ok.kt", Line: 1, Column: 1, Origin: "mr",
					References: []rules.Reference{{Project: "github:github.com/a/b", Ref: "main", Path: bad, Line: 1}}}}
			},
		} {
			t.Run(fmt.Sprintf("%s %q", name, bad), func(t *testing.T) {
				in := packageInput(t, file("ok.kt", "", core.FileModified))
				mutate(&in)
				wp, review, err := core.BuildWorkPackage(in)
				var pe *core.PathError
				if !errors.As(err, &pe) {
					t.Fatalf("error %v is not a *core.PathError", err)
				}
				if strings.ContainsAny(pe.Error(), "\n\t\r\x00") {
					t.Errorf("the error message holds a raw control character: %q", pe.Error())
				}
				if len(wp.Files) != 0 || len(review.Files) != 0 {
					t.Error("a work package comes with the error")
				}
			})
		}
	}
}

func TestBuildWorkPackageAcceptsOrdinaryPaths(t *testing.T) {
	for _, path := range []string{"app/src/main/Repo.kt", "docs/My File.md", "src/日本語/ファイル.kt", "a--b/c.kt", "x%20y.kt", "back\\slash.kt"} {
		d, err := core.NewFileDiff(path, "", core.FileModified, "@@ -1,1 +1,1 @@\n-a\n+b\n")
		if err != nil {
			t.Fatal(err)
		}
		in := packageInput(t, core.PackageFileInput{File: core.ChangedFile{Diff: d, Head: []string{"b"}, Base: []string{"a"}}, Language: "kotlin"})
		if _, _, err := core.BuildWorkPackage(in); err != nil {
			t.Errorf("path %q: %v", path, err)
		}
	}
}

// buildPackage builds a work package that must be valid.
func buildPackage(t *testing.T, in core.WorkPackageInput) (core.WorkPackage, core.Review) {
	t.Helper()
	wp, review, err := core.BuildWorkPackage(in)
	if err != nil {
		t.Fatal(err)
	}
	return wp, review
}

func TestBuildWorkPackageCI(t *testing.T) {
	failure := "failure"
	tests := []struct {
		name string
		ci   core.CI
	}{
		{"none", core.CI{State: core.CINone, Checks: []core.Check{}}},
		{"failing", core.CI{State: core.CIFailing, Checks: []core.Check{{Name: "unit", Status: "completed", Conclusion: &failure}, {Name: "lint", Status: "queued"}}}},
		{"unknown", core.CI{State: core.CIUnknown, Reason: "forbidden", Detail: "needs checks: read", Checks: []core.Check{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := packageInput(t, modified(t, "A.kt", 20, 3))
			in.CI = tt.ci
			wp, _ := buildPackage(t, in)
			checkWorkPackage(t, wp)
			if !reflect.DeepEqual(wp.CI, tt.ci) {
				t.Errorf("ci %+v, want %+v", wp.CI, tt.ci)
			}
		})
	}
	t.Run("checks are never null", func(t *testing.T) {
		in := packageInput(t, modified(t, "A.kt", 20, 3))
		in.CI = core.CI{State: core.CINone}
		wp, _ := buildPackage(t, in)
		data := checkWorkPackage(t, wp)
		if !strings.Contains(string(data), `"ci":{"state":"none","checks":[]}`) {
			t.Errorf("work package ci: %s", data)
		}
	})
}

func TestBuildWorkPackageRefusesAnInvalidCI(t *testing.T) {
	tests := []struct {
		name string
		ci   core.CI
	}{
		{"no state", core.CI{}},
		{"unknown state", core.CI{State: "green"}},
		{"unknown without a reason", core.CI{State: core.CIUnknown}},
		{"reason on a known state", core.CI{State: core.CIPassing, Reason: "error"}},
		{"unknown reason", core.CI{State: core.CIUnknown, Reason: "timeout"}},
		{"checks on unknown", core.CI{State: core.CIUnknown, Reason: "error", Checks: []core.Check{{Name: "a", Status: "queued"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := packageInput(t, modified(t, "A.kt", 20, 3))
			in.CI = tt.ci
			var ce *core.CIError
			if _, _, err := core.BuildWorkPackage(in); !errors.As(err, &ce) {
				t.Errorf("error = %v, want a *CIError", err)
			}
		})
	}
}

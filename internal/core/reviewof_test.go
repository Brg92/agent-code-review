package core_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// bigAndSmall are a file larger than a whole-file code block, shown in
// regions, and a small one shown whole.
func bigAndSmall(t *testing.T) core.WorkPackageInput {
	t.Helper()
	return packageInput(t,
		modified(t, "app/src/main/kotlin/Big.kt", 400, 100, 300),
		modified(t, "app/src/main/kotlin/Small.kt", 10, 5))
}

func judgedFinding(path string, line int) core.JudgedFinding {
	return core.JudgedFinding{
		Rule: "mobile-judged", Severity: core.Minor, Path: path, Side: core.Right, Line: line,
		Title: "A title", Why: "Because.", Fix: "Fix it.", References: []core.Reference{},
	}
}

// TestReviewOfGivesTheFindingsOfTheOriginalReview: finalizing in another
// process, from the work package on disk, gives what finalizing in the
// process that built it gives: the same findings, anchors and ids, for
// every line the work package shows.
func TestReviewOfGivesTheFindingsOfTheOriginalReview(t *testing.T) {
	wp, orig, err := core.BuildWorkPackage(bigAndSmall(t))
	if err != nil {
		t.Fatal(err)
	}
	set, _ := syntheticSet(t)
	got, err := core.ReviewOf(wp, set)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CR.Same(orig.CR) || got.HeadSHA != orig.HeadSHA {
		t.Errorf("review of %s at %s, want %s at %s", got.CR, got.HeadSHA, orig.CR, orig.HeadSHA)
	}
	if len(got.Files) != len(orig.Files) {
		t.Fatalf("%d files, want %d", len(got.Files), len(orig.Files))
	}
	for i := range orig.Files {
		if !reflect.DeepEqual(got.Files[i].Diff, orig.Files[i].Diff) {
			t.Errorf("%s: diff %+v, want %+v", orig.Files[i].Diff.Path, got.Files[i].Diff, orig.Files[i].Diff)
		}
	}
	if len(got.Rules) != len(orig.Rules) {
		t.Errorf("%d rules, want %d", len(got.Rules), len(orig.Rules))
	}
	for id, want := range orig.Rules {
		r := got.Rules[id]
		if r == nil || r.Title != want.Title || r.Severity != want.Severity || r.NeedsJudgement != want.NeedsJudgement || r.Why != want.Why || r.Fix != want.Fix {
			t.Errorf("rule %s: %+v, want %+v", id, r, want)
		}
	}
	if !reflect.DeepEqual(got.Signals, orig.Signals) {
		t.Errorf("signals %+v, want %+v", got.Signals, orig.Signals)
	}

	big, small := "app/src/main/kotlin/Big.kt", "app/src/main/kotlin/Small.kt"
	j := core.Judgement{
		Schema: 1, CR: wpCR(wp), HeadSHA: wp.HeadSHA,
		Findings: []core.JudgedFinding{judgedFinding(big, 100), judgedFinding(small, 5)},
		// A context line next to the change, and a line far from it, in
		// the part of the file the work package does show.
		PreExisting: []core.JudgedFinding{judgedFinding(big, 105), judgedFinding(big, 299)},
	}
	for _, s := range wp.Signals {
		j.Signals = append(j.Signals, core.SignalDecision{ID: s.ID, Decision: core.Dismiss, Reason: "synthetic"})
	}
	want, err := core.Finalize(orig, j)
	if err != nil {
		t.Fatal(err)
	}
	have, err := core.Finalize(got, j)
	if err != nil {
		t.Fatalf("finalizing the rebuilt review: %v", err)
	}
	if !reflect.DeepEqual(have, want) {
		t.Errorf("finalized differently:\n got %+v\nwant %+v", have, want)
	}
}

func wpCR(wp core.WorkPackage) string { return wp.CR.String() }

// TestReviewOfKnowsTheLinesOfTheCodeItShows: a line past the end of a file
// shown whole is refused as it is in the original review; the end of a
// file shown in regions is not known, but what the work package does not
// show is refused whatever the end.
func TestReviewOfKnowsTheLinesOfTheCodeItShows(t *testing.T) {
	wp, _, err := core.BuildWorkPackage(bigAndSmall(t))
	if err != nil {
		t.Fatal(err)
	}
	set, _ := syntheticSet(t)
	r, err := core.ReviewOf(wp, set)
	if err != nil {
		t.Fatal(err)
	}
	small, big := "app/src/main/kotlin/Small.kt", "app/src/main/kotlin/Big.kt"
	j := core.Judgement{
		Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA, PreExisting: []core.JudgedFinding{},
		Findings: []core.JudgedFinding{judgedFinding(small, 11), judgedFinding(big, 400), judgedFinding(big, 7)},
	}
	ve, ok := core.ValidateJudgement(r, j).(core.ValidationError)
	if !ok || len(ve) != 3 || ve[0].Pointer != "/findings/0/line" || !strings.Contains(ve[0].Message, "past the end of "+small) ||
		ve[1].Pointer != "/findings/1/line" || ve[2].Pointer != "/findings/2/line" || !strings.Contains(ve[2].Message, "line 7 of "+big+" was not shown") {
		t.Errorf("errors: %v", ve)
	}
	j.Findings = []core.JudgedFinding{judgedFinding(big, 110), judgedFinding(big, 300)}
	res, err := core.Finalize(r, j)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Findings) != 2 || res.Findings[0].ID == "" || res.Findings[0].ID == res.Findings[1].ID {
		t.Errorf("findings %+v", res.Findings)
	}
}

// TestReviewOfRules: a rule is the one of the loaded packs with the
// options of the work package; a repository rule is made from its text;
// a rule no pack has is an error naming it.
func TestReviewOfRules(t *testing.T) {
	in := bigAndSmall(t)
	wp, _, err := core.BuildWorkPackage(in)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := syntheticSet(t)
	r, err := core.ReviewOf(wp, set)
	if err != nil {
		t.Fatal(err)
	}
	repo := r.Rules["repo-a"]
	if repo == nil || repo.Title != "Use the tracker." || repo.Severity != string(core.Major) {
		t.Errorf("repository rule %+v", repo)
	}
	if r.Rules["mobile-judged"] == nil || !r.Rules["mobile-judged"].NeedsJudgement {
		t.Errorf("pack rule %+v", r.Rules["mobile-judged"])
	}

	t.Run("pack not loaded", func(t *testing.T) {
		_, err := core.ReviewOf(wp, &packs.Set{Rules: rules.Core().Rules})
		if err == nil || !strings.Contains(err.Error(), "mobile-plain") || !strings.Contains(err.Error(), "mobile") {
			t.Errorf("error %v", err)
		}
	})
	t.Run("options", func(t *testing.T) {
		with := wp
		with.Rules = append([]core.PackageRule(nil), wp.Rules...)
		for i, pr := range with.Rules {
			if pr.ID == "mobile-lines" {
				with.Rules[i].Options = map[string]int{"max": 77}
			}
		}
		got, err := core.ReviewOf(with, set)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got.Rules["mobile-lines"].Title, "77") {
			t.Errorf("title %q does not name the option", got.Rules["mobile-lines"].Title)
		}
		with.Rules[0].Options = map[string]int{"nope": 1}
		if _, err := core.ReviewOf(with, set); err == nil {
			t.Error("an option the rule does not have was accepted")
		}
	})
}

// TestReviewOfBadWorkPackage: a diff that does not parse is an error.
func TestReviewOfBadWorkPackage(t *testing.T) {
	wp, _, err := core.BuildWorkPackage(bigAndSmall(t))
	if err != nil {
		t.Fatal(err)
	}
	set, _ := syntheticSet(t)
	bad := wp
	bad.Files = append([]core.PackageFile(nil), wp.Files...)
	bad.Files[0].Diff = "@@ nonsense\n"
	if _, err := core.ReviewOf(bad, set); err == nil || !strings.Contains(err.Error(), bad.Files[0].Path) {
		t.Errorf("error %v", err)
	}
	bad.Files[0].Diff, bad.Files[0].Status = "", "weird"
	if _, err := core.ReviewOf(bad, set); err == nil {
		t.Error("an unknown status was accepted")
	}
}

// shiftedBigFile is a file of 400 lines shown in regions around its two
// changes, 100 and 300, with the texts "target A" and "target B" far from
// both, as lines 200 and 350 and moved down by shift lines, as a push that
// adds lines above would move them. The existing code there has a signal
// of the same rule on each.
func shiftedBigFile(t *testing.T, shift int) core.WorkPackageInput {
	t.Helper()
	const path = "app/src/main/kotlin/Big.kt"
	head := numbered(400 + shift)
	head[199+shift], head[349+shift] = "target A", "target B"
	var patch strings.Builder
	for _, l := range []int{100 + shift, 300 + shift} {
		fmt.Fprintf(&patch, "@@ -%d,1 +%d,1 @@\n-old %d\n+%s\n", l, l, l, head[l-1])
	}
	d, err := core.NewFileDiff(path, "", core.FileModified, patch.String())
	if err != nil {
		t.Fatal(err)
	}
	in := packageInput(t, core.PackageFileInput{File: core.ChangedFile{Diff: d, Head: head, Base: head}, Language: "kotlin"})
	for _, line := range []int{200 + shift, 350 + shift} {
		in.FileSignals = append(in.FileSignals, rules.Signal{RuleID: "mobile-lines", Severity: "MINOR", Path: path, Line: line, Column: 1, Origin: rules.OriginExisting})
	}
	return in
}

// TestFingerprintOfASignalOutsideTheRegionsStartsFromItsLineText: the
// findings of two signals of one rule in one file, on lines the work
// package does not show, are told apart by the text of their lines, not by
// their order, so they keep their ids when a push moves them.
func TestFingerprintOfASignalOutsideTheRegionsStartsFromItsLineText(t *testing.T) {
	set, _ := syntheticSet(t)
	ids := func(shift int) (map[string]string, core.Result) {
		wp, orig, err := core.BuildWorkPackage(shiftedBigFile(t, shift))
		if err != nil {
			t.Fatal(err)
		}
		if len(wp.Signals) != 2 || wp.Signals[0].LineText != "target A" || wp.Signals[1].LineText != "target B" {
			t.Fatalf("signals %+v", wp.Signals)
		}
		r, err := core.ReviewOf(wp, set)
		if err != nil {
			t.Fatal(err)
		}
		j := core.Judgement{Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA, Findings: []core.JudgedFinding{}, PreExisting: []core.JudgedFinding{}}
		res, err := core.Finalize(r, j)
		if err != nil {
			t.Fatal(err)
		}
		// What the collecting process, with the whole file, makes of them.
		want, err := core.Finalize(orig, j)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(res, want) {
			t.Errorf("shift %d: finalized differently:\n got %+v\nwant %+v", shift, res, want)
		}
		byText := map[string]string{}
		for i, f := range res.PreExisting {
			byText[wp.Signals[i].LineText] = f.ID
		}
		return byText, res
	}
	before, res := ids(0)
	if len(res.PreExisting) != 2 || before["target A"] == "" || before["target A"] == before["target B"] {
		t.Fatalf("findings %+v", res.PreExisting)
	}
	after, _ := ids(5)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("ids moved with the lines: %v, then %v", before, after)
	}
}

// TestReviewOfShowsOnlyWhatTheWorkPackageShows: the text of a line the work
// package carries nowhere is not made up.
func TestReviewOfShowsOnlyWhatTheWorkPackageShows(t *testing.T) {
	wp, _, err := core.BuildWorkPackage(shiftedBigFile(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	set, _ := syntheticSet(t)
	r, err := core.ReviewOf(wp, set)
	if err != nil {
		t.Fatal(err)
	}
	// Line 7 is outside the regions and has no signal.
	j := core.Judgement{Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA, Findings: []core.JudgedFinding{judgedFinding("app/src/main/kotlin/Big.kt", 7)}, PreExisting: []core.JudgedFinding{}}
	_, err = core.Finalize(r, j)
	if err == nil {
		t.Fatal("a finding on a line the work package does not show was finalized")
	}
}

// TestValidateJudgementRefusesLinesTheAgentWasNotShown: a finding is on
// lines of the file the brief showed: all lines of a file shown whole, the
// lines of its regions, and the lines of the diff of its side.
func TestValidateJudgementRefusesLinesTheAgentWasNotShown(t *testing.T) {
	in := bigAndSmall(t)
	// A file the platform gave no patch of: nothing of it was shown.
	in.Files = append(in.Files, core.PackageFileInput{File: core.ChangedFile{Diff: core.FileDiff{Path: "app/src/main/kotlin/Gen.kt", Status: core.FileModified}}})
	wp, _, err := core.BuildWorkPackage(in)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := syntheticSet(t)
	r, err := core.ReviewOf(wp, set)
	if err != nil {
		t.Fatal(err)
	}
	const big, small, gen = "app/src/main/kotlin/Big.kt", "app/src/main/kotlin/Small.kt", "app/src/main/kotlin/Gen.kt"
	at := func(path string, side core.Side, start, line int) core.JudgedFinding {
		f := judgedFinding(path, line)
		f.Side, f.StartLine = side, start
		return f
	}
	tests := []struct {
		name    string
		finding core.JudgedFinding
		pointer string // empty: accepted
		message string
	}{
		{"an added line", at(big, core.Right, 0, 100), "", ""},
		{"a line of a region", at(big, core.Right, 0, 110), "", ""},
		{"the first line of a region", at(big, core.Right, 0, 80), "", ""},
		{"a line of a file shown whole", at(small, core.Right, 0, 9), "", ""},
		{"a removed line", at(big, core.Left, 0, 100), "", ""},
		{"a range inside a region", at(big, core.Right, 95, 105), "", ""},
		{"a file with nothing shown", at(gen, core.Right, 0, 40), "", ""},
		{"a line far from the regions", at(big, core.Right, 0, 7), "/findings/0/line", "line 7 of " + big + " was not shown to the agent"},
		{"the line after a region", at(big, core.Right, 0, 121), "/findings/0/line", "line 121 of " + big + " was not shown to the agent"},
		{"an unchanged line on the left", at(big, core.Left, 0, 110), "/findings/0/line", "line 110 of " + big + " was not shown to the agent"},
		{"a range that starts outside", at(big, core.Right, 70, 100), "/findings/0/startLine", "line 70 of " + big + " was not shown to the agent"},
		{"a range that crosses a gap", at(big, core.Right, 100, 290), "/findings/0/startLine", "line 121 of " + big + " was not shown to the agent"},
	}
	for _, tt := range tests {
		for _, list := range []string{"findings", "preExisting"} {
			t.Run(tt.name+" in "+list, func(t *testing.T) {
				j := core.Judgement{Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA, Findings: []core.JudgedFinding{}, PreExisting: []core.JudgedFinding{}}
				if list == "findings" {
					j.Findings = []core.JudgedFinding{tt.finding}
				} else {
					j.PreExisting = []core.JudgedFinding{tt.finding}
				}
				err := core.ValidateJudgement(r, j)
				if tt.pointer == "" {
					if err != nil {
						t.Errorf("refused: %v", err)
					}
					return
				}
				ve, ok := err.(core.ValidationError)
				if !ok || len(ve) != 1 || ve[0].Pointer != strings.Replace(tt.pointer, "findings", list, 1) || !strings.Contains(ve[0].Message, tt.message) {
					t.Errorf("errors %v, want %s: %s", err, tt.pointer, tt.message)
				}
			})
		}
	}
}

package core_test

import (
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

// TestReviewOfKnowsTheLinesOfTheCodeItShows: a line past the end of a
// file shown whole is refused as it is in the original review; a line of
// a file shown in regions whose end is not known is not.
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
		Schema: 1, CR: wp.CR.String(), HeadSHA: wp.HeadSHA,
		Findings: []core.JudgedFinding{judgedFinding(small, 11), judgedFinding(big, 400), judgedFinding(big, 7)},
	}
	err = core.ValidateJudgement(r, j)
	ve, ok := err.(core.ValidationError)
	if !ok || len(ve) != 1 || ve[0].Pointer != "/findings/0/line" || !strings.Contains(ve[0].Message, "past the end of "+small) {
		t.Errorf("errors: %v", err)
	}
	res, err := core.Finalize(r, core.Judgement{Schema: 1, CR: j.CR, HeadSHA: j.HeadSHA, Findings: j.Findings[1:]})
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

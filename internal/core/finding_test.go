package core

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// brief describes a finding as "<rule> <severity> <path>:<side><line>
// <anchor> <placement>", with " crash" and " from <signal>" when they apply.
func brief(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		s := fmt.Sprintf("%s %s %s:%s%d %s %s", f.Rule, f.Severity, f.Path, f.Side, f.Line, show(f.Anchor), f.Placement)
		if f.Crash {
			s += " crash"
		}
		if f.Signal != "" {
			s += " from " + f.Signal
		}
		out = append(out, s)
	}
	return out
}

func TestFinalize(t *testing.T) {
	res, err := Finalize(testReview(t), testJudgement())
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s\n%q\nwant\n%q", name, got, want)
		}
	}
	check("findings", brief(res.Findings), []string{
		"empty-catch BLOCKER app/A.kt:RIGHT2 added RIGHT 2 inline from s1",
		"no-double-bang MAJOR app/A.kt:RIGHT3 context RIGHT 3 inline from s2",
		"logic.always-throws BLOCKER app/A.kt:RIGHT6 relocated RIGHT 4 for 6 inline",
	})
	check("pre-existing", brief(res.PreExisting), []string{
		"no-double-bang BLOCKER app/A.kt:RIGHT5 relocated RIGHT 4 for 5 inline crash from s3",
		"logic.always-throws MAJOR app/Old.kt:LEFT2 removed LEFT 2 inline",
	})
	// Findings of signals take the text of their rule.
	if f := res.Findings[0]; f.Title != "Empty catch block" || f.Why != "The error vanishes." || f.Fix != "Handle or rethrow it." || f.References == nil {
		t.Errorf("signal finding text = %+v", f)
	}
	if f := res.Findings[2]; f.Title != "f() always throws" || len(f.References) != 1 {
		t.Errorf("judged finding = %+v", f)
	}
	if res.CR != testReview(t).CR || res.HeadSHA != testHead {
		t.Errorf("result for %s %s", res.CR, res.HeadSHA)
	}
	j := testJudgement()
	if !reflect.DeepEqual(res.Questions, j.Questions) || !reflect.DeepEqual(res.Notes, j.Notes) || !reflect.DeepEqual(res.NotVerified, j.NotVerified) {
		t.Errorf("report fields not kept: %+v %+v %+v", res.Questions, res.Notes, res.NotVerified)
	}
	if len(res.Dismissed) != 0 {
		t.Errorf("dismissed = %+v", res.Dismissed)
	}
}

func TestFinalizeDismissedSignals(t *testing.T) {
	j := testJudgement()
	j.Signals = append(j.Signals[:1], SignalDecision{ID: "s3", Decision: Dismiss, Reason: "the value is checked above"},
		SignalDecision{ID: "s1", Decision: Dismiss, Reason: "the catch logs through a wrapper"})
	res, err := Finalize(testReview(t), j)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range res.Dismissed {
		got = append(got, d.Signal.ID+" "+d.Signal.Rule+" "+d.Reason)
	}
	want := []string{"s1 empty-catch the catch logs through a wrapper", "s3 no-double-bang the value is checked above"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dismissed %q, want %q", got, want)
	}
	for _, f := range append(res.Findings, res.PreExisting...) {
		if f.Signal == "s1" || f.Signal == "s3" {
			t.Errorf("dismissed signal %s became a finding", f.Signal)
		}
	}
}

func TestFinalizeSuggestions(t *testing.T) {
	r := testReview(t)
	big, err := NewFileDiff("app/Big.kt", "", FileAdded, "")
	if err != nil {
		t.Fatal(err)
	}
	r.Files = append(r.Files, ChangedFile{Diff: big})
	r.Rules["big"] = RuleInfo{Severity: Minor, Title: "t", Why: "w", Fix: "f"}
	tests := []struct {
		name            string
		path            string
		side            Side
		start, line     int
		keep            bool
		placement, show string
	}{
		{"added and context lines of one hunk", "app/A.kt", Right, 2, 3, true, "inline", "context RIGHT 3"},
		{"single added line", "app/A.kt", Right, 0, 2, true, "inline", "added RIGHT 2"},
		{"lines past the hunk", "app/A.kt", Right, 3, 5, false, "inline", "relocated RIGHT 4 for 5"},
		{"left side", "app/Old.kt", Left, 0, 1, false, "inline", "removed LEFT 1"},
		{"file without a patch", "app/Big.kt", Right, 0, 1, false, "summary", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := testJudgement()
			rule := "logic.always-throws"
			if tt.path == "app/Big.kt" {
				rule = "big"
			}
			f := JudgedFinding{Rule: rule, Severity: Minor, Path: tt.path, Side: tt.side, StartLine: tt.start, Line: tt.line,
				Title: "t", Why: "w", Fix: "Do this.", Suggestion: new("x := 1"), References: []Reference{}}
			if tt.side == Left {
				j.PreExisting = []JudgedFinding{f}
			} else {
				j.Findings = []JudgedFinding{f}
			}
			res, err := Finalize(r, j)
			if err != nil {
				t.Fatal(err)
			}
			got := res.Findings[len(res.Findings)-1]
			if tt.side == Left {
				got = res.PreExisting[len(res.PreExisting)-1]
			}
			if string(got.Placement) != tt.placement || show(got.Anchor) != tt.show {
				t.Errorf("placement %s at %s, want %s at %s", got.Placement, show(got.Anchor), tt.placement, tt.show)
			}
			if tt.keep {
				if got.Suggestion == nil || *got.Suggestion != "x := 1" || got.Fix != "Do this." {
					t.Errorf("suggestion %v, fix %q: want the suggestion kept", got.Suggestion, got.Fix)
				}
				return
			}
			if got.Suggestion != nil || got.Fix != "Do this.\n\n```\nx := 1\n```" {
				t.Errorf("suggestion %v, fix %q: want it turned into a code block of the fix", got.Suggestion, got.Fix)
			}
		})
	}
}

func idsOf(fs ...[]Finding) []string {
	var out []string
	for _, l := range fs {
		for _, f := range l {
			out = append(out, f.ID)
		}
	}
	return out
}

func TestFinalizeFingerprints(t *testing.T) {
	r := testReview(t)
	res, err := Finalize(r, testJudgement())
	if err != nil {
		t.Fatal(err)
	}
	// The text of the requested line is read from the file version of the
	// finding's side, even outside the hunks.
	want := []string{
		FindingFingerprint("empty-catch", "app/A.kt", "b", 0),
		FindingFingerprint("no-double-bang", "app/A.kt", "c", 0),
		FindingFingerprint("logic.always-throws", "app/A.kt", "f", 0),
		FindingFingerprint("no-double-bang", "app/A.kt", "e", 0),
		FindingFingerprint("logic.always-throws", "app/Old.kt", "q", 0),
	}
	if got := idsOf(res.Findings, res.PreExisting); !reflect.DeepEqual(got, want) {
		t.Errorf("ids\n%q\nwant\n%q", got, want)
	}

	// The same text on a shifted line keeps its fingerprint: two lines
	// added above it.
	shifted := testReview(t)
	shifted.Files[0] = mustFile(t, "app/A.kt", FileModified, "@@ -1,3 +1,6 @@\n a\n+n1\n+n2\n+b\n c\n d")
	shifted.Files[0].Head = []string{"a", "n1", "n2", "b", "c", "d", "e", "f"}
	shifted.Files[0].Base = r.Files[0].Base
	for i := range shifted.Signals {
		shifted.Signals[i].Line += 2
	}
	j := testJudgement()
	j.Findings[0].Line = 8
	moved, err := Finalize(shifted, j)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(moved.Findings, moved.PreExisting); !reflect.DeepEqual(got, want) {
		t.Errorf("after the lines moved, ids\n%q\nwant\n%q", got, want)
	}
}

func TestFinalizeOccurrenceIndex(t *testing.T) {
	r := testReview(t)
	r.Files[1].Head = []string{"  launch {", "x", "launch   {"}
	r.Signals = nil
	finding := func(line int) JudgedFinding {
		return JudgedFinding{Rule: "logic.always-throws", Severity: Major, Path: "app/New.kt", Side: Right, Line: line,
			Title: "t", Why: "w", Fix: "f", References: []Reference{}}
	}
	j := testJudgement()
	j.Signals = nil
	j.Findings = []JudgedFinding{finding(3), finding(2), finding(1)}
	j.PreExisting = nil
	res, err := Finalize(r, j)
	if err != nil {
		t.Fatal(err)
	}
	// Lines 1 and 3 have the same normalised text: line 1 is occurrence 0
	// and line 3 occurrence 1, whatever the order of the judgement.
	want := []string{
		FindingFingerprint("logic.always-throws", "app/New.kt", "launch {", 1),
		FindingFingerprint("logic.always-throws", "app/New.kt", "x", 0),
		FindingFingerprint("logic.always-throws", "app/New.kt", "launch {", 0),
	}
	if got := idsOf(res.Findings); !reflect.DeepEqual(got, want) {
		t.Errorf("ids\n%q\nwant\n%q", got, want)
	}
	if want[0] == want[2] {
		t.Error("two identical lines share a fingerprint")
	}
}

func TestFinalizeErrors(t *testing.T) {
	j := testJudgement()
	j.Schema = 0
	_, err := Finalize(testReview(t), j)
	var ve ValidationError
	if !errors.As(err, &ve) || len(ve) != 1 {
		t.Errorf("invalid judgement: %v, want its ValidationError", err)
	}

	r := testReview(t)
	r.Rules["empty-catch"] = RuleInfo{Severity: Blocker, Title: "Empty catch block", Why: "The error vanishes."}
	if _, err := Finalize(r, testJudgement()); err == nil || !strings.Contains(err.Error(), `rule "empty-catch" has no fix`) {
		t.Errorf("rule without a fix: %v", err)
	}

	r = testReview(t)
	r.Signals = append(r.Signals, Signal{ID: "s4", Rule: "gone", Severity: Minor, Path: "app/A.kt", Line: 1, Column: 1, Origin: OriginMR})
	if _, err := Finalize(r, testJudgement()); err == nil || !strings.Contains(err.Error(), `signal s4: rule "gone"`) {
		t.Errorf("signal of an unknown rule: %v", err)
	}
}

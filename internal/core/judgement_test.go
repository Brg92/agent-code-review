package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

const (
	testCR   = "github:github.com/acr-plugin-sandbox/sample-app#2"
	testHead = "76ae5f13ca1575b65fe1567c471e8a7b51a00af4"
)

// testReview is a change request with a modified, an added and a removed
// file, three rules and three signals.
func testReview(t *testing.T) Review {
	t.Helper()
	cr, err := ParseKey(testCR)
	if err != nil {
		t.Fatal(err)
	}
	modified := mustFile(t, "app/A.kt", FileModified, "@@ -1,3 +1,4 @@\n a\n+b\n c\n d")
	modified.Base = []string{"a", "c", "d", "e", "f"}
	modified.Head = []string{"a", "b", "c", "d", "e", "f"}
	added := mustFile(t, "app/New.kt", FileAdded, "@@ -0,0 +1,3 @@\n+x\n+y\n+z")
	added.Head = []string{"x", "y", "z"}
	removed := mustFile(t, "app/Old.kt", FileRemoved, "@@ -1,2 +0,0 @@\n-p\n-q")
	removed.Base = []string{"p", "q"}
	return Review{
		CR:      cr,
		HeadSHA: testHead,
		Files:   []ChangedFile{modified, added, removed},
		Rules: RulesOf(&rules.Catalog{Pack: "test", Rules: []*rules.Rule{
			{ID: "empty-catch", Severity: "BLOCKER", Title: "Empty catch block", Why: "The error vanishes.", Fix: "Handle or rethrow it."},
			{ID: "no-double-bang", Severity: "MAJOR", NeedsJudgement: true, Title: "Not-null assertion", Why: "It throws on null.", Fix: "Handle null."},
			{ID: "logic-defect", Severity: "BLOCKER", Title: "Always throws", Why: "The call always fails.", Fix: "Remove the failing path."},
			{ID: "dead-code", Severity: "MAJOR", Title: "Dead code", Why: "It hides bugs.", Fix: "Remove it."},
		}}),
		Signals: []Signal{
			{ID: "s1", Rule: "empty-catch", Severity: Blocker, Path: "app/A.kt", Line: 2, Column: 5, Origin: OriginMR},
			{ID: "s2", Rule: "no-double-bang", Path: "app/A.kt", Line: 3, Column: 9, Origin: OriginMR, NeedsJudgement: true},
			{ID: "s3", Rule: "no-double-bang", Path: "app/A.kt", Line: 5, Column: 9, Origin: OriginExisting, NeedsJudgement: true},
		},
	}
}

func mustFile(t *testing.T, path string, status FileStatus, patch string) ChangedFile {
	t.Helper()
	d, err := NewFileDiff(path, "", status, patch)
	if err != nil {
		t.Fatal(err)
	}
	return ChangedFile{Diff: d}
}

// testJudgement is a valid judgement of testReview.
func testJudgement() Judgement {
	return Judgement{
		Schema:  1,
		CR:      testCR,
		HeadSHA: testHead,
		Findings: []JudgedFinding{{
			Rule: "logic-defect", Severity: Blocker, Path: "app/A.kt", Side: Right, Line: 6,
			Title: "f() always throws", Why: "Nothing sets the cache.", Fix: "Set the cache in init.",
			References: []Reference{{Project: "github:github.com/acr-plugin-sandbox/core-lib", Ref: "main", Path: "src/C.kt", Line: 3}},
		}},
		PreExisting: []JudgedFinding{{
			Rule: "dead-code", Severity: Major, Path: "app/Old.kt", Side: Left, Line: 2,
			Title: "q() is unreachable", Why: "Dead code hides bugs.", Fix: "Remove it.",
		}},
		Signals: []SignalDecision{
			{ID: "s2", Decision: Confirm, Severity: Major},
			{ID: "s3", Decision: Confirm, Severity: Blocker, Crash: true},
		},
		Questions:   []Question{{Path: "app/A.kt", Line: 2, Text: "Is the catch meant to be empty?"}},
		Notes:       []string{"Reviewed with the app profile."},
		NotVerified: []string{"How the cache is filled in core-lib."},
	}
}

func pointers(err error) []string {
	var ve ValidationError
	if !errors.As(err, &ve) {
		return []string{"not a ValidationError: " + err.Error()}
	}
	var out []string
	for _, e := range ve {
		out = append(out, e.Pointer)
	}
	return out
}

func TestValidateJudgementAcceptsAValidJudgement(t *testing.T) {
	if err := ValidateJudgement(testReview(t), testJudgement()); err != nil {
		t.Fatal(err)
	}
}

// TestValidateJudgementReportsEveryError checks that one judgement with
// many problems reports each of them, in document order, with its JSON
// pointer.
func TestValidateJudgementReportsEveryError(t *testing.T) {
	j := testJudgement()
	j.Schema = 2
	j.HeadSHA = strings.Repeat("0", 40)
	j.Findings = append(j.Findings,
		JudgedFinding{Rule: "unknown-rule", Severity: "CRITICAL", Path: "app/Other.kt", Side: "UP", Line: 0, Title: "a\nb", Why: " ", References: []Reference{}},
		JudgedFinding{Rule: "empty-catch", Severity: Major, Path: "app/New.kt", Side: Right, StartLine: 3, Line: 2, Title: "t", Why: "w", Fix: "f", Crash: true,
			References: []Reference{{Project: "github.com/x", Ref: "", Path: "../x.kt", Line: 0}}},
	)
	j.PreExisting[0].Crash = true
	j.Signals = []SignalDecision{
		{ID: "s3", Decision: Confirm, Severity: Major, Crash: true},
		{ID: "s9", Decision: Dismiss, Reason: "gone"},
		{ID: "s3", Decision: "maybe"},
		{ID: "s1", Decision: Dismiss, Severity: Minor},
	}
	j.Questions = []Question{{Line: 4, Text: ""}}
	j.Notes = []string{"one", strings.Repeat("x", 201), "three", ""}
	j.NotVerified = []string{"\t"}

	got := pointers(ValidateJudgement(testReview(t), j))
	want := []string{
		"/schema",
		"/headSha",
		"/findings/1/rule",
		"/findings/1/severity",
		"/findings/1/path",
		"/findings/1/side",
		"/findings/1/line",
		"/findings/1/title",
		"/findings/1/why",
		"/findings/1/fix",
		"/findings/2/severity",
		"/findings/2/startLine",
		"/findings/2/crash",
		"/findings/2/references/0/project",
		"/findings/2/references/0/ref",
		"/findings/2/references/0/path",
		"/findings/2/references/0/line",
		"/preExisting/0/crash",
		"/signals/0/crash",
		"/signals/1/id",
		"/signals/2/id",
		"/signals/2/decision",
		"/signals/3/severity",
		"/signals/3/reason",
		"/signals",
		"/questions/0/path",
		"/questions/0/text",
		"/notes",
		"/notes/1",
		"/notes/3",
		"/notVerified/0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pointers\n%q\nwant\n%q\n%v", got, want, ValidateJudgement(testReview(t), j))
	}
}

func TestValidateJudgementSingleErrors(t *testing.T) {
	tests := []struct {
		name   string
		change func(j *Judgement)
		want   string
	}{
		{"other change request", func(j *Judgement) { j.CR = "github:github.com/acr-plugin-sandbox/sample-app#3" }, "/cr"},
		{"malformed change request", func(j *Judgement) { j.CR = "sample-app#2" }, "/cr"},
		{"same change request in another case", func(j *Judgement) { j.CR = "github:github.com/ACR-plugin-sandbox/Sample-App#2" }, ""},
		{"other head", func(j *Judgement) { j.HeadSHA = "1bb2c03af22c5360e04698de42d0833c0387b499" }, "/headSha"},
		{"core rule of another stack is unknown", func(j *Judgement) { j.Findings[0].Rule = "kmp-jvm-api-in-common" }, "/findings/0/rule"},
		{"path that is not a changed file", func(j *Judgement) { j.Findings[0].Path = "app/B.kt" }, "/findings/0/path"},
		{"reference outside the change request", func(j *Judgement) { j.Findings[0].References[0].Path = "anywhere/Else.kt" }, ""},
		{"line past the end of the head file", func(j *Judgement) { j.Findings[0].Line = 7 }, "/findings/0/line"},
		{"line past the end of the base file", func(j *Judgement) { j.Findings[0].Side, j.Findings[0].Line = Left, 6 }, "/findings/0/line"},
		{"left side of an added file", func(j *Judgement) { j.Findings[0].Path, j.Findings[0].Side, j.Findings[0].Line = "app/New.kt", Left, 1 }, "/findings/0/side"},
		{"right side of a removed file", func(j *Judgement) { j.PreExisting[0].Side = Right }, "/preExisting/0/side"},
		{"start line after line", func(j *Judgement) { j.Findings[0].StartLine = 7 }, "/findings/0/startLine"},
		{"start line equal to line", func(j *Judgement) { j.Findings[0].StartLine = 6 }, ""},
		{"negative start line", func(j *Judgement) { j.Findings[0].StartLine = -1 }, "/findings/0/startLine"},
		{"title on two lines", func(j *Judgement) { j.Findings[0].Title = "f()\nthrows" }, "/findings/0/title"},
		{"blank why", func(j *Judgement) { j.Findings[0].Why = "" }, "/findings/0/why"},
		{"no fix", func(j *Judgement) { j.Findings[0].Fix = " \n" }, "/findings/0/fix"},
		{"crash on a finding of the change request", func(j *Judgement) { j.Findings[0].Crash = true }, "/findings/0/crash"},
		{"crash on a pre-existing MAJOR", func(j *Judgement) { j.PreExisting[0].Crash = true }, "/preExisting/0/crash"},
		{"crash on a pre-existing BLOCKER", func(j *Judgement) { j.PreExisting[0].Crash, j.PreExisting[0].Severity = true, Blocker }, ""},
		{"severity other than the rule's", func(j *Judgement) { j.Findings[0].Severity = Major }, "/findings/0/severity"},
		{"pre-existing BLOCKER of a MAJOR rule without a crash", func(j *Judgement) { j.PreExisting[0].Severity = Blocker }, "/preExisting/0/severity"},
		{"pre-existing MINOR of a MAJOR rule", func(j *Judgement) { j.PreExisting[0].Severity = Minor }, "/preExisting/0/severity"},
		{"any severity of a rule that needs judgement", func(j *Judgement) { j.Findings[0].Rule, j.Findings[0].Severity = "no-double-bang", Minor }, ""},
		{"signal confirmed with the severity of its rule", func(j *Judgement) {
			j.Signals = append(j.Signals, SignalDecision{ID: "s1", Decision: Confirm, Severity: Blocker})
		}, ""},
		{"signal confirmed with another severity", func(j *Judgement) {
			j.Signals = append(j.Signals, SignalDecision{ID: "s1", Decision: Confirm, Severity: Major})
		}, "/signals/2/severity"},
		{"finding that repeats a signal", func(j *Judgement) {
			j.Findings = append(j.Findings, JudgedFinding{Rule: "empty-catch", Severity: Blocker, Path: "app/A.kt", Side: Right, Line: 2, Title: "t", Why: "w", Fix: "f"})
		}, "/findings/1"},
		{"signal that needs judgement left undecided", func(j *Judgement) { j.Signals = j.Signals[1:] }, "/signals"},
		{"signal that needs judgement confirmed without severity", func(j *Judgement) { j.Signals[0].Severity = "" }, "/signals/0/severity"},
		{"signal that needs judgement dismissed", func(j *Judgement) {
			j.Signals[0] = SignalDecision{ID: "s2", Decision: Dismiss, Reason: "checked above"}
		}, ""},
		{"dismissal without a reason", func(j *Judgement) { j.Signals[0] = SignalDecision{ID: "s2", Decision: Dismiss, Reason: "  "} }, "/signals/0/reason"},
		{"dismissal with a crash", func(j *Judgement) {
			j.Signals[1] = SignalDecision{ID: "s3", Decision: Dismiss, Reason: "fine", Crash: true}
		}, "/signals/1/crash"},
		{"automatic signal dismissed with a reason", func(j *Judgement) {
			j.Signals = append(j.Signals, SignalDecision{ID: "s1", Decision: Dismiss, Reason: "the catch logs"})
		}, ""},
		{"crash on a signal of the change request", func(j *Judgement) { j.Signals[0].Severity, j.Signals[0].Crash = Blocker, true }, "/signals/0/crash"},
		{"unknown signal", func(j *Judgement) { j.Signals = append(j.Signals, SignalDecision{ID: "s4", Decision: Confirm}) }, "/signals/2/id"},
		{"signal decided twice", func(j *Judgement) {
			j.Signals = append(j.Signals, SignalDecision{ID: "s2", Decision: Confirm, Severity: Minor})
		}, "/signals/2/id"},
		{"unknown severity on a signal", func(j *Judgement) { j.Signals[0].Severity = "LOW" }, "/signals/0/severity"},
		{"four notes", func(j *Judgement) { j.Notes = []string{"a", "b", "c", "d"} }, "/notes"},
		{"note of 200 characters", func(j *Judgement) { j.Notes = []string{strings.Repeat("è", 200)} }, ""},
		{"note of 201 characters", func(j *Judgement) { j.Notes = []string{strings.Repeat("è", 201)} }, "/notes/0"},
		{"question without a line", func(j *Judgement) { j.Questions = []Question{{Text: "Why?"}} }, ""},
		{"question with a negative line", func(j *Judgement) { j.Questions[0].Line = -2 }, "/questions/0/line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := testJudgement()
			tt.change(&j)
			err := ValidateJudgement(testReview(t), j)
			if tt.want == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if got := pointers(err); len(got) != 1 || got[0] != tt.want {
				t.Errorf("pointers = %q, want [%q]; error: %v", got, tt.want, err)
			}
		})
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := ValidationError{{Pointer: "/findings/2/line", Message: "line 40 is past the end of the file (12 lines)"}, {Pointer: "/notes", Message: "4 notes, at most 3"}}
	want := "invalid judgement:\n/findings/2/line: line 40 is past the end of the file (12 lines)\n/notes: 4 notes, at most 3"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestDecodeJudgement(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "scenarios", "*", "judgements", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no scenario judgements: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		j, err := DecodeJudgement(data)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		back, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if err := json.Unmarshal(data, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(back, &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s does not survive decoding and encoding:\n%s", f, back)
		}
	}
	for _, bad := range []string{`{"schema":1,"message":"x"}`, `{"schema":1} {}`, `[`} {
		if _, err := DecodeJudgement([]byte(bad)); err == nil {
			t.Errorf("DecodeJudgement(%s) succeeded", bad)
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\r\nb\n\n", []string{"a", "b", ""}},
	}
	for _, tt := range tests {
		if got := SplitLines(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSignalSeverityIsNullWhenJudged(t *testing.T) {
	data, err := json.Marshal([]Signal{
		{ID: "s1", Rule: "empty-catch", Severity: Blocker, Path: "a.kt", Line: 1, Column: 1, Origin: OriginMR},
		{ID: "s2", Rule: "no-double-bang", Path: "a.kt", Line: 2, Column: 3, Origin: OriginExisting, NeedsJudgement: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"s1","rule":"empty-catch","severity":"BLOCKER","path":"a.kt","line":1,"column":1,"origin":"mr","needsJudgement":false},` +
		`{"id":"s2","rule":"no-double-bang","severity":null,"path":"a.kt","line":2,"column":3,"origin":"existing","needsJudgement":true}]`
	if string(data) != want {
		t.Errorf("got  %s\nwant %s", data, want)
	}
	var back []Signal
	if err := json.Unmarshal(data, &back); err != nil || back[1].Severity != "" || back[0].Severity != Blocker {
		t.Errorf("decoded %+v, %v", back, err)
	}
}

// TestValidateJudgementPromotesACrash checks the one change of severity a
// judgement may make to a rule that needs no judgement: a pre-existing
// crash is a BLOCKER, whatever the rule says.
func TestValidateJudgementPromotesACrash(t *testing.T) {
	r := testReview(t)
	r.Signals = append(r.Signals, Signal{ID: "s4", Rule: "dead-code", Severity: Major, Path: "app/A.kt", Line: 6, Column: 1, Origin: OriginExisting})
	tests := []struct {
		name     string
		decision SignalDecision
		want     []string
	}{
		{"crash promoted to BLOCKER", SignalDecision{ID: "s4", Decision: Confirm, Severity: Blocker, Crash: true}, nil},
		{"BLOCKER without a crash", SignalDecision{ID: "s4", Decision: Confirm, Severity: Blocker}, []string{"/signals/2/severity"}},
		{"crash on a line of the change request", SignalDecision{ID: "s1", Decision: Confirm, Severity: Blocker, Crash: true}, []string{"/signals/2/crash"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := testJudgement()
			j.Signals = append(j.Signals, tt.decision)
			var got []string
			if err := ValidateJudgement(r, j); err != nil {
				got = pointers(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pointers %q, want %q", got, tt.want)
			}
		})
	}
	j := testJudgement()
	j.PreExisting[0].Severity, j.PreExisting[0].Crash = Blocker, true
	if err := ValidateJudgement(r, j); err != nil {
		t.Errorf("pre-existing crash of a MAJOR rule promoted to BLOCKER: %v", err)
	}
}

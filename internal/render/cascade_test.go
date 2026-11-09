package render

import (
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
)

const sandboxKey = "github:github.com/acr-plugin-sandbox/"

// testCascade is a root with two reviewed related change requests, one
// skipped as already done, one line without a checkbox and, when
// incomplete, a related change request that was not found.
func testCascade(t *testing.T, incomplete bool) (cascade.Result, []Reviewed) {
	t.Helper()
	root, lib, kmp := mustKey(t, sandboxKey+"sample-app#7"), mustKey(t, sandboxKey+"core-lib#3"), mustKey(t, sandboxKey+"kmp-shared#2")
	res := cascade.Result{
		Entries: []core.Entry{
			{CR: root, Status: core.Finalized},
			{CR: lib, Depth: 1, Parent: &root, Status: core.Finalized},
			{CR: kmp, Depth: 2, Parent: &lib, Status: core.Finalized},
			{CR: mustKey(t, sandboxKey+"feature-lib#2"), Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: core.AlreadyDone},
		},
		Order:       []core.Key{kmp, lib, root},
		NotFollowed: []cascade.Line{{CR: root, Line: 9, Text: "- https://github.com/acr-plugin-sandbox/kmp-shared/pull/1"}},
	}
	if incomplete {
		res.Entries = append(res.Entries, core.Entry{CR: mustKey(t, sandboxKey+"core-lib#9999"), Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: core.NotFound})
		res.Unparsed = []cascade.Line{{CR: root, Line: 11, Text: "- [ ] the login change"}}
	}
	doc := func(k core.Key, findings, pre []core.Finding) core.Document {
		d := testDocument(t)
		d.CR, d.Findings, d.PreExisting = k, findings, pre
		return d
	}
	base := testDocument(t)
	minor := base.Findings[0]
	minor.Severity = core.Minor
	reviewed := []Reviewed{
		{Doc: doc(root, base.Findings[:1], nil), Report: "work/sample-app-7/report.txt"},
		{Doc: doc(lib, base.Findings, base.PreExisting), Report: "work/core-lib-3/report.txt"},
		{Doc: doc(kmp, []core.Finding{minor}, nil), Report: "work/kmp-shared-2/report.txt"},
	}
	if incomplete {
		reviewed[1].Doc.Findings, reviewed[1].Doc.PreExisting = base.Findings[:2], nil
	}
	return res, reviewed
}

func TestCascadeSummary(t *testing.T) {
	res, reviewed := testCascade(t, false)
	got, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	want := `Cascade — acr-plugin-sandbox/sample-app#7 · 3 reviewed · 1 skipped · 1 not followed

  1. acr-plugin-sandbox/kmp-shared#2   OK        0 blocker · 0 major · 1 minor (+0 existing)
  2. acr-plugin-sandbox/core-lib#3     BLOCKED   1 blocker · 1 major · 0 minor (+1 existing, 1 crash)
  3. acr-plugin-sandbox/sample-app#7   OK        0 blocker · 1 major · 0 minor (+0 existing)
Skipped:
  acr-plugin-sandbox/feature-lib#2   already done
Not followed: https://github.com/acr-plugin-sandbox/kmp-shared/pull/1 (no checkbox)
Overall: BLOCKED
`
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCascadeSummaryIncomplete(t *testing.T) {
	res, reviewed := testCascade(t, true)
	got, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\n  2. acr-plugin-sandbox/core-lib#3     OK        0 blocker · 1 major · 0 minor (+0 existing)\n",
		"\nSkipped:\n  acr-plugin-sandbox/feature-lib#2   already done\n  acr-plugin-sandbox/core-lib#9999   not found\n",
		"\nUnparsed: \"- [ ] the login change\"\n",
		"\nOverall: INCOMPLETE — not reviewed: acr-plugin-sandbox/core-lib#9999 (not found), acr-plugin-sandbox/sample-app#7 line 11 (unparsed)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
}

func TestCascadeSummaryOK(t *testing.T) {
	res, reviewed := testCascade(t, false)
	reviewed[1].Doc.Findings, reviewed[1].Doc.PreExisting = nil, nil
	got, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "\nOverall: OK\n") {
		t.Errorf("got\n%s", got)
	}
}

func TestCascadeSummaryNeedsEveryReview(t *testing.T) {
	res, reviewed := testCascade(t, false)
	if _, err := Cascade(res, reviewed[1:]); err == nil || !strings.Contains(err.Error(), "sample-app#7") {
		t.Errorf("error %v, want one that names the change request without findings", err)
	}
}

func TestSkipReasonsAreAllWorded(t *testing.T) {
	for _, r := range []core.SkipReason{core.AlreadyDone, core.NotAChangeRequest, core.NotFound, core.OtherPlatform,
		core.OverDepthLimit, core.OverCountLimit, core.Cycle, core.SelfReference} {
		if skipWords[r] == "" {
			t.Errorf("no words for %s", r)
		}
	}
}

func TestFinal(t *testing.T) {
	res, reviewed := testCascade(t, false)
	got, err := Final(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	want := `Review completed. Nothing was written to GitHub.

Reports:

- acr-plugin-sandbox/kmp-shared#2 — work/kmp-shared-2/report.txt
- acr-plugin-sandbox/core-lib#3 — work/core-lib-3/report.txt
- acr-plugin-sandbox/sample-app#7 — work/sample-app-7/report.txt

` + summary
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestFinalNamesThePlatform(t *testing.T) {
	res, reviewed := testCascade(t, false)
	gl := mustKey(t, "gitlab:gitlab.com/group/app#7")
	res.Entries[0].CR, res.Order[2], reviewed[0].Doc.CR = gl, gl, gl
	got, err := Final(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "Review completed. Nothing was written to GitLab.\n") {
		t.Errorf("got\n%s", got)
	}
}

// TestCascadeSummaryNamesTheHostOfOtherPlatforms keeps a change request
// on another platform apart from one of the same project and number on the
// platform of the root.
func TestCascadeSummaryNamesTheHostOfOtherPlatforms(t *testing.T) {
	res, reviewed := testCascade(t, false)
	reviewed[1].Doc.Findings, reviewed[1].Doc.PreExisting = nil, nil
	root := res.Entries[0].CR
	res.Entries = append(res.Entries,
		core.Entry{CR: mustKey(t, sandboxKey+"sample-app#1"), Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: core.NotAChangeRequest},
		core.Entry{CR: mustKey(t, "gitlab:gitlab.com/acr-plugin-sandbox/sample-app#1"), Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: core.OtherPlatform},
	)
	got, err := Cascade(res, reviewed)
	if err != nil {
		t.Fatal(err)
	}
	want := `Skipped:
  acr-plugin-sandbox/feature-lib#2             already done
  acr-plugin-sandbox/sample-app#1              not a change request
  gitlab.com/acr-plugin-sandbox/sample-app#1   other platform
`
	if !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(got, "\nOverall: INCOMPLETE — not reviewed: gitlab.com/acr-plugin-sandbox/sample-app#1 (other platform)\n") {
		t.Errorf("overall:\n%s", got)
	}
}

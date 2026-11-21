package productflow

import (
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

func TestCollectS2Cascade(t *testing.T) {
	f := Collect(t, "S2", "sample-app", 7)
	var got []string
	for _, r := range f.CRs {
		got = append(got, r.CR.Project[len("acr-plugin-sandbox/"):])
	}
	if want := []string{"feature-lib", "kmp-shared", "core-lib", "sample-app", "sample-app"}; len(got) != len(want) {
		t.Fatalf("change requests %v, want %v", got, want)
	}
	last := f.CRs[len(f.CRs)-1]
	if last.CR.Number != 7 || last.WorkPackage.Title == "" || len(last.Review.Signals) == 0 || last.Judgement.CR != last.CR.String() {
		t.Errorf("the root: %s, %q, %d signals, judgement of %s", last.CR, last.WorkPackage.Title, len(last.Review.Signals), last.Judgement.CR)
	}
	if r := f.Of(t, "core-lib", 3); r.Finalize(t, nil).Verdict != core.Blocked {
		t.Error("core-lib#3 is not BLOCKED with its scenario judgement")
	}
}

func TestFinalizeEditsACopy(t *testing.T) {
	r := Collect(t, "S1", "sample-app", 2).Of(t, "sample-app", 2)
	before := len(r.Judgement.Findings)
	res := r.Finalize(t, func(j *core.Judgement) {
		j.Findings = append(j.Findings[:0:0], j.Findings[:1]...)
		j.Signals[0].Decision, j.Signals[0].Reason = core.Dismiss, "edited"
	})
	if len(r.Judgement.Findings) != before || r.Judgement.Signals[0].Decision != core.Confirm {
		t.Errorf("the judgement of the flow was edited: %d findings, signal s1 %s", len(r.Judgement.Findings), r.Judgement.Signals[0].Decision)
	}
	if len(res.Dismissed) != 3 {
		t.Errorf("%d dismissed signals, want the 2 of the judgement and the edited one", len(res.Dismissed))
	}
	doc := r.Document(t, nil, func(h *core.Header) { h.CI = core.CI{State: core.CIFailing} })
	if doc.CI.State != core.CIFailing || doc.Title != r.WorkPackage.Title {
		t.Errorf("header: CI %s, title %q", doc.CI.State, doc.Title)
	}
}

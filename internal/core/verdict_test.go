package core

import "testing"

func TestChangeRequestVerdict(t *testing.T) {
	f := func(s Severity, crash bool) Finding { return Finding{Severity: s, Crash: crash} }
	tests := []struct {
		name        string
		findings    []Finding
		preExisting []Finding
		want        Verdict
	}{
		{"nothing", nil, nil, OK},
		{"a BLOCKER of the change request", []Finding{f(Minor, false), f(Blocker, false)}, nil, Blocked},
		{"MAJOR, MINOR and INFO never block", []Finding{f(Major, false), f(Minor, false), f(Info, false)}, nil, OK},
		{"a pre-existing crash", nil, []Finding{f(Blocker, true)}, Blocked},
		{"a pre-existing BLOCKER that is not a crash", nil, []Finding{f(Blocker, false)}, OK},
		{"pre-existing MAJOR", []Finding{f(Major, false)}, []Finding{f(Major, false)}, OK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChangeRequestVerdict(tt.findings, tt.preExisting); got != tt.want {
				t.Errorf("verdict %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFinalizeSetsTheVerdict(t *testing.T) {
	res, err := Finalize(testReview(t), testJudgement())
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != Blocked {
		t.Errorf("verdict %s, want BLOCKED: s1 is an automatic BLOCKER", res.Verdict)
	}
	j := testJudgement()
	j.Findings = nil
	j.Signals = []SignalDecision{
		{ID: "s1", Decision: Dismiss, Reason: "logged by the wrapper"},
		{ID: "s2", Decision: Confirm, Severity: Major},
		{ID: "s3", Decision: Confirm, Severity: Major},
	}
	if res, err = Finalize(testReview(t), j); err != nil || res.Verdict != OK {
		t.Errorf("verdict %s, %v, want OK with only MAJOR findings", res.Verdict, err)
	}
}

func TestVerdictValid(t *testing.T) {
	for _, v := range []Verdict{Blocked, OK, Incomplete} {
		if !v.Valid() {
			t.Errorf("%s is not valid", v)
		}
	}
	if Verdict("APPROVED").Valid() {
		t.Error("APPROVED is a verdict: the plugin never approves")
	}
}

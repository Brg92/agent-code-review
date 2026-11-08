package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustKey(t *testing.T, s string) Key {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestDefaultLimits(t *testing.T) {
	if got, want := DefaultLimits(), (Limits{MaxDepth: 3, MaxChangeRequests: 10}); got != want {
		t.Errorf("DefaultLimits() = %+v, want %+v", got, want)
	}
}

func TestLimitsValidate(t *testing.T) {
	tests := []struct {
		limits  Limits
		wantErr string
	}{
		{Limits{MaxDepth: 0, MaxChangeRequests: 1}, ""},
		{DefaultLimits(), ""},
		{Limits{MaxDepth: -1, MaxChangeRequests: 10}, "maxDepth"},
		{Limits{MaxDepth: 3, MaxChangeRequests: 0}, "maxChangeRequests"},
	}
	for _, tt := range tests {
		err := tt.limits.Validate()
		if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%+v.Validate() = %v, want error containing %q", tt.limits, err, tt.wantErr)
		}
	}
}

func TestStatusOrder(t *testing.T) {
	order := []Status{Pending, Collected, Judged, Finalized}
	for i, s := range order {
		if !s.Valid() {
			t.Errorf("%q is not valid", s)
		}
		for j, other := range order {
			if got := s.AtLeast(other); got != (i >= j) {
				t.Errorf("%q.AtLeast(%q) = %v", s, other, got)
			}
		}
		if s.AtLeast(Skipped) || Skipped.AtLeast(s) {
			t.Errorf("skipped is ordered against %q", s)
		}
	}
	if !Skipped.Valid() || Status("approved").Valid() || Status("").Valid() {
		t.Error("wrong status validity")
	}
}

func TestSkipReasons(t *testing.T) {
	valid := []SkipReason{AlreadyDone, NotAChangeRequest, NotFound, OtherPlatform, OverDepthLimit, OverCountLimit, Cycle, SelfReference}
	want := []string{"already-done", "not-a-change-request", "not-found", "other-platform", "over-depth-limit", "over-count-limit", "cycle", "self-reference"}
	for i, r := range valid {
		if string(r) != want[i] || !r.Valid() {
			t.Errorf("skip reason %q (valid %v), want %q", r, r.Valid(), want[i])
		}
	}
	for _, r := range []SkipReason{"over-limit", "ticked", ""} {
		if r.Valid() {
			t.Errorf("%q is valid", r)
		}
	}
}

func TestEnumJSON(t *testing.T) {
	var s Status
	if err := json.Unmarshal([]byte(`"judged"`), &s); err != nil || s != Judged {
		t.Errorf("status = %q, %v", s, err)
	}
	if err := json.Unmarshal([]byte(`"approved"`), &s); err == nil {
		t.Error("unknown status accepted")
	}
	var r SkipReason
	if err := json.Unmarshal([]byte(`"cycle"`), &r); err != nil || r != Cycle {
		t.Errorf("skip reason = %q, %v", r, err)
	}
	if err := json.Unmarshal([]byte(`"over-limit"`), &r); err == nil {
		t.Error("unknown skip reason accepted")
	}
	if _, err := json.Marshal(Status("approved")); err == nil {
		t.Error("marshal accepted an unknown status")
	}
	if _, err := json.Marshal(SkipReason("ticked")); err == nil {
		t.Error("marshal accepted an unknown skip reason")
	}
}

func TestEntryValidate(t *testing.T) {
	root := mustKey(t, "github:github.com/acr-plugin-sandbox/sample-app#7")
	child := mustKey(t, "github:github.com/acr-plugin-sandbox/core-lib#3")
	sha := strings.Repeat("ab", 20)
	tests := []struct {
		name    string
		entry   Entry
		wantErr string
	}{
		{"root pending", Entry{CR: root, Status: Pending}, ""},
		{"child finalized", Entry{CR: child, Depth: 1, Parent: &root, Status: Finalized, HeadSHA: sha, Title: "t"}, ""},
		{"skipped", Entry{CR: child, Depth: 1, Parent: &root, Status: Skipped, SkipReason: Cycle}, ""},
		{"zero key", Entry{Status: Pending}, "key"},
		{"negative depth", Entry{CR: child, Depth: -1, Status: Pending}, "depth"},
		{"unknown status", Entry{CR: root, Status: "approved"}, "status"},
		{"missing status", Entry{CR: root}, "status"},
		{"skipped without reason", Entry{CR: child, Depth: 1, Status: Skipped}, "skipReason"},
		{"reason without skip", Entry{CR: child, Depth: 1, Status: Pending, SkipReason: Cycle}, "skipReason"},
		{"unknown reason", Entry{CR: child, Depth: 1, Status: Skipped, SkipReason: "ticked"}, "skipReason"},
		{"short sha", Entry{CR: root, Status: Collected, HeadSHA: "abc"}, "headSha"},
		{"uppercase sha", Entry{CR: root, Status: Collected, HeadSHA: strings.ToUpper(sha)}, "headSha"},
		{"invalid parent", Entry{CR: child, Depth: 1, Parent: &Key{}, Status: Pending}, "parent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.entry.Validate()
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestEntryJSON(t *testing.T) {
	root := mustKey(t, "github:github.com/acr-plugin-sandbox/sample-app#7")
	entries := []Entry{
		{CR: root, Status: Pending},
		{CR: mustKey(t, "github:github.com/acr-plugin-sandbox/core-lib#3"), Depth: 1, Parent: &root, Status: Skipped, SkipReason: AlreadyDone},
	}
	out, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"cr":"github:github.com/acr-plugin-sandbox/sample-app#7","depth":0,"status":"pending"},` +
		`{"cr":"github:github.com/acr-plugin-sandbox/core-lib#3","depth":1,"parent":"github:github.com/acr-plugin-sandbox/sample-app#7","status":"skipped","skipReason":"already-done"}]`
	if string(out) != want {
		t.Errorf("marshal =\n%s\nwant\n%s", out, want)
	}
}

package state

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
)

var phases = []Phase{Resolved, Collected, Judged, Finalized, Planned, Published}

func TestPhaseNames(t *testing.T) {
	want := []string{"resolved", "collected", "judged", "finalized", "planned", "published"}
	for i, p := range phases {
		if string(p) != want[i] || !p.Valid() {
			t.Errorf("phase %q (valid %v), want %q", p, p.Valid(), want[i])
		}
	}
	for _, p := range []Phase{"approved", "", "Resolved"} {
		if p.Valid() {
			t.Errorf("%q is valid", p)
		}
	}
}

func TestPhaseJSON(t *testing.T) {
	var p Phase
	if err := json.Unmarshal([]byte(`"planned"`), &p); err != nil || p != Planned {
		t.Errorf("phase = %q, %v", p, err)
	}
	if err := json.Unmarshal([]byte(`"approved"`), &p); err == nil {
		t.Error("unknown phase accepted")
	}
	if _, err := json.Marshal(Phase("approved")); err == nil {
		t.Error("marshal accepted an unknown phase")
	}
}

func TestCheckTransition(t *testing.T) {
	allowed := map[[2]Phase]bool{
		{Resolved, Collected}: true,
		{Collected, Judged}:   true,
		{Judged, Finalized}:   true,
		{Finalized, Planned}:  true,
		{Planned, Planned}:    true,
		{Planned, Published}:  true,
	}
	all := append(phases, "approved")
	for _, from := range all {
		for _, to := range all {
			err := CheckTransition(from, to)
			if allowed[[2]Phase{from, to}] {
				if err != nil {
					t.Errorf("%s -> %s: %v", from, to, err)
				}
				continue
			}
			var te *TransitionError
			if !errors.As(err, &te) {
				t.Errorf("%s -> %s: error %v is not a *TransitionError", from, to, err)
				continue
			}
			if te.From != from || te.To != to {
				t.Errorf("%s -> %s: error has from %q to %q", from, to, te.From, te.To)
			}
		}
	}
}

func TestTransitionErrorMessage(t *testing.T) {
	err := CheckTransition(Published, Resolved)
	if got, want := err.Error(), "phase transition published -> resolved is not allowed"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

var (
	t0 = time.Date(2026, 11, 2, 10, 15, 0, 0, time.UTC)
	t1 = t0.Add(42*time.Minute + 7*time.Second)
)

func key(t *testing.T, s string) core.Key {
	t.Helper()
	k, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// cascade returns a root, a child and a skipped child, all pending except
// the skipped one.
func cascade(t *testing.T) []core.Entry {
	root := key(t, "github:github.com/acr-plugin-sandbox/sample-app#7")
	return []core.Entry{
		{CR: root, Status: core.Pending},
		{CR: key(t, "github:github.com/acr-plugin-sandbox/core-lib#3"), Depth: 1, Parent: &root, Status: core.Pending},
		{CR: key(t, "github:github.com/acr-plugin-sandbox/feature-lib#2"), Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: core.AlreadyDone},
	}
}

func newState(t *testing.T) *State {
	t.Helper()
	entries := cascade(t)
	s, err := New("20261102T101500Z-0a1b2c3d", entries[0].CR, core.DefaultLimits(), entries, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNew(t *testing.T) {
	entries := cascade(t)
	s, err := New("20261102T101500Z-0a1b2c3d", entries[0].CR, core.DefaultLimits(), entries, t0.Add(500*time.Millisecond).In(time.FixedZone("CET", 3600)))
	if err != nil {
		t.Fatal(err)
	}
	if s.Phase != Resolved || s.Forge != core.GitHub || s.Root != entries[0].CR || s.Cascade != core.DefaultLimits() {
		t.Errorf("New() = %+v", s)
	}
	if !s.CreatedAt.Equal(t0) || !s.UpdatedAt.Equal(t0) || s.CreatedAt.Location() != time.UTC {
		t.Errorf("timestamps %v %v, want %v in UTC", s.CreatedAt, s.UpdatedAt, t0)
	}
	entries[0].Status = core.Finalized
	if s.ChangeRequests[0].Status != core.Pending {
		t.Error("New() keeps the caller's slice")
	}
	if _, err := New("20261102T101500Z-0a1b2c3d", entries[0].CR, core.Limits{}, cascade(t), t0); err == nil {
		t.Error("New() accepted zero limits")
	}
}

func TestAdvance(t *testing.T) {
	s := newState(t)
	set := func(status core.Status) {
		for i := range s.ChangeRequests {
			if s.ChangeRequests[i].Status != core.Skipped {
				s.ChangeRequests[i].Status = status
			}
		}
	}
	steps := []struct {
		status core.Status
		to     Phase
	}{
		{core.Collected, Collected},
		{core.Judged, Judged},
		{core.Finalized, Finalized},
		{core.Finalized, Planned},
		{core.Finalized, Planned},
		{core.Finalized, Published},
	}
	for i, step := range steps {
		set(step.status)
		now := t0.Add(time.Duration(i+1) * time.Minute)
		if err := s.Advance(step.to, now); err != nil {
			t.Fatalf("Advance(%s): %v", step.to, err)
		}
		if s.Phase != step.to || !s.UpdatedAt.Equal(now) || !s.CreatedAt.Equal(t0) {
			t.Fatalf("after Advance(%s): phase %s, updated %v", step.to, s.Phase, s.UpdatedAt)
		}
	}
	err := s.Advance(Resolved, t1)
	var te *TransitionError
	if !errors.As(err, &te) || s.Phase != Published {
		t.Errorf("Advance after published = %v, phase %s", err, s.Phase)
	}
}

func TestAdvanceChecksStatuses(t *testing.T) {
	tests := []struct {
		name     string
		statuses []core.Status // of the two non-skipped entries
		from, to Phase
	}{
		{"collected with a pending entry", []core.Status{core.Collected, core.Pending}, Resolved, Collected},
		{"judged with a collected entry", []core.Status{core.Judged, core.Collected}, Collected, Judged},
		{"finalized with a judged entry", []core.Status{core.Judged, core.Finalized}, Judged, Finalized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newState(t)
			s.Phase = tt.from
			s.ChangeRequests[0].Status = tt.statuses[0]
			s.ChangeRequests[1].Status = tt.statuses[1]
			err := s.Advance(tt.to, t1)
			var ce *ConsistencyError
			if !errors.As(err, &ce) {
				t.Fatalf("Advance(%s) = %v, want a *ConsistencyError", tt.to, err)
			}
			if s.Phase != tt.from || !s.UpdatedAt.Equal(t0) {
				t.Errorf("failed Advance changed the state: phase %s, updated %v", s.Phase, s.UpdatedAt)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(*State)
		wantErr string
	}{
		{"valid", func(*State) {}, ""},
		{"later statuses at resolved", func(s *State) { s.ChangeRequests[0].Status = core.Finalized }, ""},
		{"unknown forge", func(s *State) { s.Forge = "bitbucket" }, "forge"},
		{"root on another forge", func(s *State) { s.Forge = core.GitLab }, "forge"},
		{"missing root", func(s *State) { s.Root = core.Key{} }, "root"},
		{"unknown phase", func(s *State) { s.Phase = "approved" }, "phase"},
		{"bad limits", func(s *State) { s.Cascade.MaxChangeRequests = 0 }, "maxChangeRequests"},
		{"missing createdAt", func(s *State) { s.CreatedAt = time.Time{} }, "createdAt"},
		{"missing updatedAt", func(s *State) { s.UpdatedAt = time.Time{} }, "updatedAt"},
		{"skipped without reason", func(s *State) { s.ChangeRequests[2].SkipReason = "" }, "skipReason"},
		{"reason on a pending entry", func(s *State) { s.ChangeRequests[1].SkipReason = core.Cycle }, "skipReason"},
		{"pending entry at planned", func(s *State) {
			s.Phase = Planned
			s.ChangeRequests[0].Status = core.Finalized
		}, "pending"},
		{"judged entry at published", func(s *State) {
			s.Phase = Published
			s.ChangeRequests[0].Status = core.Finalized
			s.ChangeRequests[1].Status = core.Judged
		}, "judged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newState(t)
			tt.edit(s)
			err := s.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() = %v", err)
				}
				return
			}
			var ce *ConsistencyError
			if !errors.As(err, &ce) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %v, want a *ConsistencyError containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestConsistencyErrorNamesTheEntry(t *testing.T) {
	s := newState(t)
	s.ChangeRequests[1].Depth = -1
	err := s.Validate()
	want := "inconsistent state: change request 1 (github:github.com/acr-plugin-sandbox/core-lib#3): depth -1 is negative"
	if err == nil || err.Error() != want {
		t.Errorf("Validate() = %v, want %q", err, want)
	}
}

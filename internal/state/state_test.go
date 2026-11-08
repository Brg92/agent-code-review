package state

import (
	"encoding/json"
	"errors"
	"testing"
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

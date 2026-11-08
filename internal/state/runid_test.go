package state

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

var generatedRunID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{8}$`)

func TestNewRunID(t *testing.T) {
	now := time.Date(2026, 11, 2, 11, 15, 0, 999, time.FixedZone("CET", 3600))
	got, err := NewRunID(now, bytes.NewReader([]byte{0x0a, 0x1b, 0xc2, 0xff, 0x99}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "20261102T101500Z-0a1bc2ff"; got != want {
		t.Errorf("NewRunID() = %q, want %q", got, want)
	}
	if !ValidRunID(got) {
		t.Errorf("generated %q is not a valid run id", got)
	}
}

func TestNewRunIDReadError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := NewRunID(t0, iotest.ErrReader(boom)); !errors.Is(err, boom) {
		t.Errorf("NewRunID() error = %v, want %v", err, boom)
	}
	if _, err := NewRunID(t0, bytes.NewReader([]byte{1, 2})); err == nil {
		t.Error("NewRunID() accepted 2 random bytes")
	}
}

func TestRandomRunID(t *testing.T) {
	a, err := RandomRunID(t0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomRunID(t0)
	if err != nil {
		t.Fatal(err)
	}
	if !generatedRunID.MatchString(a) || !strings.HasPrefix(a, "20261102T101500Z-") {
		t.Errorf("RandomRunID() = %q", a)
	}
	if a == b {
		t.Errorf("two random run ids are both %q", a)
	}
}

func TestValidRunID(t *testing.T) {
	for id, want := range map[string]bool{
		"20261108T101500Z-0a1b2c3d": true,
		"20261102-101500-s2cascade": true,
		"abcdefgh":                  true,
		"abcdefg":                   false,
		strings.Repeat("a", 64):     true,
		strings.Repeat("a", 65):     false,
		"run_with_underscore":       false,
		"../escape":                 false,
		"":                          false,
	} {
		if got := ValidRunID(id); got != want {
			t.Errorf("ValidRunID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestValidateRunID(t *testing.T) {
	s := newState(t)
	s.RunID = "../escape"
	var ce *ConsistencyError
	if err := s.Validate(); !errors.As(err, &ce) || !strings.Contains(err.Error(), "run id") {
		t.Errorf("Validate() = %v, want a *ConsistencyError about the run id", err)
	}
}

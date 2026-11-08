package state

import (
	"errors"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

// treeState returns a state whose change requests are in discovery order
// (depth first):
//
//	0 sample-app#7                 root
//	1   core-lib#3                 reviewed
//	2     kmp-shared#2             reviewed
//	3     sample-app#7             cycle
//	4   feature-lib#2              already-done
//	5   sample-app#7               self-reference
//	6   core-lib#9999              not-found
//	7   gitlab.com sample-app#1    other-platform
func treeState(t *testing.T) *State {
	t.Helper()
	k := func(s string) core.Key { return key(t, s) }
	root := k("github:github.com/acr-plugin-sandbox/sample-app#7")
	core3 := k("github:github.com/acr-plugin-sandbox/core-lib#3")
	skip := func(cr core.Key, depth int, parent core.Key, reason core.SkipReason) core.Entry {
		return core.Entry{CR: cr, Depth: depth, Parent: &parent, Status: core.Skipped, SkipReason: reason}
	}
	entries := []core.Entry{
		{CR: root, Status: core.Pending},
		{CR: core3, Depth: 1, Parent: &root, Status: core.Pending},
		{CR: k("github:github.com/acr-plugin-sandbox/kmp-shared#2"), Depth: 2, Parent: &core3, Status: core.Pending},
		skip(root, 2, core3, core.Cycle),
		skip(k("github:github.com/acr-plugin-sandbox/feature-lib#2"), 1, root, core.AlreadyDone),
		skip(root, 1, root, core.SelfReference),
		skip(k("github:github.com/acr-plugin-sandbox/core-lib#9999"), 1, root, core.NotFound),
		skip(k("gitlab:gitlab.com/acr-plugin-sandbox/sample-app#1"), 1, root, core.OtherPlatform),
	}
	s, err := New("20261102T101500Z-0a1b2c3d", root, core.DefaultLimits(), entries, t0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestValidateCascadeStructure(t *testing.T) {
	other := func(t *testing.T, s string) *core.Key { k := key(t, s); return &k }
	tests := []struct {
		name    string
		edit    func(*testing.T, *State)
		wantErr string
	}{
		{"valid tree", func(*testing.T, *State) {}, ""},
		{"no change requests", func(_ *testing.T, s *State) { s.ChangeRequests = nil }, "root"},
		{"first is not the root", func(_ *testing.T, s *State) {
			s.ChangeRequests[0], s.ChangeRequests[4] = s.ChangeRequests[4], s.ChangeRequests[0]
		}, "first change request must be the root"},
		{"root skipped", func(_ *testing.T, s *State) {
			s.ChangeRequests[0].Status, s.ChangeRequests[0].SkipReason = core.Skipped, core.Cycle
		}, "root"},
		{"root with a parent", func(t *testing.T, s *State) {
			s.ChangeRequests[0].Parent = other(t, "github:github.com/acr-plugin-sandbox/core-lib#3")
		}, "root"},
		{"root with depth", func(_ *testing.T, s *State) { s.ChangeRequests[0].Depth = 1 }, "root"},
		{"entry without a parent", func(_ *testing.T, s *State) { s.ChangeRequests[6].Parent = nil }, "no parent"},
		{"depth zero below the root", func(_ *testing.T, s *State) { s.ChangeRequests[6].Depth = 0 }, "depth"},
		{"depth is not parent depth plus one", func(_ *testing.T, s *State) { s.ChangeRequests[2].Depth = 3 }, "depth"},
		{"parent is unknown", func(t *testing.T, s *State) {
			s.ChangeRequests[6].Parent = other(t, "github:github.com/acr-plugin-sandbox/ghost#1")
		}, "parent"},
		{"parent is skipped", func(t *testing.T, s *State) {
			s.ChangeRequests[7].Parent = other(t, "github:github.com/acr-plugin-sandbox/feature-lib#2")
			s.ChangeRequests[7].Depth = 2
		}, "parent"},
		{"parent comes later", func(_ *testing.T, s *State) {
			s.ChangeRequests[1], s.ChangeRequests[2] = s.ChangeRequests[2], s.ChangeRequests[1]
		}, "parent"},
		{"not depth first", func(_ *testing.T, s *State) {
			// feature-lib#2 (depth 1) closes core-lib#3; a later child of
			// core-lib#3 is out of order.
			s.ChangeRequests[3], s.ChangeRequests[4] = s.ChangeRequests[4], s.ChangeRequests[3]
		}, "parent"},
		{"reviewed twice", func(_ *testing.T, s *State) {
			s.ChangeRequests[3].Status, s.ChangeRequests[3].SkipReason = core.Pending, ""
		}, "twice"},
		{"reviewed twice with other case", func(t *testing.T, s *State) {
			s.ChangeRequests[3].CR = key(t, "github:github.com/ACR-plugin-sandbox/Sample-App#7")
			s.ChangeRequests[3].Status, s.ChangeRequests[3].SkipReason = core.Pending, ""
		}, "twice"},
		{"cycle without a reviewed entry", func(t *testing.T, s *State) {
			s.ChangeRequests[3].CR = key(t, "github:github.com/acr-plugin-sandbox/ghost#1")
		}, "cycle"},
		{"self-reference to another change request", func(t *testing.T, s *State) {
			s.ChangeRequests[5].CR = key(t, "github:github.com/acr-plugin-sandbox/core-lib#3")
		}, "self-reference"},
		{"reviewed on another forge", func(t *testing.T, s *State) {
			s.ChangeRequests[2].CR = key(t, "gitlab:gitlab.com/acr-plugin-sandbox/kmp-shared#2")
		}, "platform"},
		{"reviewed on another host", func(t *testing.T, s *State) {
			s.ChangeRequests[2].CR = key(t, "github:github.example.com/acr-plugin-sandbox/kmp-shared#2")
		}, "platform"},
		{"other-platform on the same platform", func(t *testing.T, s *State) {
			s.ChangeRequests[7].CR = key(t, "github:github.com/acr-plugin-sandbox/sample-app#1")
		}, "other-platform"},
		{"over-count-limit beyond maxDepth", func(_ *testing.T, s *State) {
			s.Cascade = core.Limits{MaxDepth: 1, MaxChangeRequests: 2}
			s.ChangeRequests[2].Status, s.ChangeRequests[2].SkipReason = core.Skipped, core.OverCountLimit
		}, "over-depth-limit"},
		{"reviewed deeper than maxDepth", func(_ *testing.T, s *State) { s.Cascade.MaxDepth = 1 }, "maxDepth"},
		{"over-depth-limit within maxDepth", func(_ *testing.T, s *State) {
			s.ChangeRequests[6].SkipReason = core.OverDepthLimit
		}, "over-depth-limit"},
		{"reviewed more than maxChangeRequests", func(_ *testing.T, s *State) { s.Cascade.MaxChangeRequests = 2 }, "maxChangeRequests"},
		{"over-count-limit below the limit", func(_ *testing.T, s *State) {
			s.ChangeRequests[6].SkipReason = core.OverCountLimit
		}, "over-count-limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := treeState(t)
			tt.edit(t, s)
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

func TestValidateLimitsReachedExactly(t *testing.T) {
	s := treeState(t)
	// Three reviewed change requests reach maxChangeRequests 3, so the next
	// reviewable one is over-count-limit.
	s.Cascade = core.Limits{MaxDepth: 2, MaxChangeRequests: 3}
	s.ChangeRequests[6].SkipReason = core.OverCountLimit
	if err := s.Validate(); err != nil {
		t.Errorf("over-count-limit after 3 reviewed: %v", err)
	}
	s.Cascade = core.Limits{MaxDepth: 1, MaxChangeRequests: 3}
	s.ChangeRequests[2].Status, s.ChangeRequests[2].SkipReason = core.Skipped, core.OverDepthLimit
	s.ChangeRequests[6].SkipReason = core.NotFound
	if err := s.Validate(); err != nil {
		t.Errorf("over-depth-limit at maxDepth+1: %v", err)
	}
}

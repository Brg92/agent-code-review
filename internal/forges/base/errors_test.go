package base

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestErrorKinds(t *testing.T) {
	cause := errors.New("connection reset")
	tests := []struct {
		name       string
		err        error
		kind       Kind
		reason     string
		message    string
		permission []string
	}{
		{"bare kind", ErrNotFound, ErrNotFound, "not-found", "not found or no access", nil},
		{"forbidden with permissions",
			&Error{Kind: ErrForbidden, Op: "tree acme/app@main", Permissions: []string{"contents: read", "metadata: read"}},
			ErrForbidden, "forbidden", "tree acme/app@main: forbidden (needs contents: read, metadata: read)",
			[]string{"contents: read", "metadata: read"}},
		{"search unavailable", &Error{Kind: ErrSearchUnavailable, Op: "search acme/app@dev"},
			ErrSearchUnavailable, "search-unavailable", "search acme/app@dev: search unavailable for this ref", nil},
		{"tree truncated", &Error{Kind: ErrTreeTruncated, Op: "tree acme/app@main"},
			ErrTreeTruncated, "tree-truncated", "tree acme/app@main: tree truncated", nil},
		{"list truncated", &Error{Kind: ErrListTruncated, Op: "files acme/app#1"},
			ErrListTruncated, "list-truncated", "files acme/app#1: list truncated", nil},
		{"rate limited", &Error{Kind: ErrRateLimited, Op: "ci acme/app@abc"},
			ErrRateLimited, "rate-limited", "ci acme/app@abc: rate limited", nil},
		{"transport with cause", &Error{Kind: ErrTransport, Op: "notes acme/app#1", Err: cause},
			ErrTransport, "transport", "notes acme/app#1: transport error: connection reset", nil},
		{"rejected", &Error{Kind: ErrRejected, Op: "review acme/app#1"},
			ErrRejected, "rejected", "review acme/app#1: rejected by the platform", nil},
		{"wrapped", fmt.Errorf("criteria: %w", &Error{Kind: ErrNotFound, Op: "files acme/app@main"}),
			ErrNotFound, "not-found", "criteria: files acme/app@main: not found or no access", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, tt.kind) {
				t.Errorf("errors.Is(%v, %v) = false", tt.err, tt.kind)
			}
			for _, other := range Kinds {
				if other != tt.kind && errors.Is(tt.err, other) {
					t.Errorf("errors.Is(%v, %v) = true, want only %v", tt.err, other, tt.kind)
				}
			}
			if got := ReasonOf(tt.err); got != tt.reason {
				t.Errorf("ReasonOf = %q, want %q", got, tt.reason)
			}
			if got := tt.err.Error(); got != tt.message {
				t.Errorf("Error() = %q, want %q", got, tt.message)
			}
			if got := PermissionsOf(tt.err); !reflect.DeepEqual(got, tt.permission) {
				t.Errorf("PermissionsOf = %v, want %v", got, tt.permission)
			}
		})
	}
}

func TestErrorUnwrapsItsCause(t *testing.T) {
	cause := errors.New("connection reset")
	err := error(&Error{Kind: ErrTransport, Op: "tree", Err: cause})
	if !errors.Is(err, cause) {
		t.Error("errors.Is(err, cause) = false: the cause must stay reachable")
	}
}

func TestReasonOfOtherErrors(t *testing.T) {
	for _, err := range []error{nil, errors.New("boom"), Kind("made-up")} {
		if got := ReasonOf(err); got != "" {
			t.Errorf("ReasonOf(%v) = %q, want empty: not a read error", err, got)
		}
	}
	if got := Kind("made-up").Error(); got != "made-up" {
		t.Errorf("Error() of an unknown kind = %q, want its name", got)
	}
}

func TestRejectedOf(t *testing.T) {
	named := &Error{Kind: ErrRejected, Op: "review acme/app#1", Rejected: []int{2, 0}}
	if got, ok := RejectedOf(fmt.Errorf("publish: %w", named)); !ok || !reflect.DeepEqual(got, []int{2, 0}) {
		t.Errorf("RejectedOf(named) = %v, %v, want [2 0], true", got, ok)
	}
	if got, ok := RejectedOf(&Error{Kind: ErrRejected}); !ok || got != nil {
		t.Errorf("RejectedOf(unnamed) = %v, %v, want nil, true: the platform did not say which", got, ok)
	}
	if got, ok := RejectedOf(&Error{Kind: ErrNotFound, Rejected: []int{1}}); ok || got != nil {
		t.Errorf("RejectedOf(not found) = %v, %v, want nothing", got, ok)
	}
	if got, ok := RejectedOf(nil); ok || got != nil {
		t.Errorf("RejectedOf(nil) = %v, %v", got, ok)
	}
}

package base

import (
	"strings"
	"testing"
)

func TestSearchOn(t *testing.T) {
	tests := []struct {
		scope SearchScope
		ref   string
		want  bool
	}{
		{SearchAnyRef, "main", true},
		{SearchAnyRef, "release/2.0", true},
		{SearchDefaultBranch, "main", true},
		{SearchDefaultBranch, "release/2.0", false},
		{SearchDefaultBranch, "", false},
		{SearchNone, "main", false},
		{SearchNone, "release/2.0", false},
	}
	for _, tt := range tests {
		c := Capabilities{Search: tt.scope}
		if got := c.SearchOn(tt.ref, "main"); got != tt.want {
			t.Errorf("Search %q: SearchOn(%q, main) = %v, want %v", tt.scope, tt.ref, got, tt.want)
		}
	}
}

func TestCISourcesReadable(t *testing.T) {
	tests := []struct {
		ci   CISources
		want bool
	}{
		{CISources{}, false},
		{CISources{CheckRuns: true}, true},
		{CISources{CommitStatuses: true}, true},
		{CISources{CheckRuns: true, CommitStatuses: true}, true},
	}
	for _, tt := range tests {
		if got := tt.ci.Readable(); got != tt.want {
			t.Errorf("%+v.Readable() = %v, want %v", tt.ci, got, tt.want)
		}
	}
}

func TestCapabilitiesValidate(t *testing.T) {
	tests := []struct {
		name string
		c    Capabilities
		want string
	}{
		{"any ref", Capabilities{Search: SearchAnyRef}, ""},
		{"default branch", Capabilities{Search: SearchDefaultBranch, TreeMayTruncate: true}, ""},
		{"no search", Capabilities{Search: SearchNone, CI: CISources{CommitStatuses: true}}, ""},
		{"zero scope", Capabilities{}, `unknown search scope ""`},
		{"made-up scope", Capabilities{Search: "branches"}, `unknown search scope "branches"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.c.Validate()
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("Validate() = %v, want an error with %q", err, tt.want)
			}
		})
	}
}

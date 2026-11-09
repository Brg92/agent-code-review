package github

import (
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
)

func TestCapabilities(t *testing.T) {
	c := Capabilities()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	want := base.Capabilities{
		Search:          base.SearchDefaultBranch,
		TreeMayTruncate: true,
		CI:              base.CISources{CheckRuns: true, CommitStatuses: true},
	}
	if c != want {
		t.Errorf("Capabilities() = %+v, want %+v", c, want)
	}
	if c.SearchOn("release/2.0", "main") {
		t.Error("SearchOn a branch other than the default = true: GitHub code search only indexes the default branch")
	}
}

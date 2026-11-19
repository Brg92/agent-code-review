package base_test

import (
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/testutil/forgecontract"
)

// TestContract runs the contract suite on the in-memory fake, with GitHub's
// capabilities. The GitHub provider runs the same suite on the replay of
// recorded scenarios, in its own package.
func TestContract(t *testing.T) {
	providers := []struct {
		name string
		skip string
		new  func() (base.Reader, forgecontract.Fixture)
	}{
		{name: "fake with github capabilities", new: func() (base.Reader, forgecontract.Fixture) {
			return forgecontract.Example(github.Capabilities())
		}},
		{name: "gitlab", skip: "GitLab provider arrives after 1.0.0"},
	}
	for _, p := range providers {
		t.Run(p.name, func(t *testing.T) {
			if p.skip != "" {
				t.Skip(p.skip)
			}
			r, fx := p.new()
			forgecontract.Run(t, r, fx)
		})
	}
}

package refs

import "github.com/Brg92/agent-code-review/internal/core"

type composed struct {
	primary Registry
	others  []Registry
}

// Compose returns the registry of a cascade: the references of primary,
// plus the links of the other platforms' registries. A cascade uses one
// platform, but a link to another one must be recognised to be reported as
// skipped rather than as an unreadable line. The short forms of the other
// platforms are not read.
func Compose(primary Registry, others ...Registry) Registry {
	return composed{primary: primary, others: others}
}

func (c composed) Forge() core.Forge { return c.primary.Forge() }

func (c composed) Parse(line string, from core.Key) []Ref {
	found := c.primary.Parse(line, from)
	for _, o := range c.others {
		if o.Forge() == c.primary.Forge() {
			continue
		}
		for _, r := range o.Parse(line, from) {
			if r.URL && r.Key.Forge != c.primary.Forge() {
				found = append(found, r)
			}
		}
	}
	return Normalize(found)
}

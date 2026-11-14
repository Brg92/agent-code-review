package base

import (
	"fmt"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
)

// Selection is the platform of a cascade: its root and the registry that
// reads its Related Changes. A cascade uses this platform only.
type Selection struct {
	Root core.Key
	// Registry reads the references of the root's platform and the links
	// of the others, which are then skipped as other-platform.
	Registry refs.Registry
}

// PlatformError reports a root reference that is not exactly one change
// request reference of a known platform.
type PlatformError struct {
	Root string
	// Found are the change requests the reference names, if more than one.
	Found []core.Key
}

func (e *PlatformError) Error() string {
	if len(e.Found) > 1 {
		return fmt.Sprintf("%q names %d change requests, want one", e.Root, len(e.Found))
	}
	return fmt.Sprintf("%q is not a change request reference of a known platform", e.Root)
}

// SelectPlatform reads the root reference a review starts from, a link or
// a short form such as owner/repo#N, and returns the platform of its
// cascade. from is the change request short forms are read against: its
// platform, host and project, the number unused; short forms of another
// platform are not read. registries are the platforms' reference
// registries; one must belong to the platform of from.
func SelectPlatform(root string, from core.Key, registries ...refs.Registry) (Selection, error) {
	primary, others := split(from.Forge, registries)
	if primary == nil {
		return Selection{}, fmt.Errorf("select platform: no reference registry for %s", from.Forge)
	}
	found := refs.Compose(primary, others...).Parse(strings.TrimSpace(root), from)
	if len(found) != 1 {
		var keys []core.Key
		if len(found) > 1 {
			for _, r := range found {
				keys = append(keys, r.Key)
			}
		}
		return Selection{}, &PlatformError{Root: root, Found: keys}
	}
	rootKey := found[0].Key
	chosen, rest := split(rootKey.Forge, registries)
	return Selection{Root: rootKey, Registry: refs.Compose(chosen, rest...)}, nil
}

// split returns the registry of forge and the others.
func split(forge core.Forge, registries []refs.Registry) (refs.Registry, []refs.Registry) {
	var primary refs.Registry
	var others []refs.Registry
	for _, r := range registries {
		if primary == nil && r.Forge() == forge {
			primary = r
			continue
		}
		others = append(others, r)
	}
	return primary, others
}

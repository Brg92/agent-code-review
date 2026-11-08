package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// Set is the packs available to a run, with every rule they and the core
// define.
type Set struct {
	Packs []*Pack
	// Rules are the core rules, then the rules of each pack in order.
	Rules []*rules.Rule
}

// Load loads every pack in the given directories, each holding one pack
// per subdirectory: the built-in packs first, then any configured extra
// directories. Packs are listed by directory, then by name. Pack ids and
// rule ids, the core rules included, must be unique.
func Load(dirs ...string) (*Set, error) {
	set := &Set{}
	seen := map[string]string{}
	catalogs := []*rules.Catalog{rules.Core()}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("packs: %w", err)
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			p, err := LoadPack(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if prev, ok := seen[p.ID]; ok {
				return nil, fmt.Errorf("packs: pack %q is in both %s and %s", p.ID, prev, p.Dir)
			}
			seen[p.ID] = p.Dir
			set.Packs = append(set.Packs, p)
			catalogs = append(catalogs, p.Rules)
		}
	}
	var err error
	if set.Rules, err = rules.Merge(catalogs...); err != nil {
		return nil, fmt.Errorf("packs: %w", err)
	}
	return set, nil
}

// ActiveRules returns the rules that can apply to a repository: the core
// rules, then those of each active pack in order. Their conditions are
// checked per file with Assignment.Context.
func (s *Set) ActiveRules(active []*Pack) []*rules.Rule {
	out := append([]*rules.Rule(nil), rules.Core().Rules...)
	for _, p := range active {
		out = append(out, p.Rules.Rules...)
	}
	return out
}

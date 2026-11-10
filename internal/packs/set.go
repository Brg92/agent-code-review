package packs

import (
	"fmt"
	"io/fs"
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

// Location is a directory of packs, one pack per subdirectory, read
// through FS; Dir names it in paths and errors.
type Location struct {
	Dir string
	FS  fs.FS
}

// Load loads every pack in the given directories from the disk; see
// LoadFS.
func Load(dirs ...string) (*Set, error) {
	locs := make([]Location, 0, len(dirs))
	for _, dir := range dirs {
		locs = append(locs, Location{Dir: dir, FS: os.DirFS(dir)})
	}
	return LoadFS(locs...)
}

// LoadFS loads every pack in the given locations: the built-in packs
// first, then any configured extra directories. Packs are listed by
// location, then by name. Pack ids and rule ids, the core rules included,
// must be unique.
func LoadFS(locs ...Location) (*Set, error) {
	set := &Set{}
	seen := map[string]string{}
	catalogs := []*rules.Catalog{rules.Core()}
	for _, loc := range locs {
		entries, err := fs.ReadDir(loc.FS, ".")
		if err != nil {
			return nil, fmt.Errorf("packs: %s: %w", loc.Dir, err)
		}
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			sub, err := fs.Sub(loc.FS, name)
			if err != nil {
				return nil, fmt.Errorf("packs: %w", err)
			}
			p, err := loadPack(sub, filepath.Join(loc.Dir, name))
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

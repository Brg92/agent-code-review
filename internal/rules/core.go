package rules

import (
	_ "embed"
	"fmt"
)

// CorePack is the pack name of the core catalog.
const CorePack = "core"

//go:embed core.json
var coreJSON []byte

// core is the parsed core catalog; a broken embedded file fails at start.
var core = func() *Catalog {
	c, err := LoadCatalog(coreJSON)
	if err != nil {
		panic(fmt.Sprintf("embedded core rules: %v", err))
	}
	return c
}()

// Core returns the core catalog: the rules that need the whole change
// request or the cascade, whichever stack it uses. They have no match.
func Core() *Catalog { return core }

// Merge lists the rules of the catalogs in order, and fails if two
// catalogs, or one catalog twice, define the same rule id.
func Merge(catalogs ...*Catalog) ([]*Rule, error) {
	owner := map[string]string{}
	var rules []*Rule
	for _, c := range catalogs {
		for _, r := range c.Rules {
			if prev, ok := owner[r.ID]; ok {
				return nil, fmt.Errorf("rule %q is defined by both %s and %s", r.ID, prev, c.Pack)
			}
			owner[r.ID] = c.Pack
			rules = append(rules, r)
		}
	}
	return rules, nil
}

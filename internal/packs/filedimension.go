package packs

import (
	"fmt"
	"sort"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// FileDimension is an axis along which the changed files of a repository
// vary, for example the platform of a source set. Each file has one value:
// that of the first Map entry whose glob matches it, else Default.
type FileDimension struct {
	ID      string
	Values  []string
	Map     []FileMapEntry
	Default string
}

// FileMapEntry maps the files matching Path to Value.
type FileMapEntry struct {
	Path  rules.Glob
	Value string
}

type (
	rawFileDim struct {
		ID      *string      `json:"id"`
		Values  *[]string    `json:"values"`
		Map     *[]rawFileEn `json:"map"`
		Default *string      `json:"default"`
	}
	rawFileEn struct {
		Path  *string `json:"path"`
		Value *string `json:"value"`
	}
)

func (p *Pack) loadFileDimensions(raws []rawFileDim) error {
	seen := map[string]bool{}
	for _, d := range p.Dimensions {
		seen[d.ID] = true
	}
	for _, rd := range raws {
		if rd.ID == nil || !dimensionID.MatchString(*rd.ID) {
			return fmt.Errorf("fileDimensions: id %s is not a lowercase dimension id", deref(rd.ID))
		}
		d := FileDimension{ID: *rd.ID}
		fail := func(format string, args ...any) error {
			return fmt.Errorf("file dimension %s: "+format, append([]any{d.ID}, args...)...)
		}
		switch {
		case d.ID == ProfileDimension:
			return fail("the id is reserved for the profile")
		case seen[d.ID]:
			return fail("listed twice, or the id of a dimension of the pack")
		}
		seen[d.ID] = true
		if rd.Values == nil || len(*rd.Values) == 0 {
			return fail("values is empty")
		}
		has := map[string]bool{}
		for _, v := range *rd.Values {
			if !dimensionID.MatchString(v) || has[v] {
				return fail("value %q is not a distinct lowercase value", v)
			}
			has[v] = true
			d.Values = append(d.Values, v)
		}
		if rd.Default == nil || !has[*rd.Default] {
			return fail("default %s is not one of the values", deref(rd.Default))
		}
		d.Default = *rd.Default
		if rd.Map == nil {
			return fail("map is required")
		}
		for _, e := range *rd.Map {
			if e.Value == nil || !has[*e.Value] {
				return fail("an entry shows value %s, which is not one of the values", deref(e.Value))
			}
			if e.Path == nil {
				return fail("an entry has no path")
			}
			g, err := rules.CompileGlob(*e.Path)
			if err != nil {
				return fail("map: %v", err)
			}
			d.Map = append(d.Map, FileMapEntry{Path: g, Value: *e.Value})
		}
		p.FileDimensions = append(p.FileDimensions, d)
	}
	return nil
}

// ResolveFileDimensions returns the value of each file dimension of the
// active packs for a file. When several packs declare a dimension, the one
// with the highest priority decides, the first in order among equals; its
// first matching map entry gives the value, else its default.
func ResolveFileDimensions(active []*Pack, filePath string) map[string]string {
	out := map[string]string{}
	for id, w := range winners(active) {
		out[id] = w.value(filePath)
	}
	return out
}

// winners picks, for each file dimension id, the declaration of the active
// pack with the highest priority, the first in order among equals.
func winners(active []*Pack) map[string]winner {
	out := map[string]winner{}
	for _, p := range active {
		for i := range p.FileDimensions {
			d := &p.FileDimensions[i]
			if w, ok := out[d.ID]; !ok || p.Priority > w.priority {
				out[d.ID] = winner{pack: p, priority: p.Priority, dim: d}
			}
		}
	}
	return out
}

type winner struct {
	pack     *Pack
	priority int
	dim      *FileDimension
}

func (w winner) value(filePath string) string {
	for _, e := range w.dim.Map {
		if e.Path.Match(filePath) {
			return e.Value
		}
	}
	return w.dim.Default
}

// checkFileDimensions fails when two active packs of the highest priority
// for a file dimension declare it differently: neither could win.
func checkFileDimensions(active []*Pack) error {
	won := winners(active)
	ids := make([]string, 0, len(won))
	for id := range won {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w := won[id]
		for _, p := range active {
			if p == w.pack || p.Priority != w.priority {
				continue
			}
			for i := range p.FileDimensions {
				if d := &p.FileDimensions[i]; d.ID == id && !d.equal(w.dim) {
					return fmt.Errorf("file dimension %s: packs %s and %s have the same priority %d and map it differently", id, w.pack.ID, p.ID, w.priority)
				}
			}
		}
	}
	return nil
}

func (d *FileDimension) equal(o *FileDimension) bool {
	if d.Default != o.Default || len(d.Values) != len(o.Values) || len(d.Map) != len(o.Map) {
		return false
	}
	for i := range d.Values {
		if d.Values[i] != o.Values[i] {
			return false
		}
	}
	for i := range d.Map {
		if d.Map[i].Value != o.Map[i].Value || d.Map[i].Path.String() != o.Map[i].Path.String() {
			return false
		}
	}
	return true
}

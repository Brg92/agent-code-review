package packs

import (
	"fmt"

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

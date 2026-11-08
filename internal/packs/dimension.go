package packs

import (
	"fmt"
	"strings"
)

// Where the value of a dimension comes from.
const (
	DimensionFromConfig  = "config"
	DimensionFromPack    = "pack-detect"
	DimensionFromDefault = "pack-default"
)

// DimensionValue is the resolved value of a dimension and why it was
// chosen.
type DimensionValue struct {
	ID     string
	Value  string
	Reason string
	Detail string
}

// ResolveDimensions resolves the dimensions of the active packs, in pack
// order. When two active packs declare the same dimension, the first
// wins. A value in configured, keyed by dimension id, wins over
// detection; a configured dimension no active pack declares is ignored.
// Without a configured value, the signs decide: no sign matching gives the
// default, signs of one value give it, signs of several give the combined
// value of the dimension, or the value of the first sign that matches.
func ResolveDimensions(active []*Pack, configured map[string]string, paths []string, repo Reader) ([]DimensionValue, error) {
	var out []DimensionValue
	seen := map[string]bool{}
	for _, p := range active {
		for _, d := range p.Dimensions {
			if seen[d.ID] {
				continue
			}
			seen[d.ID] = true
			v, err := d.resolve(p.ID, configured, paths, repo)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	}
	return out, nil
}

func (d Dimension) resolve(pack string, configured map[string]string, paths []string, repo Reader) (DimensionValue, error) {
	v := DimensionValue{ID: d.ID}
	if c, ok := configured[d.ID]; ok {
		if !d.has(c) {
			return v, fmt.Errorf("pack %s: dimension %s: the configuration sets %q, which is not one of %s", pack, d.ID, c, strings.Join(d.Values, ", "))
		}
		v.Value, v.Reason = c, DimensionFromConfig
		return v, nil
	}
	var found []string
	var first DimensionSign
	for _, s := range d.Detect {
		if contains(found, s.Value) {
			continue
		}
		ok, err := s.matches(paths, repo)
		if err != nil {
			return v, fmt.Errorf("pack %s: dimension %s: %w", pack, d.ID, err)
		}
		if ok {
			if len(found) == 0 {
				first = s
			}
			found = append(found, s.Value)
		}
	}
	switch {
	case len(found) == 0:
		v.Value, v.Reason, v.Detail = d.Default, DimensionFromDefault, pack
	case len(found) == 1 || d.Combined == "":
		v.Value, v.Reason, v.Detail = first.Value, DimensionFromPack, pack+": "+first.describe()
	default:
		v.Value, v.Reason, v.Detail = d.Combined, DimensionFromPack, pack+": "+strings.Join(found, ", ")
	}
	return v, nil
}

func (d Dimension) has(value string) bool { return contains(d.Values, value) }

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

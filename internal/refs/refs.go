// Package refs reads the Related Changes section of a change request
// description: it finds the section, classifies its lines and, through a
// per-platform Registry, turns the references on a line into change request
// keys. It does no I/O and follows nothing; walking the cascade is the job
// of package cascade.
package refs

import (
	"slices"

	"github.com/Brg92/agent-code-review/internal/core"
)

// Ref is a reference to a change request found on a line.
type Ref struct {
	Key core.Key
	// Pos is the byte offset of the reference in the line.
	Pos int
	// URL is true for a reference written as a link, false for a short
	// form such as #N.
	URL bool
}

// Registry recognises the references of one platform. Implementations live
// with the platform code (internal/forges/<forge>).
type Registry interface {
	// Forge is the platform the registry belongs to.
	Forge() core.Forge
	// Parse returns the references on line, each at most once per key,
	// in line order. from is the change request whose description holds
	// the line; it gives the host and project of short forms such as #N.
	Parse(line string, from core.Key) []Ref
}

// Normalize sorts references by position and keeps the first of each change
// request. Registries return their references normalized.
func Normalize(found []Ref) []Ref {
	slices.SortStableFunc(found, func(a, b Ref) int { return a.Pos - b.Pos })
	var out []Ref
	for _, r := range found {
		if !slices.ContainsFunc(out, func(o Ref) bool { return o.Key.Same(r.Key) }) {
			out = append(out, r)
		}
	}
	return out
}

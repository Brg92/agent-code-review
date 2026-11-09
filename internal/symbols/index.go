package symbols

import (
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// File is a file a change request changes, as the index sees it.
type File struct {
	// Path is the path at head, the old path for a removed file.
	Path string
	// Spec and Table are those of the language of the file; a file
	// without Spec is not compared.
	Spec  *Spec
	Table *lexer.Table
	// Base and Head are the contents; empty for an added or a removed
	// file.
	Base, Head string
}

// ChangeKind is how a change request changes a public symbol.
type ChangeKind string

// The kinds of changes.
const (
	Added            ChangeKind = "added"
	Removed          ChangeKind = "removed"
	SignatureChanged ChangeKind = "signature-changed"
)

// Located is a declaration in a file.
type Located struct {
	Path string
	Decl
}

// Change is a change of a public symbol.
type Change struct {
	Kind ChangeKind
	ID   Identity
	// Base and Head are the declaration at base and at head, the first
	// overload of a function; nil on the side where it is absent.
	Base, Head *Located
	// NewRequired lists the parameters without a default that the function
	// has at head and, by name, not at base. It is set only when the
	// function has a single overload at base and at head.
	NewRequired []string
}

// side holds the public declarations of one side, by identity, in the
// order they first appear.
type side struct {
	order []Identity
	decls map[Identity][]Located
	// at is the position of each identity: file index, then line.
	at map[Identity][2]int
}

func newSide() *side {
	return &side{decls: map[Identity][]Located{}, at: map[Identity][2]int{}}
}

func (s *side) add(file int, path string, d Decl) {
	id := d.ID()
	if _, ok := s.decls[id]; !ok {
		s.order = append(s.order, id)
		s.at[id] = [2]int{file, d.Line}
	}
	s.decls[id] = append(s.decls[id], Located{Path: path, Decl: d})
}

// IndexOf compares the public declarations of the files at base and at
// head, across the whole change request: a symbol moved from one changed
// file to another is not a change, and one that stops being public is
// removed. Changes follow the files, then the line of the declaration:
// at head when the symbol has a head side, else at base.
func IndexOf(files []File) []Change {
	base, head := newSide(), newSide()
	for i, f := range files {
		if f.Spec == nil {
			continue
		}
		for _, s := range []struct {
			side *side
			src  string
		}{{base, f.Base}, {head, f.Head}} {
			for _, d := range Extract(f.Spec, f.Table, s.src) {
				if d.Public {
					s.side.add(i, f.Path, d)
				}
			}
		}
	}
	type placed struct {
		Change
		at [2]int
	}
	var out []placed
	for _, id := range base.order {
		b := base.decls[id]
		h, ok := head.decls[id]
		switch {
		case !ok:
			out = append(out, placed{Change{Kind: Removed, ID: id, Base: &b[0]}, base.at[id]})
		case id.Kind == Function && !sameSignatures(b, h):
			c := Change{Kind: SignatureChanged, ID: id, Base: &b[0], Head: &h[0]}
			if len(b) == 1 && len(h) == 1 {
				c.NewRequired = newRequired(b[0].Params, h[0].Params)
			}
			out = append(out, placed{c, head.at[id]})
		}
	}
	for _, id := range head.order {
		if _, ok := base.decls[id]; !ok {
			out = append(out, placed{Change{Kind: Added, ID: id, Head: &head.decls[id][0]}, head.at[id]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].at, out[j].at
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		return a[1] < b[1]
	})
	changes := make([]Change, len(out))
	for i, p := range out {
		changes[i] = p.Change
	}
	return changes
}

// sameSignatures reports whether two sides declare the same overloads,
// in any order.
func sameSignatures(a, b []Located) bool {
	if len(a) != len(b) {
		return false
	}
	sigs := func(ls []Located) []string {
		out := make([]string, len(ls))
		for i, l := range ls {
			var ps []string
			for _, p := range l.Params {
				if p.Default {
					ps = append(ps, p.Name+"=")
				} else {
					ps = append(ps, p.Name)
				}
			}
			out[i] = strings.Join(ps, ",")
		}
		sort.Strings(out)
		return out
	}
	x, y := sigs(a), sigs(b)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// newRequired returns the parameters of head without a default whose name
// base does not have, or nil.
func newRequired(base, head []Param) []string {
	had := map[string]bool{}
	for _, p := range base {
		had[p.Name] = true
	}
	var out []string
	for _, p := range head {
		if !p.Default && !had[p.Name] {
			out = append(out, p.Name)
		}
	}
	return out
}

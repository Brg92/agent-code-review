package rules

import (
	"sort"

	"github.com/Brg92/agent-code-review/internal/symbols"
)

// detectSymbolUsage signals the public symbols the change request removes,
// or gives a new required parameter, that the repositories of its
// ancestors in the cascade still use. A removed symbol is signalled on
// its removed declaration, a changed one on its declaration at head, with
// a reference to each use. A use counts when the search of the ancestor's
// target branch finds it in code, not only in a comment or a string of
// its line, outside the lines the ancestor removes or replaces, and, for
// a removed symbol, on a line the ancestor adds: the ancestor's own
// update of a call to a changed function is not a use. A
// symbol any of whose searches fails gives no signal and its file is not
// verified.
func detectSymbolUsage(r *Rule, cr ChangeRequest, emit func(Signal), skip func(path, reason, symbol string)) {
	if len(cr.Ancestors) == 0 {
		return
	}
	var files []symbols.File
	for _, f := range cr.Files {
		if !r.appliesTo(f.Language) {
			continue
		}
		sf := symbols.File{Path: f.Path, Spec: f.Symbols, Table: f.Table, Base: f.Base}
		if !f.Removed {
			sf.Head = f.Source
		}
		files = append(files, sf)
	}
	removed := r.match.symbolChange == symbolRemoved
	for _, c := range symbols.IndexOf(files) {
		var at *symbols.Located
		side := ""
		switch {
		case removed && c.Kind == symbols.Removed:
			at, side = c.Base, SideLeft
		case !removed && len(c.NewRequired) > 0:
			at = c.Head
		default:
			continue
		}
		refs, err := uses(cr, c, removed)
		switch {
		case err != nil:
			skip(at.Path, ReasonSearchUnavailable, c.ID.Name)
		case len(refs) > 0:
			emit(Signal{Path: at.Path, Side: side, Line: at.Line, Column: at.Column, References: refs})
		}
	}
}

// uses returns the uses of a changed symbol in the repositories of the
// ancestors, ancestor by ancestor and then by path and line.
func uses(cr ChangeRequest, c symbols.Change, addedCount bool) ([]Reference, error) {
	var out []Reference
	for _, a := range cr.Ancestors {
		matches, err := cr.search(a.Project, a.Target, c.ID.Name)
		if err != nil {
			return nil, err
		}
		changed := map[string]ChangedFile{}
		for _, f := range a.Files {
			changed[f.Path] = f
			if f.OldPath != "" {
				changed[f.OldPath] = f
			}
		}
		seen := map[Reference]bool{}
		var refs []Reference
		keep := func(ref Reference) {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
		for _, m := range matches {
			if f, ok := changed[m.Path]; ok && (f.Removed || f.RemovedLines[m.Line]) {
				continue
			}
			if a.Project == cr.Project && c.Base != nil && m.Path == c.Base.Path && m.Line == c.Base.Line {
				continue // the declaration itself
			}
			if !cr.inCode(m, c.ID.Name) {
				continue
			}
			keep(Reference{Project: a.Project, Ref: a.Target, Path: m.Path, Line: m.Line})
		}
		if addedCount {
			for _, f := range a.Files {
				if f.Removed || f.Table == nil {
					continue
				}
				for _, p := range symbols.Uses(f.Table, f.Source, c.ID.Name) {
					if f.Added[p.Line] {
						keep(Reference{Project: a.Project, Ref: a.Head, Path: f.Path, Line: p.Line})
					}
				}
			}
		}
		sort.SliceStable(refs, func(i, j int) bool {
			if refs[i].Path != refs[j].Path {
				return refs[i].Path < refs[j].Path
			}
			return refs[i].Line < refs[j].Line
		})
		out = append(out, refs...)
	}
	return out, nil
}

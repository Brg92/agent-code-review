package rules

import (
	"strings"

	"github.com/Brg92/agent-code-review/internal/symbols"
)

// detectExpectActual signals each expect declaration that the change
// request adds or changes in a file of the common group, when a platform
// group of its module has files at head and holds no matching actual, or,
// for an intermediate group without files, one of its target groups that
// has files holds none:
// same package, containers, name and kind, with the actual modifier. An
// actual is looked for in the changed files at head, then by a search of
// the target branch outside them. A module whose platform groups cannot
// be listed, or a declaration whose search fails, gives no signal and
// leaves its file not verified.
func detectExpectActual(r *Rule, cr ChangeRequest, emit func(Signal), skip func(path, reason, symbol string)) {
	e := r.match.expect
	changed := map[string]bool{}
	for _, f := range cr.Files {
		changed[f.Path] = true
		if f.OldPath != "" {
			changed[f.OldPath] = true
		}
	}
	type platforms struct {
		dirs []string // platform groups with files at head
		err  error
	}
	modules := map[string]platforms{}
	present := func(module string) platforms {
		if p, ok := modules[module]; ok {
			return p
		}
		var p platforms
		// with lists the directory of a group when it has files, and
		// reports whether it has.
		with := func(g string) (bool, error) {
			dir := module + strings.Replace(e.pattern, groupPlaceholder, g, 1)
			paths, err := cr.list(dir)
			if err == nil && len(paths) > 0 {
				p.dirs = append(p.dirs, dir)
			}
			return len(paths) > 0, err
		}
	groups:
		for _, g := range e.groups {
			has, err := with(g)
			if err != nil {
				p = platforms{err: err}
				break
			}
			if has {
				continue
			}
			for _, t := range e.targets[g] {
				if _, err := with(t); err != nil {
					p = platforms{err: err}
					break groups
				}
			}
		}
		modules[module] = p
		return p
	}
	for _, f := range cr.Files {
		if f.Removed || f.Symbols == nil || !r.appliesTo(f.Language) {
			continue
		}
		module, _, ok := underGroup(f.Path, e.pattern, e.common)
		if !ok {
			continue
		}
		for _, d := range symbols.Extract(f.Symbols, f.Table, f.Source) {
			if !hasModifier(d, e.expect) || !touched(d, f.Added) {
				continue
			}
			p := present(module)
			if p.err != nil {
				skip(f.Path, ReasonTreeUnavailable, d.Name)
				continue
			}
			switch missing, err := missingActual(cr, e.actual, d, p.dirs, changed); {
			case err != nil:
				skip(f.Path, ReasonSearchUnavailable, d.Name)
			case missing:
				emit(Signal{Path: f.Path, Line: d.Line, Column: d.Column})
			}
		}
	}
}

// missingActual reports whether one of the platform directories holds no
// actual of the expect declaration d, in the changed files at head or,
// outside them, through a search made at most once.
func missingActual(cr ChangeRequest, actual string, d symbols.Decl, dirs []string, changed map[string]bool) (bool, error) {
	var matches []Match
	searched := false
	for _, dir := range dirs {
		if actualInChange(cr, actual, d, dir) {
			continue
		}
		if !searched {
			var err error
			if matches, err = cr.search(cr.Project, cr.Target, d.Name); err != nil {
				return false, err
			}
			searched = true
		}
		found := false
		for _, m := range matches {
			found = found || strings.HasPrefix(m.Path, dir) && !changed[m.Path]
		}
		if !found {
			return true, nil
		}
	}
	return false, nil
}

// actualInChange reports whether a changed file under dir declares, at
// head, the actual of d.
func actualInChange(cr ChangeRequest, actual string, d symbols.Decl, dir string) bool {
	for _, f := range cr.Files {
		if f.Removed || f.Symbols == nil || !strings.HasPrefix(f.Path, dir) {
			continue
		}
		for _, a := range symbols.Extract(f.Symbols, f.Table, f.Source) {
			if hasModifier(a, actual) && a.ID() == d.ID() {
				return true
			}
		}
	}
	return false
}

func hasModifier(d symbols.Decl, m string) bool {
	for _, x := range d.Modifiers {
		if x == m {
			return true
		}
	}
	return false
}

// touched reports whether the change adds a line of the declaration.
func touched(d symbols.Decl, added map[int]bool) bool {
	for l := d.StartLine; l <= d.EndLine; l++ {
		if added[l] {
			return true
		}
	}
	return false
}

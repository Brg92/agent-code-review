package rules

import (
	"path"
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// Tree is the repository tree at the head of a change request.
type Tree interface {
	// List returns the paths of the files whose path starts with prefix,
	// relative to the repository with forward slashes.
	List(prefix string) []string
}

// ChangedFile is a file a change request changes, as the change-request
// rules see it. Path is the path at head, the old path for a removed
// file; Source and Added are the contents at head and the lines the
// change request adds. A file no pack covers has no language or table.
type ChangedFile struct {
	File
	// OldPath is the previous path of a renamed or copied file.
	OldPath string
	// Removed is set when the change request deletes the file.
	Removed bool
}

// ChangeRequest is what the change-request rules see.
type ChangeRequest struct {
	Files []ChangedFile
	// Tree lists the files at head; nil lists none.
	Tree Tree
	// Variants are the variants of the repository configuration.
	Variants []string
}

func (cr ChangeRequest) list(prefix string) []string {
	if cr.Tree == nil {
		return nil
	}
	return cr.Tree.List(prefix)
}

// RunChangeRequest runs the rules whose checks see the whole change
// request; the others are left to Run. Every signal has origin mr: the
// change request is what they are about. Signals follow the order of the
// files, then line, column and rule order, one per rule, file and line.
func RunChangeRequest(rules []*Rule, cr ChangeRequest) []Signal {
	fileAt := map[string]int{}
	for i, f := range cr.Files {
		if _, ok := fileAt[f.Path]; !ok {
			fileAt[f.Path] = i
		}
	}
	order := map[string]int{}
	var sigs []Signal
	for i, r := range rules {
		if !r.OnChangeRequest() {
			continue
		}
		order[r.ID] = i
		type place struct {
			path string
			line int
		}
		seen := map[place]bool{}
		add := func(f File, line, column int) {
			key := place{f.Path, line}
			if seen[key] {
				return
			}
			seen[key] = true
			s := r.signal(f, line, column)
			s.Origin = OriginMR
			sigs = append(sigs, s)
		}
		switch r.match.detector {
		case detectorMissingTest:
			detectMissingTest(r, cr, add)
		case detectorCounterpart:
			detectCounterpart(r, cr, add)
		}
	}
	sort.SliceStable(sigs, func(i, j int) bool {
		a, b := sigs[i], sigs[j]
		switch {
		case a.Path != b.Path:
			return fileAt[a.Path] < fileAt[b.Path]
		case a.Line != b.Line:
			return a.Line < b.Line
		case a.Column != b.Column:
			return a.Column < b.Column
		}
		return order[a.RuleID] < order[b.RuleID]
	})
	return sigs
}

// detectMissingTest signals the sources the change request adds or modifies
// whose name ends with a suffix and whose test the change request neither
// adds nor modifies, and that no test root of their module holds at head.
// The signal is on the declaration named like the file, line 1 if none.
func detectMissingTest(r *Rule, cr ChangeRequest, add func(File, int, int)) {
	t := r.match.test
	inChange := map[string]bool{}
	for _, f := range cr.Files {
		if !f.Removed {
			inChange[path.Base(f.Path)] = true
		}
	}
	for _, f := range cr.Files {
		if f.Removed || !r.appliesTo(f.Language) || !anyGlob(t.sources, f.Path) {
			continue
		}
		base := path.Base(f.Path)
		name := strings.TrimSuffix(base, path.Ext(base))
		if !hasAnySuffix(name, t.suffixes) {
			continue
		}
		test := strings.Replace(t.name, namePlaceholder, name, 1)
		names := map[string]bool{}
		found := false
		for _, ext := range t.extensions {
			names[test+ext] = true
			found = found || inChange[test+ext]
		}
		module := modulePrefix(f.Path, t.moduleSeparator)
		for _, root := range t.roots {
			for _, p := range cr.list(module + root + "/") {
				found = found || names[path.Base(p)]
			}
		}
		if !found {
			line, column := declaration(f.File, name)
			add(f.File, line, column)
		}
	}
}

// detectCounterpart signals a file the change request adds or modifies
// under one group when a file with the same path relative to its group,
// platform suffix stripped, exists at head under another group of the
// same module and the change request does not change it. The signal is a
// candidate on the first added line, line 1 if none. Groups taken from
// the variants need at least two of them.
func detectCounterpart(r *Rule, cr ChangeRequest, add func(File, int, int)) {
	c := r.match.counterpart
	groups := c.groups
	if c.fromVariants {
		if groups = cr.Variants; len(groups) < 2 {
			return
		}
	}
	changed := map[string]bool{}
	for _, f := range cr.Files {
		changed[f.Path] = true
		if f.OldPath != "" {
			changed[f.OldPath] = true
		}
	}
	for _, f := range cr.Files {
		if f.Removed || !r.appliesTo(f.Language) {
			continue
		}
		for _, a := range groups {
			module, rel, ok := underGroup(f.Path, c.pattern, a)
			if !ok {
				continue
			}
			key := stripSuffix(rel, c.strip)
			if hasCounterpart(cr, c, groups, a, module, key, changed) {
				add(f.File, firstAdded(f.Added), 1)
			}
			break
		}
	}
}

// underGroup reports whether the path contains the pattern of the group
// at the start of a segment, and returns what comes before and after it.
func underGroup(p, pattern, group string) (module, rel string, ok bool) {
	needle := strings.Replace(pattern, groupPlaceholder, group, 1)
	i := segmentIndex(p, needle)
	if i < 0 {
		return "", "", false
	}
	return p[:i], p[i+len(needle):], true
}

// hasCounterpart reports whether a group other than a holds, at head and
// unchanged, a file of the module whose relative path is key once
// stripped.
func hasCounterpart(cr ChangeRequest, c counterpartParams, groups []string, a, module, key string, changed map[string]bool) bool {
	for _, b := range groups {
		if b == a {
			continue
		}
		dir := module + strings.Replace(c.pattern, groupPlaceholder, b, 1)
		for _, p := range cr.list(dir) {
			if !changed[p] && stripSuffix(strings.TrimPrefix(p, dir), c.strip) == key {
				return true
			}
		}
	}
	return false
}

// stripSuffix removes the first of the suffixes that ends the file name
// before its extension.
func stripSuffix(rel string, suffixes []string) string {
	dir, base := path.Split(rel)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for _, s := range suffixes {
		if strings.HasSuffix(stem, s) {
			stem = strings.TrimSuffix(stem, s)
			break
		}
	}
	return dir + stem + ext
}

// firstAdded returns the first added line, 1 if none.
func firstAdded(added map[int]bool) int {
	first := 0
	for l := range added {
		if first == 0 || l < first {
			first = l
		}
	}
	if first == 0 {
		return 1
	}
	return first
}

func hasAnySuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// segmentIndex returns the index of the first occurrence of the fragment
// at the start of a segment of the path, or -1.
func segmentIndex(p, fragment string) int {
	for i := 0; i+len(fragment) <= len(p); i++ {
		if (i == 0 || p[i-1] == '/') && strings.HasPrefix(p[i:], fragment) {
			return i
		}
	}
	return -1
}

// modulePrefix returns the part of a path before the module separator,
// with its final slash; "" when the separator starts the path or does not
// occur at the start of a segment.
func modulePrefix(p, separator string) string {
	if i := segmentIndex(p, separator); i > 0 {
		return p[:i]
	}
	return ""
}

// declaration finds the first identifier named name that follows a
// keyword, as in "class Name", and returns its line and column; 1, 1 when
// the file has none or no lexer table.
func declaration(f File, name string) (int, int) {
	if f.Table == nil {
		return 1, 1
	}
	code := codeTokens(lexer.Tokenize(f.Table, f.Source))
	for i, t := range code {
		if i > 0 && t.Kind == lexer.Identifier && t.Text == name && code[i-1].Kind == lexer.Keyword {
			return t.Line, t.Column
		}
	}
	return 1, 1
}

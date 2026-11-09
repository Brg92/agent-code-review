package core

import (
	"strings"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// AddedLines returns the RIGHT side numbers of the lines the diff adds.
func (d FileDiff) AddedLines() map[int]bool {
	added := map[int]bool{}
	for _, h := range d.Hunks {
		for _, l := range h.Lines {
			if l.Kind == LineAdded {
				added[l.New] = true
			}
		}
	}
	return added
}

// RulesFile returns the changed file as the change-request rules see it.
// assigned holds what the packs give its path: the language, the lexer
// table and whether it is a test; the zero value when no pack covers it.
// The path, the old path, the contents at head and the added lines come
// from the change.
func (f ChangedFile) RulesFile(assigned rules.File) rules.ChangedFile {
	src := ""
	if len(f.Head) > 0 {
		src = strings.Join(f.Head, "\n") + "\n"
	}
	assigned.Path, assigned.Source, assigned.Added = f.Diff.Path, src, f.Diff.AddedLines()
	return rules.ChangedFile{File: assigned, OldPath: f.Diff.OldPath, Removed: f.Diff.Status == FileRemoved}
}

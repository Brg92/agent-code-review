package core

import (
	"sort"
	"strconv"
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

// RemovedLines returns the LEFT side numbers of the lines the diff
// removes.
func (d FileDiff) RemovedLines() map[int]bool {
	removed := map[int]bool{}
	for _, h := range d.Hunks {
		for _, l := range h.Lines {
			if l.Kind == LineRemoved {
				removed[l.Old] = true
			}
		}
	}
	return removed
}

// RulesFile returns the changed file as the change-request rules see it.
// assigned holds what the packs give its path: the language, the lexer
// table, the symbols and whether it is a test; the zero value when no
// pack covers it. The path, the old path, the contents at base and at
// head and the added and removed lines come from the change.
func (f ChangedFile) RulesFile(assigned rules.File) rules.ChangedFile {
	assigned.Path, assigned.Source, assigned.Added = f.Diff.Path, joinLines(f.Head), f.Diff.AddedLines()
	return rules.ChangedFile{
		File: assigned, OldPath: f.Diff.OldPath, Removed: f.Diff.Status == FileRemoved,
		Base: joinLines(f.Base), RemovedLines: f.Diff.RemovedLines(),
	}
}

// joinLines joins lines into a source, each ended by a line break.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// NumberSignals lists the signals of a change request as its work package
// does and gives each its short id: s1, s2, ... The lists are typically
// the signals of each file and those of the change-request rules. Signals
// follow the order of the changed files, then line and column; on a tie,
// and for a path that is not a changed file, which comes last, they keep
// the order of the lists.
func NumberSignals(files []ChangedFile, lists ...[]rules.Signal) []Signal {
	fileAt := map[string]int{}
	for i, f := range files {
		if _, ok := fileAt[f.Diff.Path]; !ok {
			fileAt[f.Diff.Path] = i
		}
	}
	at := func(path string) int {
		if i, ok := fileAt[path]; ok {
			return i
		}
		return len(files)
	}
	var all []rules.Signal
	for _, l := range lists {
		all = append(all, l...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		switch {
		case at(a.Path) != at(b.Path):
			return at(a.Path) < at(b.Path)
		case at(a.Path) == len(files):
			return false
		case a.Line != b.Line:
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	out := make([]Signal, 0, len(all))
	for i, s := range all {
		sig := Signal{
			ID: "s" + strconv.Itoa(i+1), Rule: s.RuleID, Severity: Severity(s.Severity), Path: s.Path,
			Line: s.Line, Column: s.Column, Origin: Origin(s.Origin), NeedsJudgement: s.NeedsJudgement,
		}
		if s.Side == rules.SideLeft {
			sig.Side = Left
		}
		for _, r := range s.References {
			sig.References = append(sig.References, Reference{Project: r.Project, Ref: r.Ref, Path: r.Path, Line: r.Line})
		}
		out = append(out, sig)
	}
	return out
}

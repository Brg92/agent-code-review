package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// Defaults of the work package builder.
const (
	// DefaultWholeFileLines is the most lines a file may have at head to
	// be included whole.
	DefaultWholeFileLines = 300
	// DefaultContextLines are the lines of context around each hunk of a
	// larger file.
	DefaultContextLines = 20
	// DefaultPartBudget is the most bytes of files a part carries, unless
	// a single file is larger.
	DefaultPartBudget = 40000
)

// Reasons a file of the work package is placed in the summary, besides
// the not verified reasons, such as binary, that the caller gives.
const (
	ReasonPatchUnavailable = "patch-unavailable"
	ReasonRenamedUnchanged = "renamed-without-changes"
	ReasonCopiedUnchanged  = "copied-without-changes"
)

// WorkPackage is everything the agent needs to judge one change request.
// Its JSON form is defined by schemas/workpackage.schema.json.
type WorkPackage struct {
	Schema         int              `json:"schema"`
	RunID          string           `json:"runId"`
	CR             Key              `json:"cr"`
	URL            string           `json:"url"`
	Title          string           `json:"title"`
	Description    string           `json:"description"`
	HeadSHA        string           `json:"headSha"`
	BaseSHA        string           `json:"baseSha"`
	BaseRef        string           `json:"baseRef"`
	Criteria       PackageCriteria  `json:"criteria"`
	Rules          []PackageRule    `json:"rules"`
	Checklist      PackageChecklist `json:"checklist"`
	Files          []PackageFile    `json:"files"`
	Signals        []Signal         `json:"signals"`
	NotVerified    []UnverifiedFile `json:"notVerified"`
	RelatedChanges []RelatedChange  `json:"relatedChanges"`
	Cascade        PackageCascade   `json:"cascade"`
	Parts          Parts            `json:"parts"`
	JudgementPath  string           `json:"judgementPath"`
}

// PackageCriteria are the criteria a change request is judged with.
type PackageCriteria struct {
	Source        string   `json:"source"`
	Profile       string   `json:"profile"`
	ProfileReason string   `json:"profileReason"`
	Packs         []string `json:"packs"`
	// Dimensions are the repository dimensions, by id.
	Dimensions map[string]DimensionChoice `json:"dimensions"`
	// Files are the criteria files read on the target branch.
	Files   []string      `json:"files"`
	Ignored []IgnoredItem `json:"ignored"`
}

// PackageRule is a rule that applies, as the work package lists it.
type PackageRule struct {
	ID             string         `json:"id"`
	Severity       Severity       `json:"severity"`
	Title          string         `json:"title"`
	Source         string         `json:"source"`
	Group          string         `json:"group,omitempty"`
	NeedsJudgement bool           `json:"needsJudgement"`
	Options        map[string]int `json:"options,omitempty"`
}

// PackageChecklist is the checklist of the work package, by rule id.
type PackageChecklist struct {
	Sections   []ChecklistSection `json:"sections"`
	Repository []string           `json:"repository"`
}

// ChecklistSection is one group of the checklist.
type ChecklistSection struct {
	Heading string   `json:"heading,omitempty"`
	Rules   []string `json:"rules"`
}

// PackageFile is a changed file as the work package lists it.
type PackageFile struct {
	Path         string     `json:"path"`
	PreviousPath string     `json:"previousPath,omitempty"`
	Status       FileStatus `json:"status"`
	Language     string     `json:"language,omitempty"`
	// Placement is Inline when the diff shows lines of the file, Summary
	// when it shows none.
	Placement Placement `json:"placement"`
	// Reason says why a file is placed in the summary.
	Reason string `json:"reason,omitempty"`
	// Diff holds the hunks of the file, as a unified diff.
	Diff string `json:"diff,omitempty"`
	Code *Code  `json:"code,omitempty"`
}

// Code is the code of a file at head: whole, or in regions.
type Code struct {
	Whole   bool     `json:"whole"`
	Regions []Region `json:"regions"`
}

// Region is consecutive lines of a file, from line Start.
type Region struct {
	Start int      `json:"start"`
	Lines []string `json:"lines"`
}

// RelatedChange is another change request of the same cascade.
type RelatedChange struct {
	CR    Key    `json:"cr"`
	Title string `json:"title"`
	// WorkPackage is its work package, relative to the run's work
	// directory, when it is in the same run.
	WorkPackage string `json:"workPackage,omitempty"`
}

// PackageCascade is where the change request is in the cascade.
type PackageCascade struct {
	Ancestors []Ancestor `json:"ancestors"`
}

// Ancestor is a change request above another in the cascade.
type Ancestor struct {
	CR    Key    `json:"cr"`
	Title string `json:"title"`
}

// Parts tell how the work package is served.
type Parts struct {
	Total int `json:"total"`
	// ChangeRequestSignals are the ids of the signals served with part 1:
	// those of the change-request rules, and any not on a changed file.
	ChangeRequestSignals []string `json:"changeRequestSignals"`
	// Files are the paths of the files of each part.
	Files [][]string `json:"files"`
}

// PackageFileInput is a changed file and the language of the pack that
// reviews it; empty when no pack does.
type PackageFileInput struct {
	File     ChangedFile
	Language string
}

// WorkPackageInput is everything collected about a change request that its
// work package is built from.
type WorkPackageInput struct {
	RunID, URL, Title, Description string
	CR                             Key
	HeadSHA, BaseSHA, BaseRef      string
	// Files are the changed files, in diff order.
	Files   []PackageFileInput
	Ruleset Ruleset
	// Profile and ProfileReason, and Dimensions, are the context of the
	// change request with the reasons for the report.
	Profile, ProfileReason string
	Dimensions             map[string]DimensionChoice
	Criteria               RepoCriteria
	// FileSignals and ChangeRequestSignals are the signals of the rule
	// engine on each file and on the whole change request.
	FileSignals, ChangeRequestSignals []rules.Signal
	NotVerified                       []UnverifiedFile
	// Ancestors are the change requests above it, root first.
	Ancestors      []Ancestor
	RelatedChanges []RelatedChange
	JudgementPath  string
	// WholeFileLines, ContextLines and PartBudget tune the code and the
	// parts; 0 takes the defaults.
	WholeFileLines, ContextLines, PartBudget int
}

// BuildWorkPackage builds the work package of a change request and the
// review its judgement is validated and finalized against, from the same
// files, rules and signals. It reads nothing: everything comes from in.
//
// A file of at most WholeFileLines lines at head is included whole; a
// larger one in regions, each hunk with ContextLines lines around it,
// merged when they overlap or touch. A removed file and a file no pack
// covers have no code. A file the diff shows no line of is placed in the
// summary, with the reason. Files are served in parts of at most
// PartBudget bytes, in diff order, a file never split and a larger one
// alone.
func BuildWorkPackage(in WorkPackageInput) (WorkPackage, Review) {
	whole, context, budget := orDefault(in.WholeFileLines, DefaultWholeFileLines), orDefault(in.ContextLines, DefaultContextLines),
		orDefault(in.PartBudget, DefaultPartBudget)
	changed := make([]ChangedFile, 0, len(in.Files))
	for _, f := range in.Files {
		changed = append(changed, f.File)
	}
	signals, from := numberSignals(changed, in.FileSignals, in.ChangeRequestSignals)
	review := Review{CR: in.CR, HeadSHA: in.HeadSHA, Files: changed, Rules: in.Ruleset.ByID(), Signals: signals}

	wp := WorkPackage{
		Schema: 1, RunID: in.RunID, CR: in.CR, URL: in.URL, Title: in.Title, Description: in.Description,
		HeadSHA: in.HeadSHA, BaseSHA: in.BaseSHA, BaseRef: in.BaseRef,
		Criteria: PackageCriteria{
			Source: in.Criteria.Source, Profile: in.Profile, ProfileReason: in.ProfileReason, Packs: nonNil(in.Criteria.Packs),
			Dimensions: map[string]DimensionChoice{}, Files: nonNil(in.Criteria.Files), Ignored: append([]IgnoredItem{}, in.Ruleset.Ignored...),
		},
		Rules: []PackageRule{}, Files: []PackageFile{}, Signals: signals,
		NotVerified: append([]UnverifiedFile{}, in.NotVerified...), RelatedChanges: append([]RelatedChange{}, in.RelatedChanges...),
		Cascade: PackageCascade{Ancestors: append([]Ancestor{}, in.Ancestors...)}, JudgementPath: in.JudgementPath,
	}
	for id, d := range in.Dimensions {
		wp.Criteria.Dimensions[id] = d
	}
	for _, r := range in.Ruleset.Rules {
		wp.Rules = append(wp.Rules, PackageRule{ID: r.ID, Severity: r.Severity, Title: r.Title, Source: r.Source, Group: r.Group,
			NeedsJudgement: r.NeedsJudgement, Options: r.Options})
	}
	wp.Checklist = packageChecklist(in.Ruleset.Checklist())
	unverified := map[string]string{}
	for _, n := range in.NotVerified {
		if _, ok := unverified[n.Path]; !ok {
			unverified[n.Path] = n.Reason
		}
	}
	for _, f := range in.Files {
		wp.Files = append(wp.Files, packageFile(f, unverified[f.File.Diff.Path], whole, context))
	}
	wp.Parts = parts(wp.Files, budget)
	onFile := map[string]bool{}
	for _, f := range wp.Files {
		onFile[f.Path] = true
	}
	wp.Parts.ChangeRequestSignals = []string{}
	for i, s := range signals {
		if from[i] == 1 || !onFile[s.Path] {
			wp.Parts.ChangeRequestSignals = append(wp.Parts.ChangeRequestSignals, s.ID)
		}
	}
	if wp.Signals == nil {
		wp.Signals = []Signal{}
	}
	return wp, review
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func packageChecklist(cl Checklist) PackageChecklist {
	out := PackageChecklist{Sections: []ChecklistSection{}, Repository: []string{}}
	for _, s := range cl.Sections {
		sec := ChecklistSection{Heading: s.Heading}
		for _, r := range s.Rules {
			sec.Rules = append(sec.Rules, r.ID)
		}
		out.Sections = append(out.Sections, sec)
	}
	for _, r := range cl.Repository {
		out.Repository = append(out.Repository, r.ID)
	}
	return out
}

// packageFile lists one changed file; unverified is the reason it was not
// verified, if any.
func packageFile(in PackageFileInput, unverified string, whole, context int) PackageFile {
	d := in.File.Diff
	f := PackageFile{Path: d.Path, PreviousPath: d.OldPath, Status: d.Status, Language: in.Language, Placement: Inline}
	if !d.Visible() {
		f.Placement = Summary
		switch {
		case unverified != "":
			f.Reason = unverified
		case d.Status == FileRenamed:
			f.Reason = ReasonRenamedUnchanged
		case d.Status == FileCopied:
			f.Reason = ReasonCopiedUnchanged
		default:
			f.Reason = ReasonPatchUnavailable
		}
		return f
	}
	if d.Status == FileRemoved {
		return f
	}
	f.Diff = unifiedDiff(d.Hunks)
	if in.Language == "" || len(in.File.Head) == 0 {
		return f
	}
	head := in.File.Head
	if len(head) <= whole {
		f.Code = &Code{Whole: true, Regions: []Region{{Start: 1, Lines: append([]string{}, head...)}}}
		return f
	}
	f.Code = &Code{Regions: []Region{}}
	for _, span := range spans(d.Hunks, len(head), context) {
		f.Code.Regions = append(f.Code.Regions, Region{Start: span[0], Lines: append([]string{}, head[span[0]-1:span[1]]...)})
	}
	return f
}

// spans returns the line ranges, first and last, of each hunk at head
// with context lines around it, within the file and merged when they
// overlap or touch.
func spans(hunks []Hunk, lines, context int) [][2]int {
	var out [][2]int
	for _, h := range hunks {
		first, last := h.NewStart, h.NewStart+h.NewLines-1
		if h.NewLines == 0 {
			last = first
		}
		first, last = max(first-context, 1), min(last+context, lines)
		if first > last {
			continue
		}
		if n := len(out); n > 0 && first <= out[n-1][1]+1 {
			out[n-1][1] = max(out[n-1][1], last)
			continue
		}
		out = append(out, [2]int{first, last})
	}
	return out
}

// unifiedDiff writes hunks as a unified diff.
func unifiedDiff(hunks []Hunk) string {
	var b strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@%s\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section)
		for _, l := range h.Lines {
			b.WriteString(map[LineKind]string{LineAdded: "+", LineRemoved: "-", LineContext: " "}[l.Kind] + l.Text + "\n")
			if l.NoNewline {
				b.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return b.String()
}

// parts splits the files into parts of at most budget bytes of JSON, in
// order; a file larger than the budget has a part of its own.
func parts(files []PackageFile, budget int) Parts {
	p := Parts{Files: [][]string{{}}}
	used := 0
	for _, f := range files {
		data, _ := json.Marshal(f)
		cur := len(p.Files) - 1
		if len(p.Files[cur]) > 0 && used+len(data) > budget {
			p.Files = append(p.Files, []string{})
			cur, used = cur+1, 0
		}
		p.Files[cur] = append(p.Files[cur], f.Path)
		used += len(data)
	}
	p.Total = len(p.Files)
	return p
}

// PackagePart is what one part of a work package serves.
type PackagePart struct {
	Number, Total int
	Files         []PackageFile
	// Signals are those on the files of the part, without the
	// change-request signals.
	Signals []Signal
	// ChangeRequestSignals are served with part 1 only.
	ChangeRequestSignals []Signal
}

// Part returns part n, from 1, of the work package.
func (w WorkPackage) Part(n int) (PackagePart, bool) {
	if n < 1 || n > len(w.Parts.Files) {
		return PackagePart{}, false
	}
	p := PackagePart{Number: n, Total: w.Parts.Total}
	in := map[string]bool{}
	for _, path := range w.Parts.Files[n-1] {
		in[path] = true
	}
	for _, f := range w.Files {
		if in[f.Path] {
			p.Files = append(p.Files, f)
		}
	}
	cr := map[string]bool{}
	for _, id := range w.Parts.ChangeRequestSignals {
		cr[id] = true
	}
	for _, s := range w.Signals {
		switch {
		case cr[s.ID] && n == 1:
			p.ChangeRequestSignals = append(p.ChangeRequestSignals, s)
		case !cr[s.ID] && in[s.Path]:
			p.Signals = append(p.Signals, s)
		}
	}
	return p, true
}

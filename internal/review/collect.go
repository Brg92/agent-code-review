package review

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/lexer"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/state"
)

// analysis is what later change requests of the cascade need of one
// collected: its repository, its head and target branch, and its changed
// files as the change-request rules see them.
type analysis struct {
	project, head, target string
	files                 []rules.ChangedFile
}

func lower(k core.Key) string { return strings.ToLower(k.String()) }

// changed reads the changed files of a change request once.
func (c *collector) changed(ctx context.Context, k core.Key) ([]core.FileChange, error) {
	if files, ok := c.changes[lower(k)]; ok {
		return files, nil
	}
	c.d.Log.SetCR(k)
	files, err := c.d.Reader.ChangedFiles(ctx, k)
	if err != nil {
		if c.unreadable(k, err) {
			return nil, &unreadableError{key: k, err: err}
		}
		return nil, classify(err, c.host(), "could not read the changed files of "+k.String(), k.String()+", read the changed files")
	}
	c.changes[lower(k)] = files
	return files, nil
}

// criteria reads the criteria of the repository of m on its target branch.
// A configuration that is not valid does not stop the review: it is left
// out, with every invalid field, under the criteria ignored.
func (c *collector) criteria(ctx context.Context, m core.ChangeRequest) (core.RepoCriteria, error) {
	files, err := c.changed(ctx, m.Key)
	if err != nil {
		return core.RepoCriteria{}, err
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	c.d.Log.SetCR(m.Key)
	crit, err := core.ReadCriteria(ctx, c.d.Reader.Files(), core.CriteriaInput{Project: m.Key, Ref: m.TargetBranch, Rules: c.d.Packs.Rules, Changed: paths})
	var invalid *core.CriteriaError
	switch {
	case errors.As(err, &invalid):
		c.d.Log.NotVerified("read criteria "+invalid.File+" on "+m.TargetBranch, "criteria-invalid")
		return core.IgnoreInvalidCriteria(m.Key, m.TargetBranch, invalid), nil
	case err != nil && c.unreadable(m.Key, err):
		return core.RepoCriteria{}, &unreadableError{key: m.Key, err: err}
	case err != nil:
		return core.RepoCriteria{}, classify(err, c.host(), "could not read the criteria of "+m.Key.String()+" on "+m.TargetBranch, m.Key.String()+", read the criteria")
	}
	return crit, nil
}

// entryOf returns the entry of k.
func (c *collector) entryOf(k core.Key) (core.Entry, bool) {
	for _, e := range c.entries {
		if e.Status != core.Skipped && e.CR.Same(k) {
			return e, true
		}
	}
	return core.Entry{}, false
}

// ancestors returns the change requests above e: parent first, root last,
// for the rules, and root first for the work package.
func (c *collector) ancestors(e core.Entry) ([]rules.Ancestor, []core.Ancestor) {
	var up []rules.Ancestor
	var down []core.Ancestor
	for p := e.Parent; p != nil; {
		pe, ok := c.entryOf(*p)
		if !ok {
			break
		}
		if a := c.analyses[lower(pe.CR)]; a != nil {
			up = append(up, rules.Ancestor{Project: a.project, Head: a.head, Target: a.target, Files: a.files})
		}
		down = append([]core.Ancestor{{CR: pe.CR, Title: pe.Title}}, down...)
		p = pe.Parent
	}
	return up, down
}

// dirName is the directory of the change request e in the run.
func dirName(e core.Entry) (string, error) { return state.ChangeRequestDirName(e.CR, e.HeadSHA) }

// activate returns the packs that apply to a repository: those its
// criteria name, or those whose signs match its files. A name that is no
// loaded pack is left out, with an item under the criteria ignored.
func (c *collector) activate(crit *core.RepoCriteria, paths []string, repo packs.Reader) ([]*packs.Pack, error) {
	if len(crit.Packs) == 0 {
		return c.d.Packs.Activate(paths, repo)
	}
	return namedPacks(c.d.Packs, crit), nil
}

// namedPacks returns the packs a repository's criteria name, the highest
// priority first. A name that is no loaded pack is left out, with an item
// under the criteria ignored.
func namedPacks(set *packs.Set, crit *core.RepoCriteria) []*packs.Pack {
	var active []*packs.Pack
	for _, id := range crit.Packs {
		i := slices.IndexFunc(set.Packs, func(p *packs.Pack) bool { return p.ID == id })
		if i < 0 {
			item := core.IgnoredItem{Item: "`" + id + "` in packs", Reason: "no pack with this id is loaded"}
			crit.Ignored = append(crit.Ignored, item)
			crit.Notes = append(crit.Notes, item.Note())
			continue
		}
		if !slices.Contains(active, set.Packs[i]) {
			active = append(active, set.Packs[i])
		}
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].Priority > active[j].Priority })
	return active
}

// profileReason says why a profile was chosen, in a few words.
func profileReason(p packs.Profile) string {
	switch p.Reason {
	case packs.ProfileFromConfig:
		return "set in the repository criteria"
	case packs.ProfileFromCascade:
		return "cascade dependency of " + p.Detail
	case packs.ProfileFromPackDefault:
		return p.Detail + " default"
	case packs.ProfileUndetermined:
		return "no pack applies to this repository"
	}
	return p.Detail
}

// lines reads a file of a change request at ref and splits it; a file that
// is not there has no lines.
func (c *collector) lines(ctx context.Context, k core.Key, ref, p string) ([]string, error) {
	data, found, err := c.d.Reader.Files().Get(ctx, k, ref, p)
	if err != nil || !found {
		return nil, err
	}
	return core.SplitLines(string(data)), nil
}

// collectOne collects one change request: it reads what the platform
// holds, runs the rules and writes its work package.
func (c *collector) collectOne(ctx context.Context, e core.Entry, crit *core.RepoCriteria) (Package, error) {
	m := c.metas[lower(e.CR)]
	k := e.CR
	c.d.Log.SetCR(k)
	changes, err := c.changed(ctx, k)
	if err != nil {
		return Package{}, err
	}
	if crit == nil {
		read, err := c.criteria(ctx, m)
		if err != nil {
			return Package{}, err
		}
		crit = &read
	}
	c.d.Log.SetCR(k)
	ci := base.ReadCI(ctx, c.d.Reader, k, m.HeadSHA)
	if ci.State == core.CIUnknown {
		c.d.Log.NotVerified("ci "+m.HeadSHA, ci.Reason)
	}

	var changedPaths []string
	for _, f := range changes {
		if f.Status != core.FileRemoved {
			changedPaths = append(changedPaths, f.Path)
		}
	}
	tree := c.d.Reader.Tree(ctx, k, m.HeadSHA)
	paths, err := tree.List("")
	treeErr := err
	if err != nil {
		c.d.Log.NotVerified("tree "+m.HeadSHA, base.ReasonOf(err))
		paths = changedPaths
	}
	repo := base.Repo(ctx, c.d.Reader.Files(), k, m.HeadSHA)
	packsNamed := len(crit.Packs) > 0
	active, err := c.activate(crit, paths, repo)
	if err != nil {
		return Package{}, classify(err, c.host(), "could not choose the packs of "+k.String(), k.String()+", detect the packs")
	}
	profile, err := packs.ResolveProfile(active, packs.ProfileInput{Configured: crit.Profile, DependentCR: parentKey(e)}, paths, repo)
	if err != nil {
		return Package{}, classify(err, c.host(), "could not choose the profile of "+k.String(), k.String()+", detect the profile")
	}
	if profile.Name == "" {
		// No pack applies, so the packs name no profile: the contract
		// has two, and the core rules apply to either.
		profile.Name = "app"
	}
	dims, err := packs.ResolveDimensions(active, crit.Dimensions, paths, repo)
	if err != nil {
		return Package{}, classify(err, c.host(), "could not read the dimensions of "+k.String(), k.String()+", detect the dimensions")
	}
	choices := map[string]core.DimensionChoice{}
	for _, d := range dims {
		reason := d.Detail
		if d.Reason == packs.DimensionFromConfig {
			reason = "set in the repository criteria"
		}
		choices[d.ID] = core.DimensionChoice{Value: d.Value, Reason: reason}
	}
	crit.Packs = make([]string, 0, len(active))
	for _, p := range active {
		crit.Packs = append(crit.Packs, p.ID)
	}
	rs := core.EffectiveRules(core.RulesetInput{Set: c.d.Packs, Active: active, Profile: profile.Name, Dimensions: dims, Criteria: *crit})

	var (
		files       []core.PackageFileInput
		fileSignals []rules.Signal
		notVerified []core.UnverifiedFile
		engineFiles []rules.ChangedFile
	)
	for _, fc := range changes {
		if fc.Status == core.FileUnchanged {
			continue
		}
		diff, err := fc.Diff()
		if err != nil {
			// A patch that cannot be read is a patch the platform did not give.
			diff, _ = core.NewFileDiff(fc.Path, fc.OldPath, fc.Status, "")
			notVerified = append(notVerified, core.UnverifiedFile{Path: fc.Path, Reason: core.ReasonPatchUnavailable, Detail: "the patch could not be read"})
		}
		f := core.ChangedFile{Diff: diff}
		in := core.PackageFileInput{File: f}
		a := c.d.Packs.Assign(active, fc.Path)
		var assigned rules.File
		switch {
		case a.NotVerified != nil:
			nv := core.UnverifiedFile{Path: fc.Path, Reason: a.NotVerified.Reason, Detail: a.NotVerified.Detail}
			switch {
			case nv.Reason == packs.ReasonUnsupportedLanguage && (fc.Binary || fc.Missing == core.PatchBinary):
				nv = core.UnverifiedFile{Path: fc.Path, Reason: "binary"}
			case nv.Reason == packs.ReasonUnsupportedLanguage && treeErr != nil && !packsNamed:
				nv.Detail += "; the repository tree could not be read (" + base.ReasonOf(treeErr) + "), so the packs were detected from the changed files only"
			}
			notVerified = append(notVerified, nv)
		case !diff.Visible() && diff.Status != core.FileRenamed:
			if fc.Missing == core.PatchBinary || fc.Binary {
				notVerified = append(notVerified, core.UnverifiedFile{Path: fc.Path, Reason: "binary"})
			} else if !hasReason(notVerified, fc.Path) {
				notVerified = append(notVerified, core.UnverifiedFile{Path: fc.Path, Reason: core.ReasonPatchUnavailable})
			}
			in.Language = a.Language.ID
		default:
			in.Language = a.Language.ID
			assigned = a.File("", nil)
			var readErr error
			if diff.Status != core.FileRemoved && diff.Visible() {
				f.Head, readErr = c.lines(ctx, k, m.HeadSHA, fc.Path)
			}
			if readErr == nil && diff.Status != core.FileAdded && diff.Visible() {
				old := fc.Path
				if fc.OldPath != "" {
					old = fc.OldPath
				}
				f.Base, readErr = c.lines(ctx, k, m.BaseSHA, old)
			}
			if readErr != nil {
				// The file is left out of the rules, and listed.
				c.d.Log.NotVerified("read "+fc.Path, core.ReasonContentUnavailable)
				notVerified = append(notVerified, core.UnverifiedFile{Path: fc.Path, Reason: core.ReasonContentUnavailable, Detail: whyOf(readErr)})
				f.Head, f.Base, in.Language, assigned = nil, nil, "", rules.File{}
			}
			if len(f.Head) > 0 {
				res := rules.Run(rs.ForFile(a.Context(profile.Name, dims)), a.File(strings.Join(f.Head, "\n")+"\n", diff.AddedLines()), rules.Options{})
				fileSignals = append(fileSignals, res.Signals...)
				for _, n := range res.NotVerified {
					notVerified = append(notVerified, core.UnverifiedFile{Path: n.Path, Reason: n.Reason, Detail: n.Detail})
				}
			}
			in.File = f
		}
		files = append(files, in)
		engineFiles = append(engineFiles, f.RulesFile(assigned))
	}

	up, down := c.ancestors(e)
	res := rules.RunChangeRequest(rs.PackRules(), rules.ChangeRequest{
		Files: engineFiles, Tree: tree, Variants: crit.Variants, Project: k.ProjectKey(), Head: m.HeadSHA, Target: m.TargetBranch,
		Search: c.d.Reader.Search(), Ancestors: up,
		TableOf: func(p string) *lexer.Table {
			if a := c.d.Packs.Assign(active, p); a.NotVerified == nil {
				return a.Language.Table
			}
			return nil
		},
	})
	for _, n := range res.NotVerified {
		c.d.Log.NotVerified(n.Reason+" "+n.Path, n.Reason)
		notVerified = append(notVerified, core.UnverifiedFile{Path: n.Path, Reason: n.Reason, Detail: n.Detail})
	}

	dir, err := state.CreateChangeRequestDir(c.runDir, k, m.HeadSHA)
	if err != nil {
		return Package{}, err
	}
	c.written = append(c.written, dir)
	name := filepath.Base(dir)
	var related []core.RelatedChange
	for _, o := range c.entries {
		if o.Status == core.Skipped || o.CR.Same(k) {
			continue
		}
		other, err := dirName(o)
		if err != nil {
			return Package{}, err
		}
		related = append(related, core.RelatedChange{CR: o.CR, Title: o.Title, WorkPackage: path.Join(other, state.WorkPackageFile)})
	}
	wp, built, err := core.BuildWorkPackage(core.WorkPackageInput{
		RunID: c.runID, CR: k, URL: k.URL(), Title: m.Title, Description: m.Description,
		HeadSHA: m.HeadSHA, BaseSHA: m.BaseSHA, BaseRef: m.TargetBranch,
		Files: files, Ruleset: rs, Profile: profile.Name, ProfileReason: profileReason(profile), Dimensions: choices, Criteria: *crit, CI: ci,
		FileSignals: fileSignals, ChangeRequestSignals: res.Signals, NotVerified: notVerified, Ancestors: down, RelatedChanges: related,
		JudgementPath: path.Join(name, state.JudgementFile),
	})
	if err != nil {
		var pe *core.PathError
		f := classify(err, c.host(), "could not build the work package of "+k.String(), k.String()+", build the work package")
		if errors.As(err, &pe) {
			f.Hint = "the change request is not reviewed: a path with a control character could forge a line of the brief; rename the file"
		}
		return Package{}, f
	}
	data, err := core.EncodeWorkPackage(wp)
	if err != nil {
		return Package{}, err
	}
	file := filepath.Join(dir, state.WorkPackageFile)
	if err := writeFile(file, data); err != nil {
		return Package{}, err
	}
	c.analyses[lower(k)] = &analysis{project: k.ProjectKey(), head: m.HeadSHA, target: m.TargetBranch, files: engineFiles}
	return Package{CR: k, Title: m.Title, Dir: dir, File: file, Judgement: filepath.Join(dir, state.JudgementFile), Parts: wp.Parts.Total, NotVerified: len(wp.NotVerified), review: built}, nil
}

// parentKey is the key of the change request that lists e, or "".
func parentKey(e core.Entry) string {
	if e.Parent == nil {
		return ""
	}
	return e.Parent.String()
}

// hasReason reports whether a file is already listed as not verified.
func hasReason(list []core.UnverifiedFile, p string) bool {
	return slices.ContainsFunc(list, func(u core.UnverifiedFile) bool { return u.Path == p })
}

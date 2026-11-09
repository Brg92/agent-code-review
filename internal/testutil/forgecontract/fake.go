package forgecontract

import (
	"context"
	"slices"
	"strings"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// Call names a read of the fake: an operation on a project, by project
// key, at a ref ("" for change requests and notes, the head for CI).
type Call struct {
	Op      Op
	Project string
	Ref     string
}

// Fake is an in-memory provider. Everything it answers comes from its
// maps; Fail makes a call fail with the given error instead.
type Fake struct {
	Caps base.Capabilities
	// Items are the change requests and issues, by key.
	Items map[core.Key]cascade.Item
	// DefaultBranch is the default branch of each project, by project key.
	DefaultBranch map[string]string
	// Repos are the files of a project at a ref, by "<project key>@<ref>",
	// then by path.
	Repos map[string]map[string]string
	// CIs are the CI states, by "<project key>@<head>".
	CIs map[string]core.CI
	// Notes are the notes on each change request, by key.
	NoteSets map[core.Key][]core.ReviewNote
	Fail     map[Call]error
}

// Capabilities implements base.Reader.
func (f *Fake) Capabilities() base.Capabilities { return f.Caps }

// Changes implements base.Reader.
func (f *Fake) Changes() cascade.Source { return changes{f} }

// Files implements base.Reader.
func (f *Fake) Files() core.Files { return files{f} }

// Search implements base.Reader.
func (f *Fake) Search() rules.Search { return search{f} }

// Tree implements base.Reader.
func (f *Fake) Tree(_ context.Context, project core.Key, ref string) rules.Tree {
	return tree{f, project.ProjectKey(), ref}
}

// CI implements base.Reader.
func (f *Fake) CI(_ context.Context, project core.Key, head string) (core.CI, error) {
	pk := project.ProjectKey()
	if err := f.Fail[Call{OpCI, pk, head}]; err != nil {
		return core.CI{}, err
	}
	ci, ok := f.CIs[pk+"@"+head]
	if !ok {
		return core.CI{}, &base.Error{Kind: base.ErrNotFound, Op: "ci " + pk + "@" + head}
	}
	return ci, nil
}

// Notes implements base.Reader.
func (f *Fake) Notes(_ context.Context, cr core.Key) ([]core.ReviewNote, error) {
	if err := f.Fail[Call{OpNotes, cr.ProjectKey(), ""}]; err != nil {
		return nil, err
	}
	if f.Items[cr].Kind != cascade.KindChangeRequest {
		return nil, &base.Error{Kind: base.ErrNotFound, Op: "notes " + cr.String()}
	}
	return f.NoteSets[cr], nil
}

// repo returns the files of a project at ref, or ErrNotFound.
func (f *Fake) repo(op Op, project, ref string) (map[string]string, error) {
	if err := f.Fail[Call{op, project, ref}]; err != nil {
		return nil, err
	}
	repo, ok := f.Repos[project+"@"+ref]
	if !ok {
		return nil, &base.Error{Kind: base.ErrNotFound, Op: string(op) + " " + project + "@" + ref}
	}
	return repo, nil
}

// paths returns the sorted paths of repo that start with prefix.
func paths(repo map[string]string, prefix string) []string {
	var out []string
	for p := range repo {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

type changes struct{ f *Fake }

func (c changes) Get(_ context.Context, key core.Key) (cascade.Item, error) {
	if err := c.f.Fail[Call{OpChange, key.ProjectKey(), ""}]; err != nil {
		return cascade.Item{}, err
	}
	return c.f.Items[key], nil
}

type files struct{ f *Fake }

func (fs files) Get(_ context.Context, project core.Key, ref, path string) ([]byte, bool, error) {
	repo, err := fs.f.repo(OpFilesGet, project.ProjectKey(), ref)
	if err != nil {
		return nil, false, err
	}
	content, ok := repo[path]
	if !ok {
		return nil, false, nil
	}
	return []byte(content), true, nil
}

func (fs files) List(_ context.Context, project core.Key, ref, dir string) ([]core.DirEntry, error) {
	repo, err := fs.f.repo(OpFilesList, project.ProjectKey(), ref)
	if err != nil {
		return nil, err
	}
	prefix := ""
	if dir != "" {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}
	var out []core.DirEntry
	for _, p := range paths(repo, prefix) {
		name, rest, sub := strings.Cut(p[len(prefix):], "/")
		e := core.DirEntry{Name: name, Dir: sub && rest != ""}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out, nil
}

type tree struct {
	f            *Fake
	project, ref string
}

func (t tree) List(prefix string) ([]string, error) {
	repo, err := t.f.repo(OpTree, t.project, t.ref)
	if err != nil {
		return nil, err
	}
	return paths(repo, prefix), nil
}

type search struct{ f *Fake }

func (s search) Search(project, ref, term string) ([]rules.Match, error) {
	if !s.f.Caps.SearchOn(ref, s.f.DefaultBranch[project]) {
		return nil, &base.Error{Kind: base.ErrSearchUnavailable, Op: "search " + project + "@" + ref}
	}
	repo, err := s.f.repo(OpSearch, project, ref)
	if err != nil {
		return nil, err
	}
	var out []rules.Match
	for _, p := range paths(repo, "") {
		for i, line := range strings.Split(repo[p], "\n") {
			if strings.Contains(line, term) {
				out = append(out, rules.Match{Path: p, Line: i + 1, Text: line})
			}
		}
	}
	return out, nil
}

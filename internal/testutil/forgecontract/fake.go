package forgecontract

import (
	"context"
	"fmt"
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
	// ChangeRequests are the metadata of the change requests, by key; an
	// issue or an unknown key is not found.
	ChangeRequests map[core.Key]core.ChangeRequest
	// FileSets are the changed files of each change request, by key.
	FileSets map[core.Key][]core.FileChange
	// DefaultBranch is the default branch of each project, by project key.
	DefaultBranch map[string]string
	// Repos are the files of a project at a ref, by "<project key>@<ref>",
	// then by path.
	Repos map[string]map[string]string
	// CIs are the CI states, by "<project key>@<head>".
	CIs map[string]core.CI
	// Notes are the notes on each change request, by key.
	NoteSets map[core.Key][]core.ReviewNote
	// Login is the login of the token's account.
	Login string
	// IssueSets are the issues of each project, by project key; Labels the
	// labels of each project and Assignees who can be assigned in it.
	IssueSets map[string][]core.Issue
	Labels    map[string][]string
	Assignees map[string][]string
	// ThreadSets are the review threads of each change request.
	ThreadSets map[core.Key][]core.ReviewThread
	// RefusedLines are the positions, "<path>:<line>", of inline comments
	// the fake refuses; Unnamed makes it refuse without saying which.
	RefusedLines map[string]bool
	Unnamed      bool
	Fail         map[Call]error
}

// Capabilities implements base.Reader.
func (f *Fake) Capabilities() base.Capabilities { return f.Caps }

// Changes implements base.Reader.
func (f *Fake) Changes() cascade.Source { return changes{f} }

// ChangeRequest implements base.Reader.
func (f *Fake) ChangeRequest(_ context.Context, key core.Key) (core.ChangeRequest, error) {
	if err := f.Fail[Call{OpChangeRequest, key.ProjectKey(), ""}]; err != nil {
		return core.ChangeRequest{}, err
	}
	cr, ok := f.ChangeRequests[key]
	if !ok {
		return core.ChangeRequest{}, &base.Error{Kind: base.ErrNotFound, Op: "change request " + key.String()}
	}
	return cr, nil
}

// ChangedFiles implements base.Reader.
func (f *Fake) ChangedFiles(_ context.Context, key core.Key) ([]core.FileChange, error) {
	if err := f.Fail[Call{OpChangedFiles, key.ProjectKey(), ""}]; err != nil {
		return nil, err
	}
	if _, ok := f.ChangeRequests[key]; !ok {
		return nil, &base.Error{Kind: base.ErrNotFound, Op: "changed files " + key.String()}
	}
	return slices.Clone(f.FileSets[key]), nil
}

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

// Self implements base.Reader.
func (f *Fake) Self(context.Context) (string, error) {
	if err := f.Fail[Call{OpSelf, "", ""}]; err != nil {
		return "", err
	}
	return f.Login, nil
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

// Issues implements base.Reader.
func (f *Fake) Issues(_ context.Context, project core.Key, _ string) ([]core.Issue, error) {
	if err := f.Fail[Call{OpIssues, project.ProjectKey(), ""}]; err != nil {
		return nil, err
	}
	if _, ok := f.DefaultBranch[project.ProjectKey()]; !ok {
		return nil, &base.Error{Kind: base.ErrNotFound, Op: "issues " + project.ProjectKey()}
	}
	return append([]core.Issue{}, f.IssueSets[project.ProjectKey()]...), nil
}

// HasLabel implements base.Reader.
func (f *Fake) HasLabel(_ context.Context, project core.Key, name string) (bool, error) {
	if err := f.Fail[Call{OpHasLabel, project.ProjectKey(), ""}]; err != nil {
		return false, err
	}
	if _, ok := f.DefaultBranch[project.ProjectKey()]; !ok {
		return false, &base.Error{Kind: base.ErrNotFound, Op: "label " + project.ProjectKey()}
	}
	return slices.Contains(f.Labels[project.ProjectKey()], name), nil
}

// Assignable implements base.Reader.
func (f *Fake) Assignable(_ context.Context, project core.Key, login string) (bool, error) {
	if err := f.Fail[Call{OpAssignable, project.ProjectKey(), ""}]; err != nil {
		return false, err
	}
	if _, ok := f.DefaultBranch[project.ProjectKey()]; !ok {
		return false, &base.Error{Kind: base.ErrNotFound, Op: "assignee " + project.ProjectKey()}
	}
	return slices.Contains(f.Assignees[project.ProjectKey()], login), nil
}

// ReviewThreads implements base.Reader.
func (f *Fake) ReviewThreads(_ context.Context, cr core.Key) ([]core.ReviewThread, error) {
	if err := f.Fail[Call{OpThreads, cr.ProjectKey(), ""}]; err != nil {
		return nil, err
	}
	if _, ok := f.ChangeRequests[cr]; !ok {
		return nil, &base.Error{Kind: base.ErrNotFound, Op: "review threads " + cr.String()}
	}
	return append([]core.ReviewThread{}, f.ThreadSets[cr]...), nil
}

var _ base.ForgeProvider = (*Fake)(nil)

// writable fails like a platform does for a write to a change request that
// is not there, or that the token cannot write to.
func (f *Fake) writable(op Op, what string, cr core.Key) error {
	if err := f.Fail[Call{op, cr.ProjectKey(), ""}]; err != nil {
		return err
	}
	if _, ok := f.ChangeRequests[cr]; !ok {
		return &base.Error{Kind: base.ErrNotFound, Op: what + " " + cr.String()}
	}
	return nil
}

func (f *Fake) addNote(cr core.Key, body string) {
	if f.NoteSets == nil {
		f.NoteSets = map[core.Key][]core.ReviewNote{}
	}
	f.NoteSets[cr] = append(f.NoteSets[cr], core.ReviewNote{Author: f.Login, Body: body, Trusted: true})
}

// CreateReview implements base.Writer: nothing is posted if any comment is
// refused.
func (f *Fake) CreateReview(_ context.Context, cr core.Key, _ string, comments []base.ReviewComment) error {
	if err := f.writable(OpCreateReview, "review", cr); err != nil {
		return err
	}
	var refused []int
	for i, c := range comments {
		if f.RefusedLines[fmt.Sprintf("%s:%d", c.Path, c.Line)] {
			refused = append(refused, i)
		}
	}
	if len(refused) > 0 {
		if f.Unnamed {
			refused = nil
		}
		return &base.Error{Kind: base.ErrRejected, Op: "review " + cr.String(), Rejected: refused}
	}
	for _, c := range comments {
		f.addNote(cr, c.Body)
	}
	return nil
}

// CreateComment implements base.Writer.
func (f *Fake) CreateComment(_ context.Context, cr core.Key, body string) error {
	if err := f.writable(OpCreateComment, "comment", cr); err != nil {
		return err
	}
	f.addNote(cr, body)
	return nil
}

// CreateIssue implements base.Writer.
func (f *Fake) CreateIssue(_ context.Context, project core.Key, issue base.NewIssue) (int, error) {
	pk := project.ProjectKey()
	if err := f.Fail[Call{OpCreateIssue, pk, ""}]; err != nil {
		return 0, err
	}
	if _, ok := f.DefaultBranch[pk]; !ok {
		return 0, &base.Error{Kind: base.ErrNotFound, Op: "issue " + pk}
	}
	if f.IssueSets == nil {
		f.IssueSets = map[string][]core.Issue{}
	}
	n := len(f.IssueSets[pk]) + 1
	f.IssueSets[pk] = append(f.IssueSets[pk], core.Issue{Number: n, Open: true, Author: f.Login, Trusted: true, Title: issue.Title, Body: issue.Body})
	return n, nil
}

// UpdateIssue implements base.Writer.
func (f *Fake) UpdateIssue(_ context.Context, project core.Key, number int, title, body string) error {
	pk := project.ProjectKey()
	if err := f.Fail[Call{OpUpdateIssue, pk, ""}]; err != nil {
		return err
	}
	for i, is := range f.IssueSets[pk] {
		if is.Number == number {
			f.IssueSets[pk][i].Title, f.IssueSets[pk][i].Body = title, body
			return nil
		}
	}
	return &base.Error{Kind: base.ErrNotFound, Op: fmt.Sprintf("issue %s#%d", pk, number)}
}

// CreateLabel implements base.Writer.
func (f *Fake) CreateLabel(_ context.Context, project core.Key, label core.Label) error {
	pk := project.ProjectKey()
	if err := f.Fail[Call{OpCreateLabel, pk, ""}]; err != nil {
		return err
	}
	if _, ok := f.DefaultBranch[pk]; !ok {
		return &base.Error{Kind: base.ErrNotFound, Op: "label " + pk}
	}
	if f.Labels == nil {
		f.Labels = map[string][]string{}
	}
	if !slices.Contains(f.Labels[pk], label.Name) {
		f.Labels[pk] = append(f.Labels[pk], label.Name)
	}
	return nil
}

// UpdateDescription implements base.Writer.
func (f *Fake) UpdateDescription(_ context.Context, cr core.Key, description string) error {
	if err := f.writable(OpUpdateDescription, "description", cr); err != nil {
		return err
	}
	meta := f.ChangeRequests[cr]
	meta.Description = description
	f.ChangeRequests[cr] = meta
	if item, ok := f.Items[cr]; ok {
		item.Description = description
		f.Items[cr] = item
	}
	return nil
}

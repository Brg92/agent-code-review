// Package forgecontract is the contract suite every provider of
// internal/forges must pass, and an in-memory provider that passes it. It
// is test-only.
//
// Run checks a base.Reader against a Fixture, the answers the provider is
// expected to give on a project prepared for it: each read, the typed
// errors of failed reads (never an empty result in their place), and the
// consistency of the answers with the provider's capabilities.
package forgecontract

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// Op is a read operation of a Reader.
type Op string

// The operations.
const (
	OpChange    Op = "change"
	OpFilesGet  Op = "files-get"
	OpFilesList Op = "files-list"
	OpTree      Op = "tree"
	OpSearch    Op = "search"
	OpCI        Op = "ci"
	OpNotes     Op = "notes"
)

// Fixture is what a provider must answer.
type Fixture struct {
	Capabilities base.Capabilities

	// ChangeRequest reads as Item; Issue as an issue; Missing as nothing.
	ChangeRequest core.Key
	Item          cascade.Item
	Issue         core.Key
	Missing       core.Key

	// Project has the same files at DefaultBranch and at OtherRef.
	Project       core.Key
	DefaultBranch string
	OtherRef      string
	// Path holds Content; MissingPath and MissingDir do not exist.
	Path        string
	Content     string
	MissingPath string
	// Dir lists as Entries.
	Dir        string
	Entries    []core.DirEntry
	MissingDir string
	// TreePrefix lists as TreePaths.
	TreePrefix string
	TreePaths  []string
	// Term is found at Matches wherever the search can run.
	Term    string
	Matches []At

	// Head of Project has CI.
	Head string
	CI   core.CI

	// The notes on ChangeRequest carry Fingerprints once the notes of
	// Author, who wrote ChangeRequest, are left out.
	Author       string
	Fingerprints []string

	// Failures are reads that must fail with their kind.
	Failures []Failure
}

// At is where a search found the term. Providers need not give the text
// of the line, so it is not compared.
type At struct {
	Path string
	Line int
}

// Failure is a read that must fail: Op on Project at Ref.
type Failure struct {
	Op      Op
	Project core.Key
	Ref     string
	Kind    base.Kind
}

// Run checks r against fx. It reports through t.Errorf only, so that a
// test can check that the suite catches a broken provider.
func Run(t testing.TB, r base.Reader, fx Fixture) {
	t.Helper()
	ctx := context.Background()
	checkFixture(t, fx)
	checkCapabilities(t, r, fx)
	checkChanges(t, ctx, r, fx)
	checkFiles(t, ctx, r, fx)
	checkTree(t, ctx, r, fx)
	checkSearch(t, r, fx)
	checkCI(t, ctx, r, fx)
	checkNotes(t, ctx, r, fx)
	for _, f := range fx.Failures {
		checkFailure(t, ctx, r, fx, f)
	}
}

// checkFixture rejects a fixture whose expected answers are empty, which
// could not tell an answer from a swallowed failure.
func checkFixture(t testing.TB, fx Fixture) {
	t.Helper()
	switch {
	case fx.Item.Kind != cascade.KindChangeRequest:
		t.Errorf("fixture: Item must be a change request")
	case fx.Content == "" || len(fx.Entries) == 0 || len(fx.TreePaths) == 0 || len(fx.Matches) == 0:
		t.Errorf("fixture: Content, Entries, TreePaths and Matches must not be empty")
	case fx.OtherRef == fx.DefaultBranch:
		t.Errorf("fixture: OtherRef must differ from DefaultBranch")
	case len(fx.Fingerprints) == 0:
		t.Errorf("fixture: Fingerprints must not be empty")
	}
	if fx.Capabilities.TreeMayTruncate && !slices.ContainsFunc(fx.Failures, func(f Failure) bool { return f.Kind == base.ErrTreeTruncated }) {
		t.Errorf("fixture: the provider may truncate trees, so a truncated tree must be among the failures")
	}
}

func checkCapabilities(t testing.TB, r base.Reader, fx Fixture) {
	t.Helper()
	c := r.Capabilities()
	if err := c.Validate(); err != nil {
		t.Errorf("capabilities: %v", err)
	}
	if c != fx.Capabilities {
		t.Errorf("capabilities = %+v, want %+v", c, fx.Capabilities)
	}
}

func checkChanges(t testing.TB, ctx context.Context, r base.Reader, fx Fixture) {
	t.Helper()
	tests := []struct {
		key  core.Key
		want cascade.Kind
	}{
		{fx.ChangeRequest, cascade.KindChangeRequest},
		{fx.Issue, cascade.KindIssue},
		{fx.Missing, cascade.KindNotFound},
	}
	for _, tt := range tests {
		item, err := r.Changes().Get(ctx, tt.key)
		if err != nil {
			t.Errorf("Changes().Get(%s) error = %v, want an item of kind %v", tt.key, err, tt.want)
			continue
		}
		if item.Kind != tt.want {
			t.Errorf("Changes().Get(%s) kind = %v, want %v", tt.key, item.Kind, tt.want)
		}
		if tt.want == cascade.KindChangeRequest && item != fx.Item {
			t.Errorf("Changes().Get(%s) = %+v, want %+v", tt.key, item, fx.Item)
		}
	}
}

func checkFiles(t testing.TB, ctx context.Context, r base.Reader, fx Fixture) {
	t.Helper()
	files := r.Files()
	content, found, err := files.Get(ctx, fx.Project, fx.DefaultBranch, fx.Path)
	if err != nil || !found || string(content) != fx.Content {
		t.Errorf("Files().Get(%s) = %q, %v, %v, want the content", fx.Path, content, found, err)
	}
	content, found, err = files.Get(ctx, fx.Project, fx.DefaultBranch, fx.MissingPath)
	if err != nil || found || content != nil {
		t.Errorf("Files().Get(%s) = %q, %v, %v, want not found and no error", fx.MissingPath, content, found, err)
	}
	entries, err := files.List(ctx, fx.Project, fx.DefaultBranch, fx.Dir)
	if err != nil || !reflect.DeepEqual(entries, fx.Entries) {
		t.Errorf("Files().List(%s) = %+v, %v, want %+v", fx.Dir, entries, err, fx.Entries)
	}
	entries, err = files.List(ctx, fx.Project, fx.DefaultBranch, fx.MissingDir)
	if err != nil || len(entries) != 0 {
		t.Errorf("Files().List(%s) = %+v, %v, want no entries and no error", fx.MissingDir, entries, err)
	}

	repo := base.Repo(ctx, files, fx.Project, fx.DefaultBranch)
	if data, err := repo.ReadFile(fx.Path); err != nil || string(data) != fx.Content {
		t.Errorf("Repo().ReadFile(%s) = %q, %v, want the content", fx.Path, data, err)
	}
	if data, err := repo.ReadFile(fx.MissingPath); !errors.Is(err, base.ErrNotFound) || data != nil {
		t.Errorf("Repo().ReadFile(%s) = %q, %v, want ErrNotFound", fx.MissingPath, data, err)
	}
}

func checkTree(t testing.TB, ctx context.Context, r base.Reader, fx Fixture) {
	t.Helper()
	tree := r.Tree(ctx, fx.Project, fx.DefaultBranch)
	got, err := tree.List(fx.TreePrefix)
	if err != nil || !reflect.DeepEqual(got, fx.TreePaths) {
		t.Errorf("Tree().List(%q) = %v, %v, want %v", fx.TreePrefix, got, err, fx.TreePaths)
	}
	all, err := tree.List("")
	if err != nil {
		t.Errorf("Tree().List(\"\") error = %v", err)
	}
	for _, p := range fx.TreePaths {
		if !slices.Contains(all, p) {
			t.Errorf("Tree().List(\"\") misses %s", p)
		}
	}
	for _, p := range got {
		if !strings.HasPrefix(p, fx.TreePrefix) {
			t.Errorf("Tree().List(%q) returns %s, outside the prefix", fx.TreePrefix, p)
		}
	}
}

func checkSearch(t testing.TB, r base.Reader, fx Fixture) {
	t.Helper()
	caps := r.Capabilities()
	for _, ref := range []string{fx.DefaultBranch, fx.OtherRef} {
		got, err := r.Search().Search(fx.Project.ProjectKey(), ref, fx.Term)
		if !caps.SearchOn(ref, fx.DefaultBranch) {
			if !errors.Is(err, base.ErrSearchUnavailable) || got != nil {
				t.Errorf("Search at %s = %v, %v, want ErrSearchUnavailable: search scope is %q", ref, got, err, caps.Search)
			}
			continue
		}
		if err != nil {
			t.Errorf("Search at %s error = %v", ref, err)
			continue
		}
		var at []At
		for _, m := range got {
			at = append(at, At{m.Path, m.Line})
		}
		if !reflect.DeepEqual(at, fx.Matches) {
			t.Errorf("Search at %s = %v, want %v", ref, at, fx.Matches)
		}
	}
}

func checkCI(t testing.TB, ctx context.Context, r base.Reader, fx Fixture) {
	t.Helper()
	ci, err := r.CI(ctx, fx.Project, fx.Head)
	if err != nil || !reflect.DeepEqual(ci, fx.CI) {
		t.Errorf("CI(%s) = %+v, %v, want %+v", fx.Head, ci, err, fx.CI)
	}
	if ci.State == core.CIUnknown {
		t.Errorf("CI(%s) returned an unknown state: a read that fails is an error", fx.Head)
	}
	want := fx.CI
	if !r.Capabilities().CI.Readable() {
		want = core.CI{State: core.CIUnknown, Reason: "error", Detail: "the platform reports no CI", Checks: []core.Check{}}
	}
	if got := base.ReadCI(ctx, r, fx.Project, fx.Head); !reflect.DeepEqual(got, want) {
		t.Errorf("ReadCI(%s) = %+v, want %+v", fx.Head, got, want)
	}
}

func checkNotes(t testing.TB, ctx context.Context, r base.Reader, fx Fixture) {
	t.Helper()
	notes, err := r.Notes(ctx, fx.ChangeRequest)
	if err != nil {
		t.Errorf("Notes(%s) error = %v", fx.ChangeRequest, err)
		return
	}
	var got []string
	for _, fp := range core.ParseFingerprints(notes, fx.Author) {
		got = append(got, fp.Value)
	}
	if !reflect.DeepEqual(got, fx.Fingerprints) {
		t.Errorf("fingerprints in Notes(%s) = %v, want %v", fx.ChangeRequest, got, fx.Fingerprints)
	}
}

// checkFailure checks that a read fails with its kind and returns nothing.
func checkFailure(t testing.TB, ctx context.Context, r base.Reader, fx Fixture, f Failure) {
	t.Helper()
	caps := r.Capabilities()
	switch {
	case f.Kind == base.ErrTreeTruncated && (f.Op != OpTree || !caps.TreeMayTruncate):
		t.Errorf("failure %s: a truncated tree needs the tree operation and TreeMayTruncate", f.Op)
		return
	case f.Kind == base.ErrSearchUnavailable && f.Op != OpSearch:
		t.Errorf("failure %s: search unavailable is a search failure", f.Op)
		return
	}

	var err error
	var empty bool
	switch f.Op {
	case OpChange:
		var item cascade.Item
		item, err = r.Changes().Get(ctx, f.Project)
		empty = item == cascade.Item{}
	case OpFilesGet:
		var content []byte
		var found bool
		content, found, err = r.Files().Get(ctx, f.Project, f.Ref, fx.Path)
		empty = content == nil && !found
	case OpFilesList:
		var entries []core.DirEntry
		entries, err = r.Files().List(ctx, f.Project, f.Ref, fx.Dir)
		empty = entries == nil
	case OpTree:
		var paths []string
		paths, err = r.Tree(ctx, f.Project, f.Ref).List(fx.TreePrefix)
		empty = paths == nil
	case OpSearch:
		var matches []rules.Match
		matches, err = r.Search().Search(f.Project.ProjectKey(), f.Ref, fx.Term)
		empty = matches == nil
	case OpCI:
		var ci core.CI
		ci, err = r.CI(ctx, f.Project, f.Ref)
		empty = reflect.DeepEqual(ci, core.CI{})
	case OpNotes:
		var notes []core.ReviewNote
		notes, err = r.Notes(ctx, f.Project)
		empty = notes == nil
	default:
		t.Errorf("failure: unknown operation %q", f.Op)
		return
	}

	at := string(f.Op) + " " + f.Project.String() + "@" + f.Ref
	switch {
	case err == nil:
		t.Errorf("%s: no error, want %v: a failed read must never look like an empty answer", at, f.Kind)
	case !errors.Is(err, f.Kind):
		t.Errorf("%s: error %v (reason %q), want %v", at, err, base.ReasonOf(err), f.Kind)
	case !empty:
		t.Errorf("%s: failed with %v but also returned a result", at, err)
	case f.Kind == base.ErrForbidden && len(base.PermissionsOf(err)) == 0:
		t.Errorf("%s: forbidden without the permissions the platform needed", at)
	}
}

package base

import (
	"context"
	"errors"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// Reader is the read side of a platform. It is made of the interfaces the
// core already reads through, reached per project and ref where they are
// bound to one, plus the reads that have no core interface yet. Every
// method follows the error contract of the package.
type Reader interface {
	// Capabilities are what the platform can read.
	Capabilities() Capabilities
	// Changes reads change requests for the cascade.
	Changes() cascade.Source
	// Files reads files and directories of a project at a ref; criteria
	// are read through it from the target branch.
	Files() core.Files
	// Search searches code in a project at a ref. Where Capabilities say
	// the ref is not covered, it returns ErrSearchUnavailable.
	Search() rules.Search
	// Tree lists the files of project at ref. The tree may be read lazily:
	// failures, ErrTreeTruncated among them, come from its List.
	Tree(ctx context.Context, project core.Key, ref string) rules.Tree
	// CI returns the CI state of the commit head of project. A read that
	// fails is an error, never a state; ReadCI turns it into one.
	CI(ctx context.Context, project core.Key, head string) (core.CI, error)
	// Notes returns the notes on change request cr, review comments
	// included, that carry the fingerprints of earlier runs.
	Notes(ctx context.Context, cr core.Key) ([]core.ReviewNote, error)
}

// Repo returns the packs.Reader of project at ref, read through files.
// Packs only read files they know exist, so a missing file is
// ErrNotFound.
func Repo(ctx context.Context, files core.Files, project core.Key, ref string) packs.Reader {
	return repo{ctx: ctx, files: files, project: project, ref: ref}
}

type repo struct {
	ctx     context.Context
	files   core.Files
	project core.Key
	ref     string
}

func (r repo) ReadFile(path string) ([]byte, error) {
	content, found, err := r.files.Get(r.ctx, r.project, r.ref, path)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &Error{Kind: ErrNotFound, Op: "file " + path + " at " + r.ref}
	}
	return content, nil
}

// ReadCI returns the CI state of head for the findings document. A
// platform without a CI source, or a read that fails, gives an unknown
// state whose reason is forbidden or error and whose detail says why.
func ReadCI(ctx context.Context, r Reader, project core.Key, head string) core.CI {
	unknown := func(reason, detail string) core.CI {
		return core.CI{State: core.CIUnknown, Reason: reason, Detail: detail, Checks: []core.Check{}}
	}
	if !r.Capabilities().CI.Readable() {
		return unknown("error", "the platform reports no CI")
	}
	ci, err := r.CI(ctx, project, head)
	switch {
	case errors.Is(err, ErrForbidden):
		return unknown("forbidden", err.Error())
	case err != nil:
		return unknown("error", err.Error())
	}
	return ci
}

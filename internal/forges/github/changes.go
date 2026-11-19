package github

import (
	"context"
	"errors"
	"strconv"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// filesCap is the most files GitHub lists for a pull request; a list that
// reaches it may have been cut.
const filesCap = 3000

type pullJSON struct {
	Title  string  `json:"title"`
	Body   *string `json:"body"`
	State  string  `json:"state"`
	Merged bool    `json:"merged"`
	Draft  bool    `json:"draft"`
	User   *struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
}

func (p *Provider) pull(ctx context.Context, op string, k core.Key) (pullJSON, error) {
	var v pullJSON
	err := p.getJSON(ctx, op, repoPath(k, "pulls", strconv.Itoa(k.Number)), &v)
	return v, err
}

// changes is the cascade.Source of the provider.
type changes struct{ p *Provider }

// Get reads a pull request; when there is none, the issue of the same
// number tells an issue from nothing. A number the token cannot read is
// nothing, as the cascade defines it.
func (c changes) Get(ctx context.Context, k core.Key) (cascade.Item, error) {
	op := "change request " + k.String()
	if err := c.p.checkHost(op, k); err != nil {
		return cascade.Item{}, err
	}
	pr, err := c.p.pull(ctx, op, k)
	switch {
	case err == nil:
		item := cascade.Item{Kind: cascade.KindChangeRequest, Title: pr.Title, Head: pr.Head.SHA}
		if pr.Body != nil {
			item.Description = *pr.Body
		}
		return item, nil
	case !unreadable(err):
		return cascade.Item{}, err
	}
	var issue struct{}
	err = c.p.getJSON(ctx, op, repoPath(k, "issues", strconv.Itoa(k.Number)), &issue)
	switch {
	case err == nil:
		return cascade.Item{Kind: cascade.KindIssue}, nil
	case unreadable(err):
		return cascade.Item{Kind: cascade.KindNotFound}, nil
	}
	return cascade.Item{}, err
}

// unreadable reports whether GitHub answered that the number is not there
// or not for this token: 404 or 403, which the cascade reads as nothing. A
// refused token (401), a missing token or a limit is an error, never
// nothing.
func unreadable(err error) bool { return isStatus(err, 404) || isStatus(err, 403) }

// ChangeRequest implements base.Reader.
func (p *Provider) ChangeRequest(ctx context.Context, k core.Key) (core.ChangeRequest, error) {
	op := "change request " + k.String()
	if err := p.checkHost(op, k); err != nil {
		return core.ChangeRequest{}, err
	}
	pr, err := p.pull(ctx, op, k)
	if err != nil {
		return core.ChangeRequest{}, err
	}
	cr := core.ChangeRequest{
		Key: k, Title: pr.Title, HeadSHA: pr.Head.SHA, BaseSHA: pr.Base.SHA, TargetBranch: pr.Base.Ref, Draft: pr.Draft,
		State: core.ChangeState(pr.State),
	}
	if pr.Body != nil {
		cr.Description = *pr.Body
	}
	if pr.User != nil {
		cr.Author = pr.User.Login
	}
	if pr.Merged {
		cr.State = core.StateMerged
	}
	if err := cr.Validate(); err != nil {
		return core.ChangeRequest{}, &base.Error{Kind: base.ErrTransport, Op: op, Err: err}
	}
	return cr, nil
}

type fileJSON struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename"`
	Status           string  `json:"status"`
	Changes          int     `json:"changes"`
	Patch            *string `json:"patch"`
}

// ChangedFiles implements base.Reader.
func (p *Provider) ChangedFiles(ctx context.Context, k core.Key) ([]core.FileChange, error) {
	op := "changed files " + k.String()
	if err := p.checkHost(op, k); err != nil {
		return nil, err
	}
	var list []fileJSON
	path := repoPath(k, "pulls", strconv.Itoa(k.Number), "files") + "?per_page=" + strconv.Itoa(perPage)
	if err := pagesJSON(ctx, p, op, path, func(page []fileJSON) { list = append(list, page...) }); err != nil {
		return nil, err
	}
	if len(list) >= filesCap {
		return nil, &base.Error{Kind: base.ErrListTruncated, Op: op, Err: errors.New("GitHub lists at most " + strconv.Itoa(filesCap) + " files")}
	}
	out := make([]core.FileChange, 0, len(list))
	for _, f := range list {
		out = append(out, fileChange(f))
	}
	return out, nil
}

// fileChange maps a file of a pull request. GitHub leaves the patch out of
// a file whose diff is too large, of a binary file, and of a file whose
// content did not change; it says none of the three. A file with changed
// lines and no patch is too large; one with no changed line is a change of
// mode or a rename when the status says so, and otherwise a file the
// platform left out, which may be binary: GitHub gives no binary flag, so
// FileChange.Binary stays false.
func fileChange(f fileJSON) core.FileChange {
	status := core.FileStatus(f.Status)
	if status == "changed" {
		status = core.FileModified
	}
	fc := core.FileChange{Path: f.Filename, OldPath: f.PreviousFilename, Status: status}
	if f.Patch != nil {
		fc.Patch = *f.Patch
		return fc
	}
	switch {
	case f.Changes > 0:
		fc.Missing = core.PatchTooLarge
	case status == core.FileRenamed || status == core.FileCopied || status == core.FileUnchanged:
	default:
		fc.Missing = core.PatchOmitted
	}
	return fc
}

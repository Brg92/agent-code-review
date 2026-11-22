package github

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

type issueJSON struct {
	Number int     `json:"number"`
	State  string  `json:"state"`
	Title  string  `json:"title"`
	Body   *string `json:"body"`
	User   *struct {
		Login string `json:"login"`
	} `json:"user"`
	AuthorAssociation string `json:"author_association"`
	// PullRequest is set on the pull requests that GitHub lists among the
	// issues.
	PullRequest *struct{} `json:"pull_request"`
}

// Issues implements base.Reader: the issues of the project with the label,
// open and closed. The author of an issue is trusted as the author of a
// note is.
func (p *Provider) Issues(ctx context.Context, project core.Key, label string) ([]core.Issue, error) {
	op := "issues " + project.ProjectKey()
	if err := p.checkHost(op, project); err != nil {
		return nil, err
	}
	q := url.Values{"state": {"all"}, "labels": {label}, "per_page": {strconv.Itoa(perPage)}}
	var raw []issueJSON
	err := pagesJSON(ctx, p, op, repoPath(project, "issues")+"?"+q.Encode(), func(page []issueJSON) { raw = append(raw, page...) })
	if err != nil {
		return nil, err
	}
	issues := []core.Issue{}
	self := ""
	for _, r := range raw {
		if r.PullRequest != nil {
			continue
		}
		issue := core.Issue{Number: r.Number, Open: r.State == "open", Author: ghost, Trusted: trustedAssociation(r.AuthorAssociation), Title: r.Title}
		if r.Body != nil {
			issue.Body = *r.Body
		}
		if r.User != nil {
			issue.Author = r.User.Login
		}
		if !issue.Trusted && issue.Author != ghost {
			if self == "" {
				if self, err = p.Self(ctx); err != nil {
					return nil, err
				}
			}
			issue.Trusted = strings.EqualFold(issue.Author, self)
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// repoReadable fails when the repository of project cannot be read, so that
// a 404 from one of its sub-resources can mean that the thing is missing.
func (p *Provider) repoReadable(ctx context.Context, project core.Key) error {
	_, err := p.repoInfo(ctx, project)
	return err
}

// HasLabel implements base.Reader.
func (p *Provider) HasLabel(ctx context.Context, project core.Key, name string) (bool, error) {
	op := "label " + name + " of " + project.ProjectKey()
	if err := p.checkHost(op, project); err != nil {
		return false, err
	}
	if err := p.repoReadable(ctx, project); err != nil {
		return false, err
	}
	_, err := p.get(ctx, repoPath(project, "labels", url.PathEscape(name)), "")
	switch {
	case err == nil:
		return true, nil
	case isKind(p.fail(op, err), base.ErrNotFound):
		return false, nil
	}
	return false, p.fail(op, err)
}

// Assignable implements base.Reader: GitHub answers 204 for an account that
// can be assigned to the issues of the repository and 404 for one that
// cannot.
func (p *Provider) Assignable(ctx context.Context, project core.Key, login string) (bool, error) {
	op := "assignee " + login + " of " + project.ProjectKey()
	if err := p.checkHost(op, project); err != nil {
		return false, err
	}
	if err := p.repoReadable(ctx, project); err != nil {
		return false, err
	}
	_, err := p.get(ctx, repoPath(project, "assignees", url.PathEscape(login)), "")
	switch {
	case err == nil:
		return true, nil
	case isKind(p.fail(op, err), base.ErrNotFound):
		return false, nil
	}
	return false, p.fail(op, err)
}

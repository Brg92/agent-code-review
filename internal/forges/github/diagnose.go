package github

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// Account implements base.Diagnoser. GitHub lists the scopes of a classic
// token in the X-OAuth-Scopes header of every answer, and has no such
// header for a fine-grained token.
func (p *Provider) Account(ctx context.Context) (base.Account, error) {
	const op = "token account"
	resp, err := p.get(ctx, "/user", "")
	if err != nil {
		return base.Account{}, p.fail(op, err)
	}
	var v struct {
		Login string `json:"login"`
	}
	if err := decode(op, resp.Body, &v); err != nil {
		return base.Account{}, err
	}
	if v.Login == "" {
		return base.Account{}, &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("the answer has no login")}
	}
	acct := base.Account{Login: v.Login}
	if values, ok := resp.Header["X-Oauth-Scopes"]; ok {
		acct.Classic = true
		for _, scope := range strings.Split(strings.Join(values, ","), ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				acct.Scopes = append(acct.Scopes, scope)
			}
		}
	}
	return acct, nil
}

// Access implements base.Diagnoser.
func (p *Provider) Access(ctx context.Context, project core.Key, acct base.Account) (base.Access, error) {
	op := "access to " + project.ProjectKey()
	if err := p.checkHost(op, project); err != nil {
		return base.Access{}, err
	}
	var out base.Access
	add := func(area base.Area, err error) { out.Reads = append(out.Reads, base.AreaRead{Area: area, Err: err}) }

	var repo struct {
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}
	repoErr := p.getJSON(ctx, "repository "+project.Project, repoPath(project), &repo)
	add(base.AreaRepository, repoErr)
	probe := func(area base.Area, path string) {
		_, err := p.get(ctx, path, "")
		if err != nil {
			err = p.fail(string(area)+" of "+project.Project, err)
		}
		add(area, err)
	}
	probe(base.AreaPullRequests, repoPath(project, "pulls")+"?per_page=1")
	probe(base.AreaContents, repoPath(project, "contents"))
	probe(base.AreaIssues, repoPath(project, "issues")+"?per_page=1")
	switch {
	case repoErr != nil:
		add(base.AreaChecks, repoErr)
	case repo.DefaultBranch == "":
		add(base.AreaChecks, &base.Error{Kind: base.ErrNotFound, Op: "check runs of " + project.Project, Err: errors.New("the repository has no default branch")})
	default:
		probe(base.AreaChecks, repoPath(project, "commits", url.PathEscape(repo.DefaultBranch), "check-runs")+"?per_page=1")
		last := &out.Reads[len(out.Reads)-1]
		var be *base.Error
		if errors.Is(last.Err, base.ErrForbidden) && len(base.PermissionsOf(last.Err)) == 0 && errors.As(last.Err, &be) {
			be.Permissions = checkRunsPermissions
		}
	}
	for _, area := range []base.Area{base.AreaPullRequests, base.AreaIssues} {
		out.Writes = append(out.Writes, writeAccess(area, acct, repoErr == nil, repo.Private))
	}
	return out, nil
}

// writeAccess says whether a token may write to an area of a repository.
// Only a classic token can be told apart from reads: it writes with the
// repo scope, or with public_repo on a public repository. For any other
// token the platform says nothing short of a write, which is never made.
func writeAccess(area base.Area, acct base.Account, repoKnown, private bool) base.WriteAccess {
	w := base.WriteAccess{Area: area}
	switch {
	case !repoKnown:
		w.Detail = "the repository could not be read"
	case !acct.Classic:
		w.Detail = "a token that is not classic has permissions that only a write shows; publish names the one it lacks"
	case slices.Contains(acct.Scopes, "repo"):
		w.State, w.Detail = base.WriteAllowed, "the token has the repo scope"
	case slices.Contains(acct.Scopes, "public_repo") && !private:
		w.State, w.Detail = base.WriteAllowed, "the token has the public_repo scope and the repository is public"
	case private:
		w.State, w.Detail = base.WriteDenied, "the token needs the repo scope for a private repository"
	default:
		w.State, w.Detail = base.WriteDenied, "the token needs the repo or public_repo scope"
	}
	return w
}

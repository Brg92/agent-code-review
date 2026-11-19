package github

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// checkRunsPermissions is what a fine-grained token needs to read check
// runs, which have no permission of their own there; it is reported when a
// 403 does not say what it needed.
var checkRunsPermissions = []string{"actions: read"}

// CI implements base.Reader. The check runs of the commit are the source;
// the combined status, which external services set, is added to them. A
// read that fails is an error: an unreadable CI is never "none".
func (p *Provider) CI(ctx context.Context, project core.Key, head string) (core.CI, error) {
	op := "ci " + project.ProjectKey() + "@" + head
	if err := p.checkHost(op, project); err != nil {
		return core.CI{}, err
	}
	sha := url.PathEscape(head)
	var runs []checkRunJSON
	path := repoPath(project, "commits", sha, "check-runs") + "?per_page=" + strconv.Itoa(perPage)
	err := pagesJSON(ctx, p, op, path, func(page struct {
		CheckRuns []checkRunJSON `json:"check_runs"`
	}) {
		runs = append(runs, page.CheckRuns...)
	})
	if err != nil {
		if isStatus(err, 422) {
			// GitHub answers 422, not 404, for a SHA it has no commit for.
			return core.CI{}, &base.Error{Kind: base.ErrNotFound, Op: op, Err: err}
		}
		if isKind(err, base.ErrForbidden) && len(base.PermissionsOf(err)) == 0 {
			var be *base.Error
			if errors.As(err, &be) {
				be.Permissions = checkRunsPermissions
			}
		}
		return core.CI{}, err
	}
	var status struct {
		Statuses []statusJSON `json:"statuses"`
	}
	if err := p.getJSON(ctx, op, repoPath(project, "commits", sha, "status"), &status); err != nil {
		return core.CI{}, err
	}

	checks := make([]core.Check, 0, len(runs)+len(status.Statuses))
	failing, pending := false, false
	for _, r := range runs {
		c := core.Check{Name: r.Name, Status: r.Status, Conclusion: r.Conclusion}
		checks = append(checks, c)
		switch {
		case r.Status != "completed":
			pending = true
		case r.Conclusion != nil && !passes(*r.Conclusion):
			failing = true
		}
	}
	for _, s := range status.Statuses {
		c := core.Check{Name: s.Context, Status: "completed"}
		conclusion := ""
		switch s.State {
		case "success":
			conclusion = "success"
		case "failure", "error":
			conclusion = "failure"
			failing = true
		default:
			c.Status, pending = "in_progress", true
		}
		if conclusion != "" {
			c.Conclusion = &conclusion
		}
		checks = append(checks, c)
	}
	state := core.CIPassing
	switch {
	case failing:
		state = core.CIFailing
	case pending:
		state = core.CIPending
	case len(checks) == 0:
		state = core.CINone
	}
	return core.CI{State: state, Checks: checks}, nil
}

type checkRunJSON struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Conclusion *string `json:"conclusion"`
}

type statusJSON struct {
	Context string `json:"context"`
	State   string `json:"state"`
}

// passes reports whether the conclusion of a completed check is not a
// failure.
func passes(conclusion string) bool {
	switch conclusion {
	case "success", "neutral", "skipped":
		return true
	}
	return false
}

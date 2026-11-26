package cli

import (
	crand "crypto/rand"
	"fmt"
	"io"

	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/review"
)

const queueUsage = `usage: agent-code-review queue [--limit N] [--verbose]

Lists the open pull requests on github.com where the account of the token is
a requested reviewer, the most recently updated first: N of them, 30 by
default and 100 at most. For each one it says whether a review of it is in
the work directory and whether the pull request has new commits since. It
only reads. The agent never chooses what to review: the user does, and runs
agent-code-review review <pull request>.

--verbose also writes the lines of run.log to stderr as they happen.
`

// runQueue lists the change requests that ask for the user's review.
func runQueue(args []string, e env) int {
	o, err := parseOptions("queue", args, flagLimit)
	limit := base.DefaultQueueLimit
	switch {
	case err != nil:
		return usageError(e, "queue", err.Error(), queueUsage)
	case len(o.args) > 0:
		return usageError(e, "queue", "takes no argument", queueUsage)
	case o.limitSet && (o.limit < 1 || o.limit > base.MaxQueueLimit):
		return usageError(e, "queue", fmt.Sprintf("--limit is a number from 1 to %d", base.MaxQueueLimit), queueUsage)
	case o.limitSet:
		limit = o.limit
	}
	work, err := e.workDir()
	if err != nil {
		return finish(e, err, nil)
	}
	locals, err := review.ReadLocalReviews(work)
	if err != nil {
		return finish(e, err, nil)
	}
	log := e.logFor("queue", o.verbose)
	defer log.Close()
	provider, err := e.reader(defaultHost, log)
	if err != nil {
		return finish(e, err, nil)
	}
	queue, ok := provider.(base.Queue)
	if !ok {
		return finish(e, &review.Failure{Kind: review.KindUnexpected, What: "the platform cannot list a review queue", Where: defaultHost,
			Why: "its reader has no review queue", Hint: "report it"}, nil)
	}
	ctx := e.context()
	res, err := queue.ReviewQueue(ctx, limit)
	if err != nil {
		return finish(e, review.PlatformFailure(err, defaultHost, "could not read the review queue", defaultHost+", review queue"), nil)
	}
	view := render.QueueView{Host: defaultHost, Total: res.Total, Incomplete: res.Incomplete, Skipped: res.Skipped, Unreadable: locals.Unreadable}
	for _, it := range res.Items {
		line := render.QueueLine{Key: it.Key, Title: it.Title, Author: it.Author, Draft: it.Draft, UpdatedAt: it.UpdatedAt}
		if local, found := locals.Of(it.Key); found {
			line.Local = render.QueueLocal{Reviewed: true, Phase: string(local.Phase), Head8: local.Head8(), Head: render.HeadSame}
			// The queue does not carry the head: read it for the few that
			// have a review.
			if cr, err := provider.ChangeRequest(ctx, it.Key); err != nil {
				line.Local.Head = render.HeadUnchecked
			} else if local.HeadChanged(cr.HeadSHA) {
				line.Local.Head = render.HeadChanged
			}
		}
		view.Lines = append(view.Lines, line)
	}
	var random io.Reader = crand.Reader
	if e.random != nil {
		random = e.random
	}
	token, err := render.QueueToken(random)
	if err != nil {
		return finish(e, err, nil)
	}
	text, err := render.Queue(view, token)
	if err != nil {
		return finish(e, &review.Failure{Kind: review.KindUnexpected, What: "could not write the review queue", Why: err.Error(), Err: err,
			Hint: "a title or an author holds the delimiter of untrusted content; report it"}, nil)
	}
	fmt.Fprint(e.stdout, text)
	return exitOK
}

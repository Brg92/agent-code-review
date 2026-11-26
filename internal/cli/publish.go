package cli

import (
	"fmt"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/review"
)

const publishUsage = `usage: agent-code-review publish --plan <comments|issues|both> [--verbose] [--run <run id>]
       agent-code-review publish --apply [--verbose] [--run <run id>]

--plan reads the platform and writes, for every change request of the most
recent run that is finalized and not published, or of the run given, a plan of
what would be written: the inline comments and the note that nothing blocks
(comments), the issues (issues), or all of it (both). It prints a summary of
each plan and writes nothing to the platform.

--apply writes exactly the plans that --plan made, once the user has said yes.
It writes nothing for a change request whose head changed, that is closed, or
that was opened by the account of the token. Run it again after a failure: what
was done is not done twice.

--verbose also writes the lines of run.log to stderr as they happen.
`

// runPublish plans or applies what is written to the platform.
func runPublish(args []string, e env) int {
	o, err := parseOptions("publish", args, flagRun|flagPlan|flagApply)
	var choice core.Choice
	switch {
	case err != nil && o.plan == "" && !o.apply && err.Error() == "flag needs an argument: -plan":
		return usageError(e, "publish", "--plan needs what to plan: comments, issues or both", publishUsage)
	case err != nil:
		return usageError(e, "publish", err.Error(), publishUsage)
	case len(o.args) > 0:
		return usageError(e, "publish", "takes no argument", publishUsage)
	case o.apply && o.plan != "":
		return usageError(e, "publish", "give --plan or --apply, not both", publishUsage)
	case !o.apply && o.plan == "":
		return usageError(e, "publish", "give --plan <comments|issues|both> or --apply", publishUsage)
	}
	if !o.apply {
		if choice, err = core.ParseChoice(o.plan); err != nil {
			return usageError(e, "publish", "--plan needs what to plan: comments, issues or both, not "+fmt.Sprintf("%q", o.plan), publishUsage)
		}
	}
	work, err := e.workDir()
	if err != nil {
		return finish(e, err, nil)
	}
	log := e.logFor("publish", o.verbose)
	defer log.Close()
	opened, err := review.OpenRun(work, o.run, log)
	if err != nil {
		return finish(e, err, log)
	}
	provider, err := e.reader(opened.State.Root.Host, log)
	if err != nil {
		return finish(e, err, log)
	}
	deps := review.PublishDeps{Provider: provider, Registries: registries(), Now: e.clock()}
	if o.apply {
		applied, err := review.ApplyPublish(e.context(), opened, deps, log)
		if applied != nil {
			fmt.Fprint(e.stdout, applied.Text)
		}
		if err != nil {
			return finish(e, err, log)
		}
		return exitOK
	}
	planned, err := review.PlanPublish(e.context(), opened, deps, choice, log)
	if planned != nil {
		fmt.Fprint(e.stdout, planned.Text)
	}
	if err != nil {
		return finish(e, err, log)
	}
	return exitOK
}

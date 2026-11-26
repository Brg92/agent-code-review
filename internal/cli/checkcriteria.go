package cli

import (
	crand "crypto/rand"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/review"
)

const checkCriteriaUsage = `usage: agent-code-review check-criteria [<dir>]

Checks the repository criteria in the working copy <dir>, the current
directory by default, as a review would read them from the target branch:
the neutral layout .agent-code-review/ or the legacy one, with the packs
loaded. It prints the repository rules (id, severity, source), the
disabledRules and ruleOptions that apply, and every item a review would
ignore, with the reason, as the report lists them under "Repository criteria
ignored".

It needs no token and no network, and reads only the files of <dir>: it does
not look in the directories above, and it never reads git objects.

It ends with 0 when nothing would be ignored, and with 3 otherwise, an
invalid config.json included. A review only notes an invalid config.json and
goes on with the packs alone; this command is where it is fixed.
`

// runCheckCriteria validates the criteria of a working copy.
func runCheckCriteria(args []string, e env) int {
	o, err := parseOptions("check-criteria", args, 0)
	switch {
	case err != nil:
		return usageError(e, "check-criteria", err.Error(), checkCriteriaUsage)
	case len(o.args) > 1:
		return usageError(e, "check-criteria", "give one directory, not "+fmt.Sprint(len(o.args)), checkCriteriaUsage)
	}
	dir := "."
	if len(o.args) == 1 {
		dir = o.args[0]
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	cfg, err := e.userConfig()
	if err != nil {
		return finish(e, err, nil)
	}
	set, err := e.loadPacks(cfg)
	if err != nil {
		return finish(e, err, nil)
	}
	criteria, err := review.CheckCriteria(e.context(), dir, set)
	if err != nil {
		return finish(e, err, nil)
	}
	var random io.Reader = crand.Reader
	if e.random != nil {
		random = e.random
	}
	token, err := render.QueueToken(random)
	if err != nil {
		return finish(e, err, nil)
	}
	text, err := render.CriteriaCheck(dir, criteria, token)
	if err != nil {
		return finish(e, &review.Failure{Kind: review.KindUnexpected, What: "could not write the criteria", Why: err.Error(), Err: err,
			Hint: "an item holds the delimiter of untrusted content; report it"}, nil)
	}
	fmt.Fprint(e.stdout, text)
	if len(criteria.Ignored) > 0 {
		return exitValidation
	}
	return exitOK
}

package cli

import (
	"fmt"
	"regexp"

	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/version"
)

const finalizeUsage = `usage: agent-code-review finalize [--verbose] [--run <run id>]

Checks the judgement of every change request of the most recent run that is
not published, or of the run given, and writes its findings and its report.
Nothing is written unless every judgement is valid: otherwise it lists every
problem, by change request, with the field it is in. It reads nothing from
the platform, and writes nothing to it.

--verbose also writes the lines of run.log to stderr as they happen.
`

const showUsage = `usage: agent-code-review show [--verbose] [--run <run id>] [<cr>]
       agent-code-review show --save [<folder>] [<cr>] [--verbose] [--run <run id>]

Prints the summary of the cascade of a finalized run, or with <cr>, a change
request of the run as <owner>/<repo>#<number> or its key, its full report.

--save writes the report to a new file instead: the one of the root, with the
summary of the cascade, or the one of <cr>, in <folder> or, when none is
named, in agent-code-review/reports in the home directory. The folder must be
outside any git work tree and have no "..". It prints the path of the file.

--verbose also writes the lines of run.log to stderr as they happen.
`

// registries are the reference registries of every platform the command
// line knows.
func registries() []refs.Registry { return []refs.Registry{github.Registry{}, gitlab.Registry{}} }

// runFinalize validates the judgements and writes the reports.
func runFinalize(args []string, e env) int {
	o, err := parseOptions("finalize", args, flagRun)
	switch {
	case err != nil:
		return usageError(e, "finalize", err.Error(), finalizeUsage)
	case len(o.args) > 0:
		return usageError(e, "finalize", "takes no argument", finalizeUsage)
	}
	work, err := e.workDir()
	if err != nil {
		return finish(e, err, nil)
	}
	log := e.logFor("finalize", o.verbose)
	defer log.Close()
	opened, err := review.OpenRun(work, o.run, log)
	if err != nil {
		return finish(e, err, log)
	}
	cfg, err := e.userConfig()
	if err != nil {
		return finish(e, err, log)
	}
	set, err := e.loadPacks(cfg)
	if err != nil {
		return finish(e, err, log)
	}
	done, err := review.Finalize(opened, review.FinalizeDeps{Packs: set, Registries: registries(), Version: version.Version, Now: e.clock()}, log)
	if err != nil {
		return finish(e, err, log)
	}
	fmt.Fprint(e.stdout, done.Text)
	return exitOK
}

// crReference is what a change request looks like on the command line: it
// ends with # and a number.
var crReference = regexp.MustCompile(`#[0-9]+$`)

// runShow prints the summary of a finalized run or one of its reports, or
// saves one.
func runShow(args []string, e env) int {
	o, err := parseOptions("show", args, flagRun|flagSave)
	if err != nil {
		return usageError(e, "show", err.Error(), showUsage)
	}
	cr, dir, msg := showArguments(o)
	if msg != "" {
		return usageError(e, "show", msg, showUsage)
	}
	work, err := e.workDir()
	if err != nil {
		return finish(e, err, nil)
	}
	log := e.logFor("show", o.verbose)
	defer log.Close()
	opened, err := review.OpenRun(work, o.run, log)
	if err != nil {
		return finish(e, err, log)
	}
	deps := review.ShowDeps{Registries: registries(), Version: version.Version, Now: e.clock(), HomeDir: e.homeDir()}
	if !o.save {
		text, err := review.Show(opened, deps, log, cr)
		if err != nil {
			return finish(e, err, log)
		}
		fmt.Fprint(e.stdout, text)
		return exitOK
	}
	path, err := review.Save(opened, deps, log, dir, cr)
	if err != nil {
		return finish(e, err, log)
	}
	fmt.Fprintln(e.stdout, path)
	return exitOK
}

// showArguments tells the change request from the folder among the
// arguments of show; msg is what is wrong with them, if anything.
func showArguments(o options) (cr, dir, msg string) {
	for _, a := range o.args {
		switch {
		case crReference.MatchString(a) && cr == "":
			cr = a
		case crReference.MatchString(a):
			return "", "", "give one change request"
		case !o.save:
			return "", "", "a folder is for --save: " + a + " is not a change request"
		case dir != "":
			return "", "", "give one folder"
		default:
			dir = a
		}
	}
	return cr, dir, ""
}

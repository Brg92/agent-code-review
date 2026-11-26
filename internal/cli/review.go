package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/transport"
)

const reviewUsage = `usage: agent-code-review review [--verbose] <pull request>

<pull request> is the address of a pull request, or owner/repo#N. The review
collects it and every change request its Related Changes list, writes their
work packages in the work directory and prints a summary.

--verbose also writes the lines of run.log to stderr as they happen.
`

const nextUsage = `usage: agent-code-review next [--verbose] [--run <run id>] [--part <n>]

Prints the next part to judge of the most recent run that is not published,
or of the run given, with where to write its judgement. Once every part of a
change request was printed it says where to write the judgement, until it is
there; when nothing is left to judge it says so.

--part prints part n of the change request being judged again, without
moving on.

--verbose also writes the lines of run.log to stderr as they happen.
`

// options are the arguments of the commands that work on a run.
type options struct {
	verbose bool
	run     string
	// plan is the argument of --plan; apply is --apply.
	plan    string
	apply   bool
	part    int
	partSet bool
	// limit is the argument of --limit.
	limit    int
	limitSet bool
	// repo is the argument of --repo.
	repo string
	save bool
	args []string
}

// allowed are the flags besides --verbose a command takes.
type allowed uint8

const (
	flagRun allowed = 1 << iota
	flagPart
	flagSave
	flagPlan
	flagApply
	flagLimit
	flagRepo
)

// parseOptions reads the flags of a command wherever they are among its
// arguments; allow says which of --run, --part and --save it takes.
func parseOptions(name string, args []string, allow allowed) (options, error) {
	var o options
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.verbose, "verbose", false, "")
	if allow&flagRun != 0 {
		fs.StringVar(&o.run, "run", "", "")
	}
	if allow&flagPart != 0 {
		fs.IntVar(&o.part, "part", 0, "")
	}
	if allow&flagLimit != 0 {
		fs.IntVar(&o.limit, "limit", 0, "")
	}
	if allow&flagRepo != 0 {
		fs.StringVar(&o.repo, "repo", "", "")
	}
	if allow&flagSave != 0 {
		fs.BoolVar(&o.save, "save", false, "")
	}
	if allow&flagPlan != 0 {
		fs.StringVar(&o.plan, "plan", "", "")
	}
	if allow&flagApply != 0 {
		fs.BoolVar(&o.apply, "apply", false, "")
	}
	for {
		if err := fs.Parse(args); err != nil {
			switch {
			case strings.HasPrefix(err.Error(), "flag needs an argument"):
				return o, err
			case strings.Contains(err.Error(), "invalid value"):
				// The value is not quoted: it may be anything.
				name := "--part"
				if strings.Contains(err.Error(), "flag -limit") {
					name = "--limit"
				}
				return o, errors.New(name + " is not a number")
			}
			return o, errors.New("unknown flag")
		}
		if fs.NArg() == 0 {
			fs.Visit(func(f *flag.Flag) {
				o.partSet = o.partSet || f.Name == "part"
				o.limitSet = o.limitSet || f.Name == "limit"
			})
			return o, nil
		}
		o.args = append(o.args, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func usageError(e env, command, msg, usage string) int {
	fmt.Fprintf(e.stderr, "agent-code-review %s: %s\n%s", command, msg, usage)
	return exitUsage
}

// finish prints the failure of a command and returns its exit code. The
// path of the run log is added when the command has one.
func finish(e env, err error, log *runlog.Log) int {
	var f *review.Failure
	if !errors.As(err, &f) {
		f = &review.Failure{Kind: review.KindUnexpected, What: "the command failed", Why: err.Error(), Hint: "run it again; if it fails the same way, report it"}
	}
	if f.LogPath == "" && log != nil {
		f.LogPath = log.Path()
	}
	fmt.Fprint(e.stderr, f.Report())
	return exitCodeOf(f.Kind)
}

// exitCodeOf is the exit code of a failure of kind, as docs/exit-codes.md
// lists them.
func exitCodeOf(kind review.Kind) int {
	switch kind {
	case review.KindUsage:
		return exitUsage
	case review.KindAuth:
		return exitAuth
	case review.KindPlatform:
		return exitPlatform
	case review.KindState:
		return exitState
	case review.KindValidation:
		return exitValidation
	case review.KindHeadChanged:
		return exitHeadChanged
	case review.KindPath:
		return exitPath
	}
	return exitUnexpected
}

func (e env) clock() func() time.Time {
	if e.now != nil {
		return e.now
	}
	return time.Now
}

// logFor makes the run log of a command, echoed to stderr with --verbose.
func (e env) logFor(command string, verbose bool) *runlog.Log {
	var w io.Writer
	if verbose {
		w = e.stderr
	}
	l := runlog.New(e.clock(), w)
	l.SetCommand(command)
	return l
}

// workDir is the base of the work directories.
func (e env) workDir() (string, error) {
	dir, err := state.WorkDirBase(e.getenv, e.cacheDir())
	if err != nil {
		return "", &review.Failure{Kind: review.KindUnexpected, What: "could not find the work directory", Where: state.WorkDirEnv, Why: err.Error(), Err: err,
			Hint: "set " + state.WorkDirEnv + " to an absolute path, or make the user cache directory available"}
	}
	return dir, nil
}

// userConfig reads the user configuration.
func (e env) userConfig() (state.UserConfig, error) {
	path, err := state.UserConfigPath(e.configDir())
	if err != nil {
		return state.UserConfig{KeepDays: state.DefaultKeepDays}, nil
	}
	cfg, err := state.LoadUserConfig(path)
	if err != nil {
		return state.UserConfig{}, &review.Failure{Kind: review.KindValidation, What: "the user configuration is not valid", Where: path, Why: err.Error(), Err: err,
			Hint: "fix the fields it names, or remove the file to use the defaults"}
	}
	return cfg, nil
}

// loadPacks finds and loads the packs.
func (e env) loadPacks(cfg state.UserConfig) (*packs.Set, error) {
	layout, err := e.locate(cfg)
	if err == nil {
		var set *packs.Set
		if set, err = packs.Load(layout.Dirs...); err == nil {
			return set, nil
		}
	}
	return nil, &review.Failure{Kind: review.KindUnexpected, What: "could not load the packs", Where: packs.PacksDirEnv, Why: err.Error(), Err: err,
		Hint: "install the packs next to the executable, or set " + packs.PacksDirEnv + " or packPaths in the user configuration"}
}

func (e env) reader(host string, log *runlog.Log) (base.ForgeProvider, error) {
	opts := github.Options{Transport: transport.Config{Observe: log.Observe}}
	newReader := e.newReader
	if newReader == nil {
		newReader = github.New
	}
	r, err := newReader(host, e.resolver(), opts)
	var unsupported *github.UnsupportedHostError
	if errors.As(err, &unsupported) {
		return nil, unsupportedHost(host)
	}
	if err != nil {
		return nil, &review.Failure{Kind: review.KindUnexpected, What: "could not start the platform reader", Where: host, Why: err.Error(), Err: err, Hint: "report it"}
	}
	return r, nil
}

func unsupportedHost(where string) error {
	return &review.Failure{Kind: review.KindUsage, What: "the pull request is on a platform or host this release does not read", Where: where,
		Why: "this platform or host is not supported: only github.com is", Hint: "give the address of a pull request on github.com"}
}

// runReview collects a pull request and what it relates to.
func runReview(args []string, e env) int {
	o, err := parseOptions("review", args, 0)
	switch {
	case err != nil:
		return usageError(e, "review", err.Error(), reviewUsage)
	case len(o.args) == 0:
		return usageError(e, "review", "missing the pull request to review", reviewUsage)
	case len(o.args) > 1:
		return usageError(e, "review", "give one pull request, not "+fmt.Sprint(len(o.args)), reviewUsage)
	}
	ref := o.args[0]
	from := core.Key{Forge: core.GitHub, Host: defaultHost}
	registries := []refs.Registry{github.Registry{}, gitlab.Registry{}}
	sel, err := base.SelectPlatform(ref, from, registries...)
	if err != nil {
		return usageError(e, "review", fmt.Sprintf("%q is not a pull request address or owner/repo#N", ref), reviewUsage)
	}
	root := sel.Root
	if root.Forge != core.GitHub || !github.SupportedHost(root.Host) {
		return finish(e, unsupportedHost(root.String()), nil)
	}
	work, err := e.workDir()
	if err != nil {
		return finish(e, err, nil)
	}
	cfg, err := e.userConfig()
	if err != nil {
		return finish(e, err, nil)
	}
	set, err := e.loadPacks(cfg)
	if err != nil {
		return finish(e, err, nil)
	}
	log := e.logFor("review", o.verbose)
	defer log.Close()
	reader, err := e.reader(root.Host, log)
	if err != nil {
		return finish(e, err, nil)
	}
	run, err := review.Collect(e.context(), review.Deps{
		Reader: reader, Registries: registries, Packs: set, WorkDir: work, KeepDays: cfg.KeepDays, Now: e.clock(), Random: e.random, Log: log,
	}, root)
	if err != nil {
		return finish(e, err, log)
	}
	summary, err := review.Summary(run)
	if err != nil {
		return finish(e, err, log)
	}
	fmt.Fprint(e.stdout, summary)
	return exitOK
}

// runNext serves the next part to judge.
func runNext(args []string, e env) int {
	o, err := parseOptions("next", args, flagRun|flagPart)
	switch {
	case err != nil:
		return usageError(e, "next", err.Error(), nextUsage)
	case len(o.args) > 0:
		return usageError(e, "next", "takes no argument", nextUsage)
	case o.partSet && o.part < 1:
		return usageError(e, "next", "--part is a part number from 1", nextUsage)
	}
	work, err := e.workDir()
	if err != nil {
		return finish(e, err, nil)
	}
	log := e.logFor("next", o.verbose)
	defer log.Close()
	opened, err := review.OpenRun(work, o.run, log)
	if err != nil {
		return finish(e, err, log)
	}
	served, err := review.Next(opened, log, o.part)
	if err != nil {
		return finish(e, err, log)
	}
	// Without the schema beside the packs the line is left out: doctor says so.
	cfg, _ := e.userConfig()
	var schema string
	if layout, err := e.locate(cfg); err == nil {
		schema, _ = layout.Schema(judgementSchema)
	}
	fmt.Fprint(e.stdout, served.WithSchema(schema).Text)
	return exitOK
}

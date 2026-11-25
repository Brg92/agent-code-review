package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/review"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
	"github.com/Brg92/agent-code-review/internal/version"
)

const doctorUsage = `usage: agent-code-review doctor [--repo owner/repo] [--verbose]

Runs fixed checks, in a fixed order, and prints one line for each: ok, warn
or fail, what was checked and, for warn and fail, what to do. It checks the
version, git, where the token would be stored, the token for github.com and
what it can do, the packs, the user configuration and the work directory.
With --repo it also reads the repository, its pull requests, contents,
issues and check runs, and says whether the token can write to pull requests
and issues, which only publish needs.

It reads and prints no token, and writes nothing to the platform. It ends
with 0 unless a check fails; the code of a failure is the one of
docs/exit-codes.md for its cause.

--verbose also writes the lines of run.log to stderr as they happen.
`

type verdict string

const (
	checkOK   verdict = "ok"
	checkWarn verdict = "warn"
	checkFail verdict = "fail"
)

// check is the result of one check of doctor.
type check struct {
	verdict verdict
	what    string
	detail  string
	// todo is what to do, for a warning or a failure.
	todo string
	// code is the exit code a failure ends the command with.
	code int
}

// line is the check on one line: the verdict, what was checked, what was
// found and what to do.
func (c check) line() string {
	text := oneLine(c.what + ": " + c.detail)
	if c.todo != "" {
		text += "; to do: " + oneLine(c.todo)
	}
	return fmt.Sprintf("%-4s  %s", c.verdict, text)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func pass(what, detail string) check { return check{verdict: checkOK, what: what, detail: detail} }

func warn(what, detail, todo string) check {
	return check{verdict: checkWarn, what: what, detail: detail, todo: todo}
}

func failed(code int, what, detail, todo string) check {
	return check{verdict: checkFail, what: what, detail: detail, todo: todo, code: code}
}

// failurePriority orders the exit codes of failed checks: the first one a
// check failed with is the exit code of doctor.
var failurePriority = []int{exitAuth, exitUnsafeStorage, exitPlatform, exitValidation}

// doctorExit is the exit code of the checks: 0 when none failed, otherwise
// the code of the first kind of failure in failurePriority that one of them
// has, or 1.
func doctorExit(checks []check) int {
	failures := map[int]bool{}
	for _, c := range checks {
		if c.verdict == checkFail {
			failures[c.code] = true
		}
	}
	if len(failures) == 0 {
		return exitOK
	}
	for _, code := range failurePriority {
		if failures[code] {
			return code
		}
	}
	return exitUnexpected
}

// ignoredTokenVariables are the variables of CI that are never read as a
// token.
var ignoredTokenVariables = []string{"GITHUB_TOKEN", "GH_TOKEN"}

// ciTokenExample passes the token of a GitHub Actions job.
const ciTokenExample = "ACR_GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}"

// runDoctor checks the environment, in a fixed order.
func runDoctor(args []string, e env) int {
	o, err := parseOptions("doctor", args, flagRepo)
	switch {
	case err != nil:
		return usageError(e, "doctor", err.Error(), doctorUsage)
	case len(o.args) > 0:
		return usageError(e, "doctor", "takes no argument", doctorUsage)
	}
	var project core.Key
	if o.repo != "" {
		var ok bool
		if project, ok = repoKey(o.repo); !ok {
			return usageError(e, "doctor", "--repo is owner/repo, such as acme/app", doctorUsage)
		}
	}
	log := e.logFor("doctor", o.verbose)
	defer log.Close()
	d := &doctor{e: e, log: log, ctx: context.Background()}

	var checks []check
	emit := func(c ...check) { checks = append(checks, c...) }
	emit(pass("version", version.Version))
	emit(d.git())
	emit(d.storage())
	_, tokenCheck := d.token()
	emit(tokenCheck)
	emit(d.ignoredVariables()...)
	acct, accountCheck := d.account(tokenCheck)
	emit(accountCheck)
	if o.repo != "" {
		emit(d.repository(project, accountCheck, acct)...)
	}
	emit(d.packs())
	emit(d.userConfig())
	emit(d.workDir())

	for _, c := range checks {
		fmt.Fprintln(e.stdout, c.line())
	}
	return doctorExit(checks)
}

// repoKey reads owner/repo.
func repoKey(repo string) (core.Key, bool) {
	if strings.Count(repo, "/") != 1 {
		return core.Key{}, false
	}
	k, err := core.ParseKey("github:" + defaultHost + "/" + repo + "#1")
	return k, err == nil
}

type doctor struct {
	e   env
	log *runlog.Log
	ctx context.Context
	// provider is the reader of the platform, made once.
	provider    base.ForgeProvider
	providerErr error
}

func (d *doctor) git() check {
	out, _, err := d.e.git.Run(d.ctx, "", "--version")
	if err != nil {
		return failed(exitAuth, "git", "not found or does not run: "+err.Error(), "install git and put it on the PATH: the token is read through it")
	}
	return pass("git", strings.TrimSpace(out))
}

// tokenInEnvironment reports whether the token of github.com is given in
// its variable, so that nothing is stored.
func (d *doctor) tokenInEnvironment() bool {
	name := auth.EnvVar(defaultHost)
	return name != "" && strings.TrimSpace(d.e.getenv(name)) != ""
}

func (d *doctor) storage() check {
	const what = "credential storage"
	helpers, err := d.e.resolver().CheckHelpers(d.ctx, defaultHost)
	if err != nil {
		return failed(exitAuth, what, "the git credential helpers cannot be read: "+err.Error(), "check that git works and that its configuration can be read")
	}
	if err := helpers.Err(); err != nil {
		detail, todo, _ := strings.Cut(strings.TrimPrefix(err.Error(), "auth: "), "; ")
		if d.tokenInEnvironment() {
			// Nothing is stored when the token comes from the environment.
			return warn(what, detail+", which only matters for a token stored with `auth login`", todo)
		}
		return failed(exitUnsafeStorage, what, detail, todo)
	}
	if warnings := helpers.Warnings(); len(warnings) > 0 {
		return warn(what, strings.Join(warnings, "; "), "check where the helper keeps the token")
	}
	var names []string
	for _, h := range helpers.Helpers {
		names = append(names, fmt.Sprintf("%s (%s)", h.Value, h.Kind))
	}
	return pass(what, strings.Join(names, ", "))
}

// token finds the token for github.com. The value never leaves the
// resolver: only where it came from is reported.
func (d *doctor) token() (found bool, c check) {
	what := "token for " + defaultHost
	tok, err := d.e.resolver().Resolve(d.ctx, defaultHost)
	switch {
	case errors.Is(err, auth.ErrNoToken):
		return false, failed(exitAuth, what, "no token", "run "+loginHint(defaultHost))
	case err != nil:
		return false, failed(exitAuth, what, err.Error(), "run "+loginHint(defaultHost))
	}
	return true, pass(what, "from "+tok.Describe())
}

// ignoredVariables warns of the variables of CI that are set while the
// token's own is not: they are never read as a token.
func (d *doctor) ignoredVariables() []check {
	if d.tokenInEnvironment() {
		return nil
	}
	var set []string
	for _, name := range ignoredTokenVariables {
		if strings.TrimSpace(d.e.getenv(name)) != "" {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return nil
	}
	return []check{warn(strings.Join(set, " and "), "set but never read as a token",
		"pass the token as "+auth.EnvVar(defaultHost)+", in CI with "+ciTokenExample)}
}

// diagnoser makes the reader of the platform, once.
func (d *doctor) diagnoser() (base.Diagnoser, error) {
	if d.provider == nil && d.providerErr == nil {
		d.provider, d.providerErr = d.e.reader(defaultHost, d.log)
	}
	if d.providerErr != nil {
		return nil, d.providerErr
	}
	diag, ok := d.provider.(base.Diagnoser)
	if !ok {
		return nil, errors.New("the platform reader cannot say what the token can do")
	}
	return diag, nil
}

// failedRead is the failed check of a read of the platform: the code is
// the one of the failure the read is, as the other commands give it.
func failedRead(err error, what string) check {
	f := review.PlatformFailure(err, defaultHost, what, what)
	return failed(exitCodeOf(f.Kind), what, f.Why, f.Hint)
}

func (d *doctor) account(token check) (base.Account, check) {
	const what = "GET /user"
	if token.verdict == checkFail {
		return base.Account{}, failed(token.code, what, "not checked: there is no token to try", token.todo)
	}
	diag, err := d.diagnoser()
	if err != nil {
		return base.Account{}, failed(exitUnexpected, what, err.Error(), "report it")
	}
	acct, err := diag.Account(d.ctx)
	if err != nil {
		return base.Account{}, failedRead(err, what)
	}
	kind := "token is not classic (a fine-grained token or another kind)"
	if acct.Classic {
		scopes := "none"
		if len(acct.Scopes) > 0 {
			scopes = strings.Join(acct.Scopes, ", ")
		}
		kind = "classic token (scopes: " + scopes + ")"
	}
	return acct, pass(what, "logged in as "+acct.Login+"; "+kind)
}

// repository checks what the token can read in a repository, and whether
// it may write to what publish writes to.
func (d *doctor) repository(project core.Key, account check, acct base.Account) []check {
	repo := project.Project
	if account.verdict == checkFail {
		return []check{failed(account.code, "repository "+repo, "not checked: the token could not be used", account.todo)}
	}
	diag, err := d.diagnoser()
	if err != nil {
		return []check{failed(exitUnexpected, "repository "+repo, err.Error(), "report it")}
	}
	access, err := diag.Access(d.ctx, project, acct)
	if err != nil {
		return []check{failedRead(err, "repository "+repo)}
	}
	var out []check
	repoRead := true
	for _, r := range access.Reads {
		what := string(r.Area) + " of " + repo
		if r.Area == base.AreaRepository {
			what = "repository " + repo
		}
		if r.Err != nil {
			repoRead = repoRead && r.Area != base.AreaRepository
			out = append(out, failedRead(r.Err, what))
			continue
		}
		out = append(out, pass(what, "readable"))
	}
	if !repoRead {
		return out
	}
	for _, w := range access.Writes {
		what := "write to " + string(w.Area) + " of " + repo
		switch w.State {
		case base.WriteAllowed:
			out = append(out, pass(what, w.Detail))
		case base.WriteUnknown:
			out = append(out, pass(what, "cannot be checked without writing: "+w.Detail))
		default:
			out = append(out, warn(what, w.Detail, "needed only by publish: give the token the permission before you run it"))
		}
	}
	return out
}

func (d *doctor) packs() check {
	const what = "packs"
	cfg, _ := d.e.userConfig()
	dirs, err := packs.Locate(packs.LocateInput{Getenv: d.e.getenv, PackPaths: cfg.PackPaths})
	if err != nil {
		return failed(exitUnexpected, what, err.Error(), "install the packs next to the executable, or set "+packs.PacksDirEnv+" or packPaths in the user configuration")
	}
	set, err := packs.Load(dirs...)
	if err != nil {
		return failed(exitValidation, what, err.Error(), "fix the pack it names, or remove it from the directories above")
	}
	ids := make([]string, len(set.Packs))
	for i, p := range set.Packs {
		ids[i] = p.ID
	}
	return pass(what, fmt.Sprintf("%s (from %s)", strings.Join(ids, ", "), strings.Join(dirs, ", ")))
}

func (d *doctor) userConfig() check {
	const what = "user config"
	cfgDir := d.e.userConfigDir
	if cfgDir == nil {
		cfgDir = os.UserConfigDir
	}
	path, err := state.UserConfigPath(cfgDir)
	if err != nil {
		return pass(what, "there is no user configuration directory, the defaults apply")
	}
	if _, err := d.e.userConfig(); err != nil {
		var f *review.Failure
		if errors.As(err, &f) {
			return failed(exitCodeOf(f.Kind), what, f.Why, f.Hint)
		}
		return failed(exitUnexpected, what, path+": "+err.Error(), "report it")
	}
	if _, err := os.Stat(path); err != nil {
		return pass(what, path+" is not there, the defaults apply")
	}
	return pass(what, path+" is valid")
}

func (d *doctor) workDir() check {
	const what = "work directory"
	dir, err := d.e.workDir()
	if err != nil {
		return failed(exitUnexpected, what, err.Error(), "set "+state.WorkDirEnv+" to an absolute path, or make the user cache directory available")
	}
	hint := "make " + dir + " writable, or set " + state.WorkDirEnv + " to another absolute path"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return failed(exitUnexpected, what, dir+" cannot be created: "+err.Error(), hint)
	}
	probe, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return failed(exitUnexpected, what, dir+" cannot be written: "+err.Error(), hint)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return pass(what, dir+" is writable")
}

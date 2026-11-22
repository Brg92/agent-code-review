package review

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// Kind classifies a failure, which is what the exit code of the command
// says.
type Kind int

// The kinds of failure.
const (
	// KindUnexpected is a failure the command does not classify.
	KindUnexpected Kind = iota
	// KindUsage is a reference that names no change request of a platform
	// this release reads.
	KindUsage
	// KindAuth is a missing, expired or insufficient token.
	KindAuth
	// KindPlatform is a platform that cannot be reached or that answers
	// with an error that is not about the token, a rate limit included.
	KindPlatform
	// KindState is a run that is missing, in a phase the command does not
	// fit, or whose state file cannot be read.
	KindState
	// KindValidation is a file the agent wrote that does not match its
	// schema or the work package: every problem is in Details.
	KindValidation
	// KindHeadChanged is a change request that has new commits since it
	// was reviewed or planned: nothing is written for it.
	KindHeadChanged
	// KindPath is a path the command may not use: a folder for saved
	// reports inside a git work tree, with "..", or that cannot be
	// written.
	KindPath
)

// Failure is why a command stopped. Its report has a fixed shape: what
// failed, where, why, what to do, and the run log when a run exists.
type Failure struct {
	Kind Kind
	// What failed, as a short sentence.
	What string
	// Where: the change request and the operation, when there is one.
	Where string
	// Why: the status or the reason the platform or the state gave.
	Why string
	// Hint is what the user can do about it.
	Hint string
	// Details are the lines of the problems the failure lists, one per
	// line, in the order the report shows them; a line is indented by the
	// report.
	Details []string
	// LogPath is the run log, when a run exists.
	LogPath string
	// Err is the cause.
	Err error
}

func (f *Failure) Error() string {
	if f.Why == "" {
		return f.What
	}
	return f.What + ": " + f.Why
}

// Unwrap returns the cause.
func (f *Failure) Unwrap() error { return f.Err }

// Report is the message of the failure for stderr: five labelled lines,
// the last only when a run log exists. It never holds a token: the causes
// it is made from are errors of the transport, which redacts them.
func (f *Failure) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent-code-review: %s\n", oneLine(f.What))
	fmt.Fprintf(&b, "  where: %s\n", orNone(oneLine(f.Where)))
	fmt.Fprintf(&b, "  why: %s\n", orNone(oneLine(f.Why)))
	if len(f.Details) > 0 {
		b.WriteString("  problems:\n")
		for _, d := range f.Details {
			// The indentation is the caller's; the text is one line, whatever
			// it was made from.
			text := strings.TrimLeft(d, " ")
			fmt.Fprintf(&b, "    %s%s\n", d[:len(d)-len(text)], oneLine(text))
		}
	}
	fmt.Fprintf(&b, "  what to do: %s\n", orNone(oneLine(f.Hint)))
	if f.LogPath != "" {
		fmt.Fprintf(&b, "  log: %s\n", f.LogPath)
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// oneLine puts a message on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

var statusPattern = regexp.MustCompile(`\bstatus ([1-5][0-9][0-9])\b`)

// classify turns a failed read into a failure. what says what was being
// done and where the change request and operation; host is the host of the
// platform, for the name of the variable that may hold the token.
func classify(err error, host, what, where string) *Failure {
	f := &Failure{Kind: KindPlatform, What: what, Where: where, Err: err}
	var be *base.Error
	if errors.As(err, &be) && be.Op != "" {
		f.Where = where + ", " + be.Op
	}
	f.Why = whyOf(err)
	var re *cascade.RootError
	switch {
	case errors.As(err, &re) && re.Kind == cascade.KindIssue:
		f.Kind = KindUsage
		f.Hint = "give the address or owner/repo#N of a pull request, not of an issue"
	case errors.Is(err, auth.ErrNoToken):
		f.Kind = KindAuth
		f.Hint = "run " + loginCommand(host)
	case errors.Is(err, base.ErrForbidden):
		f.Kind = KindAuth
		if perms := base.PermissionsOf(err); len(perms) > 0 {
			f.Hint = "the token lacks " + strings.Join(perms, ", ") + "; add it to the fine-grained token, or run " + loginCommand(host) + " to store another"
		} else {
			f.Hint = "the token was refused or lacks a permission; check it, or run " + loginCommand(host) + " to store a valid one"
		}
	case errors.Is(err, base.ErrRateLimited):
		f.Hint = "the rate limit does not reset within the longest wait; run the command again later"
	case errors.Is(err, base.ErrNotFound), errors.As(err, &re):
		f.Hint = "check the reference, and that the token can read the repository"
	case errors.Is(err, base.ErrListTruncated):
		f.Hint = "the platform lists fewer files than the change request changes, so it cannot be reviewed in full; split it"
	default:
		f.Hint = "check the network and run the command again; the run log lists the requests"
	}
	return f
}

// loginCommand is the command that stores a token for host, and the
// variable that may hold it.
func loginCommand(host string) string {
	cmd := "`agent-code-review auth login`"
	if host != "" && host != "github.com" {
		cmd = "`agent-code-review auth login --host " + host + "`"
	}
	if v := auth.EnvVar(host); v != "" {
		cmd += " or set " + v
	}
	return cmd
}

// whyOf is the reason of a failed read: its kind, the permissions it
// needed, the HTTP status when the platform gave one. The text of the
// platform's answer is left out.
func whyOf(err error) string {
	var re *cascade.RootError
	switch {
	case errors.As(err, &re):
		return re.Error()
	case errors.Is(err, auth.ErrNoToken):
		return "no token"
	}
	reason := base.ReasonOf(err)
	if reason == "" {
		// Not a read: one of our own checks, which says what it found.
		return oneLine(err.Error())
	}
	why := reason
	if perms := base.PermissionsOf(err); len(perms) > 0 {
		why += " (needs " + strings.Join(perms, ", ") + ")"
	}
	if m := statusPattern.FindStringSubmatch(err.Error()); m != nil {
		why += ", status " + m[1]
	}
	return why
}

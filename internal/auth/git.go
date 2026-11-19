package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultGitTimeout bounds each git command when GitRunner.Timeout is not
// set: a credential helper waiting for a graphical unlock must not hang the
// command.
const DefaultGitTimeout = 30 * time.Second

// killDelay is how long Run waits, after killing git, for the pipes that a
// helper started by git may still hold open.
const killDelay = 2 * time.Second

// TimeoutError is a git command that did not finish in time and was killed.
type TimeoutError struct{ After time.Duration }

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("git did not answer within %s; a credential helper may be waiting for an unlock or a confirmation", e.After)
}

// nonInteractive keeps git and the credential helpers from prompting: no
// terminal prompt, no askpass program, no Git Credential Manager dialog.
var nonInteractive = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=",
	"SSH_ASKPASS=",
	"GCM_INTERACTIVE=never",
	// The runner reads git's messages to tell a missing credential from a
	// failing helper, so they must not be translated.
	"LC_ALL=C",
}

// GitRunner runs the git found on PATH, without a shell and without prompts.
type GitRunner struct {
	// Dir is the working directory; "" means the current one.
	Dir string
	// Timeout bounds each command; 0 or less means DefaultGitTimeout.
	Timeout time.Duration
	// program replaces git in tests; "" means git.
	program string
}

func (g GitRunner) timeout() time.Duration {
	if g.Timeout <= 0 {
		return DefaultGitTimeout
	}
	return g.Timeout
}

// Run runs git with args, feeding stdin. A non-zero exit is an *ExitError;
// a command that outlives the timeout is killed and gives a *TimeoutError.
func (g GitRunner) Run(parent context.Context, stdin string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(parent, g.timeout())
	defer cancel()
	program := g.program
	if program == "" {
		program = "git"
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.WaitDelay = killDelay
	cmd.Dir = g.Dir
	cmd.Env = append(os.Environ(), nonInteractive...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	switch {
	case err == nil:
	case parent.Err() != nil:
		err = parent.Err()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = &TimeoutError{After: g.timeout()}
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = &ExitError{Code: exit.ExitCode()}
		}
	}
	return stdout.String(), stderr.String(), err
}

// Package cli implements the agent-code-review command line.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/version"
)

// Exit codes, as listed in docs/exit-codes.md.
const (
	exitOK            = 0
	exitUnexpected    = 1
	exitUsage         = 2
	exitAuth          = 6
	exitUnsafeStorage = 10
	exitInterrupted   = 130
)

const usage = `usage: agent-code-review <command>

commands:
  version | --version            print the version
  auth login [--host <host>]     store a token read from stdin
  auth status [--host <host>]    say whether a token is available, and from where
`

// env is what a command may use from the outside world.
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	getenv         func(string) string
	git            auth.Runner
	term           terminal
	// notify relays interrupt signals to c until the returned stop is called.
	notify func(c chan<- os.Signal) (stop func())
}

// Run executes the command in args (program name excluded) and returns its exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(args, env{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		getenv: os.Getenv,
		git:    auth.GitRunner{},
		term:   systemTerminal{},
		notify: notifyInterrupts,
	})
}

func run(args []string, e env) int {
	switch {
	case len(args) == 1 && (args[0] == "version" || args[0] == "--version"):
		fmt.Fprintln(e.stdout, version.Version)
		return exitOK
	case len(args) >= 1 && args[0] == "auth":
		return runAuth(args[1:], e)
	}
	fmt.Fprint(e.stderr, usage)
	return exitUsage
}

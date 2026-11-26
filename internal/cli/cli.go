// Package cli implements the agent-code-review command line.
package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/version"
)

// Exit codes, as listed in docs/exit-codes.md.
const (
	exitOK            = 0
	exitUnexpected    = 1
	exitUsage         = 2
	exitValidation    = 3
	exitHeadChanged   = 4
	exitAuth          = 6
	exitPlatform      = 7
	exitState         = 8
	exitPath          = 9
	exitUnsafeStorage = 10
	exitInterrupted   = 130
)

const usage = `usage: agent-code-review <command>

commands:
  version | --version            print the version
  review [--verbose] <pull request>
                                 collect a pull request and its related changes
  next [--verbose] [--run <id>]  print the next part to judge
  finalize [--verbose] [--run <id>]
                                 validate the judgements and write the reports
  show [--verbose] [--run <id>] [<cr>]
                                 print the summary of the cascade, or a report
  show --save [<folder>] [<cr>]  save a report outside any git work tree
  publish --plan <comments|issues|both> [--run <id>]
                                 plan what to write to the platform; writes nothing there
  publish --apply [--run <id>]   write exactly the plans, only after the user said yes
  queue [--limit N]              list the pull requests that ask for your review
  doctor [--repo owner/repo]     check the environment, the token and the packs
  check-criteria [<dir>]         check the repository criteria of a working copy
  auth login [--host <host>]     store a token read from stdin
  auth status [--host <host>]    say whether a token is available, and from where
  auth logout [--host <host>]    remove the stored token
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

	// The rest has a default, which tests replace: the clock, the source
	// of the random part of run ids, the reader of the platform, and where
	// the user cache and configuration live.
	now           func() time.Time
	random        io.Reader
	newReader     func(host string, tokens github.Resolver, opts github.Options) (base.ForgeProvider, error)
	userCacheDir  func() (string, error)
	userConfigDir func() (string, error)
	// userHomeDir is where reports are saved when no folder is named.
	userHomeDir func() (string, error)
	// goos names the operating system the advice on credential helpers
	// is for; "" means runtime.GOOS.
	goos string
}

// dirs finds the directories of the user in the environment of the command.
func (e env) dirs() userDirs { return userDirs{getenv: e.getenv, goos: runtime.GOOS} }

// cacheDir, configDir and homeDir are the user directories, from the
// injected function when there is one, else from the environment.
func (e env) cacheDir() func() (string, error) {
	if e.userCacheDir != nil {
		return e.userCacheDir
	}
	return e.dirs().cache
}

func (e env) configDir() func() (string, error) {
	if e.userConfigDir != nil {
		return e.userConfigDir
	}
	return e.dirs().config
}

func (e env) homeDir() func() (string, error) {
	if e.userHomeDir != nil {
		return e.userHomeDir
	}
	return e.dirs().home
}

// hostOS is the operating system the advice on credential helpers is for.
func (e env) hostOS() string {
	if e.goos == "" {
		return runtime.GOOS
	}
	return e.goos
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
	case len(args) >= 1 && args[0] == "review":
		return runReview(args[1:], e)
	case len(args) >= 1 && args[0] == "next":
		return runNext(args[1:], e)
	case len(args) >= 1 && args[0] == "finalize":
		return runFinalize(args[1:], e)
	case len(args) >= 1 && args[0] == "show":
		return runShow(args[1:], e)
	case len(args) >= 1 && args[0] == "publish":
		return runPublish(args[1:], e)
	case len(args) >= 1 && args[0] == "queue":
		return runQueue(args[1:], e)
	case len(args) >= 1 && args[0] == "doctor":
		return runDoctor(args[1:], e)
	case len(args) >= 1 && args[0] == "check-criteria":
		return runCheckCriteria(args[1:], e)
	}
	fmt.Fprint(e.stderr, usage)
	return exitUsage
}

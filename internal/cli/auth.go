package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Brg92/agent-code-review/internal/auth"
)

const authUsage = `usage: agent-code-review auth <command> [--host <host>]

commands:
  login     store a token for the host in the git credential helper
  status    say whether a token is available, and from where
  logout    remove the stored token for the host

--host defaults to github.com. login reads the token from stdin: hidden
when typed at a terminal, or one line piped from another program. It is
never taken from the command line.
`

const defaultHost = "github.com"

// runAuth dispatches the auth subcommands.
func runAuth(args []string, e env) int {
	if len(args) == 0 {
		return authUsageError(e, "missing auth command")
	}
	switch args[0] {
	case "-h", "-help", "--help":
		fmt.Fprint(e.stdout, authUsage)
		return exitOK
	case "login":
		return withHost(args, e, authLogin)
	case "status":
		return withHost(args, e, authStatus)
	case "logout":
		return withHost(args, e, authLogout)
	}
	return authUsageError(e, fmt.Sprintf("unknown auth command %q", args[0]))
}

// withHost parses [--host <host>] for subcommand args[0] and runs cmd.
func withHost(args []string, e env, cmd func(e env, host string) int) int {
	fs := flag.NewFlagSet("auth "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", defaultHost, "")
	// Messages never quote what was given: it may be a misplaced token.
	const fromStdin = " (the token is read from stdin, never from the command line)"
	err := fs.Parse(args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprint(e.stdout, authUsage)
		return exitOK
	case err != nil && strings.HasPrefix(err.Error(), "flag needs an argument"):
		return authUsageError(e, err.Error())
	case err != nil:
		return authUsageError(e, "unknown flag"+fromStdin)
	case fs.NArg() > 0:
		return authUsageError(e, "unexpected argument"+fromStdin)
	}
	if auth.ValidateHost(*host) != nil {
		return authUsageError(e, "invalid --host value: want a host name such as github.com, with an optional :port")
	}
	return cmd(e, *host)
}

func authUsageError(e env, msg string) int {
	fmt.Fprintf(e.stderr, "agent-code-review auth: %s\n%s", msg, authUsage)
	return exitUsage
}

func (e env) resolver() auth.Resolver { return auth.Resolver{Getenv: e.getenv, Git: e.git} }

// fail prints an error and returns code.
func fail(e env, code int, format string, a ...any) int {
	fmt.Fprintf(e.stderr, "agent-code-review: "+format+"\n", a...)
	return code
}

// loginHint is the command that stores a token for host.
func loginHint(host string) string {
	cmd := "`agent-code-review auth login`"
	if host != defaultHost {
		cmd = "`agent-code-review auth login --host " + host + "`"
	}
	if v := auth.EnvVar(host); v != "" {
		return cmd + " or set " + v
	}
	return cmd
}

func authStatus(e env, host string) int {
	tok, err := e.resolver().Resolve(context.Background(), host)
	switch {
	case errors.Is(err, auth.ErrNoToken):
		return fail(e, exitAuth, "%s: no token; run %s", host, loginHint(host))
	case err != nil:
		return fail(e, exitAuth, "%s: %v", host, err)
	}
	fmt.Fprintf(e.stdout, "%s: token available from %s\n", host, tok.Describe())
	return exitOK
}

func authLogout(e env, host string) int {
	if err := e.resolver().Erase(context.Background(), host); err != nil {
		return fail(e, exitAuth, "%s: %v", host, err)
	}
	if v := auth.EnvVar(host); v != "" && strings.TrimSpace(e.getenv(v)) != "" {
		fmt.Fprintf(e.stderr, "agent-code-review: warning: %s is still set and keeps providing a token\n", v)
	}
	fmt.Fprintf(e.stdout, "%s: token removed from the git credential helper (username %s)\n", host, auth.Username)
	return exitOK
}

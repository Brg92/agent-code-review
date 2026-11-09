package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Brg92/agent-code-review/internal/auth"
)

const authUsage = `usage: agent-code-review auth <command> [--host <host>]

commands:
  status    say whether a token is available, and from where

--host defaults to github.com.
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
	case "status":
		return withHost(args, e, authStatus)
	}
	return authUsageError(e, fmt.Sprintf("unknown auth command %q", args[0]))
}

// withHost parses [--host <host>] for subcommand args[0] and runs cmd.
func withHost(args []string, e env, cmd func(e env, host string) int) int {
	fs := flag.NewFlagSet("auth "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", defaultHost, "")
	if err := fs.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(e.stdout, authUsage)
		return exitOK
	} else if err != nil {
		return authUsageError(e, err.Error())
	}
	if fs.NArg() > 0 {
		return authUsageError(e, fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	if err := auth.ValidateHost(*host); err != nil {
		return authUsageError(e, err.Error())
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

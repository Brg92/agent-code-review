// Package cli implements the agent-code-review command line.
package cli

import (
	"fmt"
	"io"

	"github.com/Brg92/agent-code-review/internal/version"
)

const (
	exitOK    = 0
	exitUsage = 2
)

const usage = "usage: agent-code-review <version | --version>\n"

// Run executes the command in args (program name excluded) and returns its exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Fprintln(stdout, version.Version)
		return exitOK
	}
	fmt.Fprint(stderr, usage)
	return exitUsage
}

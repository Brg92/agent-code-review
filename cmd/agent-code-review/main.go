// Command agent-code-review reviews change requests and their related changes.
package main

import (
	"os"

	"github.com/Brg92/agent-code-review/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

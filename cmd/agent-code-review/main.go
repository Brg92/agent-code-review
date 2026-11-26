// Command agent-code-review reviews change requests and their related changes.
package main

import (
	"context"
	"os"

	"github.com/Brg92/agent-code-review/internal/cli"
)

// main is the only place that reads the arguments, the environment and the
// standard files of the process; everything else gets them as parameters.
func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], cli.EnvMap(os.Environ()), os.Stdin, os.Stdout, os.Stderr))
}

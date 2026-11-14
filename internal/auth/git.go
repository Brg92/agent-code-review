package auth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// nonInteractive keeps git and the credential helpers from prompting: no
// terminal prompt, no askpass program, no Git Credential Manager dialog.
var nonInteractive = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_ASKPASS=",
	"SSH_ASKPASS=",
	"GCM_INTERACTIVE=never",
}

// GitRunner runs the git found on PATH, without a shell and without prompts.
type GitRunner struct {
	// Dir is the working directory; "" means the current one.
	Dir string
}

// Run runs git with args, feeding stdin. A non-zero exit is an *ExitError.
func (g GitRunner) Run(ctx context.Context, stdin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	cmd.Env = append(os.Environ(), nonInteractive...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = &ExitError{Code: exit.ExitCode()}
	}
	return stdout.String(), stderr.String(), err
}

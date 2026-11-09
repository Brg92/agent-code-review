package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ExitError is a git command that ran and exited with a non-zero status.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("git exited with status %d", e.Code) }

// credentialInput is the git credential protocol description of the tool's
// entry for host, optionally with the token.
func credentialInput(host, token string) string {
	var b strings.Builder
	b.WriteString("protocol=https\nhost=" + host + "\nusername=" + Username + "\n")
	if token != "" {
		b.WriteString("password=" + token + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// parseCredential reads key=value lines up to the first blank line.
func parseCredential(out string) map[string]string {
	attrs := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			attrs[k] = v
		}
	}
	return attrs
}

// fill asks the credential helpers for the tool's token on host.
func (r Resolver) fill(ctx context.Context, host string) (Token, error) {
	stdout, _, err := r.Git.Run(ctx, credentialInput(host, ""), "credential", "fill")
	var exit *ExitError
	switch {
	case errors.As(err, &exit):
		// With prompts disabled, git fails when no helper has the entry.
		return Token{}, ErrNoToken
	case err != nil:
		return Token{}, fmt.Errorf("auth: git credential fill: %w", err)
	}
	attrs := parseCredential(stdout)
	if attrs["username"] != Username || attrs["password"] == "" {
		return Token{}, ErrNoToken
	}
	return Token{value: attrs["password"], Source: SourceCredentialHelper}, nil
}

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

// ErrInvalidToken means the token contains whitespace or control characters.
var ErrInvalidToken = errors.New("auth: the token contains whitespace or control characters")

// normalizeToken trims surrounding whitespace and rejects anything that
// could break the git credential protocol.
func normalizeToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrNoToken
	}
	for _, r := range token {
		if r <= ' ' || r == 0x7f {
			return "", ErrInvalidToken
		}
	}
	return token, nil
}

// Save stores token for host in the git credential helpers under the username
// acr-api, after the helper check, and reads it back to confirm a helper
// kept it. The returned Check carries the warnings to show.
func (r Resolver) Save(ctx context.Context, host, token string) (Check, error) {
	token, err := normalizeToken(token)
	if err != nil {
		return Check{}, err
	}
	check, err := r.CheckHelpers(ctx, host)
	if err != nil {
		return check, err
	}
	if err := check.Err(); err != nil {
		return check, err
	}
	// stderr is not quoted: a helper could echo what it received.
	if _, _, err := r.Git.Run(ctx, credentialInput(host, token), "credential", "approve"); err != nil {
		return check, fmt.Errorf("auth: git credential approve: %w", err)
	}
	stored, err := r.fill(ctx, host)
	if err != nil || stored.value != token {
		return check, errors.New("auth: the git credential helper did not keep the token; check its configuration")
	}
	return check, nil
}

// Erase removes the acr-api entry for host from the git credential helpers.
func (r Resolver) Erase(ctx context.Context, host string) error {
	if err := ValidateHost(host); err != nil {
		return err
	}
	if _, _, err := r.Git.Run(ctx, credentialInput(host, ""), "credential", "reject"); err != nil {
		return fmt.Errorf("auth: git credential reject: %w", err)
	}
	return nil
}

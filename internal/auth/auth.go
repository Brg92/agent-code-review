// Package auth resolves the platform token: from an environment variable
// (intended for CI) or from the OS keychain through `git credential`, under
// the dedicated username acr-api so the user's own git credentials for the
// same host are never read or overwritten.
//
// The token value stays in memory. It is never printed, logged, written to
// disk or put in an error; only its source is reported.
package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Username is the git credential username the tool stores its token under.
const Username = "acr-api"

// Source says where a token came from.
type Source string

// Token sources.
const (
	SourceEnvironment      Source = "environment"
	SourceCredentialHelper Source = "credential helper"
)

// ErrNoToken means no token is configured for the host.
var ErrNoToken = errors.New("auth: no token")

// redacted is printed in place of a token.
const redacted = "[REDACTED]"

// Token is a resolved token. Its value is reachable only through Value; every
// fmt verb prints a redaction marker instead.
type Token struct {
	value string
	// Source is where the token came from.
	Source Source
	// EnvVar names the variable for SourceEnvironment.
	EnvVar string
}

// Value returns the secret. Pass it only to the transport.
func (t Token) Value() string { return t.value }

// Format implements fmt.Formatter so that no verb prints the value.
func (t Token) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "auth.Token{Source: %q, EnvVar: %q, value: %s}", string(t.Source), t.EnvVar, redacted)
}

// Describe names the source for users, never the value.
func (t Token) Describe() string {
	if t.Source == SourceEnvironment {
		return "the environment variable " + t.EnvVar
	}
	return "the git credential helper (username " + Username + ")"
}

// envVars maps a host to the environment variable holding its CI token.
var envVars = map[string]string{"github.com": "ACR_GITHUB_TOKEN"}

// EnvVar returns the environment variable read for host, or "".
func EnvVar(host string) string { return envVars[strings.ToLower(host)] }

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*(:[0-9]{1,5})?$`)

// ValidateHost accepts a host name with an optional port, nothing else. It
// keeps newlines and URL syntax out of the git credential protocol.
func ValidateHost(host string) error {
	if !hostPattern.MatchString(host) {
		return fmt.Errorf("auth: invalid host %q: want a host name such as github.com, with an optional :port", host)
	}
	return nil
}

// Runner runs git with the given stdin and arguments.
type Runner interface {
	Run(ctx context.Context, stdin string, args ...string) (stdout, stderr string, err error)
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(ctx context.Context, stdin string, args ...string) (string, string, error)

// Run calls f.
func (f RunnerFunc) Run(ctx context.Context, stdin string, args ...string) (string, string, error) {
	return f(ctx, stdin, args...)
}

// Resolver finds, stores and erases tokens.
type Resolver struct {
	// Getenv reads the environment, usually os.Getenv.
	Getenv func(string) string
	// Git runs git, usually a GitRunner.
	Git Runner
}

// Resolve returns the token for host: from its environment variable first,
// then from the git credential helper. It returns ErrNoToken if neither has one.
func (r Resolver) Resolve(ctx context.Context, host string) (Token, error) {
	if err := ValidateHost(host); err != nil {
		return Token{}, err
	}
	if name := EnvVar(host); name != "" {
		if v := strings.TrimSpace(r.Getenv(name)); v != "" {
			return Token{value: v, Source: SourceEnvironment, EnvVar: name}, nil
		}
	}
	return r.fill(ctx, host)
}

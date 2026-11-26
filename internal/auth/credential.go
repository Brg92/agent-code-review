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

// HelperError means git or a credential helper failed: git is missing, it
// timed out, or it or a helper exited with an error. It is not "no
// credential stored", which is ErrNoToken. It never quotes what git printed:
// a helper could echo what it received.
type HelperError struct {
	// Op is the git command, such as "git credential fill".
	Op  string
	Err error
}

func (e *HelperError) Error() string { return fmt.Sprintf("auth: %s: %v", e.Op, e.Err) }

func (e *HelperError) Unwrap() error { return e.Err }

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

// noCredential reports whether a failed `git credential fill` only says that
// no helper has the entry. With prompts disabled git then ends with
// "fatal: could not read <Username|Password> for '<url>': terminal prompts
// disabled" and status 128. Any other line, or another status, means git
// or a helper failed: a locked keychain, for example, prints its own error
// before that fatal line.
func noCredential(exit *ExitError, stderr string) bool {
	if exit.Code != 128 {
		return false
	}
	seen := false
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "fatal: could not read ") && strings.HasSuffix(line, ": terminal prompts disabled"):
			seen = true
		default:
			return false
		}
	}
	return seen
}

// ask runs `git credential fill` for the tool's entry on host and returns
// the username and the password git got from the helpers. It returns
// ErrNoToken when no helper has the entry, and a *HelperError when git or a
// helper failed.
func (r Resolver) ask(ctx context.Context, host string) (username, password string, err error) {
	stdout, stderr, err := r.Git.Run(ctx, credentialInput(host, ""), "credential", "fill")
	var exit *ExitError
	switch {
	case errors.As(err, &exit) && noCredential(exit, stderr):
		return "", "", ErrNoToken
	case err != nil:
		return "", "", &HelperError{Op: "git credential fill", Err: err}
	}
	attrs := parseCredential(stdout)
	return attrs["username"], attrs["password"], nil
}

// fill asks the credential helpers for the tool's token on host.
func (r Resolver) fill(ctx context.Context, host string) (Token, error) {
	username, password, err := r.ask(ctx, host)
	if err != nil {
		return Token{}, err
	}
	if username != Username || password == "" {
		return Token{}, ErrNoToken
	}
	value, err := normalizeToken(password)
	if err != nil {
		return Token{}, fmt.Errorf("the git credential helper: %w", err)
	}
	return Token{value: value, Source: SourceCredentialHelper}, nil
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

// ErrNotStored means git accepted the token but no credential helper kept
// it. It is the error of a *NotStoredError.
var ErrNotStored = errors.New("auth: no git credential helper kept the token")

// NotStoredReason says what git answered when the token was read back.
type NotStoredReason int

// Reasons a token was not kept.
const (
	// NotStoredNothing: no helper had a token for acr-api.
	NotStoredNothing NotStoredReason = iota + 1
	// NotStoredOtherToken: a helper had another token for acr-api.
	NotStoredOtherToken
	// NotStoredOtherAccount: a helper answered with a token of another
	// account, for example the helper of the GitHub CLI.
	NotStoredOtherAccount
)

// PreviousToken says what became of the token acr-api had before a login
// that failed.
type PreviousToken int

// What became of the previous token.
const (
	// PreviousNone: there was none.
	PreviousNone PreviousToken = iota
	// PreviousKept: it was put back, and read again to make sure.
	PreviousKept
	// PreviousLost: it could not be put back.
	PreviousLost
)

// NotStoredError is the helpers' answer to reading back a token that was
// just approved. It never quotes a token or a username.
type NotStoredError struct {
	Reason NotStoredReason
	// Previous says what became of the token acr-api had before.
	Previous PreviousToken
}

func (e *NotStoredError) Error() string {
	switch e.Reason {
	case NotStoredOtherToken:
		return "auth: git accepted the token, but reading it back returned a different token"
	case NotStoredOtherAccount:
		return "auth: git accepted the token, but reading it back returned a token of another account: a helper that is not meant for " +
			Username + " answers for this host"
	}
	return "auth: git accepted the token, but reading it back returned nothing: no configured helper keeps it"
}

// Is makes errors.Is(err, ErrNotStored) true.
func (e *NotStoredError) Is(target error) bool { return target == ErrNotStored }

// Save stores token for host in the git credential helpers under the username
// acr-api, after the helper check, and reads it back to confirm a helper
// kept it. If none did, it rejects what it approved, so no half-stored
// credential stays, puts back the token acr-api had before, if any, and
// returns a *NotStoredError that says whether it did. The returned Check
// carries the warnings to show.
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
	previous, err := r.previous(ctx, host)
	if err != nil {
		return check, err
	}
	// stderr is not quoted: a helper could echo what it received.
	if _, _, err := r.Git.Run(ctx, credentialInput(host, token), "credential", "approve"); err != nil {
		return check, &HelperError{Op: "git credential approve", Err: err}
	}
	username, password, err := r.ask(ctx, host)
	switch {
	case errors.Is(err, ErrNoToken):
		err = &NotStoredError{Reason: NotStoredNothing}
	case err == nil:
		err = readBack(username, password, token)
	}
	if err != nil {
		// Best effort: the error to report is why the token was not kept.
		// Rejecting removes the entry from every helper, the previous token
		// included, so it is put back.
		_ = r.Erase(ctx, host)
		restored := r.restore(ctx, host, previous)
		var ns *NotStoredError
		if errors.As(err, &ns) {
			ns.Previous = restored
		}
		return check, err
	}
	return check, nil
}

// previous reads, in memory, the token acr-api has now: "" if there is
// none, or if what git returns is not acr-api's. A failing git or helper is
// an error: nothing may be changed that cannot be put back.
func (r Resolver) previous(ctx context.Context, host string) (string, error) {
	username, password, err := r.ask(ctx, host)
	switch {
	case errors.Is(err, ErrNoToken):
		return "", nil
	case err != nil:
		return "", err
	case username != Username:
		return "", nil
	}
	if value, err := normalizeToken(password); err == nil {
		return value, nil
	}
	return "", nil
}

// restore approves the previous token again and reads it back.
func (r Resolver) restore(ctx context.Context, host, previous string) PreviousToken {
	if previous == "" {
		return PreviousNone
	}
	if _, _, err := r.Git.Run(ctx, credentialInput(host, previous), "credential", "approve"); err != nil {
		return PreviousLost
	}
	username, password, err := r.ask(ctx, host)
	if got, nerr := normalizeToken(password); err != nil || nerr != nil || username != Username || got != previous {
		return PreviousLost
	}
	return PreviousKept
}

// readBack compares, in memory, what the helpers returned with the token
// that was approved.
func readBack(username, password, token string) error {
	switch {
	case password == "":
		return &NotStoredError{Reason: NotStoredNothing}
	case username != Username:
		return &NotStoredError{Reason: NotStoredOtherAccount}
	}
	if got, err := normalizeToken(password); err != nil || got != token {
		return &NotStoredError{Reason: NotStoredOtherToken}
	}
	return nil
}

// Erase removes the acr-api entry for host from the git credential helpers.
func (r Resolver) Erase(ctx context.Context, host string) error {
	if err := ValidateHost(host); err != nil {
		return err
	}
	if _, _, err := r.Git.Run(ctx, credentialInput(host, ""), "credential", "reject"); err != nil {
		return &HelperError{Op: "git credential reject", Err: err}
	}
	return nil
}

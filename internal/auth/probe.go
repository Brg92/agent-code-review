package auth

import (
	"context"
	"errors"
	"strings"
)

// Probe is what `git credential fill` answers for acr-api on a host. It
// holds no token: only whether one came back.
type Probe struct {
	// Username is the username git ended up with: acr-api, unless a helper
	// answered with another one.
	Username string
	// HasToken says that a password came back.
	HasToken bool
}

// OtherAccount reports whether a helper answered with a token of another
// account, which a helper that was not chosen for acr-api does, as the
// helper of the GitHub CLI does for github.com.
func (p Probe) OtherAccount() bool { return p.HasToken && p.Username != Username }

// Probe asks the credential helpers for the tool's entry on host and says
// what came back, without the token. Nothing coming back is not an error; a
// failing git or helper is a *HelperError.
func (r Resolver) Probe(ctx context.Context, host string) (Probe, error) {
	if err := ValidateHost(host); err != nil {
		return Probe{}, err
	}
	username, password, err := r.ask(ctx, host)
	if errors.Is(err, ErrNoToken) {
		return Probe{}, nil
	}
	if err != nil {
		return Probe{}, err
	}
	return Probe{Username: username, HasToken: password != ""}, nil
}

// SuggestedHelper is the credential helper that keeps secrets in the
// credential store of the operating system goos names.
func SuggestedHelper(goos string) string {
	switch goos {
	case "darwin":
		return "osxkeychain"
	case "windows":
		return "manager"
	}
	return "libsecret"
}

// SetupArgs are the git arguments that give acr-api its own credential
// helper on host. The empty value first resets the helpers that apply to
// the host, such as the one of the GitHub CLI; the section names acr-api, so
// the user's own git operations on the host keep their helpers.
func SetupArgs(host, helper string) [][]string {
	key := "credential.https://" + Username + "@" + host + ".helper"
	return [][]string{
		{"config", "--global", "--add", key, ""},
		{"config", "--global", "--add", key, helper},
	}
}

// SetupCommands are the SetupArgs as command lines to show to the user.
func SetupCommands(host, helper string) []string {
	var out []string
	for _, args := range SetupArgs(host, helper) {
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = a
			if a == "" {
				quoted[i] = `""`
			}
		}
		out = append(out, "git "+strings.Join(quoted, " "))
	}
	return out
}

package auth

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

// HelperKind classifies a git credential helper by where it keeps secrets.
type HelperKind int

// Helper kinds.
const (
	HelperUnknown HelperKind = iota
	HelperKeychain
	HelperPlainText
)

func (k HelperKind) String() string {
	switch k {
	case HelperKeychain:
		return "OS keychain"
	case HelperPlainText:
		return "plain text"
	}
	return "unknown"
}

// keychainHelpers keep secrets in the operating system's credential store.
var keychainHelpers = map[string]bool{
	"osxkeychain":   true, // macOS Keychain
	"manager":       true, // Git Credential Manager
	"manager-core":  true, // Git Credential Manager, older name
	"wincred":       true, // Windows Credential Manager
	"libsecret":     true, // GNOME Keyring, KWallet through Secret Service
	"gnome-keyring": true, // older Secret Service helper
}

// classifyHelper classifies a credential.helper value.
func classifyHelper(value string) HelperKind {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "!") {
		return HelperUnknown
	}
	name := strings.Fields(value)[0]
	if q := value[0]; q == '"' || q == '\'' {
		end := strings.IndexByte(value[1:], q)
		if end < 0 {
			return HelperUnknown
		}
		name = value[1 : end+1]
	}
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	name = strings.TrimPrefix(name, "git-credential-")
	switch {
	case name == "store":
		return HelperPlainText
	case keychainHelpers[name]:
		return HelperKeychain
	}
	return HelperUnknown
}

// Helper is one configured credential helper.
type Helper struct {
	Value string
	Kind  HelperKind
}

// Check is the credential helper set git would use for a host.
type Check struct {
	Helpers []Helper
}

var (
	// ErrNoHelper means git has no credential helper, so it would store nothing.
	ErrNoHelper = errors.New("auth: no git credential helper is configured")
	// ErrPlainTextHelper means a helper would write the token to disk in plain text.
	ErrPlainTextHelper = errors.New("auth: the git credential helper \"store\" keeps tokens in plain text on disk")
)

const helperHint = "configure an OS keychain helper, for example `git config --global credential.helper osxkeychain` (macOS), " +
	"`git config --global credential.helper manager` (Git Credential Manager, Windows and others) or " +
	"`git config --global credential.helper libsecret` (Linux)"

// Err reports why the token must not be stored, or nil.
func (c Check) Err() error {
	if len(c.Helpers) == 0 {
		return fmt.Errorf("%w; %s", ErrNoHelper, helperHint)
	}
	for _, h := range c.Helpers {
		if h.Kind == HelperPlainText {
			return fmt.Errorf("%w, and git hands a stored token to every configured helper; "+
				"remove it (`git config --global --unset-all credential.helper store`) and %s", ErrPlainTextHelper, helperHint)
		}
	}
	return nil
}

// Warnings lists helpers whose storage is unknown.
func (c Check) Warnings() []string {
	var out []string
	for _, h := range c.Helpers {
		if h.Kind == HelperUnknown {
			out = append(out, fmt.Sprintf("unknown git credential helper %q: make sure it keeps secrets encrypted, not in plain text", h.Value))
		}
	}
	return out
}

// CheckHelpers reads the credential helpers git would use for host, in
// configuration order, honouring an empty value that resets the list.
func (r Resolver) CheckHelpers(ctx context.Context, host string) (Check, error) {
	if err := ValidateHost(host); err != nil {
		return Check{}, err
	}
	out, _, err := r.Git.Run(ctx, "", "config", "--null", "--get-regexp", `^credential\.(.*\.)?helper$`)
	var exit *ExitError
	if errors.As(err, &exit) && exit.Code == 1 {
		return Check{}, nil // no matching key
	}
	if err != nil {
		return Check{}, fmt.Errorf("auth: reading credential helpers: %w", err)
	}
	var helpers []Helper
	for _, entry := range strings.Split(out, "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		if !ok || !helperKeyApplies(key, host) {
			continue
		}
		if value == "" {
			helpers = nil
			continue
		}
		helpers = append(helpers, Helper{Value: value, Kind: classifyHelper(value)})
	}
	return Check{Helpers: helpers}, nil
}

// helperKeyApplies reports whether a credential.[<url>.]helper key applies
// to https://acr-api@host, following git's matching of partial URLs.
func helperKeyApplies(key, host string) bool {
	rest, ok := strings.CutPrefix(key, "credential.")
	if !ok {
		return false
	}
	pattern, ok := strings.CutSuffix(rest, "helper")
	if !ok {
		return false
	}
	if pattern == "" {
		return true
	}
	pattern = strings.TrimSuffix(pattern, ".")
	if scheme, after, found := strings.Cut(pattern, "://"); found {
		if !strings.EqualFold(scheme, "https") {
			return false
		}
		pattern = after
	}
	if p, extra, found := strings.Cut(pattern, "/"); found {
		if extra != "" {
			return false // a path never matches the tool's host-only entry
		}
		pattern = p
	}
	if user, h, found := strings.Cut(pattern, "@"); found {
		if user != Username {
			return false
		}
		pattern = h
	}
	return hostMatches(pattern, host)
}

// hostMatches compares host names label by label; "*" matches one label.
func hostMatches(pattern, host string) bool {
	if pattern == "" {
		return true
	}
	pl := strings.Split(strings.ToLower(pattern), ".")
	hl := strings.Split(strings.ToLower(host), ".")
	if len(pl) != len(hl) {
		return false
	}
	for i := range pl {
		if pl[i] != "*" && pl[i] != hl[i] {
			return false
		}
	}
	return true
}

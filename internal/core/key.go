// Package core holds the neutral model of a review run: the change request
// key, the forge it lives on, the entries of a cascade and the cascade
// limits. It knows no platform API and no technology stack.
package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Forge is the platform that hosts change requests.
type Forge string

// The supported forges.
const (
	GitHub Forge = "github"
	GitLab Forge = "gitlab"
)

// ParseForge returns the forge named s; names are lowercase.
func ParseForge(s string) (Forge, error) {
	f := Forge(s)
	if !f.Valid() {
		return "", fmt.Errorf("unknown forge %q", s)
	}
	return f, nil
}

// Valid reports whether f is a supported forge.
func (f Forge) Valid() bool { return f == GitHub || f == GitLab }

// MarshalText implements encoding.TextMarshaler.
func (f Forge) MarshalText() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("unknown forge %q", string(f))
	}
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (f *Forge) UnmarshalText(b []byte) error {
	v, err := ParseForge(string(b))
	if err != nil {
		return err
	}
	*f = v
	return nil
}

// Key identifies a change request: <forge>:<host>/<project>#<number>.
type Key struct {
	Forge Forge
	// Host is lowercase and may end with :<port>.
	Host string
	// Project is the path of the project, with at least two segments;
	// GitLab groups may nest.
	Project string
	// Number is the change request number, at least 1.
	Number int
}

// KeyError reports a string that is not a change request key.
type KeyError struct {
	Key    string
	Reason string
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("invalid change request key %q: %s", e.Key, e.Reason)
}

var (
	hostPattern    = regexp.MustCompile(`^[a-z0-9.-]+(?::[0-9]+)?$`)
	projectPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+$`)
	numberPattern  = regexp.MustCompile(`^[1-9][0-9]*$`)
)

// ParseKey parses a change request key. The host is lowercased; everything
// else must already be in canonical form.
func ParseKey(s string) (Key, error) {
	fail := func(reason string) (Key, error) { return Key{}, &KeyError{Key: s, Reason: reason} }
	forge, rest, ok := strings.Cut(s, ":")
	if !ok {
		return fail("missing forge")
	}
	f, err := ParseForge(forge)
	if err != nil {
		return fail(err.Error())
	}
	rest, number, ok := strings.Cut(rest, "#")
	if !ok {
		return fail("missing number")
	}
	if !numberPattern.MatchString(number) {
		return fail("number must be a positive integer without leading zeros")
	}
	n, err := strconv.Atoi(number)
	if err != nil {
		return fail("number is too large")
	}
	host, project, ok := strings.Cut(rest, "/")
	if !ok {
		return fail("missing project")
	}
	host = strings.ToLower(host)
	if !hostPattern.MatchString(host) {
		return fail("invalid host")
	}
	if !projectPattern.MatchString(project) {
		return fail("project must be at least two path segments of letters, digits, '_', '.' or '-'")
	}
	return Key{Forge: f, Host: host, Project: project, Number: n}, nil
}

// String formats the key; it is the inverse of ParseKey.
func (k Key) String() string {
	return fmt.Sprintf("%s:%s/%s#%d", k.Forge, k.Host, k.Project, k.Number)
}

// ProjectKey formats the project of the key, without the number:
// <forge>:<host>/<project>.
func (k Key) ProjectKey() string {
	return fmt.Sprintf("%s:%s/%s", k.Forge, k.Host, k.Project)
}

// IsZero reports whether k is the zero key.
func (k Key) IsZero() bool { return k == Key{} }

// MarshalText implements encoding.TextMarshaler. It rejects a key that
// would not parse back.
func (k Key) MarshalText() ([]byte, error) {
	s := k.String()
	if parsed, err := ParseKey(s); err != nil || parsed != k {
		return nil, &KeyError{Key: s, Reason: "not in canonical form"}
	}
	return []byte(s), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (k *Key) UnmarshalText(b []byte) error {
	v, err := ParseKey(string(b))
	if err != nil {
		return err
	}
	*k = v
	return nil
}

// Same reports whether k and o name the same change request. Platforms
// ignore the case of project paths, so Same does too; the key itself keeps
// the spelling it was written with.
func (k Key) Same(o Key) bool {
	return k.Forge == o.Forge && k.Host == o.Host && k.Number == o.Number && strings.EqualFold(k.Project, o.Project)
}

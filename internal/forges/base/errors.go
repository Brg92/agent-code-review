// Package base is the platform abstraction of agent-code-review: what a
// provider in internal/forges/<forge> gives the core so that it can read a
// cascade without knowing the platform. It has a read side only; nothing
// here writes to a platform.
//
// # Read errors
//
// A read either returns a complete answer or an error. It never returns an
// empty or partial result in place of an error: a tree the platform cut
// short is ErrTreeTruncated, not the part it sent; a search the platform
// cannot run on a ref is ErrSearchUnavailable, not "no matches"; a project
// the token cannot read is ErrNotFound, not an empty listing. Absence is
// an answer only where a core interface says so: a change request that
// does not exist or cannot be read is a cascade.Item of KindNotFound, a
// file that does not exist is found false, a directory that does not exist
// has no entries.
//
// Errors are *Error values, or bare Kind values, and are tested with
// errors.Is against the Kind constants. ReasonOf gives the stable name of
// the kind. The core never reads an error as an absence: it turns it into
// a Not verified entry (tree-unavailable, search-unavailable, unknown CI)
// with that name as detail.
package base

import (
	"errors"
	"strings"
)

// Kind is the kind of a read error. Its value is the stable name ReasonOf
// returns.
type Kind string

// The kinds of read errors.
const (
	// ErrNotFound: the project, ref or object does not exist or the token
	// cannot see it; platforms often do not tell the two apart.
	ErrNotFound Kind = "not-found"
	// ErrForbidden: the token can see the object but lacks a permission;
	// Error.Permissions lists what the platform said it needed.
	ErrForbidden Kind = "forbidden"
	// ErrSearchUnavailable: the platform cannot search this ref.
	ErrSearchUnavailable Kind = "search-unavailable"
	// ErrTreeTruncated: the platform returned only part of the tree.
	ErrTreeTruncated Kind = "tree-truncated"
	// ErrRateLimited: the platform refused the request for its rate limit.
	ErrRateLimited Kind = "rate-limited"
	// ErrTransport: the request failed for any other reason.
	ErrTransport Kind = "transport"
)

// Kinds are all the kinds, in a fixed order.
var Kinds = []Kind{ErrNotFound, ErrForbidden, ErrSearchUnavailable, ErrTreeTruncated, ErrRateLimited, ErrTransport}

var messages = map[Kind]string{
	ErrNotFound:          "not found or no access",
	ErrForbidden:         "forbidden",
	ErrSearchUnavailable: "search unavailable for this ref",
	ErrTreeTruncated:     "tree truncated",
	ErrRateLimited:       "rate limited",
	ErrTransport:         "transport error",
}

func (k Kind) Error() string {
	if m, ok := messages[k]; ok {
		return m
	}
	return string(k)
}

// Error is a read error.
type Error struct {
	Kind Kind
	// Op says what was read, such as "tree acme/app@main". It must not
	// hold a token or any other secret.
	Op string
	// Permissions are what the platform said the read needed; set by the
	// provider for ErrForbidden when the platform says it.
	Permissions []string
	// Err is the cause, if any.
	Err error
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op + ": ")
	}
	b.WriteString(e.Kind.Error())
	if len(e.Permissions) > 0 {
		b.WriteString(" (needs " + strings.Join(e.Permissions, ", ") + ")")
	}
	if e.Err != nil {
		b.WriteString(": " + e.Err.Error())
	}
	return b.String()
}

// Is reports whether target is the kind of e.
func (e *Error) Is(target error) bool { return target == e.Kind }

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// ReasonOf returns the name of the kind of err, or "" when err is not a
// read error.
func ReasonOf(err error) string {
	for _, k := range Kinds {
		if errors.Is(err, k) {
			return string(k)
		}
	}
	return ""
}

// PermissionsOf returns the permissions a forbidden read needed, if the
// platform said them.
func PermissionsOf(err error) []string {
	var e *Error
	if errors.As(err, &e) {
		return e.Permissions
	}
	return nil
}

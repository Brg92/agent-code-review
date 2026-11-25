package base

import (
	"context"

	"github.com/Brg92/agent-code-review/internal/core"
)

// Account is the account of the token, as the platform says it.
type Account struct {
	Login string
	// Classic is true when the platform lists the scopes of the token,
	// which only a classic token has; false for any other kind.
	Classic bool
	// Scopes are the scopes of a classic token.
	Scopes []string
}

// Area is a part of a repository that the plugin reads, or writes to.
type Area string

// The areas, in the order they are checked.
const (
	AreaRepository   Area = "repository"
	AreaPullRequests Area = "pull requests"
	AreaContents     Area = "contents"
	AreaIssues       Area = "issues"
	AreaChecks       Area = "check runs"
)

// AreaRead is what a read of one area said.
type AreaRead struct {
	Area Area
	// Err is nil when the area can be read; otherwise a *Error that names
	// the permissions the platform said were missing, when it did.
	Err error
}

// WriteState says whether the token may write to an area.
type WriteState int

// The write states.
const (
	// WriteUnknown: reads cannot tell, and nothing is written to find out.
	WriteUnknown WriteState = iota
	// WriteAllowed: the token has what writing needs.
	WriteAllowed
	// WriteDenied: the token lacks what writing needs.
	WriteDenied
)

// WriteAccess is whether the token may write to an area, as far as reads
// can tell: nothing is ever written to check it.
type WriteAccess struct {
	Area   Area
	State  WriteState
	Detail string
}

// Access is what the token can do in a repository.
type Access struct {
	// Reads are the areas the plugin reads, in order.
	Reads []AreaRead
	// Writes are the areas publish writes to.
	Writes []WriteAccess
}

// Diagnoser is a platform that can say what its token can do, for the
// doctor command. It only reads.
type Diagnoser interface {
	// Account reads the account of the token and what kind of token it is.
	Account(ctx context.Context) (Account, error)
	// Access reads every area of project, one request each, and says which
	// ones the token can read; and, from the token and the repository,
	// whether it may write. acct is the account Account returned.
	Access(ctx context.Context, project core.Key, acct Account) (Access, error)
}

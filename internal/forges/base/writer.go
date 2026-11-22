package base

import (
	"context"

	"github.com/Brg92/agent-code-review/internal/core"
)

// ForgeProvider is a platform: what the core reads from it and the few
// things publish writes to it. It has no way to approve, merge, push,
// rebase, check out, build, close or resolve anything; internal/guards
// keeps it so.
type ForgeProvider interface {
	Reader
	Writer
}

// ReviewComment is one inline comment of a review: where it goes and its
// text.
type ReviewComment struct {
	Path string
	Side core.Side
	// StartLine is the first line of a comment over several lines, 0 for
	// one line; Line is the last.
	StartLine, Line int
	Body            string
}

// NewIssue is an issue to open.
type NewIssue struct {
	Title, Body string
	// Label is the label the issue carries; it must exist.
	Label string
	// Assignee is the login the issue is assigned to; empty for nobody.
	Assignee string
}

// Writer is the write side of a platform, and only what publish uses.
// Every method follows the error contract of the package: a refused
// request is an error, never a success that did nothing, and a request
// whose outcome is unknown (a network error, a 5xx) is ErrTransport or
// the kind of the status, never retried by the provider.
type Writer interface {
	// CreateReview posts comments on the diff of change request cr as one
	// review that only comments: it does not approve, and it does not
	// ask for changes. head is the commit the positions are about. If the
	// platform refuses the position of some comments, nothing is posted
	// and the error is ErrRejected, with the indexes of the comments the
	// platform named, if it named any (see RejectedOf).
	CreateReview(ctx context.Context, cr core.Key, head string, comments []ReviewComment) error
	// CreateComment posts a comment on the conversation of cr.
	CreateComment(ctx context.Context, cr core.Key, body string) error
	// CreateIssue opens an issue in the project of project and returns
	// its number. An assignee the platform cannot assign is a failure of
	// the platform to say: callers check Assignable first.
	CreateIssue(ctx context.Context, project core.Key, issue NewIssue) (int, error)
	// UpdateIssue rewrites the title and body of an issue; its state, its
	// labels and its assignees stay as they are.
	UpdateIssue(ctx context.Context, project core.Key, number int, title, body string) error
	// CreateLabel creates a label in project. A label that already exists
	// is not an error.
	CreateLabel(ctx context.Context, project core.Key, label core.Label) error
	// UpdateDescription replaces the description of change request cr.
	UpdateDescription(ctx context.Context, cr core.Key, description string) error
}

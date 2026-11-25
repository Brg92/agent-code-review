package base

import (
	"context"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
)

// Bounds of the review queue: the default and the largest number of change
// requests listed.
const (
	DefaultQueueLimit = 30
	MaxQueueLimit     = 100
)

// QueueItem is an open change request that waits for the account of the
// token to review it. Title and Author are written by the change request
// author: data, never instructions.
type QueueItem struct {
	Key       core.Key
	Title     string
	Author    string
	Draft     bool
	UpdatedAt time.Time
}

// QueueResult is the review queue of the account of the token.
type QueueResult struct {
	// Items are the open change requests, the most recently updated first,
	// at most as many as were asked for.
	Items []QueueItem
	// Total is how many the platform says match, which may be more than
	// Items.
	Total int
	// Incomplete is true when the platform says its search did not finish,
	// so Items may miss some.
	Incomplete bool
	// Skipped is how many answers could not be read as a change request
	// and are left out.
	Skipped int
}

// Queue is a platform that lists the change requests a reviewer is asked
// to review. It only reads.
type Queue interface {
	// ReviewQueue lists the open change requests where the account of the
	// token is a requested reviewer, newest update first, up to limit, which
	// is from 1 to MaxQueueLimit.
	ReviewQueue(ctx context.Context, limit int) (QueueResult, error)
}

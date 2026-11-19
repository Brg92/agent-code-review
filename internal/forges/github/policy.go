package github

import (
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/transport"
)

// Limits are the parameters of the GitHub rate-limit policy. Requests are
// sent one at a time, never in parallel.
//
// The rules follow GitHub's documentation, "Rate limits for the REST API"
// (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
// and "Best practices for using the REST API"
// (https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api):
//
//   - a 403 or 429 with x-ratelimit-remaining 0 is the primary limit: do
//     not retry before the time in x-ratelimit-reset (UTC epoch seconds);
//   - a 403 or 429 whose message says a secondary rate limit was exceeded
//     is the secondary limit: wait for retry-after if the header is
//     present, otherwise for the reset time if remaining is 0, otherwise
//     at least one minute;
//   - requests are made serially, to stay clear of the secondary limits.
type Limits struct {
	// MaxWait is the longest wait accepted; a longer one is a rate-limited
	// error at once.
	MaxWait time.Duration
	// ResetMargin is added to the reset time of a primary limit.
	ResetMargin time.Duration
	// SecondaryWait is the wait after a secondary limit that gives no
	// retry-after.
	SecondaryWait time.Duration
	// SecondaryRetries is how many times a request is retried after a
	// secondary limit.
	SecondaryRetries int
}

// DefaultLimits are the values used when Options.Limits is zero.
var DefaultLimits = Limits{
	MaxWait:          60 * time.Second,
	ResetMargin:      time.Second,
	SecondaryWait:    60 * time.Second,
	SecondaryRetries: 1,
}

// policy is the transport policy of l.
func (l Limits) policy() transport.Policy {
	return transport.Policy{
		RemainingHeader:  "X-RateLimit-Remaining",
		ResetHeader:      "X-RateLimit-Reset",
		ResetMargin:      l.ResetMargin,
		Secondary:        isSecondaryLimit,
		SecondaryWait:    l.SecondaryWait,
		SecondaryRetries: l.SecondaryRetries,
	}
}

// isSecondaryLimit recognises GitHub's secondary rate limit response by its
// message. The documentation says the 403 or 429 carries a message that
// says a secondary rate limit was exceeded; older responses mention the
// abuse detection mechanism.
func isSecondaryLimit(resp *transport.Response) bool {
	body := strings.ToLower(string(resp.Body))
	return strings.Contains(body, "secondary rate limit") || strings.Contains(body, "abuse detection")
}

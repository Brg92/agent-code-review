package transport

import (
	"errors"
	"time"
)

// Outcome says how a request, or one attempt of it, ended.
type Outcome string

// The outcomes of an Event.
const (
	// OutcomeOK: a 2xx answer, or a 304 answered from the cache.
	OutcomeOK Outcome = "ok"
	// OutcomeRetry: the attempt failed and another follows.
	OutcomeRetry Outcome = "retry"
	// OutcomeRateLimited: the request ended on a rate limit.
	OutcomeRateLimited Outcome = "rate-limited"
	// OutcomeError: the request ended on any other failure.
	OutcomeError Outcome = "error"
)

// Event describes a request for diagnostics. It holds no header, no query,
// no body and no URL host: nothing in it can carry a token.
type Event struct {
	// Method is the HTTP method of the request: GET, or POST or PATCH for
	// an endpoint the caller allowed.
	Method string
	// Path is the URL path of the request, without its query.
	Path string
	// Status is the HTTP status of the last attempt, 0 when there was no
	// answer.
	Status int
	// Duration is the time of the attempt for OutcomeRetry, and of the
	// whole request, retries and waits included, for any other outcome.
	Duration time.Duration
	// Attempts is the number of attempts made so far.
	Attempts int
	Outcome  Outcome
}

// outcomeOf is the outcome of a finished request.
func outcomeOf(err error) Outcome {
	var rl *RateLimitError
	switch {
	case err == nil:
		return OutcomeOK
	case errors.As(err, &rl):
		return OutcomeRateLimited
	}
	return OutcomeError
}

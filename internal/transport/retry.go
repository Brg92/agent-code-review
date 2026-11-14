package transport

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retry defaults applied by New for zero Config fields.
const (
	DefaultMaxRetries  = 3
	DefaultBaseBackoff = time.Second
	DefaultMaxBackoff  = 30 * time.Second
	DefaultMaxWait     = 2 * time.Minute
)

// Policy names the platform's rate-limit headers. The zero Policy knows only
// the standard signals: status 429, and Retry-After on a 429, 403 or 5xx.
type Policy struct {
	// RemainingHeader holds the requests left in the window; "0" on a 403
	// or 429 marks the response as rate limited.
	RemainingHeader string
	// ResetHeader holds the Unix time, in seconds, when the window resets.
	ResetHeader string
}

// rateLimited reports whether resp was refused by a rate limit.
func (p Policy) rateLimited(resp *Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		if resp.Header.Get("Retry-After") != "" {
			return true
		}
		return p.RemainingHeader != "" && strings.TrimSpace(resp.Header.Get(p.RemainingHeader)) == "0"
	}
	return false
}

// resetTime returns when the platform says the limit resets.
func (p Policy) resetTime(h http.Header) (time.Time, bool) {
	if p.ResetHeader == "" {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(h.Get(p.ResetHeader)), 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

// retryAfter parses a Retry-After header, in seconds or as an HTTP date.
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if at, err := http.ParseTime(v); err == nil {
		return max(at.Sub(now), 0), true
	}
	return 0, false
}

// backoff returns the exponential delay before retry number n (0-based).
func (c *Client) backoff(n int) time.Duration {
	d := c.baseBackoff
	for range n {
		d *= 2
		if d >= c.maxBackoff {
			return c.maxBackoff
		}
	}
	return min(d, c.maxBackoff)
}

// hintedDelay is the wait the server asked for, if any.
func (c *Client) hintedDelay(resp *Response, rateLimited bool) (time.Duration, bool) {
	now := c.now()
	if d, ok := retryAfter(resp.Header, now); ok {
		return d, true
	}
	if rateLimited {
		if at, ok := c.policy.resetTime(resp.Header); ok {
			return max(at.Sub(now), 0), true
		}
	}
	return 0, false
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

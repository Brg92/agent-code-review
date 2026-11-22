package transport

import (
	"context"
	"errors"
	"net/http"
)

// Post sends a POST of body, which is JSON, to path, relative to the base
// URL, and returns the response. The request goes only if an endpoint of
// Config.Allow allows it; otherwise it is a *NotAllowedError and nothing is
// sent. See write for how it is retried.
func (c *Client) Post(ctx context.Context, path, accept string, body []byte) (*Response, error) {
	return c.write(ctx, http.MethodPost, path, accept, body)
}

// Patch is Post for a PATCH.
func (c *Client) Patch(ctx context.Context, path, accept string, body []byte) (*Response, error) {
	return c.write(ctx, http.MethodPatch, path, accept, body)
}

// write sends one request that may change something. Unlike a read, it is
// retried only after a rate limit, which refused it: a network error or a
// 5xx leaves it unknown whether the platform did what was asked, and doing
// it again could do it twice. The caller reads the platform to find out.
func (c *Client) write(ctx context.Context, method, path, accept string, body []byte) (resp *Response, err error) {
	u, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	if body == nil {
		body = []byte{}
	}
	start := c.now()
	status, attempts := 0, 0
	if c.observe != nil {
		defer func() {
			c.observe(Event{Method: method, Path: u.EscapedPath(), Status: status, Duration: c.now().Sub(start), Attempts: attempts, Outcome: outcomeOf(err)})
		}()
	}
	token, err := c.tokenValue(ctx)
	if err != nil {
		return nil, errors.New("transport: token: " + err.Error())
	}
	secrets := []string{token}
	shortTermRetries := 0
	for attempt := 0; ; attempt++ {
		attempts, status = attempt+1, 0
		resp, err := c.attempt(ctx, method, u, accept, token, "", body, secrets)
		if err != nil {
			return nil, err
		}
		status = resp.StatusCode
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode <= 299:
			return resp, nil
		case c.policy.rateLimited(resp):
			d, hinted := c.hintedDelay(resp, true)
			if !hinted {
				d = c.backoff(attempt)
			}
			last := attempt >= c.maxRetries
			if c.policy.shortTerm(resp) {
				shortTermRetries++
				last = last || (c.policy.SecondaryRetries > 0 && shortTermRetries > c.policy.SecondaryRetries)
			}
			if last || d > c.maxWait {
				return nil, &RateLimitError{Method: method, StatusCode: resp.StatusCode, URL: redactURL(u.String()), RetryAt: c.retryAt(d, hinted)}
			}
			if err := c.sleep(ctx, d); err != nil {
				return nil, err
			}
		default:
			return nil, c.statusError(u, resp, secrets)
		}
	}
}

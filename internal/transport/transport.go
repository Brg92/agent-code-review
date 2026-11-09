// Package transport is the generic, read-only HTTP client used by the forge
// providers. It knows no platform: base URL, token source, Accept header and
// rate-limit policy come from the caller.
//
// Only GET requests can be sent. The client has no method that takes an HTTP
// method, and its round tripper refuses anything else.
package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/version"
)

// Defaults applied by New for zero Config fields.
const (
	DefaultTimeout      = 30 * time.Second
	DefaultMaxBodyBytes = 32 << 20
)

// TokenSource returns the token to send, or "" to send none. It is called
// once per request, so a source may cache or refresh as it likes.
type TokenSource func(ctx context.Context) (string, error)

// Config configures a Client.
type Config struct {
	// BaseURL is the API root, such as https://api.github.com. Request paths
	// are appended to it.
	BaseURL string
	// Token supplies the bearer token; nil sends no Authorization header.
	Token TokenSource
	// Timeout bounds one HTTP exchange, redirects included. Zero means
	// DefaultTimeout.
	Timeout time.Duration
	// MaxBodyBytes caps a response body. Zero means DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// MaxRetries is how many times a request is retried after a network
	// error, a 5xx or a rate limit. Zero means DefaultMaxRetries; a negative
	// value disables retries.
	MaxRetries int
	// BaseBackoff is the first retry delay, doubled on each retry up to
	// MaxBackoff. Zero means DefaultBaseBackoff and DefaultMaxBackoff.
	BaseBackoff, MaxBackoff time.Duration
	// MaxWait is the longest delay the client accepts from Retry-After or a
	// rate-limit reset; a longer one ends the request at once. Zero means
	// DefaultMaxWait.
	MaxWait time.Duration
	// Policy names the platform's rate-limit headers.
	Policy Policy
	// Now and Sleep replace the clock; nil means the real one.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	// base replaces the network round tripper in tests.
	base http.RoundTripper
}

// Response is a fully read HTTP response.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	// URL is the URL that answered, after redirects.
	URL string
}

// Client sends read-only requests to one API root.
type Client struct {
	base     *url.URL
	token    TokenSource
	http     *http.Client
	maxBody  int64
	userAgnt string

	maxRetries              int
	baseBackoff, maxBackoff time.Duration
	maxWait                 time.Duration
	policy                  Policy
	now                     func() time.Time
	sleep                   func(context.Context, time.Duration) error
}

// New returns a Client for cfg.
func New(cfg Config) (*Client, error) {
	base, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = DefaultMaxBodyBytes
	}
	rt := cfg.base
	if rt == nil {
		rt = newNetworkTransport(timeout)
	}
	c := &Client{
		base:     base,
		token:    cfg.Token,
		maxBody:  maxBody,
		userAgnt: "agent-code-review/" + version.Version,
		http: &http.Client{
			Transport:     getOnly{rt},
			Timeout:       timeout,
			CheckRedirect: checkRedirect,
		},
		maxRetries:  cfg.MaxRetries,
		baseBackoff: cfg.BaseBackoff,
		maxBackoff:  cfg.MaxBackoff,
		maxWait:     cfg.MaxWait,
		policy:      cfg.Policy,
		now:         cfg.Now,
		sleep:       cfg.Sleep,
	}
	switch {
	case c.maxRetries == 0:
		c.maxRetries = DefaultMaxRetries
	case c.maxRetries < 0:
		c.maxRetries = 0
	}
	if c.baseBackoff <= 0 {
		c.baseBackoff = DefaultBaseBackoff
	}
	if c.maxBackoff <= 0 {
		c.maxBackoff = max(DefaultMaxBackoff, c.baseBackoff)
	}
	if c.maxWait <= 0 {
		c.maxWait = DefaultMaxWait
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	return c, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("transport: invalid base URL: %w", err)
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return nil, errors.New("transport: base URL must use https or http")
	case u.Host == "":
		return nil, errors.New("transport: base URL has no host")
	case u.User != nil:
		return nil, errors.New("transport: base URL must not contain credentials")
	case u.RawQuery != "" || u.Fragment != "":
		return nil, errors.New("transport: base URL must not contain a query or fragment")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// newNetworkTransport uses the system TLS roots and the proxy from the
// environment, with explicit timeouts on every phase.
func newNetworkTransport(timeout time.Duration) *http.Transport {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
	}
}

// getOnly refuses every request that is not a GET, whatever builds it.
type getOnly struct{ next http.RoundTripper }

// errMethodNotAllowed is returned for a request that is not a GET.
var errMethodNotAllowed = errors.New("transport: only GET requests are allowed")

func (g getOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, errMethodNotAllowed
	}
	return g.next.RoundTrip(req)
}

// resolve appends path, which must start with a single "/", to the base URL.
func (c *Client) resolve(path string) (*url.URL, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, fmt.Errorf("transport: request path %q must start with a single /", path)
	}
	rel, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("transport: invalid request path: %w", err)
	}
	u := *c.base
	u.Path = c.base.Path + rel.Path
	u.RawPath = ""
	u.RawQuery = rel.RawQuery
	return &u, nil
}

// Get sends a GET for path, relative to the base URL, with the given Accept
// header ("" sends none), and returns the response.
func (c *Client) Get(ctx context.Context, path, accept string) (*Response, error) {
	u, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	return c.get(ctx, u, accept)
}

func (c *Client) get(ctx context.Context, u *url.URL, accept string) (*Response, error) {
	token, err := c.tokenValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("transport: token: %w", err)
	}
	secrets := []string{token}
	for attempt := 0; ; attempt++ {
		resp, err := c.attempt(ctx, u, accept, token, secrets)
		last := attempt >= c.maxRetries
		var delay time.Duration
		switch {
		case err != nil:
			var ne *NetworkError
			if !errors.As(err, &ne) || ctx.Err() != nil || last {
				return nil, err
			}
			delay = c.backoff(attempt)
		case resp.StatusCode >= 200 && resp.StatusCode <= 299:
			return resp, nil
		case c.policy.rateLimited(resp):
			d, hinted := c.hintedDelay(resp, true)
			if !hinted {
				d = c.backoff(attempt)
			}
			if last || d > c.maxWait {
				return nil, &RateLimitError{StatusCode: resp.StatusCode, URL: redactURL(u.String()), RetryAt: c.retryAt(d, hinted)}
			}
			delay = d
		case resp.StatusCode >= 500:
			d, hinted := c.hintedDelay(resp, false)
			if !hinted {
				d = c.backoff(attempt)
			}
			if last || d > c.maxWait {
				return nil, c.statusError(u, resp, secrets)
			}
			delay = d
		default:
			return nil, c.statusError(u, resp, secrets)
		}
		if err := c.sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
}

func (c *Client) retryAt(d time.Duration, hinted bool) time.Time {
	if !hinted {
		return time.Time{}
	}
	return c.now().Add(d)
}

func (c *Client) statusError(u *url.URL, resp *Response, secrets []string) error {
	return &StatusError{StatusCode: resp.StatusCode, URL: redactURL(u.String()), Message: bodyMessage(resp.Body, secrets)}
}

// attempt sends one request and reads its body, whatever the status.
func (c *Client) attempt(ctx context.Context, u *url.URL, accept, token string, secrets []string) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgnt)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	// A refused redirect is final: retrying would get the same answer.
	for _, final := range []error{ErrCrossHostRedirect, errTooManyRedirects} {
		if errors.Is(err, final) {
			return nil, fmt.Errorf("transport: GET %s: %w", redactURL(u.String()), final)
		}
	}
	if err != nil {
		return nil, newNetworkError(u.String(), err, secrets)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return nil, newNetworkError(u.String(), err, secrets)
	}
	if int64(len(body)) > c.maxBody {
		return nil, fmt.Errorf("transport: GET %s: response body exceeds %d bytes", redactURL(u.String()), c.maxBody)
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body, URL: resp.Request.URL.String()}, nil
}

func (c *Client) tokenValue(ctx context.Context) (string, error) {
	if c.token == nil {
		return "", nil
	}
	return c.token(ctx)
}

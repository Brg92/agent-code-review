// Package transport is the generic HTTP client used by the forge providers,
// which only reads unless the caller allows a few writes. It knows no platform: base URL, token source, Accept header and
// rate-limit policy come from the caller.
//
// Reads are GET requests. The only other requests the client sends are the
// POST and PATCH of the endpoints the caller allows in Config.Allow: it has
// no method that takes an HTTP method, and its round tripper refuses
// anything else, with a *NotAllowedError, before it reaches the network.
package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
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

	// Allow lists the only requests besides GET that Post and Patch may
	// send; none by default.
	Allow []Endpoint

	// Observe, if set, is called for each request the client finishes and
	// for each attempt it retries, for the diagnostics of the caller. It
	// must not block.
	Observe func(Event)

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
	// method is the HTTP method of the request that was answered.
	method string
	// FromCache is true when the server answered 304 Not Modified and the
	// body is the one cached for the same ETag.
	FromCache bool
}

// Client sends requests to one API root: reads, and the writes the caller
// allows.
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
	cache                   *etagCache
	observe                 func(Event)
}

// New returns a Client for cfg.
func New(cfg Config) (*Client, error) {
	base, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	for _, e := range cfg.Allow {
		if err := e.validate(); err != nil {
			return nil, err
		}
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
			Transport:     restrict{next: rt, basePath: base.Path, allow: slices.Clone(cfg.Allow)},
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
		cache:       newETagCache(DefaultCacheBytes),
		observe:     cfg.Observe,
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

// ErrInsecureScheme is returned for a base URL that would send the token in
// clear text: http is accepted only for loopback hosts.
var ErrInsecureScheme = errors.New("transport: base URL must use https unless the host is loopback")

// isLoopbackHost reports whether host is localhost or a loopback address.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("transport: invalid base URL: %w", err)
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return nil, errors.New("transport: base URL must use https or http")
	case u.Scheme == "http" && !isLoopbackHost(u.Hostname()):
		return nil, fmt.Errorf("%w: %s", ErrInsecureScheme, u.Redacted())
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

func (c *Client) get(ctx context.Context, u *url.URL, accept string) (resp *Response, err error) {
	start := c.now()
	status, attempts := 0, 0
	if c.observe != nil {
		defer func() {
			c.observe(Event{Method: http.MethodGet, Path: u.EscapedPath(), Status: status, Duration: c.now().Sub(start), Attempts: attempts, Outcome: outcomeOf(err)})
		}()
	}
	token, err := c.tokenValue(ctx)
	if err != nil {
		return nil, fmt.Errorf("transport: token: %w", err)
	}
	secrets := []string{token}
	key := cacheKey(u.String(), accept, token)
	cached, haveCached := c.cache.get(key)
	shortTermRetries := 0
	for attempt := 0; ; attempt++ {
		attemptStart := c.now()
		attempts, status = attempt+1, 0
		resp, err := c.attempt(ctx, http.MethodGet, u, accept, token, cached.etag, nil, secrets)
		if resp != nil {
			status = resp.StatusCode
		}
		last := attempt >= c.maxRetries
		var delay time.Duration
		switch {
		case err != nil:
			var ne *NetworkError
			if !errors.As(err, &ne) || ctx.Err() != nil || last {
				return nil, err
			}
			delay = c.backoff(attempt)
		case resp.StatusCode == http.StatusNotModified && haveCached:
			return cached.response(), nil
		case resp.StatusCode >= 200 && resp.StatusCode <= 299:
			if etag := resp.Header.Get("ETag"); etag != "" {
				c.cache.put(key, cachedResponse{etag: etag, status: resp.StatusCode, header: resp.Header.Clone(), body: append([]byte(nil), resp.Body...), url: resp.URL})
			}
			return resp, nil
		case c.policy.rateLimited(resp):
			d, hinted := c.hintedDelay(resp, true)
			if !hinted {
				d = c.backoff(attempt)
			}
			if c.policy.shortTerm(resp) {
				shortTermRetries++
				last = last || (c.policy.SecondaryRetries > 0 && shortTermRetries > c.policy.SecondaryRetries)
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
		if c.observe != nil {
			c.observe(Event{Method: http.MethodGet, Path: u.EscapedPath(), Status: status, Duration: c.now().Sub(attemptStart), Attempts: attempts, Outcome: OutcomeRetry})
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
	return &StatusError{Method: resp.method, StatusCode: resp.StatusCode, URL: redactURL(u.String()), Message: bodyMessage(resp.Body, secrets), Header: resp.Header.Clone(),
		Body: []byte(redactText(string(resp.Body), secrets))}
}

// attempt sends one request and reads its body, whatever the status.
func (c *Client) attempt(ctx context.Context, method string, u *url.URL, accept, token, etag string, body []byte, secrets []string) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var payload io.Reader
	if body != nil {
		payload = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", c.userAgnt)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	// A request that is not allowed never reaches the network, and a
	// refused redirect is final: retrying would get the same answer.
	var na *NotAllowedError
	if errors.As(err, &na) {
		return nil, na
	}
	for _, final := range []error{ErrCrossHostRedirect, errTooManyRedirects} {
		if errors.Is(err, final) {
			return nil, fmt.Errorf("transport: %s %s: %w", method, redactURL(u.String()), final)
		}
	}
	if err != nil {
		return nil, newNetworkError(method, u.String(), err, secrets)
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return nil, newNetworkError(method, u.String(), err, secrets)
	}
	if int64(len(answer)) > c.maxBody {
		return nil, fmt.Errorf("transport: %s %s: response body exceeds %d bytes", method, redactURL(u.String()), c.maxBody)
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: answer, URL: resp.Request.URL.String(), method: method}, nil
}

func (c *Client) tokenValue(ctx context.Context) (string, error) {
	if c.token == nil {
		return "", nil
	}
	return c.token(ctx)
}

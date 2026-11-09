package transport

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// redacted replaces every secret in an error message.
const redacted = "[REDACTED]"

// maxErrorBody is how much of a response body an error message quotes.
const maxErrorBody = 200

// StatusError is a response with an unexpected HTTP status.
type StatusError struct {
	StatusCode int
	// URL is the request URL with credentials and sensitive query values removed.
	URL string
	// Message is the start of the response body, on one line, redacted.
	Message string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("transport: GET %s: status %d", e.URL, e.StatusCode)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// NetworkError is a request that got no HTTP response: refused connection,
// DNS or TLS failure, timeout, cancelled context.
type NetworkError struct {
	URL     string
	msg     string
	timeout bool
	err     error
}

func (e *NetworkError) Error() string { return "transport: GET " + e.URL + ": " + e.msg }

// Timeout reports whether the request timed out.
func (e *NetworkError) Timeout() bool { return e.timeout }

// Unwrap returns the cause, so errors.Is works for context errors.
func (e *NetworkError) Unwrap() error { return e.err }

// newNetworkError builds a NetworkError from an http.Client error. The
// url.Error wrapper is dropped: it carries the unredacted URL.
func newNetworkError(rawURL string, err error, secrets []string) *NetworkError {
	cause := err
	var ue *url.Error
	if errors.As(err, &ue) {
		cause = ue.Err
	}
	var ne net.Error
	timeout := errors.As(err, &ne) && ne.Timeout()
	return &NetworkError{
		URL:     redactURL(rawURL),
		msg:     redactText(cause.Error(), secrets),
		timeout: timeout,
		err:     cause,
	}
}

// sensitiveParam reports whether a query parameter may carry a credential.
func sensitiveParam(name string) bool {
	name = strings.ToLower(name)
	for _, s := range []string{"token", "secret", "password", "key", "sig", "auth", "code", "credential"} {
		if strings.Contains(name, s) {
			return true
		}
	}
	return false
}

// redactURL drops user info and fragment and masks sensitive query values.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return redacted
	}
	u.User = nil
	u.Fragment = ""
	u.RawFragment = ""
	if u.RawQuery != "" {
		q := u.Query()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			for _, v := range q[k] {
				if sensitiveParam(k) {
					v = redacted
				} else {
					v = url.QueryEscape(v)
				}
				parts = append(parts, url.QueryEscape(k)+"="+v)
			}
		}
		u.RawQuery = strings.Join(parts, "&")
	}
	return u.String()
}

// redactText replaces every non-empty secret in s.
func redactText(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, redacted)
		}
	}
	return s
}

// bodyMessage turns the start of a body into one printable, redacted line.
func bodyMessage(body []byte, secrets []string) string {
	s := redactText(string(body), secrets)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return ' '
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxErrorBody {
		// Cut on a rune boundary.
		cut := maxErrorBody
		for cut > 0 && !utf8RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

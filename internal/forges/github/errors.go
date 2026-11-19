package github

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/transport"
)

func isKind(err error, k base.Kind) bool { return errors.Is(err, k) }

// permissionsHeader is where GitHub lists the permissions a request needed.
const permissionsHeader = "X-Accepted-GitHub-Permissions"

// fail turns an error of the transport into a read error of op. Its
// message never holds a token, an Authorization header or a URL with its
// query.
func (p *Provider) fail(op string, err error) error {
	var be *base.Error
	if errors.As(err, &be) {
		return err
	}
	var (
		rl *transport.RateLimitError
		se *transport.StatusError
		ne *transport.NetworkError
	)
	switch {
	case errors.Is(err, auth.ErrNoToken):
		return &base.Error{Kind: base.ErrForbidden, Op: op, Err: auth.ErrNoToken}
	case errors.As(err, &rl):
		msg := fmt.Sprintf("status %d", rl.StatusCode)
		if !rl.RetryAt.IsZero() {
			msg += "; retry after " + rl.RetryAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		return &base.Error{Kind: base.ErrRateLimited, Op: op, Err: errors.New(msg)}
	case errors.As(err, &se):
		return statusError(op, se)
	case errors.Is(err, transport.ErrCrossHostRedirect):
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("redirect to another host refused")}
	case errors.Is(err, transport.ErrPageLimit):
		return &base.Error{Kind: base.ErrListTruncated, Op: op, Err: errors.New("more pages than the page cap")}
	case errors.As(err, &ne):
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: scrubbed(ne.Error(), ne.URL)}
	}
	return &base.Error{Kind: base.ErrTransport, Op: op, Err: scrubbed(err.Error(), "")}
}

// statusError maps an HTTP status to a read error.
func statusError(op string, se *transport.StatusError) error {
	cause := &statusCause{Code: se.StatusCode, Message: se.Message}
	switch se.StatusCode {
	case 404, 410, 451:
		return &base.Error{Kind: base.ErrNotFound, Op: op, Err: cause}
	case 401, 403:
		return &base.Error{Kind: base.ErrForbidden, Op: op, Permissions: acceptedPermissions(se), Err: cause}
	}
	return &base.Error{Kind: base.ErrTransport, Op: op, Err: cause}
}

// acceptedPermissions reads the permissions GitHub says a request needed,
// such as "pull_requests=read; issues=read", as "pull_requests: read".
func acceptedPermissions(se *transport.StatusError) []string {
	raw := se.Header.Get(permissionsHeader)
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ',' }) {
		name, level, ok := strings.Cut(strings.TrimSpace(part), "=")
		switch {
		case !ok:
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		default:
			out = append(out, strings.TrimSpace(name)+": "+strings.TrimSpace(level))
		}
	}
	return out
}

var urlInText = regexp.MustCompile(`https?://\S+`)

// scrubbed turns a message into an error without any URL: each one is
// replaced by its path, so that no query, user info or host is kept.
func scrubbed(msg, rawURL string) error {
	if u, err := url.Parse(rawURL); err == nil && rawURL != "" {
		msg = strings.ReplaceAll(msg, rawURL, u.Path)
	}
	msg = urlInText.ReplaceAllStringFunc(msg, func(m string) string {
		trimmed := strings.TrimRight(m, ":;,.")
		suffix := m[len(trimmed):]
		u, err := url.Parse(trimmed)
		if err != nil {
			return "[URL]" + suffix
		}
		return u.Path + suffix
	})
	return errors.New(msg)
}

// statusCause is the cause of a read error that came from an HTTP answer.
type statusCause struct {
	Code    int
	Message string
}

func (c *statusCause) Error() string {
	if c.Message == "" {
		return "status " + strconv.Itoa(c.Code)
	}
	return "status " + strconv.Itoa(c.Code) + ": " + c.Message
}

// isStatus reports whether err is a read error that came from an HTTP
// answer with the given status.
func isStatus(err error, status int) bool {
	var c *statusCause
	return errors.As(err, &c) && c.Code == status
}

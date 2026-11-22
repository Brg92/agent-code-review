package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Endpoint is a request that is not a GET and that the client may send:
// only POST and PATCH can be allowed, and only to the paths a caller lists.
type Endpoint struct {
	// Method is POST or PATCH.
	Method string
	// Path is the path of the request below the base URL, with a "*" for
	// each segment that varies, such as /repos/*/*/pulls/*/reviews. The
	// query is not part of it. A "*" matches exactly one segment.
	Path string
	// ReadOnlyQuery marks a POST that only carries a GraphQL query: its
	// body must be a JSON object whose "query" is a query, never a
	// mutation or a subscription. Only POST may have it.
	ReadOnlyQuery bool
}

// ErrNotAllowed is the error of a request the client does not send: a
// method other than GET that no endpoint allows at that path.
var ErrNotAllowed = errors.New("transport: request not allowed")

// NotAllowedError is a request refused before it reached the network.
type NotAllowedError struct {
	Method, Path string
	// Reason says what was wrong when the endpoint was allowed but the
	// request was not, such as a GraphQL document that is not a query.
	Reason string
}

func (e *NotAllowedError) Error() string {
	msg := fmt.Sprintf("transport: %s %s is not allowed", e.Method, e.Path)
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Is reports whether target is ErrNotAllowed.
func (e *NotAllowedError) Is(target error) bool { return target == ErrNotAllowed }

// validate reports an endpoint that could let through more than a write
// the caller means.
func (e Endpoint) validate() error {
	switch {
	case e.Method != http.MethodPost && e.Method != http.MethodPatch:
		return fmt.Errorf("transport: endpoint %q %s: only POST and PATCH can be allowed", e.Method, e.Path)
	case !strings.HasPrefix(e.Path, "/") || strings.HasPrefix(e.Path, "//") || strings.Contains(e.Path, "?"):
		return fmt.Errorf("transport: endpoint path %q must start with a single / and have no query", e.Path)
	case e.ReadOnlyQuery && e.Method != http.MethodPost:
		return fmt.Errorf("transport: endpoint %s %s: only a POST carries a GraphQL query", e.Method, e.Path)
	}
	return nil
}

// matches reports whether path is the path of the endpoint.
func (e Endpoint) matches(method, path string) bool {
	if e.Method != method {
		return false
	}
	want, got := strings.Split(e.Path, "/"), strings.Split(path, "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] == "*" && got[i] != "" {
			continue
		}
		if want[i] != got[i] {
			return false
		}
	}
	return true
}

// restrict refuses every request that is not a GET and that no endpoint
// allows, whatever builds it.
type restrict struct {
	next http.RoundTripper
	// basePath is the path of the base URL, which the endpoints are
	// relative to.
	basePath string
	allow    []Endpoint
}

func (r restrict) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method == http.MethodGet {
		return r.next.RoundTrip(req)
	}
	if err := r.check(req); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	return r.next.RoundTrip(req)
}

func (r restrict) check(req *http.Request) error {
	path := strings.TrimPrefix(req.URL.Path, r.basePath)
	refuse := func(reason string) error { return &NotAllowedError{Method: req.Method, Path: path, Reason: reason} }
	for _, e := range r.allow {
		if !e.matches(req.Method, path) {
			continue
		}
		if !e.ReadOnlyQuery {
			return nil
		}
		if req.GetBody == nil {
			return refuse("the body cannot be read")
		}
		body, err := req.GetBody()
		if err != nil {
			return refuse("the body cannot be read")
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil || !readOnlyQuery(data) {
			return refuse("the body is not a GraphQL query")
		}
		return nil
	}
	return refuse("")
}

var (
	graphQLComment = regexp.MustCompile(`#[^\n\r]*`)
	// graphQLWrite is a keyword of an operation that writes.
	graphQLWrite = regexp.MustCompile(`\b(?:mutation|subscription)\b`)
	graphQLQuery = regexp.MustCompile(`^(?:\{|query\b)`)
)

// readOnlyQuery reports whether body is a GraphQL request whose document
// only has queries. It is strict on purpose: anything it does not
// recognise, a keyword inside a string included, is refused.
func readOnlyQuery(body []byte) bool {
	var doc struct {
		Query *string `json:"query"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&doc); err != nil || doc.Query == nil {
		return false
	}
	text := strings.TrimSpace(graphQLComment.ReplaceAllString(*doc.Query, " "))
	return graphQLQuery.MatchString(text) && !graphQLWrite.MatchString(text)
}

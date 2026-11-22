package github

import (
	"net/http"

	"github.com/Brg92/agent-code-review/internal/transport"
)

// writeEndpoints are the only requests besides GET that the provider can
// send: the writes of base.Writer, and the GraphQL query that reads the
// review threads, which GitHub serves only through a POST. The transport
// refuses everything else, whoever asks.
var writeEndpoints = []transport.Endpoint{
	// CreateReview
	{Method: http.MethodPost, Path: "/repos/*/*/pulls/*/reviews"},
	// CreateComment
	{Method: http.MethodPost, Path: "/repos/*/*/issues/*/comments"},
	// CreateIssue
	{Method: http.MethodPost, Path: "/repos/*/*/issues"},
	// UpdateIssue
	{Method: http.MethodPatch, Path: "/repos/*/*/issues/*"},
	// CreateLabel
	{Method: http.MethodPost, Path: "/repos/*/*/labels"},
	// UpdateDescription
	{Method: http.MethodPatch, Path: "/repos/*/*/pulls/*"},
	// ReviewThreads: a query only
	{Method: http.MethodPost, Path: graphQLPath, ReadOnlyQuery: true},
}

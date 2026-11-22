package github

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// graphQLPath is the GraphQL endpoint below the base URL of the API.
const graphQLPath = "/graphql"

// threadsQuery reads the review threads of a pull request. GitHub shows
// whether a thread is resolved only through GraphQL. It is a query: the
// transport refuses anything else on this endpoint.
const threadsQuery = `query($owner: String!, $name: String!, $number: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          isResolved
          comments(first: 1) { nodes { body authorAssociation author { login } } }
        }
      }
    }
  }
}`

type threadsAnswer struct {
	Data *struct {
		Repository *struct {
			PullRequest *struct {
				ReviewThreads struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
						Comments   struct {
							Nodes []struct {
								Body              string `json:"body"`
								AuthorAssociation string `json:"authorAssociation"`
								Author            *struct {
									Login string `json:"login"`
								} `json:"author"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"errors"`
}

// ReviewThreads implements base.Reader with one read-only GraphQL query per
// page of threads.
func (p *Provider) ReviewThreads(ctx context.Context, cr core.Key) ([]core.ReviewThread, error) {
	op := "review threads " + cr.String()
	if err := p.checkHost(op, cr); err != nil {
		return nil, err
	}
	owner, name, _ := strings.Cut(cr.Project, "/")
	threads := []core.ReviewThread{}
	self := ""
	after := ""
	for page := 0; ; page++ {
		if page >= p.maxPages {
			return nil, &base.Error{Kind: base.ErrListTruncated, Op: op, Err: errors.New("more pages than the page cap")}
		}
		vars := map[string]any{"owner": owner, "name": name, "number": cr.Number}
		if after != "" {
			vars["after"] = after
		}
		body, err := json.Marshal(map[string]any{"query": threadsQuery, "variables": vars})
		if err != nil {
			return nil, &base.Error{Kind: base.ErrTransport, Op: op, Err: err}
		}
		resp, err := p.post(ctx, graphQLPath, body)
		if err != nil {
			return nil, p.fail(op, err)
		}
		var v threadsAnswer
		if err := decode(op, resp.Body, &v); err != nil {
			return nil, err
		}
		if err := graphQLError(op, v); err != nil {
			return nil, err
		}
		if v.Data == nil || v.Data.Repository == nil || v.Data.Repository.PullRequest == nil {
			return nil, &base.Error{Kind: base.ErrNotFound, Op: op}
		}
		rt := v.Data.Repository.PullRequest.ReviewThreads
		for _, n := range rt.Nodes {
			t := core.ReviewThread{Resolved: n.IsResolved}
			if len(n.Comments.Nodes) > 0 {
				c := n.Comments.Nodes[0]
				t.Body, t.Author, t.Trusted = c.Body, ghost, trustedAssociation(c.AuthorAssociation)
				if c.Author != nil {
					t.Author = c.Author.Login
				}
				if !t.Trusted && t.Author != ghost {
					if self == "" {
						if self, err = p.Self(ctx); err != nil {
							return nil, err
						}
					}
					t.Trusted = strings.EqualFold(t.Author, self)
				}
			}
			threads = append(threads, t)
		}
		if !rt.PageInfo.HasNextPage {
			return threads, nil
		}
		after = rt.PageInfo.EndCursor
	}
}

// graphQLError turns the errors of a GraphQL answer into a read error.
func graphQLError(op string, v threadsAnswer) error {
	if len(v.Errors) == 0 {
		return nil
	}
	e := v.Errors[0]
	kind := base.ErrTransport
	switch e.Type {
	case "NOT_FOUND":
		kind = base.ErrNotFound
	case "FORBIDDEN", "INSUFFICIENT_SCOPES":
		kind = base.ErrForbidden
	}
	return &base.Error{Kind: kind, Op: op, Err: errors.New("graphql: " + e.Message)}
}

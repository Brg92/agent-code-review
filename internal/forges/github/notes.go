package github

import (
	"context"
	"strconv"

	"github.com/Brg92/agent-code-review/internal/core"
)

type commentJSON struct {
	User *struct {
		Login string `json:"login"`
	} `json:"user"`
	Body string `json:"body"`
}

// ghost is the login GitHub shows for an account that was deleted.
const ghost = "ghost"

// Notes implements base.Reader: the comments of the conversation, then the
// review comments on the diff, each with the login of its author.
func (p *Provider) Notes(ctx context.Context, cr core.Key) ([]core.ReviewNote, error) {
	op := "notes " + cr.String()
	if err := p.checkHost(op, cr); err != nil {
		return nil, err
	}
	notes := []core.ReviewNote{}
	for _, kind := range []string{"issues", "pulls"} {
		path := repoPath(cr, kind, strconv.Itoa(cr.Number), "comments") + "?per_page=" + strconv.Itoa(perPage)
		err := pagesJSON(ctx, p, op, path, func(page []commentJSON) {
			for _, c := range page {
				author := ghost
				if c.User != nil {
					author = c.User.Login
				}
				notes = append(notes, core.ReviewNote{Author: author, Body: c.Body})
			}
		})
		if err != nil {
			return nil, err
		}
	}
	return notes, nil
}

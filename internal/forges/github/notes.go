package github

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

type commentJSON struct {
	User *struct {
		Login string `json:"login"`
	} `json:"user"`
	// AuthorAssociation is how the author relates to the repository.
	AuthorAssociation string `json:"author_association"`
	Body              string `json:"body"`
}

// ghost is the login GitHub shows for an account that was deleted.
const ghost = "ghost"

// trustedAssociation reports whether GitHub shows an author as someone who
// can speak for the repository: its owner, a member of its organization or
// a collaborator. A contributor, a first-time contributor and everyone else
// are not trusted, nor is a deleted account.
func trustedAssociation(a string) bool {
	switch a {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}
	return false
}

// Notes implements base.Reader: the comments of the conversation, then the
// review comments on the diff, each with the login of its author. A note is
// trusted when its author is an owner, a member or a collaborator of the
// repository, as GitHub says in the payload, or is the account of the
// token. The login of the token is read only when a note needs it.
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
				notes = append(notes, core.ReviewNote{Author: author, Body: c.Body, Trusted: trustedAssociation(c.AuthorAssociation)})
			}
		})
		if err != nil {
			return nil, err
		}
	}
	self := ""
	for i, n := range notes {
		if n.Trusted || n.Author == ghost {
			continue
		}
		if self == "" {
			var err error
			if self, err = p.Self(ctx); err != nil {
				return nil, err
			}
		}
		notes[i].Trusted = strings.EqualFold(n.Author, self)
	}
	return notes, nil
}

// Self implements base.Reader: the login of the account the token belongs
// to, read once.
func (p *Provider) Self(ctx context.Context) (string, error) {
	p.mu.Lock()
	login := p.self
	p.mu.Unlock()
	if login != "" {
		return login, nil
	}
	const op = "token account"
	var v struct {
		Login string `json:"login"`
	}
	if err := p.getJSON(ctx, op, "/user", &v); err != nil {
		return "", err
	}
	if v.Login == "" {
		return "", &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("the answer has no login")}
	}
	p.mu.Lock()
	p.self = v.Login
	p.mu.Unlock()
	return v.Login, nil
}

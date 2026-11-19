package github

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// Tree implements base.Reader. The tree is read at the first List, once;
// ref should be a commit SHA or a branch name without a slash, which the
// API path takes.
func (p *Provider) Tree(ctx context.Context, project core.Key, ref string) rules.Tree {
	return &tree{p: p, ctx: ctx, k: project, ref: ref}
}

type tree struct {
	p   *Provider
	ctx context.Context
	k   core.Key
	ref string

	once  sync.Once
	paths []string
	err   error
}

// List implements rules.Tree.
func (t *tree) List(prefix string) ([]string, error) {
	t.once.Do(t.load)
	if t.err != nil {
		return nil, t.err
	}
	out := []string{}
	for _, p := range t.paths {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (t *tree) load() {
	op := "tree " + t.k.ProjectKey() + "@" + t.ref
	if t.err = t.p.checkHost(op, t.k); t.err != nil {
		return
	}
	var v struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	path := repoPath(t.k, "git", "trees", url.PathEscape(t.ref)) + "?recursive=1"
	resp, err := t.p.get(t.ctx, path, "")
	if err != nil {
		err = t.p.fail(op, err)
		if isKind(err, base.ErrNotFound) {
			// Not found is a missing ref only if the repository can be read.
			if rerr := t.p.readableRepo(t.ctx, t.k); rerr != nil {
				err = rerr
			}
		}
		t.err = err
		return
	}
	if t.err = decode(op, resp.Body, &v); t.err != nil {
		return
	}
	if v.Truncated {
		t.err = &base.Error{Kind: base.ErrTreeTruncated, Op: op}
		return
	}
	for _, e := range v.Tree {
		if e.Type == "blob" {
			t.paths = append(t.paths, e.Path)
		}
	}
	slices.Sort(t.paths)
}

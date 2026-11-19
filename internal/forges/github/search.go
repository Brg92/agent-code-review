package github

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// search is the rules.Search of the provider.
type search struct{ p *Provider }

// Search implements base.Reader.
func (p *Provider) Search() rules.Search { return search{p} }

// Search implements rules.Search. GitHub's code search only indexes the
// default branch, so on any other ref it is ErrSearchUnavailable without a
// search being sent. It returns files and fragments, not line numbers: each
// file it returns is read at ref and its matching lines are returned. A
// search that is incomplete, longer than what is read, or that returns a
// file with no matching line proves nothing, and is ErrSearchUnavailable,
// never an empty result. It has no context to carry, so it uses the
// transport's timeouts.
func (s search) Search(project, ref, term string) ([]rules.Match, error) {
	ctx := context.Background()
	k, err := core.ParseKey(project + "#1")
	op := "search " + project + "@" + ref
	if err != nil {
		return nil, &base.Error{Kind: base.ErrNotFound, Op: op, Err: errors.New("not a project key")}
	}
	if err := s.p.checkHost(op, k); err != nil {
		return nil, err
	}
	defaultBranch, err := s.p.repoInfo(ctx, k)
	if err != nil {
		return nil, err
	}
	if !s.p.Capabilities().SearchOn(ref, defaultBranch) {
		return nil, &base.Error{Kind: base.ErrSearchUnavailable, Op: op}
	}

	q := url.Values{"q": {term + " repo:" + k.Project}, "per_page": {strconv.Itoa(perPage)}}
	var paths []string
	incomplete, total := false, 0
	err = pagesJSON(ctx, s.p, op, "/search/code?"+q.Encode(), func(page struct {
		Total      int  `json:"total_count"`
		Incomplete bool `json:"incomplete_results"`
		Items      []struct {
			Path string `json:"path"`
		} `json:"items"`
	}) {
		total, incomplete = page.Total, incomplete || page.Incomplete
		for _, it := range page.Items {
			paths = append(paths, it.Path)
		}
	})
	if err != nil {
		// A query GitHub cannot run, or on a repository it cannot search,
		// is a search that is not available.
		if isStatus(err, 422) {
			return nil, &base.Error{Kind: base.ErrSearchUnavailable, Op: op, Err: err}
		}
		return nil, err
	}
	if incomplete || total > len(paths) || len(paths) > s.p.maxSearchFiles {
		return nil, &base.Error{Kind: base.ErrSearchUnavailable, Op: op,
			Err: errors.New("incomplete or too long for the files that are read")}
	}

	sort.Strings(paths)
	out := []rules.Match{}
	for _, path := range paths {
		content, found, err := (files{s.p}).Get(ctx, k, ref, path)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &base.Error{Kind: base.ErrNotFound, Op: op, Err: errors.New("a file the search returned cannot be read: " + path)}
		}
		matches := matchingLines(path, string(content), term)
		if len(matches) == 0 {
			return nil, &base.Error{Kind: base.ErrSearchUnavailable, Op: op, Err: errors.New("no line of " + path + " holds the term")}
		}
		out = append(out, matches...)
	}
	return out, nil
}

// matchingLines returns the lines of content that hold term, in the case it
// is written or, if none does, in any case: GitHub's search ignores case.
func matchingLines(path, content, term string) []rules.Match {
	lines := strings.Split(content, "\n")
	for _, fold := range []bool{false, true} {
		var out []rules.Match
		for i, line := range lines {
			line = strings.TrimSuffix(line, "\r")
			if (!fold && strings.Contains(line, term)) || (fold && strings.Contains(strings.ToLower(line), strings.ToLower(term))) {
				out = append(out, rules.Match{Path: path, Line: i + 1, Text: line})
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

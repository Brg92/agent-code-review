package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// queueSearch is the search of the review queue: the open pull requests
// where the account of the token is a requested reviewer, on repositories
// that are not archived.
const queueSearch = "is:pr is:open review-requested:@me archived:false"

// ReviewQueue implements base.Queue. It is one request to the search API,
// the first page, sorted by the last update, so that its cost is one of the
// 30 requests a minute the search API allows; a rate limit is handled by the
// transport like any other.
func (p *Provider) ReviewQueue(ctx context.Context, limit int) (base.QueueResult, error) {
	const op = "review queue"
	if limit < 1 || limit > base.MaxQueueLimit {
		return base.QueueResult{}, fmt.Errorf("%s: the limit is from 1 to %d, not %d", op, base.MaxQueueLimit, limit)
	}
	q := url.Values{"q": {queueSearch}, "sort": {"updated"}, "order": {"desc"}, "per_page": {strconv.Itoa(limit)}}
	var page struct {
		Total      int  `json:"total_count"`
		Incomplete bool `json:"incomplete_results"`
		Items      []struct {
			Number        int       `json:"number"`
			Title         string    `json:"title"`
			Draft         bool      `json:"draft"`
			UpdatedAt     time.Time `json:"updated_at"`
			RepositoryURL string    `json:"repository_url"`
			User          struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"items"`
	}
	if err := p.getJSON(ctx, op, "/search/issues?"+q.Encode(), &page); err != nil {
		return base.QueueResult{}, err
	}
	res := base.QueueResult{Total: page.Total, Incomplete: page.Incomplete}
	for _, it := range page.Items {
		if len(res.Items) == limit {
			break
		}
		key, ok := p.queueKey(it.RepositoryURL, it.Number)
		if !ok {
			res.Skipped++
			continue
		}
		res.Items = append(res.Items, base.QueueItem{Key: key, Title: it.Title, Author: it.User.Login, Draft: it.Draft, UpdatedAt: it.UpdatedAt.UTC()})
	}
	return res, nil
}

// queueKey is the key of pull request number of the repository that
// repositoryURL, the API address of a repository, names.
func (p *Provider) queueKey(repositoryURL string, number int) (core.Key, bool) {
	u, err := url.Parse(repositoryURL)
	if err != nil || number < 1 {
		return core.Key{}, false
	}
	project, ok := strings.CutPrefix(u.Path, "/repos/")
	if !ok {
		return core.Key{}, false
	}
	key, err := core.ParseKey(fmt.Sprintf("%s:%s/%s#%d", core.GitHub, p.host, project, number))
	return key, err == nil
}

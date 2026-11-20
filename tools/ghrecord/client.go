package ghrecord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Client sends read-only requests to the GitHub REST API and writes one
// sanitized recording per response page into Dir.
type Client struct {
	BaseURL   string
	Token     string
	HTTP      *http.Client
	Sanitizer *Sanitizer
	Dir       string
	// SearchThrottle spaces the code search requests; nil sends them
	// without waiting.
	SearchThrottle *Throttle
	// repos are the repositories already recorded by this client.
	repos map[string]bool
}

// maxBody bounds the size of a response body.
const maxBody = 16 << 20

// Get sends a GET request, follows its pages and records each one. It
// returns the status of the first page and the raw body of every page.
func (c *Client) Get(req Request) (int, [][]byte, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return 0, nil, fmt.Errorf("base URL: %w", err)
	}
	basePath := strings.TrimSuffix(base.Path, "/")
	next := *base
	next.Path = basePath + req.Path
	values := url.Values{}
	for k, v := range req.Query {
		values.Set(k, v)
	}
	next.RawQuery = values.Encode()

	first := 0
	var pages [][]byte
	for {
		status, body, link, err := c.fetch(&next, basePath)
		if err != nil {
			return 0, nil, err
		}
		if first == 0 {
			first = status
		}
		pages = append(pages, body)
		target := NextLink(link)
		if status != http.StatusOK || target == "" {
			return first, pages, nil
		}
		u, err := url.Parse(target)
		if err != nil {
			return 0, nil, fmt.Errorf("%s: next link: %w", req.Path, err)
		}
		if u.Scheme != base.Scheme || u.Host != base.Host || !strings.HasPrefix(u.Path, basePath+"/") {
			return 0, nil, fmt.Errorf("%s: next link leaves the API base URL", req.Path)
		}
		next = *u
	}
}

// fetch sends one GET request and records its response.
func (c *Client) fetch(u *url.URL, basePath string) (int, []byte, string, error) {
	path := strings.TrimPrefix(u.Path, basePath)
	query := map[string]string{}
	for k, v := range u.Query() {
		if len(v) != 1 {
			return 0, nil, "", fmt.Errorf("%s: query parameter %q has %d values", path, k, len(v))
		}
		query[k] = v[0]
	}
	if path == SearchPath {
		c.SearchThrottle.Wait()
	}
	httpReq, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, nil, "", err
	}
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	httpReq.Header.Set("User-Agent", "agent-code-review-recorder")
	httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return 0, nil, "", fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return 0, nil, "", fmt.Errorf("GET %s: %w", path, err)
	}
	if len(body) > maxBody {
		return 0, nil, "", fmt.Errorf("GET %s: body larger than %d bytes", path, maxBody)
	}

	rec := Recording{Method: http.MethodGet, Path: path, Query: query, Status: resp.StatusCode, Headers: RecordedHeaders(resp.Header)}
	for k, v := range rec.Headers {
		rec.Headers[k] = c.Sanitizer.Text(v)
	}
	if len(bytes.TrimSpace(body)) > 0 {
		clean, err := c.Sanitizer.JSON(body)
		if err != nil {
			return 0, nil, "", fmt.Errorf("GET %s: %w", path, err)
		}
		rec.Body = clean
	}
	data, err := rec.Encode()
	if err != nil {
		return 0, nil, "", err
	}
	if err := os.WriteFile(filepath.Join(c.Dir, FileName(rec.Method, rec.Path, rec.Query)), data, 0o644); err != nil {
		return 0, nil, "", err
	}
	return resp.StatusCode, body, resp.Header.Get("Link"), nil
}

// Record records every request a review of the target makes. A number that
// is not a pull request is looked up as an issue, which tells an issue from
// a number that does not exist.
func (c *Client) Record(t Target) error {
	if err := c.recordRepo(t); err != nil {
		return err
	}
	status, pages, err := c.Get(PullRequestRequest(t))
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		status, _, err := c.Get(IssueRequest(t))
		if err == nil && status != http.StatusOK && status != http.StatusNotFound {
			err = fmt.Errorf("%s: issue: status %d", t, status)
		}
		return err
	default:
		return fmt.Errorf("%s: status %d", t, status)
	}
	var pr PullRequest
	if err := json.Unmarshal(pages[0], &pr); err != nil {
		return fmt.Errorf("%s: %w", t, err)
	}

	status, pages, err = c.Get(FilesRequest(t))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: files: status %d", t, status)
	}
	var files []PullRequestFile
	for _, page := range pages {
		var batch []PullRequestFile
		if err := json.Unmarshal(page, &batch); err != nil {
			return fmt.Errorf("%s: files: %w", t, err)
		}
		files = append(files, batch...)
	}

	if err := c.recordTree(t, pr.Head.SHA); err != nil {
		return err
	}
	for _, req := range FollowUps(t, pr, files) {
		status, _, err := c.Get(req)
		if err != nil {
			return err
		}
		if status != http.StatusOK && status != http.StatusNotFound {
			return fmt.Errorf("%s: %s: status %d", t, req.Path, status)
		}
	}
	return nil
}

// recordTree records the recursive tree at the head commit, with its
// truncated flag as GitHub returns it. There is no tree at the base.
func (c *Client) recordTree(t Target, sha string) error {
	status, _, err := c.Get(TreeRequest(t, sha))
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("%s: tree at %s: status %d", t, sha, status)
	}
	return nil
}

// SearchResult is what a recorded code search found.
type SearchResult struct {
	Total int
	// Incomplete is the incomplete_results flag as GitHub returned it.
	Incomplete bool
	// Paths are the files returned, in order and without repeats.
	Paths []string
}

// RecordSearch records the code search of term in owner/repo and then the
// contents of every file it returns at ref, because a code search returns
// files and fragments, not line numbers. Code search only indexes the
// default branch, so ref is that branch. An empty or incomplete result is
// recorded as returned and reported in the result.
func (c *Client) RecordSearch(owner, repo, term, ref string) (SearchResult, error) {
	req := SearchRequest(owner, repo, term)
	status, pages, err := c.Get(req)
	if err != nil {
		return SearchResult{}, err
	}
	if status != http.StatusOK {
		return SearchResult{}, fmt.Errorf("search %q in %s/%s: status %d", term, owner, repo, status)
	}
	var res SearchResult
	seen := map[string]bool{}
	for _, page := range pages {
		var body struct {
			Total      int  `json:"total_count"`
			Incomplete bool `json:"incomplete_results"`
			Items      []struct {
				Path string `json:"path"`
			} `json:"items"`
		}
		if err := json.Unmarshal(page, &body); err != nil {
			return SearchResult{}, fmt.Errorf("search %q in %s/%s: %w", term, owner, repo, err)
		}
		res.Total, res.Incomplete = body.Total, res.Incomplete || body.Incomplete
		for _, it := range body.Items {
			if !seen[it.Path] {
				seen[it.Path] = true
				res.Paths = append(res.Paths, it.Path)
			}
		}
	}
	t := Target{Owner: owner, Repo: repo}
	for _, p := range res.Paths {
		status, _, err := c.Get(contents(t, p, ref))
		if err != nil {
			return SearchResult{}, err
		}
		if status != http.StatusOK {
			return SearchResult{}, fmt.Errorf("contents of %s at %s: status %d", p, ref, status)
		}
	}
	return res, nil
}

// recordRepo records the repository of the target, once per client. The
// repository gives the default branch, and tells a missing file from a
// repository the token cannot see.
func (c *Client) recordRepo(t Target) error {
	if c.repos[t.repoPath()] {
		return nil
	}
	if status, _, err := c.Get(RepoRequest(t)); err != nil {
		return err
	} else if status != http.StatusOK {
		return fmt.Errorf("%s: repository: status %d", t, status)
	}
	if c.repos == nil {
		c.repos = map[string]bool{}
	}
	c.repos[t.repoPath()] = true
	return nil
}

// isRecorded reports whether an earlier run recorded the request.
func (c *Client) isRecorded(req Request) bool {
	_, err := os.Stat(filepath.Join(c.Dir, FileName(http.MethodGet, req.Path, req.Query)))
	return err == nil
}

// RecordMissing records the requests a review of an already recorded pull
// request makes that no earlier run recorded: the repository, the tree at
// the head commit, and what FollowUps lists except the contents of the
// changed files. It reads the pull request from Dir and sends no request
// that has a recording, so every existing recording stays as it is. A
// number recorded as not being a pull request only needs its repository.
func (c *Client) RecordMissing(t Target) error {
	req := PullRequestRequest(t)
	data, err := os.ReadFile(filepath.Join(c.Dir, FileName(http.MethodGet, req.Path, req.Query)))
	if err != nil {
		return fmt.Errorf("%s: the pull request is not recorded: %w", t, err)
	}
	var rec Recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return fmt.Errorf("%s: %w", t, err)
	}
	if !c.isRecorded(RepoRequest(t)) {
		if err := c.recordRepo(t); err != nil {
			return err
		}
	}
	if rec.Status != http.StatusOK {
		return nil
	}
	var pr PullRequest
	if err := json.Unmarshal(rec.Body, &pr); err != nil || pr.Head.SHA == "" {
		return fmt.Errorf("%s: the recorded pull request has no head commit", t)
	}
	if tree := TreeRequest(t, pr.Head.SHA); !c.isRecorded(tree) {
		if err := c.recordTree(t, pr.Head.SHA); err != nil {
			return err
		}
	}
	for _, req := range FollowUps(t, pr, nil) {
		if c.isRecorded(req) {
			continue
		}
		status, _, err := c.Get(req)
		if err != nil {
			return err
		}
		if status != http.StatusOK && status != http.StatusNotFound {
			return fmt.Errorf("%s: %s: status %d", t, req.Path, status)
		}
	}
	return nil
}

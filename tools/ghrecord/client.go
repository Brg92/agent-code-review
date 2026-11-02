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

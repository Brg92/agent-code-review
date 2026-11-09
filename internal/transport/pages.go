package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	// ErrPageLimit is returned with the pages read so far when more pages
	// follow the caller's cap.
	ErrPageLimit = errors.New("transport: page limit reached")
	// ErrForeignLink is returned when a next link points to another host.
	ErrForeignLink = errors.New("transport: next page on another host refused")
	// ErrPageLoop is returned when a next link points to a page already read.
	ErrPageLoop = errors.New("transport: next page already read")
)

// GetPages reads path and every page its Link header chains with rel="next",
// up to maxPages pages. When more pages follow, it returns the pages read and
// an error wrapping ErrPageLimit.
func (c *Client) GetPages(ctx context.Context, path, accept string, maxPages int) ([]*Response, error) {
	if maxPages < 1 {
		return nil, fmt.Errorf("transport: page cap %d must be at least 1", maxPages)
	}
	u, err := c.resolve(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var pages []*Response
	for {
		seen[u.String()] = true
		resp, err := c.get(ctx, u, accept)
		if err != nil {
			return pages, err
		}
		pages = append(pages, resp)
		next := nextLink(resp.Header)
		if next == "" {
			return pages, nil
		}
		nu, err := u.Parse(next)
		if err != nil {
			return pages, fmt.Errorf("transport: GET %s: invalid next link: %w", redactURL(u.String()), err)
		}
		if nu.Scheme != c.base.Scheme || !strings.EqualFold(nu.Host, c.base.Host) {
			return pages, fmt.Errorf("transport: GET %s: %w", redactURL(u.String()), ErrForeignLink)
		}
		nu.Fragment, nu.RawFragment = "", ""
		if seen[nu.String()] {
			return pages, fmt.Errorf("transport: GET %s: %w", redactURL(nu.String()), ErrPageLoop)
		}
		if len(pages) >= maxPages {
			return pages, fmt.Errorf("transport: GET %s: %w after %d pages", redactURL(c.base.String()+path), ErrPageLimit, maxPages)
		}
		u = nu
	}
}

// nextLink returns the target of the rel="next" link in an RFC 8288 Link
// header, or "".
func nextLink(h http.Header) string {
	for _, v := range h.Values("Link") {
		for v = strings.TrimSpace(v); v != ""; {
			end := strings.IndexByte(v, '>')
			if v[0] != '<' || end < 0 {
				break
			}
			target := v[1:end]
			rest := v[end+1:]
			params := rest
			if i := strings.IndexByte(rest, ','); i >= 0 {
				params, rest = rest[:i], rest[i+1:]
			} else {
				rest = ""
			}
			if hasNextRel(params) {
				return target
			}
			v = strings.TrimSpace(rest)
		}
	}
	return ""
}

func hasNextRel(params string) bool {
	for _, p := range strings.Split(params, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
			continue
		}
		for _, rel := range strings.Fields(strings.Trim(strings.TrimSpace(value), `"`)) {
			if strings.EqualFold(rel, "next") {
				return true
			}
		}
	}
	return false
}

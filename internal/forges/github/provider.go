package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/transport"
)

// Resolver gives the token of a host; auth.Resolver is one. The token only
// goes to the transport.
type Resolver interface {
	Resolve(ctx context.Context, host string) (auth.Token, error)
}

// Options tune a Provider. The zero value is what the CLI uses.
type Options struct {
	// Transport sets the timeouts, the retries and the clock. Its BaseURL,
	// Token, Policy and MaxWait are set by New and ignored.
	Transport transport.Config
	// Limits are the rate-limit parameters; zero means DefaultLimits.
	Limits Limits
	// MaxPages caps the pages read of a list; zero means DefaultMaxPages.
	MaxPages int
	// MaxSearchFiles caps the files a code search may return, each of
	// which is read; more is search unavailable. Zero means
	// DefaultMaxSearchFiles.
	MaxSearchFiles int
}

// Defaults of Options.
const (
	DefaultMaxPages       = 50
	DefaultMaxSearchFiles = 100
)

// perPage is the largest page GitHub gives.
const perPage = 100

const (
	acceptJSON = "application/vnd.github+json"
	acceptRaw  = "application/vnd.github.raw+json"
)

// Provider reads GitHub. It implements base.Reader and has no way to write.
type Provider struct {
	host           string
	client         *transport.Client
	maxPages       int
	maxSearchFiles int

	// seq makes the requests sequential, whoever asks.
	seq sync.Mutex

	mu    sync.Mutex
	repos map[string]repoState
	// self is the login of the token's account, once read.
	self string
}

var _ base.Reader = (*Provider)(nil)

// repoState is what is known of a repository: its default branch, or why
// it cannot be read.
type repoState struct {
	defaultBranch string
	err           error
}

// New returns the reader of host, which must be in the host table. The
// token of the host is resolved through tokens at each request and handed to
// the transport alone.
func New(host string, tokens Resolver, opts Options) (base.Reader, error) {
	api, ok := apiBases[strings.ToLower(host)]
	if !ok {
		return nil, &UnsupportedHostError{Host: host}
	}
	p, err := newProvider(strings.ToLower(host), api, tokens, opts)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// newProvider is New for an API base URL of the caller's, which tests point
// at a replay server.
func newProvider(host, apiBase string, tokens Resolver, opts Options) (*Provider, error) {
	limits := opts.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits
	}
	cfg := opts.Transport
	cfg.BaseURL = apiBase
	cfg.MaxWait = limits.MaxWait
	cfg.Policy = limits.policy()
	cfg.Allow = writeEndpoints
	cfg.Token = nil
	if tokens != nil {
		cfg.Token = func(ctx context.Context) (string, error) {
			t, err := tokens.Resolve(ctx, host)
			if err != nil {
				return "", err
			}
			return t.Value(), nil
		}
	}
	client, err := transport.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	p := &Provider{host: host, client: client, maxPages: opts.MaxPages, maxSearchFiles: opts.MaxSearchFiles, repos: map[string]repoState{}}
	if p.maxPages <= 0 {
		p.maxPages = DefaultMaxPages
	}
	if p.maxSearchFiles <= 0 {
		p.maxSearchFiles = DefaultMaxSearchFiles
	}
	return p, nil
}

// Capabilities implements base.Reader.
func (p *Provider) Capabilities() base.Capabilities { return Capabilities() }

// Changes implements base.Reader.
func (p *Provider) Changes() cascade.Source { return changes{p} }

// Files implements base.Reader.
func (p *Provider) Files() core.Files { return files{p} }

// checkHost refuses a key of another host before anything is sent.
func (p *Provider) checkHost(op string, k core.Key) error {
	if k.Forge == core.GitHub && strings.EqualFold(k.Host, p.host) {
		return nil
	}
	return &base.Error{Kind: base.ErrNotFound, Op: op, Err: &UnsupportedHostError{Host: k.Host}}
}

// repoPath is the API path of a resource of the repository of k.
func repoPath(k core.Key, rest ...string) string {
	var b strings.Builder
	b.WriteString("/repos")
	for _, seg := range strings.Split(k.Project, "/") {
		b.WriteString("/" + url.PathEscape(seg))
	}
	for _, r := range rest {
		b.WriteString("/" + r)
	}
	return b.String()
}

// escapePath escapes each segment of a file path.
func escapePath(path string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// get sends one GET. Requests are sequential.
func (p *Provider) get(ctx context.Context, path, accept string) (*transport.Response, error) {
	p.seq.Lock()
	defer p.seq.Unlock()
	if accept == "" {
		accept = acceptJSON
	}
	return p.client.Get(ctx, path, accept)
}

// post sends one POST with a JSON body. Requests are sequential. The
// transport sends it only to the endpoints of writeEndpoints.
func (p *Provider) post(ctx context.Context, path string, body []byte) (*transport.Response, error) {
	p.seq.Lock()
	defer p.seq.Unlock()
	return p.client.Post(ctx, path, acceptJSON, body)
}

// patch sends one PATCH with a JSON body. Requests are sequential.
func (p *Provider) patch(ctx context.Context, path string, body []byte) (*transport.Response, error) {
	p.seq.Lock()
	defer p.seq.Unlock()
	return p.client.Patch(ctx, path, acceptJSON, body)
}

// pages reads a list and the pages that follow it, up to the page cap; a
// longer list is an error, never the part read.
func (p *Provider) pages(ctx context.Context, path string) ([]*transport.Response, error) {
	p.seq.Lock()
	defer p.seq.Unlock()
	return p.client.GetPages(ctx, path, acceptJSON, p.maxPages)
}

// getJSON reads path and decodes the body into v.
func (p *Provider) getJSON(ctx context.Context, op, path string, v any) error {
	resp, err := p.get(ctx, path, "")
	if err != nil {
		return p.fail(op, err)
	}
	return decode(op, resp.Body, v)
}

func decode(op string, body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: fmt.Errorf("unreadable answer: %w", err)}
	}
	return nil
}

// pagesJSON reads every page of a list and decodes each page into a new
// value that add receives.
func pagesJSON[T any](ctx context.Context, p *Provider, op, path string, add func(T)) error {
	pages, err := p.pages(ctx, path)
	if err != nil {
		return p.fail(op, err)
	}
	for _, page := range pages {
		var v T
		if err := decode(op, page.Body, &v); err != nil {
			return err
		}
		add(v)
	}
	return nil
}

// repoInfo reads the repository of k once and keeps the answer: its default
// branch, or the reason it cannot be read. GitHub answers 404 both for a
// missing object and for a repository the token cannot see, so a 404 on a
// file or a tree is only a missing file when this succeeds.
func (p *Provider) repoInfo(ctx context.Context, k core.Key) (string, error) {
	p.mu.Lock()
	st, ok := p.repos[k.Project]
	p.mu.Unlock()
	if ok {
		return st.defaultBranch, st.err
	}
	op := "repository " + k.Project
	resp, err := p.get(ctx, repoPath(k), "")
	if err != nil {
		err = p.fail(op, err)
		// Only an answer is kept: a limit or a network failure may pass.
		if !isKind(err, base.ErrNotFound) && !isKind(err, base.ErrForbidden) {
			return "", err
		}
		st = repoState{err: err}
	} else {
		var v struct {
			DefaultBranch string `json:"default_branch"`
		}
		if err := decode(op, resp.Body, &v); err != nil {
			return "", err
		}
		st = repoState{defaultBranch: v.DefaultBranch}
	}
	p.mu.Lock()
	p.repos[k.Project] = st
	p.mu.Unlock()
	return st.defaultBranch, st.err
}

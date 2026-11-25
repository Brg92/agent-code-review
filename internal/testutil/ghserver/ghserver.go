// Package ghserver is a synthetic GitHub API for the tests of what publish
// reads and writes: pull requests with their comments, review comments and
// threads, issues, labels and assignable accounts, kept in memory, and the
// requests it received, with their bodies and in order. It can fail on
// demand with an HTTP status, a rate limit or a dropped connection. It is
// test support code only, and it is what stands between the tests and the
// real sandbox: no write ever reaches GitHub from a test.
package ghserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Request is a request the server received.
type Request struct {
	Method string
	Path   string
	Query  string
	Body   string
}

// String is "METHOD path".
func (r Request) String() string { return r.Method + " " + r.Path }

// Fault makes requests fail.
type Fault struct {
	// Status is the HTTP status to answer with; Body its body.
	Status int
	Body   string
	Header map[string]string
	// Drop closes the connection without an answer.
	Drop bool
	// Apply makes the server do what was asked and then fail, as a
	// platform does when the answer is lost on the way: the write is there
	// and its caller does not know.
	Apply bool
	// Times is how many requests fail; 0 means every one. After a fault is
	// spent the request is served as usual.
	Times int

	method  string
	pattern *regexp.Regexp
	used    int
}

// Comment is a comment on the conversation of a pull request.
type Comment struct {
	ID     int
	Author string
	// Assoc is the author_association GitHub shows.
	Assoc string
	Body  string
}

// Inline is a review comment on the diff, which GraphQL shows as the first
// comment of a thread.
type Inline struct {
	Comment
	Path      string
	Side      string
	Line      int
	StartLine int
	// Resolved is the state of its thread.
	Resolved bool
}

// PR is a pull request.
type PR struct {
	Number      int
	Title, Body string
	State       string
	Merged      bool
	Draft       bool
	Author      string
	Head, Base  string
	TargetRef   string
	Comments    []*Comment
	Inlines     []*Inline
	// Lines are the lines the diff shows, by path and side ("LEFT",
	// "RIGHT"); an inline comment anywhere else is refused with a 422. Nil
	// shows every line.
	Lines map[string]map[string][]int
}

// Issue is an issue.
type Issue struct {
	Number    int
	Open      bool
	Author    string
	Assoc     string
	Title     string
	Body      string
	Labels    []string
	Assignees []string
}

// Repo is a repository.
type Repo struct {
	Name          string
	DefaultBranch string
	Labels        map[string]bool
	// Assignable are the accounts GitHub lets be assigned.
	Assignable map[string]bool
	PRs        map[int]*PR
	Issues     []*Issue
}

// Server is the synthetic GitHub.
type Server struct {
	t   testing.TB
	srv *httptest.Server

	// Token is the bearer token the server requires; empty accepts any.
	Token string
	// Login is the account the token belongs to; Assoc what GitHub calls
	// it in comments it writes.
	Login string
	Assoc string
	// NameRejected makes a 422 for the position of a review comment say
	// which comment it was; otherwise it does not.
	NameRejected bool

	mu       sync.Mutex
	repos    map[string]*Repo
	reqs     []Request
	faults   []*Fault
	nextID   int
	mutation bool
}

// New starts a server. It is closed when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{t: t, Login: "token-login", Assoc: "COLLABORATOR", repos: map[string]*Repo{}, nextID: 1000}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the base URL of the API.
func (s *Server) URL() string { return s.srv.URL }

// AddRepo adds a repository whose accounts in assignable can be assigned.
func (s *Server) AddRepo(name string, assignable ...string) *Repo {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &Repo{Name: name, DefaultBranch: "main", Labels: map[string]bool{}, Assignable: map[string]bool{}, PRs: map[int]*PR{}}
	for _, a := range assignable {
		r.Assignable[a] = true
	}
	s.repos[strings.ToLower(name)] = r
	return r
}

// AddPR adds an open pull request to the repository.
func (r *Repo) AddPR(number int, author, head string) *PR {
	pr := &PR{Number: number, Title: fmt.Sprintf("Change %d", number), State: "open", Author: author, Head: head, Base: strings.Repeat("b", 40), TargetRef: "main"}
	r.PRs[number] = pr
	return pr
}

// AddIssue adds an open issue and returns it.
func (r *Repo) AddIssue(author, assoc, title, body string, labels ...string) *Issue {
	is := &Issue{Number: r.nextNumber(), Open: true, Author: author, Assoc: assoc, Title: title, Body: body, Labels: labels}
	r.Issues = append(r.Issues, is)
	return is
}

// nextNumber is the next number of the issues and pull requests, which
// share one counter.
func (r *Repo) nextNumber() int {
	n := len(r.Issues)
	for k := range r.PRs {
		n = max(n, k)
	}
	for _, is := range r.Issues {
		n = max(n, is.Number)
	}
	return n + 1
}

// Lock runs f with the state locked, for tests that change it while the
// server runs.
func (s *Server) Lock(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

// Repo returns a repository, or fails the test.
func (s *Server) Repo(name string) *Repo {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repos[strings.ToLower(name)]
	if r == nil {
		s.t.Fatalf("ghserver: no repository %s", name)
	}
	return r
}

// Fail makes the requests of method whose path matches the regular
// expression fail.
func (s *Server) Fail(method, pathPattern string, f Fault) *Fault {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.method, f.pattern = method, regexp.MustCompile(pathPattern)
	s.faults = append(s.faults, &f)
	return &f
}

// Requests returns every request received, in order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.reqs...)
}

// Writes returns the requests that were not reads: every one that is not a
// GET, except the GraphQL queries.
func (s *Server) Writes() []Request {
	var out []Request
	for _, r := range s.Requests() {
		if r.Method != http.MethodGet && r.Path != "/graphql" {
			out = append(out, r)
		}
	}
	return out
}

// SawMutation reports whether a GraphQL document with a mutation reached
// the server.
func (s *Server) SawMutation() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mutation
}

func (s *Server) id() int { s.nextID++; return s.nextID }

var (
	pathRepo = regexp.MustCompile(`^/repos/([^/]+/[^/]+)(/.*)?$`)
)

func (s *Server) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, Request{Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery, Body: string(body)})
	if s.Token != "" && req.Header.Get("Authorization") != "Bearer "+s.Token {
		reply(w, 401, `{"message":"Bad credentials"}`)
		return
	}
	for _, f := range s.faults {
		if f.method != req.Method || !f.pattern.MatchString(req.URL.Path) || (f.Times > 0 && f.used >= f.Times) {
			continue
		}
		f.used++
		if f.Apply {
			s.dispatch(httptest.NewRecorder(), req, body)
		}
		if f.Drop {
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
			return
		}
		for k, v := range f.Header {
			w.Header().Set(k, v)
		}
		reply(w, f.Status, f.Body)
		return
	}
	s.dispatch(w, req, body)
}

// dispatch answers a request that no fault stops.
func (s *Server) dispatch(w http.ResponseWriter, req *http.Request, body []byte) {
	switch {
	case req.URL.Path == "/user" && req.Method == http.MethodGet:
		reply(w, 200, jsonOf(map[string]any{"login": s.Login}))
	case req.URL.Path == "/graphql" && req.Method == http.MethodPost:
		s.graphql(w, body)
	default:
		m := pathRepo.FindStringSubmatch(req.URL.Path)
		if m == nil {
			reply(w, 404, `{"message":"Not Found"}`)
			return
		}
		repo := s.repos[strings.ToLower(m[1])]
		if repo == nil {
			reply(w, 404, `{"message":"Not Found"}`)
			return
		}
		s.repo(w, req, repo, strings.Split(strings.Trim(m[2], "/"), "/"), body)
	}
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }

func notFound(w http.ResponseWriter) { reply(w, 404, `{"message":"Not Found"}`) }

func unprocessable(w http.ResponseWriter, errs ...any) {
	reply(w, 422, jsonOf(map[string]any{"message": "Unprocessable Entity", "errors": errs}))
}

func number(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0
}

func (s *Server) repo(w http.ResponseWriter, req *http.Request, r *Repo, seg []string, body []byte) {
	method := req.Method
	switch {
	case len(seg) == 1 && seg[0] == "" && method == "GET":
		reply(w, 200, jsonOf(map[string]any{"default_branch": r.DefaultBranch}))
	case len(seg) == 2 && seg[0] == "pulls" && (method == "GET" || method == "PATCH"):
		n, _ := number(seg[1])
		pr := r.PRs[n]
		if pr == nil {
			notFound(w)
			return
		}
		if method == "PATCH" {
			var in map[string]any
			if err := json.Unmarshal(body, &in); err != nil {
				unprocessable(w, "bad body")
				return
			}
			for k, v := range in {
				if k != "body" {
					s.t.Errorf("ghserver: PATCH pulls/%d sets %q, only the description may change", n, k)
				}
				if str, ok := v.(string); ok && k == "body" {
					pr.Body = str
				}
			}
		}
		reply(w, 200, s.pullJSON(pr))
	case len(seg) == 3 && seg[0] == "issues" && seg[2] == "comments" && method == "GET":
		n, _ := number(seg[1])
		pr := r.PRs[n]
		if pr == nil {
			notFound(w)
			return
		}
		var out []map[string]any
		for _, c := range pr.Comments {
			out = append(out, commentJSON(c))
		}
		reply(w, 200, jsonOf(nonNil(out)))
	case len(seg) == 3 && seg[0] == "issues" && seg[2] == "comments" && method == "POST":
		n, _ := number(seg[1])
		pr := r.PRs[n]
		var in struct {
			Body string `json:"body"`
		}
		if pr == nil {
			notFound(w)
			return
		}
		if json.Unmarshal(body, &in) != nil || strings.TrimSpace(in.Body) == "" {
			unprocessable(w, map[string]string{"resource": "IssueComment", "code": "missing_field", "field": "body"})
			return
		}
		c := &Comment{ID: s.id(), Author: s.Login, Assoc: s.Assoc, Body: in.Body}
		pr.Comments = append(pr.Comments, c)
		reply(w, 201, jsonOf(commentJSON(c)))
	case len(seg) == 3 && seg[0] == "pulls" && seg[2] == "comments" && method == "GET":
		n, _ := number(seg[1])
		pr := r.PRs[n]
		if pr == nil {
			notFound(w)
			return
		}
		var out []map[string]any
		for _, c := range pr.Inlines {
			out = append(out, commentJSON(&c.Comment))
		}
		reply(w, 200, jsonOf(nonNil(out)))
	case len(seg) == 3 && seg[0] == "pulls" && seg[2] == "reviews" && method == "POST":
		s.review(w, r, seg, body)
	case len(seg) == 1 && seg[0] == "issues" && method == "GET":
		s.listIssues(w, r, req.URL.Query())
	case len(seg) == 1 && seg[0] == "issues" && method == "POST":
		s.createIssue(w, r, body)
	case len(seg) == 2 && seg[0] == "issues" && method == "PATCH":
		s.updateIssue(w, r, seg[1], body)
	case len(seg) == 2 && seg[0] == "labels" && method == "GET":
		if r.Labels[unescape(seg[1])] {
			reply(w, 200, jsonOf(map[string]any{"name": unescape(seg[1])}))
			return
		}
		notFound(w)
	case len(seg) == 1 && seg[0] == "labels" && method == "POST":
		var in struct {
			Name        string `json:"name"`
			Color       string `json:"color"`
			Description string `json:"description"`
		}
		if json.Unmarshal(body, &in) != nil || in.Name == "" {
			unprocessable(w, map[string]string{"resource": "Label", "code": "missing_field", "field": "name"})
			return
		}
		if r.Labels[in.Name] {
			unprocessable(w, map[string]string{"resource": "Label", "code": "already_exists", "field": "name"})
			return
		}
		r.Labels[in.Name] = true
		reply(w, 201, jsonOf(map[string]any{"name": in.Name}))
	case len(seg) == 2 && seg[0] == "assignees" && method == "GET":
		if r.Assignable[unescape(seg[1])] {
			reply(w, 204, "")
			return
		}
		notFound(w)
	default:
		s.t.Errorf("ghserver: no route for %s %s", method, req.URL.Path)
		reply(w, 501, `{"message":"not implemented"}`)
	}
}

func unescape(s string) string {
	u, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return u
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func commentJSON(c *Comment) map[string]any {
	return map[string]any{"id": c.ID, "body": c.Body, "user": map[string]any{"login": c.Author}, "author_association": c.Assoc}
}

func (s *Server) pullJSON(pr *PR) string {
	return jsonOf(map[string]any{
		"number": pr.Number, "title": pr.Title, "body": pr.Body, "state": pr.State, "merged": pr.Merged, "draft": pr.Draft,
		"user": map[string]any{"login": pr.Author},
		"head": map[string]any{"sha": pr.Head},
		"base": map[string]any{"sha": pr.Base, "ref": pr.TargetRef},
	})
}

func (s *Server) listIssues(w http.ResponseWriter, r *Repo, q url.Values) {
	var out []map[string]any
	for _, is := range r.Issues {
		if l := q.Get("labels"); l != "" && !contains(is.Labels, l) {
			continue
		}
		if st := q.Get("state"); st == "open" && !is.Open || st == "closed" && is.Open {
			continue
		}
		state := "open"
		if !is.Open {
			state = "closed"
		}
		out = append(out, map[string]any{"number": is.Number, "state": state, "title": is.Title, "body": is.Body,
			"user": map[string]any{"login": is.Author}, "author_association": is.Assoc})
	}
	// A pull request is an issue too, which the API lists without a label.
	if q.Get("labels") == "" {
		for _, pr := range r.PRs {
			out = append(out, map[string]any{"number": pr.Number, "state": pr.State, "title": pr.Title, "body": pr.Body,
				"user": map[string]any{"login": pr.Author}, "author_association": "OWNER", "pull_request": map[string]any{}})
		}
	}
	reply(w, 200, jsonOf(nonNil(out)))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (s *Server) createIssue(w http.ResponseWriter, r *Repo, body []byte) {
	var in struct {
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Labels    []string `json:"labels"`
		Assignees []string `json:"assignees"`
	}
	if json.Unmarshal(body, &in) != nil || strings.TrimSpace(in.Title) == "" {
		unprocessable(w, map[string]string{"resource": "Issue", "code": "missing_field", "field": "title"})
		return
	}
	for _, l := range in.Labels {
		if !r.Labels[l] {
			// GitHub creates a label that does not exist; a test that
			// reaches this has skipped the label-create of its plan.
			s.t.Errorf("ghserver: issue created with the label %q, which the repository does not have", l)
		}
	}
	// GitHub silently drops assignees that cannot be assigned.
	var assigned []string
	for _, a := range in.Assignees {
		if r.Assignable[a] {
			assigned = append(assigned, a)
		}
	}
	is := &Issue{Number: r.nextNumber(), Open: true, Author: s.Login, Assoc: s.Assoc, Title: in.Title, Body: in.Body, Labels: in.Labels, Assignees: assigned}
	r.Issues = append(r.Issues, is)
	reply(w, 201, jsonOf(map[string]any{"number": is.Number, "state": "open", "title": is.Title, "body": is.Body, "user": map[string]any{"login": s.Login}}))
}

func (s *Server) updateIssue(w http.ResponseWriter, r *Repo, num string, body []byte) {
	n, _ := number(num)
	var is *Issue
	for _, i := range r.Issues {
		if i.Number == n {
			is = i
		}
	}
	if is == nil {
		notFound(w)
		return
	}
	var in map[string]any
	if json.Unmarshal(body, &in) != nil {
		unprocessable(w, "bad body")
		return
	}
	for k, v := range in {
		switch str, _ := v.(string); k {
		case "title":
			is.Title = str
		case "body":
			is.Body = str
		default:
			s.t.Errorf("ghserver: PATCH issues/%d sets %q, only the title and the body may change", n, k)
		}
	}
	reply(w, 200, jsonOf(map[string]any{"number": is.Number, "title": is.Title, "body": is.Body}))
}

func (s *Server) review(w http.ResponseWriter, r *Repo, seg []string, body []byte) {
	n, _ := number(seg[1])
	pr := r.PRs[n]
	if pr == nil {
		notFound(w)
		return
	}
	var in struct {
		CommitID string `json:"commit_id"`
		Body     string `json:"body"`
		Event    string `json:"event"`
		Comments []struct {
			Path      string `json:"path"`
			Body      string `json:"body"`
			Line      int    `json:"line"`
			Side      string `json:"side"`
			StartLine int    `json:"start_line"`
			StartSide string `json:"start_side"`
		} `json:"comments"`
	}
	if json.Unmarshal(body, &in) != nil {
		unprocessable(w, "bad body")
		return
	}
	if in.Event != "COMMENT" {
		s.t.Errorf("ghserver: a review with the event %q: only COMMENT is ever sent", in.Event)
		unprocessable(w, "event")
		return
	}
	var refused []int
	for i, c := range in.Comments {
		if !s.shows(pr, c.Path, c.Side, c.Line) || (c.StartLine > 0 && !s.shows(pr, c.Path, c.Side, c.StartLine)) {
			refused = append(refused, i)
		}
	}
	if len(refused) > 0 {
		var errs []any
		for _, i := range refused {
			// Without NameRejected the answer is the one GitHub gave on the
			// sandbox (testdata/githubwrite/review-422-line-outside-diff.json).
			msg := "Line could not be resolved"
			if s.NameRejected {
				msg = fmt.Sprintf("comments[%d]: Pull request review thread line must be part of the diff", i)
			}
			errs = append(errs, msg)
		}
		unprocessable(w, errs...)
		return
	}
	for _, c := range in.Comments {
		pr.Inlines = append(pr.Inlines, &Inline{Comment: Comment{ID: s.id(), Author: s.Login, Assoc: s.Assoc, Body: c.Body}, Path: c.Path, Side: c.Side, Line: c.Line, StartLine: c.StartLine})
	}
	reply(w, 200, jsonOf(map[string]any{"id": s.id(), "state": "COMMENTED", "body": in.Body, "commit_id": in.CommitID}))
}

func (s *Server) shows(pr *PR, path, side string, line int) bool {
	if pr.Lines == nil {
		return true
	}
	for _, l := range pr.Lines[path][side] {
		if l == line {
			return true
		}
	}
	return false
}

// graphql answers the query that reads the review threads, and records
// whether a mutation ever arrived.
func (s *Server) graphql(w http.ResponseWriter, body []byte) {
	var in struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if json.Unmarshal(body, &in) != nil {
		reply(w, 400, `{"message":"Problems parsing JSON"}`)
		return
	}
	if regexp.MustCompile(`\b(mutation|subscription)\b`).MatchString(in.Query) {
		s.mutation = true
		reply(w, 200, `{"errors":[{"type":"X","message":"mutations are not served"}]}`)
		return
	}
	owner, _ := in.Variables["owner"].(string)
	name, _ := in.Variables["name"].(string)
	num, _ := in.Variables["number"].(float64)
	repo := s.repos[strings.ToLower(owner+"/"+name)]
	if repo == nil || repo.PRs[int(num)] == nil {
		reply(w, 200, `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`)
		return
	}
	pr := repo.PRs[int(num)]
	start := 0
	if after, ok := in.Variables["after"].(string); ok {
		start, _ = strconv.Atoi(after)
	}
	end := min(start+100, len(pr.Inlines))
	var nodes []map[string]any
	for _, c := range pr.Inlines[start:end] {
		nodes = append(nodes, map[string]any{"isResolved": c.Resolved, "comments": map[string]any{"nodes": []any{map[string]any{
			"body": c.Body, "authorAssociation": c.Assoc, "author": map[string]any{"login": c.Author}}}}})
	}
	reply(w, 200, jsonOf(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": end < len(pr.Inlines), "endCursor": strconv.Itoa(end)},
		"nodes":    nonNil(nodes),
	}}}}}))
}

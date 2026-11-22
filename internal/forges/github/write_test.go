package github

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/ghserver"
)

const writeHead = "0123456789abcdef0123456789abcdef01234567"

// onServer is a provider on the synthetic GitHub, with a repository acme/app
// that has pull request 7 and a label.
func onServer(t *testing.T) (*Provider, *ghserver.Server, *ghserver.Repo, *tokens) {
	t.Helper()
	srv := ghserver.New(t)
	srv.Token = canary
	repo := srv.AddRepo("acme/app", "author-login")
	repo.AddPR(7, "author-login", writeHead)
	tok := &tokens{token: canary}
	p, err := newProvider("github.com", srv.URL(), tok, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	return p, srv, repo, tok
}

func bodyOf(t *testing.T, r ghserver.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(r.Body), &v); err != nil {
		t.Fatalf("%s: %v", r, err)
	}
	return v
}

func keysOf(m map[string]any) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestCreateReviewIsOneCommentOnlyReview(t *testing.T) {
	p, srv, repo, _ := onServer(t)
	err := p.CreateReview(context.Background(), synthKey, writeHead, []base.ReviewComment{
		{Path: "app/A.kt", Side: core.Right, Line: 12, Body: "first"},
		{Path: "app/B.kt", Side: core.Left, StartLine: 3, Line: 5, Body: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := srv.Writes()
	if len(w) != 1 || w[0].String() != "POST /repos/acme/app/pulls/7/reviews" {
		t.Fatalf("writes = %v, want one review", w)
	}
	in := bodyOf(t, w[0])
	if got, want := keysOf(in), []string{"comments", "commit_id", "event"}; !reflect.DeepEqual(got, want) {
		t.Errorf("review keys = %v, want %v: no body, no other field", got, want)
	}
	if in["event"] != "COMMENT" || in["commit_id"] != writeHead {
		t.Errorf("review = %v, want the event COMMENT on the head", in)
	}
	comments := in["comments"].([]any)
	first, second := comments[0].(map[string]any), comments[1].(map[string]any)
	if !reflect.DeepEqual(keysOf(first), []string{"body", "line", "path", "side"}) || first["line"] != float64(12) || first["side"] != "RIGHT" {
		t.Errorf("first comment = %v", first)
	}
	if !reflect.DeepEqual(keysOf(second), []string{"body", "line", "path", "side", "start_line", "start_side"}) ||
		second["start_line"] != float64(3) || second["start_side"] != "LEFT" || second["side"] != "LEFT" {
		t.Errorf("second comment = %v, want a range on the LEFT side", second)
	}
	if got := repo.PRs[7].Inlines; len(got) != 2 || got[0].Body != "first" || got[1].StartLine != 3 {
		t.Errorf("the pull request has %+v", got)
	}
}

func TestCreateReviewEventIsNeverAParameter(t *testing.T) {
	p, srv, _, _ := onServer(t)
	for range 3 {
		if err := p.CreateReview(context.Background(), synthKey, writeHead, []base.ReviewComment{{Path: "a", Side: core.Right, Line: 1, Body: "x"}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range srv.Writes() {
		for _, bad := range []string{"APPROVE", "REQUEST_CHANGES", "PENDING"} {
			if strings.Contains(w.Body, bad) {
				t.Errorf("%s sends %s", w, bad)
			}
		}
	}
}

func TestCreateReviewRejectedPositions(t *testing.T) {
	for _, named := range []bool{true, false} {
		p, srv, repo, _ := onServer(t)
		srv.NameRejected = named
		repo.PRs[7].Lines = map[string]map[string][]int{"app/A.kt": {"RIGHT": {1, 2, 3}}}
		err := p.CreateReview(context.Background(), synthKey, writeHead, []base.ReviewComment{
			{Path: "app/A.kt", Side: core.Right, Line: 2, Body: "fine"},
			{Path: "app/A.kt", Side: core.Right, Line: 99, Body: "outside"},
			{Path: "app/A.kt", Side: core.Right, Line: 3, Body: "fine too"},
			{Path: "app/Z.kt", Side: core.Right, Line: 1, Body: "no such file in the diff"},
		})
		got, ok := base.RejectedOf(err)
		if !isKind(err, base.ErrRejected) || !ok {
			t.Fatalf("named %v: error = %v, want ErrRejected", named, err)
		}
		if want := []int{1, 3}; named && !reflect.DeepEqual(got, want) || !named && got != nil {
			t.Errorf("named %v: rejected = %v, want %v", named, got, map[bool][]int{true: want}[named])
		}
		if n := len(repo.PRs[7].Inlines); n != 0 {
			t.Errorf("named %v: %d comments were posted by a refused review", named, n)
		}
		if strings.Contains(err.Error(), "outside") {
			t.Errorf("the error holds the text of a comment: %v", err)
		}
	}
}

func TestCreateReviewOnAPullRequestThatIsNotThere(t *testing.T) {
	p, _, _, _ := onServer(t)
	k := synthKey
	k.Number = 99
	if err := p.CreateReview(context.Background(), k, writeHead, []base.ReviewComment{{Path: "a", Side: core.Right, Line: 1, Body: "x"}}); !isKind(err, base.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestCreateCommentAndUpdateDescription(t *testing.T) {
	p, srv, repo, _ := onServer(t)
	if err := p.CreateComment(context.Background(), synthKey, "Review completed: no blocking issues found!\n\n<!-- acr:v1:ok:01234567 -->"); err != nil {
		t.Fatal(err)
	}
	if err := p.UpdateDescription(context.Background(), synthKey, "## Related Changes\n\n- [x] acme/lib#3\n"); err != nil {
		t.Fatal(err)
	}
	w := srv.Writes()
	if len(w) != 2 || w[0].String() != "POST /repos/acme/app/issues/7/comments" || w[1].String() != "PATCH /repos/acme/app/pulls/7" {
		t.Fatalf("writes = %v", w)
	}
	if got := keysOf(bodyOf(t, w[0])); !reflect.DeepEqual(got, []string{"body"}) {
		t.Errorf("comment keys = %v", got)
	}
	if got := keysOf(bodyOf(t, w[1])); !reflect.DeepEqual(got, []string{"body"}) {
		t.Errorf("description keys = %v: only the description may be written, never the state", got)
	}
	pr := repo.PRs[7]
	if len(pr.Comments) != 1 || pr.Body != "## Related Changes\n\n- [x] acme/lib#3\n" || pr.State != "open" {
		t.Errorf("pull request = %+v", pr)
	}
}

func TestIssues(t *testing.T) {
	p, srv, repo, _ := onServer(t)
	repo.Labels["review"] = true
	n, err := p.CreateIssue(context.Background(), synthKey, base.NewIssue{Title: "Review of #7: MAJOR findings", Body: "b", Label: "review", Assignee: "author-login"})
	if err != nil || n != 8 {
		t.Fatalf("CreateIssue = %d, %v, want 8 (7 is the pull request)", n, err)
	}
	if err := p.UpdateIssue(context.Background(), synthKey, n, "new title", "new body"); err != nil {
		t.Fatal(err)
	}
	w := srv.Writes()
	create, update := bodyOf(t, w[0]), bodyOf(t, w[1])
	if w[0].String() != "POST /repos/acme/app/issues" || !reflect.DeepEqual(keysOf(create), []string{"assignees", "body", "labels", "title"}) ||
		!reflect.DeepEqual(create["labels"], []any{"review"}) || !reflect.DeepEqual(create["assignees"], []any{"author-login"}) {
		t.Errorf("create = %s %v", w[0], create)
	}
	if w[1].String() != "PATCH /repos/acme/app/issues/8" || !reflect.DeepEqual(keysOf(update), []string{"body", "title"}) {
		t.Errorf("update = %s %v: the title and the body only, never the state", w[1], update)
	}
	is := repo.Issues[0]
	if is.Title != "new title" || is.Body != "new body" || !is.Open || !reflect.DeepEqual(is.Labels, []string{"review"}) || !reflect.DeepEqual(is.Assignees, []string{"author-login"}) {
		t.Errorf("issue = %+v", is)
	}
	// Without an assignee the field is not sent.
	if _, err := p.CreateIssue(context.Background(), synthKey, base.NewIssue{Title: "t", Body: "b", Label: "review"}); err != nil {
		t.Fatal(err)
	}
	if _, has := bodyOf(t, srv.Writes()[2])["assignees"]; has {
		t.Error("assignees sent for no assignee")
	}
}

func TestUpdateIssueThatIsNotThere(t *testing.T) {
	p, _, _, _ := onServer(t)
	if err := p.UpdateIssue(context.Background(), synthKey, 99, "t", "b"); !isKind(err, base.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestCreateLabelIsIdempotent(t *testing.T) {
	p, srv, repo, _ := onServer(t)
	label := core.Label{Name: "review", Color: "5319e7", Description: "Findings of a code review"}
	for range 2 {
		if err := p.CreateLabel(context.Background(), synthKey, label); err != nil {
			t.Fatal(err)
		}
	}
	if !repo.Labels["review"] {
		t.Error("the label was not created")
	}
	w := srv.Writes()
	if len(w) != 2 || w[0].String() != "POST /repos/acme/app/labels" || !reflect.DeepEqual(keysOf(bodyOf(t, w[0])), []string{"color", "description", "name"}) {
		t.Errorf("writes = %v", w)
	}
}

func TestCreateLabelFailsOnAnyOtherRefusal(t *testing.T) {
	p, srv, _, _ := onServer(t)
	srv.Fail("POST", `/labels$`, ghserver.Fault{Status: 422, Body: `{"message":"Validation Failed","errors":[{"resource":"Label","code":"invalid","field":"color"}]}`})
	if err := p.CreateLabel(context.Background(), synthKey, core.Label{Name: "review", Color: "5319e7"}); !isKind(err, base.ErrRejected) {
		t.Errorf("error = %v, want ErrRejected", err)
	}
}

func TestWriteFailures(t *testing.T) {
	calls := map[string]func(p *Provider) error{
		"review": func(p *Provider) error {
			return p.CreateReview(context.Background(), synthKey, writeHead, []base.ReviewComment{{Path: "a", Side: core.Right, Line: 1, Body: "x"}})
		},
		"comment": func(p *Provider) error { return p.CreateComment(context.Background(), synthKey, "x") },
		"issue": func(p *Provider) error {
			_, err := p.CreateIssue(context.Background(), synthKey, base.NewIssue{Title: "t", Label: "review"})
			return err
		},
		"update": func(p *Provider) error { return p.UpdateIssue(context.Background(), synthKey, 1, "t", "b") },
		"label": func(p *Provider) error {
			return p.CreateLabel(context.Background(), synthKey, core.Label{Name: "review", Color: "5319e7"})
		},
		"description": func(p *Provider) error { return p.UpdateDescription(context.Background(), synthKey, "d") },
	}
	paths := map[string]string{"review": "/reviews$", "comment": "/comments$", "issue": "/issues$", "update": "/issues/1$", "label": "/labels$", "description": "/pulls/7$"}
	methods := map[string]string{"update": "PATCH", "description": "PATCH"}
	faults := []struct {
		name  string
		fault ghserver.Fault
		want  base.Kind
	}{
		{"not found", ghserver.Fault{Status: 404, Body: `{"message":"Not Found"}`}, base.ErrNotFound},
		{"refused token", ghserver.Fault{Status: 401, Body: `{"message":"Bad credentials"}`}, base.ErrForbidden},
		{"no permission", ghserver.Fault{Status: 403, Body: `{"message":"Resource not accessible by personal access token"}`, Header: map[string]string{"X-Accepted-GitHub-Permissions": "pull_requests=write; issues=write"}}, base.ErrForbidden},
		{"server error", ghserver.Fault{Status: 502, Body: `{}`}, base.ErrTransport},
		{"dropped connection", ghserver.Fault{Drop: true}, base.ErrTransport},
		{"rate limit beyond the wait", ghserver.Fault{Status: 429, Header: map[string]string{"Retry-After": "86400"}}, base.ErrRateLimited},
	}
	for name, call := range calls {
		for _, f := range faults {
			t.Run(name+"/"+f.name, func(t *testing.T) {
				p, srv, repo, _ := onServer(t)
				repo.Labels["review"] = true
				method := methods[name]
				if method == "" {
					method = "POST"
				}
				srv.Fail(method, paths[name], f.fault)
				err := call(p)
				if !isKind(err, f.want) {
					t.Fatalf("error = %v, want %v", err, f.want)
				}
				if strings.Contains(err.Error(), canary) {
					t.Errorf("the error holds the token: %v", err)
				}
				if f.want == base.ErrTransport || f.want == base.ErrForbidden || f.want == base.ErrNotFound {
					if n := len(srv.Writes()); n != 1 {
						t.Errorf("%d write requests, want 1: a write whose outcome is unknown is not repeated", n)
					}
				}
				if f.name == "no permission" {
					if got := base.PermissionsOf(err); !reflect.DeepEqual(got, []string{"pull_requests: write", "issues: write"}) {
						t.Errorf("permissions = %v", got)
					}
				}
			})
		}
	}
}

func TestWritesToAnotherHostNeverSendAnything(t *testing.T) {
	p, srv, _, tok := onServer(t)
	k := synthKey
	k.Host = "ghe.example.com"
	for _, err := range []error{
		p.CreateReview(context.Background(), k, writeHead, []base.ReviewComment{{Path: "a", Side: core.Right, Line: 1, Body: "x"}}),
		p.CreateComment(context.Background(), k, "x"),
		p.UpdateIssue(context.Background(), k, 1, "t", "b"),
		p.CreateLabel(context.Background(), k, core.Label{Name: "a", Color: "000000"}),
		p.UpdateDescription(context.Background(), k, "d"),
	} {
		if !isKind(err, base.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	}
	if _, err := p.CreateIssue(context.Background(), k, base.NewIssue{Title: "t"}); !isKind(err, base.ErrNotFound) {
		t.Errorf("CreateIssue error = %v", err)
	}
	if len(srv.Requests()) != 0 || tok.asked.Load() != 0 {
		t.Errorf("requests %v, token asked %d times, want neither", srv.Requests(), tok.asked.Load())
	}
}

func TestNoTokenIsAnError(t *testing.T) {
	p, srv, _, tok := onServer(t)
	tok.err = errors.New("no token anywhere")
	if err := p.CreateComment(context.Background(), synthKey, "x"); err == nil {
		t.Error("a comment was sent without a token")
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("requests = %v", srv.Requests())
	}
}

// TestTheProviderCanOnlyDoWhatWriterSays: whatever a bug asks the
// transport to send, only the endpoints of base.Writer and the GraphQL
// query go out.
func TestTheProviderCanOnlyDoWhatWriterSays(t *testing.T) {
	p, srv, _, _ := onServer(t)
	for _, call := range []struct{ method, path string }{
		{"POST", "/repos/acme/app/pulls/7/merge"}, {"PUT", "/repos/acme/app/pulls/7/merge"},
		{"POST", "/repos/acme/app/pulls/7/reviews/1/events"}, {"POST", "/repos/acme/app/issues/7/lock"},
		{"PATCH", "/repos/acme/app"}, {"POST", "/repos/acme/app/git/refs"},
	} {
		var err error
		if call.method == "POST" {
			_, err = p.post(context.Background(), call.path, []byte(`{}`))
		} else {
			_, err = p.patch(context.Background(), call.path, []byte(`{}`))
		}
		if err == nil {
			t.Errorf("%s %s was sent", call.method, call.path)
		}
	}
	if len(srv.Requests()) != 0 {
		t.Errorf("requests = %v, want none", srv.Requests())
	}
	if _, err := p.post(context.Background(), "/graphql", []byte(`{"query":"mutation { x }"}`)); err == nil {
		t.Error("a mutation was sent")
	}
	if srv.SawMutation() {
		t.Error("the server saw a mutation")
	}
}

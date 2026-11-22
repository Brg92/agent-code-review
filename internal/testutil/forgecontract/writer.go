package forgecontract

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// WriterFixture is what a provider must do with its writes: the change
// request and project they go to, and the places where they must fail.
type WriterFixture struct {
	// ChangeRequest is open and Head its head commit; Project is its
	// project, which has the label Label, and Login is the account of the
	// token.
	ChangeRequest core.Key
	Head          string
	Project       core.Key
	Label         string
	Login         string
	// Refused is the position of a comment the platform refuses, as the
	// platform sees the diff.
	Refused base.ReviewComment
	// Missing is a change request that does not exist, in a project that
	// does; NoProject is a project that does not exist.
	Missing, NoProject core.Key
	// Forbidden is a project the token cannot write to, and Busy one whose
	// writes are refused for the rate limit; Broken one whose writes fail
	// on the platform's side.
	Forbidden, Busy, Broken core.Key
}

// RunWriter checks that the writes of p do what they say, as the reads of
// p see it afterwards, and that they fail with their kind. It reports
// through t.Errorf only. It must run on a provider whose platform is in
// the state wf describes, and writes to it.
func RunWriter(t testing.TB, p base.ForgeProvider, wf WriterFixture) {
	t.Helper()
	ctx := context.Background()
	notes := func(cr core.Key) []core.ReviewNote {
		n, err := p.Notes(ctx, cr)
		if err != nil {
			t.Errorf("Notes(%s) = %v", cr, err)
		}
		return n
	}
	has := func(cr core.Key, body string) (core.ReviewNote, bool) {
		for _, n := range notes(cr) {
			if n.Body == body {
				return n, true
			}
		}
		return core.ReviewNote{}, false
	}

	// A label, twice: a label that exists is not an error.
	for range 2 {
		if err := p.CreateLabel(ctx, wf.Project, core.Label{Name: wf.Label, Color: "5319e7", Description: "Findings of a code review"}); err != nil {
			t.Errorf("CreateLabel = %v", err)
		}
	}
	if ok, err := p.HasLabel(ctx, wf.Project, wf.Label); err != nil || !ok {
		t.Errorf("HasLabel(%s) after CreateLabel = %v, %v", wf.Label, ok, err)
	}

	// A comment, then a review of two comments, which the reads see as the
	// account of the token, trusted.
	comment := "Review completed: no blocking issues found!\n\n" + core.Embed(core.OKFingerprint(wf.Head))
	if err := p.CreateComment(ctx, wf.ChangeRequest, comment); err != nil {
		t.Errorf("CreateComment = %v", err)
	}
	if n, ok := has(wf.ChangeRequest, comment); !ok || n.Author != wf.Login || !n.Trusted {
		t.Errorf("the comment reads back as %+v, %v, want one by %s that is trusted", n, ok, wf.Login)
	}
	first := base.ReviewComment{Path: wf.Refused.Path, Side: wf.Refused.Side, Line: 1, Body: "first inline comment"}
	second := base.ReviewComment{Path: wf.Refused.Path, Side: wf.Refused.Side, Line: 2, Body: "second inline comment"}
	if err := p.CreateReview(ctx, wf.ChangeRequest, wf.Head, []base.ReviewComment{first, second}); err != nil {
		t.Errorf("CreateReview = %v", err)
	}
	for _, c := range []base.ReviewComment{first, second} {
		if n, ok := has(wf.ChangeRequest, c.Body); !ok || n.Author != wf.Login || !n.Trusted {
			t.Errorf("the inline comment %q reads back as %+v, %v", c.Body, n, ok)
		}
	}
	threads, err := p.ReviewThreads(ctx, wf.ChangeRequest)
	if err != nil || len(threads) < 2 || threads[len(threads)-2].Body != first.Body || threads[len(threads)-1].Resolved {
		t.Errorf("ReviewThreads after CreateReview = %+v, %v, want the two comments as open threads", threads, err)
	}

	// A review with a comment the platform refuses posts nothing.
	before := len(notes(wf.ChangeRequest))
	bad := wf.Refused
	bad.Body = "refused inline comment"
	err = p.CreateReview(ctx, wf.ChangeRequest, wf.Head, []base.ReviewComment{{Path: first.Path, Side: first.Side, Line: 1, Body: "innocent bystander"}, bad})
	if !errors.Is(err, base.ErrRejected) {
		t.Errorf("CreateReview with a refused position = %v, want ErrRejected", err)
	}
	if idx, ok := base.RejectedOf(err); !ok || (idx != nil && !slices.Equal(idx, []int{1})) {
		t.Errorf("RejectedOf = %v, %v, want nothing or [1]", idx, ok)
	}
	if after := len(notes(wf.ChangeRequest)); after != before {
		t.Errorf("a refused review posted %d comments", after-before)
	}

	// An issue, rewritten.
	number, err := p.CreateIssue(ctx, wf.Project, base.NewIssue{Title: "Review of #7: MAJOR findings", Body: "first body", Label: wf.Label})
	if err != nil || number < 1 {
		t.Fatalf("CreateIssue = %d, %v", number, err)
	}
	find := func() (core.Issue, bool) {
		issues, err := p.Issues(ctx, wf.Project, wf.Label)
		if err != nil {
			t.Errorf("Issues = %v", err)
		}
		for _, is := range issues {
			if is.Number == number {
				return is, true
			}
		}
		return core.Issue{}, false
	}
	if is, ok := find(); !ok || !is.Open || is.Author != wf.Login || !is.Trusted || is.Title != "Review of #7: MAJOR findings" || is.Body != "first body" {
		t.Errorf("the issue reads back as %+v, %v", is, ok)
	}
	if err := p.UpdateIssue(ctx, wf.Project, number, "Review of #7: MAJOR findings", "second body"); err != nil {
		t.Errorf("UpdateIssue = %v", err)
	}
	if is, ok := find(); !ok || !is.Open || is.Body != "second body" {
		t.Errorf("the issue after UpdateIssue reads back as %+v, %v, want it open with the new body", is, ok)
	}

	// The description, and nothing else of the change request.
	meta, err := p.ChangeRequest(ctx, wf.ChangeRequest)
	if err != nil {
		t.Fatalf("ChangeRequest = %v", err)
	}
	if err := p.UpdateDescription(ctx, wf.ChangeRequest, "## Related Changes\n\n- [x] acme/lib#3\n"); err != nil {
		t.Errorf("UpdateDescription = %v", err)
	}
	after, err := p.ChangeRequest(ctx, wf.ChangeRequest)
	want := meta
	want.Description = "## Related Changes\n\n- [x] acme/lib#3\n"
	if err != nil || after != want {
		t.Errorf("the change request after UpdateDescription = %+v, %v, want only its description changed: %+v", after, err, want)
	}

	// Failures.
	one := []base.ReviewComment{first}
	for _, c := range []struct {
		name string
		err  error
		want base.Kind
	}{
		{"CreateComment on a missing change request", p.CreateComment(ctx, wf.Missing, "x"), base.ErrNotFound},
		{"CreateReview on a missing change request", p.CreateReview(ctx, wf.Missing, wf.Head, one), base.ErrNotFound},
		{"UpdateDescription of a missing change request", p.UpdateDescription(ctx, wf.Missing, "x"), base.ErrNotFound},
		{"UpdateIssue of a missing issue", p.UpdateIssue(ctx, wf.Project, 9999, "t", "b"), base.ErrNotFound},
		{"CreateLabel in a missing project", p.CreateLabel(ctx, wf.NoProject, core.Label{Name: "a", Color: "000000"}), base.ErrNotFound},
		{"CreateComment without permission", p.CreateComment(ctx, wf.Forbidden, "x"), base.ErrForbidden},
		{"CreateComment on a busy platform", p.CreateComment(ctx, wf.Busy, "x"), base.ErrRateLimited},
		{"CreateComment on a broken platform", p.CreateComment(ctx, wf.Broken, "x"), base.ErrTransport},
	} {
		if !errors.Is(c.err, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.err, c.want)
		}
	}
	if _, err := p.CreateIssue(ctx, wf.NoProject, base.NewIssue{Title: "t", Label: wf.Label}); !errors.Is(err, base.ErrNotFound) {
		t.Errorf("CreateIssue in a missing project = %v, want ErrNotFound", err)
	}
	if n, err := p.CreateIssue(ctx, wf.Forbidden, base.NewIssue{Title: "t", Label: wf.Label}); !errors.Is(err, base.ErrForbidden) || n != 0 {
		t.Errorf("CreateIssue without permission = %d, %v, want ErrForbidden and no issue", n, err)
	}
}

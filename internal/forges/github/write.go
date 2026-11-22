package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/transport"
)

var (
	_ base.ForgeProvider = (*Provider)(nil)

	// commentIndex finds the comment a refusal is about, if it says.
	commentIndex = regexp.MustCompile(`comments?\[(\d+)\]`)
)

// reviewEvent is the only event of a review the provider ever sends: the
// review comments and does nothing else.
const reviewEvent = "COMMENT"

type reviewCommentJSON struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
}

// CreateReview implements base.Writer: one request that carries every
// comment, with the event COMMENT and no text of its own.
func (p *Provider) CreateReview(ctx context.Context, cr core.Key, head string, comments []base.ReviewComment) error {
	op := "review " + cr.String()
	if err := p.checkHost(op, cr); err != nil {
		return err
	}
	in := struct {
		CommitID string              `json:"commit_id"`
		Event    string              `json:"event"`
		Comments []reviewCommentJSON `json:"comments"`
	}{CommitID: head, Event: reviewEvent, Comments: []reviewCommentJSON{}}
	for _, c := range comments {
		jc := reviewCommentJSON{Path: c.Path, Body: c.Body, Line: c.Line, Side: string(c.Side)}
		if c.StartLine > 0 {
			jc.StartLine, jc.StartSide = c.StartLine, string(c.Side)
		}
		in.Comments = append(in.Comments, jc)
	}
	return p.send(ctx, op, repoPath(cr, "pulls", strconv.Itoa(cr.Number), "reviews"), in, nil)
}

// CreateComment implements base.Writer.
func (p *Provider) CreateComment(ctx context.Context, cr core.Key, body string) error {
	op := "comment " + cr.String()
	if err := p.checkHost(op, cr); err != nil {
		return err
	}
	return p.send(ctx, op, repoPath(cr, "issues", strconv.Itoa(cr.Number), "comments"), map[string]string{"body": body}, nil)
}

// CreateIssue implements base.Writer.
func (p *Provider) CreateIssue(ctx context.Context, project core.Key, issue base.NewIssue) (int, error) {
	op := "issue " + project.ProjectKey()
	if err := p.checkHost(op, project); err != nil {
		return 0, err
	}
	in := struct {
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Labels    []string `json:"labels"`
		Assignees []string `json:"assignees,omitempty"`
	}{Title: issue.Title, Body: issue.Body, Labels: []string{issue.Label}}
	if issue.Assignee != "" {
		in.Assignees = []string{issue.Assignee}
	}
	var out struct {
		Number int `json:"number"`
	}
	if err := p.send(ctx, op, repoPath(project, "issues"), in, &out); err != nil {
		return 0, err
	}
	if out.Number < 1 {
		return 0, &base.Error{Kind: base.ErrTransport, Op: op, Err: errors.New("the answer has no issue number")}
	}
	return out.Number, nil
}

// UpdateIssue implements base.Writer: the title and the body, nothing
// else.
func (p *Provider) UpdateIssue(ctx context.Context, project core.Key, number int, title, body string) error {
	op := fmt.Sprintf("issue %s#%d", project.ProjectKey(), number)
	if err := p.checkHost(op, project); err != nil {
		return err
	}
	in := struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}{title, body}
	return p.sendPatch(ctx, op, repoPath(project, "issues", strconv.Itoa(number)), in)
}

// CreateLabel implements base.Writer. A label that exists is not an error.
func (p *Provider) CreateLabel(ctx context.Context, project core.Key, label core.Label) error {
	op := "label " + label.Name + " of " + project.ProjectKey()
	if err := p.checkHost(op, project); err != nil {
		return err
	}
	in := struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	}{label.Name, label.Color, label.Description}
	body, err := json.Marshal(in)
	if err != nil {
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: err}
	}
	if _, err := p.post(ctx, repoPath(project, "labels"), body); err != nil {
		if labelExists(err) {
			return nil
		}
		return p.failWrite(op, err)
	}
	return nil
}

// labelExists reports whether GitHub refused a label because it is there.
func labelExists(err error) bool {
	var se *transport.StatusError
	if !errors.As(err, &se) || se.StatusCode != 422 {
		return false
	}
	var answer struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(se.Body, &answer) != nil {
		return false
	}
	for _, e := range answer.Errors {
		if e.Code == "already_exists" {
			return true
		}
	}
	return false
}

// UpdateDescription implements base.Writer: the description, never the
// state of the pull request.
func (p *Provider) UpdateDescription(ctx context.Context, cr core.Key, description string) error {
	op := "description " + cr.String()
	if err := p.checkHost(op, cr); err != nil {
		return err
	}
	return p.sendPatch(ctx, op, repoPath(cr, "pulls", strconv.Itoa(cr.Number)), map[string]string{"body": description})
}

// send posts in as JSON and, if out is not nil, reads the answer into it.
func (p *Provider) send(ctx context.Context, op, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: err}
	}
	resp, err := p.post(ctx, path, body)
	if err != nil {
		return p.failWrite(op, err)
	}
	if out == nil {
		return nil
	}
	return decode(op, resp.Body, out)
}

func (p *Provider) sendPatch(ctx context.Context, op, path string, in any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return &base.Error{Kind: base.ErrTransport, Op: op, Err: err}
	}
	if _, err := p.patch(ctx, path, body); err != nil {
		return p.failWrite(op, err)
	}
	return nil
}

// failWrite turns an error of a write into one of base's kinds. A 422 is
// ErrRejected; when its answer names the comments of a review it refused,
// by their positions, they are in Rejected.
func (p *Provider) failWrite(op string, err error) error {
	var se *transport.StatusError
	if !errors.As(err, &se) || se.StatusCode != 422 {
		return p.fail(op, err)
	}
	e := &base.Error{Kind: base.ErrRejected, Op: op, Err: &statusCause{Code: 422, Message: se.Message}}
	var answer struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(se.Body, &answer) == nil {
		seen := map[int]bool{}
		for _, raw := range answer.Errors {
			for _, m := range commentIndex.FindAllStringSubmatch(string(raw), -1) {
				if i, err := strconv.Atoi(m[1]); err == nil && !seen[i] {
					seen[i] = true
					e.Rejected = append(e.Rejected, i)
				}
			}
		}
	}
	return e
}

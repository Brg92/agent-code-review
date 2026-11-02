package ghrecord

import (
	"reflect"
	"testing"
)

var target = Target{Owner: "o", Repo: "core", Number: 3}

func TestTargetRequests(t *testing.T) {
	tests := []struct {
		name string
		got  Request
		want Request
	}{
		{"pull request", PullRequestRequest(target), Request{Path: "/repos/o/core/pulls/3"}},
		{"issue", IssueRequest(target), Request{Path: "/repos/o/core/issues/3"}},
		{"files", FilesRequest(target), Request{Path: "/repos/o/core/pulls/3/files", Query: map[string]string{"per_page": "100"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestTargetString(t *testing.T) {
	if got := target.String(); got != "o/core#3" {
		t.Errorf("String() = %q, want %q", got, "o/core#3")
	}
}

func patch(s string) *string { return &s }

func TestFollowUps(t *testing.T) {
	pr := PullRequest{
		Head: Ref{Ref: "feat/x", SHA: "headsha"},
		Base: Ref{Ref: "main", SHA: "basesha"},
	}
	files := []PullRequestFile{
		{Filename: "src/Added.kt", Status: "added", Patch: patch("@@")},
		{Filename: "src/Removed.kt", Status: "removed", Patch: patch("@@")},
		{Filename: "src/Changed.kt", Status: "modified", Patch: patch("@@")},
		{Filename: "src/new/Moved.kt", PreviousFilename: "src/Moved.kt", Status: "renamed", Patch: patch("@@")},
		{Filename: "src/new/Same.kt", PreviousFilename: "src/Same.kt", Status: "renamed"},
		{Filename: "res/icon.png", Status: "added"},
		{Filename: "gen/Huge.kt", Status: "modified"},
	}
	page := map[string]string{"per_page": "100"}
	atBase := func(p string) Request {
		return Request{Path: "/repos/o/core/contents/" + p, Query: map[string]string{"ref": "main"}}
	}
	atSHA := func(p, sha string) Request {
		return Request{Path: "/repos/o/core/contents/" + p, Query: map[string]string{"ref": sha}}
	}
	want := []Request{
		{Path: "/repos/o/core/pulls/3/commits", Query: page},
		{Path: "/repos/o/core/pulls/3/reviews", Query: page},
		{Path: "/repos/o/core/pulls/3/comments", Query: page},
		{Path: "/repos/o/core/issues/3/comments", Query: page},
		{Path: "/repos/o/core/commits/headsha/check-runs", Query: page},
		{Path: "/repos/o/core/commits/headsha/status"},
		atBase(".agent-code-review/config.json"),
		atBase(".agent-code-review/criteria.md"),
		atBase(".github/mr-review.json"),
		atBase(".github/skills"),
		atBase(".github/skills/core-code-review/SKILL.md"),
		atSHA("src/Added.kt", "headsha"),
		atSHA("src/Removed.kt", "basesha"),
		atSHA("src/Changed.kt", "headsha"),
		atSHA("src/Changed.kt", "basesha"),
		atSHA("src/new/Moved.kt", "headsha"),
		atSHA("src/Moved.kt", "basesha"),
	}
	if got := FollowUps(target, pr, files); !reflect.DeepEqual(got, want) {
		t.Errorf("FollowUps =\n%+v\nwant\n%+v", got, want)
	}
}

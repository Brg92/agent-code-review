package github

import (
	"context"
	"testing"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func TestChangesGet(t *testing.T) {
	ctx := context.Background()
	pull := sandbox.PullRequest(t, "S2", "core-lib", 3)
	tests := []struct {
		name string
		key  core.Key
		want cascade.Kind
	}{
		{"change request", key("core-lib", 3), cascade.KindChangeRequest},
		{"issue", key("sample-app", 1), cascade.KindIssue},
		{"nothing", key("core-lib", 9999), cascade.KindNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, replay, _ := onReplay(t, "S2")
			item, err := p.Changes().Get(ctx, tt.key)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if item.Kind != tt.want {
				t.Errorf("Kind = %v, want %v", item.Kind, tt.want)
			}
			if tt.want == cascade.KindChangeRequest {
				if item.Head != pull.HeadSHA || item.Title == "" || item.Description == "" {
					t.Errorf("Item = %+v, want the recorded head %s, a title and the description", item, pull.HeadSHA)
				}
				if n := len(replay.Requests()); n != 1 {
					t.Errorf("%d requests, want only the pull request: %v", n, replay.Requests())
				}
			} else if item.Title != "" || item.Head != "" || item.Description != "" {
				t.Errorf("Item = %+v, want nothing but the kind", item)
			}
		})
	}
}

func TestChangesGetLooksForAnIssueOnlyAfterA404(t *testing.T) {
	p, replay, _ := onReplay(t, "S2")
	if _, err := p.Changes().Get(context.Background(), key("core-lib", 9999)); err != nil {
		t.Fatal(err)
	}
	repo := "/repos/" + sandbox.Owner + "/core-lib"
	if replay.Count("GET", repo+"/pulls/9999") != 1 || replay.Count("GET", repo+"/issues/9999") != 1 || len(replay.Requests()) != 2 {
		t.Errorf("requests = %v, want the pull request and then the issue", replay.Requests())
	}
}

func TestChangeRequest(t *testing.T) {
	p, _, _ := onReplay(t, "S1")
	pull := sandbox.PullRequest(t, "S1", "sample-app", 2)
	cr, err := p.ChangeRequest(context.Background(), key("sample-app", 2))
	if err != nil {
		t.Fatal(err)
	}
	want := core.ChangeRequest{
		Key: key("sample-app", 2), Title: "Prefetch the session on startup",
		Description: "Refreshes the session before the first frame and prefetches the signed-in user.",
		HeadSHA:     pull.HeadSHA, BaseSHA: pull.BaseSHA, TargetBranch: pull.BaseRef, Author: pull.Author, State: core.StateOpen,
	}
	if cr != want {
		t.Errorf("ChangeRequest() = %+v, want %+v", cr, want)
	}
	if err := cr.Validate(); err != nil {
		t.Error(err)
	}
}

func TestChangeRequestNotFound(t *testing.T) {
	p, _, _ := onReplay(t, "S2")
	for _, k := range []core.Key{key("core-lib", 9999)} {
		cr, err := p.ChangeRequest(context.Background(), k)
		if !isKind(err, base.ErrNotFound) || cr != (core.ChangeRequest{}) {
			t.Errorf("ChangeRequest(%s) = %+v, %v, want ErrNotFound", k, cr, err)
		}
	}
}

func TestChangedFilesOnTheRecordedScenarios(t *testing.T) {
	tests := []struct {
		scenario, repo string
		number         int
	}{
		{"S1", "sample-app", 2}, {"S3", "core-lib", 1}, {"S4", "feature-lib", 1}, {"S5", "sample-app", 3},
	}
	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			p, _, _ := onReplay(t, tt.scenario)
			got, err := p.ChangedFiles(context.Background(), key(tt.repo, tt.number))
			if err != nil {
				t.Fatal(err)
			}
			want := sandbox.PullFiles(t, tt.scenario, tt.repo, tt.number)
			if len(got) != len(want) {
				t.Fatalf("%d files, want %d", len(got), len(want))
			}
			for i, f := range want {
				g := got[i]
				patch := ""
				if f.Patch != nil {
					patch = *f.Patch
				}
				if g.Path != f.Filename || g.OldPath != f.PreviousFilename || string(g.Status) != f.Status || g.Patch != patch {
					t.Errorf("file %d = %+v, want %s %s %s", i, g, f.Filename, f.PreviousFilename, f.Status)
				}
				if _, err := g.Diff(); err != nil {
					t.Errorf("%s: Diff() = %v", g.Path, err)
				}
			}
		})
	}
}

func TestChangedFilesWithoutPatches(t *testing.T) {
	tests := []struct {
		scenario, repo string
		number         int
		path           string
		want           core.FileChange
	}{
		{"S3", "core-lib", 1, "src/main/res/drawable-nodpi/badge_background.png",
			core.FileChange{Status: core.FileAdded, Missing: core.PatchOmitted}},
		{"S3", "core-lib", 1, "src/main/java/com/example/core/ui/badge/StatusBadgeView.kt",
			core.FileChange{OldPath: "src/main/java/com/example/core/ui/StatusBadgeView.kt", Status: core.FileRenamed}},
		{"S4", "feature-lib", 1, "src/main/java/com/example/feature/catalog/IconCatalog.kt",
			core.FileChange{Status: core.FileAdded, Missing: core.PatchTooLarge}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			p, _, _ := onReplay(t, tt.scenario)
			got, err := p.ChangedFiles(context.Background(), key(tt.repo, tt.number))
			if err != nil {
				t.Fatal(err)
			}
			tt.want.Path = tt.path
			for _, g := range got {
				if g.Path == tt.path {
					if g != tt.want {
						t.Errorf("file = %+v, want %+v", g, tt.want)
					}
					return
				}
			}
			t.Errorf("%s not listed", tt.path)
		})
	}
}

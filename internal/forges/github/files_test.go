package github

import (
	"context"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func TestFilesGetRecorded(t *testing.T) {
	tests := []struct {
		repo, path, ref string
	}{
		{"core-lib", ".github/mr-review.json", "main"},
		{"core-lib", ".github/skills/core-lib-code-review/SKILL.md", "main"},
		{"sample-app", ".agent-code-review/config.json", "main"},
		{"sample-app", ".agent-code-review/criteria.md", "chore/criteria-s6-invalid-overlay-base"},
		{"kmp-shared", "shared/src/commonMain/kotlin/com/example/shared/Greeting.kt", "915c4dd07f7847183fc8b976497a755b48df03a9"},
	}
	p, _, _ := onReplay(t, "S6")
	for _, tt := range tests {
		t.Run(tt.repo+"/"+tt.path, func(t *testing.T) {
			want := sandbox.Contents(t, "S6", tt.repo, tt.path, tt.ref)
			if !want.Recorded || want.Status != 200 || want.File == nil {
				t.Fatalf("recording: %+v", want)
			}
			got, found, err := p.Files().Get(context.Background(), key(tt.repo, 1), tt.ref, tt.path)
			if err != nil || !found || string(got) != string(want.File) {
				t.Errorf("Get() = %q, %v, %v, want %q", got, found, err, want.File)
			}
		})
	}
}

func TestFilesGetMissingFileOfAReadableRepository(t *testing.T) {
	p, replay, _ := onReplay(t, "S6")
	ctx := context.Background()
	// Two missing files of one repository: the repository is read once.
	for _, path := range []string{".agent-code-review/config.json", ".agent-code-review/criteria.md"} {
		got, found, err := p.Files().Get(ctx, key("core-lib", 1), "main", path)
		if err != nil || found || got != nil {
			t.Errorf("Get(%s) = %q, %v, %v, want not found and no error", path, got, found, err)
		}
	}
	repo := "/repos/" + sandbox.Owner + "/core-lib"
	if n := replay.Count("GET", repo); n != 1 {
		t.Errorf("the repository was read %d times, want once: %v", n, replay.Requests())
	}
	// A file that exists does not need the repository.
	other, _, _ := onReplay(t, "S6")
	if _, found, err := other.Files().Get(ctx, key("core-lib", 1), "main", ".github/mr-review.json"); err != nil || !found {
		t.Fatalf("Get = %v, %v", found, err)
	}
}

func TestFilesList(t *testing.T) {
	p, _, _ := onReplay(t, "S6")
	ctx := context.Background()
	want := sandbox.Contents(t, "S6", "core-lib", ".github/skills", "main")
	got, err := p.Files().List(ctx, key("core-lib", 1), "main", ".github/skills")
	var entries []core.DirEntry
	for _, e := range want.Dir {
		entries = append(entries, core.DirEntry{Name: e.Name, Dir: e.Dir})
	}
	if err != nil || len(entries) == 0 || !reflect.DeepEqual(got, entries) {
		t.Errorf("List() = %+v, %v, want %+v", got, err, entries)
	}
	got, err = p.Files().List(ctx, key("sample-app", 1), "main", ".github/skills")
	if err != nil || len(got) != 0 {
		t.Errorf("List() of a missing directory = %+v, %v, want no entries and no error", got, err)
	}
}

func TestTreeRecorded(t *testing.T) {
	tests := []struct {
		scenario, repo string
		number         int
	}{
		{"S1", "sample-app", 2}, {"S2", "core-lib", 3}, {"S5", "sample-app", 3}, {"S7", "kmp-shared", 3}, {"S7", "sample-app", 8},
	}
	for _, tt := range tests {
		t.Run(tt.scenario+"/"+tt.repo, func(t *testing.T) {
			p, _, _ := onReplay(t, tt.scenario)
			head := sandbox.PullRequest(t, tt.scenario, tt.repo, tt.number).HeadSHA
			want := sandbox.TreeOf(t, tt.scenario, tt.repo, head)
			if !want.Recorded || want.Truncated || len(want.Paths) == 0 {
				t.Fatalf("recording: %+v", want)
			}
			tree := p.Tree(context.Background(), key(tt.repo, tt.number), head)
			all, err := tree.List("")
			if err != nil || !reflect.DeepEqual(all, sortedCopy(want.Paths)) {
				t.Errorf("List(\"\") = %v, %v, want %v", all, err, want.Paths)
			}
			prefix := want.Paths[len(want.Paths)-1][:1]
			sub, err := tree.List(prefix)
			if err != nil || len(sub) == 0 {
				t.Errorf("List(%q) = %v, %v", prefix, sub, err)
			}
			for _, s := range sub {
				if s[:1] != prefix {
					t.Errorf("List(%q) returns %s", prefix, s)
				}
			}
			none, err := tree.List("no/such/dir/")
			if err != nil || none == nil || len(none) != 0 {
				t.Errorf("List(no prefix match) = %#v, %v, want an empty list", none, err)
			}
		})
	}
}

func TestTreeIsReadOnceAndLazily(t *testing.T) {
	p, replay, _ := onReplay(t, "S1")
	head := sandbox.PullRequest(t, "S1", "sample-app", 2).HeadSHA
	tree := p.Tree(context.Background(), key("sample-app", 2), head)
	if n := len(replay.Requests()); n != 0 {
		t.Fatalf("%d requests before the first List", n)
	}
	for _, prefix := range []string{"", "app/", ".github/"} {
		if _, err := tree.List(prefix); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(replay.Requests()); n != 1 {
		t.Errorf("%d requests for three Lists, want 1: %v", n, replay.Requests())
	}
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

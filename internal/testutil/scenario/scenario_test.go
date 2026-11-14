package scenario

import (
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

func TestReview(t *testing.T) {
	r := Review(t, "core-lib", 1, "S2", "S3")
	if r.CR.String() != "github:github.com/acr-plugin-sandbox/core-lib#1" || r.HeadSHA != "1bb2c03af22c5360e04698de42d0833c0387b499" || len(r.Files) != 6 {
		t.Fatalf("Review = %s %s, %d files", r.CR, r.HeadSHA, len(r.Files))
	}
	for _, f := range r.Files {
		switch f.Diff.Path {
		case "src/main/java/com/example/core/session/codec/TokenCodec.kt":
			// The base version of a renamed file is read at its old path.
			if len(f.Head) == 0 || len(f.Base) == 0 || f.Base[0] != "package com.example.core.session" {
				t.Errorf("TokenCodec.kt: head %d lines, base %q", len(f.Head), f.Base)
			}
		case "src/main/java/com/example/core/session/SessionExpiry.kt":
			if f.Base != nil || len(f.Head) != 11 {
				t.Errorf("SessionExpiry.kt: base %q, %d head lines", f.Base, len(f.Head))
			}
		}
	}
	for _, id := range []string{"uses-removed-api", "logic-defect", "no-double-bang"} {
		if _, ok := r.Rules[id]; !ok {
			t.Errorf("rule %s missing", id)
		}
	}
}

func TestJudgement(t *testing.T) {
	j := Judgement(t, "S3", "core-lib", 1)
	if j.CR != "github:github.com/acr-plugin-sandbox/core-lib#1" || len(j.Findings) != 2 {
		t.Errorf("Judgement = %+v", j)
	}
}

func TestRecordedTree(t *testing.T) {
	head := sandbox.PullRequest(t, "S7", "sample-app", 8).HeadSHA
	tree := RecordedTree(t, "S7", "sample-app", head)
	got, err := tree.List("app/src/test/")
	if err != nil || len(got) != 2 {
		t.Errorf("List(app/src/test/) = %v, %v; want the two tests of the tree", got, err)
	}
	if got, err := tree.List("nothing/"); err != nil || len(got) != 0 {
		t.Errorf("List(nothing/) = %v, %v", got, err)
	}
	if _, err := RecordedTreeOrError("S7", sandbox.Tree{}).List("app/"); err == nil {
		t.Error("a tree that was not recorded listed files")
	}
	if _, err := RecordedTreeOrError("S7", sandbox.Tree{Recorded: true, Truncated: true, Paths: []string{"a"}}).List("a"); err == nil {
		t.Error("a truncated tree listed files, which would pass a missing file for a missing test")
	}
}

func TestRecordedSearch(t *testing.T) {
	search := RecordedSearch(t, "S2")
	project := "github:github.com/acr-plugin-sandbox/sample-app"
	// GitHub returned no items and incomplete_results for the sandbox: the
	// search did not run, which a detector must not read as "no use".
	if got, err := search.Search(project, "main", "legacyToken"); err == nil {
		t.Errorf("an incomplete search found %v without an error", got)
	}
	if _, err := search.Search(project, "main", "neverSearched"); err == nil {
		t.Error("a search that was not recorded succeeded")
	}
	if _, err := search.Search(project, "feat/other", "legacyToken"); err == nil {
		t.Error("a search of a branch that is not the default one succeeded: code search only indexes the default branch")
	}
}

func TestMatchesIn(t *testing.T) {
	got := matchesIn("a/B.kt", "class B {\n    fun term()\n    // term\n}\n", "term")
	want := []rules.Match{{Path: "a/B.kt", Line: 2, Text: "    fun term()"}, {Path: "a/B.kt", Line: 3, Text: "    // term"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("matchesIn = %+v, want %+v", got, want)
	}
}

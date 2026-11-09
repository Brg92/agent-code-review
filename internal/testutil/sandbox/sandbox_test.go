package sandbox

import (
	"strings"
	"testing"
)

func TestPullFiles(t *testing.T) {
	files := PullFiles(t, "S3", "core-lib", 1)
	if len(files) != 6 {
		t.Fatalf("%d files, want 6", len(files))
	}
	var rename *File
	for i := range files {
		if files[i].Filename == "src/main/java/com/example/core/session/codec/TokenCodec.kt" {
			rename = &files[i]
		}
	}
	if rename == nil || rename.Status != "renamed" || rename.PreviousFilename != "src/main/java/com/example/core/session/TokenCodec.kt" ||
		rename.Patch == nil || rename.Additions != 1 || rename.Deletions != 2 {
		t.Errorf("TokenCodec.kt = %+v", rename)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Filename, ".png") && f.Patch != nil {
			t.Errorf("%s has a patch", f.Filename)
		}
	}
}

func TestAllPullFiles(t *testing.T) {
	lists := AllPullFiles(t)
	if len(lists) != 13 {
		t.Errorf("%d recorded file lists, want 13", len(lists))
	}
	for _, l := range lists {
		if l.Scenario == "" || l.Repo == "" || l.Number < 1 || len(l.Files) == 0 {
			t.Errorf("incomplete list %+v", l)
		}
	}
}

func TestContent(t *testing.T) {
	src, ok := Content(t, "S1", "sample-app", "app/src/main/java/com/example/sample/ui/HomeViewModel.kt", "76ae5f13ca1575b65fe1567c471e8a7b51a00af4")
	if !ok || !strings.HasPrefix(src, "package com.example.sample.ui\n") {
		t.Errorf("Content = %q, %v", src[:min(len(src), 40)], ok)
	}
	if _, ok := Content(t, "S1", "sample-app", "README.md", "main"); ok {
		t.Error("a file that was not recorded was found")
	}
}

func TestPullRequest(t *testing.T) {
	p := PullRequest(t, "S1", "sample-app", 2)
	if p.HeadSHA != "76ae5f13ca1575b65fe1567c471e8a7b51a00af4" || p.BaseSHA != "fdd842c45ac32e215b8c8bbf97ecf19e31fee79f" || p.Author != "sandbox-author" {
		t.Errorf("PullRequest = %+v", p)
	}
}

func TestContents(t *testing.T) {
	file := Contents(t, "S6", "core-lib", ".github/mr-review.json", "main")
	if !file.Recorded || file.Status != 200 || !strings.Contains(string(file.File), `"profile": "library"`) || file.Dir != nil {
		t.Errorf("mr-review.json = %+v", file)
	}
	dir := Contents(t, "S6", "core-lib", ".github/skills", "main")
	if !dir.Recorded || dir.Status != 200 || len(dir.Dir) != 1 || dir.Dir[0] != (Entry{Name: "core-lib-code-review", Dir: true}) {
		t.Errorf(".github/skills = %+v", dir)
	}
	missing := Contents(t, "S6", "kmp-shared", ".github/skills", "main")
	if !missing.Recorded || missing.Status != 404 || missing.File != nil || missing.Dir != nil {
		t.Errorf("missing .github/skills = %+v", missing)
	}
	if other := Contents(t, "S6", "core-lib", ".github/mr-review.json", "develop"); other.Recorded {
		t.Errorf("a ref that was not recorded: %+v", other)
	}
}

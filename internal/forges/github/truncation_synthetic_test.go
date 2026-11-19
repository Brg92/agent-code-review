package github

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// Synthetic: no recorded tree is truncated and no recorded list is long.

func TestTruncatedTreeIsNeverAPartialList(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/git/trees/main", map[string]string{"recursive": "1"}, nil,
		`{"sha":"x","truncated":true,"tree":[{"path":"a.kt","type":"blob"},{"path":"b.kt","type":"blob"}]}`)
	tree := s.Tree(context.Background(), synthKey, "main")
	for _, prefix := range []string{"", "a"} {
		got, err := tree.List(prefix)
		if !isKind(err, base.ErrTreeTruncated) || got != nil {
			t.Errorf("List(%q) = %v, %v, want ErrTreeTruncated and nothing else", prefix, got, err)
		}
	}
	if n := s.Replay.Count("GET", repo+"/git/trees/main"); n != 1 {
		t.Errorf("the tree was read %d times, want once", n)
	}
}

func TestTreeKeepsFilesAndLeavesDirectoriesAndSubmodules(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/git/trees/main", map[string]string{"recursive": "1"}, nil,
		`{"truncated":false,"tree":[{"path":"src","type":"tree"},{"path":"src/b.kt","type":"blob"},{"path":"lib","type":"commit"},{"path":"a.kt","type":"blob"}]}`)
	got, err := s.Tree(context.Background(), synthKey, "main").List("")
	if err != nil || strings.Join(got, ",") != "a.kt,src/b.kt" {
		t.Errorf("List() = %v, %v, want the files, sorted", got, err)
	}
}

func filesPage(from, n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"filename":"f` + strconv.Itoa(from+i) + `.kt","status":"added","changes":1,"patch":"@@ -0,0 +1 @@\n+x"}`)
	}
	return b.String() + "]"
}

func TestChangedFilesResolvesEveryPage(t *testing.T) {
	s := onSynthetic(t, nil)
	next := func(page int) map[string]string {
		return map[string]string{"Link": `<https://api.github.com` + repo + `/pulls/7/files?per_page=100&page=` + strconv.Itoa(page) + `>; rel="next"`}
	}
	s.route(200, repo+"/pulls/7/files", perPageQuery, next(2), filesPage(0, 2))
	s.route(200, repo+"/pulls/7/files", map[string]string{"per_page": "100", "page": "2"}, next(3), filesPage(2, 2))
	s.route(200, repo+"/pulls/7/files", map[string]string{"per_page": "100", "page": "3"}, nil, filesPage(4, 1))
	files, err := s.ChangedFiles(context.Background(), synthKey)
	if err != nil || len(files) != 5 || files[4].Path != "f4.kt" {
		t.Errorf("ChangedFiles() = %d files, %v, want the 5 files of 3 pages in order", len(files), err)
	}
}

func TestChangedFilesBeyondThePageCapIsTruncated(t *testing.T) {
	s := onSynthetic(t, func(o *Options) { o.MaxPages = 2 })
	next := func(page int) map[string]string {
		return map[string]string{"Link": `<https://api.github.com` + repo + `/pulls/7/files?per_page=100&page=` + strconv.Itoa(page) + `>; rel="next"`}
	}
	s.route(200, repo+"/pulls/7/files", perPageQuery, next(2), filesPage(0, 1))
	s.route(200, repo+"/pulls/7/files", map[string]string{"per_page": "100", "page": "2"}, next(3), filesPage(1, 1))
	files, err := s.ChangedFiles(context.Background(), synthKey)
	if !isKind(err, base.ErrListTruncated) || files != nil {
		t.Errorf("ChangedFiles() = %v, %v, want ErrListTruncated and not the pages read", files, err)
	}
	if n := s.Replay.Count("GET", repo+"/pulls/7/files"); n != 2 {
		t.Errorf("%d pages read, want the cap of 2", n)
	}
}

func TestChangedFilesAtGitHubsCapIsTruncated(t *testing.T) {
	s := onSynthetic(t, func(o *Options) { o.MaxPages = 100 })
	for page := 1; page <= 30; page++ {
		q := map[string]string{"per_page": "100"}
		var h map[string]string
		if page > 1 {
			q["page"] = strconv.Itoa(page)
		}
		if page < 30 {
			h = map[string]string{"Link": `<https://api.github.com` + repo + `/pulls/7/files?per_page=100&page=` + strconv.Itoa(page+1) + `>; rel="next"`}
		}
		s.route(200, repo+"/pulls/7/files", q, h, filesPage((page-1)*100, 100))
	}
	files, err := s.ChangedFiles(context.Background(), synthKey)
	if !isKind(err, base.ErrListTruncated) || files != nil {
		t.Errorf("ChangedFiles() = %d files, %v, want ErrListTruncated: GitHub lists at most 3000", len(files), err)
	}
}

package github

import (
	"context"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
)

// Synthetic: the recorded files are small, UTF-8 and plain.

func TestFilesGetOfAnUnusualFile(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		body      string
		raw       string // the raw media type answer, if the file is asked for twice
		want      string
		wantFound bool
		wantKind  base.Kind
	}{
		{"empty file", "empty.txt", `{"type":"file","encoding":"base64","size":0,"content":""}`, "", "", true, ""},
		{"line wrapped base64", "a.md", `{"type":"file","encoding":"base64","size":5,"content":"` + b64wrapped("hello") + `"}`, "", "hello", true, ""},
		{"a file too large to be inlined is read raw", "big.txt", `{"type":"file","encoding":"none","size":2000000,"content":""}`, "line one\nline two\n", "line one\nline two\n", true, ""},
		{"a directory is not a file", "dir", `[{"name":"a","type":"file"}]`, "", "", false, ""},
		{"a symlink is not a file", "link", `{"type":"symlink","size":4,"target":"a.md"}`, "", "", false, ""},
		{"a submodule is not a file", "mod", `{"type":"submodule","size":0}`, "", "", false, ""},
		{"bytes that are not UTF-8", "bin.dat", `{"type":"file","encoding":"base64","size":2,"content":"/wA="}`, "", "", false, base.ErrTransport},
		{"base64 that is not base64", "bad.txt", `{"type":"file","encoding":"base64","size":2,"content":"@@@@"}`, "", "", false, base.ErrTransport},
		{"an answer that is not JSON", "x.txt", `<html>`, "", "", false, base.ErrTransport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			if tt.raw != "" {
				s.Replay.Add(sandbox.Route{Path: repo + "/contents/" + tt.path, Query: refMain, Status: 200, Body: tt.body, Then: []sandbox.Route{{Status: 200, Body: tt.raw}}})
			} else {
				s.route(200, repo+"/contents/"+tt.path, refMain, nil, tt.body)
			}
			got, found, err := s.Files().Get(context.Background(), synthKey, "main", tt.path)
			switch {
			case tt.wantKind != "":
				if !isKind(err, tt.wantKind) || found || got != nil {
					t.Errorf("Get() = %q, %v, %v, want %v", got, found, err, tt.wantKind)
				}
			case err != nil || found != tt.wantFound || (tt.wantFound && (string(got) != tt.want || got == nil)):
				t.Errorf("Get() = %q, %v, %v, want %q, %v", got, found, err, tt.want, tt.wantFound)
			}
		})
	}
}

func b64wrapped(s string) string {
	enc := b64(s)
	return enc[:4] + `\n` + enc[4:]
}

func TestFilesGetEscapesThePathAndTheRef(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/contents/dir with space/a#b%c.md", map[string]string{"ref": "feature/x y&z"}, nil, fileBody("ok"))
	got, found, err := s.Files().Get(context.Background(), synthKey, "feature/x y&z", "dir with space/a#b%c.md")
	if err != nil || !found || string(got) != "ok" {
		t.Errorf("Get() = %q, %v, %v", got, found, err)
	}
}

func TestFilesListOfARootAndOfAFile(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/contents", refMain, nil, `[{"name":"src","type":"dir"},{"name":"README.md","type":"file"}]`)
	s.route(200, repo+"/contents/README.md", refMain, nil, fileBody("x"))
	ctx := context.Background()
	got, err := s.Files().List(ctx, synthKey, "main", "")
	if want := []core.DirEntry{{Name: "src", Dir: true}, {Name: "README.md"}}; err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("List(root) = %+v, %v, want %+v", got, err, want)
	}
	got, err = s.Files().List(ctx, synthKey, "main", "README.md")
	if err != nil || len(got) != 0 {
		t.Errorf("List(a file) = %+v, %v, want no entries: it is not a directory", got, err)
	}
}

func TestFileFailuresAreTypedErrors(t *testing.T) {
	tests := []struct {
		status int
		want   base.Kind
	}{{401, base.ErrForbidden}, {403, base.ErrForbidden}, {500, base.ErrTransport}, {422, base.ErrTransport}}
	for _, tt := range tests {
		s := onSynthetic(t, nil)
		s.route(tt.status, repo+"/contents/a.md", refMain, nil, `{"message":"x"}`)
		got, found, err := s.Files().Get(context.Background(), synthKey, "main", "a.md")
		if !isKind(err, tt.want) || found || got != nil {
			t.Errorf("status %d: Get() = %q, %v, %v, want %v", tt.status, got, found, err, tt.want)
		}
	}
}

func TestChangedFilesStatusesAndMissingPatches(t *testing.T) {
	s := onSynthetic(t, nil)
	s.route(200, repo+"/pulls/7/files", perPageQuery, nil, `[
		{"filename":"a","status":"changed","changes":1,"patch":"@@ -1 +1 @@\n-a\n+b"},
		{"filename":"b","status":"unchanged","changes":0},
		{"filename":"c","previous_filename":"c0","status":"copied","changes":0},
		{"filename":"d","status":"modified","changes":0},
		{"filename":"e","status":"removed","changes":500}]`)
	files, err := s.ChangedFiles(context.Background(), synthKey)
	want := []core.FileChange{
		{Path: "a", Status: core.FileModified, Patch: "@@ -1 +1 @@\n-a\n+b"},
		{Path: "b", Status: core.FileUnchanged},
		{Path: "c", OldPath: "c0", Status: core.FileCopied},
		{Path: "d", Status: core.FileModified, Missing: core.PatchOmitted},
		{Path: "e", Status: core.FileRemoved, Missing: core.PatchTooLarge},
	}
	if err != nil || len(files) != len(want) {
		t.Fatalf("ChangedFiles() = %+v, %v", files, err)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("file %d = %+v, want %+v", i, files[i], want[i])
		}
	}
}

func TestChangeRequestStates(t *testing.T) {
	tests := []struct {
		body  string
		state core.ChangeState
		draft bool
	}{
		{`"state":"open","draft":true`, core.StateOpen, true},
		{`"state":"closed"`, core.StateClosed, false},
		{`"state":"closed","merged":true`, core.StateMerged, false},
	}
	for _, tt := range tests {
		s := onSynthetic(t, nil)
		s.route(200, repo+"/pulls/7", nil, nil, `{"title":"t","body":null,`+tt.body+`,"head":{"sha":"h"},"base":{"sha":"b","ref":"main"},"user":null}`)
		cr, err := s.ChangeRequest(context.Background(), synthKey)
		if err != nil || cr.State != tt.state || cr.Draft != tt.draft || cr.Description != "" || cr.Author != "" {
			t.Errorf("ChangeRequest(%s) = %+v, %v, want %s draft %v, no description and no author", tt.body, cr, err, tt.state, tt.draft)
		}
	}
	// Metadata the core cannot use is an error, not a half-filled value.
	s := onSynthetic(t, nil)
	s.route(200, repo+"/pulls/7", nil, nil, `{"title":"t","state":"weird","head":{"sha":"h"},"base":{"sha":"b","ref":"main"}}`)
	cr, err := s.ChangeRequest(context.Background(), synthKey)
	if !isKind(err, base.ErrTransport) || cr != (core.ChangeRequest{}) {
		t.Errorf("ChangeRequest() = %+v, %v, want a transport error", cr, err)
	}
}

func TestChangesGetOfAnUnreadableChangeRequest(t *testing.T) {
	tests := []struct {
		name         string
		pull, issue  int
		wantKind     string
		wantErr      bool
		wantErrKinds base.Kind
	}{
		{"403 on both", 403, 403, "nothing", false, ""},
		{"404 then 403", 404, 403, "nothing", false, ""},
		{"server error on the pull request", 500, 200, "", true, base.ErrTransport},
		{"issue lookup fails", 404, 500, "", true, base.ErrTransport},
		{"a refused token is not nothing", 401, 401, "", true, base.ErrForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := onSynthetic(t, nil)
			s.route(tt.pull, repo+"/pulls/7", nil, nil, `{"message":"x"}`)
			s.route(tt.issue, repo+"/issues/7", nil, nil, `{}`)
			item, err := s.Changes().Get(context.Background(), synthKey)
			if tt.wantErr {
				if !isKind(err, tt.wantErrKinds) {
					t.Errorf("Get() = %+v, %v, want %v", item, err, tt.wantErrKinds)
				}
				return
			}
			if err != nil || item.Kind.String() != tt.wantKind {
				t.Errorf("Get() = %+v, %v, want %s", item, err, tt.wantKind)
			}
		})
	}
}

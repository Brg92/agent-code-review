package github

import (
	"context"
	"errors"
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/base"
)

func TestOtherHostKeysAreNeverRead(t *testing.T) {
	p, replay, tok := onReplay(t, "S1")
	other := key("sample-app", 2)
	other.Host = "ghe.example.com"
	ctx := context.Background()

	_, errChange := p.ChangeRequest(ctx, other)
	_, errFiles := p.ChangedFiles(ctx, other)
	_, _, errGet := p.Files().Get(ctx, other, "main", "README.md")
	_, errList := p.Files().List(ctx, other, "main", "")
	_, errTree := p.Tree(ctx, other, "main").List("")
	_, errCI := p.CI(ctx, other, "abc")
	_, errNotes := p.Notes(ctx, other)
	_, errSearch := p.Search().Search(other.ProjectKey(), "main", "x")
	_, errItem := p.Changes().Get(ctx, other)
	for name, err := range map[string]error{"ChangeRequest": errChange, "ChangedFiles": errFiles, "Files.Get": errGet,
		"Files.List": errList, "Tree": errTree, "CI": errCI, "Notes": errNotes, "Search": errSearch, "Changes": errItem} {
		var ue *UnsupportedHostError
		if !errors.Is(err, base.ErrNotFound) || !errors.As(err, &ue) {
			t.Errorf("%s on another host: error = %v, want ErrNotFound caused by an UnsupportedHostError", name, err)
		}
	}
	if got := replay.Requests(); len(got) != 0 {
		t.Errorf("requests for a key on another host: %v", got)
	}
	if tok.asked.Load() != 0 {
		t.Error("the token was asked for a key on another host")
	}
}

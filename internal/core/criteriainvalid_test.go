package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestIgnoreInvalidCriteria(t *testing.T) {
	err := &CriteriaError{File: ConfigFile, Errors: []FieldError{{Pointer: "/profile", Message: "must be a string"}, {Pointer: "/schema", Message: "must be 1"}}}
	c := IgnoreInvalidCriteria(testProject, "main", err)
	if c.Source != SourceDefault || c.Project != testProject.ProjectKey() || c.Ref != "main" || c.Schema != 1 {
		t.Errorf("criteria %+v", c)
	}
	if len(c.Packs)+len(c.Rules)+len(c.DisabledRules)+len(c.Files) != 0 || c.Profile != "" {
		t.Errorf("an invalid configuration still sets something: %+v", c)
	}
	want := []IgnoredItem{{
		Item:   "`" + ConfigFile + "`",
		Reason: "the file is not valid, so none of the repository criteria apply: /profile: must be a string; /schema: must be 1",
	}}
	if !reflect.DeepEqual(c.Ignored, want) {
		t.Errorf("ignored = %+v, want %+v", c.Ignored, want)
	}
	if len(c.Notes) != 1 || c.Notes[0] != want[0].Note() || !strings.HasPrefix(c.Notes[0], IgnoredPrefix) {
		t.Errorf("notes = %q", c.Notes)
	}
}

// TestIgnoreInvalidCriteriaIsLikeNoCriteria: what is left is what a
// repository without criteria gets, plus the item.
func TestIgnoreInvalidCriteriaIsLikeNoCriteria(t *testing.T) {
	none, err := ReadCriteria(t.Context(), newMemFiles(t, nil), CriteriaInput{Project: testProject, Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	got := IgnoreInvalidCriteria(testProject, "main", &CriteriaError{File: ConfigFile, Errors: []FieldError{{Pointer: "/x", Message: "m"}}})
	got.Notes, got.Ignored, none.Notes = nil, nil, nil
	if !reflect.DeepEqual(got, none) {
		t.Errorf("got %+v, want %+v", got, none)
	}
}

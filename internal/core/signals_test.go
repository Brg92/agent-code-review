package core

import (
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

func TestAddedLines(t *testing.T) {
	d, err := NewFileDiff("a.kt", "", FileModified, "@@ -1,3 +1,4 @@\n a\n-b\n+B\n+C\n c\n@@ -10,1 +11,2 @@\n x\n+y\n")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d.AddedLines(), map[int]bool{2: true, 3: true, 12: true}; !reflect.DeepEqual(got, want) {
		t.Errorf("added %v, want %v", got, want)
	}
	if got := (FileDiff{Path: "b.png", Status: FileAdded}).AddedLines(); len(got) != 0 {
		t.Errorf("a diff without hunks adds %v", got)
	}
}

func TestRulesFile(t *testing.T) {
	d, err := NewFileDiff("app/src/main/B.kt", "app/src/main/A.kt", FileRenamed, "@@ -1,1 +1,2 @@\n a\n+b\n")
	if err != nil {
		t.Fatal(err)
	}
	assigned := rules.File{Path: "ignored", Language: "kotlin", Test: true, Source: "ignored"}
	got := ChangedFile{Diff: d, Head: []string{"a", "b"}}.RulesFile(assigned)
	want := rules.ChangedFile{
		File:    rules.File{Path: "app/src/main/B.kt", Language: "kotlin", Test: true, Source: "a\nb\n", Added: map[int]bool{2: true}},
		OldPath: "app/src/main/A.kt",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RulesFile = %+v, want %+v", got, want)
	}
	gone := ChangedFile{Diff: FileDiff{Path: "app/src/main/C.kt", Status: FileRemoved}, Base: []string{"x"}}.RulesFile(rules.File{})
	if !gone.Removed || gone.Source != "" || gone.Path != "app/src/main/C.kt" || len(gone.Added) != 0 {
		t.Errorf("removed file = %+v", gone)
	}
}

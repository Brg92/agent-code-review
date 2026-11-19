package core

import "testing"

func TestChangeStateValid(t *testing.T) {
	tests := []struct {
		s    ChangeState
		want bool
	}{
		{StateOpen, true},
		{StateClosed, true},
		{StateMerged, true},
		{"", false},
		{"draft", false},
	}
	for _, tt := range tests {
		if got := tt.s.Valid(); got != tt.want {
			t.Errorf("ChangeState(%q).Valid() = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestPatchReasonValid(t *testing.T) {
	for _, r := range []PatchReason{PatchTooLarge, PatchBinary, PatchOmitted} {
		if !r.Valid() {
			t.Errorf("PatchReason(%q).Valid() = false", r)
		}
	}
	for _, r := range []PatchReason{"", "huge"} {
		if r.Valid() {
			t.Errorf("PatchReason(%q).Valid() = true", r)
		}
	}
}

func TestFileChangeDiff(t *testing.T) {
	const patch = "@@ -1,2 +1,2 @@\n keep\n-old\n+new"
	tests := []struct {
		name      string
		in        FileChange
		wantLines int
		wantErr   bool
	}{
		{"with a patch", FileChange{Path: "a.kt", Status: FileModified, Patch: patch}, 3, false},
		{"rename without changes", FileChange{Path: "b.kt", OldPath: "a.kt", Status: FileRenamed}, 0, false},
		{"patch missing", FileChange{Path: "big.kt", Status: FileModified, Missing: PatchTooLarge}, 0, false},
		{"unchanged", FileChange{Path: "c.kt", Status: FileUnchanged}, 0, false},
		{"unknown status", FileChange{Path: "d.kt", Status: "weird"}, 0, true},
		{"bad patch", FileChange{Path: "e.kt", Status: FileModified, Patch: "garbage"}, 0, true},
		{"patch and a reason", FileChange{Path: "f.kt", Status: FileModified, Patch: patch, Missing: PatchOmitted}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := tt.in.Diff()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Diff() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			n := 0
			for _, h := range d.Hunks {
				n += len(h.Lines)
			}
			if n != tt.wantLines {
				t.Errorf("Diff() has %d lines, want %d", n, tt.wantLines)
			}
			if d.Path != tt.in.Path || d.OldPath != tt.in.OldPath || d.Status != tt.in.Status {
				t.Errorf("Diff() = %+v, want the path, old path and status of %+v", d, tt.in)
			}
		})
	}
}

func TestFileChangeHasPatch(t *testing.T) {
	tests := []struct {
		in   FileChange
		want bool
	}{
		{FileChange{Status: FileModified, Patch: "@@ -1 +1 @@\n-a\n+b"}, true},
		{FileChange{Status: FileRenamed}, false},
		{FileChange{Status: FileModified, Missing: PatchBinary, Binary: true}, false},
	}
	for _, tt := range tests {
		if got := tt.in.HasPatch(); got != tt.want {
			t.Errorf("%+v.HasPatch() = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestChangeRequestZeroIsInvalid(t *testing.T) {
	if err := (ChangeRequest{}).Validate(); err == nil {
		t.Error("Validate() accepted an empty change request")
	}
	ok := ChangeRequest{
		Key: Key{Forge: GitHub, Host: "github.com", Project: "acme/app", Number: 1}, Title: "t",
		HeadSHA: "a", BaseSHA: "b", TargetBranch: "main", Author: "u", State: StateOpen,
	}
	if err := ok.Validate(); err != nil {
		t.Errorf("Validate() = %v", err)
	}
	bad := ok
	bad.State = "x"
	if err := bad.Validate(); err == nil {
		t.Error("Validate() accepted an unknown state")
	}
}

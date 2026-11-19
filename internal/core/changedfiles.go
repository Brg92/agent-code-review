package core

import (
	"errors"
	"fmt"
)

// ChangeState is the state of a change request on its platform.
type ChangeState string

// The states of a change request.
const (
	StateOpen   ChangeState = "open"
	StateClosed ChangeState = "closed"
	StateMerged ChangeState = "merged"
)

// Valid reports whether s is a known state.
func (s ChangeState) Valid() bool {
	switch s {
	case StateOpen, StateClosed, StateMerged:
		return true
	}
	return false
}

// ChangeRequest is the metadata of a change request as its platform gives
// it.
type ChangeRequest struct {
	Key Key
	// Title and Description are as written by the author.
	Title, Description string
	// HeadSHA and BaseSHA are the commits the change request compares.
	HeadSHA, BaseSHA string
	// TargetBranch is the branch the change request is merged into; the
	// criteria are read from it.
	TargetBranch string
	// Author is the login of the author.
	Author string
	State  ChangeState
	Draft  bool
}

// Validate reports a change request a provider filled wrongly.
func (c ChangeRequest) Validate() error {
	switch {
	case c.Key == (Key{}):
		return errors.New("change request: no key")
	case c.HeadSHA == "" || c.BaseSHA == "" || c.TargetBranch == "":
		return fmt.Errorf("change request %s: head, base and target branch are required", c.Key)
	case !c.State.Valid():
		return fmt.Errorf("change request %s: unknown state %q", c.Key, string(c.State))
	}
	return nil
}

// PatchReason is why a platform gave no patch for a changed file whose
// content differs.
type PatchReason string

// The reasons a patch is missing.
const (
	// PatchTooLarge: the platform does not return diffs this large.
	PatchTooLarge PatchReason = "too-large"
	// PatchBinary: the file is binary.
	PatchBinary PatchReason = "binary"
	// PatchOmitted: the platform left the patch out and did not say why.
	PatchOmitted PatchReason = "omitted"
)

// Valid reports whether r is a known reason.
func (r PatchReason) Valid() bool {
	switch r {
	case PatchTooLarge, PatchBinary, PatchOmitted:
		return true
	}
	return false
}

// FileChange is a changed file of a change request as its platform lists
// it: the unified diff when the platform gives it, otherwise why not.
type FileChange struct {
	// Path is the path in the change request; for a removed file, its old
	// path.
	Path string
	// OldPath is the previous path of a renamed or copied file.
	OldPath string
	Status  FileStatus
	// Patch is the unified diff of the file, with hunks only. It is empty
	// when the platform gave none.
	Patch string
	// Missing is why the platform gave no patch of a file whose content
	// differs; empty when the patch is complete, which includes a file
	// with no change of content, such as a rename.
	Missing PatchReason
	// Binary is set when the platform says the file is binary.
	Binary bool
}

// HasPatch reports whether the platform gave the patch of the file.
func (f FileChange) HasPatch() bool { return f.Patch != "" && f.Missing == "" }

// Diff parses the patch into the diff the work package is built from. A
// file without a patch has no hunks.
func (f FileChange) Diff() (FileDiff, error) {
	if f.Patch != "" && f.Missing != "" {
		return FileDiff{}, fmt.Errorf("%s: a patch and the reason it is missing", f.Path)
	}
	return NewFileDiff(f.Path, f.OldPath, f.Status, f.Patch)
}

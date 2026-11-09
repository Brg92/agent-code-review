package state

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

var cleanNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// makeEntry creates a directory, or a file, under runs and sets its
// modification time to age before cleanNow.
func makeEntry(t *testing.T, runs, name string, dir bool, age time.Duration) string {
	t.Helper()
	p := filepath.Join(runs, name)
	var err error
	if dir {
		err = os.MkdirAll(filepath.Join(p, "inner"), 0o700)
		if err == nil {
			err = os.WriteFile(filepath.Join(p, FileName), []byte("{}"), 0o600)
		}
	} else {
		err = os.WriteFile(p, []byte("x"), 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	when := cleanNow.Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

const day = 24 * time.Hour

func TestCleanRuns(t *testing.T) {
	base := t.TempDir()
	runs := filepath.Join(base, "runs")
	old := makeEntry(t, runs, "20261020T080000Z-00000001", true, 20*day)
	older := makeEntry(t, runs, "20261001T080000Z-00000002", true, 39*day)
	young := makeEntry(t, runs, "20261108T080000Z-00000003", true, 1*day)
	edge := makeEntry(t, runs, "20261102T120000Z-00000004", true, 7*day)
	current := makeEntry(t, runs, "20261001T080000Z-00000005", true, 39*day)
	notARun := makeEntry(t, runs, "my notes", true, 30*day)
	file := makeEntry(t, runs, "20261001T080000Z-00000006", false, 30*day)

	res := CleanRuns(base, 7, cleanNow, "20261001T080000Z-00000005")
	if len(res.Errors) != 0 {
		t.Errorf("errors: %v", res.Errors)
	}
	if want := []string{older, old}; !slices.Equal(res.Removed, want) {
		t.Errorf("removed %q, want %q", res.Removed, want)
	}
	for _, p := range []string{old, older} {
		if exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	for _, p := range []string{young, edge, current, notARun, file} {
		if !exists(p) {
			t.Errorf("%s was removed", p)
		}
	}
}

func TestCleanRunsKeepsEverythingWithZeroDays(t *testing.T) {
	base := t.TempDir()
	old := makeEntry(t, filepath.Join(base, "runs"), "20261001T080000Z-00000001", true, 300*day)
	if res := CleanRuns(base, 0, cleanNow, ""); len(res.Removed) != 0 || len(res.Errors) != 0 || !exists(old) {
		t.Errorf("keepDays 0: %+v", res)
	}
}

func TestCleanRunsWithoutRuns(t *testing.T) {
	if res := CleanRuns(filepath.Join(t.TempDir(), "missing"), 7, cleanNow, ""); len(res.Removed) != 0 || len(res.Errors) != 0 {
		t.Errorf("no runs directory: %+v", res)
	}
}

// TestCleanRunsNeverFollowsSymlinks checks that a link named like a run,
// or a runs directory that is a link, is left alone with what it points
// to, however old that is.
func TestCleanRunsNeverFollowsSymlinks(t *testing.T) {
	outside := t.TempDir()
	target := makeEntry(t, outside, "20261001T080000Z-0000000a", true, 300*day)
	base := t.TempDir()
	runs := filepath.Join(base, "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(runs, "20261001T080000Z-0000000b")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	res := CleanRuns(base, 7, cleanNow, "")
	if len(res.Removed) != 0 || !exists(link) || !exists(filepath.Join(target, FileName)) {
		t.Errorf("a link to an old run: %+v", res)
	}

	linkedBase := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(linkedBase, "runs")); err != nil {
		t.Fatal(err)
	}
	res = CleanRuns(linkedBase, 7, cleanNow, "")
	if len(res.Removed) != 0 || len(res.Errors) != 1 || !exists(filepath.Join(target, FileName)) {
		t.Errorf("runs is a link: %+v", res)
	}
}

func TestCleanRunsReportsErrors(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs directory permissions that the user cannot bypass")
	}
	base := t.TempDir()
	runs := filepath.Join(base, "runs")
	old := makeEntry(t, runs, "20261001T080000Z-00000001", true, 30*day)
	locked := filepath.Join(old, "inner")
	if err := os.WriteFile(filepath.Join(locked, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	young := makeEntry(t, runs, "20261108T080000Z-00000002", true, day)
	res := CleanRuns(base, 7, cleanNow, "")
	if len(res.Errors) != 1 || len(res.Removed) != 0 || !exists(young) {
		t.Errorf("an old run that cannot be removed: %+v", res)
	}
}

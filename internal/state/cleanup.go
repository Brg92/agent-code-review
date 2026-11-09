package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CleanResult is what CleanRuns did: the run directories it removed, in
// name order, and the problems it met, none of which stops a run.
type CleanResult struct {
	Removed []string
	Errors  []error
}

// CleanRuns removes the run directories under <base>/runs that were last
// changed more than keepDays days before now, except current. Only
// directories whose name is a run id are candidates; symbolic links are
// never followed nor removed, and a runs directory that is a link is left
// alone. keepDays 0 keeps every run.
func CleanRuns(base string, keepDays int, now time.Time, current string) CleanResult {
	var res CleanResult
	if keepDays <= 0 {
		return res
	}
	runs := filepath.Join(base, runsDir)
	info, err := os.Lstat(runs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return res
	case err != nil:
		res.Errors = append(res.Errors, fmt.Errorf("clean runs: %w", err))
		return res
	case info.Mode()&fs.ModeSymlink != 0 || !info.IsDir():
		res.Errors = append(res.Errors, fmt.Errorf("clean runs: %s is not a directory; nothing is removed", runs))
		return res
	}
	entries, err := os.ReadDir(runs)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("clean runs: %w", err))
		return res
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	cutoff := now.Add(-time.Duration(keepDays) * 24 * time.Hour)
	for _, e := range entries {
		name := e.Name()
		// ReadDir reports the type of a link as a link, never of its target.
		if name == current || !ValidRunID(name) || e.Type()&fs.ModeSymlink != 0 || !e.IsDir() {
			continue
		}
		p := filepath.Join(runs, name)
		info, err := os.Lstat(p)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("clean runs: %w", err))
			continue
		}
		if !info.IsDir() || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("clean runs: %w", err))
			continue
		}
		res.Removed = append(res.Removed, p)
	}
	return res
}

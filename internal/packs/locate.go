package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PacksDirEnv names the environment variable that replaces the packs
// directory next to the executable.
const PacksDirEnv = "ACR_PACKS_DIR"

// packsDirName is the directory of the packs next to the executable.
const packsDirName = "packs"

// LocateInput is what finding the pack locations reads. A nil function is
// the real one: os.Getenv, os.Executable and filepath.EvalSymlinks.
type LocateInput struct {
	Getenv       func(string) string
	Executable   func() (string, error)
	EvalSymlinks func(string) (string, error)
	// PackPaths are the extra pack directories of the user configuration.
	PackPaths []string
}

// NoPacksError reports that no location holds a pack.
type NoPacksError struct {
	// Tried are the locations looked at, in order.
	Tried []string
}

func (e *NoPacksError) Error() string {
	return fmt.Sprintf("no packs found: looked in %s; set %s or packPaths in the user configuration to a directory of packs",
		strings.Join(e.Tried, ", "), PacksDirEnv)
}

// Locate returns the directories to load packs from, in order: the
// directory in PacksDirEnv when it is set, otherwise the packs directory
// next to the executable, its symbolic links resolved; then each of the
// pack paths. A directory is kept once, and only when it holds a pack: a
// subdirectory with a manifest. With none, it fails with a *NoPacksError.
func Locate(in LocateInput) ([]string, error) {
	if in.Getenv == nil {
		in.Getenv = os.Getenv
	}
	if in.Executable == nil {
		in.Executable = os.Executable
	}
	if in.EvalSymlinks == nil {
		in.EvalSymlinks = filepath.EvalSymlinks
	}
	var candidates []string
	if dir := in.Getenv(PacksDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("%s must be an absolute path, not %q", PacksDirEnv, dir)
		}
		candidates = append(candidates, dir)
	} else if exe, err := in.Executable(); err == nil {
		if real, err := in.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), packsDirName))
	}
	candidates = append(candidates, in.PackPaths...)
	var tried, found []string
	seen := map[string]bool{}
	for _, c := range candidates {
		c = filepath.Clean(c)
		if seen[c] {
			continue
		}
		seen[c] = true
		tried = append(tried, c)
		if holdsPack(c) {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return nil, &NoPacksError{Tried: tried}
	}
	return found, nil
}

// holdsPack reports whether dir has a subdirectory with a manifest.
func holdsPack(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(dir, e.Name(), ManifestFile)); e.IsDir() && err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

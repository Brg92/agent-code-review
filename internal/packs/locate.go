package packs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PacksDirEnv names the environment variable that replaces the places
// where the packs are looked for.
const PacksDirEnv = "ACR_PACKS_DIR"

const (
	// packsDirName and schemasDirName are the directories of the packs and
	// of the JSON Schemas in a root.
	packsDirName   = "packs"
	schemasDirName = "schemas"
	// pluginFile marks a plugin root: the layout of the release branch.
	pluginFile = "plugin.json"
)

// LocateInput is what finding the pack locations reads. A nil Executable or
// EvalSymlinks is the real one: os.Executable and filepath.EvalSymlinks. A
// nil Getenv reads nothing.
type LocateInput struct {
	Getenv       func(string) string
	Executable   func() (string, error)
	EvalSymlinks func(string) (string, error)
	// PackPaths are the extra pack directories of the user configuration.
	PackPaths []string
}

// Layout is where the packs and the schemas were found.
type Layout struct {
	// Dirs are the directories to load packs from, in order.
	Dirs []string
	// ExeDir is the folder the executable was started from, its symbolic
	// links resolved; "" when it cannot be told.
	ExeDir string
	// Root is the folder that holds the packs of the layout in use, and the
	// schemas beside them; "" when the packs come only from the user
	// configuration.
	Root string
}

// Schema returns the absolute path of the JSON Schema file name in the schemas
// directory of the root, and whether the file is there.
func (l Layout) Schema(name string) (string, bool) {
	if l.Root == "" {
		return "", false
	}
	path := filepath.Join(l.Root, schemasDirName, name)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
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

// Locate finds the packs, in this order, the first of the first three that
// holds a pack being the root:
//
//  1. the directory in PacksDirEnv, when it is set; the root is its parent,
//     and the next two are not looked at;
//  2. packs/ next to the executable, its symbolic links resolved: the layout
//     of the GitHub Release archive;
//  3. packs/ of the plugin root two levels above the folder of the
//     executable, only when that folder holds plugin.json: the layout of the
//     release branch, bin/<os>-<arch>/<executable>;
//  4. each of the pack paths of the user configuration, which add packs
//     and never move the root.
//
// It looks nowhere else, and never higher. A directory is kept once, and only
// when it holds a pack: a subdirectory with a manifest. With none, it fails
// with a *NoPacksError.
func Locate(in LocateInput) (Layout, error) {
	if in.Getenv == nil {
		in.Getenv = func(string) string { return "" }
	}
	if in.Executable == nil {
		in.Executable = os.Executable
	}
	if in.EvalSymlinks == nil {
		in.EvalSymlinks = filepath.EvalSymlinks
	}
	var loc Layout
	var tried []string
	// candidates are the roots to try: the packs directory, and its root.
	type candidate struct{ packs, root string }
	var candidates []candidate
	exe, err := in.Executable()
	if err == nil {
		if real, err := in.EvalSymlinks(exe); err == nil {
			exe = real
		}
		loc.ExeDir = filepath.Dir(exe)
	}
	if dir := in.Getenv(PacksDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return Layout{}, fmt.Errorf("%s must be an absolute path, not %q", PacksDirEnv, dir)
		}
		candidates = append(candidates, candidate{dir, filepath.Dir(filepath.Clean(dir))})
	} else if err == nil {
		candidates = append(candidates, candidate{filepath.Join(loc.ExeDir, packsDirName), loc.ExeDir})
		if plugin := filepath.Dir(filepath.Dir(loc.ExeDir)); isRegular(filepath.Join(plugin, pluginFile)) {
			candidates = append(candidates, candidate{filepath.Join(plugin, packsDirName), plugin})
		}
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		c.packs = filepath.Clean(c.packs)
		seen[c.packs] = true
		tried = append(tried, c.packs)
		if holdsPack(c.packs) {
			loc.Dirs, loc.Root = []string{c.packs}, c.root
			break
		}
	}
	for _, p := range in.PackPaths {
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		tried = append(tried, p)
		if holdsPack(p) {
			loc.Dirs = append(loc.Dirs, p)
		}
	}
	if len(loc.Dirs) == 0 {
		return Layout{ExeDir: loc.ExeDir}, &NoPacksError{Tried: tried}
	}
	return loc, nil
}

// isRegular reports whether path is a regular file.
func isRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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

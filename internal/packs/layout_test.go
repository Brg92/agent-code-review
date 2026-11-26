package packs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const judgementSchema = "judgement.schema.json"

// install is a temporary installation: the files of a layout, with every
// symbolic link of its path resolved.
type install struct {
	root string
	exe  string
}

// addInstall writes what a layout holds under root: one pack, the
// judgement schema and an executable at exe (relative to root).
func addInstall(t *testing.T, root, exe string) install {
	t.Helper()
	writeFile(t, filepath.Join(root, "packs", "android", ManifestFile), "{}")
	writeFile(t, filepath.Join(root, "schemas", judgementSchema), "{}")
	path := filepath.Join(root, filepath.FromSlash(exe))
	writeFile(t, path, "binary")
	return install{root: root, exe: path}
}

func locateAt(t *testing.T, exe string, vars map[string]string, packPaths ...string) (Layout, error) {
	t.Helper()
	return Locate(LocateInput{
		Getenv:     func(k string) string { return vars[k] },
		Executable: func() (string, error) { return exe, nil },
		PackPaths:  packPaths,
	})
}

// The layout of a GitHub Release archive: packs/ and schemas/ next to the
// executable.
func TestLocateArchiveLayout(t *testing.T) {
	in := addInstall(t, realDir(t), "agent-code-review")
	got, err := locateAt(t, in.exe, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Layout{Dirs: []string{filepath.Join(in.root, "packs")}, ExeDir: in.root, Root: in.root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Locate = %+v, want %+v", got, want)
	}
	if schema, ok := got.Schema(judgementSchema); !ok || schema != filepath.Join(in.root, "schemas", judgementSchema) {
		t.Errorf("Schema = %q, %v", schema, ok)
	}
}

// The layout of the release branch: the plugin root holds plugin.json,
// packs/ and schemas/, and the executable is in bin/<os>-<arch>/.
func TestLocateReleaseLayout(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "bin/linux-amd64/agent-code-review")
	writeFile(t, filepath.Join(root, "plugin.json"), "{}")
	got, err := locateAt(t, in.exe, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Layout{Dirs: []string{filepath.Join(root, "packs")}, ExeDir: filepath.Join(root, "bin", "linux-amd64"), Root: root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Locate = %+v, want %+v", got, want)
	}
	if schema, ok := got.Schema(judgementSchema); !ok || schema != filepath.Join(root, "schemas", judgementSchema) {
		t.Errorf("Schema = %q, %v", schema, ok)
	}
}

// Without plugin.json the folder two levels above is just a folder: its packs
// are not used, and neither is anything further up.
func TestLocateReleaseLayoutWithoutPluginJSON(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "bin/linux-amd64/agent-code-review")
	_, err := locateAt(t, in.exe, nil)
	var none *NoPacksError
	if !errors.As(err, &none) {
		t.Fatalf("Locate error = %v, want a *NoPacksError", err)
	}
	for _, tried := range none.Tried {
		if tried == filepath.Join(root, "packs") {
			t.Errorf("tried %q, which is not a plugin root", tried)
		}
	}
}

// The archive layout is looked at first, and the plugin root is never
// searched higher than two levels.
func TestLocateDoesNotWalkUp(t *testing.T) {
	root := realDir(t)
	addInstall(t, root, "x")
	writeFile(t, filepath.Join(root, "plugin.json"), "{}")
	deep := filepath.Join(root, "bin", "linux-amd64", "extra", "agent-code-review")
	writeFile(t, deep, "binary")
	if _, err := locateAt(t, deep, nil); err == nil {
		t.Error("found packs three levels above the executable")
	}
}

func TestLocateKeepsThePacksNextToTheExecutableFirst(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "bin/linux-amd64/agent-code-review")
	writeFile(t, filepath.Join(root, "plugin.json"), "{}")
	writeFile(t, filepath.Join(root, "bin", "linux-amd64", "packs", "web", ManifestFile), "{}")
	got, err := locateAt(t, in.exe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "bin", "linux-amd64", "packs")}; !reflect.DeepEqual(got.Dirs, want) {
		t.Errorf("Dirs = %q, want %q", got.Dirs, want)
	}
	if got.Root != filepath.Join(root, "bin", "linux-amd64") {
		t.Errorf("Root = %q, want the folder of the executable", got.Root)
	}
}

// ACR_PACKS_DIR replaces the layouts, and the schemas are then looked for
// beside it.
func TestLocateEnvironmentOverridesTheLayouts(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "bin/linux-amd64/agent-code-review")
	writeFile(t, filepath.Join(root, "plugin.json"), "{}")
	other := realDir(t)
	writeFile(t, filepath.Join(other, "packs", "web", ManifestFile), "{}")
	got, err := locateAt(t, in.exe, map[string]string{PacksDirEnv: filepath.Join(other, "packs")})
	if err != nil {
		t.Fatal(err)
	}
	if want := (Layout{Dirs: []string{filepath.Join(other, "packs")}, ExeDir: filepath.Join(root, "bin", "linux-amd64"), Root: other}); !reflect.DeepEqual(got, want) {
		t.Errorf("Locate = %+v, want %+v", got, want)
	}
	if schema, ok := got.Schema(judgementSchema); ok {
		t.Errorf("Schema = %q: the packs of the environment have no schemas beside them", schema)
	}
}

// The pack paths of the user configuration add packs; they never move the root
// the schemas come from.
func TestLocatePackPathsDoNotMoveTheRoot(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "agent-code-review")
	extra := packsRoot(t, false)
	got, err := locateAt(t, in.exe, nil, extra)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(root, "packs"), extra}; !reflect.DeepEqual(got.Dirs, want) || got.Root != root {
		t.Errorf("Locate = %+v, want %q from the root %q", got, want, root)
	}
	// With nothing in the layout, the pack paths are all there is and
	// there is no root.
	bare := realDir(t)
	writeFile(t, filepath.Join(bare, "agent-code-review"), "binary")
	got, err = locateAt(t, filepath.Join(bare, "agent-code-review"), nil, extra)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "" || !reflect.DeepEqual(got.Dirs, []string{extra}) {
		t.Errorf("Locate = %+v, want only %q and no root", got, extra)
	}
	if schema, ok := got.Schema(judgementSchema); ok {
		t.Errorf("Schema = %q without a root", schema)
	}
}

func TestLocateSchemaMissing(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "agent-code-review")
	if err := os.Remove(filepath.Join(root, "schemas", judgementSchema)); err != nil {
		t.Fatal(err)
	}
	got, err := locateAt(t, in.exe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if schema, ok := got.Schema(judgementSchema); ok {
		t.Errorf("Schema = %q, want it missing", schema)
	}
	if got.Root != root {
		t.Errorf("Root = %q: the packs are still found", got.Root)
	}
}

// A link to the executable, as a package manager makes it, leads to the
// layout of the real one.
func TestLocateSymlinkedExecutableInTheReleaseLayout(t *testing.T) {
	root := realDir(t)
	in := addInstall(t, root, "bin/linux-amd64/agent-code-review")
	writeFile(t, filepath.Join(root, "plugin.json"), "{}")
	link := filepath.Join(t.TempDir(), "agent-code-review")
	if err := os.Symlink(in.exe, link); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	got, err := locateAt(t, link, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != root || got.ExeDir != filepath.Join(root, "bin", "linux-amd64") {
		t.Errorf("Locate = %+v, want the root %q", got, root)
	}
}

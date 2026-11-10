package packs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// packsRoot makes a directory with one pack directory in it, unless
// empty, and returns its path.
func packsRoot(t *testing.T, empty bool) string {
	t.Helper()
	root := t.TempDir()
	if !empty {
		writeFile(t, filepath.Join(root, "web", ManifestFile), "{}")
	}
	return root
}

// realDir makes a temporary directory and returns its path with every
// symbolic link resolved, as the temporary directory of macOS has one.
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLocate(t *testing.T) {
	env, exeDir, extra, other := packsRoot(t, false), realDir(t), packsRoot(t, false), packsRoot(t, false)
	bundled := filepath.Join(exeDir, "packs")
	writeFile(t, filepath.Join(bundled, "android", ManifestFile), "{}")
	exe := filepath.Join(exeDir, "agent-code-review")
	writeFile(t, exe, "binary")
	empty := packsRoot(t, true)
	tests := []struct {
		name      string
		env       string
		exe       func() (string, error)
		packPaths []string
		want      []string
	}{
		{"next to the executable", "", nil, nil, []string{bundled}},
		{"the environment wins over the executable", env, nil, nil, []string{env}},
		{"then the user's pack paths, in order", "", nil, []string{extra, other}, []string{bundled, extra, other}},
		{"a location listed twice counts once", env, nil, []string{extra, env, extra + string(filepath.Separator)}, []string{env, extra}},
		{"a location without packs is skipped", empty, nil, []string{filepath.Join(empty, "missing"), extra}, []string{extra}},
		{"an executable that cannot be found is skipped", "", func() (string, error) { return "", errors.New("no executable") }, []string{extra}, []string{extra}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := LocateInput{
				Getenv:     func(k string) string { return map[string]string{PacksDirEnv: tt.env}[k] },
				Executable: func() (string, error) { return exe, nil },
				PackPaths:  tt.packPaths,
			}
			if tt.exe != nil {
				in.Executable = tt.exe
			}
			got, err := Locate(in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("locations %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLocateFollowsTheExecutableSymlink checks that the packs are looked
// for next to the real executable, not next to a link to it, as a package
// manager installs it.
func TestLocateFollowsTheExecutableSymlink(t *testing.T) {
	install, bin := realDir(t), t.TempDir()
	exe := filepath.Join(install, "agent-code-review")
	writeFile(t, exe, "binary")
	writeFile(t, filepath.Join(install, "packs", "web", ManifestFile), "{}")
	link := filepath.Join(bin, "agent-code-review")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}
	got, err := Locate(LocateInput{Getenv: func(string) string { return "" }, Executable: func() (string, error) { return link, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(install, "packs"); len(got) != 1 || got[0] != want {
		t.Errorf("locations %q, want %q", got, want)
	}
}

func TestLocateErrors(t *testing.T) {
	empty := realDir(t)
	none := func() (string, error) { return filepath.Join(empty, "agent-code-review"), nil }
	_, err := Locate(LocateInput{Getenv: func(string) string { return "" }, Executable: none, PackPaths: []string{empty}})
	var nf *NoPacksError
	if !errors.As(err, &nf) {
		t.Fatalf("error %v, want a *NoPacksError", err)
	}
	if want := []string{filepath.Join(empty, "packs"), empty}; !reflect.DeepEqual(nf.Tried, want) {
		t.Errorf("tried %q, want %q", nf.Tried, want)
	}
	if !strings.Contains(err.Error(), PacksDirEnv) || !strings.Contains(err.Error(), empty) {
		t.Errorf("message %q names neither the variable nor the locations", err)
	}
	_, err = Locate(LocateInput{Getenv: func(string) string { return filepath.Join("relative", "packs") }, Executable: none})
	if err == nil || !strings.Contains(err.Error(), "absolute") || errors.As(err, &nf) {
		t.Errorf("error %v, want one that asks for an absolute %s", err, PacksDirEnv)
	}
}

// TestLocateDefaults checks that the zero input reads the real
// environment and executable.
func TestLocateDefaults(t *testing.T) {
	dir := packsRoot(t, false)
	t.Setenv(PacksDirEnv, dir)
	got, err := Locate(LocateInput{})
	if err != nil || len(got) != 1 || got[0] != dir {
		t.Errorf("Locate() = %q, %v; want %s from the environment", got, err, dir)
	}
}

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Brg92/agent-code-review/internal/version"
)

const (
	shLauncher  = "bin/agent-code-review"
	cmdLauncher = "bin/agent-code-review.cmd"
)

func TestTreeHoldsTheLaunchers(t *testing.T) {
	files := buildReal(t, nil)
	sh, cmd := files[shLauncher], files[cmdLauncher]
	if !bytes.HasPrefix(sh, []byte("#!/bin/sh\n")) || bytes.Contains(sh, []byte("\r")) {
		t.Errorf("%s must be a POSIX sh script with LF line endings:\n%s", shLauncher, sh)
	}
	if !bytes.HasPrefix(cmd, []byte("@echo off\r\n")) || bytes.Count(cmd, []byte("\n")) != bytes.Count(cmd, []byte("\r\n")) {
		t.Errorf("%s must have CRLF line endings on every line:\n%q", cmdLauncher, cmd)
	}
	if !isExecutable(shLauncher) || isExecutable(cmdLauncher) || isExecutable("skills/code-review/SKILL.md") {
		t.Error("only the sh launcher is executable")
	}
}

// A launcher picks the executable and starts it; it sets nothing and knows
// nothing of the packs.
func TestLaunchersHaveNoOtherLogic(t *testing.T) {
	files := buildReal(t, nil)
	for _, p := range []string{shLauncher, cmdLauncher} {
		text := string(files[p])
		for _, bad := range []string{"ACR_", "packs", "schemas", "export ", "PATH="} {
			if strings.Contains(text, bad) {
				t.Errorf("%s contains %q", p, bad)
			}
		}
		if len(text) > 2048 {
			t.Errorf("%s is %d bytes: a launcher is small", p, len(text))
		}
	}
	for _, bad := range []string{"set \"ACR", "setx"} {
		if strings.Contains(string(files[cmdLauncher]), bad) {
			t.Errorf("%s sets the environment: %q", cmdLauncher, bad)
		}
	}
}

func TestBuildRefusesALauncherThatIsNotOne(t *testing.T) {
	for name, content := range map[string]string{
		"plugin/bin/agent-code-review":     "echo hi\n",
		"plugin/bin/agent-code-review.cmd": "echo hi\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			src := crlfSources(t)
			src[name] = &fstest.MapFile{Data: []byte(content)}
			o := options{Src: src, Version: testVersion, Allowlist: realAllowlist(t)}
			if _, err := buildTree(o); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("buildTree error = %v, want a refusal that names %s", err, name)
			}
		})
	}
}

func TestWriteTreeMarksTheShLauncherExecutable(t *testing.T) {
	out := filepath.Join(t.TempDir(), "tree")
	if err := writeTree(out, buildReal(t, nil)); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit; the modes file records it")
	}
	for p, want := range map[string]os.FileMode{shLauncher: 0o755, cmdLauncher: 0o644, "plugin.json": 0o644} {
		info, err := os.Stat(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: mode %v, err %v, want %v", p, info.Mode().Perm(), err, want)
		}
	}
}

func TestTreeModes(t *testing.T) {
	files := map[string][]byte{"bin/agent-code-review": nil, "bin/agent-code-review.cmd": nil, "a/b": nil}
	want := "100644 a/b\n100755 bin/agent-code-review\n100644 bin/agent-code-review.cmd\n"
	if got := treeModes(files); got != want {
		t.Errorf("treeModes = %q, want %q", got, want)
	}
}

func TestDigestCoversTheMode(t *testing.T) {
	a := map[string][]byte{"bin/agent-code-review": []byte("x")}
	if treeDigest(a) == treeDigest(map[string][]byte{"bin/agent-code-review.cmd": []byte("x")}) {
		t.Error("the digest ignores the path")
	}
	if !strings.Contains(treeModes(a), "100755") {
		t.Error("the mode of the launcher is not recorded")
	}
}

func TestRunWritesTheModesFile(t *testing.T) {
	out, modes := filepath.Join(t.TempDir(), "tree"), filepath.Join(t.TempDir(), "modes.txt")
	if code, _, stderr := runTool(t, "-src", repoRoot, "-out", out, "-modes", modes); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	data, err := os.ReadFile(modes)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"100755 bin/agent-code-review\n", "100644 bin/agent-code-review.cmd\n", "100644 plugin.json\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("modes file has no line %q:\n%s", want, data)
		}
	}
}

// installed is a temporary release tree with the real executable of this
// system where the launchers look for it.
type installed struct {
	dir, launcher, exe string
	target             string
}

func newRelease(t *testing.T) installed { return newReleaseIn(t, "plugin root") }

// newReleaseIn is newRelease with the plugin root in a folder of this name.
func newReleaseIn(t *testing.T, name string) installed {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := writeTree(dir, buildReal(t, func(o *options) { o.Version = "0.0.0-dev" })); err != nil {
		t.Fatal(err)
	}
	r := installed{dir: dir, target: runtime.GOOS + "-" + runtime.GOARCH}
	r.exe = filepath.Join(dir, "bin", r.target, "agent-code-review")
	r.launcher = filepath.Join(dir, "bin", "agent-code-review")
	if runtime.GOOS == "windows" {
		r.exe += ".exe"
		r.launcher += ".cmd"
	}
	build := exec.Command("go", "build", "-o", r.exe, "./cmd/agent-code-review")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return r
}

// start runs the launcher with args and returns what it prints and how it ends.
func (r installed) start(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return startLauncher(t, r.launcher, env, args...)
}

// startLauncher runs launcher with args. An entry of env that is NAME= removes
// the variable instead of setting it empty, which Windows would keep as a
// variable that is defined.
func startLauncher(t *testing.T, launcher string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	name := launcher
	dir := ""
	if runtime.GOOS == "windows" {
		// Go starts a batch file through cmd /c, which cannot take a path
		// with spaces together with an argument with spaces. The launcher
		// is started by a relative name from its folder; it finds itself
		// through %~dp0, spaces and all.
		name, dir = `.\`+filepath.Base(launcher), filepath.Dir(launcher)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &out, &errOut
	cmd.Env = withEnv(os.Environ(), env)
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("run %s %q: %v", launcher, args, err)
		}
		code = exit.ExitCode()
	}
	return out.String(), errOut.String(), code
}

// withEnv sets the variables of changes in base, and removes those that
// changes gives as NAME= with no value.
func withEnv(base, changes []string) []string {
	var out []string
	for _, kv := range base {
		keep := true
		for _, c := range changes {
			if name, _, _ := strings.Cut(c, "="); strings.EqualFold(name, strings.SplitN(kv, "=", 2)[0]) {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	for _, c := range changes {
		if _, value, _ := strings.Cut(c, "="); value != "" {
			out = append(out, c)
		}
	}
	return out
}

// refusal checks that stderr is the message of a launcher that cannot
// start: what, why and what to do.
func refusal(t *testing.T, stderr string, mentions ...string) {
	t.Helper()
	for _, want := range append([]string{"agent-code-review: ", "\n  why: ", "\n  what to do: "}, mentions...) {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

// The launcher of the release tree, with the executable built for this
// system in the place the release branch puts it: it starts it, hands over
// the arguments as they are and ends with its exit code.
func TestLauncherStartsTheExecutable(t *testing.T) {
	r := newRelease(t)

	t.Run("version", func(t *testing.T) {
		stdout, stderr, code := r.start(t, nil, "version")
		if code != 0 || stdout != version.Version+"\n" || stderr != "" {
			t.Errorf("version: code %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("an argument with spaces arrives whole", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "a folder with  spaces")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := r.start(t, nil, "check-criteria", dir)
		if code != 0 || !strings.Contains(stdout, "Repository criteria in "+dir+"\n") {
			t.Errorf("check-criteria: code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
	})

	t.Run("the exit code is the one of the executable", func(t *testing.T) {
		stdout, stderr, code := r.start(t, nil, "no-such-command")
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "usage: agent-code-review") {
			t.Errorf("unknown command: code %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("no argument", func(t *testing.T) {
		if _, _, code := r.start(t, nil); code != 2 {
			t.Errorf("code %d, want 2", code)
		}
	})

	t.Run("a launcher called from another folder and by a link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the .cmd launcher is found by its path; links are a POSIX matter")
		}
		link := filepath.Join(t.TempDir(), "acr")
		if err := os.Symlink(r.launcher, link); err != nil {
			t.Fatal(err)
		}
		if stdout, _, code := startLauncher(t, link, nil, "version"); code != 0 || stdout != version.Version+"\n" {
			t.Errorf("through a link: code %d, stdout %q", code, stdout)
		}
	})

	t.Run("the executable is missing", func(t *testing.T) {
		moved := r.exe + ".moved"
		if err := os.Rename(r.exe, moved); err != nil {
			t.Fatal(err)
		}
		defer os.Rename(moved, r.exe)
		stdout, stderr, code := r.start(t, nil, "version")
		if code != 1 || stdout != "" {
			t.Errorf("code %d, stdout %q, want 1 and nothing", code, stdout)
		}
		refusal(t, stderr, r.target)
	})
}

// A system the launcher has no executable for ends with 1 and says why. The
// POSIX launcher reads uname: a program of that name first on the PATH stands
// for another system.
func TestShLauncherRefusesWhatItHasNoExecutableFor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher is not run on Windows")
	}
	r := newRelease(t)
	fake := filepath.Join(t.TempDir(), "uname")
	script := "#!/bin/sh\ncase $1 in -s) echo \"$FAKE_OS\" ;; -m) echo \"$FAKE_ARCH\" ;; esac\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "PATH=" + filepath.Dir(fake) + string(os.PathListSeparator) + os.Getenv("PATH")
	tests := []struct {
		name, os, arch string
		mentions       []string
		code           int
	}{
		{"FreeBSD", "FreeBSD", "amd64", []string{"FreeBSD"}, 1},
		{"Git Bash on Windows", "MINGW64_NT-10.0", "x86_64", []string{"MINGW64_NT-10.0", "agent-code-review.cmd"}, 1},
		{"a 32-bit machine", "Linux", "i686", []string{"i686"}, 1},
		{"riscv", "Darwin", "riscv64", []string{"riscv64"}, 1},
		// Supported systems and their other names reach the lookup of the
		// executable, which is not there for another system.
		{"Linux on arm64 by its other name", "Linux", "aarch64", []string{"linux-arm64"}, 1},
		{"macOS on arm64", "Darwin", "arm64", []string{"darwin-arm64"}, 1},
		{"macOS on x86_64", "Darwin", "x86_64", []string{"darwin-amd64"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mentions[0] == "linux-arm64" && r.target == "linux-arm64" || tt.mentions[0] == "darwin-arm64" && r.target == "darwin-arm64" || tt.mentions[0] == "darwin-amd64" && r.target == "darwin-amd64" {
				t.Skip("this is the target the executable was built for")
			}
			stdout, stderr, code := r.start(t, []string{path, "FAKE_OS=" + tt.os, "FAKE_ARCH=" + tt.arch}, "version")
			if code != tt.code || stdout != "" {
				t.Errorf("code %d, stdout %q, want %d and nothing", code, stdout, tt.code)
			}
			refusal(t, stderr, tt.mentions...)
		})
	}
}

// The Windows launcher reads the architecture variables, the one of the
// 32-bit process first when there is one.
func TestCmdLauncherRefusesWhatItHasNoExecutableFor(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the .cmd launcher runs on Windows only")
	}
	r := newRelease(t)
	tests := []struct {
		name, arch, wow string
		code            int
		mentions        []string
	}{
		{"32-bit Windows", "x86", "", 1, []string{"x86"}},
		{"a 32-bit process on 64-bit Windows starts the 64-bit executable", "x86", "AMD64", 0, nil},
		{"a 32-bit process on arm64 Windows wants the arm64 executable", "x86", "ARM64", 1, []string{"windows-arm64"}},
		{"ia64", "IA64", "", 1, []string{"IA64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code == 0 && runtime.GOARCH != "amd64" {
				t.Skip("needs the amd64 executable")
			}
			env := []string{"PROCESSOR_ARCHITECTURE=" + tt.arch, "PROCESSOR_ARCHITEW6432=" + tt.wow}
			stdout, stderr, code := r.start(t, env, "version")
			if code != tt.code {
				t.Fatalf("code %d, want %d\nstdout %q\nstderr %s", code, tt.code, stdout, stderr)
			}
			if tt.code == 0 {
				if stdout != version.Version+"\n" {
					t.Errorf("stdout %q", stdout)
				}
				return
			}
			refusal(t, stderr, tt.mentions...)
		})
	}
}

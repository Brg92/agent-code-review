// Package credhelper turns a test binary into a git credential helper, so
// tests can drive a real git without touching the user's keychain. The
// helper keeps its entries in a JSON file in the test's temp dir. It is test
// support code only.
//
// A test package uses it from TestMain:
//
//	func TestMain(m *testing.M) {
//		credhelper.Main()
//		os.Exit(m.Run())
//	}
//
// and calls Install in a test to point git at the helper.
package credhelper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Name is the helper name git is configured with: git runs it as
// git-credential-acrtest, found on PATH. It keeps what it is given.
const Name = "acrtest"

// NameDrop is a helper that accepts every request and keeps nothing, as a
// helper that ignores store does.
const NameDrop = "acrdrop"

// NameOther is a helper that answers every get with the account OtherUser
// and the token OtherToken, whatever the request names, and ignores store
// and erase, as the helper of the GitHub CLI does for github.com.
const NameOther = "acrother"

// NameStale is a helper that keeps the first token it is given for an entry
// and ignores every later store, as a helper that does not update. It has
// a store of its own, StaleStore, apart from the one of Name.
const NameStale = "acrstale"

// OtherUser and OtherToken are what NameOther answers: canary values, so a
// test can tell which token git returned. They are not secrets.
const (
	OtherUser  = "other-user"
	OtherToken = "canary-other-token-0001"
)

// envStore carries the store path to the helper process.
const envStore = "ACR_TEST_CREDENTIAL_STORE"

// envFail makes the helper fail with the given message.
const envFail = "ACR_TEST_CREDENTIAL_FAIL"

// FailWith makes the helper, for the rest of the test, print msg on stderr
// and exit with status 1 on every request, as a helper does when the
// keychain is locked.
func FailWith(t *testing.T, msg string) { t.Setenv(envFail, msg) }

// recordedEnv are the variables the helper records, to check how git ran.
var recordedEnv = []string{"GIT_TERMINAL_PROMPT", "GIT_ASKPASS", "GCM_INTERACTIVE", "LC_ALL"}

// Entry is one stored credential.
type Entry struct {
	Host, Username, Password string
}

type state struct {
	Entries []Entry
	Env     map[string]string
}

// Main serves one helper request and exits when the process was started by
// git as the helper; otherwise it returns at once.
func Main() {
	store := os.Getenv(envStore)
	if store == "" || len(os.Args) != 2 {
		return
	}
	op := os.Args[1]
	if op != "get" && op != "store" && op != "erase" {
		return
	}
	if msg := os.Getenv(envFail); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	switch modeOf(os.Args[0]) {
	case NameOther:
		serveOther(op, os.Stdin, os.Stdout)
		os.Exit(0)
	case NameDrop:
		serveDrop(op, os.Stdin, os.Stdout)
		os.Exit(0)
	case NameStale:
		if err := serveStale(op, os.Stdin, os.Stdout, StaleStore(store), os.Getenv); err != nil {
			fmt.Fprintln(os.Stderr, "credhelper:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err := serve(op, os.Stdin, os.Stdout, store, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "credhelper:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// modeOf names the helper a program was started as, from its file name, or
// "" if it is none of ours.
func modeOf(arg0 string) string {
	base := path.Base(strings.ReplaceAll(arg0, `\`, "/"))
	base = strings.TrimSuffix(base, ".exe")
	name, ok := strings.CutPrefix(base, "git-credential-")
	if ok && (name == Name || name == NameDrop || name == NameOther || name == NameStale) {
		return name
	}
	return ""
}

// serveOther answers like a helper for another account: the same user and
// token for any request, and nothing is stored or erased.
func serveOther(op string, in io.Reader, out io.Writer) {
	_, _ = io.Copy(io.Discard, in)
	if op == "get" {
		fmt.Fprintf(out, "username=%s\npassword=%s\n", OtherUser, OtherToken)
	}
}

// StaleStore is the file the helper NameStale keeps its entries in, for the
// store Install returned.
func StaleStore(store string) string { return store + ".stale" }

// serveStale is serve, except that a store of an entry that is already there
// changes nothing.
func serveStale(op string, in io.Reader, out io.Writer, store string, getenv func(string) string) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if op == "store" {
		attrs := map[string]string{}
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() && sc.Text() != "" {
			if k, v, ok := strings.Cut(sc.Text(), "="); ok {
				attrs[k] = v
			}
		}
		st, err := load(store)
		if err != nil {
			return err
		}
		for _, e := range st.Entries {
			if e.Host == attrs["host"] && e.Username == attrs["username"] {
				return nil
			}
		}
	}
	return serve(op, bytes.NewReader(data), out, store, getenv)
}

// serveDrop accepts every request, keeps nothing and answers nothing.
func serveDrop(_ string, in io.Reader, _ io.Writer) { _, _ = io.Copy(io.Discard, in) }

// Config describes the git configuration Install writes.
type Config struct {
	// Helpers are the credential.helper values, in order; nil means the
	// test helper only. The helpers Name, NameDrop, NameOther and NameStale
	// are installed when named here.
	Helpers []string
	// Extra is added to the git configuration after the helpers.
	Extra string
}

// Install puts the test helper on PATH and isolates git from the user's
// configuration: no system config, a global config in a temp dir, and HOME
// in a temp dir. It returns the store path and the home directory.
func Install(t *testing.T, cfg Config) (store, home string) {
	t.Helper()
	dir := t.TempDir()
	home = filepath.Join(dir, "home")
	bin := filepath.Join(dir, "bin")
	for _, d := range []string{home, bin} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helpers := cfg.Helpers
	if helpers == nil {
		helpers = []string{Name}
	}
	// The test binary is every helper named in the configuration, found on
	// PATH under the name git gives it.
	for _, h := range append(append([]string{}, helpers...), cfg.Extra) {
		for _, name := range []string{Name, NameDrop, NameOther, NameStale} {
			if !strings.Contains(h, name) {
				continue
			}
			file := "git-credential-" + name
			if runtime.GOOS == "windows" {
				file += ".exe"
			}
			if _, err := os.Stat(filepath.Join(bin, file)); err != nil {
				copyFile(t, exe, filepath.Join(bin, file))
			}
		}
	}
	var conf strings.Builder
	conf.WriteString("[credential]\n")
	for _, h := range helpers {
		fmt.Fprintf(&conf, "\thelper = %s\n", h)
	}
	conf.WriteString(cfg.Extra)
	gitconfig := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(gitconfig, []byte(conf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	store = filepath.Join(dir, "store.json")

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(envStore, store)
	// A race-enabled helper would otherwise wait a second at every exit.
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	checkIsolation(t, dir, helpers)
	return store, home
}

// checkIsolation stops the test if git sees any helper but the configured
// ones, for example one compiled into git or set by the runner: a test must
// never reach a real keychain.
func checkIsolation(t *testing.T, dir string, want []string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--null", "--get-all", "credential.helper")
	cmd.Dir = dir
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
		t.Fatalf("git config: %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(out) == 0 {
		got = nil
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("git sees credential helpers %q, want only %q: the test isolation leaks", got, want)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o700); err != nil {
		t.Fatal(err)
	}
}

// Read returns the stored entries and the environment the helper last saw.
func Read(t *testing.T, store string) ([]Entry, map[string]string) {
	t.Helper()
	st, err := load(store)
	if err != nil {
		t.Fatal(err)
	}
	return st.Entries, st.Env
}

func load(store string) (state, error) {
	st := state{Env: map[string]string{}}
	data, err := os.ReadFile(store)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(data, &st)
	return st, err
}

// serve answers one git credential helper operation.
func serve(op string, in io.Reader, out io.Writer, store string, getenv func(string) string) error {
	attrs := map[string]string{}
	sc := bufio.NewScanner(in)
	for sc.Scan() && sc.Text() != "" {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			attrs[k] = v
		}
	}
	st, err := load(store)
	if err != nil {
		return err
	}
	for _, k := range recordedEnv {
		st.Env[k] = getenv(k)
	}
	match := func(e Entry) bool { return e.Host == attrs["host"] && e.Username == attrs["username"] }
	kept := st.Entries[:0]
	for _, e := range st.Entries {
		switch {
		case !match(e):
			kept = append(kept, e)
		case op == "get":
			fmt.Fprintf(out, "username=%s\npassword=%s\n", e.Username, e.Password)
			kept = append(kept, e)
		}
	}
	switch op {
	case "get", "erase":
	case "store":
		kept = append(kept, Entry{Host: attrs["host"], Username: attrs["username"], Password: attrs["password"]})
	default:
		return fmt.Errorf("unknown operation %q", op)
	}
	st.Entries = kept
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(store, data, 0o600)
}

package credhelper

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestServe(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store.json")
	env := map[string]string{"GIT_TERMINAL_PROMPT": "0"}
	getenv := func(k string) string { return env[k] }
	run := func(op, in string) string {
		t.Helper()
		var out bytes.Buffer
		if err := serve(op, strings.NewReader(in), &out, store, getenv); err != nil {
			t.Fatalf("serve %s: %v", op, err)
		}
		return out.String()
	}

	if got := run("get", "protocol=https\nhost=h\nusername=u\n\n"); got != "" {
		t.Errorf("get on empty store = %q", got)
	}
	run("store", "protocol=https\nhost=h\nusername=u\npassword=p\n\n")
	run("store", "protocol=https\nhost=h\nusername=other\npassword=q\n\n")
	if got := run("get", "protocol=https\nhost=h\nusername=u\n\n"); got != "username=u\npassword=p\n" {
		t.Errorf("get = %q", got)
	}
	run("erase", "protocol=https\nhost=h\nusername=u\n\n")
	if got := run("get", "protocol=https\nhost=h\nusername=u\n\n"); got != "" {
		t.Errorf("get after erase = %q", got)
	}
	entries, seen := Read(t, store)
	if len(entries) != 1 || entries[0] != (Entry{Host: "h", Username: "other", Password: "q"}) {
		t.Errorf("entries = %+v", entries)
	}
	if seen["GIT_TERMINAL_PROMPT"] != "0" || seen["GCM_INTERACTIVE"] != "" {
		t.Errorf("seen env = %v", seen)
	}
	var out bytes.Buffer
	if err := serve("bogus", strings.NewReader(""), &out, store, getenv); err == nil {
		t.Error("serve(bogus) = nil error")
	}
}

func TestReadMissingStore(t *testing.T) {
	entries, seen := Read(t, filepath.Join(t.TempDir(), "none.json"))
	if len(entries) != 0 || len(seen) != 0 {
		t.Errorf("Read = %v %v", entries, seen)
	}
}

func TestModeOf(t *testing.T) {
	tests := map[string]string{
		"git-credential-acrtest":            Name,
		"git-credential-acrdrop":            NameDrop,
		"git-credential-acrother":           NameOther,
		"/tmp/bin/git-credential-acrother":  NameOther,
		`C:\bin\git-credential-acrdrop.exe`: NameDrop,
		"git-credential-other":              "",
		"credhelper.test":                   "",
		"":                                  "",
	}
	for arg0, want := range tests {
		if got := modeOf(arg0); got != want {
			t.Errorf("modeOf(%q) = %q, want %q", arg0, got, want)
		}
	}
}

func TestServeOther(t *testing.T) {
	var out bytes.Buffer
	in := "protocol=https\nhost=github.com\nusername=acr-api\n\n"
	serveOther("get", strings.NewReader(in), &out)
	if want := "username=" + OtherUser + "\npassword=" + OtherToken + "\n"; out.String() != want {
		t.Errorf("get = %q, want %q", out.String(), want)
	}
	for _, op := range []string{"store", "erase"} {
		out.Reset()
		serveOther(op, strings.NewReader(in), &out)
		if out.Len() != 0 {
			t.Errorf("%s answered %q, want nothing", op, out.String())
		}
	}
}

func TestServeDrop(t *testing.T) {
	var out bytes.Buffer
	in := "protocol=https\nhost=github.com\nusername=acr-api\npassword=p\n\n"
	for _, op := range []string{"get", "store", "erase"} {
		serveDrop(op, strings.NewReader(in), &out)
	}
	if out.Len() != 0 {
		t.Errorf("a helper that keeps nothing answered %q", out.String())
	}
}

func TestServeStale(t *testing.T) {
	store := filepath.Join(t.TempDir(), "stale.json")
	getenv := func(string) string { return "" }
	run := func(op, in string) string {
		t.Helper()
		var out bytes.Buffer
		if err := serveStale(op, strings.NewReader(in), &out, store, getenv); err != nil {
			t.Fatalf("serveStale %s: %v", op, err)
		}
		return out.String()
	}
	entry := func(pass string) string { return "protocol=https\nhost=h\nusername=u\npassword=" + pass + "\n\n" }
	query := "protocol=https\nhost=h\nusername=u\n\n"

	run("store", entry("first"))
	run("store", entry("second"))
	if got := run("get", query); got != "username=u\npassword=first\n" {
		t.Errorf("get after a second store = %q, want the first token: a stale helper ignores updates", got)
	}
	run("erase", query)
	if got := run("get", query); got != "" {
		t.Errorf("get after erase = %q", got)
	}
	run("store", entry("third"))
	if got := run("get", query); got != "username=u\npassword=third\n" {
		t.Errorf("get after erase and store = %q", got)
	}
	if got := StaleStore("/x/store.json"); got != "/x/store.json.stale" {
		t.Errorf("StaleStore = %q", got)
	}
}

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

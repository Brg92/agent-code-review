package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestParseForge(t *testing.T) {
	tests := []struct {
		in      string
		want    Forge
		wantErr bool
	}{
		{"github", GitHub, false},
		{"gitlab", GitLab, false},
		{"GitHub", "", true},
		{"bitbucket", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := ParseForge(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseForge(%q) = %q, %v; want %q, error %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestForgeJSON(t *testing.T) {
	var f Forge
	if err := json.Unmarshal([]byte(`"gitlab"`), &f); err != nil || f != GitLab {
		t.Errorf("unmarshal gitlab = %q, %v", f, err)
	}
	if err := json.Unmarshal([]byte(`"bitbucket"`), &f); err == nil {
		t.Error("unmarshal accepted an unknown forge")
	}
	if _, err := json.Marshal(Forge("bitbucket")); err == nil {
		t.Error("marshal accepted an unknown forge")
	}
}

func TestParseKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Key
		// canonical is the formatted key; empty means the input itself.
		canonical string
	}{
		{"github", "github:github.com/acr-plugin-sandbox/core-lib#3",
			Key{GitHub, "github.com", "acr-plugin-sandbox/core-lib", 3}, ""},
		{"gitlab", "gitlab:gitlab.com/acr-plugin-sandbox/sample-app#1",
			Key{GitLab, "gitlab.com", "acr-plugin-sandbox/sample-app", 1}, ""},
		{"nested groups", "gitlab:gitlab.example.test/group/sub.group/deep_er/my-app#42",
			Key{GitLab, "gitlab.example.test", "group/sub.group/deep_er/my-app", 42}, ""},
		{"port", "gitlab:git.example.test:8443/team/app#7",
			Key{GitLab, "git.example.test:8443", "team/app", 7}, ""},
		{"self-hosted github", "github:ghe.example.test/Org/Repo.Name#120",
			Key{GitHub, "ghe.example.test", "Org/Repo.Name", 120}, ""},
		{"leading dot name", "github:github.com/team/.github#2",
			Key{GitHub, "github.com", "team/.github", 2}, ""},
		{"dots inside a name", "github:github.com/team/a..b#2",
			Key{GitHub, "github.com", "team/a..b", 2}, ""},
		{"three dots", "github:github.com/team/...#2",
			Key{GitHub, "github.com", "team/...", 2}, ""},
		{"uppercase host", "github:GitHub.COM/acr-plugin-sandbox/core-lib#3",
			Key{GitHub, "github.com", "acr-plugin-sandbox/core-lib", 3},
			"github:github.com/acr-plugin-sandbox/core-lib#3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseKey(tt.in)
			if err != nil {
				t.Fatalf("ParseKey(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseKey(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			canonical := tt.canonical
			if canonical == "" {
				canonical = tt.in
			}
			if s := got.String(); s != canonical {
				t.Errorf("String() = %q, want %q", s, canonical)
			}
			again, err := ParseKey(got.String())
			if err != nil || again != got {
				t.Errorf("round trip = %+v, %v; want %+v", again, err, got)
			}
		})
	}
}

func TestParseKeyRejects(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty", ""},
		{"unknown forge", "bitbucket:bitbucket.org/team/app#1"},
		{"uppercase forge", "GitHub:github.com/team/app#1"},
		{"no forge", "github.com/team/app#1"},
		{"empty forge", ":github.com/team/app#1"},
		{"no host", "github:/team/app#1"},
		{"no project", "github:github.com#1"},
		{"single segment project", "github:github.com/app#1"},
		{"empty segment", "github:github.com/team//app#1"},
		{"trailing slash", "github:github.com/team/app/#1"},
		{"no number", "github:github.com/team/app"},
		{"empty number", "github:github.com/team/app#"},
		{"zero number", "github:github.com/team/app#0"},
		{"negative number", "github:github.com/team/app#-1"},
		{"plus sign", "github:github.com/team/app#+1"},
		{"leading zero", "github:github.com/team/app#07"},
		{"number overflow", "github:github.com/team/app#99999999999999999999"},
		{"two hashes", "github:github.com/team/app#1#2"},
		{"bad host char", "github:git_hub.com/team/app#1"},
		{"empty port", "github:github.com:/team/app#1"},
		{"letters in port", "github:github.com:ab/team/app#1"},
		{"space in project", "github:github.com/team/my app#1"},
		{"surrounding space", " github:github.com/team/app#1"},
		{"dot segment", "github:github.com/team/./app#1"},
		{"dot dot segment", "github:github.com/team/../app#1"},
		{"leading dot dot", "github:github.com/../app#1"},
		{"leading dot", "github:github.com/./app#1"},
		{"trailing dot dot", "github:github.com/team/..#1"},
		{"trailing dot", "github:github.com/team/.#1"},
		{"dot dot escape", "github:github.com/../../user#1"},
		{"nested dot dot", "gitlab:gitlab.com/group/sub/../app#1"},
		{"percent-encoded dot dot", "github:github.com/team/%2e%2e#1"},
		{"percent-encoded upper dot dot", "github:github.com/team/%2E%2E#1"},
		{"half percent-encoded dot dot", "github:github.com/team/.%2e#1"},
		{"percent-encoded slash", "github:github.com/team%2Fapp#1"},
		{"percent in segment", "github:github.com/team/app%20x#1"},
		{"backslash dot dot", "github:github.com/team/..\\app#1"},
		{"backslash separator", "github:github.com/team\\app#1"},
		{"backslash dot", "github:github.com/.\\app#1"},
		{"newline", "github:github.com/team/app\n#1"},
		{"nul", "github:github.com/team/app\x00#1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, err := ParseKey(tt.in)
			if err == nil {
				t.Fatalf("ParseKey(%q) = %+v, want an error", tt.in, k)
			}
			var ke *KeyError
			if !errors.As(err, &ke) || ke.Key != tt.in {
				t.Errorf("ParseKey(%q) error %v is not a *KeyError for the input", tt.in, err)
			}
		})
	}
}

// keyPatterns returns every change request key pattern (ending with the
// number) and project key pattern (without it) of the schemas, by schema
// file and path in the file.
func keyPatterns(t *testing.T) (crKeys, projectKeys map[string]string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "schemas", "*.schema.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no schemas: %v", err)
	}
	crKeys, projectKeys = map[string]string{}, map[string]string{}
	var walk func(file, at string, v any)
	walk = func(file, at string, v any) {
		switch v := v.(type) {
		case map[string]any:
			if p, ok := v["pattern"].(string); ok && strings.HasPrefix(p, "^(?:github|gitlab):") {
				if strings.HasSuffix(p, "#[1-9][0-9]*$") {
					crKeys[file+at] = p
				} else {
					projectKeys[file+at] = p
				}
			}
			for k, c := range v {
				walk(file, at+"/"+k, c)
			}
		case []any:
			for i, c := range v {
				walk(file, at+"/"+strconv.Itoa(i), c)
			}
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		walk(filepath.Base(f), "", doc)
	}
	return crKeys, projectKeys
}

// TestKeysMatchSchema checks that a string parses exactly when it matches
// every key pattern of the schemas once its host is lowercased, the
// project key patterns included, so that no file the schemas accept holds
// a key the parser refuses, a "." or ".." segment above all.
func TestKeysMatchSchema(t *testing.T) {
	crKeys, projectKeys := keyPatterns(t)
	if _, ok := crKeys["state.schema.json/$defs/crKey"]; !ok || len(crKeys) < 5 || len(projectKeys) < 5 {
		t.Fatalf("found %d key patterns and %d project patterns, want the ones of every schema: %v", len(crKeys), len(projectKeys), crKeys)
	}
	inputs := []string{
		"github:github.com/a/b#1",
		"gitlab:h:1/a/b/c/d#10",
		"github:h/a.b/_-.#9",
		"github:h:/a/b#1",
		"github:h/a#1",
		"github:h/a/b#0",
		"gitlab:H.EXAMPLE.TEST/a/b#5",
		"github:h/a/b/#1",
		"github:h//a/b#1",
		"github:h/a/b#1\n",
		"github:h/.github/b#1",
		"github:h/a/...#1",
		"github:h/a/..b#1",
		"github:h/a/b..#1",
		"github:h/./b#1",
		"github:h/a/.#1",
		"github:h/../b#1",
		"github:h/a/..#1",
		"github:h/a/../b#1",
		"github:h/a/b/../../c#1",
		"github:h/a/%2e%2e#1",
		"github:h/a\\..\\b#1",
		"github:h/a/./.#1",
	}
	for _, in := range inputs {
		_, err := ParseKey(in)
		for name, p := range crKeys {
			re := regexp.MustCompile(p)
			if ok := re.MatchString(lowerHost(in)); ok != (err == nil) {
				t.Errorf("%s: pattern matches %q = %v, ParseKey error = %v", name, in, ok, err)
			}
		}
		project, _, _ := strings.Cut(in, "#")
		_, err = ParseKey(project + "#1")
		for name, p := range projectKeys {
			re := regexp.MustCompile(p)
			if ok := re.MatchString(lowerHost(project)); ok != (err == nil) {
				t.Errorf("%s: pattern matches %q = %v, ParseKey error = %v", name, project, ok, err)
			}
		}
	}
}

// lowerHost lowercases the host of a key, as ParseKey does.
func lowerHost(s string) string {
	forge, rest, ok := strings.Cut(s, ":")
	if !ok {
		return s
	}
	host, tail, ok := strings.Cut(rest, "/")
	if !ok {
		return s
	}
	return forge + ":" + strings.ToLower(host) + "/" + tail
}

func TestKeyJSON(t *testing.T) {
	in := `["github:GITHUB.com/team/app#5"]`
	var keys []Key
	if err := json.Unmarshal([]byte(in), &keys); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if want := `["github:github.com/team/app#5"]`; string(out) != want {
		t.Errorf("marshal = %s, want %s", out, want)
	}
	if err := json.Unmarshal([]byte(`"github:github.com/app#5"`), new(Key)); err == nil {
		t.Error("unmarshal accepted an invalid key")
	}
	if _, err := json.Marshal(Key{}); err == nil {
		t.Error("marshal accepted the zero key")
	}
}

func TestKeyIsZero(t *testing.T) {
	if !(Key{}).IsZero() {
		t.Error("zero key is not zero")
	}
	if (Key{Number: 1}).IsZero() {
		t.Error("non-zero key is zero")
	}
}

func TestKeySame(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"github:github.com/Acme/Lib#3", "github:github.com/acme/lib#3", true},
		{"github:github.com/acme/lib#3", "github:github.com/acme/lib#4", false},
		{"github:github.com/acme/lib#3", "github:github.example.com/acme/lib#3", false},
		{"github:github.com/acme/lib#3", "gitlab:github.com/acme/lib#3", false},
		{"gitlab:gitlab.com/Grp/Sub/App#1", "gitlab:gitlab.com/grp/sub/app#1", true},
	}
	for _, tt := range tests {
		a, err := ParseKey(tt.a)
		if err != nil {
			t.Fatal(err)
		}
		b, err := ParseKey(tt.b)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.Same(b); got != tt.want || b.Same(a) != tt.want {
			t.Errorf("%s.Same(%s) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

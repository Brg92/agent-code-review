package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

// TestKeysMatchSchema checks that a string parses exactly when it matches
// the crKey pattern of the state schema once its host is lowercased.
func TestKeysMatchSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "schemas", "state.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			CRKey struct {
				Pattern string `json:"pattern"`
			} `json:"crKey"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(schema.Defs.CRKey.Pattern)
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
	}
	for _, in := range inputs {
		k, err := ParseKey(in)
		if err == nil && !pattern.MatchString(k.String()) {
			t.Errorf("ParseKey(%q) formats as %q, which the schema rejects", in, k)
		}
		if err != nil && pattern.MatchString(in) {
			t.Errorf("ParseKey(%q) rejects a key the schema accepts: %v", in, err)
		}
	}
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

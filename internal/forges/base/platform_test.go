package base_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/refs"
)

func key(t *testing.T, s string) core.Key {
	t.Helper()
	k, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSelectPlatform(t *testing.T) {
	gh := key(t, "github:github.com/acme/app#1")
	gl := key(t, "gitlab:gitlab.com/grp/app#1")
	both := []refs.Registry{github.Registry{}, gitlab.Registry{}}
	reversed := []refs.Registry{gitlab.Registry{}, github.Registry{}}
	tests := []struct {
		name       string
		root       string
		from       core.Key
		registries []refs.Registry
		want       string
	}{
		{"github url", "https://github.com/acme/lib/pull/12", gh, both, "github:github.com/acme/lib#12"},
		{"github url with tab and spaces", "  https://github.com/acme/lib/pull/12/files \n", gh, both, "github:github.com/acme/lib#12"},
		{"github enterprise url", "https://git.example.com/acme/lib/pull/3", gh, both, "github:git.example.com/acme/lib#3"},
		{"owner/repo#N", "acme/lib#12", gh, both, "github:github.com/acme/lib#12"},
		{"#N", "#5", gh, both, "github:github.com/acme/app#5"},
		{"github only", "acme/lib#12", gh, []refs.Registry{github.Registry{}}, "github:github.com/acme/lib#12"},
		{"gitlab url, github origin", "https://gitlab.com/grp/sub/lib/-/merge_requests/4", gh, both, "gitlab:gitlab.com/grp/sub/lib#4"},
		{"gitlab url, registries reversed", "https://gitlab.com/grp/sub/lib/-/merge_requests/4", gh, reversed, "gitlab:gitlab.com/grp/sub/lib#4"},
		{"gitlab short form, gitlab origin", "grp/lib!4", gl, both, "gitlab:gitlab.com/grp/lib#4"},
		{"github url, gitlab origin", "https://github.com/acme/lib/pull/12", gl, both, "github:github.com/acme/lib#12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, err := base.SelectPlatform(tt.root, tt.from, tt.registries...)
			if err != nil {
				t.Fatal(err)
			}
			if got := sel.Root.String(); got != tt.want {
				t.Errorf("Root = %s, want %s", got, tt.want)
			}
			if sel.Registry.Forge() != sel.Root.Forge {
				t.Errorf("Registry.Forge() = %s, want the root's %s: one platform per cascade", sel.Registry.Forge(), sel.Root.Forge)
			}
		})
	}
}

func TestSelectedRegistryReadsOnePlatform(t *testing.T) {
	gh := key(t, "github:github.com/acme/app#1")
	sel, err := base.SelectPlatform("https://gitlab.com/grp/lib/-/merge_requests/4", gh, github.Registry{}, gitlab.Registry{})
	if err != nil {
		t.Fatal(err)
	}
	line := "- [ ] !5, acme/other#6 and https://github.com/acme/app/pull/7"
	var got []string
	for _, r := range sel.Registry.Parse(line, sel.Root) {
		got = append(got, r.Key.String())
	}
	want := []string{"gitlab:gitlab.com/grp/lib#5", "github:github.com/acme/app#7"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %v, want %v: short forms of the cascade's platform, links of the others", got, want)
	}
}

func TestSelectPlatformUnknown(t *testing.T) {
	gh := key(t, "github:github.com/acme/app#1")
	tests := []struct {
		name  string
		root  string
		found int
	}{
		{"unknown host form", "https://bitbucket.org/acme/app/pull-requests/3", 0},
		{"no number", "https://github.com/acme/app", 0},
		{"plain text", "the login change", 0},
		{"empty", "", 0},
		{"gitlab short form, github origin", "grp/lib!4", 0},
		{"two references", "acme/a#1 acme/b#2", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := base.SelectPlatform(tt.root, gh, github.Registry{}, gitlab.Registry{})
			var pe *base.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("SelectPlatform(%q) error = %v, want a *PlatformError", tt.root, err)
			}
			if pe.Root != tt.root || len(pe.Found) != tt.found {
				t.Errorf("PlatformError = %+v, want root %q and %d found", pe, tt.root, tt.found)
			}
			if !strings.Contains(err.Error(), "change request") {
				t.Errorf("Error() = %q, want it to say what is wrong", err)
			}
		})
	}
}

func TestSelectPlatformNeedsTheOriginRegistry(t *testing.T) {
	gl := key(t, "gitlab:gitlab.com/grp/app#1")
	_, err := base.SelectPlatform("acme/lib#1", gl, github.Registry{})
	var pe *base.PlatformError
	if err == nil || errors.As(err, &pe) {
		t.Errorf("SelectPlatform without a registry for the origin = %v, want a plain error", err)
	}
}

package gitlab

import (
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
)

var _ refs.Registry = Registry{}

func mustKey(t *testing.T, s string) core.Key {
	t.Helper()
	k, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestRegistryForge(t *testing.T) {
	if got := (Registry{}).Forge(); got != core.GitLab {
		t.Errorf("Forge() = %q", got)
	}
}

func TestParse(t *testing.T) {
	const (
		flat   = "gitlab:gitlab.com/acme/app#7"
		nested = "gitlab:gitlab.com/group/sub/app#7"
		onPrem = "gitlab:git.example.com:8443/group/sub/app#7"
	)
	tests := []struct {
		name, line string
		from       string
		want       []string
	}{
		// URLs.
		{"full URL", "- [ ] https://gitlab.com/acme/lib/-/merge_requests/21", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"nested groups", "https://gitlab.com/group/sub/deep/lib/-/merge_requests/21", flat, []string{"gitlab:gitlab.com/group/sub/deep/lib#21"}},
		{"diffs tab", "https://gitlab.com/acme/lib/-/merge_requests/21/diffs", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"commits tab", "https://gitlab.com/acme/lib/-/merge_requests/21/commits", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"note anchor", "https://gitlab.com/acme/lib/-/merge_requests/21#note_345", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"query", "https://gitlab.com/acme/lib/-/merge_requests/21?tab=diffs&w=1", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"trailing slash", "https://gitlab.com/acme/lib/-/merge_requests/21/", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"http and uppercase host", "http://GitLab.com/acme/lib/-/merge_requests/21", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"another host", "https://git.example.com:8443/acme/lib/-/merge_requests/21", flat, []string{"gitlab:git.example.com:8443/acme/lib#21"}},
		{"markdown link", "- [ ] [lib](https://gitlab.com/acme/lib/-/merge_requests/21)", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"link whose text is a reference", "- [ ] [other!4](https://gitlab.com/acme/lib/-/merge_requests/21)", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"angle brackets", "<https://gitlab.com/acme/lib/-/merge_requests/21>", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"sentence punctuation", "see https://gitlab.com/acme/lib/-/merge_requests/21.", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"URL and reference to the same", "https://gitlab.com/acme/lib/-/merge_requests/21 acme/lib!21", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"single segment project", "https://gitlab.com/lib/-/merge_requests/21", flat, nil},
		{"not a merge request URL", "https://gitlab.com/acme/lib/-/issues/21", flat, nil},
		{"letter after the number", "https://gitlab.com/acme/lib/-/merge_requests/21x", flat, nil},
		{"github URL is not ours", "https://github.com/acme/lib/pull/3", flat, nil},

		// References.
		{"group/sub/project!N", "- [ ] group/sub/lib!21", flat, []string{"gitlab:gitlab.com/group/sub/lib#21"}},
		{"project!N in the namespace of a flat project", "- [ ] lib!21", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"project!N in a nested namespace", "- [ ] lib!21", nested, []string{"gitlab:gitlab.com/group/sub/lib#21"}},
		{"!N in the same project", "- [ ] !21", nested, []string{"gitlab:gitlab.com/group/sub/app#21"}},
		{"!N on the host of the description", "- [ ] !21", onPrem, []string{"gitlab:git.example.com:8443/group/sub/app#21"}},
		{"reference after text", "Depends on acme/lib!21, please", flat, []string{"gitlab:gitlab.com/acme/lib#21"}},
		{"reference in parentheses", "(!21)", flat, []string{"gitlab:gitlab.com/acme/app#21"}},
		{"dots and dashes", "my.org/my-lib.kt!4", flat, []string{"gitlab:gitlab.com/my.org/my-lib.kt#4"}},
		{"two references", "!1 and acme/lib!2", flat, []string{"gitlab:gitlab.com/acme/app#1", "gitlab:gitlab.com/acme/lib#2"}},
		{"same reference twice", "!1 and !1", flat, []string{"gitlab:gitlab.com/acme/app#1"}},
		{"issue reference is not a merge request", "#12", flat, nil},
		{"!0", "!0", flat, nil},
		{"leading zero", "!021", flat, nil},
		{"letter after the number", "!21x", flat, nil},
		{"glued to a path", "a/b/!21", flat, nil},
		{"double bang", "!!21", flat, nil},
		{"exclamation in prose", "Great work!", flat, nil},
		{"github short form is not ours", "acme/lib#3", flat, nil},
		{"empty", "", flat, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, r := range (Registry{}).Parse(tt.line, mustKey(t, tt.from)) {
				got = append(got, r.Key.String())
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestParsePositionsAndURLFlag(t *testing.T) {
	const line = "!2 https://gitlab.com/acme/lib/-/merge_requests/3 acme/other!4"
	got := (Registry{}).Parse(line, mustKey(t, "gitlab:gitlab.com/acme/app#7"))
	want := []refs.Ref{
		{Key: mustKey(t, "gitlab:gitlab.com/acme/app#2"), Pos: 0},
		{Key: mustKey(t, "gitlab:gitlab.com/acme/lib#3"), Pos: 3, URL: true},
		{Key: mustKey(t, "gitlab:gitlab.com/acme/other#4"), Pos: 50},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}

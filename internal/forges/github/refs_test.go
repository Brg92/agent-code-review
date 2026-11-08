package github

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
	if got := (Registry{}).Forge(); got != core.GitHub {
		t.Errorf("Forge() = %q", got)
	}
}

func TestParse(t *testing.T) {
	const (
		app    = "github:github.com/acr-plugin-sandbox/sample-app#7"
		core3  = "github:github.com/acr-plugin-sandbox/core-lib#3"
		feat2  = "github:github.com/acr-plugin-sandbox/feature-lib#2"
		app2   = "github:github.com/acr-plugin-sandbox/sample-app#2"
		ghe    = "github:github.example.com/acr-plugin-sandbox/core-lib#3"
		enterp = "github:github.example.com:8443/acme/lib#9"
	)
	tests := []struct {
		name, line string
		from       string
		want       []string
	}{
		// The S2 table.
		{"full URL", "- [ ] https://github.com/acr-plugin-sandbox/core-lib/pull/3", app, []string{core3}},
		{"owner/repo#N", "- [x] acr-plugin-sandbox/feature-lib#2", app, []string{feat2}},
		{"#N in the same repository", "- [ ] #2", app, []string{app2}},
		{"gitlab URL is not ours", "- [ ] https://gitlab.com/acr-plugin-sandbox/sample-app/-/merge_requests/1", app, nil},
		{"other pull request", "- [ ] acr-plugin-sandbox/core-lib#9999", app, []string{"github:github.com/acr-plugin-sandbox/core-lib#9999"}},

		// URL forms.
		{"files tab", "https://github.com/acr-plugin-sandbox/core-lib/pull/3/files", app, []string{core3}},
		{"commits tab", "https://github.com/acr-plugin-sandbox/core-lib/pull/3/commits", app, []string{core3}},
		{"commit anchor", "https://github.com/acr-plugin-sandbox/core-lib/pull/3/commits/abc123", app, []string{core3}},
		{"fragment", "https://github.com/acr-plugin-sandbox/core-lib/pull/3#issuecomment-123", app, []string{core3}},
		{"diff anchor", "https://github.com/acr-plugin-sandbox/core-lib/pull/3/files#diff-0a1b", app, []string{core3}},
		{"query", "https://github.com/acr-plugin-sandbox/core-lib/pull/3?diff=split&w=1", app, []string{core3}},
		{"trailing slash", "https://github.com/acr-plugin-sandbox/core-lib/pull/3/", app, []string{core3}},
		{"http", "http://github.com/acr-plugin-sandbox/core-lib/pull/3", app, []string{core3}},
		{"uppercase host", "https://GitHub.com/acr-plugin-sandbox/core-lib/pull/3", app, []string{core3}},
		{"issue URL", "https://github.com/acr-plugin-sandbox/core-lib/issues/3", app, []string{core3}},
		{"another host of the same platform", "https://github.example.com/acr-plugin-sandbox/core-lib/pull/3", app, []string{ghe}},
		{"host with a port", "https://github.example.com:8443/acme/lib/pull/9", app, []string{enterp}},
		{"markdown link", "- [ ] [core](https://github.com/acr-plugin-sandbox/core-lib/pull/3)", app, []string{core3}},
		{"markdown link whose text is a reference", "- [ ] [#12](https://github.com/acr-plugin-sandbox/core-lib/pull/3)", app, []string{core3}},
		{"angle brackets", "- [ ] <https://github.com/acr-plugin-sandbox/core-lib/pull/3>", app, []string{core3}},
		{"sentence punctuation", "see https://github.com/acr-plugin-sandbox/core-lib/pull/3.", app, []string{core3}},
		{"in parentheses", "(https://github.com/acr-plugin-sandbox/core-lib/pull/3)", app, []string{core3}},
		{"URL and short form of the same pull request", "https://github.com/acr-plugin-sandbox/core-lib/pull/3 acr-plugin-sandbox/core-lib#3", app, []string{core3}},
		{"URL not a pull request", "https://github.com/acr-plugin-sandbox/core-lib/tree/main", app, nil},
		{"URL with a letter after the number", "https://github.com/acr-plugin-sandbox/core-lib/pull/3x", app, nil},
		{"pull zero", "https://github.com/acr-plugin-sandbox/core-lib/pull/0", app, nil},
		{"pull with leading zero", "https://github.com/acr-plugin-sandbox/core-lib/pull/03", app, nil},
		{"number too large", "https://github.com/acr-plugin-sandbox/core-lib/pull/99999999999999999999999", app, nil},

		// Short forms.
		{"short form after text", "Depends on acr-plugin-sandbox/feature-lib#2, please", app, []string{feat2}},
		{"short form in parentheses", "(acr-plugin-sandbox/feature-lib#2)", app, []string{feat2}},
		{"dots and dashes", "my.org/my-lib.kt#4", app, []string{"github:github.com/my.org/my-lib.kt#4"}},
		{"#N in parentheses", "(#2)", app, []string{app2}},
		{"#N before punctuation", "PR #2: prefetch", app, []string{app2}},
		{"#N at the end", "- [ ] See #2", app, []string{app2}},
		{"#N takes the host of its owner", "- [ ] #3", ghe, []string{"github:github.example.com/acr-plugin-sandbox/core-lib#3"}},
		{"two #N", "- [ ] #1 and #2", app, []string{"github:github.com/acr-plugin-sandbox/sample-app#1", app2}},
		{"same #N twice", "- [ ] #2 and #2", app, []string{app2}},
		{"URL then #N", "https://github.com/acr-plugin-sandbox/core-lib/pull/3 #2", app, []string{core3, app2}},
		{"#N then URL", "#2 https://github.com/acr-plugin-sandbox/core-lib/pull/3", app, []string{app2, core3}},
		{"HTML entity", "&#39;", app, nil},
		{"hex colour", "#ff0000", app, nil},
		{"#N glued to a word", "abc#2", app, nil},
		{"#N with a letter after", "#2x", app, nil},
		{"#0", "#0", app, nil},
		{"#N with leading zero", "#02", app, nil},
		{"three path segments", "x/acr-plugin-sandbox/feature-lib#2", app, nil},
		{"anchor of a path", "docs/readme#2", app, []string{"github:github.com/docs/readme#2"}},
		{"plain text", "nothing to see here", app, nil},
		{"empty", "", app, nil},
		{"gitlab short form is not ours", "group/project!4 and !5", app, nil},
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
	const line = "#2 https://github.com/acme/lib/pull/3 acme/other#4"
	got := (Registry{}).Parse(line, mustKey(t, "github:github.com/acme/app#7"))
	want := []refs.Ref{
		{Key: mustKey(t, "github:github.com/acme/app#2"), Pos: 0},
		{Key: mustKey(t, "github:github.com/acme/lib#3"), Pos: 3, URL: true},
		{Key: mustKey(t, "github:github.com/acme/other#4"), Pos: 38},
	}
	if len(got) != len(want) {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			found = found || g == w
		}
		if !found {
			t.Errorf("Parse() = %+v, missing %+v", got, w)
		}
	}
}

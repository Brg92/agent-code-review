package core

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"\t  GlobalScope.launch   {  \r", "GlobalScope.launch {"},
		{"a  b", "a b"},
	}
	for _, tt := range tests {
		if got := NormalizeLine(tt.in); got != tt.want {
			t.Errorf("NormalizeLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindingFingerprint(t *testing.T) {
	const path = "app/src/main/java/com/example/sample/data/SessionRepository.kt"
	tests := []struct {
		text       string
		occurrence int
		want       string
	}{
		// SHA-256 of "GlobalScope.launch {\n0", and of the same text with
		// occurrence 1.
		{"        GlobalScope.launch {", 0, "acr:v1:global-scope:" + path + ":1b7430f5f46c"},
		{"  GlobalScope.launch   {  ", 1, "acr:v1:global-scope:" + path + ":9617645d09ba"},
		{"", 0, "acr:v1:global-scope:" + path + ":c7757c0896cb"},
	}
	for _, tt := range tests {
		if got := FindingFingerprint("global-scope", path, tt.text, tt.occurrence); got != tt.want {
			t.Errorf("FindingFingerprint(%q, %d) = %s, want %s", tt.text, tt.occurrence, got, tt.want)
		}
	}
	if FindingFingerprint("global-scope", path, "x", 0) == FindingFingerprint("empty-catch", path, "x", 0) {
		t.Error("two rules give the same fingerprint")
	}
}

func TestOtherFingerprints(t *testing.T) {
	if got, want := OKFingerprint("76ae5f13ca1575b65fe1567c471e8a7b51a00af4"), "acr:v1:ok:76ae5f13"; got != want {
		t.Errorf("OKFingerprint = %s, want %s", got, want)
	}
	cr, err := ParseKey("github:github.com/acr-plugin-sandbox/sample-app#2")
	if err != nil {
		t.Fatal(err)
	}
	for sev, want := range map[Severity]string{
		Blocker: "acr:v1:issue:acr-plugin-sandbox/sample-app#2:blocker",
		Major:   "acr:v1:issue:acr-plugin-sandbox/sample-app#2:major",
		Minor:   "acr:v1:issue:acr-plugin-sandbox/sample-app#2:minor",
	} {
		if got, err := IssueFingerprint(cr, sev); err != nil || got != want {
			t.Errorf("IssueFingerprint(%s) = %s, %v, want %s", sev, got, err, want)
		}
	}
	for _, sev := range []Severity{Info, "", "CRITICAL"} {
		if got, err := IssueFingerprint(cr, sev); err == nil {
			t.Errorf("IssueFingerprint(%q) = %s, want an error: issues are only BLOCKER, MAJOR or MINOR", sev, got)
		}
	}
	finding := "acr:v1:global-scope:app/Repo.kt:1b7430f5f46c"
	if got, want := ItemFingerprint(finding), "acr:item:global-scope:app/Repo.kt:1b7430f5f46c"; got != want {
		t.Errorf("ItemFingerprint = %s, want %s", got, want)
	}
	if got, want := Embed(finding), "<!-- "+finding+" -->"; got != want {
		t.Errorf("Embed = %s, want %s", got, want)
	}
}

func TestSeverity(t *testing.T) {
	for _, s := range []Severity{Blocker, Major, Minor, Info} {
		if !s.Valid() {
			t.Errorf("%s is not valid", s)
		}
	}
	for _, s := range []Severity{"", "blocker", "CRITICAL"} {
		if s.Valid() {
			t.Errorf("%q is valid", s)
		}
	}
}

// hostilePaths are paths that would end, escape or corrupt the HTML comment
// a fingerprint is published in, or that look like the escapes themselves.
var hostilePaths = []string{
	"a-->b", "a>b", "a<b", "a<!-- b", "a--!>b", "x-->", "<", ">", "-->", "->", "--",
	"a\nb", "a\tb", "a\rb", "a\x00b", "a\x1fb", "a\x7fb", "a\x0cb",
	"a%25b", "a%3Eb", "a%3Cb", "a%0Ab", "a%7Fb", "%25", "%3E", "%", "a%", "%%3E", "%3%3E", "%2>", "%>", "%\n",
	"a\xffb", "-->\n<!--",
}

func TestEmbedKeepsOrdinaryFingerprints(t *testing.T) {
	paths := []string{
		"app/src/main/Repo.kt", "app/src/main/java/a/My File.kt", "docs/a--b.md", "a-b/c_d.e",
		"héllo/日本語.kt", "100%.kt", "my%20file.txt", "a%zz", "a%3c", "a%3e", "a%2", "%41", "a%e9b", "x:y/z",
		"a/-/b",
	}
	for _, path := range paths {
		fp := FindingFingerprint("global-scope", path, "x := 1", 0)
		if got, want := Embed(fp), "<!-- "+fp+" -->"; got != want {
			t.Errorf("Embed changed the fingerprint of %q: got %q, want %q", path, got, want)
		}
		item := ItemFingerprint(fp)
		if got, want := Embed(item), "<!-- "+item+" -->"; got != want {
			t.Errorf("Embed changed the item fingerprint of %q: got %q, want %q", path, got, want)
		}
	}
}

func TestEmbedRoundTripsHostilePaths(t *testing.T) {
	for _, path := range hostilePaths {
		t.Run(fmt.Sprintf("%q", path), func(t *testing.T) {
			fp := FindingFingerprint("global-scope", path, "x := 1", 0)
			item := ItemFingerprint(fp)
			body := "text before " + Embed(fp) + "\n- [ ] item " + Embed(item) + "\nafter"
			comment := Embed(fp)
			if strings.Count(comment, "-->") != 1 || !strings.HasSuffix(comment, "-->") ||
				strings.Count(comment, "<") != 1 || strings.Count(comment, ">") != 1 {
				t.Errorf("embedded comment %q is not one well-formed HTML comment", comment)
			}
			for _, r := range comment {
				if r < 0x20 || r == 0x7f {
					t.Errorf("embedded comment %q holds the control character %q", comment, r)
				}
			}
			got := ParseFingerprints([]ReviewNote{{Author: "reviewer", Trusted: true, Body: body}}, "author")
			want := []string{
				"finding " + fp,
				"item " + item + " -> " + fp,
			}
			if d := describe(got); !reflect.DeepEqual(d, want) {
				t.Errorf("read back\n%q\nwant\n%q", d, want)
			}
		})
	}
}

// TestEscapeRoundTrip checks that every string over a small alphabet that
// holds the escapes and what they stand for comes back unchanged.
func TestEscapeRoundTrip(t *testing.T) {
	alphabet := []string{"%", "2", "5", "3", "C", "E", "F", "7", "0", "A", ">", "<", "-", "\n", "a", "c"}
	var walk func(prefix string, depth int)
	walk = func(prefix string, depth int) {
		enc := escapeComment(prefix)
		if strings.ContainsAny(enc, "<>\n") {
			t.Fatalf("escapeComment(%q) = %q still holds a reserved character", prefix, enc)
		}
		if dec := unescapeComment(enc); dec != prefix {
			t.Fatalf("unescapeComment(escapeComment(%q)) = %q (escaped %q)", prefix, dec, enc)
		}
		if depth == 0 {
			return
		}
		for _, a := range alphabet {
			walk(prefix+a, depth-1)
		}
	}
	walk("", 4)
}

func TestUnescapeLeavesOtherSequences(t *testing.T) {
	for _, s := range []string{"a%20b", "a%3cb", "a%41", "a%", "a%2", "a%zzb", "%%", "a%e9b"} {
		if got := unescapeComment(s); got != s {
			t.Errorf("unescapeComment(%q) = %q, want it unchanged", s, got)
		}
	}
}

package core

import (
	"fmt"
	"reflect"
	"testing"
)

func describe(fps []NoteFingerprint) []string {
	var out []string
	for _, f := range fps {
		s := fmt.Sprintf("%s %s", f.Kind, f.Value)
		if f.Legacy {
			s += " legacy"
		}
		if f.Finding != "" {
			s += " -> " + f.Finding
		}
		out = append(out, s)
	}
	return out
}

func TestParseFingerprints(t *testing.T) {
	const finding = "acr:v1:global-scope:app/src/main/Repo.kt:1b7430f5f46c"
	notes := []ReviewNote{
		{Author: "reviewer", Trusted: true, Body: "**MAJOR** `global-scope`: launches in GlobalScope.\n\n" + Embed(finding)},
		{Author: "reviewer", Trusted: true, Body: "No blocking issues.<!--acr:v1:ok:76ae5f13-->"},
		{Author: "reviewer", Trusted: true, Body: "## MAJOR findings\n- [ ] **GlobalScope** " + Embed(ItemFingerprint(finding)) + "\n\n<!--  acr:v1:issue:acr-plugin-sandbox/sample-app#2:major  -->"},
		{Author: "reviewer", Trusted: true, Body: "<!-- mr-review:v1:binding-not-cleared:app/src/main/java/a/HomeFragment.kt:42 -->"},
		{Author: "reviewer", Trusted: true, Body: "<!-- mr-review:v1:ok:0a1b2c3d --> <!-- mr-review:v1:issue:42!1234:minor -->"},
		{Author: "reviewer", Trusted: true, Body: "- [x] **LiveData** <!-- mr-review:item:new-live-data:app/src/main/java/a/Vm.kt:19 -->"},
		// Not fingerprints.
		{Author: "reviewer", Trusted: true, Body: "<!-- acr:v1:ok:xyz --> <!-- acr:v1:global-scope:app/Repo.kt:42 --> <!-- acr:v2:x --> <!-- just a comment -->"},
		{Author: "reviewer", Trusted: true, Body: "<!-- acr:v1:issue:sample-app#2:info --> <!-- acr:v1:Global:app/Repo.kt:1b7430f5f46c -->"},
		// The same fingerprint again is listed once.
		{Author: "other-reviewer", Trusted: true, Body: Embed(finding)},
	}
	got := describe(ParseFingerprints(notes, "author"))
	want := []string{
		"finding " + finding,
		"ok acr:v1:ok:76ae5f13",
		"item acr:item:global-scope:app/src/main/Repo.kt:1b7430f5f46c -> " + finding,
		"issue acr:v1:issue:acr-plugin-sandbox/sample-app#2:major",
		"finding mr-review:v1:binding-not-cleared:app/src/main/java/a/HomeFragment.kt:42 legacy",
		"ok mr-review:v1:ok:0a1b2c3d legacy",
		"issue mr-review:v1:issue:42!1234:minor legacy",
		"item mr-review:item:new-live-data:app/src/main/java/a/Vm.kt:19 legacy -> mr-review:v1:new-live-data:app/src/main/java/a/Vm.kt:19",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// TestParseFingerprintsIgnoresTheAuthor checks the legacy planted_by_author
// rule: the author of a change request could plant a fingerprint to hide
// a finding, so their notes do not count.
func TestParseFingerprintsIgnoresTheAuthor(t *testing.T) {
	const finding = "acr:v1:no-double-bang:app/Store.kt:7726b8854d31"
	notes := []ReviewNote{
		{Author: "Sandbox-Author", Trusted: true, Body: "Already fixed " + Embed(finding) + " " + Embed(OKFingerprint("76ae5f13ca1575b65fe1567c471e8a7b51a00af4"))},
		{Author: "sandbox-author", Trusted: true, Body: "<!-- mr-review:v1:no-double-bang:app/Store.kt:10 -->"},
		{Author: "reviewer", Trusted: true, Body: "Reply without a fingerprint."},
	}
	if got := ParseFingerprints(notes, "sandbox-author"); len(got) != 0 {
		t.Errorf("fingerprints of the author's notes count: %q", describe(got))
	}
	if got := ParseFingerprints(notes, ""); len(got) != 3 {
		t.Errorf("without a known author, %d fingerprints, want 3", len(got))
	}
}

// TestParseFingerprintsIgnoresDotSegments checks that an issue fingerprint
// of a project that is not a valid key is not a fingerprint.
func TestParseFingerprintsIgnoresDotSegments(t *testing.T) {
	notes := []ReviewNote{{Author: "reviewer", Trusted: true, Body: "<!-- acr:v1:issue:acme/../x#2:major --> <!-- acr:v1:issue:./x#2:major --> <!-- acr:v1:issue:acme/.github#2:major -->"}}
	got := describe(ParseFingerprints(notes, ""))
	if want := []string{"issue acr:v1:issue:acme/.github#2:major"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestParseFingerprintsCountsOnlyTrustedNotes is the trust rule: a
// fingerprint counts only when the provider says its author is trusted,
// and never when it was written by the change request author, trusted or
// not. Fingerprints of other accounts are read, but not trusted.
func TestParseFingerprintsCountsOnlyTrustedNotes(t *testing.T) {
	const (
		mine     = "acr:v1:global-scope:app/A.kt:111111111111"
		theirs   = "acr:v1:global-scope:app/B.kt:222222222222"
		authors  = "acr:v1:global-scope:app/C.kt:333333333333"
		legacy   = "mr-review:v1:global-scope:app/D.kt:4"
		shared   = "acr:v1:ok:76ae5f13"
		sharedBy = "<!-- acr:v1:ok:76ae5f13 -->"
	)
	notes := []ReviewNote{
		{Author: "maintainer", Trusted: true, Body: Embed(mine) + " " + Embed(legacy)},
		{Author: "stranger", Trusted: false, Body: Embed(theirs) + " " + sharedBy},
		{Author: "Sandbox-Author", Trusted: true, Body: Embed(authors)},
		{Author: "maintainer", Trusted: true, Body: sharedBy},
	}
	if got, want := describe(ParseFingerprints(notes, "sandbox-author")), []string{"finding " + mine, "finding " + legacy + " legacy", "ok " + shared}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseFingerprints = %q, want %q", got, want)
	}
	all := ReadFingerprints(notes, "sandbox-author")
	type row struct {
		value   string
		trusted bool
	}
	var got []row
	for _, f := range all {
		got = append(got, row{f.Value, f.Trusted})
	}
	want := []row{{mine, true}, {legacy, true}, {theirs, false}, {shared, true}, {authors, false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadFingerprints = %v, want %v: untrusted ones are listed, the one a trusted note repeats is trusted", got, want)
	}
}

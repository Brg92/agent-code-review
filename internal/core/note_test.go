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
		{Author: "reviewer", Body: "**MAJOR** `global-scope`: launches in GlobalScope.\n\n" + Embed(finding)},
		{Author: "reviewer", Body: "No blocking issues.<!--acr:v1:ok:76ae5f13-->"},
		{Author: "reviewer", Body: "## MAJOR findings\n- [ ] **GlobalScope** " + Embed(ItemFingerprint(finding)) + "\n\n<!--  acr:v1:issue:acr-plugin-sandbox/sample-app#2:major  -->"},
		{Author: "reviewer", Body: "<!-- mr-review:v1:binding-not-cleared:app/src/main/java/a/HomeFragment.kt:42 -->"},
		{Author: "reviewer", Body: "<!-- mr-review:v1:ok:0a1b2c3d --> <!-- mr-review:v1:issue:42!1234:minor -->"},
		{Author: "reviewer", Body: "- [x] **LiveData** <!-- mr-review:item:new-live-data:app/src/main/java/a/Vm.kt:19 -->"},
		// Not fingerprints.
		{Author: "reviewer", Body: "<!-- acr:v1:ok:xyz --> <!-- acr:v1:global-scope:app/Repo.kt:42 --> <!-- acr:v2:x --> <!-- just a comment -->"},
		{Author: "reviewer", Body: "<!-- acr:v1:issue:sample-app#2:info --> <!-- acr:v1:Global:app/Repo.kt:1b7430f5f46c -->"},
		// The same fingerprint again is listed once.
		{Author: "other-reviewer", Body: Embed(finding)},
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
		{Author: "Sandbox-Author", Body: "Already fixed " + Embed(finding) + " " + Embed(OKFingerprint("76ae5f13ca1575b65fe1567c471e8a7b51a00af4"))},
		{Author: "sandbox-author", Body: "<!-- mr-review:v1:no-double-bang:app/Store.kt:10 -->"},
		{Author: "reviewer", Body: "Reply without a fingerprint."},
	}
	if got := ParseFingerprints(notes, "sandbox-author"); len(got) != 0 {
		t.Errorf("fingerprints of the author's notes count: %q", describe(got))
	}
	if got := ParseFingerprints(notes, ""); len(got) != 3 {
		t.Errorf("without a known author, %d fingerprints, want 3", len(got))
	}
}

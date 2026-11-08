package core

import "testing"

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

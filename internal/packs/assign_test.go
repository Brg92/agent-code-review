package packs

import (
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

func TestAssign(t *testing.T) {
	set := loadTestSet(t)
	mobile := set.Packs[0]
	tests := []struct {
		name, path     string
		pack, language string
		test           bool
		reason, detail string
	}{
		{"kotlin", "app/src/main/A.kt", "mobile", "kotlin", false, "", ""},
		{"kotlin script", "app/build.gradle.kts", "mobile", "kotlin", false, "", ""},
		{"java test", "app/src/test/java/ATest.java", "mobile", "java", true, "", ""},
		{"noise", "app/build/generated/B.kt", "", "", false, ReasonNoise, "mobile"},
		{"noise file", "gradle/verification-metadata.xml", "", "", false, ReasonNoise, "mobile"},
		{"a stack without a pack", "iosApp/ContentView.swift", "", "", false, ReasonUnsupportedLanguage, "no pack covers .swift files"},
		{"a pack that is not active", "web/main.js", "", "", false, ReasonUnsupportedLanguage, "the web pack covers .js files but does not apply to this repository"},
		{"no extension", "gradlew", "", "", false, ReasonUnsupportedLanguage, "no pack covers files without an extension"},
		{"hidden file", "app/.editorconfig", "", "", false, ReasonUnsupportedLanguage, "no pack covers files without an extension"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := set.Assign([]*Pack{mobile}, tt.path)
			if a.Path != tt.path || a.Test != tt.test {
				t.Errorf("assignment %+v", a)
			}
			if tt.reason != "" {
				want := rules.NotVerified{Path: tt.path, Reason: tt.reason, Detail: tt.detail}
				if a.Pack != nil || a.NotVerified == nil || *a.NotVerified != want {
					t.Errorf("not verified = %+v, want %+v (pack %v)", a.NotVerified, want, a.Pack)
				}
				return
			}
			if a.NotVerified != nil || a.Pack == nil || a.Pack.ID != tt.pack || a.Language == nil || a.Language.ID != tt.language {
				t.Errorf("assignment %+v, want pack %s language %s", a, tt.pack, tt.language)
			}
		})
	}
}

func TestAssignFirstActivePackWins(t *testing.T) {
	set := loadTestSet(t)
	web := set.Packs[1]
	clone := *web
	clone.ID = "web-clone"
	a := set.Assign([]*Pack{&clone, web}, "main.js")
	if a.Pack != &clone {
		t.Errorf("assigned to %v, want the first active pack", a.Pack)
	}
}

func TestAssignmentFile(t *testing.T) {
	set := loadTestSet(t)
	a := set.Assign(set.Packs[:1], "app/src/test/ATest.kt")
	f := a.File("val x = 1\n", map[int]bool{1: true})
	if f.Path != a.Path || f.Language != "kotlin" || f.Table != a.Language.Table || !f.Test || !f.Added[1] || f.Source != "val x = 1\n" {
		t.Errorf("file = %+v", f)
	}
}

func TestAssignmentDimensionsAndTestFilesOfAnyActivePack(t *testing.T) {
	kotlin := Language{ID: "kotlin", Extensions: []string{".kt"}}
	android := &Pack{ID: "android", Languages: []Language{kotlin}, TestFiles: []rules.Glob{glob(t, "**/src/test/**")},
		FileDimensions: platformPack(t, "x", 0, "android").FileDimensions}
	kmp := platformPack(t, "kmp", 10, "common", "**/src/commonMain/**", "common", "**/src/androidMain/**", "android")
	kmp.Languages = []Language{kotlin}
	kmp.TestFiles = []rules.Glob{glob(t, "**/src/commonTest/**")}
	active := []*Pack{kmp, android}
	set := &Set{Packs: active}
	tests := []struct {
		path     string
		platform string
		test     bool
	}{
		{"shared/src/commonMain/A.kt", "common", false},
		{"shared/src/androidMain/A.kt", "android", false},
		{"shared/src/commonTest/ATest.kt", "common", true},
		{"androidApp/src/test/ATest.kt", "common", true},
	}
	for _, tt := range tests {
		a := set.Assign(active, tt.path)
		if a.Pack != kmp || a.Dimensions["platform"] != tt.platform || a.Test != tt.test {
			t.Errorf("%s: pack %v, dimensions %v, test %v", tt.path, a.Pack, a.Dimensions, a.Test)
		}
	}
	if a := set.Assign(active, "iosApp/ContentView.swift"); a.Dimensions != nil {
		t.Errorf("a file not verified has no dimensions: %v", a.Dimensions)
	}
}

func TestAssignmentContext(t *testing.T) {
	a := Assignment{Dimensions: map[string]string{"platform": "ios"}}
	repo := []DimensionValue{{ID: "ui-toolkit", Value: "compose"}}
	got := a.Context("library", repo)
	if got.Profile != "library" || got.Dimensions["ui-toolkit"] != "compose" || got.Dimensions["platform"] != "ios" || len(got.Dimensions) != 2 {
		t.Errorf("context = %+v", got)
	}
}

func TestActiveRules(t *testing.T) {
	set := loadTestSet(t)
	core := rules.Core().Rules
	got := set.ActiveRules(set.Packs[:1])
	want := len(core) + len(set.Packs[0].Rules.Rules)
	if len(got) != want || got[0] != core[0] || got[len(core)] != set.Packs[0].Rules.Rules[0] {
		t.Errorf("%d rules, want %d: the core rules, then those of the active pack only", len(got), want)
	}
	if got := set.ActiveRules(nil); len(got) != len(core) {
		t.Errorf("without active packs: %d rules, want the %d core rules", len(got), len(core))
	}
}

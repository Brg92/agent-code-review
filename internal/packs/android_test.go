package packs

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

var packsDir = filepath.Join("..", "..", "packs")

// checklistItem is an item of the legacy checklists: its section, its
// severity there, and the kind of checklist it was in ("" for the common
// one, then views, compose or library).
type checklistItem struct{ group, id, severity, kind string }

func loadAndroid(t *testing.T) *Pack {
	t.Helper()
	p, err := LoadPack(filepath.Join(packsDir, "android"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAndroidPackLoads(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	p := set.Packs[0]
	if p.ID != "android" || !reflect.DeepEqual(p.Profiles, []string{"app", "library"}) || p.DefaultProfile != "app" {
		t.Errorf("pack = %+v", p)
	}
	var langs []string
	for _, l := range p.Languages {
		langs = append(langs, l.ID+" "+strings.Join(l.Extensions, ","))
	}
	want := []string{"kotlin .kt,.kts", "java .java", "groovy .gradle", "toml .toml", "properties .properties"}
	if !reflect.DeepEqual(langs, want) {
		t.Errorf("languages %q, want %q", langs, want)
	}
}

func TestAndroidAssignsFiles(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	active := set.Packs
	tests := []struct {
		path, language string
		test           bool
		notVerified    string
	}{
		{"app/src/main/java/a/A.kt", "kotlin", false, ""},
		{"app/build.gradle.kts", "kotlin", false, ""},
		{"app/build.gradle", "groovy", false, ""},
		{"gradle/libs.versions.toml", "toml", false, ""},
		{"gradle.properties", "properties", false, ""},
		{"app/src/test/java/a/ATest.kt", "kotlin", true, ""},
		{"app/src/androidTest/java/a/AIT.java", "java", true, ""},
		{"app/src/main/java/a/Latest.kt", "kotlin", false, ""},
		{"app/build/generated/A.kt", "", false, ReasonNoise},
		{"gradle/verification-metadata.xml", "", false, ReasonNoise},
		{"app/src/main/res/layout/a.xml", "", false, ReasonUnsupportedLanguage},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			a := set.Assign(active, tt.path)
			switch {
			case tt.notVerified != "":
				if a.NotVerified == nil || a.NotVerified.Reason != tt.notVerified {
					t.Errorf("assignment = %+v, want not verified as %s", a, tt.notVerified)
				}
			case a.NotVerified != nil || a.Language.ID != tt.language || a.Test != tt.test:
				t.Errorf("assignment = %+v, want %s (test: %v)", a, tt.language, tt.test)
			}
		})
	}
}

// TestAndroidChecklistIsInTheCatalog keeps the catalog in step with the
// legacy checklists: every item is a rule with its section and, unless
// it needs judgement, its severity, and the catalog has nothing else.
func TestAndroidChecklistIsInTheCatalog(t *testing.T) {
	p := loadAndroid(t)
	byID := map[string]*rules.Rule{}
	for _, r := range p.Rules.Rules {
		byID[r.ID] = r
	}
	when := map[string]map[string][]string{
		"":        nil,
		"views":   {"ui-toolkit": {"views", "both"}},
		"compose": {"ui-toolkit": {"compose", "both"}},
		"library": {"profile": {"library"}},
	}
	seen := map[string]bool{}
	for _, it := range androidChecklist {
		r := byID[it.id]
		if r == nil {
			t.Errorf("%s: not in the catalog", it.id)
			continue
		}
		seen[it.id] = true
		severities := strings.Split(it.severity, "/")
		if r.Severity != severities[len(severities)-1] || r.NeedsJudgement != (len(severities) == 2) {
			t.Errorf("%s: severity %s (judgement %v), legacy %s", it.id, r.Severity, r.NeedsJudgement, it.severity)
		}
		if r.Group != it.group {
			t.Errorf("%s: group %q, legacy %q", it.id, r.Group, it.group)
		}
		if !reflect.DeepEqual(r.When, when[it.kind]) {
			t.Errorf("%s: when %v, want %v", it.id, r.When, when[it.kind])
		}
	}
	for _, r := range p.Rules.Rules {
		if !seen[r.ID] {
			t.Errorf("%s: in the catalog but in no legacy checklist", r.ID)
		}
	}
	if len(androidChecklist) != len(p.Rules.Rules) {
		t.Errorf("%d checklist items, %d rules", len(androidChecklist), len(p.Rules.Rules))
	}
}

// repoOf returns a synthetic repository holding the given files.
func repoOf(files map[string]string) *fakeRepo { return &fakeRepo{files: files} }

func TestAndroidActivation(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"android plugin in kotlin script", map[string]string{"app/build.gradle.kts": `plugins { id("com.android.application") }`}, "android"},
		{"android plugin in groovy script", map[string]string{"app/build.gradle": `apply plugin: 'com.android.library'`}, "android"},
		{"android manifest", map[string]string{"app/src/main/AndroidManifest.xml": "<manifest/>"}, "android"},
		{"a jvm project", map[string]string{"build.gradle.kts": `plugins { kotlin("jvm") }`}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repoOf(tt.files)
			active, err := set.Activate(repo.paths(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(active); got != tt.want {
				t.Errorf("active = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAndroidProfileDetection(t *testing.T) {
	p := loadAndroid(t)
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"maven-publish in a kotlin script", map[string]string{"lib/build.gradle.kts": `plugins { id("maven-publish") }`}, "library"},
		{"maven-publish in a groovy script", map[string]string{"lib/build.gradle": `apply plugin: 'maven-publish'`}, "library"},
		{"publishing block", map[string]string{"lib/build.gradle.kts": "publishing {\n}"}, "library"},
		{"publishing block in groovy", map[string]string{"lib/build.gradle": "publishing {\n}"}, "library"},
		{"an application", map[string]string{"app/build.gradle.kts": `plugins { id("com.android.application") }`}, "app"},
		{"nothing to tell", map[string]string{}, "app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repoOf(tt.files)
			got, err := ResolveProfile([]*Pack{p}, ProfileInput{}, repo.paths(), repo)
			if err != nil || got.Name != tt.want {
				t.Errorf("profile = %+v, %v, want %s", got, err, tt.want)
			}
		})
	}
	repo := repoOf(map[string]string{"app/build.gradle.kts": `plugins { id("com.android.application") }`})
	got, err := ResolveProfile([]*Pack{p}, ProfileInput{DependentCR: "github:github.com/o/app#1"}, repo.paths(), repo)
	if err != nil || got.Name != "library" || got.Reason != ProfileFromCascade {
		t.Errorf("a repository another change request depends on: %+v, %v", got, err)
	}
}

func TestAndroidUIToolkitDetection(t *testing.T) {
	p := loadAndroid(t)
	tests := []struct {
		name       string
		files      map[string]string
		configured map[string]string
		want       string
	}{
		{"compose in kotlin script", map[string]string{"app/build.gradle.kts": "buildFeatures { compose = true }"}, nil, "compose"},
		{"compose dependency", map[string]string{"app/build.gradle.kts": `implementation("androidx.compose.ui:ui")`}, nil, "compose"},
		{"compose in groovy", map[string]string{"app/build.gradle": "buildFeatures {\n compose true\n}"}, nil, "compose"},
		{"compose in the version catalog", map[string]string{"gradle/libs.versions.toml": `androidx-compose = { module = "androidx.compose:compose-bom" }`}, nil, "compose"},
		{"view binding", map[string]string{"app/build.gradle.kts": "buildFeatures { viewBinding = true }"}, nil, "views"},
		{"data binding in groovy", map[string]string{"app/build.gradle": "buildFeatures {\n dataBinding true\n}"}, nil, "views"},
		{"both kinds in one script", map[string]string{"app/build.gradle.kts": "viewBinding = true\ncompose = true"}, nil, "both"},
		{"both kinds in two modules", map[string]string{"a/build.gradle.kts": "viewBinding = true", "b/build.gradle": "compose true"}, nil, "both"},
		{"no sign", map[string]string{"app/build.gradle.kts": `plugins { id("com.android.application") }`}, nil, "both"},
		{"an empty repository", map[string]string{}, nil, "both"},
		{"the configuration wins", map[string]string{"app/build.gradle.kts": "compose = true"}, map[string]string{"ui-toolkit": "views"}, "views"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := repoOf(tt.files)
			got, err := ResolveDimensions([]*Pack{p}, tt.configured, repo.paths(), repo)
			if err != nil || len(got) != 1 || got[0].ID != "ui-toolkit" || got[0].Value != tt.want {
				t.Errorf("dimensions = %+v, %v, want ui-toolkit %s", got, err, tt.want)
			}
		})
	}
}

// TestAndroidChecklistFollowsTheRepository checks the conditions of the
// rules: a views-only rule does not apply under compose, a library rule
// does not apply to an app.
func TestAndroidChecklistFollowsTheRepository(t *testing.T) {
	p := loadAndroid(t)
	headings := func(profile, toolkit string) map[string]bool {
		out := map[string]bool{}
		ctx := rules.Context{Profile: profile, Dimensions: map[string]string{"ui-toolkit": toolkit}}
		for _, s := range rules.Checklist(p.Rules.Rules, ctx) {
			out[strings.SplitN(s.Heading, ":", 2)[0]] = true
		}
		return out
	}
	tests := []struct {
		profile, toolkit        string
		views, compose, library bool
	}{
		{"app", "views", true, false, false},
		{"app", "compose", false, true, false},
		{"app", "both", true, true, false},
		{"library", "views", true, false, true},
		{"library", "compose", false, true, true},
		{"library", "both", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.profile+" "+tt.toolkit, func(t *testing.T) {
			got := headings(tt.profile, tt.toolkit)
			if !got["Coroutines and concurrency"] || !got["Tests"] {
				t.Errorf("the common sections are missing: %v", got)
			}
			if got["Views"] != tt.views || got["Compose"] != tt.compose || got["Library"] != tt.library {
				t.Errorf("sections %v, want views %v, compose %v, library %v", got, tt.views, tt.compose, tt.library)
			}
		})
	}
	ctx := rules.Context{Profile: "app", Dimensions: map[string]string{"ui-toolkit": "compose"}}
	for _, r := range rules.Applicable(p.Rules.Rules, ctx) {
		if r.ID == "find-view-by-id" || r.ID == "lateinit-binding" || r.ID == "launch-when-started" {
			t.Errorf("%s applies to a compose app", r.ID)
		}
	}
	ctx = rules.Context{Profile: "app", Dimensions: map[string]string{"ui-toolkit": "views"}}
	var applies []string
	for _, r := range rules.Applicable(p.Rules.Rules, ctx) {
		if r.ID == "find-view-by-id" || r.ID == "state-without-remember" || r.ID == "breaking-signature-change" {
			applies = append(applies, r.ID)
		}
	}
	if !reflect.DeepEqual(applies, []string{"find-view-by-id"}) {
		t.Errorf("rules that apply to a views app among three: %v", applies)
	}
}

package packs

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

func loadKMP(t *testing.T) *Pack {
	t.Helper()
	p, err := LoadPack(filepath.Join(packsDir, "kotlin-multiplatform"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKMPPackLoads(t *testing.T) {
	p := loadKMP(t)
	if p.Priority != 10 || !reflect.DeepEqual(p.Profiles, []string{"library", "app"}) || p.DefaultProfile != "library" {
		t.Errorf("priority %d, profiles %v, default %q", p.Priority, p.Profiles, p.DefaultProfile)
	}
	if len(p.Languages) != 1 || p.Languages[0].ID != "kotlin" || !reflect.DeepEqual(p.Languages[0].Extensions, []string{".kt", ".kts"}) {
		t.Errorf("languages %+v", p.Languages)
	}
	var tests []string
	for _, g := range p.TestFiles {
		tests = append(tests, g.String())
	}
	if want := []string{"**/src/commonTest/**", "**/src/androidUnitTest/**", "**/src/ios*Test/**"}; !reflect.DeepEqual(tests, want) {
		t.Errorf("testFiles %q, want %q", tests, want)
	}
	if len(p.FileDimensions) != 1 || p.FileDimensions[0].ID != "platform" || p.FileDimensions[0].Default != "common" {
		t.Errorf("file dimensions %+v", p.FileDimensions)
	}
}

func TestKMPPlatformOfSourceSets(t *testing.T) {
	p := loadKMP(t)
	tests := map[string]string{
		"shared/src/commonMain/kotlin/A.kt":      "common",
		"shared/src/commonTest/kotlin/ATest.kt":  "common",
		"shared/src/androidMain/kotlin/A.kt":     "android",
		"shared/src/androidUnitTest/kotlin/A.kt": "android",
		"shared/src/iosMain/kotlin/A.kt":         "ios",
		"shared/src/iosArm64Main/kotlin/A.kt":    "ios",
		"shared/src/iosSimulatorArm64Test/A.kt":  "ios",
		"src/commonMain/kotlin/A.kt":             "common",
		"shared/src/jvmMain/kotlin/A.kt":         "common",
		"shared/build.gradle.kts":                "common",
		"androidApp/src/main/java/a/MainView.kt": "common",
	}
	for path, want := range tests {
		if got := ResolveFileDimensions([]*Pack{p}, path)["platform"]; got != want {
			t.Errorf("%s: platform %q, want %q", path, got, want)
		}
	}
}

func TestKMPRules(t *testing.T) {
	p := loadKMP(t)
	want := map[string]struct {
		severity string
		judgment bool
		matched  bool
		when     map[string][]string
	}{
		"kmp-jvm-api-in-common":     {"MAJOR", false, true, map[string][]string{"platform": {"common"}}},
		"kmp-missing-throws":        {"MAJOR", true, false, map[string][]string{"platform": {"common"}}},
		"kmp-swift-unfriendly-api":  {"MINOR", true, false, map[string][]string{"platform": {"common"}}},
		"kmp-expect-without-actual": {"BLOCKER", true, true, nil},
		"kmp-source-set-asymmetry":  {"MAJOR", true, true, nil},
		"kmp-missing-tests":         {"MAJOR", false, true, nil},
	}
	if len(p.Rules.Rules) != len(want) {
		t.Errorf("%d rules, want %d", len(p.Rules.Rules), len(want))
	}
	for _, r := range p.Rules.Rules {
		w, ok := want[r.ID]
		if !ok {
			t.Errorf("unexpected rule %s", r.ID)
			continue
		}
		if !strings.HasPrefix(r.ID, "kmp-") || r.Severity != w.severity || r.NeedsJudgement != w.judgment || r.Matched() != w.matched || !reflect.DeepEqual(r.When, w.when) {
			t.Errorf("%s: severity %s, judgement %v, matched %v, when %v", r.ID, r.Severity, r.NeedsJudgement, r.Matched(), r.When)
		}
	}
	for _, id := range []string{"kmp-missing-throws"} {
		for _, r := range p.Rules.Rules {
			if r.ID == id && !strings.Contains(r.Description, "BLOCKER") {
				t.Errorf("%s: the description does not tell it is a BLOCKER when called from Swift", id)
			}
		}
	}
}

func TestKMPLexerIsTheAndroidOne(t *testing.T) {
	a, err := os.ReadFile(filepath.Join(packsDir, "android", "lexer", "kotlin.json"))
	if err != nil {
		t.Fatal(err)
	}
	k, err := os.ReadFile(filepath.Join(packsDir, "kotlin-multiplatform", "lexer", "kotlin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(k) {
		t.Error("lexer/kotlin.json differs from the android pack's: keep the copies identical")
	}
}

func TestKMPTemplates(t *testing.T) {
	p := loadKMP(t)
	var got []string
	for _, path := range p.Templates {
		rel, _ := filepath.Rel(p.Dir, path)
		got = append(got, filepath.ToSlash(rel))
	}
	want := []string{"templates/app/criteria.md", "templates/app/config.json", "templates/library/criteria.md", "templates/library/config.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("templates %q, want %q", got, want)
	}
	for _, profile := range []string{"app", "library"} {
		doc, err := os.ReadFile(filepath.Join(p.Dir, "templates", profile, "criteria.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, heading := range []string{"\n## Project rules\n", "\n## Known exceptions (never reported)\n"} {
			if !strings.Contains("\n"+string(doc), heading) {
				t.Errorf("%s criteria: no %q section", profile, strings.TrimSpace(heading))
			}
		}
		cfg, err := os.ReadFile(filepath.Join(p.Dir, "templates", profile, "config.json"))
		if err != nil || !strings.Contains(string(cfg), `"profile": "`+profile+`"`) || !strings.Contains(string(cfg), `"kotlin-multiplatform"`) {
			t.Errorf("%s config = %s, %v", profile, cfg, err)
		}
	}
}

// review is what the rules say about one file of a repository.
type review struct {
	signals     string // rule ids with a signal, sorted
	applicable  map[string]bool
	notVerified string
}

// reviewFile reviews one file as the pipeline will: it activates the packs
// on the repository, assigns the file, takes the rules whose conditions
// hold for the file and runs them.
func reviewFile(t *testing.T, repoFiles map[string]string, path, source string) review {
	t.Helper()
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{files: repoFiles}
	active, err := set.Activate(repo.paths(), repo)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := ResolveProfile(active, ProfileInput{}, repo.paths(), repo)
	if err != nil {
		t.Fatal(err)
	}
	dims, err := ResolveDimensions(active, nil, repo.paths(), repo)
	if err != nil {
		t.Fatal(err)
	}
	a := set.Assign(active, path)
	if a.NotVerified != nil {
		return review{notVerified: a.NotVerified.Reason}
	}
	applicable := rules.Applicable(set.ActiveRules(active), a.Context(profile.Name, dims))
	res := rules.Run(applicable, a.File(source, nil), rules.Options{})
	r := review{applicable: map[string]bool{}}
	for _, ru := range applicable {
		r.applicable[ru.ID] = true
	}
	seen := map[string]bool{}
	var ids []string
	for _, s := range res.Signals {
		if !seen[s.RuleID] {
			seen[s.RuleID] = true
			ids = append(ids, s.RuleID)
		}
	}
	sort.Strings(ids)
	r.signals = strings.Join(ids, " ")
	return r
}

const (
	kmpBuild = "plugins {\n  kotlin(\"multiplatform\")\n  id(\"com.android.library\")\n}\n"
	// sample uses a JVM API, an Android type, LiveData and a not-null assertion.
	sample = "import java.util.UUID\n\nclass A {\n    val id = UUID.randomUUID()\n    val b = android.os.Bundle()\n    val l = MutableLiveData<Int>()\n    fun f(s: String?) = s!!\n}\n"
)

func kmpRepo() map[string]string {
	return map[string]string{
		"shared/build.gradle.kts":            kmpBuild,
		"shared/src/commonMain/kotlin/A.kt":  "",
		"shared/src/androidMain/kotlin/A.kt": "",
		"shared/src/iosMain/kotlin/A.kt":     "",
		"iosApp/iosApp/ContentView.swift":    "",
	}
}

func TestKMPRepositoryActivatesBothPacks(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{files: kmpRepo()}
	active, err := set.Activate(repo.paths(), repo)
	if err != nil || ids(active) != "kotlin-multiplatform android" {
		t.Errorf("active %q, %v; want the kotlin-multiplatform pack first", ids(active), err)
	}
	plain := &fakeRepo{files: map[string]string{"app/build.gradle.kts": `plugins { id("com.android.application") }`, "app/src/main/A.kt": ""}}
	if active, err = set.Activate(plain.paths(), plain); err != nil || ids(active) != "android" {
		t.Errorf("a plain android repository: active %q, %v", ids(active), err)
	}
	gradle := &fakeRepo{files: map[string]string{"build.gradle.kts": `plugins { id("org.jetbrains.kotlin.multiplatform") }`}}
	if active, err = set.Activate(gradle.paths(), gradle); err != nil || ids(active) != "kotlin-multiplatform" {
		t.Errorf("the plugin id sign: active %q, %v", ids(active), err)
	}
}

func TestKMPFilesGetTheRulesOfTheirPlatform(t *testing.T) {
	// Kotlin-general android rule: no-double-bang. Android platform rule:
	// new-live-data. KMP rule: kmp-jvm-api-in-common.
	tests := []struct {
		name, path, want string
		applies, absent  []string
	}{
		{"common", "shared/src/commonMain/kotlin/A.kt", "kmp-jvm-api-in-common no-double-bang",
			[]string{"no-double-bang", "kmp-jvm-api-in-common", "kmp-missing-throws", "kmp-expect-without-actual", "global-scope"},
			[]string{"new-live-data", "find-view-by-id", "context-leak"}},
		{"android", "shared/src/androidMain/kotlin/A.kt", "new-live-data no-double-bang",
			[]string{"no-double-bang", "new-live-data", "context-leak", "kmp-expect-without-actual", "kmp-source-set-asymmetry"},
			[]string{"kmp-jvm-api-in-common", "kmp-missing-throws", "kmp-swift-unfriendly-api"}},
		{"ios", "shared/src/iosMain/kotlin/A.kt", "no-double-bang",
			[]string{"no-double-bang", "kmp-expect-without-actual"},
			[]string{"new-live-data", "context-leak", "kmp-jvm-api-in-common", "kmp-missing-throws"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := reviewFile(t, kmpRepo(), tt.path, sample)
			if r.signals != tt.want {
				t.Errorf("signals %q, want %q", r.signals, tt.want)
			}
			for _, id := range tt.applies {
				if !r.applicable[id] {
					t.Errorf("%s does not apply", id)
				}
			}
			for _, id := range tt.absent {
				if r.applicable[id] {
					t.Errorf("%s applies", id)
				}
			}
		})
	}
}

func TestKMPJVMAPIRule(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"import and qualified use", "import java.util.UUID\nval a = javax.crypto.Cipher.getInstance(\"x\")\nval b = android.os.Bundle()\n", "kmp-jvm-api-in-common"},
		{"android import", "import android.content.Context\n", "kmp-jvm-api-in-common"},
		{"multiplatform code", "import kotlin.collections.List\nimport androidx.compose.runtime.Composable\nimport kotlinx.coroutines.flow.Flow\nval java = 1\nval android = 2\nval x = javaClass\n", ""},
		{"in a comment and a string", "// java.util.List\nval s = \"android.os.Bundle\"\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for path, want := range map[string]string{
				"shared/src/commonMain/kotlin/A.kt":  tt.want,
				"shared/src/androidMain/kotlin/A.kt": "",
				"shared/src/iosMain/kotlin/A.kt":     "",
			} {
				got := reviewFile(t, kmpRepo(), path, tt.src).signals
				if got != want {
					t.Errorf("%s: signals %q, want %q", path, got, want)
				}
			}
		})
	}
	// One signal per offending line, imports included.
	set, _ := Load(packsDir)
	repo := &fakeRepo{files: kmpRepo()}
	active, _ := set.Activate(repo.paths(), repo)
	a := set.Assign(active, "shared/src/commonMain/kotlin/A.kt")
	var only []*rules.Rule
	for _, r := range set.ActiveRules(active) {
		if r.ID == "kmp-jvm-api-in-common" {
			only = append(only, r)
		}
	}
	res := rules.Run(only, a.File("import java.util.UUID\nval a = 1\nval b = android.os.Bundle()\n", nil), rules.Options{})
	var lines []int
	for _, s := range res.Signals {
		lines = append(lines, s.Line)
	}
	if !reflect.DeepEqual(lines, []int{1, 3}) {
		t.Errorf("lines %v, want [1 3]", lines)
	}
}

func TestPlainAndroidRepositoryIsUnchanged(t *testing.T) {
	repo := map[string]string{"app/build.gradle.kts": `plugins { id("com.android.application") }`, "app/src/main/java/a/A.kt": ""}
	r := reviewFile(t, repo, "app/src/main/java/a/A.kt", sample)
	if r.signals != "new-live-data no-double-bang" {
		t.Errorf("signals %q", r.signals)
	}
	for _, id := range []string{"new-live-data", "context-leak", "find-view-by-id", "global-scope"} {
		if !r.applicable[id] {
			t.Errorf("%s does not apply to an android file", id)
		}
	}
	for id := range r.applicable {
		if strings.HasPrefix(id, "kmp-") {
			t.Errorf("%s applies to a plain android repository", id)
		}
	}
}

func TestSwiftIsNotVerified(t *testing.T) {
	r := reviewFile(t, kmpRepo(), "iosApp/iosApp/ContentView.swift", "import SwiftUI\n")
	if r.notVerified != ReasonUnsupportedLanguage {
		t.Errorf("not verified %q, want %q", r.notVerified, ReasonUnsupportedLanguage)
	}
}

func TestComposeMultiplatformTree(t *testing.T) {
	repo := map[string]string{
		"composeApp/build.gradle.kts":             "plugins {\n  kotlin(\"multiplatform\")\n  id(\"com.android.application\")\n  id(\"org.jetbrains.compose\")\n}\n",
		"composeApp/src/commonMain/kotlin/App.kt": "",
	}
	r := reviewFile(t, repo, "composeApp/src/commonMain/kotlin/App.kt", "val x = 1\n")
	for _, id := range []string{"remember-missing-key", "lazy-list-no-key", "collect-without-lifecycle", "hardcoded-string"} {
		if !r.applicable[id] {
			t.Errorf("compose rule %s does not apply to a Compose Multiplatform common file", id)
		}
	}
	for _, id := range []string{"find-view-by-id", "binding-not-cleared", "context-captured"} {
		if r.applicable[id] {
			t.Errorf("%s applies to a common file", id)
		}
	}
	// A KMP repository without a compose sign falls back to the default of
	// the dimension (both), which Android-only view rules do not escape on
	// an android file.
	if r := reviewFile(t, kmpRepo(), "shared/src/androidMain/kotlin/A.kt", "val x = 1\n"); !r.applicable["find-view-by-id"] {
		t.Error("view rules apply to an android file when the toolkit is not detected")
	}
}

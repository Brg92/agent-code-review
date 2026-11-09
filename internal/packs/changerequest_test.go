package packs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// tree is a repository tree at head held in memory.
type tree []string

func (t tree) List(prefix string) ([]string, error) {
	var out []string
	for _, p := range t {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out, nil
}

// crCase runs the change-request rules of the active packs on synthetic
// changed files and lists the signals of one rule as path:line.
type crCase struct {
	name, rule string
	// files maps a changed path to its source; a source of "-" marks a
	// removed file. Every line of a source is added.
	files    map[string]string
	order    []string
	tree     tree
	variants []string
	want     string
}

func runChangeRequest(t *testing.T, set *Set, active []*Pack, c crCase) []rules.Signal {
	t.Helper()
	cr := rules.ChangeRequest{Tree: c.tree, Variants: c.variants}
	for _, p := range c.order {
		src := c.files[p]
		var f rules.ChangedFile
		if a := set.Assign(active, p); a.NotVerified == nil {
			f.File = a.File("", nil)
		}
		f.Path, f.Source, f.Added = p, src, map[int]bool{}
		if src == "-" {
			f.Source, f.Removed = "", true
		}
		for i := 1; i <= strings.Count(f.Source, "\n"); i++ {
			f.Added[i] = true
		}
		cr.Files = append(cr.Files, f)
	}
	var out []rules.Signal
	for _, s := range rules.RunChangeRequest(set.ActiveRules(active), cr).Signals {
		if s.RuleID == c.rule {
			out = append(out, s)
		}
	}
	return out
}

func sigLines(sigs []rules.Signal) string {
	var out []string
	for _, s := range sigs {
		out = append(out, fmt.Sprintf("%s:%d", s.Path, s.Line))
	}
	return strings.Join(out, " ")
}

// files builds the files of a case from path, source pairs, keeping their
// order.
func files(pairs ...string) (map[string]string, []string) {
	m := map[string]string{}
	var order []string
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
		order = append(order, pairs[i])
	}
	return m, order
}

func crCaseOf(name, rule string, tr tree, variants []string, want string, pairs ...string) crCase {
	m, order := files(pairs...)
	return crCase{name: name, rule: rule, files: m, order: order, tree: tr, variants: variants, want: want}
}

const vm = "app/src/main/java/a/LoginViewModel.kt"

var androidCRCases = []crCase{
	crCaseOf("no test: the class line", "missing-tests", nil, nil, vm+":3",
		vm, "package a\n\nclass LoginViewModel : ViewModel()\n"),
	crCaseOf("java repository without a test", "missing-tests", nil, nil, "data/src/main/java/a/UserRepository.java:1",
		"data/src/main/java/a/UserRepository.java", "public class UserRepository {}\n"),
	crCaseOf("test in the change request", "missing-tests", nil, nil, "",
		vm, "class LoginViewModel\n", "app/src/test/java/a/LoginViewModelTest.kt", "class LoginViewModelTest\n"),
	crCaseOf("test in the tree under src/test", "missing-tests", tree{"app/src/test/java/a/LoginViewModelTest.kt"}, nil, "",
		vm, "class LoginViewModel\n"),
	crCaseOf("java test in the tree under src/androidTest", "missing-tests", tree{"app/src/androidTest/java/a/LoginViewModelTest.java"}, nil, "",
		vm, "class LoginViewModel\n"),
	crCaseOf("removed class", "missing-tests", nil, nil, "", vm, "-"),
	crCaseOf("no suffix", "missing-tests", nil, nil, "", "app/src/main/java/a/LoginScreen.kt", "class LoginScreen\n"),
}

func TestAndroidChangeRequestRules(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	android := []*Pack{set.Packs[0]}
	for _, c := range androidCRCases {
		t.Run(c.name, func(t *testing.T) {
			if got := sigLines(runChangeRequest(t, set, android, c)); got != c.want {
				t.Errorf("%s signals %q, want %q", c.rule, got, c.want)
			}
		})
	}
	// Every suffix of the legacy check needs a test.
	for _, s := range []string{"ViewModel", "Repository", "UseCase", "Mapper", "Manager", "Helper", "Utils", "Storage", "DataSource"} {
		p := "app/src/main/java/a/Login" + s + ".kt"
		c := crCaseOf(s, "missing-tests", nil, nil, "", p, "object Login"+s+"\n")
		sigs := runChangeRequest(t, set, android, c)
		if sigLines(sigs) != p+":1" || sigs[0].Severity != "MAJOR" || sigs[0].NeedsJudgement || sigs[0].Origin != rules.OriginMR {
			t.Errorf("%s: signals %+v, want one MAJOR signal of the change request on line 1", s, sigs)
		}
	}
}

const repo = "shared/src/commonMain/kotlin/a/UserRepository.kt"

var kmpCRCases = []crCase{
	crCaseOf("common repository without a test", "kmp-missing-tests", tree{"shared/src/jvmTest/kotlin/a/UserRepositoryTest.kt"}, nil, repo+":3",
		repo, "package a\n\ninterface UserRepository\n"),
	crCaseOf("test under src/commonTest", "kmp-missing-tests", tree{"shared/src/commonTest/kotlin/a/UserRepositoryTest.kt"}, nil, "",
		repo, "interface UserRepository\n"),
	crCaseOf("test under src/androidUnitTest", "kmp-missing-tests", tree{"shared/src/androidUnitTest/kotlin/a/UserRepositoryTest.kt"}, nil, "",
		repo, "interface UserRepository\n"),
	crCaseOf("test under src/iosTest", "kmp-missing-tests", tree{"shared/src/iosTest/kotlin/a/UserRepositoryTest.kt"}, nil, "",
		repo, "interface UserRepository\n"),
	crCaseOf("test in the change request", "kmp-missing-tests", nil, nil, "",
		repo, "interface UserRepository\n", "shared/src/commonTest/kotlin/a/UserRepositoryTest.kt", "class UserRepositoryTest\n"),
	crCaseOf("platform source", "kmp-missing-tests", nil, nil, "",
		"shared/src/androidMain/kotlin/a/UserRepository.kt", "class UserRepository\n"),
}

const (
	androidClock = "shared/src/androidMain/kotlin/a/Clock.android.kt"
	iosClock     = "shared/src/iosMain/kotlin/a/Clock.ios.kt"
)

var kmpAsymmetryCases = []crCase{
	crCaseOf("android changed, ios counterpart unchanged", "kmp-source-set-asymmetry", tree{androidClock, iosClock}, nil, androidClock+":1",
		androidClock, "actual fun now() = 1L\n"),
	crCaseOf("both platforms changed", "kmp-source-set-asymmetry", tree{androidClock, iosClock}, nil, "",
		androidClock, "actual fun now() = 1L\n", iosClock, "actual fun now() = 1L\n"),
	crCaseOf("android changed, iosArm64Main counterpart unchanged", "kmp-source-set-asymmetry",
		tree{androidClock, "shared/src/iosArm64Main/kotlin/a/Clock.kt"}, nil, androidClock+":1",
		androidClock, "actual fun now() = 1L\n"),
	crCaseOf("iosSimulatorArm64Main changed, iosX64Main counterpart unchanged", "kmp-source-set-asymmetry",
		tree{"shared/src/iosSimulatorArm64Main/kotlin/a/Arch.kt", "shared/src/iosX64Main/kotlin/a/Arch.kt"}, nil,
		"shared/src/iosSimulatorArm64Main/kotlin/a/Arch.kt:1",
		"shared/src/iosSimulatorArm64Main/kotlin/a/Arch.kt", "actual val arch = \"arm64\"\n"),
	crCaseOf("no counterpart", "kmp-source-set-asymmetry", tree{androidClock}, nil, "",
		androidClock, "fun androidOnly() = 1\n"),
	crCaseOf("common code", "kmp-source-set-asymmetry", tree{"shared/src/commonMain/kotlin/a/Clock.kt", iosClock}, nil, "",
		"shared/src/commonMain/kotlin/a/Clock.kt", "expect fun now(): Long\n"),
}

func TestKMPSourceSetAsymmetryNeedsJudgement(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	sigs := runChangeRequest(t, set, []*Pack{packOf(t, set, "kotlin-multiplatform")}, kmpAsymmetryCases[0])
	if len(sigs) != 1 || !sigs[0].NeedsJudgement || sigs[0].Severity != "" {
		t.Errorf("signals %+v, want one candidate for the judgement", sigs)
	}
}

func packOf(t *testing.T, set *Set, id string) *Pack {
	t.Helper()
	for _, p := range set.Packs {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no %s pack", id)
	return nil
}

func TestKMPChangeRequestRules(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	kmp := []*Pack{packOf(t, set, "kotlin-multiplatform")}
	for _, c := range append(append([]crCase{}, kmpCRCases...), kmpAsymmetryCases...) {
		t.Run(c.name, func(t *testing.T) {
			if got := sigLines(runChangeRequest(t, set, kmp, c)); got != c.want {
				t.Errorf("%s signals %q, want %q", c.rule, got, c.want)
			}
		})
	}
	// With both packs active, each rule checks the sources of its own
	// layout only.
	both := []*Pack{packOf(t, set, "kotlin-multiplatform"), packOf(t, set, "android")}
	c := crCaseOf("both packs", "", nil, nil, "", repo, "interface UserRepository\n", vm, "class LoginViewModel\n")
	for rule, want := range map[string]string{"kmp-missing-tests": repo + ":1", "missing-tests": vm + ":1"} {
		c.rule = rule
		if got := sigLines(runChangeRequest(t, set, both, c)); got != want {
			t.Errorf("both packs: %s signals %q, want %q", rule, got, want)
		}
	}
}

// TestChangeRequestRulesAreCovered checks that every change-request rule of
// the packs has a case that signals and one that does not.
func TestChangeRequestRulesAreCovered(t *testing.T) {
	set, err := Load(packsDir)
	if err != nil {
		t.Fatal(err)
	}
	positive, negative := map[string]bool{}, map[string]bool{}
	for _, c := range changeRequestCases() {
		if c.want != "" {
			positive[c.rule] = true
		} else {
			negative[c.rule] = true
		}
	}
	for _, p := range set.Packs {
		for _, r := range p.Rules.Rules {
			if r.OnChangeRequest() && (!positive[r.ID] || !negative[r.ID]) {
				t.Errorf("%s: needs a case that signals and a case that does not", r.ID)
			}
		}
	}
}

func changeRequestCases() []crCase {
	return append(append(append([]crCase{}, androidCRCases...), kmpCRCases...), kmpAsymmetryCases...)
}

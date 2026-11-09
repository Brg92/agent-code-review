package rules

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fakeTree is a repository tree at head held in memory.
type fakeTree []string

func (t fakeTree) List(prefix string) []string {
	var out []string
	for _, p := range t {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}

// changed builds a changed file from a path: a .kt or .java path gets its
// language and lexer table. The source and the added lines are optional.
func changed(t testing.TB, path, src string, added ...int) ChangedFile {
	f := ChangedFile{File: File{Path: path, Source: src, Added: map[int]bool{}}}
	for _, l := range added {
		f.Added[l] = true
	}
	for _, l := range []string{"kotlin", "java"} {
		ext := map[string]string{"kotlin": ".kt", "java": ".java"}[l]
		if strings.HasSuffix(path, ext) {
			f.Language, f.Table = l, packTable(t, l)
		}
	}
	return f
}

func removed(t testing.TB, path string) ChangedFile {
	f := changed(t, path, "")
	f.Removed = true
	return f
}

// signalsAt renders change-request signals as path:line:column.
func signalsAt(sigs []Signal) string {
	var out []string
	for _, s := range sigs {
		out = append(out, fmt.Sprintf("%s:%d:%d", s.Path, s.Line, s.Column))
	}
	return strings.Join(out, " ")
}

const (
	androidMissingTest = `{"scope":"change-request","detector":"missing-test",` +
		`"sources":["**/src/main/**/*.kt","**/src/main/**/*.java"],"nameSuffixes":["ViewModel","Repository","Utils"],` +
		`"moduleSeparator":"src/main/","testRoots":["src/test","src/androidTest"],"testName":"{name}Test","testExtensions":[".kt",".java"]}`
	kmpMissingTest = `{"scope":"change-request","detector":"missing-test",` +
		`"sources":["**/src/commonMain/**/*.kt"],"nameSuffixes":["Repository"],"moduleSeparator":"src/commonMain/",` +
		`"testRoots":["src/commonTest","src/androidUnitTest","src/iosTest"],"testName":"{name}Test","testExtensions":[".kt"]}`
)

const viewModel = "package a\n\n// Not FooViewModel here.\nimport b.FooViewModel\n@HiltViewModel\nclass FooViewModel @Inject constructor() {\n}\n"

func TestMissingTest(t *testing.T) {
	src := "app/src/main/java/a/FooViewModel.kt"
	tests := []struct {
		name  string
		match string
		cr    ChangeRequest
		want  string
	}{
		{"no test anywhere: the declaration line", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel, 6)}}, src + ":6:7"},
		{"no declaration: line 1", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "app/src/main/java/a/StringUtils.kt", "fun String.x() = 1\n", 1)}},
			"app/src/main/java/a/StringUtils.kt:1:1"},
		{"java source", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "lib/src/main/java/a/UserRepository.java", "package a;\n\npublic final class UserRepository {}\n")}},
			"lib/src/main/java/a/UserRepository.java:3:20"},
		{"test added by the change request", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel), changed(t, "app/src/test/java/a/FooViewModelTest.kt", "")}}, ""},
		{"test modified by the change request, in another language", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel), changed(t, "app/src/test/java/a/FooViewModelTest.java", "")}}, ""},
		{"test removed by the change request", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel), removed(t, "app/src/test/java/a/FooViewModelTest.kt")}}, src + ":6:7"},
		{"test in the tree under src/test", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel)}, Tree: fakeTree{"app/src/test/java/a/FooViewModelTest.kt"}}, ""},
		{"test in the tree under src/androidTest", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel)}, Tree: fakeTree{"app/src/androidTest/kotlin/b/FooViewModelTest.java"}}, ""},
		{"test in the tree of another module", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel)}, Tree: fakeTree{"core/src/test/java/a/FooViewModelTest.kt"}}, src + ":6:7"},
		{"test in the tree outside the test roots", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel)}, Tree: fakeTree{"app/src/testFixtures/a/FooViewModelTest.kt", "app/src/main/FooViewModelTest.kt"}},
			src + ":6:7"},
		{"test with another extension in the tree", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, src, viewModel)}, Tree: fakeTree{"app/src/test/a/FooViewModelTest.groovy"}}, src + ":6:7"},
		{"module at the repository root", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "src/main/a/FooViewModel.kt", viewModel)}, Tree: fakeTree{"src/test/a/FooViewModelTest.kt"}}, ""},
		{"removed source", androidMissingTest, ChangeRequest{Files: []ChangedFile{removed(t, src)}}, ""},
		{"name without a suffix", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "app/src/main/java/a/FooScreen.kt", "class FooScreen\n")}}, ""},
		{"suffix inside the name only", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "app/src/main/java/a/ViewModelFactory.kt", "class ViewModelFactory\n")}}, ""},
		{"not a source", androidMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "app/src/test/java/a/FakeRepository.kt", "class FakeRepository\n")}}, ""},
		{"KMP test under src/iosTest", kmpMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "shared/src/commonMain/kotlin/a/UserRepository.kt", "class UserRepository\n")},
				Tree: fakeTree{"shared/src/iosTest/kotlin/a/UserRepositoryTest.kt"}}, ""},
		{"KMP test under src/androidUnitTest", kmpMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "shared/src/commonMain/kotlin/a/UserRepository.kt", "class UserRepository\n")},
				Tree: fakeTree{"shared/src/androidUnitTest/kotlin/a/UserRepositoryTest.kt"}}, ""},
		{"KMP test under src/commonTest", kmpMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "shared/src/commonMain/kotlin/a/UserRepository.kt", "class UserRepository\n")},
				Tree: fakeTree{"shared/src/commonTest/kotlin/a/UserRepositoryTest.kt"}}, ""},
		{"KMP without a test", kmpMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "shared/src/commonMain/kotlin/a/UserRepository.kt", "interface UserRepository\n")},
				Tree: fakeTree{"shared/src/jvmTest/kotlin/a/UserRepositoryTest.kt"}}, "shared/src/commonMain/kotlin/a/UserRepository.kt:1:11"},
		{"KMP rule ignores android sources", kmpMissingTest,
			ChangeRequest{Files: []ChangedFile{changed(t, "app/src/main/a/UserRepository.kt", "class UserRepository\n")}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sigs := RunChangeRequest(oneRule(t, `["kotlin","java"]`, tt.match), tt.cr)
			if got := signalsAt(sigs); got != tt.want {
				t.Errorf("signals %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMissingTestSignal(t *testing.T) {
	sigs := RunChangeRequest(oneRule(t, `["kotlin"]`, androidMissingTest), ChangeRequest{Files: []ChangedFile{
		changed(t, "app/src/main/a/FooViewModel.kt", viewModel),
	}})
	want := []Signal{{RuleID: "r", Severity: "MAJOR", Path: "app/src/main/a/FooViewModel.kt", Line: 6, Column: 7, Origin: OriginMR}}
	if !reflect.DeepEqual(sigs, want) {
		t.Errorf("signals %+v, want %+v", sigs, want)
	}
	// A rule of another language leaves the file alone.
	if sigs := RunChangeRequest(oneRule(t, `["java"]`, androidMissingTest), ChangeRequest{Files: []ChangedFile{
		changed(t, "app/src/main/a/FooViewModel.kt", viewModel),
	}}); len(sigs) != 0 {
		t.Errorf("a java rule signals a kotlin file: %+v", sigs)
	}
}

func TestRunSkipsChangeRequestRules(t *testing.T) {
	rules := oneRule(t, `["kotlin"]`, androidMissingTest)
	f := changed(t, "app/src/main/a/FooViewModel.kt", viewModel).File
	if res := Run(rules, f, Options{}); len(res.Signals) != 0 || len(res.NotVerified) != 0 {
		t.Errorf("Run ran a change-request rule: %+v", res)
	}
}

func TestRunChangeRequestOrder(t *testing.T) {
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"test","rules":[` +
		`{"id":"b","title":"T","severity":"MINOR","description":"D","why":"W","fix":"F","languages":[],"tags":[],"match":` + androidMissingTest + `},` +
		`{"id":"a","title":"T","severity":"MAJOR","description":"D","why":"W","fix":"F","languages":[],"tags":[],"match":` +
		strings.Replace(androidMissingTest, `"{name}Test"`, `"{name}Spec"`, 1) + `},` +
		`{"id":"file","title":"T","severity":"MAJOR","description":"D","why":"W","fix":"F","languages":[],"tags":[],"match":{"detector":"line-length","max":1}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	sigs := RunChangeRequest(c.Rules, ChangeRequest{Files: []ChangedFile{
		changed(t, "z/src/main/BRepository.kt", "class BRepository\n"),
		changed(t, "a/src/main/ARepository.kt", "\nclass ARepository\n"),
	}})
	var got []string
	for _, s := range sigs {
		got = append(got, s.RuleID+"@"+s.Path)
	}
	want := []string{"b@z/src/main/BRepository.kt", "a@z/src/main/BRepository.kt", "b@a/src/main/ARepository.kt", "a@a/src/main/ARepository.kt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order %v, want %v (files in change-request order, then rules in catalog order)", got, want)
	}
}

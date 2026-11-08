package rules

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

var (
	tablesOnce sync.Once
	tables     map[string]*lexer.Table
)

// packTable loads a lexer table of the android pack.
func packTable(t testing.TB, language string) *lexer.Table {
	t.Helper()
	tablesOnce.Do(func() {
		tables = map[string]*lexer.Table{}
		for _, l := range []string{"kotlin", "java"} {
			data := readFile(t, filepath.Join("..", "..", "packs", "android", "lexer", l+".json"))
			tab, err := lexer.LoadTable(data)
			if err != nil {
				t.Fatal(err)
			}
			tables[l] = tab
		}
	})
	return tables[language]
}

// oneRule builds a catalog with one rule from a match and optional extra
// rule fields, and returns its rules.
func oneRule(t testing.TB, languages, match string) []*Rule {
	t.Helper()
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"test","rules":[{"id":"r","title":"T","severity":"MAJOR",` +
		`"description":"D","languages":` + languages + `,"tags":[],"match":` + match + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return c.Rules
}

func kotlinFile(t testing.TB, src string, added ...int) File {
	f := File{Path: "app/src/main/A.kt", Language: "kotlin", Table: packTable(t, "kotlin"), Source: src, Added: map[int]bool{}}
	for _, l := range added {
		f.Added[l] = true
	}
	return f
}

// at renders signals as line:column, for compact expectations.
func at(sigs []Signal) string {
	var out []string
	for _, s := range sigs {
		out = append(out, fmt.Sprintf("%d:%d", s.Line, s.Column))
	}
	return strings.Join(out, " ")
}

func patterns(tokens ...string) string {
	return `{"patterns":[{"tokens":[` + strings.Join(tokens, ",") + `]}]}`
}

func TestRunSignal(t *testing.T) {
	rules := oneRule(t, `["kotlin"]`, patterns(`{"kind":"identifier","text":"GlobalScope"}`))
	res := Run(rules, kotlinFile(t, "fun f() {\n  GlobalScope.launch {}\n}\nval s = GlobalScope\n", 2), Options{})
	want := []Signal{
		{RuleID: "r", Severity: "MAJOR", Path: "app/src/main/A.kt", Line: 2, Column: 3, Origin: OriginMR},
		{RuleID: "r", Severity: "MAJOR", Path: "app/src/main/A.kt", Line: 4, Column: 9, Origin: OriginExisting},
	}
	if !reflect.DeepEqual(res.Signals, want) || len(res.NotVerified) != 0 {
		t.Errorf("Run = %+v, want signals %+v", res, want)
	}
}

func TestRunNeedsJudgementHasNoSeverity(t *testing.T) {
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"test","rules":[{"id":"r","title":"T","severity":"MAJOR","needsJudgement":true,` +
		`"description":"D","languages":[],"tags":[],"match":` + patterns(`{"kind":"punct","text":"!!"}`) + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	res := Run(c.Rules, kotlinFile(t, "val a = b!!\n"), Options{})
	if len(res.Signals) != 1 || res.Signals[0].Severity != "" || !res.Signals[0].NeedsJudgement {
		t.Errorf("signals = %+v", res.Signals)
	}
}

func TestRunPatterns(t *testing.T) {
	tests := []struct {
		name, match, src, want string
	}{
		{"comments and strings are not code", patterns(`{"text":"GlobalScope"}`),
			"// GlobalScope\n/* GlobalScope */ val s = \"GlobalScope\"\n", ""},
		{"template expressions are code", patterns(`{"kind":"identifier","text":"GlobalScope"}`),
			"val s = \"${GlobalScope.x}\"\nval t = \"a $GlobalScope\"\n", "1:12 2:13"},
		{"a string token keeps its text", patterns(`{"kind":"string","regexp":"!!\"$"}`),
			"dep(\"a:b:1!!\")\ndep(\"a:b:1\")\n", "1:5"},
		{"word covers keywords and soft keywords", patterns(`{"kind":"word","oneOf":["catch","val"]}`),
			"val a = 1\ntry {} catch (e: E) {}\n", "1:1 2:8"},
		{"identifier excludes keywords", patterns(`{"kind":"identifier","text":"val"}`), "val a = 1\n", ""},
		{"kind lists", patterns(`{"kind":["number","char"]}`), "f(1, 'c', x)\n", "1:3"},
		{"regexp is unanchored", patterns(`{"kind":"identifier","regexp":"LiveData"}`), "val a: MutableLiveData<Int>\n", "1:8"},
		{"a sequence", patterns(`{"text":"Dispatchers"}`, `{"text":"."}`, `{"oneOf":["IO","Main"]}`),
			"f(Dispatchers.IO)\ng(Dispatchers.Other)\nh(Dispatchers . Main)\n", "1:3 3:3"},
		{"at points the signal", patterns(`{"text":"lateinit"}`, `{"text":"var","at":true}`),
			"lateinit var b: B\n", "1:10"},
		{"one signal per line", patterns(`{"kind":"punct","text":"!!"}`), "val a = b!! + c!!\n", "1:10"},
		{"gap within max", patterns(`{"text":"a"}`, `{"gap":{"max":2}}`, `{"text":"z"}`),
			"a z\na b z\na b c z\na b c d z\n", "1:1 2:1 3:1"},
		{"gap stops", patterns(`{"text":"a"}`, `{"gap":{"stopAt":[{"text":";"}]}}`, `{"text":"z"}`),
			"a b z\na ; z\n", "1:1"},
		{"balanced gap", patterns(`{"text":"catch"}`, `{"text":"("}`, `{"gap":{"balanced":true}}`, `{"text":")"}`, `{"text":"{"}`, `{"text":"}"}`),
			"catch (e: E) {}\ncatch (e: F<(G)>) { }\ncatch (e: E) { log(e) }\n", "1:1 2:1"},
		{"balanced gap never leaves its group", patterns(`{"text":"("}`, `{"gap":{"balanced":true}}`, `{"text":"z"}`),
			"f(a) z\nf(a z)\n", "2:2"},
		{"repeat with backtracking", patterns(`{"text":":"}`, `{"repeat":[{"kind":"word"},{"text":"."}]}`, `{"oneOf":["Exception","Throwable"]}`),
			"a: Exception\nb: kotlin.Exception\nc: java.lang.Throwable\nd: Error\n", "1:2 2:2 3:2"},
		{"repeat min and max", patterns(`{"text":"x"}`, `{"repeat":[{"text":"+"}],"min":2,"max":3}`, `{"text":"y"}`),
			"x + y\nx + + y\nx + + + y\nx + + + + y\n", "2:1 3:1"},
		{"anyOf", patterns(`{"text":"f"}`, `{"anyOf":[[{"text":"<"}],[{"text":"("}]]}`),
			"f<A>()\nf()\nf.x\n", "1:1 2:1"},
		{"at inside an alternative", patterns(`{"text":"f"}`, `{"anyOf":[[{"text":"<","at":true}],[{"text":"("}]]}`),
			"f<A>()\nf()\n", "1:2 2:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Run(oneRule(t, `[]`, tt.match), kotlinFile(t, tt.src), Options{})
			if got := at(res.Signals); got != tt.want {
				t.Errorf("signals at %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunLanguages(t *testing.T) {
	java := File{Path: "A.java", Language: "java", Table: packTable(t, "java"), Source: "GlobalScope x;\n"}
	kotlin := kotlinFile(t, "GlobalScope\n")
	if res := Run(oneRule(t, `["kotlin"]`, patterns(`{"text":"GlobalScope"}`)), java, Options{}); len(res.Signals) != 0 {
		t.Errorf("a kotlin rule ran on java: %+v", res.Signals)
	}
	byPattern := oneRule(t, `["kotlin","java"]`, `{"patterns":[{"languages":["java"],"tokens":[{"text":"GlobalScope"}]}]}`)
	if res := Run(byPattern, kotlin, Options{}); len(res.Signals) != 0 {
		t.Errorf("a java pattern ran on kotlin: %+v", res.Signals)
	}
	if res := Run(byPattern, java, Options{}); len(res.Signals) != 1 {
		t.Errorf("the java pattern did not run on java: %+v", res.Signals)
	}
}

func TestRunSkipsRulesWithoutMatch(t *testing.T) {
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"test","rules":[{"id":"r","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res := Run(c.Rules, kotlinFile(t, "x\n"), Options{}); len(res.Signals)+len(res.NotVerified) != 0 {
		t.Errorf("Run = %+v", res)
	}
}

func TestRunFiles(t *testing.T) {
	gs := `{"kind":"identifier","text":"GlobalScope"}`
	withFiles := func(files string) string {
		return `{"files":` + files + `,"patterns":[{"tokens":[` + gs + `]}]}`
	}
	entryPoint := `[{"tokens":[{"text":"@"},{"text":"AndroidEntryPoint"}]}]`
	tests := []struct {
		name, match, path string
		test              bool
		src               string
		want              int
	}{
		{"tests skipped by default", patterns(gs), "src/test/A.kt", true, "GlobalScope\n", 0},
		{"tests included", withFiles(`{"tests":"include"}`), "src/test/A.kt", true, "GlobalScope\n", 1},
		{"tests only skips the rest", withFiles(`{"tests":"only"}`), "src/main/A.kt", false, "GlobalScope\n", 0},
		{"tests only", withFiles(`{"tests":"only"}`), "src/test/A.kt", true, "GlobalScope\n", 1},
		{"include matches", withFiles(`{"include":["**/*.gradle.kts"]}`), "app/build.gradle.kts", false, "GlobalScope\n", 1},
		{"include misses", withFiles(`{"include":["**/*.gradle.kts"]}`), "app/A.kt", false, "GlobalScope\n", 0},
		{"exclude", withFiles(`{"exclude":["**/*.kts"]}`), "app/build.gradle.kts", false, "GlobalScope\n", 0},
		{"contains holds", withFiles(`{"contains":` + entryPoint + `}`), "A.kt", false, "@AndroidEntryPoint\nclass A { val s = GlobalScope }\n", 1},
		{"contains fails", withFiles(`{"contains":` + entryPoint + `}`), "A.kt", false, "class A { val s = GlobalScope }\n", 0},
		{"notContains holds", withFiles(`{"notContains":` + entryPoint + `}`), "A.kt", false, "class A { val s = GlobalScope }\n", 1},
		{"notContains fails", withFiles(`{"notContains":` + entryPoint + `}`), "A.kt", false, "@AndroidEntryPoint\nclass A { val s = GlobalScope }\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := kotlinFile(t, tt.src)
			f.Path, f.Test = tt.path, tt.test
			if got := len(Run(oneRule(t, `[]`, tt.match), f, Options{}).Signals); got != tt.want {
				t.Errorf("%d signals, want %d", got, tt.want)
			}
		})
	}
}

func TestRunDefaults(t *testing.T) {
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"test",
	  "defaults":{"files":{"exclude":["**/*.kts"]},"ignoreLines":[{"tokens":[{"kind":"word","oneOf":["import","package"]}]}]},
	  "rules":[
	    {"id":"plain","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],
	     "match":{"patterns":[{"tokens":[{"text":"GlobalScope"}]}]}},
	    {"id":"own-files","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],
	     "match":{"files":{"include":["**/*.kts"]},"patterns":[{"tokens":[{"text":"GlobalScope"}]}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	src := "package a\nimport kotlinx.coroutines.GlobalScope\nval s = GlobalScope\n"
	kt := kotlinFile(t, src)
	if got := Run(c.Rules, kt, Options{}).Signals; len(got) != 1 || got[0].RuleID != "plain" || got[0].Line != 3 {
		t.Errorf("A.kt: signals %+v, want plain on line 3 only", got)
	}
	kts := kotlinFile(t, src)
	kts.Path = "build.gradle.kts"
	if got := Run(c.Rules, kts, Options{}).Signals; len(got) != 1 || got[0].RuleID != "own-files" || got[0].Line != 3 {
		t.Errorf("build.gradle.kts: signals %+v, want own-files on line 3 only", got)
	}
}

func TestRunExclude(t *testing.T) {
	rules := oneRule(t, `[]`, `{"patterns":[{"tokens":[{"kind":"punct","text":"!!"}]}],
	  "exclude":[{"tokens":[{"text":"get"},{"text":"("},{"text":")"},{"text":"="},{"kind":"identifier","regexp":"^_\\w*[Bb]inding$"},{"text":"!!"}]}]}`)
	src := "private val binding get() = _binding!!\nval a = b!!\nval c get() = _other!!\n"
	if got := at(Run(rules, kotlinFile(t, src), Options{}).Signals); got != "2:10 3:21" {
		t.Errorf("signals at %q", got)
	}
}

func TestRunAddedLinesOnly(t *testing.T) {
	rules := oneRule(t, `[]`, `{"lines":"added","patterns":[{"tokens":[{"text":"GlobalScope"}]}]}`)
	got := Run(rules, kotlinFile(t, "GlobalScope\nGlobalScope\n", 2), Options{}).Signals
	if at(got) != "2:1" || got[0].Origin != OriginMR {
		t.Errorf("signals %+v, want only line 2 with origin mr", got)
	}
}

func TestRunLineLength(t *testing.T) {
	tests := []struct {
		name, match, src string
		added            []int
		want             string
	}{
		{"longer than max", `{"detector":"line-length","max":5}`, "val a\nval ab\n", nil, "2:6"},
		{"runes, not bytes", `{"detector":"line-length","max":5}`, "\"äöü\"\n\"äöüß\"\n", nil, "2:6"},
		{"line breaks excluded", `{"detector":"line-length","max":5}`, "val a\r\nval a\rval ab\n", nil, "3:6"},
		{"added lines only", `{"detector":"line-length","max":3,"lines":"added"}`, "val a\nval b\n", []int{2}, "2:4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := at(Run(oneRule(t, `[]`, tt.match), kotlinFile(t, tt.src, tt.added...), Options{}).Signals); got != tt.want {
				t.Errorf("signals at %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunLineLengthSkipsIgnoredLines(t *testing.T) {
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"test","defaults":{"ignoreLines":[{"tokens":[{"text":"import"}]}]},
	  "rules":[{"id":"r","title":"T","severity":"MINOR","description":"D","languages":[],"tags":[],"match":{"detector":"line-length","max":10}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := at(Run(c.Rules, kotlinFile(t, "import a.b.c.d.e.f\nval abcdefgh = 1\n"), Options{}).Signals); got != "2:11" {
		t.Errorf("signals at %q", got)
	}
}

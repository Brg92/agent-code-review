package rules

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// FuzzRun runs the legacy rules and a rule that backtracks heavily on
// arbitrary sources, in Kotlin and Java, with a small and the default
// budget. It checks that Run never panics and is deterministic, that the
// matcher never spends more than its budget, that a rule out of budget
// gives no signals and lists the file once, and that signals lie in the
// source, sorted, one per rule and line.
func FuzzRun(f *testing.F) {
	for _, seed := range []string{
		"",
		"class A(val b: B) {\n    fun f() { try { g(x!!) } catch (e: Exception) { } }\n}\n",
		"@Inject lateinit var a: A\nval s = \"${GlobalScope.launch {}}\"\n",
		"catch (final java.lang.Exception | IOException e) { }\n",
		"dep(\"a:b:1!!\")\nversion { strictly(\"1\") }\n",
		"((((((((((((((((((((\n}}}}}}}}\n/** x */ fun f() = Foo::class\r\n",
		"x y z x y z x y z x y z x y z x y z x y z x y z x y z x y z\n",
	} {
		f.Add(seed, false)
		f.Add(seed, true)
	}
	c, err := LoadCatalog(readFile(f, filepath.Join(contractDir, "valid", "legacy-signals.json")))
	if err != nil {
		f.Fatal(err)
	}
	rules := append(c.Rules, slowRules(f)...)
	slow := slowRules(f)[0].match.patterns[0]
	f.Fuzz(func(t *testing.T, src string, java bool) {
		file := kotlinFile(t, src, 1, 2, 3)
		if java {
			file.Path, file.Language, file.Table = "app/src/main/A.java", "java", packTable(t, "java")
		}
		for _, budget := range []int{2000, 0} {
			res := Run(rules, file, Options{Budget: budget})
			if again := Run(rules, file, Options{Budget: budget}); !reflect.DeepEqual(res, again) {
				t.Fatalf("budget %d: two runs differ:\n%+v\n%+v", budget, res, again)
			}
			checkResult(t, rules, file, res)
		}
		m := newMatcher(lexer.Tokenize(file.Table, src), codeTokens(lexer.Tokenize(file.Table, src)), file.Table, 500)
		m.find(slow)
		if m.steps > m.budget+1 {
			t.Fatalf("matcher spent %d steps, budget %d", m.steps, m.budget)
		}
	})
}

func checkResult(t *testing.T, rules []*Rule, f File, res Result) {
	t.Helper()
	known := map[string]bool{}
	for _, r := range rules {
		known[r.ID] = true
	}
	out := map[string]bool{}
	for _, nv := range res.NotVerified {
		if nv.Path != f.Path || nv.Reason != ReasonMatchBudget || !known[nv.Detail] || out[nv.Detail] {
			t.Fatalf("bad not-verified entry %+v (listed before: %v)", nv, out[nv.Detail])
		}
		out[nv.Detail] = true
	}
	lineCount := len(lines(f.Source))
	seen := map[[2]any]bool{}
	for i, s := range res.Signals {
		if out[s.RuleID] {
			t.Fatalf("signal of a rule out of budget: %+v", s)
		}
		if s.Path != f.Path || s.Line < 1 || s.Line > lineCount || s.Column < 1 {
			t.Fatalf("signal outside the source (%d lines): %+v", lineCount, s)
		}
		key := [2]any{s.RuleID, s.Line}
		if seen[key] {
			t.Fatalf("two signals of %s on line %d", s.RuleID, s.Line)
		}
		seen[key] = true
		if i > 0 {
			p := res.Signals[i-1]
			if p.Line > s.Line || p.Line == s.Line && p.Column > s.Column {
				t.Fatalf("signals out of order: %+v before %+v", p, s)
			}
		}
		wantOrigin := OriginExisting
		if f.Added[s.Line] {
			wantOrigin = OriginMR
		}
		if s.Origin != wantOrigin {
			t.Fatalf("signal %+v: origin %s, want %s", s, s.Origin, wantOrigin)
		}
	}
}

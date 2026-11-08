package lexer

import (
	"os"
	"path/filepath"
	"testing"
)

// packTable reads a lexer table of the android pack.
func packTable(t testing.TB, language string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "packs", "android", "lexer", language+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPackTablesLoad(t *testing.T) {
	for _, language := range []string{"kotlin", "java", "groovy", "toml", "properties"} {
		if tab := mustLoad(t, packTable(t, language)); tab.Language != language {
			t.Errorf("%s.json: language %q", language, tab.Language)
		}
	}
}

// TestPackTablesPassTheLexerCases runs the cases written for the test
// tables against the pack tables, which describe the same languages in
// full.
func TestPackTablesPassTheLexerCases(t *testing.T) {
	kotlin, java := packTable(t, "kotlin"), packTable(t, "java")
	for _, cases := range [][]tokenCase{tokenizeKotlinLegacyCases, tokenizeKotlinCases, tokenizeTemplatesLegacyCases} {
		runTokenCases(t, kotlin, cases)
	}
	for _, cases := range [][]tokenCase{tokenizeJavaCases, tokenizeJavaHasNoTemplatesCases} {
		runTokenCases(t, java, cases)
	}
	for _, cases := range [][]cleanCase{cleanKotlinLegacyCases, cleanKotlinCases} {
		runCleanCases(t, kotlin, cases)
	}
	runCleanCases(t, java, cleanJavaCases)
}

func TestPackKotlinTable(t *testing.T) {
	runTokenCases(t, packTable(t, "kotlin"), []tokenCase{
		{"hard keywords", "package a\nclass B : C() { fun f() = when (x) { is D -> null } }", []string{
			"Keyword package", "Identifier a", "Keyword class", "Identifier B", "Punct :", "Identifier C", "Punct (", "Punct )",
			"Punct {", "Keyword fun", "Identifier f", "Punct (", "Punct )", "Punct =", "Keyword when", "Punct (", "Identifier x", "Punct )",
			"Punct {", "Keyword is", "Identifier D", "Punct ->", "Keyword null", "Punct }", "Punct }"}},
		{"soft keywords and modifiers", "private val x by lazy { field }", []string{
			"Identifier private [soft]", "Keyword val", "Identifier x", "Identifier by [soft]", "Identifier lazy", "Punct {", "Identifier field [soft]", "Punct }"}},
		{"modifiers", "override suspend inline fun", []string{"Identifier override [soft]", "Identifier suspend [soft]", "Identifier inline [soft]", "Keyword fun"}},
		{"null-safety operators", "a?.b ?: c!! !== d", []string{"Identifier a", "Punct ?.", "Identifier b", "Punct ?:", "Identifier c", "Punct !!", "Punct !==", "Identifier d"}},
		{"negated in and safe cast", "a !in b as? C", []string{"Identifier a", "Punct !", "Keyword in", "Identifier b", "Keyword as", "Punct ?", "Identifier C"}},
		{"ranges and references", "0..<n step 2 ::f", []string{"Number 0", "Punct ..<", "Identifier n", "Identifier step", "Number 2", "Punct ::", "Identifier f"}},
		{"nested comments", "/* a /* b */ c */ x", []string{"BlockComment /* a /* b */ c */", "Identifier x"}},
	})
}

func TestPackJavaTable(t *testing.T) {
	runTokenCases(t, packTable(t, "java"), []tokenCase{
		{"keywords", "public static void main(String[] a) { return; }", []string{
			"Keyword public", "Keyword static", "Keyword void", "Identifier main", "Punct (", "Identifier String", "Punct [", "Punct ]",
			"Identifier a", "Punct )", "Punct {", "Keyword return", "Punct ;", "Punct }"}},
		{"contextual keywords", "sealed record P(int x) permits Q", []string{
			"Identifier sealed [soft]", "Identifier record [soft]", "Identifier P", "Punct (", "Keyword int", "Identifier x", "Punct )", "Identifier permits [soft]", "Identifier Q"}},
		{"Kotlin words are identifiers", "val fun when", []string{"Identifier val", "Identifier fun", "Identifier when [soft]"}},
		{"shift operators", "a >>>= b >> c << d", []string{"Identifier a", "Punct >>>=", "Identifier b", "Punct >>", "Identifier c", "Punct <<", "Identifier d"}},
		{"varargs and lambdas", "(String... a) -> a", []string{"Punct (", "Identifier String", "Punct ...", "Identifier a", "Punct )", "Punct ->", "Identifier a"}},
		{"no nested comments", "/* a /* b */ c */", []string{"BlockComment /* a /* b */", "Identifier c", "Punct *", "Punct /"}},
	})
}

func TestPackGroovyTable(t *testing.T) {
	runTokenCases(t, packTable(t, "groovy"), []tokenCase{
		{"dependency with the strict shorthand", `implementation 'a:b:1.0!!'`, []string{"Identifier implementation", `String 'a:b:1.0!!'`}},
		{"strict version block", `version { strictly '1.0' }`, []string{"Identifier version", "Punct {", "Identifier strictly", `String '1.0'`, "Punct }"}},
		{"keywords", `def x = new Foo() as Bar`, []string{"Keyword def", "Identifier x", "Punct =", "Keyword new", "Identifier Foo", "Punct (", "Punct )", "Keyword as", "Identifier Bar"}},
		{"comments", "a // b\n/* c */ /** d */", []string{"Identifier a", "LineComment // b", "BlockComment /* c */", "DocComment /** d */"}},
		{"gstring", `"v${a}" x`, []string{`String "v${a}"`, "  Template ${a}", "    Identifier a", "Identifier x"}},
		{"gstring identifier", `"v$a" x`, []string{`String "v$a"`, "  Template $a", "    Identifier a", "Identifier x"}},
		{"single quotes do not interpolate", `'v${a}' x`, []string{`String 'v${a}'`, "Identifier x"}},
		{"triple quotes", "'''a 'b'\n c''' x", []string{"RawString '''a 'b'\n c'''", "Identifier x"}},
		{"escaped quote", `'it\'s' x`, []string{`String 'it\'s'`, "Identifier x"}},
		{"operators", `a?.b ?: c *. d <=> e`, []string{"Identifier a", "Punct ?.", "Identifier b", "Punct ?:", "Identifier c", "Punct *.", "Identifier d", "Punct <=>", "Identifier e"}},
		{"closure", `list.each { it -> it }`, []string{"Identifier list", "Punct .", "Identifier each", "Punct {", "Identifier it", "Punct ->", "Identifier it", "Punct }"}},
	})
}

func TestPackTomlTable(t *testing.T) {
	runTokenCases(t, packTable(t, "toml"), []tokenCase{
		{"table and entry", "[versions]\ncompose-bom = \"2024.01\"", []string{"Punct [", "Identifier versions", "Punct ]", "Identifier compose-bom", "Punct =", `String "2024.01"`}},
		{"comment", `a = 1 # note`, []string{"Identifier a", "Punct =", "Number 1", "LineComment # note"}},
		{"inline table with a strict version", `lib = { module = "a:b", version = { strictly = "1.0" } }`, []string{
			"Identifier lib", "Punct =", "Punct {", "Identifier module", "Punct =", `String "a:b"`, "Punct ,", "Identifier version", "Punct =",
			"Punct {", "Identifier strictly", "Punct =", `String "1.0"`, "Punct }", "Punct }"}},
		{"dotted key", `version.strictly = "1.0"`, []string{"Identifier version", "Punct .", "Identifier strictly", "Punct =", `String "1.0"`}},
		{"literal string has no escapes", `a = 'C:\' # x`, []string{"Identifier a", "Punct =", `String 'C:\'`, "LineComment # x"}},
		{"multi-line strings", "a = \"\"\"x\n\"y\"\"\"\nb = '''\\'''", []string{
			"Identifier a", "Punct =", "RawString \"\"\"x\n\"y\"\"\"", "Identifier b", "Punct =", `RawString '''\'''`}},
		{"booleans", `a = true`, []string{"Identifier a", "Punct =", "Keyword true"}},
	})
}

func TestPackPropertiesTable(t *testing.T) {
	runTokenCases(t, packTable(t, "properties"), []tokenCase{
		{"entry", `org.gradle.jvmargs=-Xmx2g`, []string{"Identifier org", "Punct .", "Identifier gradle", "Punct .", "Identifier jvmargs", "Punct =", "Punct -", "Identifier Xmx2g"}},
		{"comment", "# note\na=b", []string{"LineComment # note", "Identifier a", "Punct =", "Identifier b"}},
	})
}

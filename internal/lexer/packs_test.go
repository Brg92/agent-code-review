package lexer

import (
	"os"
	"path/filepath"
	"testing"
)

// packTable reads a lexer table of the android pack.
func packTable(t *testing.T, language string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "packs", "android", "lexer", language+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPackTablesLoad(t *testing.T) {
	for _, tt := range []struct{ language, extensions string }{
		{"kotlin", ".kt .kts"},
		{"java", ".java"},
	} {
		tab := mustLoad(t, packTable(t, tt.language))
		if tab.Language != tt.language {
			t.Errorf("%s.json: language %q", tt.language, tab.Language)
		}
		got := ""
		for i, e := range tab.Extensions {
			if i > 0 {
				got += " "
			}
			got += e
		}
		if got != tt.extensions {
			t.Errorf("%s.json: extensions %q, want %q", tt.language, got, tt.extensions)
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

package lexer

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The test tables describe Kotlin and Java with a few keywords and
// operators only; the pack tables are tested on their own.
const (
	testKotlinTable = `{
  "schema": 1, "language": "kotlin", "extensions": [".kt"],
  "lineComment": "//",
  "blockComment": {"open": "/*", "close": "*/", "nested": true},
  "docComment": "/**",
  "strings": [
    {"delimiter": "\"", "escape": "\\", "templates": true},
    {"delimiter": "\"\"\"", "raw": true, "multiline": true, "templates": true, "greedyClose": true}
  ],
  "char": {"delimiter": "'", "escape": "\\"},
  "quotedIdentifier": "` + "`" + `",
  "templates": {"identifier": "$", "open": "${", "close": "}"},
  "keywords": ["val", "fun", "if", "else", "true"],
  "softKeywords": ["get", "set", "by"],
  "operators": ["==", "===", "->", "?.", "..", "..<", "+="]
}`
	testJavaTable = `{
  "schema": 1, "language": "java", "extensions": [".java"],
  "lineComment": "//",
  "blockComment": {"open": "/*", "close": "*/", "nested": false},
  "docComment": "/**",
  "strings": [
    {"delimiter": "\"", "escape": "\\"},
    {"delimiter": "\"\"\"", "escape": "\\", "raw": true, "multiline": true}
  ],
  "char": {"delimiter": "'", "escape": "\\"},
  "identifierChars": "$",
  "keywords": ["class", "int", "return"],
  "softKeywords": ["var", "record"],
  "operators": ["==", "->", "::", ">>>", ">>"]
}`
)

func mustLoad(t testing.TB, data string) *Table {
	t.Helper()
	tab, err := LoadTable([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

// describe renders tokens as "Kind text [flags]", with the inner tokens of
// strings and templates indented below their owner.
func describe(tokens []Token) []string {
	var out []string
	var walk func([]Token, string)
	walk = func(tokens []Token, indent string) {
		for _, tok := range tokens {
			line := fmt.Sprintf("%s%s %s", indent, tok.Kind, tok.Text)
			if tok.SoftKeyword {
				line += " [soft]"
			}
			if tok.Unterminated {
				line += " [unterminated]"
			}
			out = append(out, line)
			walk(tok.Inner, indent+"  ")
		}
	}
	walk(tokens, "")
	return out
}

type tokenCase struct {
	name string
	src  string
	want []string
}

func runTokenCases(t *testing.T, table string, tests []tokenCase) {
	t.Helper()
	tab := mustLoad(t, table)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describe(Tokenize(tab, tt.src))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Tokenize(%q)\n got: %q\nwant: %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestTokenizeKotlinLegacy ports the cases of the legacy Get-CleanKotlinCode
// scanner. Cases marked "improvement" differ from the legacy on purpose.
func TestTokenizeKotlinLegacy(t *testing.T) {
	runTokenCases(t, testKotlinTable, []tokenCase{
		{"empty", "", nil},
		{"line comment", "val a = 1 // note", []string{"Keyword val", "Identifier a", "Punct =", "Number 1", "LineComment // note"}},
		{"line comment ends at the line end", "// a\nb", []string{"LineComment // a", "Identifier b"}},
		{"block comment", "a /* b */ c", []string{"Identifier a", "BlockComment /* b */", "Identifier c"}},
		{"block comment over lines", "a /* b\nc */ d", []string{"Identifier a", "BlockComment /* b\nc */", "Identifier d"}},
		{"nested block comment", "a /* b /* c */ d */ e", []string{"Identifier a", "BlockComment /* b /* c */ d */", "Identifier e"}},
		{"doubly nested block comment", "/* 1 /* 2 /* 3 */ 2 */ 1 */x", []string{"BlockComment /* 1 /* 2 /* 3 */ 2 */ 1 */", "Identifier x"}},
		{"string with escapes", `"a\nb\tc" x`, []string{`String "a\nb\tc"`, "Identifier x"}},
		{"escaped quote in string", `"say \"hi\"" x`, []string{`String "say \"hi\""`, "Identifier x"}},
		{"escaped backslash before the closing quote", `"a\\" x`, []string{`String "a\\"`, "Identifier x"}},
		{"raw string with quotes inside", `"""he said "hi" and ""ok"" """ x`, []string{`RawString """he said "hi" and ""ok"" """`, "Identifier x"}},
		{"raw string has no escapes", `"""a\""" x`, []string{`RawString """a\"""`, "Identifier x"}},
		{"raw string over lines", "\"\"\"a\n  b\n\"\"\".x", []string{"RawString \"\"\"a\n  b\n\"\"\"", "Punct .", "Identifier x"}},
		// Improvement: the legacy closed at the first """ and opened a new
		// string with the extra quote; Kotlin keeps it in the raw string.
		{"improvement: raw string ends at the last quote", `"""a"""" x`, []string{`RawString """a""""`, "Identifier x"}},
		{"chars", `'a' '\'' '"' '\\'`, []string{"Char 'a'", `Char '\''`, `Char '"'`, `Char '\\'`}},
		{"comment markers inside a string", `"// not /* a comment" x`, []string{`String "// not /* a comment"`, "Identifier x"}},
		{"comment markers inside a raw string", `"""// /* """ x`, []string{`RawString """// /* """`, "Identifier x"}},
		{"char quote inside a string", `"it's" x`, []string{`String "it's"`, "Identifier x"}},
		{"string quotes inside a line comment", `// it's "x" x`, []string{`LineComment // it's "x" x`}},
		{"string quotes inside a block comment", `/* "a' */ x`, []string{`BlockComment /* "a' */`, "Identifier x"}},
		{"crlf ends a line comment", "// a\r\nb", []string{"LineComment // a", "Identifier b"}},
		{"unterminated string ends at the line end", "\"abc\nval x", []string{`String "abc [unterminated]`, "Keyword val", "Identifier x"}},
		{"unterminated string ends before crlf", "\"abc\r\nx", []string{`String "abc [unterminated]`, "Identifier x"}},
		{"unterminated string at the end of file", `x "abc`, []string{"Identifier x", `String "abc [unterminated]`}},
		// Improvement: the legacy skipped the line break after a final
		// escape and kept the string open on the next line.
		{"improvement: escape before the line end", "\"abc\\\nx", []string{`String "abc\ [unterminated]`, "Identifier x"}},
		{"unterminated char ends at the line end", "'a\nb", []string{"Char 'a [unterminated]", "Identifier b"}},
		{"unterminated block comment ends at the end of file", "a /* b\n/* c */ d", []string{"Identifier a", "BlockComment /* b\n/* c */ d [unterminated]"}},
		{"unterminated raw string ends at the end of file", "\"\"\"a\nb", []string{"RawString \"\"\"a\nb [unterminated]"}},
		{"lone opener at the end of file", `/`, []string{"Punct /"}},
	})
}

func TestTokenizeKotlin(t *testing.T) {
	runTokenCases(t, testKotlinTable, []tokenCase{
		{"doc comment", "/** doc */ fun f()", []string{"DocComment /** doc */", "Keyword fun", "Identifier f", "Punct (", "Punct )"}},
		{"empty block comment is not a doc comment", "/**/ x", []string{"BlockComment /**/", "Identifier x"}},
		{"keywords and soft keywords", "val get = by if", []string{"Keyword val", "Identifier get [soft]", "Punct =", "Identifier by [soft]", "Keyword if"}},
		{"identifiers", "_a1 ünï x_", []string{"Identifier _a1", "Identifier ünï", "Identifier x_"}},
		// Improvement: the legacy had no notion of quoted identifiers and
		// read the quotes inside as a string.
		{"improvement: quoted identifier", "fun `returns \"x\" // y`() {}", []string{"Keyword fun", "Identifier `returns \"x\" // y`", "Punct (", "Punct )", "Punct {", "Punct }"}},
		{"unterminated quoted identifier ends at the line end", "`a b\nc", []string{"Identifier `a b [unterminated]", "Identifier c"}},
		{"numbers", "1 1_000L 0xFF 1.5f 1e-3 2E+4 .5 1..2 1..<3", []string{
			"Number 1", "Number 1_000L", "Number 0xFF", "Number 1.5f", "Number 1e-3", "Number 2E+4", "Number .5",
			"Number 1", "Punct ..", "Number 2", "Number 1", "Punct ..<", "Number 3"}},
		{"hex digit e is not an exponent", "0x1e+2", []string{"Number 0x1e", "Punct +", "Number 2"}},
		{"operators longest first", "a===b?.c->d+=1", []string{"Identifier a", "Punct ===", "Identifier b", "Punct ?.", "Identifier c", "Punct ->", "Identifier d", "Punct +=", "Number 1"}},
		{"other punctuation", "@a#;", []string{"Punct @", "Identifier a", "Punct #", "Punct ;"}},
		{"whitespace is skipped", " \t\f a\n\r\n b ", []string{"Identifier a", "Identifier b"}},
		{"invalid UTF-8", "a\xffb", []string{"Identifier a", "Punct \xff", "Identifier b"}},
	})
}

func TestTokenizeJava(t *testing.T) {
	runTokenCases(t, testJavaTable, []tokenCase{
		{"block comments do not nest", "/* /* */ x */", []string{"BlockComment /* /* */", "Identifier x", "Punct *", "Punct /"}},
		{"doc comment", "/** a */ class", []string{"DocComment /** a */", "Keyword class"}},
		{"text block", "\"\"\"\n  a \"b\" \"\"c\n  \"\"\";", []string{"RawString \"\"\"\n  a \"b\" \"\"c\n  \"\"\"", "Punct ;"}},
		{"text block with escapes", "\"\"\"\n\\\"\"\"\n\"\"\" x", []string{"RawString \"\"\"\n\\\"\"\"\n\"\"\"", "Identifier x"}},
		{"text block ends at the first closing delimiter", "\"\"\"\na\"\"\"\" x", []string{"RawString \"\"\"\na\"\"\"", `String " x [unterminated]`}},
		{"dollar in identifiers", "int a$b = $c;", []string{"Keyword int", "Identifier a$b", "Punct =", "Identifier $c", "Punct ;"}},
		{"soft keywords", "var record", []string{"Identifier var [soft]", "Identifier record [soft]"}},
		{"chars", `'A' '\''`, []string{`Char 'A'`, `Char '\''`}},
		{"backtick is punctuation", "`a`", []string{"Punct `", "Identifier a", "Punct `"}},
		{"method reference", "A::b", []string{"Identifier A", "Punct ::", "Identifier b"}},
	})
}

func TestTokenPositions(t *testing.T) {
	tab := mustLoad(t, testKotlinTable)
	src := "val ü = \"ä\" // ö\r\n/* a\n b */ x\r'c'\n\"\"\"é\n\"\"\" y"
	type pos struct {
		text                 string
		offset, line, column int
	}
	want := []pos{
		{"val", 0, 1, 1},
		{"ü", 4, 1, 5},
		{"=", 7, 1, 7},
		{`"ä"`, 9, 1, 9},
		{"// ö", 14, 1, 13},
		{"/* a\n b */", 21, 2, 1},
		{"x", 32, 3, 7},
		{"'c'", 34, 4, 1},
		{"\"\"\"é\n\"\"\"", 38, 5, 1},
		{"y", 48, 6, 5},
	}
	var got []pos
	for _, tok := range Tokenize(tab, src) {
		got = append(got, pos{tok.Text, tok.Offset, tok.Line, tok.Column})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("positions\n got: %+v\nwant: %+v", got, want)
	}
	for _, tok := range Tokenize(tab, src) {
		if src[tok.Offset:tok.Offset+len(tok.Text)] != tok.Text {
			t.Errorf("%q: offset %d does not point to the text", tok.Text, tok.Offset)
		}
	}
}

func TestKindString(t *testing.T) {
	if got := Kind(0).String(); !strings.HasPrefix(got, "Kind(") {
		t.Errorf("Kind(0).String() = %q", got)
	}
}

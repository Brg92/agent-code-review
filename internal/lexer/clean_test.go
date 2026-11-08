package lexer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

type cleanCase struct {
	name, src, want string
}

func runCleanCases(t *testing.T, table string, tests []cleanCase) {
	t.Helper()
	tab := mustLoad(t, table)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Clean(tab, tt.src)
			if got != tt.want {
				t.Errorf("Clean(%q)\n got: %q\nwant: %q", tt.src, got, tt.want)
			}
			checkShape(t, tt.src, got)
		})
	}
}

// lines splits at \n, \r\n and lone \r, keeping the line breaks.
func lines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n':
			i++
			fallthrough
		case s[i] == '\n', s[i] == '\r':
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func lineBreak(line string) string {
	return line[len(strings.TrimRight(line, "\r\n")):]
}

// checkShape checks that clean has the lines of src, with the same line
// breaks and the same number of runes on each line.
func checkShape(t *testing.T, src, clean string) {
	t.Helper()
	sl, cl := lines(src), lines(clean)
	if len(sl) != len(cl) {
		t.Fatalf("Clean has %d lines, source %d", len(cl), len(sl))
	}
	for i := range sl {
		if utf8.RuneCountInString(sl[i]) != utf8.RuneCountInString(cl[i]) {
			t.Errorf("line %d: %d runes in Clean, %d in the source", i+1, utf8.RuneCountInString(cl[i]), utf8.RuneCountInString(sl[i]))
		}
		if lineBreak(sl[i]) != lineBreak(cl[i]) {
			t.Errorf("line %d: line break %q in Clean, %q in the source", i+1, lineBreak(cl[i]), lineBreak(sl[i]))
		}
	}
}

// cleanKotlinLegacyCases port the cases of the legacy Get-CleanKotlinCode.
// Improvement over the legacy, in every string and char case: the quotes
// of literals stay, only their contents become spaces, so code like f("")
// still shows an argument. Comments are blanked whole, markers included,
// as in the legacy.
var cleanKotlinLegacyCases = []cleanCase{
	{"empty", "", ""},
	{"code only", "if (a) { b() }", "if (a) { b() }"},
	{"line comment", "a // b {", "a       "},
	{"block comment over lines", "a /* b {\nc */ d", "a       \n     d"},
	{"nested block comment", "a /* b /* c */ d */ e", "a                   e"},
	{"doc comment", "/** {a} */\nfun f()", "          \nfun f()"},
	{"string", `f("a(b")`, `f("   ")`},
	{"escapes in a string", `"a\"b\\" x`, `"      " x`},
	{"raw string with quotes", `"""a "{" b""" x`, `"""       """ x`},
	{"improvement: raw string ends at the last quote", `"""a"""" x`, `"""  """ x`},
	{"raw string over lines", "\"\"\"a {\n} b\"\"\"", "\"\"\"   \n   \"\"\""},
	{"chars", `'{' '\'' '"'`, `' ' '  ' ' '`},
	{"comment markers inside a string", `"// /*" x`, `"     " x`},
	{"quotes inside comments", "// it's \"x\"\n/* ' */ y", "           \n        y"},
	{"expression template", `"a${b + "c"}d"`, `"   b + " "  "`},
	{"braces and strings in a template", `"${m["}"] { 1 }}"`, `"  m[" "] { 1 } "`},
	{"nested templates", `"${"${x}"}"`, `"  "  x " "`},
	{"template in a raw string", "\"\"\"${a}\n\"\"\"", "\"\"\"  a \n\"\"\""},
	{"comment inside a template", `"${a /* } */}"`, `"  a         "`},
	// Improvement: the legacy blanked $name templates as string text.
	{"improvement: identifier template", `"$x!"`, `" x "`},
	{"newlines preserved", "a\nb\r\nc\rd", "a\nb\r\nc\rd"},
	{"crlf after a comment", "// a\r\nb", "    \r\nb"},
	{"unterminated string ends at the line end", "\"a{\nb", "\"  \nb"},
	{"unterminated char", "'a\nb", "' \nb"},
	{"unterminated block comment", "a /* b\nc", "a     \n "},
	{"unterminated raw string", "\"\"\"a\n{", "\"\"\" \n "},
	{"template closed on the next line", "\"${a\n}", "\"  a\n "},
	{"unterminated template", "\"${a {\nb", "\"  a {\nb"},
}

func TestCleanKotlinLegacy(t *testing.T) { runCleanCases(t, testKotlinTable, cleanKotlinLegacyCases) }

var cleanKotlinCases = []cleanCase{
	{"non-ASCII contents are one space per rune", "\"äé€\" // ö\nü", "\"   \"     \nü"},
	{"quoted identifiers stay", "fun `a \"b\"`() {}", "fun `a \"b\"`() {}"},
	{"invalid UTF-8 in a string", "\"\xff\" \xfe", "\" \" \xfe"},
}

func TestCleanKotlin(t *testing.T) { runCleanCases(t, testKotlinTable, cleanKotlinCases) }

var cleanJavaCases = []cleanCase{
	{"block comments do not nest", "/* /* */ x */", "         x */"},
	{"text block", "\"\"\"\n  a\"b\n  \"\"\"", "\"\"\"\n     \n  \"\"\""},
	{"no templates", `"${a}"`, `"    "`},
}

func TestCleanJava(t *testing.T) { runCleanCases(t, testJavaTable, cleanJavaCases) }

// TestCleanKeepsCodePositions checks that the text of every code token,
// template expressions included, is at the same line and column in the
// clean source.
func TestCleanKeepsCodePositions(t *testing.T) {
	tab := mustLoad(t, testKotlinTable)
	src := "val ä = \"ö${x + 1}\" /* ü\n */ + 'ß' // c\r\nfun `é`() = \"\"\"\n€$y\"\"\""
	cl := lines(Clean(tab, src))
	var walk func([]Token)
	walk = func(toks []Token) {
		for _, tok := range toks {
			switch tok.Kind {
			case String, RawString, Template:
				walk(tok.Inner)
			case Char, LineComment, BlockComment, DocComment:
			default:
				rest := string([]rune(cl[tok.Line-1])[tok.Column-1:])
				if !strings.HasPrefix(rest, tok.Text) {
					t.Errorf("%q at line %d column %d: Clean has %q there", tok.Text, tok.Line, tok.Column, rest)
				}
			}
		}
	}
	walk(Tokenize(tab, src))
}

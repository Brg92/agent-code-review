package lexer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const minimalTable = `{"schema":1,"language":"plain","keywords":["begin"],"softKeywords":[]}`

// withField returns minimalTable with one more top-level field.
func withField(field string) string {
	return strings.TrimSuffix(minimalTable, "}") + "," + field + "}"
}

func TestLoadTable(t *testing.T) {
	tab, err := LoadTable([]byte(withField(
		`"lineComment":"//","blockComment":{"open":"/*","close":"*/","nested":true},"docComment":"/**",` +
			`"strings":[{"delimiter":"\"","escape":"\\"},{"delimiter":"\"\"\"","raw":true,"multiline":true,"templates":true,"greedyClose":true}],` +
			`"char":{"delimiter":"'","escape":"\\"},"quotedIdentifier":"` + "`" + `",` +
			`"templates":{"identifier":"$","open":"${","close":"}"},"identifierChars":"$","operators":["==","..."]`)))
	if err != nil {
		t.Fatal(err)
	}
	if tab.Language != "plain" {
		t.Errorf("language %q", tab.Language)
	}
	if tab.BlockComment == nil || !tab.BlockComment.Nested || tab.DocComment != "/**" {
		t.Errorf("block comment %+v, doc comment %q", tab.BlockComment, tab.DocComment)
	}
	var delims []string
	for _, s := range tab.Strings {
		delims = append(delims, s.Delimiter)
	}
	if want := []string{`"""`, `"`}; !reflect.DeepEqual(delims, want) {
		t.Errorf("string delimiters = %q, want longest first %q", delims, want)
	}
	if want := []string{"...", "=="}; !reflect.DeepEqual(tab.Operators, want) {
		t.Errorf("operators = %q, want %q", tab.Operators, want)
	}
}

func TestLoadTableRejects(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"not JSON", `{`, "decode"},
		{"trailing data", minimalTable + `{}`, "trailing"},
		{"unknown field", withField(`"comments":"//"`), "unknown field"},
		{"unknown nested field", withField(`"char":{"delimiter":"'","quote":"'"}`), "unknown field"},
		{"wrong version", strings.Replace(minimalTable, `"schema":1`, `"schema":2`, 1), "schema"},
		{"no version", strings.Replace(minimalTable, `"schema":1,`, ``, 1), "schema"},
		{"no language", strings.Replace(minimalTable, `"language":"plain",`, ``, 1), "language"},
		{"language not lowercase", strings.Replace(minimalTable, `plain`, `Plain`, 1), "language"},
		{"extensions belong to the manifest", withField(`"extensions":[".pln"]`), "unknown field"},
		{"no keywords", strings.Replace(minimalTable, `"keywords":["begin"],`, ``, 1), "keywords"},
		{"no soft keywords", strings.Replace(minimalTable, `,"softKeywords":[]`, ``, 1), "softKeywords"},
		{"bad keyword", strings.Replace(minimalTable, `"begin"`, `"go to"`, 1), "keyword"},
		{"duplicate keyword", strings.Replace(minimalTable, `"begin"`, `"begin","begin"`, 1), "keyword"},
		{"keyword also soft", strings.Replace(minimalTable, `[]`, `["begin"]`, 1), "keyword"},
		{"blank line comment", withField(`"lineComment":""`), "lineComment"},
		{"block comment without close", withField(`"blockComment":{"open":"/*","nested":false}`), "blockComment"},
		{"block comment without nested", withField(`"blockComment":{"open":"/*","close":"*/"}`), "blockComment"},
		{"doc comment without block comment", withField(`"docComment":"/**"`), "docComment"},
		{"doc comment not a block opener", withField(`"blockComment":{"open":"/*","close":"*/","nested":false},"docComment":"//*"`), "docComment"},
		{"delimiter with space", withField(`"strings":[{"delimiter":"\" "}]`), "delimiter"},
		{"duplicate delimiter", withField(`"strings":[{"delimiter":"\""},{"delimiter":"\""}]`), "delimiter"},
		{"escape too long", withField(`"strings":[{"delimiter":"\"","escape":"\\\\"}]`), "escape"},
		{"string templates without templates", withField(`"strings":[{"delimiter":"\"","templates":true}]`), "templates"},
		{"char without delimiter", withField(`"char":{"escape":"\\"}`), "char"},
		{"template without close", withField(`"templates":{"identifier":"$","open":"${"}`), "templates"},
		{"operator with letters", withField(`"operators":["!in"]`), "operator"},
		{"single-character operator", withField(`"operators":["+"]`), "operator"},
		{"duplicate operator", withField(`"operators":["==","=="]`), "operator"},
		{"blank identifier char", withField(`"identifierChars":" "`), "identifierChars"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadTable([]byte(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("LoadTable(%s) error = %v, want one that mentions %q", tt.data, err, tt.want)
			}
		})
	}
}

// TestLoadTableMatchesTheContract keeps the loader in step with the lexer
// table schema: it accepts the valid contract examples and rejects the
// invalid ones.
func TestLoadTableMatchesTheContract(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "contracts", "lexer-table")
	for _, kind := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join(dir, kind, "*.json"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no %s examples: %v", kind, err)
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			_, err = LoadTable(data)
			if kind == "valid" && err != nil {
				t.Errorf("%s: %v", f, err)
			}
			if kind == "invalid" && err == nil {
				t.Errorf("%s: loaded, want an error", f)
			}
		}
	}
}

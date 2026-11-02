package jsonschema

import (
	"reflect"
	"testing"
)

// where returns the path and keyword of each issue.
func where(issues []Issue) []string {
	var out []string
	for _, i := range issues {
		out = append(out, i.Path+" "+i.Keyword)
	}
	return out
}

type validationCase struct {
	name   string
	schema string
	doc    string
	want   []string
}

func runValidationCases(t *testing.T, tests []validationCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Compile([]byte(tt.schema))
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			issues, err := s.Validate([]byte(tt.doc))
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if got := where(issues); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Validate(%s) = %q, want %q (issues: %v)", tt.doc, got, tt.want, issues)
			}
		})
	}
}

func TestCompileRejectsInvalidSchemas(t *testing.T) {
	for _, schema := range []string{
		``,
		`{`,
		`[]`,
		`{"type":"text"}`,
		`{"type":["string",3]}`,
		`{"required":"a"}`,
		`{"properties":{"a":3}}`,
		`{"additionalProperties":"no"}`,
		`{"format":"date-time"}`,
		`{"properties":{"a":{"oneOf":[]}}}`,
	} {
		if _, err := Compile([]byte(schema)); err == nil {
			t.Errorf("Compile(%s) succeeded, want error", schema)
		}
	}
}

func TestCompileAcceptsAnnotations(t *testing.T) {
	schema := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://example.com/x.json",` +
		`"title":"X","description":"d","$comment":"c","type":"object"}`
	if _, err := Compile([]byte(schema)); err != nil {
		t.Errorf("Compile: %v", err)
	}
}

func TestValidateRejectsInvalidJSON(t *testing.T) {
	s, err := Compile([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []string{``, `{`, `{} {}`} {
		if _, err := s.Validate([]byte(doc)); err == nil {
			t.Errorf("Validate(%q) succeeded, want error", doc)
		}
	}
}

func TestType(t *testing.T) {
	runValidationCases(t, []validationCase{
		{"string ok", `{"type":"string"}`, `"a"`, nil},
		{"string bad", `{"type":"string"}`, `1`, []string{" type"}},
		{"integer ok", `{"type":"integer"}`, `3`, nil},
		{"integer with zero fraction", `{"type":"integer"}`, `3.0`, nil},
		{"integer bad", `{"type":"integer"}`, `3.5`, []string{" type"}},
		{"number accepts integer", `{"type":"number"}`, `3`, nil},
		{"null", `{"type":"null"}`, `null`, nil},
		{"boolean bad", `{"type":"boolean"}`, `"true"`, []string{" type"}},
		{"array", `{"type":"array"}`, `[]`, nil},
		{"object bad", `{"type":"object"}`, `[]`, []string{" type"}},
		{"type list ok", `{"type":["string","null"]}`, `null`, nil},
		{"type list bad", `{"type":["string","null"]}`, `{}`, []string{" type"}},
		{"true schema", `true`, `{}`, nil},
		{"false schema", `false`, `{}`, []string{" false"}},
	})
}

func TestObjectKeywords(t *testing.T) {
	schema := `{"type":"object","required":["a","b"],"properties":{"a":{"type":"string"},` +
		`"n":{"type":"object","properties":{"x":{"type":"integer"}},"additionalProperties":false}},` +
		`"additionalProperties":{"type":"integer"}}`
	runValidationCases(t, []validationCase{
		{"valid", schema, `{"a":"x","b":1,"n":{"x":2}}`, nil},
		{"missing required", schema, `{"a":"x"}`, []string{" required"}},
		{"property type", schema, `{"a":1,"b":1}`, []string{"/a type"}},
		{"additional schema", schema, `{"a":"x","b":"y"}`, []string{"/b type"}},
		{"additional false", schema, `{"a":"x","b":1,"n":{"x":1,"y":2,"z":3}}`, []string{"/n/y additionalProperties", "/n/z additionalProperties"}},
		{"nested type", schema, `{"a":"x","b":1,"n":{"x":"1"}}`, []string{"/n/x type"}},
		{"keywords skip other types", schema, `"text"`, []string{" type"}},
		{"pointer escaping", `{"properties":{"a/b~c":{"type":"string"}}}`, `{"a/b~c":1}`, []string{"/a~1b~0c type"}},
	})
}

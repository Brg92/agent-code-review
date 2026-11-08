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

func TestCompileRejectsInvalidValueKeywords(t *testing.T) {
	for _, schema := range []string{
		`{"items":3}`,
		`{"items":{"type":"nope"}}`,
		`{"enum":[]}`,
		`{"enum":"a"}`,
		`{"pattern":3}`,
		`{"pattern":"("}`,
		`{"minimum":"1"}`,
	} {
		if _, err := Compile([]byte(schema)); err == nil {
			t.Errorf("Compile(%s) succeeded, want error", schema)
		}
	}
}

func TestValueKeywords(t *testing.T) {
	runValidationCases(t, []validationCase{
		{"items ok", `{"items":{"type":"integer"}}`, `[1,2]`, nil},
		{"items bad", `{"items":{"type":"integer"}}`, `[1,"x",true]`, []string{"/1 type", "/2 type"}},
		{"items on non-array", `{"items":{"type":"integer"}}`, `"x"`, nil},
		{"enum ok", `{"enum":["a","b",1]}`, `"b"`, nil},
		{"enum number", `{"enum":[1]}`, `1.0`, nil},
		{"enum bad", `{"enum":["a","b"]}`, `"c"`, []string{" enum"}},
		{"const ok", `{"const":1}`, `1`, nil},
		{"const object", `{"const":{"a":[1,"x"]}}`, `{"a":[1,"x"]}`, nil},
		{"const bad", `{"const":1}`, `2`, []string{" const"}},
		{"const type matters", `{"const":"1"}`, `1`, []string{" const"}},
		{"pattern ok", `{"pattern":"^[a-z]+#[0-9]+$"}`, `"core#3"`, nil},
		{"pattern is a search", `{"pattern":"b"}`, `"abc"`, nil},
		{"pattern bad", `{"pattern":"^[a-z]+$"}`, `"A1"`, []string{" pattern"}},
		{"pattern on non-string", `{"pattern":"^a$"}`, `1`, nil},
		{"minimum ok", `{"minimum":1}`, `1`, nil},
		{"minimum bad", `{"minimum":1}`, `0.5`, []string{" minimum"}},
		{"minimum on non-number", `{"minimum":1}`, `"0"`, nil},
		{"nested", `{"properties":{"l":{"items":{"minimum":0}}}}`, `{"l":[0,-1]}`, []string{"/l/1 minimum"}},
	})
}

func TestCompileRejectsInvalidRefs(t *testing.T) {
	for _, schema := range []string{
		`{"$ref":"other.json#/a"}`,
		`{"$ref":"#/$defs/missing"}`,
		`{"$ref":3}`,
		`{"$defs":3}`,
		`{"$defs":{"a":{"type":"nope"}}}`,
		`{"$defs":{"a":{"$ref":"#/$defs/b"},"b":{"$ref":"#/$defs/a"}},"$ref":"#/$defs/a"}`,
	} {
		if _, err := Compile([]byte(schema)); err == nil {
			t.Errorf("Compile(%s) succeeded, want error", schema)
		}
	}
}

func TestRef(t *testing.T) {
	defs := `"$defs":{"key":{"type":"string","pattern":"^[a-z]+#[0-9]+$"},"a/b":{"minimum":1},` +
		`"node":{"type":"object","properties":{"key":{"$ref":"#/$defs/key"},"children":{"items":{"$ref":"#/$defs/node"}}}}}`
	runValidationCases(t, []validationCase{
		{"ref ok", `{` + defs + `,"$ref":"#/$defs/key"}`, `"core#3"`, nil},
		{"ref bad", `{` + defs + `,"$ref":"#/$defs/key"}`, `"core"`, []string{" pattern"}},
		{"ref with siblings", `{` + defs + `,"$ref":"#/$defs/key","enum":["a#1"]}`, `"b#2"`, []string{" enum"}},
		{"escaped pointer", `{` + defs + `,"$ref":"#/$defs/a~1b"}`, `0`, []string{" minimum"}},
		{"recursive", `{` + defs + `,"$ref":"#/$defs/node"}`, `{"key":"a#1","children":[{"key":"b#2","children":[{"key":"bad"}]}]}`,
			[]string{"/children/0/children/0/key pattern"}},
		{"root ref", `{"properties":{"self":{"$ref":"#"}},"required":["x"]}`, `{"x":1,"self":{}}`, []string{"/self required"}},
	})
}

func TestCompileRejectsInvalidConditionals(t *testing.T) {
	for _, schema := range []string{
		`{"if":3}`,
		`{"if":{"type":"nope"}}`,
		`{"if":true,"then":"a"}`,
		`{"if":true,"else":{"type":"nope"}}`,
	} {
		if _, err := Compile([]byte(schema)); err == nil {
			t.Errorf("Compile(%s) succeeded, want error", schema)
		}
	}
}

func TestConditionals(t *testing.T) {
	schema := `{"if":{"properties":{"state":{"const":"unknown"}}},` +
		`"then":{"required":["reason"]},` +
		`"else":{"properties":{"reason":false}}}`
	runValidationCases(t, []validationCase{
		{"then ok", schema, `{"state":"unknown","reason":"error"}`, nil},
		{"then bad", schema, `{"state":"unknown"}`, []string{" required"}},
		{"else ok", schema, `{"state":"passing"}`, nil},
		{"else bad", schema, `{"state":"passing","reason":"error"}`, []string{"/reason false"}},
		{"if alone", `{"if":{"type":"string"}}`, `1`, nil},
		{"then alone", `{"if":{"type":"string"},"then":{"pattern":"^a"}}`, `"b"`, []string{" pattern"}},
		{"then without if", `{"then":false}`, `1`, nil},
		{"nested", `{"items":{"if":{"minimum":10},"else":{"const":0}}}`, `[10,0,3]`, []string{"/2 const"}},
	})
}

package symbols

import (
	"strings"
	"testing"
)

// The symbols of Kotlin and Java, as the android pack declares them.
const (
	kotlinSpec = `{"packageKeyword":"package",
		"declarations":[{"word":"fun","kind":"function"},{"word":"val","kind":"property"},{"word":"var","kind":"property"},
			{"word":"class","kind":"type"},{"word":"interface","kind":"type"},{"word":"object","kind":"type"},{"word":"typealias","kind":"type"}],
		"hidingModifiers":["private","internal"],"publicModifiers":["public","protected"],"publicByDefault":true,
		"annotation":"@","qualifiers":[".","?."],
		"body":{"open":"{","close":"}"},"parameters":{"open":"(","close":")"},"typeParameters":{"open":"<","close":">"},
		"parameterSeparator":",","parameterType":":","parameterDefault":"="}`
	javaSpec = `{"packageKeyword":"package",
		"declarations":[{"word":"class","kind":"type"},{"word":"interface","kind":"type"},{"word":"enum","kind":"type"},{"word":"record","kind":"type"}],
		"typedMembers":{"ends":["=",";",","]},
		"hidingModifiers":["private"],"publicModifiers":["public","protected"],"publicByDefault":false,"publicMembersIn":["interface"],
		"annotation":"@","qualifiers":["."],
		"body":{"open":"{","close":"}"},"parameters":{"open":"(","close":")"},"typeParameters":{"open":"<","close":">"},
		"parameterSeparator":","}`
)

func mustSpec(t testing.TB, data string) *Spec {
	t.Helper()
	s, err := ParseSpec([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseSpec(t *testing.T) {
	k := mustSpec(t, kotlinSpec)
	if k.Declarations["fun"] != Function || k.Declarations["val"] != Property || k.Declarations["object"] != Type {
		t.Errorf("declarations = %v", k.Declarations)
	}
	if !k.PublicByDefault || !k.HidingModifiers["internal"] || !k.PublicModifiers["protected"] || k.TypedMemberEnds != nil {
		t.Errorf("kotlin visibility = %+v", k)
	}
	if k.ParameterType != ":" || k.ParameterDefault != "=" || !k.Qualifiers["?."] || k.TypeParameters.Close != ">" {
		t.Errorf("kotlin punctuation = %+v", k)
	}
	j := mustSpec(t, javaSpec)
	if j.PublicByDefault || !j.PublicMembersIn["interface"] || len(j.TypedMemberEnds) != 3 || j.ParameterType != "" || j.ParameterDefault != "" {
		t.Errorf("java = %+v", j)
	}
}

func TestParseSpecRejects(t *testing.T) {
	tests := []struct{ name, from, to, want string }{
		{"unknown field", `"publicByDefault":true`, `"publicByDefault":true,"exported":[]`, "unknown field"},
		{"missing package keyword", `"packageKeyword":"package",`, ``, "packageKeyword"},
		{"missing publicByDefault", `"publicByDefault":true,`, ``, "publicByDefault"},
		{"empty declarations", `"declarations":[{"word":"fun","kind":"function"},{"word":"val","kind":"property"},{"word":"var","kind":"property"},
			{"word":"class","kind":"type"},{"word":"interface","kind":"type"},{"word":"object","kind":"type"},{"word":"typealias","kind":"type"}]`, `"declarations":[]`, "declarations"},
		{"unknown kind", `"kind":"function"`, `"kind":"method"`, "method"},
		{"declaration word twice", `{"word":"var","kind":"property"}`, `{"word":"val","kind":"property"}`, "twice"},
		{"declaration not a word", `"word":"fun"`, `"word":"f n"`, "word"},
		{"modifier not a word", `["private","internal"]`, `["private","inter nal"]`, "hidingModifiers"},
		{"modifier twice", `["private","internal"]`, `["private","private"]`, "hidingModifiers"},
		{"hiding and public", `["public","protected"]`, `["public","internal"]`, "internal"},
		{"blank annotation", `"annotation":"@"`, `"annotation":""`, "annotation"},
		{"qualifier with letters", `[".","?."]`, `[".","a."]`, "qualifiers"},
		{"no qualifiers", `[".","?."]`, `[]`, "qualifiers"},
		{"bracket of two characters", `"typeParameters":{"open":"<","close":">"}`, `"typeParameters":{"open":"<<","close":">>"}`, "typeParameters"},
		{"same open and close", `"body":{"open":"{","close":"}"}`, `"body":{"open":"{","close":"{"}`, "body"},
		{"type brackets are the parameters", `"typeParameters":{"open":"<","close":">"}`, `"typeParameters":{"open":"(","close":")"}`, "typeParameters"},
		{"missing separator", `"parameterSeparator":",",`, ``, "parameterSeparator"},
		{"blank parameter type", `"parameterType":":"`, `"parameterType":" "`, "parameterType"},
		{"typed members without ends", `"publicByDefault":true`, `"publicByDefault":true,"typedMembers":{"ends":[]}`, "ends"},
		{"trailing data", `"parameterDefault":"="}`, `"parameterDefault":"="} {}`, "trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(kotlinSpec, tt.from) {
				t.Fatalf("the kotlin spec has no %q", tt.from)
			}
			_, err := ParseSpec([]byte(strings.Replace(kotlinSpec, tt.from, tt.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

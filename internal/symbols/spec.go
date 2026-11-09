// Package symbols finds the declarations of a source file in its lexer
// tokens and compares them at base and at head, giving the public API a
// change request adds, removes or changes. Which words declare what, and
// which modifiers hide a declaration, come from the symbols of a pack
// language: the package knows no language.
package symbols

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/Brg92/agent-code-review/internal/lexer"
)

// Kind is the kind of symbol a declaration declares.
type Kind string

// The kinds of symbols.
const (
	// Function has a parameter list.
	Function Kind = "function"
	// Property is a value.
	Property Kind = "property"
	// Type is a type, whose body holds members.
	Type Kind = "type"
)

// Spec describes how the declarations of a language are found in its
// tokens. Its JSON form is the symbols of a language in
// schemas/pack-manifest.schema.json.
type Spec struct {
	PackageKeyword string
	// Declarations maps each declaration word to the kind it declares.
	Declarations map[string]Kind
	// TypedMemberEnds, when set, lets members of a type body be declared
	// without a declaration word, as <modifiers> <type> <name>: a property
	// when one of the ends follows the name. Nil: no such members.
	TypedMemberEnds []string
	HidingModifiers map[string]bool
	PublicModifiers map[string]bool
	PublicByDefault bool
	// PublicMembersIn holds the declaration words of the types whose
	// members are public without a public modifier.
	PublicMembersIn map[string]bool
	Annotation      string
	Qualifiers      map[string]bool
	Body            lexer.Bracket
	Parameters      lexer.Bracket
	TypeParameters  lexer.Bracket
	// ParameterSeparator separates two parameters. ParameterType, when
	// set, follows the name of a parameter; ParameterDefault, when set,
	// starts its default value.
	ParameterSeparator, ParameterType, ParameterDefault string
}

var (
	word    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	marker  = regexp.MustCompile(`^[^\sA-Za-z0-9_]+$`)
	bracket = regexp.MustCompile(`^[^\sA-Za-z0-9_]$`)
	kinds   = map[Kind]bool{Function: true, Property: true, Type: true}
)

type rawSpec struct {
	PackageKeyword *string `json:"packageKeyword"`
	Declarations   *[]struct {
		Word *string `json:"word"`
		Kind *Kind   `json:"kind"`
	} `json:"declarations"`
	TypedMembers *struct {
		Ends []string `json:"ends"`
	} `json:"typedMembers"`
	HidingModifiers    *[]string      `json:"hidingModifiers"`
	PublicModifiers    *[]string      `json:"publicModifiers"`
	PublicByDefault    *bool          `json:"publicByDefault"`
	PublicMembersIn    []string       `json:"publicMembersIn"`
	Annotation         *string        `json:"annotation"`
	Qualifiers         *[]string      `json:"qualifiers"`
	Body               *lexer.Bracket `json:"body"`
	Parameters         *lexer.Bracket `json:"parameters"`
	TypeParameters     *lexer.Bracket `json:"typeParameters"`
	ParameterSeparator *string        `json:"parameterSeparator"`
	ParameterType      *string        `json:"parameterType"`
	ParameterDefault   *string        `json:"parameterDefault"`
}

// ParseSpec decodes and checks the symbols of a language. Unknown fields
// are rejected.
func ParseSpec(data []byte) (*Spec, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw rawSpec
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("symbols: decode: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("symbols: trailing data after the document")
	}
	s, err := compileSpec(raw)
	if err != nil {
		return nil, fmt.Errorf("symbols: %w", err)
	}
	return s, nil
}

func compileSpec(raw rawSpec) (*Spec, error) {
	s := &Spec{Declarations: map[string]Kind{}}
	required := []struct {
		name string
		set  bool
	}{
		{"packageKeyword", raw.PackageKeyword != nil}, {"declarations", raw.Declarations != nil},
		{"hidingModifiers", raw.HidingModifiers != nil}, {"publicModifiers", raw.PublicModifiers != nil},
		{"publicByDefault", raw.PublicByDefault != nil}, {"annotation", raw.Annotation != nil},
		{"qualifiers", raw.Qualifiers != nil}, {"body", raw.Body != nil}, {"parameters", raw.Parameters != nil},
		{"typeParameters", raw.TypeParameters != nil}, {"parameterSeparator", raw.ParameterSeparator != nil},
	}
	for _, r := range required {
		if !r.set {
			return nil, fmt.Errorf("%s is required", r.name)
		}
	}
	if !word.MatchString(*raw.PackageKeyword) {
		return nil, fmt.Errorf("packageKeyword %q is not a word", *raw.PackageKeyword)
	}
	s.PackageKeyword, s.PublicByDefault = *raw.PackageKeyword, *raw.PublicByDefault
	if len(*raw.Declarations) == 0 {
		return nil, errors.New("declarations is empty")
	}
	for _, d := range *raw.Declarations {
		switch {
		case d.Word == nil || !word.MatchString(*d.Word):
			return nil, errors.New("declarations: every entry needs a word")
		case d.Kind == nil || !kinds[*d.Kind]:
			return nil, fmt.Errorf("declarations: %s: kind %v must be function, property or type", *d.Word, deref(d.Kind))
		}
		if _, ok := s.Declarations[*d.Word]; ok {
			return nil, fmt.Errorf("declarations: %q is listed twice", *d.Word)
		}
		s.Declarations[*d.Word] = *d.Kind
	}
	if t := raw.TypedMembers; t != nil {
		if len(t.Ends) == 0 {
			return nil, errors.New("typedMembers: ends is empty")
		}
		if _, err := set("typedMembers ends", t.Ends, marker); err != nil {
			return nil, err
		}
		s.TypedMemberEnds = t.Ends
	}
	var err error
	for _, f := range []struct {
		name string
		list []string
		form *regexp.Regexp
		dst  *map[string]bool
	}{
		{"hidingModifiers", *raw.HidingModifiers, word, &s.HidingModifiers},
		{"publicModifiers", *raw.PublicModifiers, word, &s.PublicModifiers},
		{"publicMembersIn", raw.PublicMembersIn, word, &s.PublicMembersIn},
		{"qualifiers", *raw.Qualifiers, marker, &s.Qualifiers},
	} {
		if *f.dst, err = set(f.name, f.list, f.form); err != nil {
			return nil, err
		}
	}
	if len(s.Qualifiers) == 0 {
		return nil, errors.New("qualifiers is empty")
	}
	for m := range s.HidingModifiers {
		if s.PublicModifiers[m] {
			return nil, fmt.Errorf("modifier %q both hides and makes public", m)
		}
	}
	if !marker.MatchString(*raw.Annotation) {
		return nil, fmt.Errorf("annotation %q is not punctuation", *raw.Annotation)
	}
	s.Annotation = *raw.Annotation
	pairs := map[string]bool{}
	for _, b := range []struct {
		name string
		raw  *lexer.Bracket
		dst  *lexer.Bracket
	}{
		{"body", raw.Body, &s.Body}, {"parameters", raw.Parameters, &s.Parameters}, {"typeParameters", raw.TypeParameters, &s.TypeParameters},
	} {
		if !bracket.MatchString(b.raw.Open) || !bracket.MatchString(b.raw.Close) || b.raw.Open == b.raw.Close {
			return nil, fmt.Errorf("%s: not a pair of distinct punctuation characters", b.name)
		}
		if pairs[b.raw.Open] || pairs[b.raw.Close] {
			return nil, fmt.Errorf("%s: shares a character with another pair", b.name)
		}
		pairs[b.raw.Open], pairs[b.raw.Close] = true, true
		*b.dst = *b.raw
	}
	for _, m := range []struct {
		name     string
		v        *string
		required bool
		dst      *string
	}{
		{"parameterSeparator", raw.ParameterSeparator, true, &s.ParameterSeparator},
		{"parameterType", raw.ParameterType, false, &s.ParameterType},
		{"parameterDefault", raw.ParameterDefault, false, &s.ParameterDefault},
	} {
		if m.v == nil {
			continue
		}
		if !marker.MatchString(*m.v) {
			return nil, fmt.Errorf("%s %q is not punctuation", m.name, *m.v)
		}
		*m.dst = *m.v
	}
	return s, nil
}

// set returns the distinct strings of a list, all of the given form.
func set(name string, list []string, form *regexp.Regexp) (map[string]bool, error) {
	out := map[string]bool{}
	for _, s := range list {
		if !form.MatchString(s) || out[s] {
			return nil, fmt.Errorf("%s: %q is malformed or listed twice", name, s)
		}
		out[s] = true
	}
	return out, nil
}

func deref(k *Kind) string {
	if k == nil {
		return "<none>"
	}
	return string(*k)
}

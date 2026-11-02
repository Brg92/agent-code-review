// Package jsonschema validates JSON documents against the subset of JSON
// Schema draft 2020-12 that the schemas in this repository use. It is test
// support code only.
package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Issue is one way a document does not match its schema.
type Issue struct {
	// Path is the JSON pointer of the value; the empty string is the root.
	Path    string
	Keyword string
	Message string
}

func (i Issue) String() string { return i.Path + ": " + i.Keyword + ": " + i.Message }

// Schema is a compiled schema.
type Schema struct {
	root     any
	patterns map[string]*regexp.Regexp
	refs     map[string]any
}

// annotations are keywords that do not affect validation.
var annotations = map[string]bool{
	"$schema": true, "$id": true, "$comment": true, "title": true, "description": true,
}

var typeNames = map[string]bool{
	"null": true, "boolean": true, "object": true, "array": true, "number": true, "integer": true, "string": true,
}

// Compile parses a schema and rejects keywords it does not support.
func Compile(data []byte) (*Schema, error) {
	root, err := decode(data)
	if err != nil {
		return nil, err
	}
	s := &Schema{root: root, patterns: map[string]*regexp.Regexp{}, refs: map[string]any{}}
	if err := s.check(root, ""); err != nil {
		return nil, err
	}
	if err := s.resolveRefs(); err != nil {
		return nil, err
	}
	return s, nil
}

// resolveRefs resolves every $ref, which must be a JSON pointer into the
// same schema, and rejects chains of references that loop.
func (s *Schema) resolveRefs() error {
	for ref := range s.refs {
		target, err := resolve(s.root, ref)
		if err != nil {
			return err
		}
		s.refs[ref] = target
	}
	for ref := range s.refs {
		seen := map[string]bool{}
		for next := ref; next != ""; {
			if seen[next] {
				return fmt.Errorf("$ref %q: references loop", ref)
			}
			seen[next] = true
			target, _ := s.refs[next].(map[string]any)
			next, _ = target["$ref"].(string)
		}
	}
	return nil
}

func resolve(root any, ref string) (any, error) {
	if !strings.HasPrefix(ref, "#") {
		return nil, fmt.Errorf("$ref %q: only references within the schema are supported", ref)
	}
	v := root
	pointer := strings.TrimPrefix(ref, "#")
	if pointer == "" {
		return v, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("$ref %q: not a JSON pointer", ref)
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("$ref %q: not found", ref)
		}
		if v, ok = m[token]; !ok {
			return nil, fmt.Errorf("$ref %q: not found", ref)
		}
	}
	switch v.(type) {
	case bool, map[string]any:
		return v, nil
	}
	return nil, fmt.Errorf("$ref %q: does not point to a schema", ref)
}

// Validate returns the issues of a document. The error reports a document
// that is not valid JSON.
func (s *Schema) Validate(doc []byte) ([]Issue, error) {
	v, err := decode(doc)
	if err != nil {
		return nil, err
	}
	var issues []Issue
	s.validate(s.root, v, "", &issues)
	return issues, nil
}

func decode(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("decode JSON: trailing data after the document")
	}
	return v, nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// check verifies that a schema only uses supported keywords with values of
// the right shape. at is the JSON pointer of the schema, for error messages.
func (sc *Schema) check(schema any, at string) error {
	if _, ok := schema.(bool); ok {
		return nil
	}
	s, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: a schema must be an object or a boolean", at)
	}
	for _, k := range sortedKeys(s) {
		v := s[k]
		here := at + "/" + escape(k)
		switch {
		case annotations[k]:
		case k == "type":
			if err := checkType(v); err != nil {
				return fmt.Errorf("%s: %w", here, err)
			}
		case k == "required":
			if _, err := stringList(v); err != nil {
				return fmt.Errorf("%s: %w", here, err)
			}
		case k == "properties":
			props, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: must be an object", here)
			}
			for _, name := range sortedKeys(props) {
				if err := sc.check(props[name], here+"/"+escape(name)); err != nil {
					return err
				}
			}
		case k == "additionalProperties" || k == "items":
			if err := sc.check(v, here); err != nil {
				return err
			}
		case k == "$defs":
			defs, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: must be an object", here)
			}
			for _, name := range sortedKeys(defs) {
				if err := sc.check(defs[name], here+"/"+escape(name)); err != nil {
					return err
				}
			}
		case k == "$ref":
			ref, ok := v.(string)
			if !ok {
				return fmt.Errorf("%s: must be a string", here)
			}
			sc.refs[ref] = nil
		case k == "enum":
			if list, ok := v.([]any); !ok || len(list) == 0 {
				return fmt.Errorf("%s: must be a non-empty array", here)
			}
		case k == "const":
		case k == "pattern":
			p, ok := v.(string)
			if !ok {
				return fmt.Errorf("%s: must be a string", here)
			}
			re, err := regexp.Compile(p)
			if err != nil {
				return fmt.Errorf("%s: %w", here, err)
			}
			sc.patterns[p] = re
		case k == "minimum":
			if _, ok := v.(json.Number); !ok {
				return fmt.Errorf("%s: must be a number", here)
			}
		default:
			return fmt.Errorf("%s: unsupported keyword", here)
		}
	}
	return nil
}

func checkType(v any) error {
	names := []any{v}
	if list, ok := v.([]any); ok {
		names = list
	}
	for _, n := range names {
		name, ok := n.(string)
		if !ok || !typeNames[name] {
			return fmt.Errorf("unknown type %v", n)
		}
	}
	return nil
}

func stringList(v any) ([]string, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("must be an array of strings")
	}
	out := make([]string, len(list))
	for i, e := range list {
		s, ok := e.(string)
		if !ok {
			return nil, errors.New("must be an array of strings")
		}
		out[i] = s
	}
	return out, nil
}

// escape encodes a JSON pointer token.
func escape(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

func (s *Schema) validate(schema, v any, path string, issues *[]Issue) {
	add := func(p, keyword, format string, args ...any) {
		*issues = append(*issues, Issue{Path: p, Keyword: keyword, Message: fmt.Sprintf(format, args...)})
	}
	if b, ok := schema.(bool); ok {
		if !b {
			add(path, "false", "no value is allowed")
		}
		return
	}
	sch := schema.(map[string]any)

	if ref, ok := sch["$ref"].(string); ok {
		s.validate(s.refs[ref], v, path, issues)
	}

	if t, ok := sch["type"]; ok && !hasType(t, v) {
		add(path, "type", "want %v, got %s", t, typeOf(v))
	}

	if list, ok := sch["enum"].([]any); ok && !containsEqual(list, v) {
		add(path, "enum", "value is not one of %v", list)
	}
	if c, ok := sch["const"]; ok && !equal(c, v) {
		add(path, "const", "want %v", c)
	}
	switch v := v.(type) {
	case string:
		if p, ok := sch["pattern"].(string); ok && !s.patterns[p].MatchString(v) {
			add(path, "pattern", "%q does not match %s", v, p)
		}
	case json.Number:
		if m, ok := sch["minimum"].(json.Number); ok && number(v) < number(m) {
			add(path, "minimum", "%s is less than %s", v, m)
		}
	case []any:
		if items, ok := sch["items"]; ok {
			for i, e := range v {
				s.validate(items, e, fmt.Sprintf("%s/%d", path, i), issues)
			}
		}
	}

	obj, isObject := v.(map[string]any)
	if !isObject {
		return
	}
	if req, ok := sch["required"]; ok {
		names, _ := stringList(req)
		for _, name := range names {
			if _, ok := obj[name]; !ok {
				add(path, "required", "missing property %q", name)
			}
		}
	}
	props, _ := sch["properties"].(map[string]any)
	extra, hasExtra := sch["additionalProperties"]
	for _, k := range sortedKeys(obj) {
		here := path + "/" + escape(k)
		if p, ok := props[k]; ok {
			s.validate(p, obj[k], here, issues)
			continue
		}
		if !hasExtra {
			continue
		}
		if b, ok := extra.(bool); ok {
			if !b {
				add(here, "additionalProperties", "property is not allowed")
			}
			continue
		}
		s.validate(extra, obj[k], here, issues)
	}
}

func number(n json.Number) float64 {
	f, _ := n.Float64()
	return f
}

func containsEqual(list []any, v any) bool {
	for _, e := range list {
		if equal(e, v) {
			return true
		}
	}
	return false
}

// equal compares two decoded JSON values; numbers compare by value.
func equal(a, b any) bool {
	switch a := a.(type) {
	case json.Number:
		bn, ok := b.(json.Number)
		return ok && number(a) == number(bn)
	case []any:
		bl, ok := b.([]any)
		if !ok || len(a) != len(bl) {
			return false
		}
		for i := range a {
			if !equal(a[i], bl[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bm, ok := b.(map[string]any)
		if !ok || len(a) != len(bm) {
			return false
		}
		for k, av := range a {
			bv, ok := bm[k]
			if !ok || !equal(av, bv) {
				return false
			}
		}
		return true
	}
	return a == b
}

func hasType(t, v any) bool {
	names := []any{t}
	if list, ok := t.([]any); ok {
		names = list
	}
	for _, n := range names {
		name := n.(string)
		got := typeOf(v)
		if name == got || name == "number" && got == "integer" {
			return true
		}
	}
	return false
}

func typeOf(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case json.Number:
		if f, err := v.Float64(); err == nil && f == math.Trunc(f) && !math.IsInf(f, 0) {
			return "integer"
		}
		return "number"
	}
	return fmt.Sprintf("%T", v)
}

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Files of the neutral criteria layout.
const (
	ConfigFile   = ".agent-code-review/config.json"
	CriteriaFile = ".agent-code-review/criteria.md"
)

// RepoConfig is a repository configuration, .agent-code-review/config.json,
// as defined by schemas/config.schema.json. Absent lists are nil.
type RepoConfig struct {
	Profile       string
	Packs         []string
	Variants      []string
	Dimensions    map[string]string
	DisabledRules []string
	// RuleOptions holds parameter values by rule id, then by parameter.
	RuleOptions map[string]map[string]int
	Noise       []string
	Cascade     CascadeConfig
	// SharedLibraries are the project keys of the libraries the
	// repository consumes.
	SharedLibraries []string
}

// CascadeConfig holds the cascade limits a repository sets; nil is the
// default.
type CascadeConfig struct {
	MaxDepth          *int `json:"maxDepth,omitempty"`
	MaxChangeRequests *int `json:"maxChangeRequests,omitempty"`
}

// Limits returns the limits, with the defaults where none is set.
func (c CascadeConfig) Limits() Limits {
	l := DefaultLimits()
	if c.MaxDepth != nil {
		l.MaxDepth = *c.MaxDepth
	}
	if c.MaxChangeRequests != nil {
		l.MaxChangeRequests = *c.MaxChangeRequests
	}
	return l
}

// CriteriaError reports a criteria file of the target branch that cannot
// be used: every invalid field, by JSON pointer. The review stops on it,
// without falling back to the defaults.
type CriteriaError struct {
	File   string
	Errors []FieldError
}

func (e *CriteriaError) Error() string {
	var b strings.Builder
	b.WriteString("invalid repository criteria " + e.File + ":")
	for _, f := range e.Errors {
		b.WriteString("\n" + f.Pointer + ": " + f.Message)
	}
	return b.String()
}

var (
	lowerID     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	variantName = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	ruleIDForm  = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.[a-z0-9]+(?:-[a-z0-9]+)*)*$`)
	globForm    = regexp.MustCompile(`^[^/\\]+(?:/[^/\\]+)*$`)
	// projectKeyForm is <forge>:<host>/<project>, without a number.
	projectKeyForm = regexp.MustCompile(`^(?:github|gitlab):[a-z0-9.-]+(?::[0-9]+)?/[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+$`)
)

// fields collects the problems of one JSON document.
type fields struct{ errs []FieldError }

func (f *fields) add(pointer, format string, args ...any) {
	f.errs = append(f.errs, FieldError{Pointer: pointer, Message: fmt.Sprintf(format, args...)})
}

func (f *fields) err(file string) error {
	if len(f.errs) == 0 {
		return nil
	}
	sort.SliceStable(f.errs, func(i, j int) bool { return f.errs[i].Pointer < f.errs[j].Pointer })
	return &CriteriaError{File: file, Errors: f.errs}
}

// decodeObject decodes a JSON object into its members, numbers kept as
// written.
func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("not a JSON object: %v", err)
	}
	if obj == nil {
		return nil, fmt.Errorf("not a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data after the document")
	}
	return obj, nil
}

// integer decodes a JSON integer; a number with an integral value, such
// as 1.0, is one, as in JSON Schema.
func integer(raw json.RawMessage) (int, bool) {
	var n json.Number
	// A JSON string that holds a number also decodes into a json.Number.
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] == '"' || json.Unmarshal(t, &n) != nil {
		return 0, false
	}
	if i, err := strconv.ParseInt(n.String(), 10, 0); err == nil {
		return int(i), true
	}
	f, err := n.Float64()
	if err != nil || f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0, false
	}
	return int(f), true
}

// strings decodes a list of strings that match form, adding an error for
// each that does not.
func (f *fields) strings(at string, raw json.RawMessage, form *regexp.Regexp, what string) []string {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		f.add(at, "must be an array of %s", what)
		return nil
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		var s string
		if err := json.Unmarshal(item, &s); err != nil || !form.MatchString(s) {
			f.add(fmt.Sprintf("%s/%d", at, i), "%s is not %s", item, what)
			continue
		}
		out = append(out, s)
	}
	return out
}

// DecodeRepoConfig decodes and checks a repository configuration read
// from file. It fails with a *CriteriaError that lists every invalid
// field.
func DecodeRepoConfig(file string, data []byte) (RepoConfig, error) {
	var c RepoConfig
	var f fields
	obj, err := decodeObject(data)
	if err != nil {
		f.add("", "%v", err)
		return c, f.err(file)
	}
	if raw, ok := obj["schema"]; !ok {
		f.add("/schema", "is required")
	} else if v, ok := integer(raw); !ok || v != 1 {
		f.add("/schema", "%s is not 1", raw)
	}
	for name, raw := range obj {
		at := "/" + name
		switch name {
		case "schema":
		case "profile":
			if err := json.Unmarshal(raw, &c.Profile); err != nil || c.Profile != "app" && c.Profile != "library" {
				f.add(at, "%s is not app or library", raw)
				c.Profile = ""
			}
		case "packs":
			c.Packs = f.strings(at, raw, lowerID, "a pack id")
		case "variants":
			c.Variants = f.strings(at, raw, variantName, "a variant name")
		case "disabledRules":
			c.DisabledRules = f.strings(at, raw, ruleIDForm, "a rule id")
		case "noise":
			c.Noise = f.strings(at, raw, globForm, "a glob")
		case "sharedLibraries":
			c.SharedLibraries = f.strings(at, raw, projectKeyForm, "a project key")
		case "dimensions":
			c.Dimensions = f.dimensions(at, raw)
		case "ruleOptions":
			c.RuleOptions = f.ruleOptions(at, raw)
		case "cascade":
			c.Cascade = f.cascade(at, raw)
		default:
			f.add(at, "is not a field of the configuration")
		}
	}
	if err := f.err(file); err != nil {
		return RepoConfig{}, err
	}
	return c, nil
}

func (f *fields) dimensions(at string, raw json.RawMessage) map[string]string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		f.add(at, "must be an object")
		return nil
	}
	out := map[string]string{}
	for id, v := range obj {
		var s string
		if !lowerID.MatchString(id) || json.Unmarshal(v, &s) != nil || !lowerID.MatchString(s) {
			f.add(at+"/"+id, "must be a lowercase dimension id with a lowercase value")
			continue
		}
		out[id] = s
	}
	return out
}

func (f *fields) ruleOptions(at string, raw json.RawMessage) map[string]map[string]int {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		f.add(at, "must be an object")
		return nil
	}
	out := map[string]map[string]int{}
	for id, v := range obj {
		var params map[string]json.RawMessage
		if !ruleIDForm.MatchString(id) || json.Unmarshal(v, &params) != nil || params == nil {
			f.add(at+"/"+id, "must be a rule id with an object of parameter values")
			continue
		}
		values := map[string]int{}
		for name, pv := range params {
			n, ok := integer(pv)
			if !ok {
				f.add(at+"/"+id+"/"+name, "%s is not an integer", pv)
				continue
			}
			values[name] = n
		}
		out[id] = values
	}
	return out
}

func (f *fields) cascade(at string, raw json.RawMessage) CascadeConfig {
	var c CascadeConfig
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		f.add(at, "must be an object")
		return c
	}
	for name, v := range obj {
		n, ok := integer(v)
		switch name {
		case "maxDepth":
			if !ok || n < 0 {
				f.add(at+"/"+name, "%s is not an integer of at least 0", v)
				continue
			}
			c.MaxDepth = &n
		case "maxChangeRequests":
			if !ok || n < 1 {
				f.add(at+"/"+name, "%s is not an integer of at least 1", v)
				continue
			}
			c.MaxChangeRequests = &n
		default:
			f.add(at+"/"+name, "is not a cascade limit")
		}
	}
	return c
}

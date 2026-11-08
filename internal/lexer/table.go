package lexer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Table describes the lexical structure of one language. Its JSON form is
// defined by schemas/lexer-table.schema.json.
type Table struct {
	Schema           int               `json:"schema"`
	Language         string            `json:"language"`
	LineComment      string            `json:"lineComment,omitempty"`
	BlockComment     *BlockCommentForm `json:"blockComment,omitempty"`
	DocComment       string            `json:"docComment,omitempty"`
	Strings          []StringForm      `json:"strings,omitempty"`
	Char             *CharForm         `json:"char,omitempty"`
	QuotedIdentifier string            `json:"quotedIdentifier,omitempty"`
	Templates        *Templates        `json:"templates,omitempty"`
	IdentifierChars  string            `json:"identifierChars,omitempty"`
	Keywords         []string          `json:"keywords"`
	SoftKeywords     []string          `json:"softKeywords"`
	Operators        []string          `json:"operators,omitempty"`

	keywords     map[string]bool
	softKeywords map[string]bool
}

// BlockCommentForm is a comment between an opener and a closer.
type BlockCommentForm struct {
	Open   string `json:"open"`
	Close  string `json:"close"`
	Nested bool   `json:"nested"`
}

// UnmarshalJSON decodes a block comment; nested is required.
func (b *BlockCommentForm) UnmarshalJSON(data []byte) error {
	var raw struct {
		Open   string `json:"open"`
		Close  string `json:"close"`
		Nested *bool  `json:"nested"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	if raw.Nested == nil {
		return errors.New("blockComment: nested is required")
	}
	*b = BlockCommentForm{Open: raw.Open, Close: raw.Close, Nested: *raw.Nested}
	return nil
}

// StringForm is one form of string literal.
type StringForm struct {
	Delimiter   string `json:"delimiter"`
	Escape      string `json:"escape,omitempty"`
	Raw         bool   `json:"raw,omitempty"`
	Multiline   bool   `json:"multiline,omitempty"`
	Templates   bool   `json:"templates,omitempty"`
	GreedyClose bool   `json:"greedyClose,omitempty"`
}

// CharForm is the character literal.
type CharForm struct {
	Delimiter string `json:"delimiter"`
	Escape    string `json:"escape,omitempty"`
}

// Templates are the markers of template expressions inside strings.
type Templates struct {
	Identifier string `json:"identifier"`
	Open       string `json:"open"`
	Close      string `json:"close"`
}

var (
	languageID = regexp.MustCompile(`^[a-z0-9-]+$`)
	word       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	operator   = regexp.MustCompile(`^[^\sA-Za-z0-9_]{2,}$`)
	marker     = regexp.MustCompile(`^\S+$`)
)

// LoadTable decodes and checks a lexer table. Unknown fields are rejected.
// String forms and operators are sorted longest first, the order in which
// the lexer tries them.
func LoadTable(data []byte) (*Table, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw struct {
		Table
		Schema       *int      `json:"schema"`
		Keywords     *[]string `json:"keywords"`
		SoftKeywords *[]string `json:"softKeywords"`
		// Optional markers, as pointers to tell an empty one from an
		// absent one.
		LineComment      *string `json:"lineComment"`
		DocComment       *string `json:"docComment"`
		QuotedIdentifier *string `json:"quotedIdentifier"`
	}
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("lexer table: decode: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("lexer table: trailing data after the document")
	}
	t := raw.Table
	if raw.Schema == nil || *raw.Schema != 1 {
		return nil, errors.New("lexer table: schema must be 1")
	}
	for name, list := range map[string]*[]string{"keywords": raw.Keywords, "softKeywords": raw.SoftKeywords} {
		if list == nil {
			return nil, fmt.Errorf("lexer table: %s is required", name)
		}
	}
	t.Schema, t.Keywords, t.SoftKeywords = *raw.Schema, *raw.Keywords, *raw.SoftKeywords
	for name, m := range map[string]*string{"lineComment": raw.LineComment, "docComment": raw.DocComment, "quotedIdentifier": raw.QuotedIdentifier} {
		if m != nil && !marker.MatchString(*m) {
			return nil, fmt.Errorf("lexer table: %s %q is not a marker", name, *m)
		}
	}
	t.LineComment, t.DocComment, t.QuotedIdentifier = deref(raw.LineComment), deref(raw.DocComment), deref(raw.QuotedIdentifier)
	if err := t.check(); err != nil {
		return nil, fmt.Errorf("lexer table %q: %w", t.Language, err)
	}
	sort.SliceStable(t.Strings, func(i, j int) bool { return len(t.Strings[i].Delimiter) > len(t.Strings[j].Delimiter) })
	sort.SliceStable(t.Operators, func(i, j int) bool { return len(t.Operators[i]) > len(t.Operators[j]) })
	t.keywords, t.softKeywords = set(t.Keywords), set(t.SoftKeywords)
	return &t, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func set(words []string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func (t *Table) check() error {
	if !languageID.MatchString(t.Language) {
		return fmt.Errorf("language %q is not a lowercase identifier", t.Language)
	}
	if err := distinct("keyword", append(append([]string{}, t.Keywords...), t.SoftKeywords...), word); err != nil {
		return err
	}
	if err := distinct("operator", t.Operators, operator); err != nil {
		return err
	}
	if b := t.BlockComment; b != nil {
		if !marker.MatchString(b.Open) || !marker.MatchString(b.Close) {
			return errors.New("blockComment needs an open and a close marker")
		}
	}
	if t.DocComment != "" && (t.BlockComment == nil ||
		!strings.HasPrefix(t.DocComment, t.BlockComment.Open) || t.DocComment == t.BlockComment.Open) {
		return fmt.Errorf("docComment %q must extend the block comment opener", t.DocComment)
	}
	var delims []string
	templated := false
	for _, s := range t.Strings {
		delims = append(delims, s.Delimiter)
		if err := checkEscape(s.Escape); err != nil {
			return err
		}
		templated = templated || s.Templates
	}
	if err := distinct("delimiter", delims, marker); err != nil {
		return err
	}
	if c := t.Char; c != nil {
		if !marker.MatchString(c.Delimiter) {
			return fmt.Errorf("char delimiter %q is not a marker", c.Delimiter)
		}
		if err := checkEscape(c.Escape); err != nil {
			return err
		}
	}
	if tm := t.Templates; tm != nil {
		if !marker.MatchString(tm.Identifier) || !marker.MatchString(tm.Open) || !marker.MatchString(tm.Close) {
			return errors.New("templates needs identifier, open and close markers")
		}
	} else if templated {
		return errors.New("a string form has templates but the table has no templates")
	}
	for _, r := range t.IdentifierChars {
		if unicode.IsSpace(r) || r == utf8.RuneError {
			return fmt.Errorf("identifierChars %q has a blank or invalid character", t.IdentifierChars)
		}
	}
	return nil
}

func checkEscape(e string) error {
	if e != "" && (utf8.RuneCountInString(e) != 1 || !marker.MatchString(e)) {
		return fmt.Errorf("escape %q is not a single non-blank character", e)
	}
	return nil
}

func distinct(what string, values []string, pattern *regexp.Regexp) error {
	seen := map[string]bool{}
	for _, v := range values {
		if !pattern.MatchString(v) {
			return fmt.Errorf("%s %q is not valid", what, v)
		}
		if seen[v] {
			return fmt.Errorf("%s %q is listed twice", what, v)
		}
		seen[v] = true
	}
	return nil
}

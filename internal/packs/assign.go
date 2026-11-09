package packs

import (
	"fmt"
	"path"
	"strings"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// Reasons a changed file is not verified.
const (
	ReasonNoise               = "noise"
	ReasonUnsupportedLanguage = "unsupported-language"
)

// Assignment is the pack and language that review a changed file, or why
// none does.
type Assignment struct {
	Path     string
	Pack     *Pack
	Language *Language
	// Test reports whether the file is a test source of an active pack.
	Test bool
	// Dimensions holds the value of each file dimension; nil when no pack
	// reviews the file.
	Dimensions map[string]string
	// NotVerified is set when no pack reviews the file.
	NotVerified *rules.NotVerified
}

// Assign assigns a changed file to the first active pack with a language
// for its extension. A file matching a noise glob of an active pack is
// noise; a file no active pack covers is not verified as
// unsupported-language, and the detail tells whether a pack of the set
// covers it without applying to the repository.
func (s *Set) Assign(active []*Pack, filePath string) Assignment {
	a := Assignment{Path: filePath}
	for _, p := range active {
		for _, g := range p.Noise {
			if g.Match(filePath) {
				a.NotVerified = &rules.NotVerified{Path: filePath, Reason: ReasonNoise, Detail: "matches a noise glob of the " + p.ID + " pack"}
				return a
			}
		}
	}
	ext := path.Ext(strings.TrimPrefix(path.Base(filePath), "."))
	for _, p := range active {
		if l := p.language(ext); l != nil {
			a.Pack, a.Language = p, l
			for _, q := range active {
				for _, g := range q.TestFiles {
					a.Test = a.Test || g.Match(filePath)
				}
			}
			a.Dimensions = ResolveFileDimensions(active, filePath)
			return a
		}
	}
	detail := fmt.Sprintf("no pack covers %s files", ext)
	if ext == "" {
		detail = "no pack covers files without an extension"
	}
	for _, p := range s.Packs {
		if p.language(ext) != nil {
			detail = fmt.Sprintf("the %s pack covers %s files but does not apply to this repository", p.ID, ext)
			break
		}
	}
	a.NotVerified = &rules.NotVerified{Path: filePath, Reason: ReasonUnsupportedLanguage, Detail: detail}
	return a
}

func (p *Pack) language(ext string) *Language {
	if ext == "" {
		return nil
	}
	for i := range p.Languages {
		for _, e := range p.Languages[i].Extensions {
			if e == ext {
				return &p.Languages[i]
			}
		}
	}
	return nil
}

// File returns the input of the rule engine for an assigned file. It must
// only be called when the file has a pack.
func (a Assignment) File(source string, added map[int]bool) rules.File {
	return rules.File{Path: a.Path, Language: a.Language.ID, Table: a.Language.Table, Source: source, Test: a.Test, Added: added}
}

// Context is what the conditions of rules are checked against for the
// file: the profile, the dimensions of the repository and those of the
// file.
func (a Assignment) Context(profile string, repo []DimensionValue) rules.Context {
	c := rules.Context{Profile: profile, Dimensions: map[string]string{}}
	for _, d := range repo {
		c.Dimensions[d.ID] = d.Value
	}
	for id, v := range a.Dimensions {
		c.Dimensions[id] = v
	}
	return c
}

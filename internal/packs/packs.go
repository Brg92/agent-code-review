// Package packs loads stack packs, activates them on a repository, assigns
// changed files to a pack and a language, and resolves the profile. Packs
// are data; this package knows no stack.
package packs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Brg92/agent-code-review/internal/lexer"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// ManifestFile is the name of the manifest in a pack directory.
const ManifestFile = "manifest.json"

// Pack is a loaded stack pack. Its manifest format is defined by
// schemas/pack-manifest.schema.json.
type Pack struct {
	ID          string
	Name        string
	Description string
	// Dir is the pack directory; the file paths below are inside it.
	Dir            string
	Detect         []Sign
	Languages      []Language
	Profiles       []string
	ProfileDetect  []ProfileSign
	DefaultProfile string
	Dimensions     []Dimension
	Rules          *rules.Catalog
	Templates      []string
	Noise          []rules.Glob
	TestFiles      []rules.Glob
	// GeneratedMarkers mark a file as generated in its first lines.
	GeneratedMarkers []string
}

// Sign is a file matching Path that contains Contains, when set.
type Sign struct {
	Path     rules.Glob
	Contains string
}

// ProfileSign is a sign of a profile.
type ProfileSign struct {
	Profile string
	Sign
}

// Dimension is an axis along which a repository varies within the stack,
// for example the UI toolkit. The core treats it as data: a repository
// has one of its values, and rules can depend on it.
type Dimension struct {
	ID     string
	Values []string
	// Detect holds the signs of each value, in order.
	Detect []DimensionSign
	// Default is the value when no sign matches.
	Default string
	// Combined is the value when signs of several values match; empty:
	// the value of the first sign that matches.
	Combined string
}

// DimensionSign is a sign of a value of a dimension.
type DimensionSign struct {
	Value string
	Sign
}

// Language is a language the pack reviews.
type Language struct {
	ID         string
	Extensions []string
	Table      *lexer.Table
}

var (
	packID       = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	languageID   = regexp.MustCompile(`^[a-z0-9-]+$`)
	extension    = regexp.MustCompile(`^\.[A-Za-z0-9]+$`)
	relativePath = regexp.MustCompile(`^(?:[^/\\.][^/\\]*|\.[^/\\.][^/\\]*)(?:/(?:[^/\\.][^/\\]*|\.[^/\\.][^/\\]*))*$`)
	nonBlank     = regexp.MustCompile(`\S`)
	dimensionID  = packID
	knownProfile = map[string]bool{"app": true, "library": true}
)

type (
	rawManifest struct {
		Schema           *int           `json:"schema"`
		ID               *string        `json:"id"`
		Name             *string        `json:"name"`
		Description      *string        `json:"description"`
		Detect           *[]rawSign     `json:"detect"`
		Languages        *[]rawLanguage `json:"languages"`
		Profiles         *[]string      `json:"profiles"`
		ProfileDetect    []rawProfile   `json:"profileDetect"`
		DefaultProfile   *string        `json:"defaultProfile"`
		Dimensions       []rawDimension `json:"dimensions"`
		Rules            *string        `json:"rules"`
		Templates        *[]string      `json:"templates"`
		Noise            *[]string      `json:"noise"`
		TestFiles        []string       `json:"testFiles"`
		GeneratedMarkers *[]string      `json:"generatedMarkers"`
	}
	rawSign struct {
		Path     *string `json:"path"`
		Contains *string `json:"contains"`
	}
	rawProfile struct {
		Profile  *string `json:"profile"`
		Path     *string `json:"path"`
		Contains *string `json:"contains"`
	}
	rawDimension struct {
		ID       *string       `json:"id"`
		Values   *[]string     `json:"values"`
		Detect   *[]rawDimSign `json:"detect"`
		Default  *string       `json:"default"`
		Combined *string       `json:"combined"`
	}
	rawDimSign struct {
		Value    *string `json:"value"`
		Path     *string `json:"path"`
		Contains *string `json:"contains"`
	}
	rawLanguage struct {
		ID         *string   `json:"id"`
		Extensions *[]string `json:"extensions"`
		Lexer      *string   `json:"lexer"`
	}
)

// LoadPack loads the pack in dir: its manifest, lexer tables and rules
// catalog. The manifest id must be the directory name, every file it
// names must exist in the pack, and unknown fields are rejected.
func LoadPack(dir string) (*Pack, error) {
	p, err := loadPack(dir)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", dir, err)
	}
	return p, nil
}

func loadPack(dir string) (*Pack, error) {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return nil, fmt.Errorf("read the manifest: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw rawManifest
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("manifest: decode: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("manifest: trailing data after the document")
	}
	if err := checkRequired(raw); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if *raw.Schema != 1 {
		return nil, errors.New("manifest: schema must be 1")
	}
	p := &Pack{ID: *raw.ID, Name: *raw.Name, Description: *raw.Description, Dir: dir, GeneratedMarkers: *raw.GeneratedMarkers}
	if !packID.MatchString(p.ID) {
		return nil, fmt.Errorf("id %q is not a lowercase pack id", p.ID)
	}
	if base := filepath.Base(dir); base != p.ID {
		return nil, fmt.Errorf("id %q is not the directory name %q", p.ID, base)
	}
	if !nonBlank.MatchString(p.Name) {
		return nil, errors.New("name is blank")
	}
	if err := p.loadProfiles(raw); err != nil {
		return nil, err
	}
	for _, rs := range *raw.Detect {
		s, err := sign(rs.Path, rs.Contains)
		if err != nil {
			return nil, fmt.Errorf("detect: %w", err)
		}
		p.Detect = append(p.Detect, s)
	}
	for name, list := range map[string]struct {
		in  []string
		out *[]rules.Glob
	}{"noise": {*raw.Noise, &p.Noise}, "testFiles": {raw.TestFiles, &p.TestFiles}} {
		for _, s := range list.in {
			g, err := rules.CompileGlob(s)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			*list.out = append(*list.out, g)
		}
	}
	if err := p.loadDimensions(raw.Dimensions); err != nil {
		return nil, err
	}
	if err := p.loadLanguages(*raw.Languages); err != nil {
		return nil, err
	}
	if err := p.loadFiles(raw); err != nil {
		return nil, err
	}
	return p, nil
}

func checkRequired(raw rawManifest) error {
	missing := ""
	for _, f := range []struct {
		name    string
		present bool
	}{
		{"schema", raw.Schema != nil}, {"id", raw.ID != nil}, {"name", raw.Name != nil}, {"description", raw.Description != nil},
		{"detect", raw.Detect != nil}, {"languages", raw.Languages != nil}, {"profiles", raw.Profiles != nil},
		{"rules", raw.Rules != nil}, {"templates", raw.Templates != nil},
		{"noise", raw.Noise != nil}, {"generatedMarkers", raw.GeneratedMarkers != nil},
	} {
		if !f.present && missing == "" {
			missing = f.name
		}
	}
	if missing != "" {
		return fmt.Errorf("%s is required", missing)
	}
	return nil
}

func sign(path, contains *string) (Sign, error) {
	if path == nil {
		return Sign{}, errors.New("path is required")
	}
	g, err := rules.CompileGlob(*path)
	if err != nil {
		return Sign{}, err
	}
	s := Sign{Path: g}
	if contains != nil {
		s.Contains = *contains
	}
	return s, nil
}

func (p *Pack) supports(profile string) bool {
	for _, s := range p.Profiles {
		if s == profile {
			return true
		}
	}
	return false
}

func (p *Pack) loadProfiles(raw rawManifest) error {
	for _, pr := range *raw.Profiles {
		if !knownProfile[pr] {
			return fmt.Errorf("profiles: unknown profile %q", pr)
		}
		p.Profiles = append(p.Profiles, pr)
	}
	for _, rp := range raw.ProfileDetect {
		if rp.Profile == nil || !p.supports(*rp.Profile) {
			return fmt.Errorf("profileDetect: profile %v is not one of the profiles of the pack", deref(rp.Profile))
		}
		s, err := sign(rp.Path, rp.Contains)
		if err != nil {
			return fmt.Errorf("profileDetect: %w", err)
		}
		p.ProfileDetect = append(p.ProfileDetect, ProfileSign{Profile: *rp.Profile, Sign: s})
	}
	switch {
	case raw.DefaultProfile != nil:
		if !p.supports(*raw.DefaultProfile) {
			return fmt.Errorf("defaultProfile %q is not one of the profiles of the pack", *raw.DefaultProfile)
		}
		p.DefaultProfile = *raw.DefaultProfile
	case len(p.Profiles) > 0:
		p.DefaultProfile = p.Profiles[0]
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return "(none)"
	}
	return fmt.Sprintf("%q", *s)
}

// ProfileDimension is the key of the profile in the conditions of rules;
// no dimension can have it as id.
const ProfileDimension = rules.ProfileKey

func (p *Pack) loadDimensions(raws []rawDimension) error {
	seen := map[string]bool{}
	for _, rd := range raws {
		if rd.ID == nil || !dimensionID.MatchString(*rd.ID) {
			return fmt.Errorf("dimensions: id %s is not a lowercase dimension id", deref(rd.ID))
		}
		d := Dimension{ID: *rd.ID}
		fail := func(format string, args ...any) error {
			return fmt.Errorf("dimension %s: "+format, append([]any{d.ID}, args...)...)
		}
		if d.ID == ProfileDimension {
			return fail("the id is reserved for the profile")
		}
		if seen[d.ID] {
			return fail("listed twice")
		}
		seen[d.ID] = true
		if rd.Values == nil || len(*rd.Values) == 0 {
			return fail("values is empty")
		}
		has := map[string]bool{}
		for _, v := range *rd.Values {
			if !dimensionID.MatchString(v) || has[v] {
				return fail("value %q is not a distinct lowercase value", v)
			}
			has[v] = true
			d.Values = append(d.Values, v)
		}
		if rd.Default == nil || !has[*rd.Default] {
			return fail("default %s is not one of the values", deref(rd.Default))
		}
		d.Default = *rd.Default
		if rd.Combined != nil {
			if !has[*rd.Combined] {
				return fail("combined %q is not one of the values", *rd.Combined)
			}
			d.Combined = *rd.Combined
		}
		if rd.Detect == nil {
			return fail("detect is required")
		}
		for _, rs := range *rd.Detect {
			if rs.Value == nil || !has[*rs.Value] {
				return fail("a sign shows value %s, which is not one of the values", deref(rs.Value))
			}
			s, err := sign(rs.Path, rs.Contains)
			if err != nil {
				return fail("detect: %v", err)
			}
			d.Detect = append(d.Detect, DimensionSign{Value: *rs.Value, Sign: s})
		}
		p.Dimensions = append(p.Dimensions, d)
	}
	return nil
}

func (p *Pack) loadLanguages(raws []rawLanguage) error {
	owner := map[string]string{}
	for _, rl := range raws {
		if rl.ID == nil || !languageID.MatchString(*rl.ID) {
			return fmt.Errorf("languages: id %s is not a language id", deref(rl.ID))
		}
		l := Language{ID: *rl.ID}
		if rl.Extensions == nil || len(*rl.Extensions) == 0 {
			return fmt.Errorf("language %s: extensions is empty", l.ID)
		}
		for _, e := range *rl.Extensions {
			if !extension.MatchString(e) {
				return fmt.Errorf("language %s: extension %q needs a leading dot", l.ID, e)
			}
			if prev, ok := owner[e]; ok {
				return fmt.Errorf("extension %s belongs to both %s and %s", e, prev, l.ID)
			}
			owner[e] = l.ID
			l.Extensions = append(l.Extensions, e)
		}
		if rl.Lexer == nil {
			return fmt.Errorf("language %s: lexer is required", l.ID)
		}
		path, err := p.file("lexer of "+l.ID, *rl.Lexer)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if l.Table, err = lexer.LoadTable(data); err != nil {
			return fmt.Errorf("%s: %w", *rl.Lexer, err)
		}
		if l.Table.Language != l.ID {
			return fmt.Errorf("%s: lexer table language %q, want %q", *rl.Lexer, l.Table.Language, l.ID)
		}
		p.Languages = append(p.Languages, l)
	}
	return nil
}

// file resolves a path of the manifest and checks it is a file inside the
// pack.
func (p *Pack) file(what, rel string) (string, error) {
	if !relativePath.MatchString(rel) {
		return "", fmt.Errorf("%s: %q is not a relative path inside the pack", what, rel)
	}
	path := filepath.Join(p.Dir, filepath.FromSlash(rel))
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", what, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: %s is not a regular file", what, rel)
	}
	return path, nil
}

func (p *Pack) loadFiles(raw rawManifest) error {
	path, err := p.file("rules", *raw.Rules)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if p.Rules, err = rules.LoadCatalog(data); err != nil {
		return fmt.Errorf("%s: %w", *raw.Rules, err)
	}
	if p.Rules.Pack != p.ID {
		return fmt.Errorf("%s: the catalog is for pack %q", *raw.Rules, p.Rules.Pack)
	}
	languages := map[string]bool{}
	for _, l := range p.Languages {
		languages[l.ID] = true
	}
	for _, r := range p.Rules.Rules {
		for _, l := range r.Languages {
			if !languages[l] {
				return fmt.Errorf("%s: rule %q is for language %q, which the pack lacks", *raw.Rules, r.ID, l)
			}
		}
	}
	for _, t := range *raw.Templates {
		path, err := p.file("template", t)
		if err != nil {
			return err
		}
		p.Templates = append(p.Templates, path)
	}
	return nil
}

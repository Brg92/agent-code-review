package core

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// legacyIgnored are the known settings of .github/mr-review.json that are
// not read, with the reason the note gives.
var legacyIgnored = map[string]string{
	"approvals": "the plugin never approves",
}

// legacy reads the legacy layout into c, reporting whether it exists. With
// read false it only looks for it, without reading its files.
func (r *reader) legacy(c *RepoCriteria, rr *ruleReader, read bool) (bool, error) {
	entries, err := r.files.List(r.ctx, r.in.Project, r.in.Ref, legacySkills)
	if err != nil {
		return false, fmt.Errorf("list criteria %s at %s: %w", legacySkills, r.in.Ref, err)
	}
	var skills []string
	for _, e := range entries {
		if e.Dir && strings.HasSuffix(e.Name, "-code-review") {
			skills = append(skills, e.Name)
		}
	}
	sort.Strings(skills)
	config, hasConfig, err := r.get(legacyConfig)
	if err != nil {
		return false, err
	}
	var parts []string
	if len(skills) > 0 {
		parts = append(parts, legacySkills)
	}
	if hasConfig {
		parts = append(parts, legacyConfig)
	}
	if len(parts) == 0 || !read {
		return len(parts) > 0, nil
	}
	c.Source = SourceLegacy
	c.Notes = append(c.Notes, "Legacy criteria are in use: "+strings.Join(parts, " and ")+".")
	for _, name := range skills {
		if err := r.skill(c, rr, path.Join(legacySkills, name)); err != nil {
			return false, err
		}
	}
	if hasConfig {
		if err := c.applyLegacyConfig(config, r.in.Project); err != nil {
			return false, err
		}
		c.Files = append(c.Files, legacyConfig)
	}
	return true, nil
}

// skill reads the SKILL.md of one legacy skill and its references/*.md, in
// name order.
func (r *reader) skill(c *RepoCriteria, rr *ruleReader, dir string) error {
	skill := path.Join(dir, "SKILL.md")
	data, found, err := r.get(skill)
	if err != nil {
		return err
	}
	if !found {
		c.Notes = append(c.Notes, fmt.Sprintf("%s`%s` (%s) — it has no SKILL.md", IgnoredPrefix, path.Base(dir), dir))
		return nil
	}
	c.Files = append(c.Files, skill)
	rr.read(skill, string(data), false)
	refs := path.Join(dir, "references")
	entries, err := r.files.List(r.ctx, r.in.Project, r.in.Ref, refs)
	if err != nil {
		return fmt.Errorf("list criteria %s at %s: %w", refs, r.in.Ref, err)
	}
	var names []string
	for _, e := range entries {
		if !e.Dir && strings.HasSuffix(e.Name, ".md") {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p := path.Join(refs, name)
		data, found, err := r.get(p)
		if err != nil {
			return err
		}
		if found {
			c.Files = append(c.Files, p)
			rr.read(p, string(data), true)
		}
	}
	return nil
}

// applyLegacyConfig takes the settings of .github/mr-review.json: uiToolkit
// is the ui-toolkit dimension, kind (or profile) the profile, lineLength
// the max option of line-too-long, and the project paths of consumers and
// sharedLibraries become project keys on the reviewed host. An invalid
// value fails with a
// *CriteriaError; a setting that is not read is noted.
func (c *RepoCriteria) applyLegacyConfig(data []byte, project Key) error {
	var f fields
	obj, err := decodeObject(data)
	if err != nil {
		f.add("", "%v", err)
		return f.err(legacyConfig)
	}
	text := func(name string, allowed ...string) string {
		raw, ok := obj[name]
		if !ok {
			return ""
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			for _, a := range allowed {
				if s == a {
					return s
				}
			}
		}
		f.add("/"+name, "%s is not one of %s", raw, strings.Join(allowed, ", "))
		return ""
	}
	if v := text("uiToolkit", "views", "compose", "both"); v != "" {
		c.Dimensions["ui-toolkit"] = v
	}
	kind, profile := text("kind", "app", "library"), text("profile", "app", "library")
	if kind != "" && profile != "" && kind != profile {
		f.add("/profile", "%s disagrees with kind %s", profile, kind)
	}
	c.Profile = kind
	if c.Profile == "" {
		c.Profile = profile
	}
	if raw, ok := obj["lineLength"]; ok {
		if n, ok := integer(raw); ok {
			c.RuleOptions["line-too-long"] = map[string]int{"max": n}
		} else {
			f.add("/lineLength", "%s is not an integer", raw)
		}
	}
	if raw, ok := obj["variants"]; ok {
		c.Variants = append(c.Variants, f.strings("/variants", raw, variantName, "a variant name")...)
	}
	if raw, ok := obj["publicApi"]; ok {
		c.PublicAPI = append(c.PublicAPI, f.strings("/publicApi", raw, globForm, "a glob")...)
	}
	for name, to := range map[string]*[]string{"consumers": &c.Consumers, "sharedLibraries": &c.SharedLibraries} {
		if raw, ok := obj[name]; ok {
			for _, p := range f.strings("/"+name, raw, projectPattern, "a project path") {
				*to = append(*to, Key{Forge: project.Forge, Host: project.Host, Project: p}.ProjectKey())
			}
		}
	}
	var names []string
	for name := range obj {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		switch name {
		case "uiToolkit", "kind", "profile", "lineLength", "variants", "publicApi", "consumers", "sharedLibraries":
		default:
			reason, ok := legacyIgnored[name]
			if !ok {
				reason = "not a setting of " + legacyConfig
			}
			c.Notes = append(c.Notes, fmt.Sprintf("%s`%s` (%s) — %s", IgnoredPrefix, name, legacyConfig, reason))
		}
	}
	return f.err(legacyConfig)
}

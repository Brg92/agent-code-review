package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// Files reads the files of a repository at a ref. Criteria are always read
// through it from the target branch, never from the branch under review.
type Files interface {
	// Get returns the content of the file at path; found is false when
	// there is no such file.
	Get(ctx context.Context, project Key, ref, path string) (content []byte, found bool, err error)
	// List returns the entries of the directory dir, by name; none when
	// there is no such directory.
	List(ctx context.Context, project Key, ref, dir string) ([]DirEntry, error)
}

// DirEntry is an entry of a directory listing.
type DirEntry struct {
	Name string
	Dir  bool
}

// Sources of the criteria of a repository, as in the repo-criteria schema.
const (
	// SourceOverlay is the neutral layout, .agent-code-review/.
	SourceOverlay = "overlay"
	// SourceLegacy is .github/skills/*-code-review/ and .github/mr-review.json.
	SourceLegacy = "legacy"
	// SourceDefault means no criteria files: the packs alone.
	SourceDefault = "default"
)

// RepoCriteria are the criteria of one repository as read from its target
// branch: the repo-criteria document. Profile and Packs hold what the
// criteria set, empty when they set nothing; the caller puts the resolved
// ones, and the noise of the packs, before writing the document. Lists
// and maps are empty, never nil.
type RepoCriteria struct {
	Schema        int                       `json:"schema"`
	Project       string                    `json:"project"`
	Ref           string                    `json:"ref"`
	Source        string                    `json:"source"`
	Profile       string                    `json:"profile"`
	Packs         []string                  `json:"packs"`
	Variants      []string                  `json:"variants"`
	Dimensions    map[string]string         `json:"dimensions"`
	DisabledRules []string                  `json:"disabledRules"`
	RuleOptions   map[string]map[string]int `json:"ruleOptions"`
	Rules         []RepositoryRule          `json:"rules"`
	Noise         []string                  `json:"noise"`
	PublicAPI     []string                  `json:"publicApi"`
	Consumers     []string                  `json:"consumers"`
	// SharedLibraries are the project keys of the libraries the
	// repository consumes, which dependency-mr-undeclared reads.
	SharedLibraries []string      `json:"sharedLibraries"`
	Cascade         CascadeConfig `json:"cascade"`
	// Files are the criteria files read, in order.
	Files []string `json:"files"`
	Notes []string `json:"notes"`
	// Ignored are the parts of the criteria left out, in the order of
	// their notes, which Notes also holds.
	Ignored []IgnoredItem `json:"-"`
}

// ignore leaves out a part of the criteria, with its note.
func (c *RepoCriteria) ignore(i IgnoredItem) {
	c.Notes = append(c.Notes, i.Note())
	c.Ignored = append(c.Ignored, i)
}

// CriteriaInput is what reading the criteria of a repository needs.
type CriteriaInput struct {
	// Project is the change request whose repository is read; its number
	// is not used.
	Project Key
	// Ref is the target branch of the change request.
	Ref string
	// Rules are the rules of every pack and of the core: their ids are
	// reserved, and their options are what ruleOptions may set.
	Rules []*rules.Rule
	// Changed are the paths the change request changes.
	Changed []string
}

// legacy layout paths.
const (
	legacySkills = ".github/skills"
	legacyConfig = ".github/mr-review.json"
)

// ReadCriteria reads the criteria of a repository from its target
// branch. The neutral layout wins over the legacy one; with neither, the
// packs alone apply. Parts of the criteria that cannot apply are left out
// with a note, and the review goes on; a configuration file that is not
// valid fails with a *CriteriaError, and a failed read with its error.
func ReadCriteria(ctx context.Context, files Files, in CriteriaInput) (RepoCriteria, error) {
	c := RepoCriteria{
		Schema: 1, Project: in.Project.ProjectKey(), Ref: in.Ref, Source: SourceDefault,
		Packs: []string{}, Variants: []string{}, Dimensions: map[string]string{}, DisabledRules: []string{},
		RuleOptions: map[string]map[string]int{}, Rules: []RepositoryRule{}, Noise: []string{}, PublicAPI: []string{},
		Consumers: []string{}, SharedLibraries: []string{}, Files: []string{}, Notes: []string{},
	}
	r := &reader{ctx: ctx, files: files, in: in}
	reserved := map[string]bool{}
	for _, rule := range in.Rules {
		reserved[rule.ID] = true
	}
	rr := newRuleReader(reserved)
	found, err := r.neutral(&c, rr)
	if err != nil {
		return RepoCriteria{}, err
	}
	legacy, err := r.legacy(&c, rr, !found)
	if err != nil {
		return RepoCriteria{}, err
	}
	switch {
	case found && legacy:
		c.Notes = append(c.Notes, "Legacy criteria ignored: .agent-code-review/ is present, so .github/skills and .github/mr-review.json are not read.")
	case !found && !legacy:
		c.Notes = append(c.Notes, fmt.Sprintf("No repository criteria on %s: only the pack and core rules apply.", in.Ref))
	}
	c.Rules = append(c.Rules, rr.rules...)
	c.checkSettings(in.Rules)
	if changed := changedCriteria(in.Changed); len(changed) > 0 {
		c.Notes = append(c.Notes, fmt.Sprintf("Criteria changed in this change request are not applied: %s. The criteria of %s are used.",
			strings.Join(changed, ", "), in.Ref))
	}
	for _, i := range rr.ignored {
		c.ignore(i)
	}
	return c, nil
}

// reader reads criteria files of one repository at its target branch.
type reader struct {
	ctx   context.Context
	files Files
	in    CriteriaInput
}

func (r *reader) get(path string) ([]byte, bool, error) {
	data, found, err := r.files.Get(r.ctx, r.in.Project, r.in.Ref, path)
	if err != nil {
		return nil, false, fmt.Errorf("read criteria %s at %s: %w", path, r.in.Ref, err)
	}
	return data, found, nil
}

// neutral reads the neutral layout into c, reporting whether it exists.
func (r *reader) neutral(c *RepoCriteria, rr *ruleReader) (bool, error) {
	config, hasConfig, err := r.get(ConfigFile)
	if err != nil {
		return false, err
	}
	doc, hasDoc, err := r.get(CriteriaFile)
	if err != nil {
		return false, err
	}
	if !hasConfig && !hasDoc {
		return false, nil
	}
	c.Source = SourceOverlay
	if hasConfig {
		cfg, err := DecodeRepoConfig(ConfigFile, config)
		if err != nil {
			return false, err
		}
		c.Files = append(c.Files, ConfigFile)
		c.applyConfig(cfg)
	}
	if hasDoc {
		c.Files = append(c.Files, CriteriaFile)
		rr.read(CriteriaFile, string(doc), false)
	}
	return true, nil
}

// applyConfig takes the settings of a repository configuration.
func (c *RepoCriteria) applyConfig(cfg RepoConfig) {
	c.Profile = cfg.Profile
	c.Cascade = cfg.Cascade
	for _, set := range []struct {
		to   *[]string
		from []string
	}{{&c.Packs, cfg.Packs}, {&c.Variants, cfg.Variants}, {&c.DisabledRules, cfg.DisabledRules}, {&c.Noise, cfg.Noise},
		{&c.SharedLibraries, cfg.SharedLibraries}} {
		*set.to = append(*set.to, set.from...)
	}
	for id, v := range cfg.Dimensions {
		c.Dimensions[id] = v
	}
	for id, values := range cfg.RuleOptions {
		c.RuleOptions[id] = values
	}
}

// checkSettings keeps the disabled rules that are repository rules and
// the rule options that a pack or core rule declares, within its bounds,
// noting each one it drops: pack and core rules always apply.
func (c *RepoCriteria) checkSettings(known []*rules.Rule) {
	for _, i := range c.settle(known) {
		c.ignore(i)
	}
}

// settle keeps the disabled rules that are repository rules and the rule
// options that one of known declares, within its bounds, and returns each
// one it drops with the reason.
func (c *RepoCriteria) settle(known []*rules.Rule) []IgnoredItem {
	var ignored []IgnoredItem
	ignore := func(item, reason string) { ignored = append(ignored, IgnoredItem{Item: item, Reason: reason}) }
	byID := map[string]*rules.Rule{}
	for _, r := range known {
		byID[r.ID] = r
	}
	own := map[string]bool{}
	for _, r := range c.Rules {
		own[r.ID] = true
	}
	disabled := []string{}
	for _, id := range c.DisabledRules {
		item := fmt.Sprintf("`%s` in disabledRules (%s)", id, ConfigFile)
		switch {
		case own[id]:
			disabled = append(disabled, id)
		case byID[id] != nil:
			ignore(item, "pack and core rules always apply")
		default:
			ignore(item, "no repository rule has this id")
		}
	}
	c.DisabledRules = disabled
	ids := make([]string, 0, len(c.RuleOptions))
	for id := range c.RuleOptions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		values := c.RuleOptions[id]
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			item := fmt.Sprintf("`%s.%s` in ruleOptions (%s)", id, name, ConfigFile)
			if c.Source == SourceLegacy {
				item = fmt.Sprintf("`lineLength` (%s)", legacyConfig)
			}
			if reason := optionProblem(byID[id], name, values[name]); reason != "" {
				ignore(item, reason)
				delete(values, name)
			}
		}
		if len(values) == 0 {
			delete(c.RuleOptions, id)
		}
	}
	return ignored
}

// optionProblem says why a rule option cannot be set; empty when it can.
func optionProblem(rule *rules.Rule, name string, value int) string {
	if rule == nil {
		return "no pack or core rule has this id"
	}
	for _, o := range rule.Options() {
		if o.Name != name {
			continue
		}
		if value < o.Min || value > o.Max {
			return fmt.Sprintf("%d is outside %d-%d", value, o.Min, o.Max)
		}
		return ""
	}
	return "the rule has no option " + name
}

// isCriteriaPath reports whether a repository path is a criteria file of
// either layout.
func isCriteriaPath(p string) bool {
	if p == ConfigFile || p == CriteriaFile || p == legacyConfig {
		return true
	}
	dir, _, ok := strings.Cut(strings.TrimPrefix(p, legacySkills+"/"), "/")
	return ok && strings.HasPrefix(p, legacySkills+"/") && strings.HasSuffix(dir, "-code-review")
}

// changedCriteria returns the changed paths that are criteria files, in
// order.
func changedCriteria(changed []string) []string {
	var out []string
	for _, p := range changed {
		if isCriteriaPath(p) {
			out = append(out, p)
		}
	}
	return out
}

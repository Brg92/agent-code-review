package core_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// mobileCatalog is a synthetic pack catalog with every kind of rule the
// effective rule set treats differently.
const mobileCatalog = `{"schema":1,"pack":"mobile","rules":[
 {"id":"mobile-plain","title":"Plain","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Basics"},
 {"id":"mobile-lines","title":"Line over {max}","severity":"MINOR","description":"At most {max}.","why":"Long lines.","fix":"Wrap within {max}.","languages":[],"tags":[],"group":"Style",
  "match":{"detector":"line-length","max":100,"lines":"added"},"options":{"max":{"min":60,"max":200}}},
 {"id":"mobile-views","title":"Views","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Views","when":{"ui-toolkit":["views","both"]}},
 {"id":"mobile-library","title":"Library","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Basics","when":{"profile":["library"]}},
 {"id":"mobile-android","title":"Android files","severity":"MINOR","description":"D","languages":[],"tags":[],"when":{"platform":["android"]}},
 {"id":"mobile-judged","title":"Judged","severity":"MAJOR","needsJudgement":true,"description":"D","languages":[],"tags":[],"group":"Basics"}
]}`

const webCatalog = `{"schema":1,"pack":"web","rules":[
 {"id":"web-plain","title":"Web","severity":"MINOR","description":"D","languages":[],"tags":[]}
]}`

func syntheticSet(t *testing.T) (*packs.Set, []*packs.Pack) {
	t.Helper()
	var active []*packs.Pack
	catalogs := []*rules.Catalog{rules.Core()}
	for _, src := range []string{mobileCatalog, webCatalog} {
		c, err := rules.LoadCatalog([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		p := &packs.Pack{ID: c.Pack, Rules: c}
		if c.Pack == "mobile" {
			p.FileDimensions = []packs.FileDimension{{ID: "platform", Values: []string{"android", "ios"}, Default: "android"}}
		}
		active = append(active, p)
		catalogs = append(catalogs, c)
	}
	all, err := rules.Merge(catalogs...)
	if err != nil {
		t.Fatal(err)
	}
	return &packs.Set{Packs: active, Rules: all}, active
}

func coreIDs() []string {
	var out []string
	for _, r := range rules.Core().Rules {
		out = append(out, r.ID)
	}
	return out
}

func repoCriteria(rs ...core.RepositoryRule) core.RepoCriteria {
	return core.RepoCriteria{Source: core.SourceOverlay, Rules: rs, DisabledRules: []string{}, RuleOptions: map[string]map[string]int{}}
}

var (
	repoA = core.RepositoryRule{ID: "repo-a", Source: core.RepositorySource, Severity: core.Major, Text: "Use the tracker.", File: core.CriteriaFile, Line: 3}
	repoB = core.RepositoryRule{ID: "repo-b", Source: core.RepositorySource, Severity: core.Minor, Text: "Name things well.", File: core.CriteriaFile, Line: 4}
)

func TestEffectiveRules(t *testing.T) {
	set, active := syntheticSet(t)
	views := []packs.DimensionValue{{ID: "ui-toolkit", Value: "views", Reason: packs.DimensionFromDefault}}
	compose := []packs.DimensionValue{{ID: "ui-toolkit", Value: "compose", Reason: packs.DimensionFromConfig}}
	base := "mobile-plain mobile-lines mobile-android mobile-judged web-plain"
	tests := []struct {
		name       string
		profile    string
		dimensions []packs.DimensionValue
		edit       func(c *core.RepoCriteria)
		want       string
		ignored    []core.IgnoredItem
	}{
		{"compose app", "app", compose, nil, base + " repo-a repo-b", nil},
		{"when by dimension", "app", views, nil, "mobile-plain mobile-lines mobile-views mobile-android mobile-judged web-plain repo-a repo-b", nil},
		{"when by profile", "library", compose, nil, "mobile-plain mobile-lines mobile-library mobile-android mobile-judged web-plain repo-a repo-b", nil},
		{"disabled repository rule", "app", compose, func(c *core.RepoCriteria) { c.DisabledRules = []string{"repo-b"} }, base + " repo-a", nil},
		{"disabled core and pack rules", "app", compose, func(c *core.RepoCriteria) { c.DisabledRules = []string{"logic-defect", "mobile-plain"} }, base + " repo-a repo-b",
			[]core.IgnoredItem{
				{Item: "`logic-defect` in disabledRules (.agent-code-review/config.json)", Reason: "pack and core rules always apply"},
				{Item: "`mobile-plain` in disabledRules (.agent-code-review/config.json)", Reason: "pack and core rules always apply"},
			}},
		{"disabled unknown id", "app", compose, func(c *core.RepoCriteria) { c.DisabledRules = []string{"nope"} }, base + " repo-a repo-b",
			[]core.IgnoredItem{{Item: "`nope` in disabledRules (.agent-code-review/config.json)", Reason: "no repository rule has this id"}}},
		{"option out of limits", "app", compose, func(c *core.RepoCriteria) { c.RuleOptions = map[string]map[string]int{"mobile-lines": {"max": 20}} }, base + " repo-a repo-b",
			[]core.IgnoredItem{{Item: "`mobile-lines.max` in ruleOptions (.agent-code-review/config.json)", Reason: "20 is outside 60-200"}}},
		{"option for an undeclared parameter", "app", compose, func(c *core.RepoCriteria) {
			c.RuleOptions = map[string]map[string]int{"mobile-lines": {"width": 80}, "mobile-plain": {"max": 90}, "nope": {"max": 90}}
		}, base + " repo-a repo-b",
			[]core.IgnoredItem{
				{Item: "`mobile-lines.width` in ruleOptions (.agent-code-review/config.json)", Reason: "the rule has no option width"},
				{Item: "`mobile-plain.max` in ruleOptions (.agent-code-review/config.json)", Reason: "the rule has no option max"},
				{Item: "`nope.max` in ruleOptions (.agent-code-review/config.json)", Reason: "no pack or core rule has this id"},
			}},
		{"items the criteria already ignored come first", "app", compose, func(c *core.RepoCriteria) {
			c.Ignored = []core.IgnoredItem{{Item: "`x` (.agent-code-review/criteria.md:2)", Reason: "the id is not kebab-case"}}
			c.DisabledRules = []string{"nope"}
		}, base + " repo-a repo-b",
			[]core.IgnoredItem{
				{Item: "`x` (.agent-code-review/criteria.md:2)", Reason: "the id is not kebab-case"},
				{Item: "`nope` in disabledRules (.agent-code-review/config.json)", Reason: "no repository rule has this id"},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := repoCriteria(repoA, repoB)
			if tt.edit != nil {
				tt.edit(&c)
			}
			before := c.DisabledRules
			rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: tt.profile, Dimensions: tt.dimensions, Criteria: c})
			var ids []string
			for _, r := range rs.Rules {
				ids = append(ids, r.ID)
			}
			if want := strings.Join(append(coreIDs(), tt.want), " "); strings.Join(ids, " ") != want {
				t.Errorf("rules\n%s\nwant\n%s", strings.Join(ids, " "), want)
			}
			if !reflect.DeepEqual(rs.Ignored, tt.ignored) {
				t.Errorf("ignored =\n%+v\nwant\n%+v", rs.Ignored, tt.ignored)
			}
			if !reflect.DeepEqual(c.DisabledRules, before) {
				t.Errorf("the criteria were changed: %q", c.DisabledRules)
			}
		})
	}
}

func effectiveRule(t *testing.T, rs core.Ruleset, id string) core.EffectiveRule {
	t.Helper()
	for _, r := range rs.Rules {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no rule %s", id)
	return core.EffectiveRule{}
}

func TestEffectiveRulesEntries(t *testing.T) {
	set, active := syntheticSet(t)
	c := repoCriteria(repoA)
	c.RuleOptions = map[string]map[string]int{"mobile-lines": {"max": 120}}
	rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: "app", Criteria: c})
	tests := []struct {
		id   string
		want core.EffectiveRule
	}{
		{"logic-defect", core.EffectiveRule{ID: "logic-defect", Title: rules.Core().Rules[0].Title, Severity: core.Blocker, Source: core.CoreSource, Group: "Logic", NeedsJudgement: true}},
		{"mobile-lines", core.EffectiveRule{ID: "mobile-lines", Title: "Line over 120", Severity: core.Minor, Source: "mobile", Group: "Style", Options: map[string]int{"max": 120}}},
		{"mobile-judged", core.EffectiveRule{ID: "mobile-judged", Title: "Judged", Severity: core.Major, Source: "mobile", Group: "Basics", NeedsJudgement: true}},
		{"web-plain", core.EffectiveRule{ID: "web-plain", Title: "Web", Severity: core.Minor, Source: "web"}},
		{"repo-a", core.EffectiveRule{ID: "repo-a", Title: "Use the tracker.", Severity: core.Major, Source: core.RepositorySource}},
	}
	for _, tt := range tests {
		got := effectiveRule(t, rs, tt.id)
		if got.Rule == nil || got.Rule.ID != tt.id {
			t.Errorf("%s: rule %+v", tt.id, got.Rule)
		}
		got.Rule = nil
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s =\n%+v\nwant\n%+v", tt.id, got, tt.want)
		}
	}
	if r := effectiveRule(t, rs, "mobile-lines").Rule; r.Fix != "Wrap within 120." {
		t.Errorf("the option is not applied to the rule: fix %q", r.Fix)
	}
	if got := effectiveRule(t, core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: "app", Criteria: repoCriteria()}), "mobile-lines").Options; !reflect.DeepEqual(got, map[string]int{"max": 100}) {
		t.Errorf("default options %v, want the catalog value", got)
	}
	if rs.Context.Profile != "app" || !rs.Context.PerFile["platform"] {
		t.Errorf("context %+v, want the profile and platform left to each file", rs.Context)
	}
}

func TestEffectiveRulesOnlyActivePacks(t *testing.T) {
	set, active := syntheticSet(t)
	rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active[1:], Profile: "app", Criteria: repoCriteria()})
	var ids []string
	for _, r := range rs.Rules {
		ids = append(ids, r.ID)
	}
	if want := append(coreIDs(), "web-plain"); !reflect.DeepEqual(ids, want) {
		t.Errorf("rules %q, want %q", ids, want)
	}
}

func TestRulesetChecklist(t *testing.T) {
	set, active := syntheticSet(t)
	rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: "library", Criteria: repoCriteria(repoA, repoB)})
	cl := rs.Checklist()
	var got []string
	for _, s := range cl.Sections {
		var ids []string
		for _, r := range s.Rules {
			ids = append(ids, r.ID)
		}
		got = append(got, s.Heading+": "+strings.Join(ids, " "))
	}
	want := []string{
		"Logic: logic-defect",
		": variant-asymmetry dependency-mr-undeclared uses-removed-api missing-required-argument contract-misuse mobile-android web-plain",
		"Basics: mobile-plain mobile-library mobile-judged",
		"Style: mobile-lines",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sections =\n%q\nwant\n%q", got, want)
	}
	var repo []string
	for _, r := range cl.Repository {
		repo = append(repo, r.ID)
	}
	if strings.Join(repo, " ") != "repo-a repo-b" {
		t.Errorf("repository section %q", repo)
	}
}

func TestRulesetByID(t *testing.T) {
	set, active := syntheticSet(t)
	rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: "app", Criteria: repoCriteria(repoA)})
	byID := rs.ByID()
	if len(byID) != len(rs.Rules) {
		t.Errorf("%d rules by id, want %d", len(byID), len(rs.Rules))
	}
	if r := byID["repo-a"]; r == nil || r.Severity != "MAJOR" || r.NeedsJudgement || r.Title != "Use the tracker." {
		t.Errorf("repository rule %+v", r)
	}
	if byID["mobile-views"] != nil {
		t.Error("a rule whose conditions do not hold is a rule of the change request")
	}
}

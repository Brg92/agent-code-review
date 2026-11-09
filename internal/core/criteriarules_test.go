package core

import (
	"reflect"
	"strings"
	"testing"
)

const criteriaFile = ".agent-code-review/criteria.md"

func TestReadRepositoryRules(t *testing.T) {
	doc := strings.Join([]string{
		"# sample-app review criteria", // 1
		"",
		"- Prose before any rules heading is context.",
		"- `app-prose-rule` — **MAJOR**. A rule-form item counts anywhere.",
		"",
		"## Project rules", // 6
		"",
		"### Shared libraries", // 8
		"",
		"- `app-library-contract-undeclared` — **MAJOR**. A change that depends on a", // 10
		"  new API lists the library change request under Related Changes.",
		"- `app-flag-hardcoded`, **BLOCKER**. Comma and en dash separators are accepted.",
		"- `app-tracker` – **MINOR** Analytics go through the tracker.",
		"",
		"```text",
		"- `app-in-a-fence` — **MAJOR**. Code fences are skipped.",
		"```",
		"",
		"## Known exceptions (never reported)", // 19
		"",
		"- `LegacyBridge` is exempt from `app-tracker`.",
	}, "\n") + "\n"
	r := newRuleReader(map[string]bool{})
	r.read(criteriaFile, doc, false)
	want := []RepositoryRule{
		{ID: "app-prose-rule", Source: RepositorySource, Severity: Major, Text: "A rule-form item counts anywhere.", File: criteriaFile, Line: 4},
		{ID: "app-library-contract-undeclared", Source: RepositorySource, Severity: Major,
			Text: "A change that depends on a new API lists the library change request under Related Changes.", File: criteriaFile, Line: 10},
		{ID: "app-flag-hardcoded", Source: RepositorySource, Severity: Blocker, Text: "Comma and en dash separators are accepted.", File: criteriaFile, Line: 12},
		{ID: "app-tracker", Source: RepositorySource, Severity: Minor, Text: "Analytics go through the tracker.", File: criteriaFile, Line: 13},
	}
	if !reflect.DeepEqual(r.rules, want) {
		t.Errorf("rules =\n%+v\nwant\n%+v", r.rules, want)
	}
	if len(r.notes) != 0 {
		t.Errorf("notes = %q, want none", r.notes)
	}
}

func TestReadRepositoryRulesIgnores(t *testing.T) {
	reserved := map[string]bool{"no-double-bang": true, "logic-defect": true}
	tests := []struct {
		name, doc string
		want      string
	}{
		{"id not kebab-case", "- `App_Rule` — **MAJOR**. Text.\n",
			"Repository criteria ignored: `App_Rule` (.agent-code-review/criteria.md:1) — the id is not kebab-case"},
		{"placeholder id", "- `<app>-tracker`, **MAJOR**. Text.\n",
			"Repository criteria ignored: `<app>-tracker` (.agent-code-review/criteria.md:1) — the id is not kebab-case"},
		{"dotted id", "- `app.tracker` — **MAJOR**. Text.\n",
			"Repository criteria ignored: `app.tracker` (.agent-code-review/criteria.md:1) — the id is not kebab-case"},
		{"invalid severity", "- `app-rule` — **CRITICAL**. Text.\n",
			"Repository criteria ignored: `app-rule` (.agent-code-review/criteria.md:1) — severity CRITICAL is not BLOCKER, MAJOR or MINOR"},
		{"ranged severity", "- `app-rule` — **MAJOR/BLOCKER**. Text.\n",
			"Repository criteria ignored: `app-rule` (.agent-code-review/criteria.md:1) — severity MAJOR/BLOCKER is not BLOCKER, MAJOR or MINOR"},
		{"lowercase severity", "- `app-rule` — **major**. Text.\n",
			"Repository criteria ignored: `app-rule` (.agent-code-review/criteria.md:1) — severity major is not BLOCKER, MAJOR or MINOR"},
		{"info severity", "- `app-rule` — **INFO**. Text.\n",
			"Repository criteria ignored: `app-rule` (.agent-code-review/criteria.md:1) — severity INFO is not BLOCKER, MAJOR or MINOR"},
		{"pack rule id", "- `no-double-bang` — **MINOR**. Text.\n",
			"Repository criteria ignored: `no-double-bang` (.agent-code-review/criteria.md:1) — the id is already used by a pack or core rule"},
		{"core rule id", "- `logic-defect` — **MINOR**. Text.\n",
			"Repository criteria ignored: `logic-defect` (.agent-code-review/criteria.md:1) — the id is already used by a pack or core rule"},
		{"no text", "- `app-rule` — **MAJOR**.\n",
			"Repository criteria ignored: `app-rule` (.agent-code-review/criteria.md:1) — the rule has no text"},
		{"baseline suppressions", "## Baseline suppressions\n\n- `no-double-bang` is off here.\n",
			"Repository criteria ignored: section \"Baseline suppressions\" (.agent-code-review/criteria.md:1) — pack and core rules cannot be switched off or re-weighted"},
		{"severity changes", "### Severity changes\n- `app-rule` — **MAJOR**. Not read.\n",
			"Repository criteria ignored: section \"Severity changes\" (.agent-code-review/criteria.md:1) — pack and core rules cannot be switched off or re-weighted"},
		{"item without an id under rules", "## Project rules\n\n- Keep functions short.\n",
			"Repository criteria ignored: list item (.agent-code-review/criteria.md:3) — it has no rule id, so it cannot become a finding (warning)"},
		{"item without a severity under rules", "## Extra rules\n- `app-rule` — keep it short.\n",
			"Repository criteria ignored: `app-rule` (.agent-code-review/criteria.md:2) — it has no **<SEVERITY>**, so it cannot become a finding (warning)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRuleReader(reserved)
			r.read(criteriaFile, tt.doc, false)
			if len(r.rules) != 0 || !reflect.DeepEqual(r.notes, []string{tt.want}) {
				t.Errorf("rules %+v, notes %q, want only the note %q", r.rules, r.notes, tt.want)
			}
		})
	}
}

func TestReadRepositoryRulesDuplicateAcrossFiles(t *testing.T) {
	r := newRuleReader(nil)
	r.read(".github/skills/app-code-review/SKILL.md", "- `app-rule` — **MAJOR**. First.\n", false)
	r.read(".github/skills/app-code-review/references/more.md", "# More\n\n- `app-rule` — **MINOR**. Second.\n", true)
	if len(r.rules) != 1 || r.rules[0].Text != "First." {
		t.Errorf("rules = %+v, want only the first", r.rules)
	}
	want := []string{"Repository criteria ignored: `app-rule` (.github/skills/app-code-review/references/more.md:3) — duplicate id, first defined at .github/skills/app-code-review/SKILL.md:1"}
	if !reflect.DeepEqual(r.notes, want) {
		t.Errorf("notes = %q, want %q", r.notes, want)
	}
}

func TestReadRepositoryRulesSections(t *testing.T) {
	doc := strings.Join([]string{
		"---",
		"name: app-code-review",
		"- `front-matter` — **MAJOR**. Not a rule.",
		"---",
		"# Title",
		"- Prose under the title of a references file.",
		"## Conventions",
		"- Every section of a references file lists rules.",
		"## Known exceptions",
		"- Exceptions are prose.",
		"## Project rules",
		"- Rules section.",
		"## Notes",
		"- Out of the rules section again in SKILL.md.",
		"    - A nested item is not checked.",
	}, "\n")
	skill := newRuleReader(nil)
	skill.read("SKILL.md", doc, false)
	if want := []string{"Repository criteria ignored: list item (SKILL.md:12) — it has no rule id, so it cannot become a finding (warning)"}; !reflect.DeepEqual(skill.notes, want) {
		t.Errorf("SKILL.md notes = %q, want %q", skill.notes, want)
	}
	refs := newRuleReader(nil)
	refs.read("references/x.md", doc, true)
	var lines []string
	for _, n := range refs.notes {
		at := n[:strings.Index(n, ")")]
		lines = append(lines, at[strings.LastIndex(at, ":")+1:])
	}
	if want := []string{"8", "12", "14"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("references notes at lines %q, want %q: %q", lines, want, refs.notes)
	}
	crlf := newRuleReader(nil)
	crlf.read(criteriaFile, "- `app-rule` — **MAJOR**. Text.\r\n  More.\r\n", false)
	if len(crlf.rules) != 1 || crlf.rules[0].Text != "Text. More." {
		t.Errorf("CRLF rules = %+v", crlf.rules)
	}
}

// TestReadRepositoryRulesExceptionsToPackRules checks that a known
// exception may only name rules of the repository: one that names a pack
// or core rule is reported, since pack and core rules always apply.
func TestReadRepositoryRulesExceptionsToPackRules(t *testing.T) {
	doc := strings.Join([]string{
		"## Project rules",
		"- `app-tracker` — **MAJOR**. Use the tracker.",
		"## Known exceptions (never reported)",
		"- `LegacyBridge` is exempt from `app-tracker`.",
		"- `_binding!!` is fine here, not a `no-double-bang`, nor a `logic-defect` or a `no-double-bang` again.",
		"### Generated code",
		"- `Generated.kt` is exempt from `no-double-bang`.",
		"Prose naming `no-double-bang` outside a list item is not an exception.",
	}, "\n")
	r := newRuleReader(map[string]bool{"no-double-bang": true, "logic-defect": true})
	r.read(criteriaFile, doc, false)
	want := []string{
		"Repository criteria ignored: known exception for `no-double-bang` (.agent-code-review/criteria.md:5) — pack and core rules always apply",
		"Repository criteria ignored: known exception for `logic-defect` (.agent-code-review/criteria.md:5) — pack and core rules always apply",
		"Repository criteria ignored: known exception for `no-double-bang` (.agent-code-review/criteria.md:7) — pack and core rules always apply",
	}
	if !reflect.DeepEqual(r.notes, want) {
		t.Errorf("notes =\n%q\nwant\n%q", r.notes, want)
	}
	if len(r.rules) != 1 {
		t.Errorf("rules = %+v, want app-tracker only", r.rules)
	}
}

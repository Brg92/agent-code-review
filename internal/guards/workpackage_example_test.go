package guards

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// catalogRule is a rule of a catalog, as the work package lists it.
type catalogRule struct {
	title, severity, group, token string
	needsJudgement                bool
	order                         int
}

// catalogRules reads the rules of a catalog file by id. token is the text of
// the first token of the first match pattern, when the rule has one.
func catalogRules(t *testing.T, path string) map[string]catalogRule {
	t.Helper()
	out := map[string]catalogRule{}
	rules, _ := readJSON(t, filepath.Join(repoRoot, path))["rules"].([]any)
	for i, r := range rules {
		rule, _ := r.(map[string]any)
		id, _ := rule["id"].(string)
		c := catalogRule{order: i}
		c.title, _ = rule["title"].(string)
		c.severity, _ = rule["severity"].(string)
		c.group, _ = rule["group"].(string)
		c.needsJudgement, _ = rule["needsJudgement"].(bool)
		match, _ := rule["match"].(map[string]any)
		patterns, _ := match["patterns"].([]any)
		if len(patterns) > 0 {
			pattern, _ := patterns[0].(map[string]any)
			tokens, _ := pattern["tokens"].([]any)
			if len(tokens) > 0 {
				first, _ := tokens[0].(map[string]any)
				c.token, _ = first["text"].(string)
			}
		}
		out[id] = c
	}
	return out
}

// sourceCatalogs maps the source of a rule in a work package to the file
// that holds its catalog.
var sourceCatalogs = map[string]string{
	"core":    "internal/rules/core.json",
	"android": "packs/android/rules.json",
}

// TestWorkPackageExampleRulesAgreeWithTheCatalogs keeps the rules of the
// example work package as the rule engine would list them: the title,
// severity, group and need for judgement of the catalog of their source,
// in the order of that catalog.
func TestWorkPackageExampleRulesAgreeWithTheCatalogs(t *testing.T) {
	catalogs := map[string]map[string]catalogRule{}
	for source, path := range sourceCatalogs {
		catalogs[source] = catalogRules(t, path)
	}
	for _, f := range globAll(t, "testdata/contracts/workpackage/valid/s1-signals.json") {
		wp := readJSON(t, f)
		last := map[string]int{}
		rules, _ := wp["rules"].([]any)
		for _, r := range rules {
			rule, _ := r.(map[string]any)
			id, _ := rule["id"].(string)
			source, _ := rule["source"].(string)
			catalog, ok := catalogs[source]
			if !ok {
				t.Errorf("%s: rule %s has source %q, which has no catalog here", f, id, source)
				continue
			}
			want, ok := catalog[id]
			if !ok {
				t.Errorf("%s: rule %s is not in the %s catalog", f, id, source)
				continue
			}
			group, _ := rule["group"].(string)
			needs, _ := rule["needsJudgement"].(bool)
			if rule["title"] != want.title || rule["severity"] != want.severity || group != want.group || needs != want.needsJudgement {
				t.Errorf("%s: rule %s is %v/%v/%q/%v (title/severity/group/needsJudgement), the %s catalog has %q/%s/%q/%v",
					f, id, rule["title"], rule["severity"], group, needs, source, want.title, want.severity, want.group, want.needsJudgement)
			}
			if want.order < last[source] {
				t.Errorf("%s: rule %s comes after a rule that follows it in the %s catalog", f, id, source)
			}
			last[source] = want.order
		}
	}
}

// diffAddedLines returns the numbers of the lines that a diff adds.
func diffAddedLines(t *testing.T, diff string) map[int]bool {
	t.Helper()
	added := map[int]bool{}
	next := 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@ "):
			plus := strings.Fields(line)[2] // +start,count
			start, _, _ := strings.Cut(strings.TrimPrefix(plus, "+"), ",")
			n, err := strconv.Atoi(start)
			if err != nil {
				t.Fatalf("hunk header %q: %v", line, err)
			}
			next = n
		case strings.HasPrefix(line, "+"):
			added[next] = true
			next++
		case strings.HasPrefix(line, " "):
			next++
		}
	}
	return added
}

// TestWorkPackageExampleSignalsAgreeWithTheEngine keeps the signals of the
// example work package as the rule engine would report them: the severity of
// the rule (none when the rule needs judgement), a line and column that
// point at the first token of the rule's pattern in the code of the file,
// and the origin "mr" exactly for the lines that the diff adds.
func TestWorkPackageExampleSignalsAgreeWithTheEngine(t *testing.T) {
	catalogs := map[string]catalogRule{}
	for _, path := range sourceCatalogs {
		for id, r := range catalogRules(t, path) {
			catalogs[id] = r
		}
	}
	for _, f := range globAll(t, "testdata/contracts/workpackage/valid/s1-signals.json") {
		wp := readJSON(t, f)
		type source struct {
			lines map[int]string
			added map[int]bool
		}
		files := map[string]source{}
		list, _ := wp["files"].([]any)
		for _, e := range list {
			file, _ := e.(map[string]any)
			path, _ := file["path"].(string)
			s := source{lines: map[int]string{}}
			diff, _ := file["diff"].(string)
			s.added = diffAddedLines(t, diff)
			code, _ := file["code"].(map[string]any)
			regions, _ := code["regions"].([]any)
			for _, r := range regions {
				region, _ := r.(map[string]any)
				start, _ := region["start"].(float64)
				lines, _ := region["lines"].([]any)
				for i, l := range lines {
					s.lines[int(start)+i], _ = l.(string)
				}
			}
			files[path] = s
		}
		signals, _ := wp["signals"].([]any)
		for _, sg := range signals {
			sig, _ := sg.(map[string]any)
			id, _ := sig["id"].(string)
			rule, _ := sig["rule"].(string)
			want, ok := catalogs[rule]
			if !ok {
				t.Errorf("%s: signal %s has the unknown rule %s", f, id, rule)
				continue
			}
			needs, _ := sig["needsJudgement"].(bool)
			if needs != want.needsJudgement {
				t.Errorf("%s: signal %s: needsJudgement %v, the rule has %v", f, id, needs, want.needsJudgement)
			}
			switch severity := sig["severity"]; {
			case want.needsJudgement && severity != nil:
				t.Errorf("%s: signal %s: severity %v, want none for a rule that needs judgement", f, id, severity)
			case !want.needsJudgement && severity != want.severity:
				t.Errorf("%s: signal %s: severity %v, the rule has %s", f, id, severity, want.severity)
			}
			path, _ := sig["path"].(string)
			line, _ := sig["line"].(float64)
			column, _ := sig["column"].(float64)
			src, ok := files[path]
			if !ok {
				t.Errorf("%s: signal %s is in %s, which is not a file of the example", f, id, path)
				continue
			}
			text, ok := src.lines[int(line)]
			if !ok {
				t.Errorf("%s: signal %s: line %d of %s is not in its code regions", f, id, int(line), path)
				continue
			}
			if rest := []rune(text); int(column) < 1 || int(column)-1 > len(rest) || !strings.HasPrefix(string(rest[int(column)-1:]), want.token) {
				t.Errorf("%s: signal %s: %s:%d:%d is not at %q in %q", f, id, path, int(line), int(column), want.token, text)
			}
			origin, _ := sig["origin"].(string)
			if wantOrigin := map[bool]string{true: "mr", false: "existing"}[src.added[int(line)]]; origin != wantOrigin {
				t.Errorf("%s: signal %s: origin %q, the diff of %s makes it %q", f, id, origin, path, wantOrigin)
			}
		}
	}
}

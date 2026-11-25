package review

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

// workingCopy writes files, by slash-separated path, in a new directory.
func workingCopy(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func check(t *testing.T, dir string) core.RepoCriteria {
	t.Helper()
	c, err := CheckCriteria(context.Background(), dir, loadPacks(t))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ignoredNotes(c core.RepoCriteria) []string {
	var out []string
	for _, i := range c.Ignored {
		out = append(out, i.Item+" — "+i.Reason)
	}
	return out
}

func TestCheckCriteriaOfAWorkingCopyWithNone(t *testing.T) {
	c := check(t, workingCopy(t, map[string]string{"src/main.kt": "fun main() {}"}))
	if c.Source != core.SourceDefault || len(c.Ignored) != 0 || len(c.Rules) != 0 {
		t.Errorf("criteria = %+v", c)
	}
	if len(c.Notes) != 1 || !strings.Contains(c.Notes[0], "No repository criteria") {
		t.Errorf("notes = %q", c.Notes)
	}
}

func TestCheckCriteriaOfTheNeutralLayout(t *testing.T) {
	dir := workingCopy(t, map[string]string{
		core.ConfigFile:   `{"schema":1,"packs":["android"],"profile":"library","disabledRules":["app-tracker"],"ruleOptions":{"line-too-long":{"max":120}}}`,
		core.CriteriaFile: "## Project rules\n\n- `app-tracker` — **MAJOR**. Analytics go through the tracker.\n- `app-threads` — **BLOCKER**. No disk access on the main thread.\n",
	})
	c := check(t, dir)
	if c.Source != core.SourceOverlay || len(c.Ignored) != 0 {
		t.Fatalf("source %q, ignored %v", c.Source, ignoredNotes(c))
	}
	var ids []string
	for _, r := range c.Rules {
		ids = append(ids, r.ID+" "+string(r.Severity)+" "+r.File)
	}
	if want := "app-tracker MAJOR " + core.CriteriaFile + ",app-threads BLOCKER " + core.CriteriaFile; strings.Join(ids, ",") != want {
		t.Errorf("rules = %v", ids)
	}
	if len(c.DisabledRules) != 1 || c.DisabledRules[0] != "app-tracker" || c.RuleOptions["line-too-long"]["max"] != 120 {
		t.Errorf("disabledRules %v, ruleOptions %v", c.DisabledRules, c.RuleOptions)
	}
	if len(c.Packs) != 1 || c.Profile != "library" {
		t.Errorf("packs %v, profile %q", c.Packs, c.Profile)
	}
}

func TestCheckCriteriaListsWhatAReviewWouldIgnore(t *testing.T) {
	dir := workingCopy(t, map[string]string{
		core.ConfigFile:   `{"schema":1,"packs":["android","no-such-pack"],"disabledRules":["no-double-bang","app-unknown"],"ruleOptions":{"line-too-long":{"max":999}}}`,
		core.CriteriaFile: "## Rules\n\n- `Bad Id` — **MAJOR**. Text.\n- `no-double-bang` — **MINOR**. Mine.\n",
	})
	c := check(t, dir)
	got := strings.Join(ignoredNotes(c), "\n")
	for _, want := range []string{
		"`no-such-pack` in packs — no pack with this id is loaded",
		"`no-double-bang` in disabledRules (" + core.ConfigFile + ") — pack and core rules always apply",
		"`app-unknown` in disabledRules (" + core.ConfigFile + ") — no repository rule has this id",
		"`line-too-long.max` in ruleOptions (" + core.ConfigFile + ") — 999 is outside",
		"`Bad Id`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ignored lacks %q:\n%s", want, got)
		}
	}
	// What the criteria hold as Notes is what the report shows: the same items.
	for _, i := range c.Ignored {
		if !strings.Contains(strings.Join(c.Notes, "\n"), i.Note()) {
			t.Errorf("the note of %q is not in Notes", i.Item)
		}
	}
}

func TestCheckCriteriaOfAnInvalidConfigIsOneIgnoredFile(t *testing.T) {
	dir := workingCopy(t, map[string]string{
		core.ConfigFile:   `{"schema":1,"unknown":true,"cascade":{"maxChangeRequests":0}}`,
		core.CriteriaFile: "- `app-tracker` — **MAJOR**. Text.\n",
	})
	c := check(t, dir)
	if len(c.Ignored) != 1 || c.Ignored[0].Item != "`"+core.ConfigFile+"`" || !strings.Contains(c.Ignored[0].Reason, "none of the repository criteria apply") {
		t.Fatalf("ignored = %v", ignoredNotes(c))
	}
	if !strings.Contains(c.Ignored[0].Reason, "/unknown") || !strings.Contains(c.Ignored[0].Reason, "/cascade/maxChangeRequests") {
		t.Errorf("the reason does not name every invalid field: %q", c.Ignored[0].Reason)
	}
	if len(c.Rules) != 0 {
		t.Errorf("rules = %v: none of the criteria apply", c.Rules)
	}
}

func TestCheckCriteriaOfTheLegacyLayout(t *testing.T) {
	dir := workingCopy(t, map[string]string{
		".github/skills/app-code-review/SKILL.md":        "## Project rules\n\n- `app-tracker` — **MAJOR**. Use the tracker.\n",
		".github/skills/app-code-review/references/a.md": "# A\n\n## Threads\n\n- `app-main-thread` — **BLOCKER**. No disk access on the main thread.\n",
		".github/mr-review.json":                         `{"approvals":{},"lineLength":300}`,
	})
	c := check(t, dir)
	if c.Source != core.SourceLegacy || len(c.Rules) != 2 {
		t.Errorf("source %q, rules %v", c.Source, c.Rules)
	}
	if got := strings.Join(ignoredNotes(c), "\n"); !strings.Contains(got, "approvals") {
		t.Errorf("ignored = %q, want the approvals setting", got)
	}
}

func TestCheckCriteriaReadsOnlyTheDirectoryItIsGiven(t *testing.T) {
	// The criteria are in a directory above: they are not found by walking up.
	parent := workingCopy(t, map[string]string{core.ConfigFile: `{"schema":1}`, core.CriteriaFile: "- `app-tracker` — **MAJOR**. Text.\n"})
	inner := filepath.Join(parent, "module")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	if c := check(t, inner); c.Source != core.SourceDefault || len(c.Rules) != 0 {
		t.Errorf("found criteria above the directory: %+v", c)
	}
	// A git directory is not read: its objects are never criteria.
	repo := workingCopy(t, map[string]string{".git/HEAD": "ref: refs/heads/main\n", ".git/objects/ab/cdef": "not an object"})
	if c := check(t, repo); c.Source != core.SourceDefault {
		t.Errorf("criteria = %+v", c)
	}
}

func TestCheckCriteriaRefusesALinkOutOfTheDirectory(t *testing.T) {
	outside := workingCopy(t, map[string]string{"criteria.md": "- `app-tracker` — **MAJOR**. Text.\n"})
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agent-code-review"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "criteria.md"), filepath.Join(dir, core.CriteriaFile)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symbolic links need a privilege on Windows")
		}
		t.Fatal(err)
	}
	_, err := CheckCriteria(context.Background(), dir, loadPacks(t))
	if f := asFailure(t, err, KindUnexpected); !strings.Contains(f.Where, "criteria.md") {
		t.Errorf("Where = %q", f.Where)
	}
}

func TestCheckCriteriaNeedsADirectory(t *testing.T) {
	file := workingCopy(t, map[string]string{"a.txt": "x"})
	for name, dir := range map[string]string{"missing": filepath.Join(t.TempDir(), "nothing"), "file": filepath.Join(file, "a.txt")} {
		t.Run(name, func(t *testing.T) {
			_, err := CheckCriteria(context.Background(), dir, loadPacks(t))
			asFailure(t, err, KindUsage)
		})
	}
}

func TestCheckCriteriaRefusesAFileThatIsTooLarge(t *testing.T) {
	dir := workingCopy(t, map[string]string{core.CriteriaFile: strings.Repeat("x", maxCriteriaFile+1)})
	_, err := CheckCriteria(context.Background(), dir, loadPacks(t))
	if f := asFailure(t, err, KindUnexpected); !strings.Contains(f.Why, "larger than") {
		t.Errorf("Why = %q", f.Why)
	}
}

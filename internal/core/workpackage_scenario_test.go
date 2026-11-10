package core_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/lexer"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
)

// buildFiles are synthetic build files of the sandbox repositories, which
// the recordings do not hold: enough for the packs to recognise each one.
var buildFiles = map[string]map[string]string{
	"sample-app":  {"app/build.gradle.kts": "plugins { id(\"com.android.application\") }\ndependencies { implementation(\"androidx.compose.ui:ui\") }\n"},
	"core-lib":    {"build.gradle.kts": "plugins {\n    id(\"com.android.library\")\n    id(\"maven-publish\")\n}\n"},
	"feature-lib": {"build.gradle.kts": "plugins { id(\"com.android.library\") }\nandroid { buildFeatures { viewBinding = true } }\n"},
	"kmp-shared":  {"shared/build.gradle.kts": "plugins {\n    kotlin(\"multiplatform\")\n    id(\"com.android.library\")\n}\n"},
}

// fakeRepo is the repository of a change request at head, in memory: its
// changed files, its build files and a test next to each changed source,
// as the sandbox repositories have. It is the packs.Reader and the
// rules.Tree of the review.
type fakeRepo struct {
	files map[string]string
}

func newFakeRepo(repo string, r core.Review) *fakeRepo {
	f := &fakeRepo{files: map[string]string{}}
	for p, src := range buildFiles[repo] {
		f.files[p] = src
	}
	for _, c := range r.Files {
		if c.Diff.Status == core.FileRemoved {
			continue
		}
		p := c.Diff.Path
		f.files[p] = strings.Join(c.Head, "\n")
		for _, set := range [][2]string{{"src/main/", "src/test/"}, {"src/commonMain/", "src/commonTest/"}} {
			if strings.Contains(p, set[0]) && strings.HasSuffix(p, ".kt") {
				f.files[strings.Replace(strings.TrimSuffix(p, ".kt"), set[0], set[1], 1)+"Test.kt"] = "class Test\n"
			}
		}
	}
	return f
}

func (f *fakeRepo) paths() []string {
	var out []string
	for p := range f.files {
		out = append(out, p)
	}
	return out
}

func (f *fakeRepo) ReadFile(p string) ([]byte, error) {
	if src, ok := f.files[p]; ok {
		return []byte(src), nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeRepo) List(prefix string) ([]string, error) {
	var out []string
	for p := range f.files {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out, nil
}

// scenarioSearch finds nothing, except in sample-app at main, whose
// recorded contents it searches.
type scenarioSearch struct{ sampleApp *recordedSearch }

func (s scenarioSearch) Search(project, ref, term string) ([]rules.Match, error) {
	if project == sampleAppKey && ref == "main" {
		return s.sampleApp.Search(project, ref, term)
	}
	return nil, nil
}

// scenarioCR is a change request of a scenario and those above it.
type scenarioCR struct {
	scen, repo string
	number     int
	ancestors  []scenarioCR
}

func (c scenarioCR) name() string { return c.scen + " " + c.repo + "#" + strconv.Itoa(c.number) }

func profileReason(p packs.Profile) string {
	switch p.Reason {
	case packs.ProfileFromConfig:
		return "set in the repository criteria"
	case packs.ProfileFromCascade:
		return "cascade dependency of " + p.Detail
	case packs.ProfileFromPackDefault:
		return p.Detail + " default"
	}
	return p.Detail
}

// buildScenario collects a scenario change request as the review command
// will, from the recordings and the fakes, and builds its work package.
func buildScenario(t *testing.T, cr scenarioCR, budget int) (core.WorkPackage, core.Review) {
	t.Helper()
	set, err := packs.Load(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	review := scenario.Review(t, cr.repo, cr.number, cr.scen)
	repo := newFakeRepo(cr.repo, review)
	crit, err := readRecorded(t, cr.scen, cr.repo, cr.number, "main", ".github/skills/"+cr.repo+"-code-review/references")
	if err != nil {
		t.Fatal(err)
	}
	var active []*packs.Pack
	if len(crit.Packs) > 0 {
		for _, p := range set.Packs {
			for _, id := range crit.Packs {
				if p.ID == id {
					active = append(active, p)
				}
			}
		}
	} else if active, err = set.Activate(repo.paths(), repo); err != nil {
		t.Fatal(err)
	}
	in := packs.ProfileInput{Configured: crit.Profile}
	var ancestors []core.Ancestor
	var engineAncestors []rules.Ancestor
	for _, a := range cr.ancestors {
		key := scenario.Key(t, a.repo, a.number)
		ancestors = append(ancestors, core.Ancestor{CR: key, Title: a.name()})
		ar := scenario.Review(t, a.repo, a.number, a.scen)
		engineAncestors = append(engineAncestors, rules.Ancestor{Project: key.ProjectKey(), Head: ar.HeadSHA, Target: "main", Files: rulesFiles(t, set, active, ar)})
		in.DependentCR = key.String()
	}
	profile, err := packs.ResolveProfile(active, in, repo.paths(), repo)
	if err != nil {
		t.Fatal(err)
	}
	dims, err := packs.ResolveDimensions(active, crit.Dimensions, repo.paths(), repo)
	if err != nil {
		t.Fatal(err)
	}
	choices := map[string]core.DimensionChoice{}
	for _, d := range dims {
		reason := d.Detail
		if d.Reason == packs.DimensionFromConfig {
			reason = "set in the repository criteria"
		}
		choices[d.ID] = core.DimensionChoice{Value: d.Value, Reason: reason}
	}
	rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: profile.Name, Dimensions: dims, Criteria: crit})
	var files []core.PackageFileInput
	var fileSignals []rules.Signal
	var notVerified []core.UnverifiedFile
	var engineFiles []rules.ChangedFile
	for _, f := range review.Files {
		a := set.Assign(active, f.Diff.Path)
		in := core.PackageFileInput{File: f}
		var assigned rules.File
		switch {
		case a.NotVerified != nil:
			notVerified = append(notVerified, core.UnverifiedFile{Path: f.Diff.Path, Reason: a.NotVerified.Reason, Detail: a.NotVerified.Detail})
		case !f.Diff.Visible() && f.Diff.Status != core.FileRenamed:
			notVerified = append(notVerified, core.UnverifiedFile{Path: f.Diff.Path, Reason: core.ReasonPatchUnavailable})
			in.Language = a.Language.ID
		default:
			in.Language = a.Language.ID
			assigned = a.File("", nil)
			if f.Diff.Status != core.FileRemoved && f.Diff.Visible() {
				res := rules.Run(rs.ForFile(a.Context(profile.Name, dims)), a.File(strings.Join(f.Head, "\n")+"\n", f.Diff.AddedLines()), rules.Options{})
				fileSignals = append(fileSignals, res.Signals...)
				for _, n := range res.NotVerified {
					notVerified = append(notVerified, core.UnverifiedFile{Path: n.Path, Reason: n.Reason, Detail: n.Detail})
				}
			}
		}
		files = append(files, in)
		engineFiles = append(engineFiles, f.RulesFile(assigned))
	}
	key := scenario.Key(t, cr.repo, cr.number)
	res := rules.RunChangeRequest(rs.PackRules(), rules.ChangeRequest{
		Files: engineFiles, Tree: repo, Variants: crit.Variants, Project: key.ProjectKey(), Head: review.HeadSHA, Target: "main",
		Search: scenarioSearch{newRecordedSearch(t)}, Ancestors: engineAncestors,
		TableOf: func(path string) *lexer.Table {
			if a := set.Assign(active, path); a.NotVerified == nil {
				return a.Language.Table
			}
			return nil
		},
	})
	for _, n := range res.NotVerified {
		notVerified = append(notVerified, core.UnverifiedFile{Path: n.Path, Reason: n.Reason, Detail: n.Detail})
	}
	pull := sandbox.PullRequest(t, cr.scen, cr.repo, cr.number)
	return core.BuildWorkPackage(core.WorkPackageInput{
		RunID: "20261109-130000-" + strings.ToLower(cr.scen), CR: key,
		URL:   "https://github.com/" + key.Project + "/pull/" + strconv.Itoa(key.Number),
		Title: cr.name(), Description: "Recorded scenario " + cr.scen + ".", HeadSHA: pull.HeadSHA, BaseSHA: pull.BaseSHA, BaseRef: "main",
		Files: files, Ruleset: rs, Profile: profile.Name, ProfileReason: profileReason(profile), Dimensions: choices, Criteria: crit,
		FileSignals: fileSignals, ChangeRequestSignals: res.Signals, NotVerified: notVerified, Ancestors: ancestors,
		JudgementPath: strings.ReplaceAll(key.Project, "/", "_") + "/judgement.json", PartBudget: budget,
	})
}

var (
	s1      = scenarioCR{scen: "S1", repo: "sample-app", number: 2}
	s2Root  = scenarioCR{scen: "S2", repo: "sample-app", number: 7}
	s2Lib   = scenarioCR{scen: "S2", repo: "core-lib", number: 3, ancestors: []scenarioCR{s2Root}}
	s2KMP   = scenarioCR{scen: "S2", repo: "kmp-shared", number: 2, ancestors: []scenarioCR{s2Root}}
	s3      = scenarioCR{scen: "S3", repo: "core-lib", number: 1}
	s4      = scenarioCR{scen: "S4", repo: "feature-lib", number: 1}
	s5a     = scenarioCR{scen: "S5", repo: "sample-app", number: 3}
	s5b     = scenarioCR{scen: "S5", repo: "sample-app", number: 4}
	s6a     = scenarioCR{scen: "S6", repo: "sample-app", number: 5}
	s6c     = scenarioCR{scen: "S6", repo: "core-lib", number: 2}
	s6d     = scenarioCR{scen: "S6", repo: "kmp-shared", number: 1}
	allRecs = []scenarioCR{s1, s2Root, s2Lib, s2KMP, s3, s4, s5a, s5b, s6a, s6c, s6d}
)

func TestScenarioWorkPackages(t *testing.T) {
	for _, cr := range allRecs {
		t.Run(cr.name(), func(t *testing.T) {
			wp, review := buildScenario(t, cr, 0)
			first := checkWorkPackage(t, wp)
			again, _ := buildScenario(t, cr, 0)
			if second, _ := json.Marshal(again); !bytes.Equal(first, second) {
				t.Error("the same recordings give different bytes")
			}
			if len(review.Files) != len(wp.Files) || len(review.Signals) != len(wp.Signals) {
				t.Errorf("review: %d files and %d signals; work package: %d and %d", len(review.Files), len(review.Signals), len(wp.Files), len(wp.Signals))
			}
			for i, f := range wp.Files {
				head := review.Files[i].Head
				if f.Code == nil {
					continue
				}
				for _, r := range f.Code.Regions {
					for j, l := range r.Lines {
						if head[r.Start-1+j] != l {
							t.Fatalf("%s:%d is %q at head, %q in the work package", f.Path, r.Start+j, head[r.Start-1+j], l)
						}
					}
				}
			}
			split, _ := buildScenario(t, cr, 1)
			checkWorkPackage(t, split)
			if len(wp.Files) > 1 && split.Parts.Total != len(wp.Files) {
				t.Errorf("%d parts with a budget of one byte, want one per file", split.Parts.Total)
			}
		})
	}
}

var update = flag.Bool("update", false, "rewrite the golden work packages in testdata/workpackage")

// TestScenarioS1WorkPackage compares the work package of S1 with its
// golden file, which the contract guard also checks.
func TestScenarioS1WorkPackage(t *testing.T) {
	wp, _ := buildScenario(t, s1, 0)
	got, err := json.MarshalIndent(wp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("..", "..", "testdata", "workpackage", "s1-sample-app-2-workpackage.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/core -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the S1 work package differs from %s (run go test ./internal/core -update):\n%s", path, got)
	}
	if wp.Criteria.Source != core.SourceOverlay || wp.Criteria.Profile != "app" || wp.Criteria.Dimensions["ui-toolkit"].Value != "compose" {
		t.Errorf("criteria %+v", wp.Criteria)
	}
}

func file(t *testing.T, wp core.WorkPackage, base string) core.PackageFile {
	t.Helper()
	for _, f := range wp.Files {
		if filepath.Base(f.Path) == base {
			return f
		}
	}
	t.Fatalf("no file %s", base)
	return core.PackageFile{}
}

func TestScenarioWorkPackageFiles(t *testing.T) {
	s3wp, _ := buildScenario(t, s3, 0)
	s4wp, _ := buildScenario(t, s4, 0)
	s6wp, _ := buildScenario(t, s6d, 0)
	tests := []struct {
		wp                    core.WorkPackage
		base, placement, code string
		reason                string
	}{
		{s3wp, "LegacySessionStore.kt", "inline", "no code", ""},
		{s3wp, "SessionExpiry.kt", "inline", "whole 1-", ""},
		{s3wp, "TokenCodec.kt", "inline", "whole 1-", ""},
		{s3wp, "StatusBadgeView.kt", "summary", "no code", core.ReasonRenamedUnchanged},
		{s3wp, "badge_background.png", "summary", "no code", "unsupported-language"},
		{s4wp, "IconCatalog.kt", "summary", "no code", core.ReasonPatchUnavailable},
		{s6wp, "ContentView.swift", "inline", "no code", ""},
	}
	for _, tt := range tests {
		f := file(t, tt.wp, tt.base)
		if string(f.Placement) != tt.placement || !strings.HasPrefix(regions(f), tt.code) || f.Reason != tt.reason {
			t.Errorf("%s: %s %s, reason %q; want %s %s, reason %q", tt.base, f.Placement, regions(f), f.Reason, tt.placement, tt.code, tt.reason)
		}
	}
	lib, _ := buildScenario(t, s2Lib, 0)
	if len(lib.Cascade.Ancestors) != 1 || lib.Criteria.Profile != "library" || len(lib.Parts.ChangeRequestSignals) != 1 {
		t.Errorf("core-lib below sample-app: ancestors %+v, profile %s, change-request signals %q", lib.Cascade.Ancestors, lib.Criteria.Profile, lib.Parts.ChangeRequestSignals)
	}
}

// TestScenarioJudgementsRoundTrip checks the hand-written judgements of
// S1 and of the S2 root against the review built with their work
// packages. They predate the packs: they report as findings what the
// signals point at, and decide no signal. Without the signals they are
// valid; with them, each finding on a signal and each signal that needs
// judgement is an error, exactly. The judgements in testdata/workpackage
// are the same review written for the work package: they decide the
// signals and are valid.
func TestScenarioJudgementsRoundTrip(t *testing.T) {
	const s1Dir = "app/src/main/java/com/example/sample/"
	tests := []struct {
		cr      scenarioCR
		decided string
		errors  []string
		verdict core.Verdict
		counts  [2]int
	}{
		{s1, "s1-sample-app-2-judgement.json", []string{
			"/findings/0: repeats signal s1; decide the signal instead",
			"/findings/1: repeats signal s6; decide the signal instead",
			"/findings/2: repeats signal s7; decide the signal instead",
			"/preExisting/0: repeats signal s2; decide the signal instead",
			"/preExisting/1: repeats signal s4; decide the signal instead",
			"/signals: signal s2 (no-double-bang at " + s1Dir + "data/ProfileStore.kt:8) needs judgement: confirm or dismiss it",
			"/signals: signal s3 (missing-kdoc at " + s1Dir + "data/ProfileStore.kt:10) needs judgement: confirm or dismiss it",
			"/signals: signal s5 (missing-kdoc at " + s1Dir + "data/SessionRepository.kt:18) needs judgement: confirm or dismiss it",
			"/signals: signal s7 (no-double-bang at " + s1Dir + "data/SessionRepository.kt:20) needs judgement: confirm or dismiss it",
		}, core.Blocked, [2]int{5, 2}},
		{s2Root, "s2-sample-app-7-judgement.json", []string{
			"/signals: signal s2 (missing-kdoc at " + s1Dir + "data/SessionRepository.kt:20) needs judgement: confirm or dismiss it",
			"/signals: signal s3 (missing-kdoc at " + s1Dir + "ui/HomeViewModel.kt:36) needs judgement: confirm or dismiss it",
		}, core.OK, [2]int{0, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.cr.name(), func(t *testing.T) {
			_, review := buildScenario(t, tt.cr, 0)
			j := scenario.Judgement(t, tt.cr.scen, tt.cr.repo, tt.cr.number)
			withoutSignals := review
			withoutSignals.Signals = nil
			if err := core.ValidateJudgement(withoutSignals, j); err != nil {
				t.Errorf("without signals: %v", err)
			}
			var verr core.ValidationError
			if err := core.ValidateJudgement(review, j); !errors.As(err, &verr) {
				t.Fatalf("with signals: %v, want a validation error", err)
			}
			var got []string
			for _, e := range verr {
				got = append(got, e.Pointer+": "+e.Message)
			}
			if strings.Join(got, "\n") != strings.Join(tt.errors, "\n") {
				t.Errorf("errors\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.errors, "\n"))
			}
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "workpackage", tt.decided))
			if err != nil {
				t.Fatal(err)
			}
			decided, err := core.DecodeJudgement(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := core.ValidateJudgement(review, decided); err != nil {
				t.Errorf("the judgement that decides the signals: %v", err)
			}
			res, err := core.Finalize(review, decided)
			if err != nil {
				t.Fatal(err)
			}
			if res.Verdict != tt.verdict || len(res.Findings) != tt.counts[0] || len(res.PreExisting) != tt.counts[1] {
				t.Errorf("verdict %s, %d findings, %d pre-existing; want %s, %d, %d", res.Verdict, len(res.Findings), len(res.PreExisting), tt.verdict, tt.counts[0], tt.counts[1])
			}
		})
	}
}

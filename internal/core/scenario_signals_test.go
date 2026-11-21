package core_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/lexer"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
	"github.com/Brg92/agent-code-review/internal/testutil/productflow"
	"github.com/Brg92/agent-code-review/internal/testutil/sandbox"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
)

var (
	s7App = scenarioCR{scen: "S7", repo: "sample-app", number: 8}
	s7KMP = scenarioCR{scen: "S7", repo: "kmp-shared", number: 3}
)

// recordedRepo is a repository at head as the recordings hold it: the
// recorded tree, the contents of the changed files and the synthetic build
// files of buildFiles, which the recordings do not hold. It is the
// packs.Reader of the review; its Tree is the recorded one.
type recordedRepo struct {
	tree  rules.Tree
	paths []string
	files map[string]string
}

func newRecordedRepo(t *testing.T, cr scenarioCR, r core.Review) recordedRepo {
	t.Helper()
	rec := sandbox.TreeOf(t, cr.scen, cr.repo, r.HeadSHA)
	if !rec.Recorded || rec.Truncated {
		t.Fatalf("%s: tree recorded %v, truncated %v", cr.name(), rec.Recorded, rec.Truncated)
	}
	repo := recordedRepo{tree: scenario.RecordedTreeOrError(cr.scen, rec), paths: rec.Paths, files: map[string]string{}}
	for p, src := range buildFiles[cr.repo] {
		repo.files[p] = src
	}
	for _, f := range r.Files {
		if f.Diff.Status != core.FileRemoved && f.Head != nil {
			repo.files[f.Diff.Path] = strings.Join(f.Head, "\n") + "\n"
		}
	}
	return repo
}

func (r recordedRepo) ReadFile(p string) ([]byte, error) {
	if src, ok := r.files[p]; ok {
		return []byte(src), nil
	}
	// A file of the tree whose contents were not recorded exists, empty.
	for _, q := range r.paths {
		if q == p {
			return nil, nil
		}
	}
	return nil, os.ErrNotExist
}

func (r recordedRepo) List(prefix string) ([]string, error) { return r.tree.List(prefix) }

// changeRequestSignals runs the change-request rules on a recorded change
// request with its recorded tree and, through search, its recorded code
// searches. It numbers the signals as the work package does.
func changeRequestSignals(t *testing.T, cr scenarioCR, search rules.Search) (rules.Result, core.Review) {
	t.Helper()
	set, err := packs.Load(filepath.Join("..", "..", "packs"))
	if err != nil {
		t.Fatal(err)
	}
	review := scenario.Review(t, cr.repo, cr.number, cr.scen)
	repo := newRecordedRepo(t, cr, review)
	pull := sandbox.PullRequest(t, cr.scen, cr.repo, cr.number)
	crit, err := readRecorded(t, cr.scen, cr.repo, cr.number, pull.BaseRef, ".github/skills/"+cr.repo+"-code-review/references")
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
	} else if active, err = set.Activate(repo.paths, repo); err != nil {
		t.Fatal(err)
	}
	in := packs.ProfileInput{Configured: crit.Profile}
	var ancestors []rules.Ancestor
	for _, a := range cr.ancestors {
		key := scenario.Key(t, a.repo, a.number)
		ar := scenario.Review(t, a.repo, a.number, a.scen)
		ancestors = append(ancestors, rules.Ancestor{Project: key.ProjectKey(), Head: ar.HeadSHA, Target: "main", Files: rulesFiles(t, set, active, ar)})
		in.DependentCR = key.String()
	}
	profile, err := packs.ResolveProfile(active, in, repo.paths, repo)
	if err != nil {
		t.Fatal(err)
	}
	dims, err := packs.ResolveDimensions(active, crit.Dimensions, repo.paths, repo)
	if err != nil {
		t.Fatal(err)
	}
	rs := core.EffectiveRules(core.RulesetInput{Set: set, Active: active, Profile: profile.Name, Dimensions: dims, Criteria: crit})
	// The signals of each file, as the work package lists them first.
	var fileSignals []rules.Signal
	for _, f := range review.Files {
		a := set.Assign(active, f.Diff.Path)
		if a.NotVerified != nil || !f.Diff.Visible() || f.Diff.Status == core.FileRemoved {
			continue
		}
		res := rules.Run(rs.ForFile(a.Context(profile.Name, dims)), a.File(strings.Join(f.Head, "\n")+"\n", f.Diff.AddedLines()), rules.Options{})
		fileSignals = append(fileSignals, res.Signals...)
	}
	res := rules.RunChangeRequest(rs.PackRules(), rules.ChangeRequest{
		Files: rulesFiles(t, set, active, review), Tree: repo, Variants: crit.Variants,
		Project: scenario.Key(t, cr.repo, cr.number).ProjectKey(), Head: review.HeadSHA, Target: pull.BaseRef,
		Search: search, Ancestors: ancestors,
		TableOf: func(path string) *lexer.Table {
			if a := set.Assign(active, path); a.NotVerified == nil {
				return a.Language.Table
			}
			return nil
		},
	})
	review.Rules = rs.ByID()
	review.Signals = core.NumberSignals(review.Files, fileSignals, res.Signals)
	return res, review
}

// signalWant is a change-request signal a recorded change request must
// give, or, for kind "not verified", a file one of its checks could not
// verify: the detail names the rule.
type signalWant struct {
	rule, path string
	line       int
	kind       string
}

const (
	kindAutomatic     = "automatic"
	kindJudgement     = "needs judgement"
	kindNotVerified   = "not verified"
	readmeSignalsHead = "## Change-request signals"
)

const (
	appDir   = "app/src/main/java/com/example/sample/"
	coreDir  = "src/main/java/com/example/core/session/"
	kmpDir   = "shared/src/"
	androidP = kmpDir + "androidMain/kotlin/com/example/shared/Platform.android.kt"
)

// crWants is what the change-request rules give on the recordings. Only
// missing-tests, variant-asymmetry and the asymmetry and expect/actual
// rules of the packs have a recorded tree; uses-removed-api needs a code
// search of sample-app, which GitHub does not serve (see S2/README.md).
var crWants = []struct {
	cr    scenarioCR
	wants []signalWant
}{
	{s1, []signalWant{
		{"missing-tests", appDir + "data/SessionRepository.kt", 7, kindAutomatic},
		{"missing-tests", appDir + "ui/HomeViewModel.kt", 10, kindAutomatic},
	}},
	{s2Root, []signalWant{
		{"missing-tests", appDir + "data/SessionRepository.kt", 7, kindAutomatic},
		{"missing-tests", appDir + "ui/HomeViewModel.kt", 10, kindAutomatic},
	}},
	{s2Lib, []signalWant{
		{"missing-tests", coreDir + "SessionManager.kt", 7, kindAutomatic},
		{"uses-removed-api", coreDir + "SessionManager.kt", 0, kindNotVerified},
	}},
	{s2KMP, nil},
	{s3, []signalWant{{"missing-tests", coreDir + "SessionManager.kt", 8, kindAutomatic}}},
	{s4, nil},
	{s5a, nil},
	{s5b, nil},
	{s6a, nil},
	{s6c, []signalWant{{"missing-tests", coreDir + "SessionManager.kt", 7, kindAutomatic}}},
	{s6d, nil},
	{s7App, []signalWant{
		{"variant-asymmetry", "app/src/free/java/com/example/sample/FeatureFlags.kt", 5, kindJudgement},
		{"missing-tests", appDir + "data/AvatarMapper.kt", 4, kindAutomatic},
	}},
	{s7KMP, []signalWant{
		{"kmp-source-set-asymmetry", androidP, 5, kindJudgement},
		{"kmp-expect-without-actual", kmpDir + "commonMain/kotlin/com/example/shared/DeviceInfo.kt", 0, kindNotVerified},
	}},
}

// gotSignals lists what the change-request rules gave, in the form of
// signalWant.
func gotSignals(res rules.Result) []signalWant {
	var out []signalWant
	for _, s := range res.Signals {
		kind := kindAutomatic
		if s.NeedsJudgement {
			kind = kindJudgement
		}
		out = append(out, signalWant{s.RuleID, s.Path, s.Line, kind})
	}
	for _, n := range res.NotVerified {
		rule, _, _ := strings.Cut(n.Detail, ":")
		out = append(out, signalWant{rule, n.Path, 0, kindNotVerified + ": " + n.Reason})
	}
	return out
}

func TestScenarioChangeRequestSignals(t *testing.T) {
	for _, tt := range crWants {
		t.Run(tt.cr.name(), func(t *testing.T) {
			res, _ := changeRequestSignals(t, tt.cr, scenario.RecordedSearch(t, tt.cr.scen))
			got := gotSignals(res)
			var want []signalWant
			for _, w := range tt.wants {
				if w.kind == kindNotVerified {
					w.kind += ": search-unavailable"
				}
				want = append(want, w)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("change-request signals\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// readmeRow is a row of the table of change-request signals of a README.
type readmeRow struct{ cr, rule, file, kind string }

func readmeRows(t *testing.T, scen string) []readmeRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "scenarios", scen, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []readmeRow
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			in = line == readmeSignalsHead
		case in && strings.HasPrefix(line, "|"):
			cells := strings.Split(strings.Trim(line, "| "), "|")
			for i := range cells {
				cells[i] = strings.Trim(strings.TrimSpace(cells[i]), "`")
			}
			if len(cells) != 4 || cells[0] == "Change request" || strings.HasPrefix(cells[0], "---") {
				continue
			}
			kind := cells[3]
			for _, k := range []string{kindAutomatic, kindJudgement, kindNotVerified} {
				if strings.HasPrefix(kind, k) {
					kind = k
				}
			}
			rows = append(rows, readmeRow{cells[0], cells[1], cells[2], kind})
		}
	}
	return rows
}

// TestScenarioREADMEsListTheSignals keeps the table of every scenario
// README equal to what the change-request rules give on the recordings:
// no signal, automatic or not, and no file that could not be verified, is
// missing from it, and it lists nothing that the rules do not give.
func TestScenarioREADMEsListTheSignals(t *testing.T) {
	used := map[string]map[int]bool{}
	for _, tt := range crWants {
		scen := tt.cr.scen
		rows := readmeRows(t, scen)
		if used[scen] == nil {
			used[scen] = map[int]bool{}
		}
		res, _ := changeRequestSignals(t, tt.cr, scenario.RecordedSearch(t, scen))
		name := tt.cr.repo + "#" + strconv.Itoa(tt.cr.number)
		for _, g := range gotSignals(res) {
			kind, _, _ := strings.Cut(g.kind, ":")
			found := false
			for i, r := range rows {
				if !used[scen][i] && r.cr == name && r.rule == g.rule && r.kind == kind && strings.HasSuffix(g.path, r.file) {
					used[scen][i], found = true, true
					break
				}
			}
			if !found {
				t.Errorf("%s README: no row for %s of %s in %s (%s)", scen, g.rule, g.path, name, g.kind)
			}
		}
	}
	for scen, set := range used {
		for i, r := range readmeRows(t, scen) {
			if !set[i] {
				t.Errorf("%s README lists %+v, which the change-request rules do not give", scen, r)
			}
		}
	}
}

// decidedBy reports whether the judgement decides the signal: by a
// decision on its id, or, as the judgements written before the packs do,
// by a finding on the same rule and line.
func decidedBy(j core.Judgement, s core.Signal) bool {
	for _, d := range j.Signals {
		if d.ID == s.ID {
			return true
		}
	}
	for _, f := range append(append([]core.JudgedFinding{}, j.Findings...), j.PreExisting...) {
		if f.Rule == s.Rule && f.Path == s.Path && f.Side == sideOf(s) && f.Line == s.Line {
			return true
		}
	}
	return false
}

func sideOf(s core.Signal) core.Side {
	if s.Side == core.Left {
		return core.Left
	}
	return core.Right
}

// TestScenarioJudgementsDecideTheirSignals checks that every signal of the
// change-request rules that needs judgement is decided in the hand-written
// judgement of its change request. The signals of single files, such as
// missing-kdoc, are out of its scope: the judgements decide them by the ids
// of the work package review builds, which the guard in internal/review
// validates.
func TestScenarioJudgementsDecideTheirSignals(t *testing.T) {
	for _, tt := range crWants {
		t.Run(tt.cr.name(), func(t *testing.T) {
			res, review := changeRequestSignals(t, tt.cr, scenario.RecordedSearch(t, tt.cr.scen))
			j := scenario.Judgement(t, tt.cr.scen, tt.cr.repo, tt.cr.number)
			fromChangeRequest := map[string]bool{}
			for _, g := range res.Signals {
				fromChangeRequest[g.RuleID+" "+g.Path+" "+strconv.Itoa(g.Line)] = true
			}
			for _, s := range review.Signals {
				if s.NeedsJudgement && fromChangeRequest[s.Rule+" "+s.Path+" "+strconv.Itoa(s.Line)] && !decidedBy(j, s) {
					t.Errorf("signal %s (%s at %s:%d) needs judgement and %s does not decide it", s.ID, s.Rule, s.Path, s.Line, tt.cr.name())
				}
			}
		})
	}
}

// completeSearch is a code search that works and finds nothing: what the
// index of a repository answers for a term that exists nowhere on its
// default branch.
type completeSearch struct{}

func (completeSearch) Search(project, ref, term string) ([]rules.Match, error) { return nil, nil }

// TestScenarioS7 runs the judgements of S7: from the recordings, as the
// product does, and kmp-shared with a code search that works.
func TestScenarioS7(t *testing.T) {
	// sample-app: the judgement decides the signals and is valid; the
	// change request is OK with two MAJOR findings.
	res := finalize(t, "S7", "sample-app", 8, nil)
	if res.Verdict != core.OK || len(res.Findings) != 2 || len(res.Dismissed) != 0 {
		t.Errorf("sample-app#8: verdict %s, %d findings, %d dismissed; want OK, 2, 0", res.Verdict, len(res.Findings), len(res.Dismissed))
	}
	for _, f := range res.Findings {
		if f.Severity != core.Major || (f.Rule != "variant-asymmetry" && f.Rule != "missing-tests") {
			t.Errorf("finding %s %s on %s", f.Rule, f.Severity, f.Path)
		}
	}

	// kmp-shared from the recordings: the expect cannot be searched, so
	// there is no signal for it, only the file it is in, not verified. The
	// judgement dismisses the asymmetry and the change request is OK.
	flow := productflow.Collect(t, "S7", "kmp-shared", 3).Of(t, "kmp-shared", 3)
	for _, s := range flow.Review.Signals {
		if s.Rule == "kmp-expect-without-actual" {
			t.Errorf("signal %s: the recorded search cannot give it", s.ID)
		}
	}
	const deviceInfo = "shared/src/commonMain/kotlin/com/example/shared/DeviceInfo.kt"
	if !slices.ContainsFunc(flow.WorkPackage.NotVerified, func(n core.UnverifiedFile) bool {
		return n.Path == deviceInfo && n.Reason == "search-unavailable"
	}) {
		t.Errorf("not verified %+v, want %s with search-unavailable", flow.WorkPackage.NotVerified, deviceInfo)
	}
	res = flow.Finalize(t, nil)
	checkContract(t, res)
	if res.Verdict != core.OK || len(res.Findings) != 0 || len(res.Dismissed) != 1 {
		t.Errorf("kmp-shared#3 from the recordings: verdict %s, %d findings, %d dismissed; want OK, 0, 1", res.Verdict, len(res.Findings), len(res.Dismissed))
	}

	// With a search that works, the expect without an iOS actual is a
	// BLOCKER candidate, s2, that the judgement confirms (the product has
	// no such signal to decide, so the test adds the decision); the
	// asymmetry stays dismissed.
	res2, review := changeRequestSignals(t, s7KMP, completeSearch{})
	if len(res2.NotVerified) != 0 || len(res2.Signals) != 2 || res2.Signals[1].RuleID != "kmp-expect-without-actual" {
		t.Fatalf("kmp-shared#3 with a working search: %+v", res2)
	}
	j := flow.Judgement
	j.Signals = append(append([]core.SignalDecision{}, j.Signals...), core.SignalDecision{ID: "s2", Decision: core.Confirm, Severity: core.Blocker})
	res, err := core.Finalize(review, j)
	if err != nil {
		t.Fatal(err)
	}
	checkContract(t, res)
	if res.Verdict != core.Blocked || len(res.Findings) != 1 || res.Findings[0].Rule != "kmp-expect-without-actual" || len(res.Dismissed) != 1 {
		t.Errorf("kmp-shared#3: verdict %s, findings %+v, %d dismissed; want BLOCKED by the expect, with the asymmetry dismissed", res.Verdict, res.Findings, len(res.Dismissed))
	}
}

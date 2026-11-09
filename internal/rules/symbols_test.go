package rules

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/lexer"
	"github.com/Brg92/agent-code-review/internal/symbols"
)

// packSymbols reads the symbols of a language from the android pack
// manifest.
func packSymbols(t testing.TB, language string) *symbols.Spec {
	t.Helper()
	var m struct {
		Languages []struct {
			ID      string          `json:"id"`
			Symbols json.RawMessage `json:"symbols"`
		} `json:"languages"`
	}
	if err := json.Unmarshal(readFile(t, filepath.Join("..", "..", "packs", "android", "manifest.json")), &m); err != nil {
		t.Fatal(err)
	}
	for _, l := range m.Languages {
		if l.ID == language {
			s, err := symbols.ParseSpec(l.Symbols)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
	}
	t.Fatalf("no symbols for %s", language)
	return nil
}

// fakeSearch answers searches from memory and records them. A search of
// a failing project or term fails.
type fakeSearch struct {
	hits    map[string][]Match // "project ref term"
	failing []string
	calls   []string
}

func (s *fakeSearch) Search(project, ref, term string) ([]Match, error) {
	s.calls = append(s.calls, project+" "+ref+" "+term)
	for _, f := range s.failing {
		if f == project || f == term {
			return nil, errors.New("search failed")
		}
	}
	return s.hits[project+" "+ref+" "+term], nil
}

// symbolFile is a changed Kotlin file with symbols, contents at base and
// at head, and the lines the change adds and removes.
func symbolFile(t testing.TB, path, base, head string, added, removedLines []int) ChangedFile {
	f := changed(t, path, head, added...)
	f.Symbols, f.Base, f.RemovedLines = packSymbols(t, "kotlin"), base, map[int]bool{}
	for _, l := range removedLines {
		f.RemovedLines[l] = true
	}
	return f
}

const (
	coreLib   = "github:github.com/acr-plugin-sandbox/core-lib"
	sampleApp = "github:github.com/acr-plugin-sandbox/sample-app"
	rootApp   = "github:github.com/acr-plugin-sandbox/root-app"

	managerPath = "src/main/java/com/example/core/session/SessionManager.kt"
	managerBase = `package com.example.core.session

class SessionManager {
    fun current(): Session? = session

    @Deprecated("Use an authenticated client instead.")
    fun legacyToken(): String? = session?.token

    fun clearCache() {}

    fun refresh(force: Boolean) {}
}
`
	repoPath = "app/src/main/java/com/example/sample/data/SessionRepository.kt"
)

// managerHead removes legacyToken and clearCache and adds expiresAt;
// legacyToken was on base lines 6 and 7, clearCache on line 9.
var managerHead = strings.Replace(strings.Replace(managerBase,
	"    @Deprecated(\"Use an authenticated client instead.\")\n    fun legacyToken(): String? = session?.token\n",
	"    fun expiresAt(): Long? = session?.expiresAtMillis\n", 1),
	"    fun clearCache() {}\n\n", "", 1)

const (
	removedAPI       = `{"scope":"change-request","detector":"symbol-usage","symbolChange":"removed"}`
	requiredArgument = `{"scope":"change-request","detector":"symbol-usage","symbolChange":"required-parameter"}`
)

// symbolRule loads a judged change-request rule with the given match.
func symbolRule(t testing.TB, id, m string) *Rule {
	t.Helper()
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"core","rules":[{"id":"` + id + `","title":"T","severity":"BLOCKER","needsJudgement":true,` +
		`"description":"D","why":"W","fix":"F","languages":[],"tags":[],"match":` + m + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return c.Rules[0]
}

// coreLibChange is core-lib's change request, below sample-app's in the
// cascade.
func coreLibChange(t testing.TB, search Search, ancestors ...Ancestor) ChangeRequest {
	return ChangeRequest{
		Files:   []ChangedFile{symbolFile(t, managerPath, managerBase, managerHead, []int{6}, []int{6, 7, 9})},
		Project: coreLib, Head: "c0ffee", Target: "main", Search: search, Ancestors: ancestors,
	}
}

// signalsWithRefs renders signals as side:path:line:column, each followed
// by its references.
func signalsWithRefs(sigs []Signal) []string {
	var out []string
	for _, s := range sigs {
		side := s.Side
		if side == "" {
			side = "RIGHT"
		}
		line := side + " " + s.RuleID + " " + s.Path + ":" + strconv.Itoa(s.Line) + ":" + strconv.Itoa(s.Column)
		for _, r := range s.References {
			line += " -> " + strings.TrimPrefix(r.Project, "github:github.com/acr-plugin-sandbox/") + "@" + r.Ref + ":" + r.Path + ":" + strconv.Itoa(r.Line)
		}
		out = append(out, line)
	}
	return out
}

func checkSignals(t *testing.T, res Result, want []string, wantUnverified []NotVerified) {
	t.Helper()
	if got := signalsWithRefs(res.Signals); !reflect.DeepEqual(got, want) {
		t.Errorf("signals:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !reflect.DeepEqual(res.NotVerified, wantUnverified) {
		t.Errorf("not verified %+v, want %+v", res.NotVerified, wantUnverified)
	}
}

func TestSymbolUsageRemoved(t *testing.T) {
	rule := symbolRule(t, "uses-removed-api", removedAPI)
	repoBase := "package com.example.sample.data\n\nclass SessionRepository {\n" +
		"    fun warmUp() {}\n    fun authHeader(): String = \"Bearer ${manager.legacyToken()}\"\n}\n"
	tests := []struct {
		name     string
		hits     map[string][]Match
		files    []ChangedFile // of sample-app
		want     []string
		searched []string
	}{
		{
			name: "use outside the parent's changed files",
			hits: map[string][]Match{sampleApp + " main legacyToken": {{Path: repoPath, Line: 5}}},
			want: []string{"LEFT uses-removed-api " + managerPath + ":7:9 -> sample-app@main:" + repoPath + ":5"},
		},
		{
			name:  "use in a changed file on a line the parent keeps",
			hits:  map[string][]Match{sampleApp + " main legacyToken": {{Path: repoPath, Line: 5}}},
			files: []ChangedFile{symbolFile(t, repoPath, repoBase, strings.Replace(repoBase, "    fun warmUp() {}\n", "    fun warmUp() {}\n    fun expiresAt() = manager.expiresAt()\n", 1), []int{5}, nil)},
			want:  []string{"LEFT uses-removed-api " + managerPath + ":7:9 -> sample-app@main:" + repoPath + ":5"},
		},
		{
			name:  "use the parent removes",
			hits:  map[string][]Match{sampleApp + " main legacyToken": {{Path: repoPath, Line: 5}}},
			files: []ChangedFile{symbolFile(t, repoPath, repoBase, strings.Replace(repoBase, "${manager.legacyToken()}", "${manager.token()}", 1), []int{5}, []int{5})},
		},
		{
			name:  "file the parent removes",
			hits:  map[string][]Match{sampleApp + " main legacyToken": {{Path: repoPath, Line: 5}}},
			files: []ChangedFile{removed(t, repoPath)},
		},
		{
			name:  "use the parent adds",
			files: []ChangedFile{symbolFile(t, repoPath, repoBase, repoBase+"\nfun again() = manager.legacyToken()\n", []int{7, 8}, nil)},
			want:  []string{"LEFT uses-removed-api " + managerPath + ":7:9 -> sample-app@e9009e:" + repoPath + ":8"},
		},
		{
			name: "use in the root and the parent",
			hits: map[string][]Match{
				sampleApp + " main legacyToken":  {{Path: repoPath, Line: 5}, {Path: "app/src/main/A.kt", Line: 3}},
				rootApp + " develop legacyToken": {{Path: "b/B.kt", Line: 9}},
			},
			want: []string{"LEFT uses-removed-api " + managerPath + ":7:9 -> sample-app@main:app/src/main/A.kt:3 -> sample-app@main:" + repoPath +
				":5 -> root-app@develop:b/B.kt:9"},
			searched: []string{sampleApp + " main legacyToken", rootApp + " develop legacyToken", sampleApp + " main clearCache", rootApp + " develop clearCache"},
		},
		{
			name: "added symbol in use",
			hits: map[string][]Match{sampleApp + " main expiresAt": {{Path: repoPath, Line: 5}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &fakeSearch{hits: tt.hits}
			ancestors := []Ancestor{{Project: sampleApp, Head: "e9009e", Target: "main", Files: tt.files}}
			if tt.searched != nil {
				ancestors = append(ancestors, Ancestor{Project: rootApp, Head: "abc123", Target: "develop"})
			}
			checkSignals(t, RunChangeRequest([]*Rule{rule}, coreLibChange(t, s, ancestors...)), tt.want, nil)
			if tt.searched != nil && !reflect.DeepEqual(s.calls, tt.searched) {
				t.Errorf("searches %q, want %q", s.calls, tt.searched)
			}
		})
	}
}

func TestSymbolUsageWithoutAncestorsSearchesNothing(t *testing.T) {
	s := &fakeSearch{}
	checkSignals(t, RunChangeRequest([]*Rule{symbolRule(t, "uses-removed-api", removedAPI)}, coreLibChange(t, s)), nil, nil)
	if len(s.calls) != 0 {
		t.Errorf("searches %q", s.calls)
	}
}

func TestSymbolUsageSearchFailure(t *testing.T) {
	rule := symbolRule(t, "uses-removed-api", removedAPI)
	hits := map[string][]Match{sampleApp + " main clearCache": {{Path: "app/src/main/C.kt", Line: 4}}}
	parent := Ancestor{Project: sampleApp, Head: "e9009e", Target: "main"}
	// The failed symbol gives no candidate and is not verified; the
	// other symbol is still checked.
	cr := coreLibChange(t, &fakeSearch{hits: hits, failing: []string{"legacyToken"}}, parent)
	checkSignals(t, RunChangeRequest([]*Rule{rule}, cr),
		[]string{"LEFT uses-removed-api " + managerPath + ":9:9 -> sample-app@main:app/src/main/C.kt:4"},
		[]NotVerified{{Path: managerPath, Reason: ReasonSearchUnavailable, Detail: "uses-removed-api: legacyToken"}})
	// A failure in any ancestor leaves the symbol unverified.
	cr = coreLibChange(t, &fakeSearch{hits: hits, failing: []string{rootApp}}, parent, Ancestor{Project: rootApp, Target: "main"})
	checkSignals(t, RunChangeRequest([]*Rule{rule}, cr), nil, []NotVerified{
		{Path: managerPath, Reason: ReasonSearchUnavailable, Detail: "uses-removed-api: legacyToken"},
		{Path: managerPath, Reason: ReasonSearchUnavailable, Detail: "uses-removed-api: clearCache"},
	})
	// Without a search nothing can be verified.
	checkSignals(t, RunChangeRequest([]*Rule{rule}, coreLibChange(t, nil, parent)), nil, []NotVerified{
		{Path: managerPath, Reason: ReasonSearchUnavailable, Detail: "uses-removed-api: legacyToken"},
		{Path: managerPath, Reason: ReasonSearchUnavailable, Detail: "uses-removed-api: clearCache"},
	})
}

func TestSymbolUsageOwnDeclaration(t *testing.T) {
	// An ancestor in the same repository finds the removed declaration
	// itself at the target branch: it is not a use.
	hits := map[string][]Match{coreLib + " main legacyToken": {{Path: managerPath, Line: 7}, {Path: "src/main/java/Other.kt", Line: 2}}}
	cr := coreLibChange(t, &fakeSearch{hits: hits}, Ancestor{Project: coreLib, Head: "d00d", Target: "main"})
	checkSignals(t, RunChangeRequest([]*Rule{symbolRule(t, "uses-removed-api", removedAPI)}, cr),
		[]string{"LEFT uses-removed-api " + managerPath + ":7:9 -> core-lib@main:src/main/java/Other.kt:2"}, nil)
}

func TestSymbolUsageRequiredParameter(t *testing.T) {
	rule := symbolRule(t, "missing-required-argument", requiredArgument)
	callerBase := "package a\n\nfun sync(m: SessionManager) {\n    m.refresh(true)\n}\n"
	hits := map[string][]Match{sampleApp + " main refresh": {{Path: "app/Sync.kt", Line: 4}}}
	tests := []struct {
		name, head string
		files      []ChangedFile
		want       []string
	}{
		{name: "new required parameter", head: "fun refresh(force: Boolean, reason: String) {}",
			want: []string{"RIGHT missing-required-argument " + managerPath + ":11:9 -> sample-app@main:app/Sync.kt:4"}},
		{name: "new parameter with a default", head: "fun refresh(force: Boolean, reason: String = \"\") {}"},
		{name: "call the parent updates", head: "fun refresh(force: Boolean, reason: String) {}",
			files: []ChangedFile{symbolFile(t, "app/Sync.kt", callerBase, strings.Replace(callerBase, "refresh(true)", "refresh(true, \"sync\")", 1), []int{4}, []int{4})}},
		{name: "call the parent adds", head: "fun refresh(force: Boolean, reason: String) {}",
			files: []ChangedFile{symbolFile(t, "app/Sync.kt", callerBase, callerBase+"fun again(m: SessionManager) = m.refresh(false)\n", []int{6}, nil)},
			want:  []string{"RIGHT missing-required-argument " + managerPath + ":11:9 -> sample-app@main:app/Sync.kt:4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			head := strings.Replace(managerBase, "fun refresh(force: Boolean) {}", tt.head, 1)
			cr := ChangeRequest{
				Files:   []ChangedFile{symbolFile(t, managerPath, managerBase, head, []int{11}, []int{11})},
				Project: coreLib, Target: "main", Search: &fakeSearch{hits: hits},
				Ancestors: []Ancestor{{Project: sampleApp, Head: "e9009e", Target: "main", Files: tt.files}},
			}
			checkSignals(t, RunChangeRequest([]*Rule{rule}, cr), tt.want, nil)
		})
	}
}

func TestSymbolUsageIgnoresCommentsAndStrings(t *testing.T) {
	hits := map[string][]Match{sampleApp + " main legacyToken": {
		{Path: "app/A.kt", Line: 3, Text: "    // legacyToken() goes away in core-lib 3"},
		{Path: "app/B.kt", Line: 4, Text: `    val key = "legacyToken"`},
		{Path: "app/C.kt", Line: 5, Text: `    fun header() = "Bearer ${manager.legacyToken()}"`},
		{Path: "app/D.java", Line: 6, Text: "        /* legacyToken */ String t = manager.legacyToken();"},
		{Path: "app/E.java", Line: 7, Text: "        * Calls legacyToken() on the manager."},
		{Path: "app/F.kt", Line: 8},
		{Path: "docs/G.md", Line: 9, Text: "Do not call legacyToken()."},
	}}
	tables := func(path string) *lexer.Table {
		switch {
		case strings.HasSuffix(path, ".kt"):
			return packTable(t, "kotlin")
		case strings.HasSuffix(path, ".java"):
			return packTable(t, "java")
		}
		return nil
	}
	cr := coreLibChange(t, &fakeSearch{hits: hits}, Ancestor{Project: sampleApp, Head: "e9009e", Target: "main"})
	cr.TableOf = tables
	// A match without its text, or in a file without a lexer table, cannot
	// be checked and is kept. A block comment opened on an earlier line
	// is not seen from its matched line: E.java is kept, for the judgement.
	checkSignals(t, RunChangeRequest([]*Rule{symbolRule(t, "uses-removed-api", removedAPI)}, cr), []string{
		"LEFT uses-removed-api " + managerPath + ":7:9 -> sample-app@main:app/C.kt:5 -> sample-app@main:app/D.java:6" +
			" -> sample-app@main:app/E.java:7 -> sample-app@main:app/F.kt:8 -> sample-app@main:docs/G.md:9",
	}, nil)
	// Without tables, no match is checked.
	cr.TableOf = nil
	if got := signalsWithRefs(RunChangeRequest([]*Rule{symbolRule(t, "uses-removed-api", removedAPI)}, cr).Signals); len(got) != 1 ||
		strings.Count(got[0], " -> ") != 7 {
		t.Errorf("without tables: %q, want the 7 matches", got)
	}
}

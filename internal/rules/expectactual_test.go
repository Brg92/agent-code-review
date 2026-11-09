package rules

import (
	"strings"
	"testing"
)

const (
	expectActualMatch = `{"scope":"change-request","detector":"expect-actual","expectKeyword":"expect","actualKeyword":"actual",` +
		`"commonGroup":"commonMain","groups":["androidMain","iosMain","jvmMain"],"groupPattern":"src/{group}/"}`

	kmpShared    = "github:github.com/acr-plugin-sandbox/kmp-shared"
	platformPath = "shared/src/commonMain/kotlin/p/Platform.kt"
	platformSrc  = `package p

expect fun platformName(): String

expect class Clock {
    fun now(): Long
}

fun greeting() = "Hello from " + platformName()
`
	androidPath = "shared/src/androidMain/kotlin/p/Platform.android.kt"
	androidSrc  = "package p\n\nactual fun platformName(): String = \"Android\"\n\nactual class Clock {\n    actual fun now(): Long = 0\n}\n"
	iosPath     = "shared/src/iosMain/kotlin/p/Platform.ios.kt"
	iosSrc      = "package p\n\nactual fun platformName(): String = \"iOS\"\n\nactual class Clock {\n    actual fun now(): Long = 0\n}\n"
)

// kmpTree has android and ios source sets, and no jvm one.
var kmpTree = fakeTree{platformPath, androidPath, iosPath, "shared/build.gradle.kts"}

func expectRule(t testing.TB) *Rule {
	t.Helper()
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"kotlin-multiplatform","rules":[{"id":"kmp-expect-without-actual","title":"T",` +
		`"severity":"BLOCKER","needsJudgement":true,"description":"D","why":"W","fix":"F","languages":["kotlin"],"tags":[],"match":` +
		expectActualMatch + `}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return c.Rules[0]
}

func kmpChange(tree Tree, search Search, files ...ChangedFile) ChangeRequest {
	return ChangeRequest{Files: files, Tree: tree, Project: kmpShared, Head: "f9d862", Target: "main", Search: search}
}

func TestExpectActual(t *testing.T) {
	common := symbolFile(t, platformPath, "", platformSrc, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, nil)
	found := map[string][]Match{
		kmpShared + " main platformName": {{Path: androidPath, Line: 3}, {Path: iosPath, Line: 3}},
		kmpShared + " main Clock":        {{Path: androidPath, Line: 5}, {Path: iosPath, Line: 5}},
	}
	onlyAndroid := map[string][]Match{
		kmpShared + " main platformName": {{Path: androidPath, Line: 3}, {Path: platformPath, Line: 9}},
		kmpShared + " main Clock":        {{Path: androidPath, Line: 5}, {Path: iosPath, Line: 5}},
	}
	tests := []struct {
		name     string
		tree     Tree
		hits     map[string][]Match
		files    []ChangedFile
		want     []string
		searched int
	}{
		{name: "actuals in the change request", tree: kmpTree, files: []ChangedFile{
			symbolFile(t, androidPath, "", androidSrc, []int{3, 5}, nil), symbolFile(t, iosPath, "", iosSrc, []int{3, 5}, nil)}},
		{name: "actuals through the search", tree: kmpTree, hits: found, searched: 2},
		{name: "actual missing on one platform", tree: kmpTree, hits: onlyAndroid, searched: 2,
			want: []string{"RIGHT kmp-expect-without-actual " + platformPath + ":3:12"}},
		{name: "nothing found", tree: kmpTree, searched: 2, want: []string{
			"RIGHT kmp-expect-without-actual " + platformPath + ":3:12", "RIGHT kmp-expect-without-actual " + platformPath + ":5:14"}},
		{name: "platform source set absent from the tree", tree: fakeTree{platformPath, androidPath}, hits: onlyAndroid, searched: 2},
		{name: "no platform source set", tree: fakeTree{platformPath}},
		{name: "the change request removes the actual", tree: kmpTree, hits: found, searched: 2,
			files: []ChangedFile{symbolFile(t, iosPath, iosSrc, strings.Replace(iosSrc, "actual fun platformName(): String = \"iOS\"\n", "", 1), nil, []int{3})},
			want:  []string{"RIGHT kmp-expect-without-actual " + platformPath + ":3:12"}},
		// The changed file holds the android actuals at head, in another
		// package; its stale search matches do not count.
		{name: "actual in another package", tree: kmpTree, hits: found, searched: 2,
			files: []ChangedFile{symbolFile(t, androidPath, androidSrc, strings.Replace(androidSrc, "package p", "package q", 1), []int{1}, []int{1}),
				symbolFile(t, iosPath, "", iosSrc, []int{3}, nil)},
			want: []string{"RIGHT kmp-expect-without-actual " + platformPath + ":3:12", "RIGHT kmp-expect-without-actual " + platformPath + ":5:14"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &fakeSearch{hits: tt.hits}
			cr := kmpChange(tt.tree, s, append([]ChangedFile{common}, tt.files...)...)
			checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, cr), tt.want, nil)
			if len(s.calls) != tt.searched {
				t.Errorf("searches %q, want %d", s.calls, tt.searched)
			}
		})
	}
}

func TestExpectActualOnlyChangedExpects(t *testing.T) {
	// Line 3 is added; the expect class on line 5 is not touched.
	common := symbolFile(t, platformPath, platformSrc, platformSrc, []int{3}, nil)
	s := &fakeSearch{}
	checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, kmpChange(kmpTree, s, common)),
		[]string{"RIGHT kmp-expect-without-actual " + platformPath + ":3:12"}, nil)
	if len(s.calls) != 1 {
		t.Errorf("searches %q", s.calls)
	}
	// A file outside the common group is not checked, nor a removed one.
	other := symbolFile(t, "shared/src/jvmMain/kotlin/p/X.kt", "", platformSrc, []int{3, 5}, nil)
	gone := removed(t, platformPath)
	checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, kmpChange(kmpTree, &fakeSearch{}, other, gone)), nil, nil)
}

func TestExpectActualFailures(t *testing.T) {
	common := symbolFile(t, platformPath, "", platformSrc, []int{3, 5}, nil)
	hits := map[string][]Match{kmpShared + " main Clock": {{Path: androidPath, Line: 5}}}
	// A failed search leaves its expect unverified; the other is checked.
	checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, kmpChange(kmpTree, &fakeSearch{hits: hits, failing: []string{"platformName"}}, common)),
		[]string{"RIGHT kmp-expect-without-actual " + platformPath + ":5:14"},
		[]NotVerified{{Path: platformPath, Reason: ReasonSearchUnavailable, Detail: "kmp-expect-without-actual: platformName"}})
	// A tree that cannot be read leaves every expect unverified, even one
	// whose actual is missing in a platform that could be read.
	tree := failingTree{failing: []string{"shared/src/iosMain/"}, ok: kmpTree}
	checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, kmpChange(tree, &fakeSearch{}, common)), nil, []NotVerified{
		{Path: platformPath, Reason: ReasonTreeUnavailable, Detail: "kmp-expect-without-actual: platformName"},
		{Path: platformPath, Reason: ReasonTreeUnavailable, Detail: "kmp-expect-without-actual: Clock"},
	})
	// Without a search an actual outside the change request cannot be
	// found.
	checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, kmpChange(kmpTree, nil, common)), nil, []NotVerified{
		{Path: platformPath, Reason: ReasonSearchUnavailable, Detail: "kmp-expect-without-actual: platformName"},
		{Path: platformPath, Reason: ReasonSearchUnavailable, Detail: "kmp-expect-without-actual: Clock"},
	})
}

func TestExpectActualIntermediateGroups(t *testing.T) {
	c, err := LoadCatalog([]byte(`{"schema":1,"pack":"kotlin-multiplatform","rules":[{"id":"kmp-expect-without-actual","title":"T",` +
		`"severity":"BLOCKER","needsJudgement":true,"description":"D","why":"W","fix":"F","languages":["kotlin"],"tags":[],"match":` +
		strings.TrimSuffix(expectActualMatch, "}") + `,"intermediateGroups":{"iosMain":["iosArm64Main","iosX64Main","iosSimulatorArm64Main"]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	rule := c.Rules[0]
	const (
		arm64 = "shared/src/iosArm64Main/kotlin/p/Platform.arm64.kt"
		x64   = "shared/src/iosX64Main/kotlin/p/Platform.x64.kt"
	)
	common := symbolFile(t, platformPath, "", "package p\n\nexpect fun platformName(): String\n", []int{1, 2, 3}, nil)
	at := func(paths ...string) []Match {
		var out []Match
		for _, p := range paths {
			out = append(out, Match{Path: p, Line: 3})
		}
		return out
	}
	tests := []struct {
		name string
		tree fakeTree
		hits []Match
		want []string
	}{
		{"iosMain holds the actual", fakeTree{platformPath, androidPath, iosPath, arm64}, at(androidPath, iosPath), nil},
		{"iosMain without the actual", fakeTree{platformPath, androidPath, iosPath, arm64}, at(androidPath, arm64),
			[]string{"RIGHT kmp-expect-without-actual " + platformPath + ":3:12"}},
		{"every per-target set holds it", fakeTree{platformPath, androidPath, arm64, x64}, at(androidPath, arm64, x64), nil},
		{"one per-target set lacks it", fakeTree{platformPath, androidPath, arm64, x64}, at(androidPath, arm64),
			[]string{"RIGHT kmp-expect-without-actual " + platformPath + ":3:12"}},
		{"per-target sets absent from the tree", fakeTree{platformPath, androidPath}, at(androidPath), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &fakeSearch{hits: map[string][]Match{kmpShared + " main platformName": tt.hits}}
			checkSignals(t, RunChangeRequest([]*Rule{rule}, kmpChange(tt.tree, s, common)), tt.want, nil)
		})
	}
	// Without intermediate groups, per-target sets need no actual.
	s := &fakeSearch{hits: map[string][]Match{kmpShared + " main platformName": at(androidPath)}}
	checkSignals(t, RunChangeRequest([]*Rule{expectRule(t)}, kmpChange(fakeTree{platformPath, androidPath, arm64}, s, common)), nil, nil)
}

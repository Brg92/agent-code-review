package cascade_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/state"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

// fake is a Source over a map; it counts the requests it gets.
type fake struct {
	items map[string]cascade.Item
	calls map[string]int
	err   error
}

func newFake() *fake { return &fake{items: map[string]cascade.Item{}, calls: map[string]int{}} }

func id(k core.Key) string { return strings.ToLower(k.String()) }

func (f *fake) Get(_ context.Context, k core.Key) (cascade.Item, error) {
	f.calls[id(k)]++
	if f.err != nil {
		return cascade.Item{}, f.err
	}
	if it, ok := f.items[id(k)]; ok {
		return it, nil
	}
	return cascade.Item{Kind: cascade.KindNotFound}, nil
}

// cr adds a change request whose Related Changes section has the given lines.
func (f *fake) cr(key string, lines ...string) *fake {
	desc := "Does a thing."
	if len(lines) > 0 {
		desc += "\n\n## Related Changes\n\n" + strings.Join(lines, "\n")
	}
	f.items[strings.ToLower(key)] = cascade.Item{Kind: cascade.KindChangeRequest, Title: "Title of " + key, Description: desc, Head: sha}
	return f
}

func (f *fake) issue(key string) *fake {
	f.items[strings.ToLower(key)] = cascade.Item{Kind: cascade.KindIssue, Title: "An issue"}
	return f
}

func k(t *testing.T, s string) core.Key {
	t.Helper()
	key, err := core.ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func gh(n int) string  { return fmt.Sprintf("github:github.com/acme/app#%d", n) }
func lib(n int) string { return fmt.Sprintf("github:github.com/acme/lib#%d", n) }

func resolve(t *testing.T, f *fake, root string, limits core.Limits) cascade.Result {
	t.Helper()
	reg := refs.Compose(github.Registry{}, gitlab.Registry{})
	if strings.HasPrefix(root, "gitlab:") {
		reg = refs.Compose(gitlab.Registry{}, github.Registry{})
	}
	res, err := cascade.Resolve(context.Background(), f, reg, k(t, root), limits)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the walk returns must be a state the loader accepts.
	if _, err := state.New("20261102T101500Z-0a1b2c3d", k(t, root), limits, res.Entries, time.Now()); err != nil {
		t.Fatalf("the entries are not a valid state: %v", err)
	}
	return res
}

// short names a key without its forge and host: acme/app#7 -> app#7.
func short(k core.Key) string {
	p := k.Project[strings.LastIndex(k.Project, "/")+1:]
	return fmt.Sprintf("%s#%d", p, k.Number)
}

// walk lists the entries as "<depth> <key> <parent> <status or reason>".
func walk(res cascade.Result) []string {
	var out []string
	for _, e := range res.Entries {
		parent := "-"
		if e.Parent != nil {
			parent = short(*e.Parent)
		}
		what := string(e.Status)
		if e.Status == core.Skipped {
			what = string(e.SkipReason)
		}
		out = append(out, fmt.Sprintf("%d %s <%s %s", e.Depth, short(e.CR), parent, what))
	}
	return out
}

func order(res cascade.Result) []string {
	var out []string
	for _, k := range res.Order {
		out = append(out, short(k))
	}
	return out
}

func check(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", name, got, want)
	}
}

func TestResolveDiscoveryAndReviewOrder(t *testing.T) {
	f := newFake().
		cr(gh(1), "- [ ] #2", "- [ ] #3").
		cr(gh(2), "- [ ] acme/lib#10").
		cr(gh(3)).
		cr(lib(10), "- [ ] acme/app#4").
		cr(gh(4))
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{
		"0 app#1 <- pending",
		"1 app#2 <app#1 pending",
		"2 lib#10 <app#2 pending",
		"3 app#4 <lib#10 pending",
		"1 app#3 <app#1 pending",
	})
	check(t, "review order", order(res), []string{"app#4", "lib#10", "app#2", "app#3", "app#1"})
	if res.Incomplete() {
		t.Errorf("Incomplete() with causes %+v", res.Causes())
	}
	for _, e := range res.Entries {
		if e.HeadSHA != sha || !strings.HasPrefix(e.Title, "Title of ") {
			t.Errorf("%s: title %q head %q", e.CR, e.Title, e.HeadSHA)
		}
	}
}

func TestResolveOnlyRoot(t *testing.T) {
	res := resolve(t, newFake().cr(gh(1)), gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{"0 app#1 <- pending"})
	check(t, "review order", order(res), []string{"app#1"})
}

func TestResolveSkips(t *testing.T) {
	f := newFake().
		cr(gh(1),
			"- [x] acme/lib#1",
			"- [ ] #2",
			"- [ ] #3",
			"- [ ] #4",
			"- [ ] #1",
			"- [ ] https://gitlab.com/acme/app/-/merge_requests/9",
			"- [ ] https://github.example.com/acme/app/pull/9",
			"- [ ] acme/lib#9999").
		cr(gh(2), "- [ ] #1", "- [ ] acme/lib#5").
		issue(gh(3)).
		cr(gh(4), "- [ ] #2", "- [x] #2").
		cr(lib(5))
	f.items[strings.ToLower(lib(1))] = cascade.Item{Kind: cascade.KindChangeRequest, Description: "never fetched", Head: sha}
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{
		"0 app#1 <- pending",
		"1 lib#1 <app#1 already-done",
		"1 app#2 <app#1 pending",
		"2 app#1 <app#2 cycle",
		"2 lib#5 <app#2 pending",
		"1 app#3 <app#1 not-a-change-request",
		"1 app#4 <app#1 pending",
		"2 app#2 <app#4 cycle",
		"2 app#2 <app#4 already-done",
		"1 app#1 <app#1 self-reference",
		"1 app#9 <app#1 other-platform",
		"1 app#9 <app#1 other-platform",
		"1 lib#9999 <app#1 not-found",
	})
	check(t, "review order", order(res), []string{"lib#5", "app#2", "app#4", "app#1"})
	for _, key := range []string{lib(1), "gitlab:gitlab.com/acme/app#9", "github:github.example.com/acme/app#9"} {
		if f.calls[key] != 0 {
			t.Errorf("%s was fetched %d times", key, f.calls[key])
		}
	}
	var causes []string
	for _, c := range res.Causes() {
		causes = append(causes, c.Reason)
	}
	check(t, "causes", causes, []string{"other-platform", "other-platform", "not-found"})
}

func TestResolveTickedSkipsTheSubtreeWithoutFetching(t *testing.T) {
	f := newFake().cr(gh(1), "- [x] #2").cr(gh(2), "- [ ] #3").cr(gh(3))
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{"0 app#1 <- pending", "1 app#2 <app#1 already-done"})
	if f.calls[gh(2)] != 0 || f.calls[gh(3)] != 0 {
		t.Errorf("calls = %v, want none for the ticked subtree", f.calls)
	}
	if res.Incomplete() {
		t.Error("an already-done entry makes the cascade incomplete")
	}
}

func TestResolveCycleAgainstASiblingSubtree(t *testing.T) {
	f := newFake().cr(gh(1), "- [ ] #2", "- [ ] #3").cr(gh(2), "- [ ] #4").cr(gh(3), "- [ ] #4").cr(gh(4))
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{
		"0 app#1 <- pending", "1 app#2 <app#1 pending", "2 app#4 <app#2 pending", "1 app#3 <app#1 pending", "2 app#4 <app#3 cycle",
	})
	check(t, "review order", order(res), []string{"app#4", "app#2", "app#3", "app#1"})
	if res.Incomplete() {
		t.Error("a cycle makes the cascade incomplete")
	}
}

func TestResolveCaseOfProjectPath(t *testing.T) {
	f := newFake().cr(gh(1), "- [ ] ACME/Lib#5", "- [ ] acme/lib#5").cr(lib(5))
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{"0 app#1 <- pending", "1 Lib#5 <app#1 pending", "1 lib#5 <app#1 cycle"})
}

func TestResolveSeveralOnALine(t *testing.T) {
	f := newFake().cr(gh(1), "- [ ] #2 and #3").cr(gh(2)).cr(gh(3))
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{"0 app#1 <- pending", "1 app#2 <app#1 pending", "1 app#3 <app#1 pending"})
}

func TestResolveRepeatedUnreachableIsFetchedOnce(t *testing.T) {
	f := newFake().cr(gh(1), "- [ ] #2", "- [ ] #9").cr(gh(2), "- [ ] #9")
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{
		"0 app#1 <- pending", "1 app#2 <app#1 pending", "2 app#9 <app#2 not-found", "1 app#9 <app#1 not-found",
	})
	if f.calls[gh(9)] != 1 {
		t.Errorf("fetched %d times, want 1", f.calls[gh(9)])
	}
}

func TestResolveLines(t *testing.T) {
	f := newFake().cr(gh(1),
		"- [ ] #2",
		"- #3",
		"https://github.com/acme/lib/pull/6",
		"- [ ] nothing to follow",
		"- [x] done, but no link",
		"free prose").cr(gh(2), "- [ ]")
	res := resolve(t, f, gh(1), core.DefaultLimits())
	check(t, "entries", walk(res), []string{"0 app#1 <- pending", "1 app#2 <app#1 pending"})
	var notFollowed, unparsed []string
	for _, l := range res.NotFollowed {
		notFollowed = append(notFollowed, fmt.Sprintf("%s:%d %s", short(l.CR), l.Line, l.Text))
	}
	for _, l := range res.Unparsed {
		unparsed = append(unparsed, fmt.Sprintf("%s:%d %s", short(l.CR), l.Line, l.Text))
	}
	check(t, "not followed", notFollowed, []string{"app#1:6 - #3", "app#1:7 https://github.com/acme/lib/pull/6"})
	check(t, "unparsed", unparsed, []string{"app#2:5 - [ ]", "app#1:8 - [ ] nothing to follow", "app#1:9 - [x] done, but no link"})
	if f.calls[gh(3)] != 0 || f.calls[lib(6)] != 0 {
		t.Errorf("lines without a checkbox were fetched: %v", f.calls)
	}
	if !res.Incomplete() {
		t.Fatal("unparsed lines must make the cascade incomplete")
	}
	var causes []string
	for _, c := range res.Causes() {
		causes = append(causes, fmt.Sprintf("%s %s:%d", c.Reason, short(c.Parent), c.Line))
	}
	check(t, "causes", causes, []string{"unparsed app#2:5", "unparsed app#1:8", "unparsed app#1:9"})
}

func TestResolveNotFollowedAloneIsComplete(t *testing.T) {
	res := resolve(t, newFake().cr(gh(1), "- #2"), gh(1), core.DefaultLimits())
	if res.Incomplete() || len(res.NotFollowed) != 1 {
		t.Errorf("Incomplete() = %v, NotFollowed = %+v", res.Incomplete(), res.NotFollowed)
	}
}

func TestResolveMaxDepth(t *testing.T) {
	f := newFake().
		cr(gh(1), "- [ ] #2").
		cr(gh(2), "- [ ] #3", "- [ ] #1", "- [ ] #8", "- [ ] #9").
		cr(gh(3), "- [ ] #4").
		cr(gh(4)).
		issue(gh(8))
	res := resolve(t, f, gh(1), core.Limits{MaxDepth: 1, MaxChangeRequests: 10})
	check(t, "entries", walk(res), []string{
		"0 app#1 <- pending",
		"1 app#2 <app#1 pending",
		"2 app#3 <app#2 over-depth-limit",
		"2 app#1 <app#2 cycle",
		"2 app#8 <app#2 not-a-change-request",
		"2 app#9 <app#2 not-found",
	})
	check(t, "review order", order(res), []string{"app#2", "app#1"})
	if f.calls[gh(4)] != 0 {
		t.Error("an over-depth change request was expanded")
	}
	if !res.Incomplete() {
		t.Error("over-depth-limit must make the cascade incomplete")
	}
	res = resolve(t, f, gh(1), core.Limits{MaxDepth: 0, MaxChangeRequests: 10})
	check(t, "depth 0", walk(res), []string{"0 app#1 <- pending", "1 app#2 <app#1 over-depth-limit"})
}

func TestResolveMaxChangeRequests(t *testing.T) {
	f := newFake().
		cr(gh(1), "- [ ] #2", "- [ ] #3").
		cr(gh(2), "- [ ] #4").
		cr(gh(3)).
		cr(gh(4)).
		issue(gh(5))
	// Discovery order is depth first: the root, #2 and #4 come before #3.
	res := resolve(t, f, gh(1), core.Limits{MaxDepth: 3, MaxChangeRequests: 3})
	check(t, "entries", walk(res), []string{
		"0 app#1 <- pending", "1 app#2 <app#1 pending", "2 app#4 <app#2 pending", "1 app#3 <app#1 over-count-limit",
	})
	check(t, "review order", order(res), []string{"app#4", "app#2", "app#1"})
	if !res.Incomplete() {
		t.Error("over-count-limit must make the cascade incomplete")
	}
	res = resolve(t, f, gh(1), core.Limits{MaxDepth: 3, MaxChangeRequests: 1})
	check(t, "one", walk(res), []string{"0 app#1 <- pending", "1 app#2 <app#1 over-count-limit", "1 app#3 <app#1 over-count-limit"})
	// An issue is not a change request, whatever the count.
	f.cr(gh(1), "- [ ] #5")
	res = resolve(t, f, gh(1), core.Limits{MaxDepth: 3, MaxChangeRequests: 1})
	check(t, "issue at the limit", walk(res), []string{"0 app#1 <- pending", "1 app#5 <app#1 not-a-change-request"})
}

func TestResolveDepthIsCheckedBeforeCount(t *testing.T) {
	f := newFake().cr(gh(1), "- [ ] #2").cr(gh(2), "- [ ] #3").cr(gh(3))
	res := resolve(t, f, gh(1), core.Limits{MaxDepth: 1, MaxChangeRequests: 2})
	check(t, "entries", walk(res), []string{"0 app#1 <- pending", "1 app#2 <app#1 pending", "2 app#3 <app#2 over-depth-limit"})
}

func TestResolveGitLab(t *testing.T) {
	root := "gitlab:gitlab.com/group/sub/app#7"
	f := newFake().
		cr(root, "- [ ] lib!2", "- [ ] https://gitlab.com/group/other/-/merge_requests/3", "- [ ] https://github.com/acme/app/pull/1", "- [ ] #5").
		cr("gitlab:gitlab.com/group/sub/lib#2").
		cr("gitlab:gitlab.com/group/other#3")
	res := resolve(t, f, root, core.DefaultLimits())
	check(t, "entries", walk(res), []string{
		"0 app#7 <- pending", "1 lib#2 <app#7 pending", "1 other#3 <app#7 pending", "1 app#1 <app#7 other-platform",
	})
	if len(res.Unparsed) != 1 || res.Unparsed[0].Text != "- [ ] #5" {
		t.Errorf("Unparsed = %+v: #N is an issue on GitLab", res.Unparsed)
	}
}

func TestResolveErrors(t *testing.T) {
	t.Run("root not found", func(t *testing.T) {
		_, err := cascade.Resolve(context.Background(), newFake(), github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		var re *cascade.RootError
		if !errors.As(err, &re) || re.Kind != cascade.KindNotFound {
			t.Errorf("err = %v, want a *RootError for a missing change request", err)
		}
	})
	t.Run("root is an issue", func(t *testing.T) {
		_, err := cascade.Resolve(context.Background(), newFake().issue(gh(1)), github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		var re *cascade.RootError
		if !errors.As(err, &re) || re.Kind != cascade.KindIssue {
			t.Errorf("err = %v, want a *RootError for an issue", err)
		}
	})
	t.Run("source fails", func(t *testing.T) {
		boom := errors.New("boom")
		f := newFake().cr(gh(1), "- [ ] #2")
		_, err := cascade.Resolve(context.Background(), f, github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		f.err = boom
		_, err = cascade.Resolve(context.Background(), f, github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), gh(1)) {
			t.Errorf("err = %v, want boom naming the key", err)
		}
	})
	t.Run("source fails below the root", func(t *testing.T) {
		boom := errors.New("boom")
		f := &failOn{fake: newFake().cr(gh(1), "- [ ] #2").cr(gh(2)), key: gh(2), err: boom}
		_, err := cascade.Resolve(context.Background(), f, github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		if !errors.Is(err, boom) || !strings.Contains(err.Error(), gh(2)) {
			t.Errorf("err = %v, want boom naming %s", err, gh(2))
		}
	})
	t.Run("bad limits", func(t *testing.T) {
		_, err := cascade.Resolve(context.Background(), newFake().cr(gh(1)), github.Registry{}, k(t, gh(1)), core.Limits{MaxDepth: 1})
		if err == nil || !strings.Contains(err.Error(), "maxChangeRequests") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("registry of another platform", func(t *testing.T) {
		_, err := cascade.Resolve(context.Background(), newFake().cr(gh(1)), gitlab.Registry{}, k(t, gh(1)), core.DefaultLimits())
		if err == nil {
			t.Error("a gitlab registry was accepted for a github root")
		}
	})
	t.Run("bad head", func(t *testing.T) {
		f := newFake().cr(gh(1))
		it := f.items[strings.ToLower(gh(1))]
		it.Head = "abc"
		f.items[strings.ToLower(gh(1))] = it
		_, err := cascade.Resolve(context.Background(), f, github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		if err == nil || !strings.Contains(err.Error(), "headSha") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := cascade.Resolve(ctx, newFake().cr(gh(1)), github.Registry{}, k(t, gh(1)), core.DefaultLimits())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	})
}

// failOn fails the request for one key.
type failOn struct {
	*fake
	key string
	err error
}

func (f *failOn) Get(ctx context.Context, k core.Key) (cascade.Item, error) {
	if id(k) == strings.ToLower(f.key) {
		return cascade.Item{}, f.err
	}
	return f.fake.Get(ctx, k)
}

package cascade

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/forges/gitlab"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/testutil/scenario"
)

func causeList(causes []Cause) []string {
	var out []string
	for _, c := range causes {
		s := c.Reason + " " + short(c.CR)
		if c.Line > 0 {
			s = fmt.Sprintf("%s %s line %d", c.Reason, short(c.Parent), c.Line)
		}
		out = append(out, s)
	}
	return out
}

// synthetic is a cascade of a root and one reviewed entry, plus extra
// entries and unparsed lines.
func synthetic(t *testing.T, extra []core.Entry, unparsed ...Line) Result {
	t.Helper()
	root, dep := k(t, sandbox+"app#1"), k(t, sandbox+"lib#1")
	entries := append([]core.Entry{
		{CR: root, Status: core.Finalized},
		{CR: dep, Depth: 1, Parent: &root, Status: core.Finalized},
	}, extra...)
	return Result{Entries: entries, Order: []core.Key{dep, root}, Unparsed: unparsed}
}

func skipped(t *testing.T, cr string, reason core.SkipReason) core.Entry {
	t.Helper()
	root := k(t, sandbox+"app#1")
	return core.Entry{CR: k(t, sandbox+cr), Depth: 1, Parent: &root, Status: core.Skipped, SkipReason: reason}
}

func TestCascadeVerdict(t *testing.T) {
	ok := func(t *testing.T) map[core.Key]core.Verdict {
		return map[core.Key]core.Verdict{k(t, sandbox+"app#1"): core.OK, k(t, sandbox+"lib#1"): core.OK}
	}
	tests := []struct {
		name     string
		res      func(t *testing.T) Result
		verdicts func(t *testing.T) map[core.Key]core.Verdict
		want     core.Verdict
		causes   []string
	}{
		{"all OK", func(t *testing.T) Result { return synthetic(t, nil) }, ok, core.OK, nil},
		{"over the depth limit", func(t *testing.T) Result {
			return synthetic(t, []core.Entry{skipped(t, "deep#1", core.OverDepthLimit)})
		}, ok,
			core.Incomplete, []string{"over-depth-limit deep#1"}},
		{"over the count limit", func(t *testing.T) Result {
			return synthetic(t, []core.Entry{skipped(t, "many#1", core.OverCountLimit)})
		}, ok,
			core.Incomplete, []string{"over-count-limit many#1"}},
		{"another platform", func(t *testing.T) Result { return synthetic(t, []core.Entry{skipped(t, "lab#1", core.OtherPlatform)}) }, ok,
			core.Incomplete, []string{"other-platform lab#1"}},
		{"not found", func(t *testing.T) Result { return synthetic(t, []core.Entry{skipped(t, "gone#1", core.NotFound)}) }, ok,
			core.Incomplete, []string{"not-found gone#1"}},
		{"unparsed related line", func(t *testing.T) Result {
			return synthetic(t, nil, Line{CR: k(t, sandbox+"app#1"), Line: 4, Text: "- [ ] the lib change"})
		}, ok, core.Incomplete, []string{"unparsed app#1 line 4"}},
		{"skips that leave nothing unreviewed", func(t *testing.T) Result {
			return synthetic(t, []core.Entry{
				skipped(t, "done#1", core.AlreadyDone), skipped(t, "app#1", core.Cycle),
				skipped(t, "app#1", core.SelfReference), skipped(t, "issue#1", core.NotAChangeRequest),
			})
		}, ok, core.OK, nil},
		{"blocked wins over incomplete", func(t *testing.T) Result { return synthetic(t, []core.Entry{skipped(t, "gone#1", core.NotFound)}) },
			func(t *testing.T) map[core.Key]core.Verdict {
				return map[core.Key]core.Verdict{k(t, sandbox+"app#1"): core.OK, k(t, sandbox+"lib#1"): core.Blocked}
			}, core.Blocked, []string{"blocked lib#1", "not-found gone#1"}},
		{"blocked root", func(t *testing.T) Result { return synthetic(t, nil) },
			func(t *testing.T) map[core.Key]core.Verdict {
				return map[core.Key]core.Verdict{k(t, sandbox+"app#1"): core.Blocked, k(t, sandbox+"lib#1"): core.OK}
			}, core.Blocked, []string{"blocked app#1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, causes, err := tt.res(t).Verdict(tt.verdicts(t))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || !reflect.DeepEqual(causeList(causes), tt.causes) {
				t.Errorf("verdict %s %q, want %s %q", got, causeList(causes), tt.want, tt.causes)
			}
		})
	}
}

func TestCascadeVerdictNeedsEveryReviewedVerdict(t *testing.T) {
	res := synthetic(t, nil)
	// Keys compare without the case of the project, as platforms do.
	if _, _, err := res.Verdict(map[core.Key]core.Verdict{k(t, sandbox+"App#1"): core.OK, k(t, sandbox+"lib#1"): core.OK}); err != nil {
		t.Errorf("verdict of a key spelled with another case: %v", err)
	}
	_, _, err := res.Verdict(map[core.Key]core.Verdict{k(t, sandbox+"app#1"): core.OK})
	if err == nil || !strings.Contains(err.Error(), "lib#1") {
		t.Errorf("missing verdict: %v, want an error naming lib#1", err)
	}
	_, _, err = res.Verdict(map[core.Key]core.Verdict{k(t, sandbox+"app#1"): core.OK, k(t, sandbox+"lib#1"): core.Incomplete})
	if err == nil {
		t.Error("INCOMPLETE is accepted as the verdict of one change request")
	}
}

// TestCascadeVerdictS2 finalizes every change request of the S2 cascade
// with its scenario judgement: core-lib S2 and S1 are BLOCKED, so the
// cascade is BLOCKED, and it still reports what made it incomplete.
func TestCascadeVerdictS2(t *testing.T) {
	res, err := Resolve(context.Background(), loadRecorded(t, "S2", "S4"), refs.Compose(github.Registry{}, gitlab.Registry{}), k(t, sandbox+"sample-app#7"), core.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	verdicts := map[core.Key]core.Verdict{}
	var each []string
	for _, cr := range res.Order {
		repo := cr.Project[strings.LastIndex(cr.Project, "/")+1:]
		fin, err := core.Finalize(scenario.Review(t, repo, cr.Number, "S2", "S4"), scenario.Judgement(t, "S2", repo, cr.Number))
		if err != nil {
			t.Fatalf("%s: %v", cr, err)
		}
		verdicts[cr] = fin.Verdict
		each = append(each, short(cr)+" "+string(fin.Verdict))
	}
	check(t, "verdicts", each, []string{"feature-lib#1 OK", "kmp-shared#2 OK", "core-lib#3 BLOCKED", "sample-app#2 BLOCKED", "sample-app#7 OK"})
	got, causes, err := res.Verdict(verdicts)
	if err != nil {
		t.Fatal(err)
	}
	if got != core.Blocked {
		t.Errorf("cascade verdict %s, want BLOCKED", got)
	}
	check(t, "causes", causeList(causes), []string{"blocked core-lib#3", "blocked sample-app#2", "other-platform sample-app#1", "not-found core-lib#9999"})
}

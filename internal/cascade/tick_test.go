package cascade

import (
	"errors"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/forges/github"
	"github.com/Brg92/agent-code-review/internal/refs"
)

func entries(t *testing.T, desc string) []refs.RelatedEntry {
	t.Helper()
	return refs.Related(desc, github.Registry{}, k(t, gh(1)))
}

func TestTickChangesExactlyOneByte(t *testing.T) {
	const body = "Intro\n\n## Related Changes\n\n- [ ] #2\n- [x] #3\n* [ ] acme/lib#4\n- #5\n\nTail [ ] text\n"
	for _, eol := range []string{"\n", "\r\n"} {
		desc := strings.ReplaceAll(body, "\n", eol)
		list := entries(t, desc)
		var ticked int
		for _, e := range list {
			if e.Box != refs.Open {
				continue
			}
			got, err := Tick(desc, e)
			if err != nil {
				t.Fatalf("Tick(line %d): %v", e.Line, err)
			}
			want := desc[:e.BoxOffset] + "x" + desc[e.BoxOffset+1:]
			if got != want {
				t.Errorf("Tick(line %d) =\n%q\nwant\n%q", e.Line, got, want)
			}
			if len(got) != len(desc) {
				t.Errorf("length changed from %d to %d", len(desc), len(got))
			}
			ticked++
		}
		if ticked != 2 {
			t.Errorf("ticked %d entries, want 2", ticked)
		}
	}
}

func TestTickLeavesEveryOtherByte(t *testing.T) {
	desc := "## Related PRs\r\n\r\n- [ ] #2\r\n- [ ] #3\r\n\r\nUnicode: éè \U0001F600\r\n"
	e := entries(t, desc)[1]
	got, err := Tick(desc, e)
	if err != nil {
		t.Fatal(err)
	}
	if want := "## Related PRs\r\n\r\n- [ ] #2\r\n- [x] #3\r\n\r\nUnicode: éè \U0001F600\r\n"; got != want {
		t.Errorf("Tick() = %q, want %q", got, want)
	}
}

func TestTickRefuses(t *testing.T) {
	const desc = "## Related Changes\n- [ ] #2\n- [x] #3\n- #4\n"
	list := entries(t, desc)
	open, done, none := list[0], list[1], list[2]
	tests := []struct {
		name   string
		desc   string
		entry  refs.RelatedEntry
		reason TickReason
	}{
		{"entry already ticked", desc, done, AlreadyTicked},
		{"description ticked since", strings.Replace(desc, "- [ ] #2", "- [x] #2", 1), open, AlreadyTicked},
		{"entry without a checkbox", desc, none, NoCheckbox},
		{"line moved down", "Added line\n" + desc, open, Moved},
		{"line moved up", desc[len("## Related Changes\n"):], open, Moved},
		{"checkbox text changed", strings.Replace(desc, "[ ]", "[?]", 1), open, Moved},
		{"offset past the end", desc[:open.BoxOffset], open, Moved},
		{"empty description", "", open, Moved},
		{"negative offset", desc, refs.RelatedEntry{Box: refs.Open, BoxOffset: -1}, Moved},
		{"offset at the start", desc, refs.RelatedEntry{Box: refs.Open, BoxOffset: 0}, Moved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Tick(tt.desc, tt.entry)
			var te *TickError
			if !errors.As(err, &te) || te.Reason != tt.reason {
				t.Fatalf("Tick() = %q, %v; want a *TickError with reason %v", got, err, tt.reason)
			}
			if got != "" {
				t.Errorf("Tick() returned %q with an error", got)
			}
			t.Log(err)
		})
	}
}

func TestTickReasonStrings(t *testing.T) {
	for r, want := range map[TickReason]string{AlreadyTicked: "already ticked", NoCheckbox: "no checkbox", Moved: "moved"} {
		if !strings.Contains((&TickError{Reason: r, Line: 3}).Error(), want) {
			t.Errorf("reason %d: %q does not mention %q", r, (&TickError{Reason: r, Line: 3}).Error(), want)
		}
	}
}

// FuzzTick checks that a tick never changes more than one byte, whatever the
// description and the entry.
func FuzzTick(f *testing.F) {
	for _, s := range []string{
		"## Related Changes\n- [ ] #2\n",
		"## Related PRs\r\n- [ ] #2\r\n- [x] #3\r\n",
		"- [ ] #1\n[ ]\n[ ] [ ]",
		"## Related Changes\né - [ ] #2\n```\n- [ ] #3\n```\n- [ ] #4",
		"",
	} {
		f.Add(s, 0)
		f.Add(s, 22)
	}
	f.Fuzz(func(t *testing.T, desc string, offset int) {
		check := func(e refs.RelatedEntry) {
			got, err := Tick(desc, e)
			if err != nil {
				if got != "" {
					t.Fatalf("result %q with error %v", got, err)
				}
				return
			}
			if len(got) != len(desc) {
				t.Fatalf("length %d became %d", len(desc), len(got))
			}
			var diff []int
			for i := range len(desc) {
				if desc[i] != got[i] {
					diff = append(diff, i)
				}
			}
			if len(diff) != 1 || diff[0] != e.BoxOffset || desc[diff[0]] != ' ' || got[diff[0]] != 'x' {
				t.Fatalf("Tick(%q, %+v) = %q: differences at %v", desc, e, got, diff)
			}
		}
		for _, e := range refs.Related(desc, github.Registry{}, k(t, gh(1))) {
			check(e)
		}
		check(refs.RelatedEntry{Box: refs.Open, BoxOffset: offset})
	})
}

package refs

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

// numbered is a Registry for tests: every #N on a line is change request N
// of the project of the change request that owns the description.
type numbered struct{}

var hashNumber = regexp.MustCompile(`#([0-9]+)`)

func (numbered) Forge() core.Forge { return core.GitHub }

func (numbered) Parse(line string, from core.Key) []Ref {
	var out []Ref
	for _, m := range hashNumber.FindAllStringSubmatchIndex(line, -1) {
		n, _ := strconv.Atoi(line[m[2]:m[3]])
		k := from
		k.Number = n
		out = append(out, Ref{Key: k, Pos: m[0]})
	}
	return out
}

var owner = core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 7}

// summary reduces entries to "<line> <kind> <numbers>" for table tests.
func summary(entries []RelatedEntry) []string {
	var out []string
	for _, e := range entries {
		var nums []string
		for _, r := range e.Refs {
			nums = append(nums, strconv.Itoa(r.Number))
		}
		out = append(out, strconv.Itoa(e.Line)+" "+e.Kind().String()+" "+strings.Join(nums, ","))
	}
	return out
}

func TestRelatedSectionHeadings(t *testing.T) {
	headings := []string{
		"## Related Changes", "# Related Changes", "### Related Changes",
		"## related changes", "## RELATED CHANGES", "## Related Changes:", "## Related Changes ##",
		"## Related Merge Requests", "## Related MRs", "## Related Pull Requests", "## Related PRs",
		"   ## Related PRs", "##Related PRs",
	}
	for _, h := range headings {
		got := summary(Related("Intro\n\n"+h+"\n\n- [ ] #1\n", numbered{}, owner))
		if want := []string{"5 followed 1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("heading %q: %q, want %q", h, got, want)
		}
	}
}

func TestRelatedNotASection(t *testing.T) {
	for _, h := range []string{"#### Related Changes", "Related Changes", "## Related", "## Related Changes and more", "## Unrelated Changes", "    ## Related Changes", "## Related Changes list"} {
		if got := Related(h+"\n\n- [ ] #1\n", numbered{}, owner); got != nil {
			t.Errorf("heading %q gave %q", h, summary(got))
		}
	}
}

func TestRelatedSectionEnds(t *testing.T) {
	tests := []struct {
		name, text string
		want       []string
	}{
		{"next h2", "## Related PRs\n- [ ] #1\n## Notes\n- [ ] #2\n", []string{"2 followed 1"}},
		{"next h1", "## Related PRs\n- [ ] #1\n# Notes\n- [ ] #2\n", []string{"2 followed 1"}},
		{"next h3", "## Related PRs\n- [ ] #1\n### Notes\n- [ ] #2\n", []string{"2 followed 1"}},
		{"empty heading ends", "## Related PRs\n- [ ] #1\n##\n- [ ] #2\n", []string{"2 followed 1"}},
		{"h4 does not end", "## Related PRs\n- [ ] #1\n#### Sub\n- [ ] #2\n", []string{"2 followed 1", "4 followed 2"}},
		{"hash number does not end", "## Related PRs\n#1 is first\n- [ ] #2\n", []string{"2 notfollowed 1", "3 followed 2"}},
		{"to the end", "## Related PRs\n- [ ] #1\n\ntext\n- [ ] #2", []string{"2 followed 1", "5 followed 2"}},
		{"two sections", "## Related PRs\n- [ ] #1\n## Notes\n## Related Changes\n- [ ] #2\n", []string{"2 followed 1", "5 followed 2"}},
		{"nothing before", "- [ ] #1\n## Notes\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summary(Related(tt.text, numbered{}, owner)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelatedCodeFences(t *testing.T) {
	tests := []struct {
		name, text string
		want       []string
	}{
		{"entry inside a fence", "## Related PRs\n```\n- [ ] #1\n```\n- [ ] #2\n", []string{"5 followed 2"}},
		{"tilde fence", "## Related PRs\n~~~md\n- [ ] #1\n~~~\n- [ ] #2\n", []string{"5 followed 2"}},
		{"heading inside a fence does not end", "## Related PRs\n```\n# Notes\n```\n- [ ] #2\n", []string{"5 followed 2"}},
		{"section heading inside a fence", "```\n## Related PRs\n- [ ] #1\n```\n", nil},
		{"longer fence needs a longer close", "## Related PRs\n````\n```\n- [ ] #1\n```\n````\n- [ ] #2\n", []string{"7 followed 2"}},
		{"mismatched close stays open", "## Related PRs\n```\n- [ ] #1\n~~~\n- [ ] #2\n```\n- [ ] #3\n", []string{"7 followed 3"}},
		{"unclosed fence ignores the rest", "## Related PRs\n- [ ] #1\n```\n- [ ] #2\n", []string{"2 followed 1"}},
		{"indented fence", "## Related PRs\n  ```\n- [ ] #1\n  ```\n- [ ] #2\n", []string{"5 followed 2"}},
		{"fence opened before the section", "```\n## Related PRs\n```\n- [ ] #1\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summary(Related(tt.text, numbered{}, owner)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelatedLineKinds(t *testing.T) {
	text := strings.Join([]string{
		"## Related Changes",
		"- [ ] #1",
		"* [x] #2",
		"+ [X] #3",
		"[ ] #4",
		"1. [ ] #5",
		"  - [ ] #6",
		"- #7",
		"#8 without bullet",
		"- [] #9",
		"- [ ]",
		"- [ ] fix later",
		"- [x] done, no reference",
		"Some free prose.",
		"",
		"> quoted prose",
		"- [ ] #10 and #11",
		"- [ ] #12 and again #12",
		"<!-- - [ ] #13 -->",
		"<!--",
		"- [ ] #14",
		"-->",
		"- [ ] #15",
	}, "\n")
	want := []string{
		"2 followed 1", "3 followed 2", "4 followed 3", "5 followed 4", "6 followed 5", "7 followed 6",
		"8 notfollowed 7", "9 notfollowed 8", "10 notfollowed 9", "11 unparsed ", "12 unparsed ", "13 unparsed ",
		"17 followed 10,11", "18 followed 12", "23 followed 15",
	}
	got := summary(Related(text, numbered{}, owner))
	// "- [] #9" has no checkbox: a reference without one is not followed.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestRelatedCheckboxes(t *testing.T) {
	tests := []struct {
		line string
		box  Box
	}{
		{"- [ ] #1", Open}, {"- [x] #1", Ticked}, {"- [X] #1", Ticked}, {"[ ] #1", Open},
		{"* [ ] #1", Open}, {"+ [ ] #1", Open}, {"3. [ ] #1", Open}, {"4) [x] #1", Ticked}, {"\t- [ ] #1", Open},
		{"- #1", NoBox}, {"- [] #1", NoBox}, {"- [  ] #1", NoBox}, {"- [y] #1", NoBox}, {"- text [ ] #1", NoBox},
	}
	for _, tt := range tests {
		for _, eol := range []string{"\n", "\r\n"} {
			text := "## Related Changes" + eol + tt.line + eol
			got := Related(text, numbered{}, owner)
			if len(got) != 1 || got[0].Box != tt.box {
				t.Errorf("%q: %+v, want box %v", tt.line, got, tt.box)
				continue
			}
			e := got[0]
			if tt.box == NoBox {
				if e.BoxOffset != -1 {
					t.Errorf("%q: BoxOffset %d, want -1", tt.line, e.BoxOffset)
				}
				continue
			}
			if c := text[e.BoxOffset-1 : e.BoxOffset+2]; c[0] != '[' || c[2] != ']' || (c[1] != ' ' && c[1] != 'x' && c[1] != 'X') {
				t.Errorf("%q (%q): BoxOffset %d points at %q", tt.line, eol, e.BoxOffset, c)
			}
			if strings.Contains(e.Text, "\r") || e.Text != tt.line {
				t.Errorf("%q: Text = %q", tt.line, e.Text)
			}
		}
	}
}

func TestRelatedCRLFAndNoTrailingNewline(t *testing.T) {
	text := "Intro\r\n## Related PRs\r\n\r\n- [ ] #1\r\n- [x] #2\r\n\r\n## Next\r\n- [ ] #3"
	got := Related(text, numbered{}, owner)
	if want := []string{"4 followed 1", "5 followed 2"}; !reflect.DeepEqual(summary(got), want) {
		t.Fatalf("got %q, want %q", summary(got), want)
	}
	for _, e := range got {
		if text[e.BoxOffset] != ' ' && text[e.BoxOffset] != 'x' {
			t.Errorf("line %d: BoxOffset %d points at %q", e.Line, e.BoxOffset, text[e.BoxOffset])
		}
	}
}

func TestRelatedNoSection(t *testing.T) {
	for _, text := range []string{"", "Just a description.", "- [ ] #1"} {
		if got := Related(text, numbered{}, owner); got != nil {
			t.Errorf("Related(%q) = %+v", text, got)
		}
	}
}

func TestRelatedPassesTheOwner(t *testing.T) {
	got := Related("## Related PRs\n- [ ] #5\n", numbered{}, owner)
	want := core.Key{Forge: core.GitHub, Host: "github.com", Project: "acme/app", Number: 5}
	if len(got) != 1 || len(got[0].Refs) != 1 || got[0].Refs[0] != want {
		t.Errorf("got %+v, want ref %v", got, want)
	}
}

func TestRelatedDedupesAKeyOnALine(t *testing.T) {
	got := Related("## Related PRs\n- [ ] #5 (see #5)\n", numbered{}, owner)
	if len(got) != 1 || len(got[0].Refs) != 1 {
		t.Errorf("got %+v, want one ref", got)
	}
}

func TestKindStrings(t *testing.T) {
	for k, want := range map[Kind]string{Followed: "followed", NotFollowed: "notfollowed", Unparsed: "unparsed"} {
		if k.String() != want {
			t.Errorf("%d.String() = %q, want %q", k, k.String(), want)
		}
	}
}

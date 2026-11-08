package rules

import (
	"reflect"
	"strings"
	"testing"
)

// conditional is a catalog of rules with conditions and groups.
const conditional = `{"schema":1,"pack":"test","rules":[
 {"id":"always","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Common"},
 {"id":"views-only","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Views","when":{"ui-toolkit":["views","both"]}},
 {"id":"compose-only","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Compose","when":{"ui-toolkit":["compose","both"]}},
 {"id":"library-only","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Library","when":{"profile":["library"]}},
 {"id":"library-views","title":"T","severity":"MAJOR","description":"D","languages":[],"tags":[],"group":"Library","when":{"profile":["library"],"ui-toolkit":["views"]}},
 {"id":"more-common","title":"T","severity":"MINOR","description":"D","languages":[],"tags":[],"group":"Common"},
 {"id":"ungrouped","title":"T","severity":"MINOR","description":"D","languages":[],"tags":[]}
]}`

func loadConditional(t *testing.T) []*Rule {
	t.Helper()
	c, err := LoadCatalog([]byte(conditional))
	if err != nil {
		t.Fatal(err)
	}
	return c.Rules
}

func ruleIDs(rs []*Rule) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return strings.Join(out, " ")
}

func TestApplicable(t *testing.T) {
	rs := loadConditional(t)
	tests := []struct {
		name string
		ctx  Context
		want string
	}{
		{"views app", Context{Profile: "app", Dimensions: map[string]string{"ui-toolkit": "views"}}, "always views-only more-common ungrouped"},
		{"compose app", Context{Profile: "app", Dimensions: map[string]string{"ui-toolkit": "compose"}}, "always compose-only more-common ungrouped"},
		{"both app", Context{Profile: "app", Dimensions: map[string]string{"ui-toolkit": "both"}}, "always views-only compose-only more-common ungrouped"},
		{"compose library", Context{Profile: "library", Dimensions: map[string]string{"ui-toolkit": "compose"}}, "always compose-only library-only more-common ungrouped"},
		{"views library: every condition must hold", Context{Profile: "library", Dimensions: map[string]string{"ui-toolkit": "views"}},
			"always views-only library-only library-views more-common ungrouped"},
		{"a dimension the repository lacks", Context{Profile: "app"}, "always more-common ungrouped"},
		{"no profile", Context{Dimensions: map[string]string{"ui-toolkit": "views"}}, "always views-only more-common ungrouped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ruleIDs(Applicable(rs, tt.ctx)); got != tt.want {
				t.Errorf("applicable = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChecklist(t *testing.T) {
	rs := loadConditional(t)
	sections := Checklist(rs, Context{Profile: "library", Dimensions: map[string]string{"ui-toolkit": "views"}})
	var got []string
	for _, s := range sections {
		got = append(got, s.Heading+": "+ruleIDs(s.Rules))
	}
	want := []string{"Common: always more-common", "Views: views-only", "Library: library-only library-views", ": ungrouped"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sections = %q, want %q", got, want)
	}
	if got := Checklist(rs, Context{Profile: "kmp", Dimensions: map[string]string{"ui-toolkit": "compose"}}); len(got) != 3 {
		t.Errorf("sections of a kmp compose repository: %+v", got)
	}
	if got := Checklist(nil, Context{}); len(got) != 0 {
		t.Errorf("sections of no rules: %+v", got)
	}
}

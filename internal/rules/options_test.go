package rules

import (
	"reflect"
	"strings"
	"testing"
)

// lineLengthWithOptions is a catalog with one line-length rule whose max a
// repository may set between 60 and 200, and whose text names the max.
const lineLengthWithOptions = `{"schema":1,"pack":"test","rules":[{"id":"line-too-long","title":"Line over {max}","severity":"MINOR",` +
	`"description":"Lines stay within {max} characters.","why":"Lines over {max} characters are hard to read.","fix":"Wrap within {max}.",` +
	`"languages":["kotlin"],"tags":[],"match":{"detector":"line-length","max":100},"options":{"max":{"min":60,"max":200}}}]}`

func TestLoadCatalogOptions(t *testing.T) {
	c, err := LoadCatalog([]byte(lineLengthWithOptions))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Rules[0]
	if want := []Option{{Name: "max", Min: 60, Max: 200, Default: 100}}; !reflect.DeepEqual(r.Options(), want) {
		t.Errorf("options = %+v, want %+v", r.Options(), want)
	}
	text := []string{r.Title, r.Description, r.Why, r.Fix}
	want := []string{"Line over 100", "Lines stay within 100 characters.", "Lines over 100 characters are hard to read.", "Wrap within 100."}
	if !reflect.DeepEqual(text, want) {
		t.Errorf("text = %q, want %q", text, want)
	}
	plain, err := LoadCatalog([]byte(catalogWith(`{"detector":"line-length","max":1}`)))
	if err != nil || plain.Rules[0].Options() != nil {
		t.Errorf("a rule without options has options %+v, %v", plain.Rules[0].Options(), err)
	}
}

func TestRuleWithOptions(t *testing.T) {
	c, err := LoadCatalog([]byte(lineLengthWithOptions))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Rules[0]
	wider, err := r.WithOptions(map[string]int{"max": 120})
	if err != nil {
		t.Fatal(err)
	}
	if wider.Description != "Lines stay within 120 characters." || wider.Fix != "Wrap within 120." || wider.Options()[0].Default != 120 {
		t.Errorf("with max 120: %q, %q, %+v", wider.Description, wider.Fix, wider.Options())
	}
	if r.Description != "Lines stay within 100 characters." {
		t.Errorf("WithOptions changed the original rule: %q", r.Description)
	}
	f := kotlinFile(t, strings.Repeat("x", 110)+"\n")
	if got := len(Run([]*Rule{r}, f, Options{}).Signals); got != 1 {
		t.Errorf("max 100 on a 110-rune line: %d signals, want 1", got)
	}
	if got := len(Run([]*Rule{wider}, f, Options{}).Signals); got != 0 {
		t.Errorf("max 120 on a 110-rune line: %d signals, want 0", got)
	}
	same, err := r.WithOptions(nil)
	if err != nil || same.Description != r.Description {
		t.Errorf("no options: %q, %v", same.Description, err)
	}
}

func TestRuleWithOptionsRejects(t *testing.T) {
	c, err := LoadCatalog([]byte(lineLengthWithOptions))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		values map[string]int
		want   string
	}{
		{"undeclared parameter", map[string]int{"min": 80}, `"min"`},
		{"below the bounds", map[string]int{"max": 59}, "60"},
		{"above the bounds", map[string]int{"max": 201}, "200"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Rules[0].WithOptions(tt.values)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %s", err, tt.want)
			}
		})
	}
	for _, v := range []int{60, 200} {
		if _, err := c.Rules[0].WithOptions(map[string]int{"max": v}); err != nil {
			t.Errorf("max %d, a bound: %v", v, err)
		}
	}
}

func TestLoadCatalogRejectsOptions(t *testing.T) {
	tests := []struct{ name, from, to, want string }{
		{"bounds reversed", `{"min":60,"max":200}`, `{"min":200,"max":60}`, "bounds"},
		{"default below the bounds", `{"min":60,"max":200}`, `{"min":120,"max":200}`, "100"},
		{"default above the bounds", `{"min":60,"max":200}`, `{"min":60,"max":80}`, "100"},
		{"unknown parameter", `"options":{"max"`, `"options":{"length"`, "length"},
		{"a missing bound", `{"min":60,"max":200}`, `{"min":60}`, "max"},
		{"a bound below one", `{"min":60,"max":200}`, `{"min":0,"max":200}`, "min"},
		{"not the line-length detector", `{"detector":"line-length","max":100}`, missingDocMatch, "line-length"},
		{"no match", `,"match":{"detector":"line-length","max":100}`, ``, "match"},
		{"text names an option the rule lacks", `,"options":{"max":{"min":60,"max":200}}`, ``, "{max}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := strings.Replace(lineLengthWithOptions, tt.from, tt.to, 1)
			if data == lineLengthWithOptions {
				t.Fatalf("%s does not change the catalog", tt.from)
			}
			if tt.name == "no match" {
				data = strings.Replace(data, `"why":"Lines over {max} characters are hard to read.","fix":"Wrap within {max}.",`, ``, 1)
			}
			_, err := LoadCatalog([]byte(data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

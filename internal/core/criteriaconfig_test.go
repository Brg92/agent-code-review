package core

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func configExamples(t *testing.T, kind string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", "config", kind, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s config examples: %v", kind, err)
	}
	return files
}

func TestDecodeRepoConfigMatchesTheContract(t *testing.T) {
	for _, kind := range []string{"valid", "invalid"} {
		for _, f := range configExamples(t, kind) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeRepoConfig(ConfigFile, data)
			var ce *CriteriaError
			switch {
			case kind == "valid" && err != nil:
				t.Errorf("%s: %v", f, err)
			case kind == "invalid" && !errors.As(err, &ce):
				t.Errorf("%s: error %v, want a *CriteriaError", f, err)
			}
		}
	}
}

func TestDecodeRepoConfig(t *testing.T) {
	data := `{"schema":1,"profile":"library","packs":["android"],"variants":["free","paid"],"dimensions":{"ui-toolkit":"compose"},` +
		`"disabledRules":["app-tracker"],"ruleOptions":{"line-too-long":{"max":120}},"noise":["docs/**"],"cascade":{"maxDepth":1}}`
	c, err := DecodeRepoConfig(ConfigFile, []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	depth := 1
	want := RepoConfig{
		Profile: "library", Packs: []string{"android"}, Variants: []string{"free", "paid"},
		Dimensions: map[string]string{"ui-toolkit": "compose"}, DisabledRules: []string{"app-tracker"},
		RuleOptions: map[string]map[string]int{"line-too-long": {"max": 120}}, Noise: []string{"docs/**"},
		Cascade: CascadeConfig{MaxDepth: &depth},
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("config = %+v, want %+v", c, want)
	}
	if got := c.Cascade.Limits(); got != (Limits{MaxDepth: 1, MaxChangeRequests: 10}) {
		t.Errorf("limits = %+v, want depth 1 and the default count", got)
	}
}

// TestDecodeRepoConfigNamesEveryField checks scenario S6 B: the error
// names the file and each invalid field, not only the first one.
func TestDecodeRepoConfigNamesEveryField(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "config", "invalid", "s6-invalid-overlay.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeRepoConfig(ConfigFile, data)
	var ce *CriteriaError
	if !errors.As(err, &ce) {
		t.Fatalf("error %v, want a *CriteriaError", err)
	}
	var fields []string
	for _, f := range ce.Errors {
		fields = append(fields, f.Pointer)
	}
	if ce.File != ConfigFile || !reflect.DeepEqual(fields, []string{"/profile", "/schema", "/variants"}) {
		t.Errorf("error in %s at %q, want %s at /profile, /schema and /variants", ce.File, fields, ConfigFile)
	}
	msg := err.Error()
	for _, part := range []string{ConfigFile, "/profile", "spaceship", "/schema", "/variants"} {
		if !strings.Contains(msg, part) {
			t.Errorf("message %q does not mention %q", msg, part)
		}
	}
}

func TestDecodeRepoConfigRejects(t *testing.T) {
	tests := []struct{ name, data, pointer string }{
		{"not JSON", `{"schema":1`, ""},
		{"not an object", `[1]`, ""},
		{"trailing data", `{"schema":1} {}`, ""},
		{"no schema", `{}`, "/schema"},
		{"unknown field", `{"schema":1,"publicApi":[]}`, "/publicApi"},
		{"pack id", `{"schema":1,"packs":["Android"]}`, "/packs/0"},
		{"variant", `{"schema":1,"variants":["Free"]}`, "/variants/0"},
		{"dimension id", `{"schema":1,"dimensions":{"UI":"views"}}`, "/dimensions/UI"},
		{"dimension value", `{"schema":1,"dimensions":{"ui-toolkit":"Views"}}`, "/dimensions/ui-toolkit"},
		{"rule id", `{"schema":1,"disabledRules":["a rule"]}`, "/disabledRules/0"},
		{"rule options of a bad id", `{"schema":1,"ruleOptions":{"A":{"max":1}}}`, "/ruleOptions/A"},
		{"rule option not an object", `{"schema":1,"ruleOptions":{"line-too-long":120}}`, "/ruleOptions/line-too-long"},
		{"rule option not an integer", `{"schema":1,"ruleOptions":{"line-too-long":{"max":1.5}}}`, "/ruleOptions/line-too-long/max"},
		{"noise glob", `{"schema":1,"noise":["/abs/**"]}`, "/noise/0"},
		{"cascade field", `{"schema":1,"cascade":{"depth":1}}`, "/cascade/depth"},
		{"negative depth", `{"schema":1,"cascade":{"maxDepth":-1}}`, "/cascade/maxDepth"},
		{"zero count", `{"schema":1,"cascade":{"maxChangeRequests":0}}`, "/cascade/maxChangeRequests"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeRepoConfig(ConfigFile, []byte(tt.data))
			var ce *CriteriaError
			if !errors.As(err, &ce) || len(ce.Errors) != 1 || ce.Errors[0].Pointer != tt.pointer {
				t.Errorf("error %v, want one at %q", err, tt.pointer)
			}
		})
	}
}

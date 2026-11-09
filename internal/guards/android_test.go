package guards

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAndroidPackFilesMatchTheirSchemas validates the manifest, the rules
// catalog and the criteria templates of the android pack against the
// schemas they implement.
func TestAndroidPackFilesMatchTheirSchemas(t *testing.T) {
	files := map[string][]string{
		"pack-manifest": {"packs/android/manifest.json"},
		"rules-catalog": {"packs/android/rules.json"},
		"config":        {"packs/android/templates/*/config.json"},
	}
	schemas := schemaFiles(t)
	for name, patterns := range files {
		s := compileSchema(t, schemas[name])
		found := globAll(t, patterns...)
		if len(found) == 0 || (name == "config" && len(found) != 2) {
			t.Fatalf("%d %s files in the android pack", len(found), name)
		}
		for _, f := range found {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			issues, err := s.Validate(data)
			if err != nil {
				t.Errorf("%s: %v", f, err)
			}
			for _, i := range issues {
				t.Errorf("%s: %s", f, i)
			}
		}
	}
}

// scenarioOnlyRules are rules of the example catalog that only the
// sandbox scenarios use; the android pack does not define them.
var scenarioOnlyRules = map[string]bool{
	"style.redundant-if": true,
}

func severities(t *testing.T, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	rules, _ := readJSON(t, filepath.Join(repoRoot, path))["rules"].([]any)
	for _, r := range rules {
		rule, _ := r.(map[string]any)
		id, _ := rule["id"].(string)
		out[id], _ = rule["severity"].(string)
	}
	return out
}

// TestExampleCatalogAgreesWithTheAndroidPack keeps the small example
// catalog honest: a rule it shares with the android pack has the same
// severity there, a rule it shares with the core rules has the core
// severity, and any other rule is one that only the scenarios use.
func TestExampleCatalogAgreesWithTheAndroidPack(t *testing.T) {
	pack := severities(t, "packs/android/rules.json")
	core := severities(t, "internal/rules/core.json")
	for id, severity := range severities(t, "testdata/contracts/rules-catalog/valid/android.json") {
		switch want, ok := pack[id]; {
		case ok && want != severity:
			t.Errorf("%s: severity %s in the example, %s in the pack", id, severity, want)
		case core[id] != "" && core[id] != severity:
			t.Errorf("%s: severity %s in the example, %s in the core rules", id, severity, core[id])
		case !ok && core[id] == "" && !scenarioOnlyRules[id]:
			t.Errorf("%s: in the example but in neither the pack nor the core rules", id)
		}
	}
}

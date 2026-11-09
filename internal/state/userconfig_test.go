package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func userConfigExamples(t *testing.T, kind string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", "user-config", kind, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no %s user config examples: %v", kind, err)
	}
	return files
}

func TestUserConfigPath(t *testing.T) {
	dir := t.TempDir()
	got, err := UserConfigPath(func() (string, error) { return dir, nil })
	if want := filepath.Join(dir, "agent-code-review", "config.json"); err != nil || got != want {
		t.Errorf("path = %q, %v, want %q", got, err, want)
	}
	if _, err := UserConfigPath(func() (string, error) { return "", errors.New("no config dir") }); err == nil {
		t.Error("no error without a configuration directory")
	}
}

func TestLoadUserConfigDefaults(t *testing.T) {
	c, err := LoadUserConfig(filepath.Join(t.TempDir(), "agent-code-review", "config.json"))
	if err != nil || !reflect.DeepEqual(c, UserConfig{KeepDays: 7}) {
		t.Errorf("missing file: %+v, %v, want the defaults", c, err)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"schema":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := LoadUserConfig(p); err != nil || !reflect.DeepEqual(c, UserConfig{KeepDays: 7}) {
		t.Errorf("minimal file: %+v, %v, want the defaults", c, err)
	}
}

func TestLoadUserConfig(t *testing.T) {
	pack := filepath.Join(t.TempDir(), "web")
	p := filepath.Join(t.TempDir(), "config.json")
	data := `{"schema":1,"packPaths":[` + strings.ReplaceAll(`"`+pack+`"`, `\`, `\\`) + `],"keepDays":0}`
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadUserConfig(p)
	if err != nil || !reflect.DeepEqual(c, UserConfig{PackPaths: []string{pack}, KeepDays: 0}) {
		t.Errorf("config = %+v, %v", c, err)
	}
}

func TestLoadUserConfigMatchesTheContract(t *testing.T) {
	for _, kind := range []string{"valid", "invalid"} {
		for _, f := range userConfigExamples(t, kind) {
			_, err := LoadUserConfig(f)
			var ue *UserConfigError
			switch {
			case kind == "valid" && err != nil:
				t.Errorf("%s: %v", f, err)
			case kind == "invalid" && (!errors.As(err, &ue) || ue.Path != f):
				t.Errorf("%s: error %v, want a *UserConfigError for the file", f, err)
			}
		}
	}
}

func TestLoadUserConfigRejects(t *testing.T) {
	tests := []struct{ name, data, pointer string }{
		{"not JSON", `{`, ""},
		{"no schema", `{}`, "/schema"},
		{"a token", `{"schema":1,"githubToken":"x"}`, "/githubToken"},
		{"blank pack path", `{"schema":1,"packPaths":[" "]}`, "/packPaths/0"},
		{"relative pack path", `{"schema":1,"packPaths":["packs/web"]}`, "/packPaths/0"},
		{"keep days not an integer", `{"schema":1,"keepDays":1.5}`, "/keepDays"},
		{"negative keep days", `{"schema":1,"keepDays":-1}`, "/keepDays"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(p, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadUserConfig(p)
			var ue *UserConfigError
			if !errors.As(err, &ue) || len(ue.Errors) != 1 || ue.Errors[0].Pointer != tt.pointer {
				t.Errorf("error %v, want one at %q", err, tt.pointer)
			}
		})
	}
	if _, err := LoadUserConfig(t.TempDir()); err == nil {
		t.Error("a directory loaded as the configuration")
	}
}

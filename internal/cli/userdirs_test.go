package cli

import (
	"path/filepath"
	"testing"
)

func TestUserDirs(t *testing.T) {
	root := t.TempDir()
	abs := func(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }
	tests := []struct {
		name                string
		goos                string
		vars                map[string]string
		cache, config, home string // "" means an error
	}{
		{"linux, no XDG", "linux", map[string]string{"HOME": abs("h")}, filepath.Join(abs("h"), ".cache"), filepath.Join(abs("h"), ".config"), abs("h")},
		{"linux, XDG", "linux", map[string]string{"HOME": abs("h"), "XDG_CACHE_HOME": abs("c"), "XDG_CONFIG_HOME": abs("k")}, abs("c"), abs("k"), abs("h")},
		{"linux, a relative XDG is an error", "linux", map[string]string{"HOME": abs("h"), "XDG_CACHE_HOME": "rel", "XDG_CONFIG_HOME": "rel"}, "", "", abs("h")},
		{"linux, nothing", "linux", map[string]string{}, "", "", ""},
		{"darwin", "darwin", map[string]string{"HOME": abs("h")}, filepath.Join(abs("h"), "Library", "Caches"), filepath.Join(abs("h"), "Library", "Application Support"), abs("h")},
		{"windows", "windows", map[string]string{"LocalAppData": `C:\L`, "AppData": `C:\A`, "userprofile": `C:\U`}, `C:\L`, `C:\A`, `C:\U`},
		{"windows, nothing", "windows", map[string]string{}, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := userDirs{getenv: func(k string) string { return tt.vars[k] }, goos: tt.goos}
			for _, c := range []struct {
				what string
				fn   func() (string, error)
				want string
			}{{"cache", u.cache, tt.cache}, {"config", u.config, tt.config}, {"home", u.home, tt.home}} {
				got, err := c.fn()
				if c.want == "" {
					if err == nil {
						t.Errorf("%s = %q, want an error", c.what, got)
					}
					continue
				}
				if err != nil || got != c.want {
					t.Errorf("%s = %q, %v, want %q", c.what, got, err, c.want)
				}
			}
		})
	}
}

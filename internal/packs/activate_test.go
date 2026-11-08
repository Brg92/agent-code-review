package packs

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/rules"
)

// fakeRepo is an in-memory repository: paths to contents. It counts reads.
type fakeRepo struct {
	files map[string]string
	reads int
	fail  error
}

func (r *fakeRepo) ReadFile(path string) ([]byte, error) {
	r.reads++
	if r.fail != nil {
		return nil, r.fail
	}
	c, ok := r.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(c), nil
}

func (r *fakeRepo) paths() []string {
	var out []string
	for p := range r.files {
		out = append(out, p)
	}
	return out
}

func loadTestSet(t *testing.T) *Set {
	t.Helper()
	set, err := Load(testPacks)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func ids(ps []*Pack) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return strings.Join(out, " ")
}

func TestActivate(t *testing.T) {
	set := loadTestSet(t)
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"build script with the plugin", map[string]string{"app/build.gradle.kts": "plugins { id(\"mobile.application\") }", "README.md": ""}, "mobile"},
		{"build script without it", map[string]string{"app/build.gradle.kts": "plugins { id(\"other\") }"}, ""},
		{"a marker file", map[string]string{"app/src/main/Manifest.xml": ""}, "mobile"},
		{"root file only", map[string]string{"site/package.json": "{}"}, ""},
		{"two stacks", map[string]string{"package.json": "{}", "lib/build.gradle.kts": "id(\"mobile.library\")"}, "mobile web"},
		{"nothing", map[string]string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{files: tt.files}
			active, err := set.Activate(repo.paths(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(active); got != tt.want {
				t.Errorf("active %q, want %q", got, tt.want)
			}
		})
	}
}

func TestActivateReadsOnlyCandidates(t *testing.T) {
	repo := &fakeRepo{files: map[string]string{"package.json": "{}", "a/B.kt": "", "c/D.java": ""}}
	if _, err := loadTestSet(t).Activate(repo.paths(), repo); err != nil {
		t.Fatal(err)
	}
	if repo.reads != 0 {
		t.Errorf("%d reads, want none: no sign with contains matches a path", repo.reads)
	}
}

func TestActivateReadError(t *testing.T) {
	boom := errors.New("boom")
	repo := &fakeRepo{files: map[string]string{"build.gradle.kts": ""}, fail: boom}
	if _, err := loadTestSet(t).Activate(repo.paths(), repo); !errors.Is(err, boom) {
		t.Errorf("error = %v, want the read error", err)
	}
}

func TestSignMatchesDirectories(t *testing.T) {
	g, err := rules.CompileGlob("**/src/commonMain")
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{files: map[string]string{"shared/src/commonMain/kotlin/A.kt": ""}}
	if ok, err := (Sign{Path: g}).matches(repo.paths(), repo); err != nil || !ok {
		t.Errorf("a directory sign: %v, %v", ok, err)
	}
	if ok, err := (Sign{Path: g, Contains: "x"}).matches(repo.paths(), repo); err != nil || ok {
		t.Errorf("a directory has no contents to contain text: %v, %v", ok, err)
	}
}

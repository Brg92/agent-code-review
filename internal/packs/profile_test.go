package packs

import (
	"errors"
	"testing"
)

func TestResolveProfile(t *testing.T) {
	set := loadTestSet(t)
	mobile, web := set.Packs[0], set.Packs[1]
	app := map[string]string{"app/build.gradle.kts": `id("mobile.application")`}
	published := map[string]string{"lib/build.gradle.kts": `id("mobile.application")` + "\napply(plugin = \"maven-publish\")"}
	tests := []struct {
		name   string
		active []*Pack
		in     ProfileInput
		files  map[string]string
		want   Profile
	}{
		{"repository configuration first", []*Pack{mobile}, ProfileInput{Configured: "app", DependentCR: "github:github.com/o/app#1"}, published,
			Profile{Name: "app", Reason: ProfileFromConfig}},
		{"then a dependency of another change request", []*Pack{mobile}, ProfileInput{DependentCR: "github:github.com/o/app#1"}, app,
			Profile{Name: "library", Reason: ProfileFromCascade, Detail: "github:github.com/o/app#1"}},
		{"then the pack's signs", []*Pack{mobile}, ProfileInput{}, published,
			Profile{Name: "library", Reason: ProfileFromPack, Detail: "mobile: **/build.gradle.kts contains \"maven-publish\""}},
		{"then the pack's default", []*Pack{mobile}, ProfileInput{}, app,
			Profile{Name: "app", Reason: ProfileFromPackDefault, Detail: "mobile"}},
		{"signs of every active pack before any default", []*Pack{web, mobile}, ProfileInput{}, published,
			Profile{Name: "library", Reason: ProfileFromPack, Detail: "mobile: **/build.gradle.kts contains \"maven-publish\""}},
		{"default of the first active pack", []*Pack{web, mobile}, ProfileInput{}, app,
			Profile{Name: "app", Reason: ProfileFromPackDefault, Detail: "web"}},
		{"no active pack", nil, ProfileInput{}, app, Profile{Reason: ProfileUndetermined}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{files: tt.files}
			got, err := ResolveProfile(tt.active, tt.in, repo.paths(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("profile = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveProfileReadError(t *testing.T) {
	boom := errors.New("boom")
	repo := &fakeRepo{files: map[string]string{"build.gradle.kts": ""}, fail: boom}
	if _, err := ResolveProfile(loadTestSet(t).Packs[:1], ProfileInput{}, repo.paths(), repo); !errors.Is(err, boom) {
		t.Errorf("error = %v, want the read error", err)
	}
}

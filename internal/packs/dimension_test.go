package packs

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestResolveDimensions(t *testing.T) {
	set := loadTestSet(t)
	mobile, web := set.Packs[0], set.Packs[1]
	build := func(body string) map[string]string { return map[string]string{"app/build.gradle.kts": body} }
	tests := []struct {
		name       string
		active     []*Pack
		configured map[string]string
		files      map[string]string
		want       []DimensionValue
	}{
		{"compose signs", []*Pack{mobile}, nil, build("implementation(\"androidx.compose.ui:ui\")"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "compose", Reason: DimensionFromPack, Detail: "mobile: **/build.gradle.kts contains \"androidx.compose\""}}},
		{"views signs", []*Pack{mobile}, nil, build("buildFeatures { viewBinding = true }"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "views", Reason: DimensionFromPack, Detail: "mobile: **/build.gradle.kts contains \"viewBinding = true\""}}},
		{"another sign of the same value", []*Pack{mobile}, nil, build("dataBinding = true"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "views", Reason: DimensionFromPack, Detail: "mobile: **/build.gradle.kts contains \"dataBinding = true\""}}},
		{"both kinds of signs", []*Pack{mobile}, nil, build("compose = true\nviewBinding = true"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "both", Reason: DimensionFromPack, Detail: "mobile: compose, views"}}},
		{"signs in different files", []*Pack{mobile}, nil,
			map[string]string{"a/build.gradle.kts": "compose = true", "b/build.gradle.kts": "viewBinding = true"},
			[]DimensionValue{{ID: "ui-toolkit", Value: "both", Reason: DimensionFromPack, Detail: "mobile: compose, views"}}},
		{"no sign: the default", []*Pack{mobile}, nil, build("plugins { }"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "both", Reason: DimensionFromDefault, Detail: "mobile"}}},
		{"the configuration wins over the signs", []*Pack{mobile}, map[string]string{"ui-toolkit": "views"}, build("compose = true"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "views", Reason: DimensionFromConfig}}},
		{"a dimension of no active pack is ignored", []*Pack{mobile}, map[string]string{"theme": "dark"}, build("plugins { }"),
			[]DimensionValue{{ID: "ui-toolkit", Value: "both", Reason: DimensionFromDefault, Detail: "mobile"}}},
		{"a pack without dimensions", []*Pack{web}, nil, build("compose = true"), nil},
		{"no active pack", nil, map[string]string{"ui-toolkit": "views"}, build("compose = true"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{files: tt.files}
			got, err := ResolveDimensions(tt.active, tt.configured, repo.paths(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dimensions = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveDimensionsWithoutCombined(t *testing.T) {
	mobile := *loadTestSet(t).Packs[0]
	d := mobile.Dimensions[0]
	d.Combined = ""
	mobile.Dimensions = []Dimension{d}
	repo := &fakeRepo{files: map[string]string{"build.gradle.kts": "viewBinding = true\ncompose = true"}}
	got, err := ResolveDimensions([]*Pack{&mobile}, nil, repo.paths(), repo)
	if err != nil || len(got) != 1 || got[0].Value != "compose" {
		t.Errorf("without combined the first sign wins: %+v, %v", got, err)
	}
}

func TestResolveDimensionsFirstPackWins(t *testing.T) {
	mobile := loadTestSet(t).Packs[0]
	other := *mobile
	other.ID = "other"
	d := other.Dimensions[0]
	d.Default = "views"
	other.Dimensions = []Dimension{d}
	repo := &fakeRepo{files: map[string]string{"build.gradle.kts": ""}}
	got, err := ResolveDimensions([]*Pack{&other, mobile}, nil, repo.paths(), repo)
	if err != nil || len(got) != 1 || got[0].Value != "views" || got[0].Detail != "other" {
		t.Errorf("the first active pack declares the dimension: %+v, %v", got, err)
	}
}

func TestResolveDimensionsRejectsAConfiguredValueThePackLacks(t *testing.T) {
	mobile := loadTestSet(t).Packs[:1]
	repo := &fakeRepo{files: map[string]string{}}
	_, err := ResolveDimensions(mobile, map[string]string{"ui-toolkit": "xml"}, nil, repo)
	if err == nil || !strings.Contains(err.Error(), "xml") {
		t.Errorf("error = %v, want one that mentions the value", err)
	}
}

func TestResolveDimensionsReadError(t *testing.T) {
	boom := errors.New("boom")
	repo := &fakeRepo{files: map[string]string{"build.gradle.kts": ""}, fail: boom}
	if _, err := ResolveDimensions(loadTestSet(t).Packs[:1], nil, repo.paths(), repo); !errors.Is(err, boom) {
		t.Errorf("error = %v, want the read error", err)
	}
}

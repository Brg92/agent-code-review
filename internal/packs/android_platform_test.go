package packs

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// androidPlatformRules are the rules of the android pack that only make
// sense on Android source sets: they use Android framework or AndroidX
// types, the View system, Hilt or the Android lifecycle. Every other rule
// is Kotlin-general and applies in common code as well.
var androidPlatformRules = []string{
	// Framework and AndroidX types.
	"async-task", "new-live-data", "local-broadcast-manager", "missing-di-annotation",
	"context-leak", "context-in-viewmodel", "bitmap-in-model", "full-list-refresh",
	"webview-bridge-exposure", "context-captured",
	// Views: binding.
	"binding-not-cleared", "lateinit-binding", "find-view-by-id", "binding-outside-view-lifecycle",
	// Views: lifecycle.
	"launch-when-started", "wrong-lifecycle-owner", "work-in-oncreateview",
	// Views: architecture and layout.
	"logic-in-ui-class", "adapter-holds-context",
	"deep-view-hierarchy", "hardcoded-dimension", "views-missing-content-description",
}

func TestAndroidPlatformRulesAreClassifiedByCode(t *testing.T) {
	p := loadAndroid(t)
	var got []string
	for _, r := range p.Rules.Rules {
		if v, ok := r.When["platform"]; ok {
			if !reflect.DeepEqual(v, []string{"android"}) {
				t.Errorf("%s: platform condition %v, want [android]", r.ID, v)
			}
			got = append(got, r.ID)
		}
	}
	want := append([]string(nil), androidPlatformRules...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("platform rules\n got %s\nwant %s", strings.Join(got, " "), strings.Join(want, " "))
	}
	if len(got) != 22 || len(p.Rules.Rules) != 119 {
		t.Errorf("%d platform rules of %d, want 22 of 119", len(got), len(p.Rules.Rules))
	}
}

func TestAndroidPlatformConditionsKeepTheOthers(t *testing.T) {
	p := loadAndroid(t)
	for _, r := range p.Rules.Rules {
		if _, ok := r.When["platform"]; ok && strings.HasPrefix(r.Group, "Views") {
			if !reflect.DeepEqual(r.When["ui-toolkit"], []string{"views", "both"}) {
				t.Errorf("%s lost its ui-toolkit condition: %v", r.ID, r.When)
			}
		}
	}
}

func TestAndroidFilesAreAllAndroid(t *testing.T) {
	p := loadAndroid(t)
	if p.Priority != 0 || len(p.FileDimensions) != 1 {
		t.Fatalf("priority %d, file dimensions %+v", p.Priority, p.FileDimensions)
	}
	d := p.FileDimensions[0]
	if d.ID != "platform" || len(d.Map) != 0 || d.Default != "android" || !reflect.DeepEqual(d.Values, []string{"android"}) {
		t.Errorf("platform = %+v", d)
	}
	for _, path := range []string{"app/src/main/java/A.kt", "app/src/test/A.kt", "app/build.gradle.kts", "A.kt"} {
		if got := ResolveFileDimensions([]*Pack{p}, path)["platform"]; got != "android" {
			t.Errorf("%s: platform %q, want android", path, got)
		}
	}
}

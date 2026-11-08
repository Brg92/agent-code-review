package rules

import "testing"

func TestGlob(t *testing.T) {
	tests := []struct {
		glob, path string
		want       bool
	}{
		{"build.gradle", "build.gradle", true},
		{"build.gradle", "app/build.gradle", false},
		{"*.kt", "Main.kt", true},
		{"*.kt", "src/Main.kt", false},
		{"**/*.kt", "Main.kt", true},
		{"**/*.kt", "app/src/main/Main.kt", true},
		{"**/*.kt", "app/src/main/Main.kts", false},
		{"**/*.gradle.kts", "build.gradle.kts", true},
		{"**/test/**", "test/A.kt", true},
		{"**/test/**", "app/src/test/java/A.kt", true},
		{"**/test/**", "app/src/testing/A.kt", false},
		{"**/build/**", "app/build", true},
		{"**/src/main/AndroidManifest.xml", "app/src/main/AndroidManifest.xml", true},
		{"**/src/main/AndroidManifest.xml", "src/main/AndroidManifest.xml", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "a/x/y/c", false},
		{"src/*/Main.kt", "src/x/Main.kt", true},
		{"src/*/Main.kt", "src/x/y/Main.kt", false},
		{"*Test.kt", "MainTest.kt", true},
		{"*Test*.kt", "MainTests.kt", true},
		{"a*b*c", "abc", true},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
		{"[x].kt", "[x].kt", true},
		{"?.kt", "a.kt", false},
	}
	for _, tt := range tests {
		g, err := CompileGlob(tt.glob)
		if err != nil {
			t.Fatalf("CompileGlob(%q): %v", tt.glob, err)
		}
		if got := g.Match(tt.path); got != tt.want {
			t.Errorf("%q.Match(%q) = %v, want %v", tt.glob, tt.path, got, tt.want)
		}
	}
}

func TestCompileGlobRejects(t *testing.T) {
	for _, glob := range []string{"", "/abs", "a//b", "a/", `a\b`} {
		if _, err := CompileGlob(glob); err == nil {
			t.Errorf("CompileGlob(%q) = nil error, want one", glob)
		}
	}
}

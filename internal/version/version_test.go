package version

import "testing"

func TestIsValidSemVer(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		// Valid: core.
		{"0.0.0", true},
		{"0.0.4", true},
		{"0.2.3", true},
		{"10.20.30", true},
		{"99999999999999999999999.999999999999999999.99999999999999999", true},

		// Valid: pre-release.
		{"0.0.0-dev", true},
		{"0.9.0-beta.1", true},
		{"2.0.0-alpha", true},
		{"2.0.0-alpha.1", true},
		{"2.0.0-0.3.7", true},
		{"2.0.0-x.7.z.92", true},
		{"2.0.0-x-y-z.--", true},
		{"2.0.0-alpha-a.b-c", true},
		{"2.0.0-0", true},
		{"2.0.0-0a", true},
		{"2.0.0--", true},
		{"2.0.0---RC-SNAPSHOT.12.9.1--.12", true},

		// Valid: build metadata.
		{"2.0.0+20130313144700", true},
		{"2.0.0+001", true},
		{"2.0.0+21AF26D3---117B344092BD", true},
		{"0.0.0-ci+abc1234", true},
		{"2.0.0-beta+exp.sha.5114f85", true},
		{"2.0.0-rc.1+build.1", true},

		// Invalid: core.
		{"", false},
		{"1", false},
		{"1.2", false},
		{"1.2.3.4", false},
		{"01.2.3", false},
		{"2.03.4", false},
		{"2.3.04", false},
		{"v1.2.3", false},
		{" 1.2.3", false},
		{"1.2.3 ", false},
		{"1.2.3\n", false},
		{"a.b.c", false},
		{"-1.2.3", false},
		{"1.-2.3", false},
		{"٣.2.3", false},

		// Invalid: pre-release.
		{"2.0.0-", false},
		{"2.0.0-.alpha", false},
		{"2.0.0-alpha.", false},
		{"2.0.0-alpha..1", false},
		{"2.0.0-01", false},
		{"2.0.0-alpha.01", false},
		{"2.0.0-alpha_beta", false},
		{"2.0.0-é", false},

		// Invalid: build metadata.
		{"2.0.0+", false},
		{"2.0.0+.build", false},
		{"2.0.0+build.", false},
		{"2.0.0+a..b", false},
		{"2.0.0+a+b", false},
		{"2.0.0+a_b", false},
		{"2.0.0-+", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := IsValidSemVer(tt.in); got != tt.want {
				t.Errorf("IsValidSemVer(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDefaultVersion(t *testing.T) {
	if Version != "0.0.0-dev" {
		t.Errorf("Version = %q, want %q", Version, "0.0.0-dev")
	}
	if !IsValidSemVer(Version) {
		t.Errorf("default Version %q is not valid SemVer", Version)
	}
}

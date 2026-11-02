// Package version holds the build version of the CLI.
package version

import "regexp"

// semVer is the SemVer 2.0.0 grammar, including pre-release and build metadata.
var semVer = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
	`(-(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*)(\.(0|[1-9][0-9]*|[0-9]*[a-zA-Z-][0-9a-zA-Z-]*))*)?` +
	`(\+[0-9a-zA-Z-]+(\.[0-9a-zA-Z-]+)*)?$`)

// IsValidSemVer reports whether s is a valid SemVer 2.0.0 version.
func IsValidSemVer(s string) bool {
	return semVer.MatchString(s)
}

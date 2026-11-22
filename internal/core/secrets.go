package core

import "regexp"

// secretPatterns are the patterns that keep a text from being published.
// They are data compiled into the binary: there is no way to add, remove
// or turn off one. The order is the order of the names ScanSecrets
// returns.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"github-token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`)},
	{"aws-access-key-id", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)},
	{"slack-token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	// password, passwd, secret, token, api_key or apikey assigned a quoted
	// literal of at least 8 characters, in code, configuration or JSON; a
	// name that ends in one of them counts too.
	{"credential-assignment", regexp.MustCompile(`(?i)(?:password|passwd|secret|token|api_?key)["']?[ \t]*[:=][ \t]*["'][^"'\r\n]{8,}["']`)},
}

// ScanSecrets returns the names of the secret patterns that match text,
// each once, in a fixed order; nil when none does. It never returns the
// text that matched: callers print names, never values.
func ScanSecrets(text string) []string {
	var names []string
	for _, p := range secretPatterns {
		if p.re.MatchString(text) {
			names = append(names, p.name)
		}
	}
	return names
}

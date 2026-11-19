package github

import (
	"fmt"
	"strings"
)

// apiBases maps the hosts this release supports to the base URL of their
// REST API. A host that is not here is never contacted, and no token is
// ever sent to it. Supporting another host means adding an entry.
var apiBases = map[string]string{
	"github.com": "https://api.github.com",
}

// SupportedHost reports whether host is one this release can read.
func SupportedHost(host string) bool {
	_, ok := apiBases[strings.ToLower(host)]
	return ok
}

// UnsupportedHostError is a host the provider does not read. The cascade
// does not read such a host either: a link to a host other than the root's
// is skipped as other-platform before any read.
type UnsupportedHostError struct{ Host string }

func (e *UnsupportedHostError) Error() string {
	return fmt.Sprintf("github: host %q is not supported: only %s", e.Host, supportedList())
}

func supportedList() string {
	var hosts []string
	for h := range apiBases {
		hosts = append(hosts, h)
	}
	return strings.Join(hosts, ", ")
}

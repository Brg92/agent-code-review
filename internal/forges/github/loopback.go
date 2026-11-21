package github

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/Brg92/agent-code-review/internal/forges/base"
)

// NewLoopback is New for an API served on this machine, at the base URL
// apiBase, which is how tests that replay recordings read GitHub. The base
// must be http on a loopback IP address, 127.0.0.1 or [::1], with no user
// info: any other server, https and the real API included, is refused
// before the token is asked for, so that the token can only go to a process
// on this machine. The host is the one the keys of the change requests
// carry and must be one New supports. Production code never calls it; a
// guard in internal/guards keeps it so.
func NewLoopback(host, apiBase string, tokens Resolver, opts Options) (base.Reader, error) {
	if !SupportedHost(host) {
		return nil, &UnsupportedHostError{Host: host}
	}
	if err := checkLoopback(apiBase); err != nil {
		return nil, err
	}
	p, err := newProvider(strings.ToLower(host), apiBase, tokens, opts)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func checkLoopback(apiBase string) error {
	u, err := url.Parse(apiBase)
	if err != nil {
		return errors.New("github: the loopback base URL is not a URL")
	}
	ip := net.ParseIP(u.Hostname())
	switch {
	case u.Scheme != "http" || ip == nil || !ip.IsLoopback():
		return fmt.Errorf("github: the loopback base URL must be http on 127.0.0.1 or [::1], not %s", u.Redacted())
	case u.User != nil:
		return errors.New("github: the loopback base URL must not hold credentials")
	}
	return nil
}

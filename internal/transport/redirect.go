package transport

import (
	"errors"
	"net/http"
	"strings"
)

// maxRedirects bounds a redirect chain.
const maxRedirects = 10

// ErrCrossHostRedirect is returned for a redirect to another host, port or
// scheme. Such a redirect is never followed, so the Authorization header
// never reaches another host.
var ErrCrossHostRedirect = errors.New("transport: redirect to another host refused")

var errTooManyRedirects = errors.New("transport: too many redirects")

// checkRedirect follows a redirect only within the scheme and host of the
// first request.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errTooManyRedirects
	}
	first := via[0].URL
	if req.URL.Scheme != first.Scheme || !strings.EqualFold(req.URL.Host, first.Host) {
		return ErrCrossHostRedirect
	}
	return nil
}

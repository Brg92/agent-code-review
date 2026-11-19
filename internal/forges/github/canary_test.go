package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Brg92/agent-code-review/internal/auth"
)

// The canary is the token of the tests. A server that works against the
// provider echoes it everywhere it can, and no error that comes back, in its
// message or in the chain it wraps, may hold it, an Authorization header
// or a full URL. What the server echoes of the request line is its own text
// and is only redacted; TestErrorsHoldNoQuery checks that the provider
// never puts a query in an error itself.

// reads runs every read of the provider and returns the errors.
func reads(p *Provider) map[string]error {
	ctx := context.Background()
	k := synthKey
	errs := map[string]error{}
	_, errs["Changes"] = p.Changes().Get(ctx, k)
	_, errs["ChangeRequest"] = p.ChangeRequest(ctx, k)
	_, errs["ChangedFiles"] = p.ChangedFiles(ctx, k)
	_, _, errs["Files.Get"] = p.Files().Get(ctx, k, "main", "a.md")
	_, errs["Files.List"] = p.Files().List(ctx, k, "main", "docs")
	_, errs["Tree"] = p.Tree(ctx, k, "main").List("")
	_, errs["Search"] = p.Search().Search(k.ProjectKey(), "main", "term")
	_, errs["CI"] = p.CI(ctx, k, sha)
	_, errs["Notes"] = p.Notes(ctx, k)
	return errs
}

// leaks returns what an error holds that it must not.
func leaks(err error) []string {
	var found []string
	for e := error(err); e != nil; {
		msg := e.Error()
		for _, bad := range []string{canary, "Authorization", "http://", "https://"} {
			if strings.Contains(msg, bad) {
				found = append(found, fmt.Sprintf("%q in %q", bad, msg))
			}
		}
		e = errors.Unwrap(e)
	}
	return found
}

func TestNoErrorHoldsTheToken(t *testing.T) {
	echo := func(status int, extra map[string]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// The server throws back what it was sent, in the body and in headers.
			echoed := r.Header.Get("Authorization") + " " + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") + " " + r.URL.String()
			w.Header().Set("X-Echo", echoed)
			w.Header().Set("Location-Debug", r.URL.String())
			for k, v := range extra {
				w.Header().Set(k, v)
			}
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, `{"message":%q}`, "token "+canary+" for "+r.URL.String()+" "+echoed)
		}
	}
	cases := map[string]http.HandlerFunc{
		"404":               echo(404, nil),
		"403":               echo(403, map[string]string{"X-Accepted-GitHub-Permissions": "contents=read"}),
		"401":               echo(401, nil),
		"422":               echo(422, nil),
		"500":               echo(500, nil),
		"primary limit":     echo(403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": at(time.Hour)}),
		"secondary limit":   echo(429, map[string]string{"Retry-After": "600"}),
		"invalid JSON body": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("{" + canary)) },
		"redirect elsewhere": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example/"+canary+"?token="+canary, http.StatusFound)
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			p, err := newProvider("github.com", srv.URL, &tokens{token: canary}, testOptions(&clock{now: epoch}))
			if err != nil {
				t.Fatal(err)
			}
			for op, err := range reads(p) {
				if err == nil {
					continue
				}
				if l := leaks(err); len(l) > 0 {
					t.Errorf("%s: %v", op, l)
				}
			}
		})
	}
}

func TestNoErrorHoldsTheTokenWhenTheNetworkFails(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused
	p, err := newProvider("github.com", url, &tokens{token: canary}, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	for op, err := range reads(p) {
		if err == nil {
			t.Errorf("%s: no error on a closed server", op)
			continue
		}
		if l := leaks(err); len(l) > 0 {
			t.Errorf("%s: %v", op, l)
		}
	}
}

func TestNoErrorHoldsTheTokenWhenTheTokenSourceFails(t *testing.T) {
	boom := errors.New("credential helper said: " + "no token for you")
	for _, cause := range []error{auth.ErrNoToken, boom} {
		s := onSynthetic(t, nil)
		s.Tokens.err = cause
		for op, err := range reads(s.Provider) {
			if err == nil || len(leaks(err)) > 0 {
				t.Errorf("%s: error = %v, leaks %v", op, err, leaks(err))
			}
		}
		if n := len(s.Replay.Requests()); n != 0 {
			t.Errorf("%d requests were sent without a token", n)
		}
	}
}

func TestTheTokenIsOnlySentToTheAPIHost(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(okPull))
	}))
	defer srv.Close()
	p, err := newProvider("github.com", srv.URL, &tokens{token: canary}, testOptions(&clock{now: epoch}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ChangeRequest(context.Background(), synthKey); err != nil {
		t.Fatal(err)
	}
	if len(auths) != 1 || auths[0] != "Bearer "+canary {
		t.Errorf("Authorization = %v, want the token as a bearer, once", auths)
	}
}

func TestErrorsHoldNoQuery(t *testing.T) {
	for _, status := range []int{401, 403, 404, 422, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		}))
		p, err := newProvider("github.com", srv.URL, &tokens{token: canary}, testOptions(&clock{now: epoch}))
		if err != nil {
			t.Fatal(err)
		}
		for op, err := range reads(p) {
			if err == nil {
				continue
			}
			for e := error(err); e != nil; e = errors.Unwrap(e) {
				if strings.ContainsAny(e.Error(), "?&") || len(leaks(e)) > 0 {
					t.Errorf("status %d, %s: %q holds a query or a URL", status, op, e.Error())
				}
			}
		}
		srv.Close()
	}
}

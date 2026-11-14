package transport

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type recordingTripper struct{ called bool }

func (r *recordingTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.called = true
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func TestGetOnlyRefusesOtherMethods(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
		http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace, "get", "PROPFIND"}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			next := &recordingTripper{}
			req, err := http.NewRequest(m, "https://example.invalid/x", strings.NewReader("body"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := (getOnly{next}).RoundTrip(req); !errors.Is(err, errMethodNotAllowed) {
				t.Errorf("RoundTrip(%s) error = %v, want errMethodNotAllowed", m, err)
			}
			if next.called {
				t.Errorf("RoundTrip(%s) reached the network", m)
			}
		})
	}
}

func TestGetOnlyPassesGet(t *testing.T) {
	next := &recordingTripper{}
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/x", nil)
	if _, err := (getOnly{next}).RoundTrip(req); err != nil || !next.called {
		t.Errorf("RoundTrip(GET) = %v, called %v", err, next.called)
	}
}

// TestClientExposesOnlyReads guards the read-only API: every exported method
// of Client starts with Get, and none takes an HTTP method or a body.
func TestClientExposesOnlyReads(t *testing.T) {
	typ := reflect.TypeFor[*Client]()
	for i := range typ.NumMethod() {
		m := typ.Method(i)
		if !strings.HasPrefix(m.Name, "Get") {
			t.Errorf("Client.%s: exported methods must start with Get", m.Name)
		}
		for j := 1; j < m.Type.NumIn(); j++ {
			if in := m.Type.In(j); in == reflect.TypeFor[io.Reader]() {
				t.Errorf("Client.%s takes a body", m.Name)
			}
		}
	}
}

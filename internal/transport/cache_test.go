package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// etagServer answers 304 when If-None-Match matches its current ETag.
type etagServer struct {
	mu          sync.Mutex
	etag, body  string
	ifNoneMatch []string
	full        int
}

func (s *etagServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inm := r.Header.Get("If-None-Match")
	s.ifNoneMatch = append(s.ifNoneMatch, inm)
	if s.etag != "" {
		w.Header().Set("ETag", s.etag)
	}
	if inm != "" && inm == s.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.full++
	_, _ = w.Write([]byte(s.body))
}

func TestETagReuse(t *testing.T) {
	es := &etagServer{etag: `"v1"`, body: "first"}
	srv := httptest.NewServer(es)
	defer srv.Close()
	c := newTestClient(t, srv.URL, Config{Token: staticToken(testToken)})
	ctx := context.Background()

	r1, err := c.Get(ctx, "/x", "a/json")
	if err != nil || r1.FromCache || string(r1.Body) != "first" {
		t.Fatalf("first Get = %v, %+v", err, r1)
	}
	r2, err := c.Get(ctx, "/x", "a/json")
	if err != nil || !r2.FromCache || string(r2.Body) != "first" || r2.StatusCode != http.StatusOK {
		t.Fatalf("second Get = %v, %+v; want the cached body", err, r2)
	}
	r2.Body[0] = 'X'
	if r3, _ := c.Get(ctx, "/x", "a/json"); string(r3.Body) != "first" {
		t.Errorf("cache was changed through a returned body: %q", r3.Body)
	}

	es.mu.Lock()
	es.etag, es.body = `"v2"`, "second"
	es.mu.Unlock()
	r4, err := c.Get(ctx, "/x", "a/json")
	if err != nil || r4.FromCache || string(r4.Body) != "second" {
		t.Fatalf("Get after change = %v, %+v", err, r4)
	}
	want := []string{"", `"v1"`, `"v1"`, `"v1"`}
	if strings.Join(es.ifNoneMatch, ",") != strings.Join(want, ",") {
		t.Errorf("If-None-Match = %q, want %q", es.ifNoneMatch, want)
	}
}

func TestETagCacheKeys(t *testing.T) {
	es := &etagServer{etag: `"v1"`, body: "b"}
	srv := httptest.NewServer(es)
	defer srv.Close()
	ctx := context.Background()

	c := newTestClient(t, srv.URL, Config{Token: staticToken(testToken)})
	_, _ = c.Get(ctx, "/x", "a/json")
	_, _ = c.Get(ctx, "/x", "a/diff")
	_, _ = c.Get(ctx, "/y", "a/json")
	other := newTestClient(t, srv.URL, Config{Token: staticToken("other-token-value-4567")})
	_, _ = other.Get(ctx, "/x", "a/json")
	if es.full != 4 {
		t.Errorf("full responses = %d, want 4: accept, path and token are part of the key", es.full)
	}
}

func TestNoETagNoCache(t *testing.T) {
	es := &etagServer{body: "b"}
	srv := httptest.NewServer(es)
	defer srv.Close()
	c := newTestClient(t, srv.URL, Config{})
	for range 2 {
		if r, err := c.Get(context.Background(), "/x", ""); err != nil || r.FromCache {
			t.Fatalf("Get = %v, %+v", err, r)
		}
	}
	if es.ifNoneMatch[1] != "" {
		t.Errorf("If-None-Match sent without an ETag: %q", es.ifNoneMatch[1])
	}
}

func TestUnexpected304IsAnError(t *testing.T) {
	srv, _ := scriptedServer(t, step{status: http.StatusNotModified})
	if _, err := newTestClient(t, srv.URL, Config{}).Get(context.Background(), "/x", ""); err == nil {
		t.Error("Get on an unsolicited 304 = nil error")
	}
}

func TestCacheEvictsWhenFull(t *testing.T) {
	cache := newETagCache(10)
	cache.put("a", cachedResponse{etag: `"1"`, body: []byte("123456")})
	cache.put("b", cachedResponse{etag: `"2"`, body: []byte("123456")})
	if _, ok := cache.get("a"); ok {
		t.Error("entry a survived a full cache")
	}
	if _, ok := cache.get("b"); !ok {
		t.Error("entry b missing")
	}
	cache.put("c", cachedResponse{etag: `"3"`, body: make([]byte, 11)})
	if _, ok := cache.get("c"); ok {
		t.Error("an entry larger than the cache was stored")
	}
	cache.put("b", cachedResponse{etag: `"4"`, body: []byte("1234")})
	if e, _ := cache.get("b"); e.etag != `"4"` || cache.size != 4 {
		t.Errorf("replace: etag %s size %d", e.etag, cache.size)
	}
}

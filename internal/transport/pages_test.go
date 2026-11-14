package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// pagedServer serves total pages; page n links to n+1 with an absolute URL.
func pagedServer(t *testing.T, total int, link func(srv *httptest.Server, page int) string) (*httptest.Server, *[]string) {
	t.Helper()
	var auths []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		if page < total {
			w.Header().Set("Link", link(srv, page))
		}
		fmt.Fprintf(w, "page %d", page)
	}))
	t.Cleanup(srv.Close)
	return srv, &auths
}

func absNext(srv *httptest.Server, page int) string {
	return fmt.Sprintf(`<%s/items?page=%d>; rel="next", <%s/items?page=99>; rel="last"`, srv.URL, page+1, srv.URL)
}

func bodies(pages []*Response) []string {
	var out []string
	for _, p := range pages {
		out = append(out, string(p.Body))
	}
	return out
}

func TestGetPagesFollowsNext(t *testing.T) {
	srv, auths := pagedServer(t, 3, absNext)
	c := newTestClient(t, srv.URL, Config{Token: staticToken(testToken)})
	pages, err := c.GetPages(context.Background(), "/items", "", 5)
	if err != nil {
		t.Fatalf("GetPages: %v", err)
	}
	if got := fmt.Sprint(bodies(pages)); got != "[page 1 page 2 page 3]" {
		t.Errorf("bodies = %s", got)
	}
	for i, a := range *auths {
		if a != "Bearer "+testToken {
			t.Errorf("request %d Authorization = %q", i, a)
		}
	}
}

func TestGetPagesRelativeNext(t *testing.T) {
	srv, _ := pagedServer(t, 2, func(_ *httptest.Server, page int) string {
		return fmt.Sprintf(`</items?page=%d>; rel="next"`, page+1)
	})
	pages, err := newTestClient(t, srv.URL, Config{}).GetPages(context.Background(), "/items", "", 5)
	if err != nil || len(pages) != 2 {
		t.Fatalf("GetPages = %d pages, %v", len(pages), err)
	}
}

func TestGetPagesCap(t *testing.T) {
	srv, auths := pagedServer(t, 10, absNext)
	pages, err := newTestClient(t, srv.URL, Config{}).GetPages(context.Background(), "/items", "", 3)
	if !errors.Is(err, ErrPageLimit) {
		t.Fatalf("error = %v, want ErrPageLimit", err)
	}
	if len(pages) != 3 || len(*auths) != 3 {
		t.Errorf("pages = %d requests = %d, want 3 and 3", len(pages), len(*auths))
	}
}

func TestGetPagesExactlyAtCap(t *testing.T) {
	srv, _ := pagedServer(t, 3, absNext)
	if pages, err := newTestClient(t, srv.URL, Config{}).GetPages(context.Background(), "/items", "", 3); err != nil || len(pages) != 3 {
		t.Errorf("GetPages = %d pages, %v; want 3 pages and no error", len(pages), err)
	}
}

func TestGetPagesRefusesForeignNext(t *testing.T) {
	var otherHits int
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { otherHits++ }))
	defer other.Close()
	srv, _ := pagedServer(t, 2, func(*httptest.Server, int) string {
		return `<` + other.URL + `/items?page=2>; rel="next"`
	})
	pages, err := newTestClient(t, srv.URL, Config{Token: staticToken(testToken)}).GetPages(context.Background(), "/items", "", 5)
	if !errors.Is(err, ErrForeignLink) || len(pages) != 1 || otherHits != 0 {
		t.Errorf("GetPages = %d pages, %v, other hits %d", len(pages), err, otherHits)
	}
}

func TestGetPagesStopsOnLoop(t *testing.T) {
	srv, _ := pagedServer(t, 10, func(srv *httptest.Server, _ int) string {
		return `<` + srv.URL + `/items?page=1>; rel="next"`
	})
	if _, err := newTestClient(t, srv.URL, Config{}).GetPages(context.Background(), "/items?page=1", "", 5); !errors.Is(err, ErrPageLoop) {
		t.Errorf("error = %v, want ErrPageLoop", err)
	}
}

func TestGetPagesErrors(t *testing.T) {
	c := newTestClient(t, "https://example.invalid", Config{})
	if _, err := c.GetPages(context.Background(), "/x", "", 0); err == nil {
		t.Error("GetPages with cap 0 = nil error")
	}
	if _, err := c.GetPages(context.Background(), "x", "", 1); err == nil {
		t.Error("GetPages with a bad path = nil error")
	}
	srv, _ := scriptedServer(t, step{status: 404})
	if _, err := newTestClient(t, srv.URL, Config{}).GetPages(context.Background(), "/x", "", 1); err == nil {
		t.Error("GetPages on 404 = nil error")
	}
}

func TestNextLink(t *testing.T) {
	tests := []struct{ header, want string }{
		{"", ""},
		{`<https://h/a?page=2>; rel="next"`, "https://h/a?page=2"},
		{`<https://h/a?page=1>; rel="prev", <https://h/a?page=3>; rel="next"`, "https://h/a?page=3"},
		{`<https://h/a?page=3>; rel=next`, "https://h/a?page=3"},
		{`<https://h/a?page=3>; title="x"; REL="Next"`, "https://h/a?page=3"},
		{`<https://h/a?page=9>; rel="last"`, ""},
		{`<https://h/a?page=2>; rel="prev next"`, "https://h/a?page=2"},
		{`https://h/a; rel="next"`, ""},
	}
	for _, tt := range tests {
		h := http.Header{}
		if tt.header != "" {
			h.Set("Link", tt.header)
		}
		if got := nextLink(h); got != tt.want {
			t.Errorf("nextLink(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

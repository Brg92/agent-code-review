package ghrecord

import (
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestFileName(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		query  map[string]string
		prefix string
	}{
		{"no query", "GET", "/repos/o/r/pulls/2", nil, "GET_repos_o_r_pulls_2-"},
		{"query", "GET", "/repos/o/r/pulls/2/files", map[string]string{"per_page": "100", "page": "2"}, "GET_repos_o_r_pulls_2_files_page_2_per_page_100-"},
		{"dots and slashes", "GET", "/repos/o/r/contents/.github/mr-review.json", map[string]string{"ref": "feat/x"}, "GET_repos_o_r_contents_.github_mr-review.json_ref_feat_x-"},
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9._-]+-[0-9a-f]{8}\.json$`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FileName(tt.method, tt.path, tt.query)
			if !strings.HasPrefix(got, tt.prefix) || !valid.MatchString(got) {
				t.Errorf("FileName = %q, want prefix %q and pattern %s", got, tt.prefix, valid)
			}
			if again := FileName(tt.method, tt.path, tt.query); again != got {
				t.Errorf("FileName is not deterministic: %q then %q", got, again)
			}
		})
	}
}

func TestFileNameDistinguishesSimilarRequests(t *testing.T) {
	names := map[string]bool{}
	for _, r := range []struct {
		path  string
		query map[string]string
	}{
		{"/a/b", nil},
		{"/a_b", nil},
		{"/a/B", nil},
		{"/a/b", map[string]string{"page": "2"}},
		{"/a/b", map[string]string{"page": "3"}},
	} {
		name := FileName("GET", r.path, r.query)
		if names[name] {
			t.Errorf("FileName(%q, %v) = %q collides with an earlier request", r.path, r.query, name)
		}
		names[name] = true
	}
}

func TestFileNameIsBounded(t *testing.T) {
	path := "/repos/o/r/contents/" + strings.Repeat("deep/", 60) + "File.kt"
	if got := FileName("GET", path, map[string]string{"ref": strings.Repeat("f", 40)}); len(got) > 100 {
		t.Errorf("len(FileName) = %d, want at most 100", len(got))
	}
}

func TestRecordedHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
	h.Set("ETag", `W/"abc"`)
	h.Set("X-RateLimit-Remaining", "4999")
	h.Set("Set-Cookie", "secret")
	h.Set("Authorization", "Bearer x")
	want := map[string]string{"Link": `<https://api.github.com/x?page=2>; rel="next"`, "ETag": `W/"abc"`}
	if got := RecordedHeaders(h); !reflect.DeepEqual(got, want) {
		t.Errorf("RecordedHeaders = %v, want %v", got, want)
	}
	if got := RecordedHeaders(http.Header{}); got == nil || len(got) != 0 {
		t.Errorf("RecordedHeaders(empty) = %#v, want an empty non-nil map", got)
	}
}

func TestRecordingEncode(t *testing.T) {
	tests := []struct {
		name string
		rec  Recording
		want string
	}{
		{
			name: "body",
			rec: Recording{
				Method: "GET", Path: "/repos/o/r/pulls/2", Query: map[string]string{"per_page": "100"},
				Status: 200, Headers: map[string]string{"ETag": `"e"`}, Body: []byte(`{"title":"a <b>"}`),
			},
			want: `{
  "method": "GET",
  "path": "/repos/o/r/pulls/2",
  "query": {
    "per_page": "100"
  },
  "status": 200,
  "headers": {
    "ETag": "\"e\""
  },
  "body": {
    "title": "a <b>"
  }
}
`,
		},
		{
			name: "no body, nil maps",
			rec:  Recording{Method: "GET", Path: "/repos/o/r/pulls/9999", Status: 404},
			want: `{
  "method": "GET",
  "path": "/repos/o/r/pulls/9999",
  "query": {},
  "status": 404,
  "headers": {},
  "body": null
}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.rec.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Encode =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

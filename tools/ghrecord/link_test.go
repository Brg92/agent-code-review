package ghrecord

import "testing"

func TestNextLink(t *testing.T) {
	tests := []struct {
		name, header, want string
	}{
		{"empty", "", ""},
		{"next and last", `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`, "https://api.github.com/x?page=2"},
		{"next not first", `<https://api.github.com/x?page=1>; rel="prev", <https://api.github.com/x?page=3>; rel="next"`, "https://api.github.com/x?page=3"},
		{"no next", `<https://api.github.com/x?page=1>; rel="first", <https://api.github.com/x?page=4>; rel="prev"`, ""},
		{"comma in url", `<https://api.github.com/search?q=a,b&page=2>; rel="next"`, "https://api.github.com/search?q=a,b&page=2"},
		{"several rels", `<https://api.github.com/x?page=2>; rel="next last"`, "https://api.github.com/x?page=2"},
		{"extra params and spaces", `  <https://api.github.com/x?page=2> ; title="t" ; rel=next  `, "https://api.github.com/x?page=2"},
		{"malformed", `https://api.github.com/x?page=2; rel="next"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextLink(tt.header); got != tt.want {
				t.Errorf("NextLink(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

package ghrecord

import (
	"strings"
	"testing"
)

func testSanitizer(t *testing.T) *Sanitizer {
	t.Helper()
	s, err := NewSanitizer([]Identity{
		{Login: "RealReviewer", Synthetic: "sandbox-reviewer", ID: 1001},
		{Login: "real-author", Synthetic: "sandbox-author", ID: 1002},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewSanitizerRejectsInvalidIdentities(t *testing.T) {
	tests := []struct {
		name string
		ids  []Identity
	}{
		{"none", nil},
		{"empty login", []Identity{{Login: "", Synthetic: "a", ID: 1}}},
		{"empty synthetic", []Identity{{Login: "a", Synthetic: "", ID: 1}}},
		{"synthetic contains login", []Identity{{Login: "bob", Synthetic: "sandbox-bob", ID: 1}}},
		{"duplicate login", []Identity{{Login: "a", Synthetic: "x", ID: 1}, {Login: "A", Synthetic: "y", ID: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSanitizer(tt.ids); err == nil {
				t.Errorf("NewSanitizer(%v) succeeded, want error", tt.ids)
			}
		})
	}
}

func TestSanitizeText(t *testing.T) {
	s := testSanitizer(t)
	tests := []struct {
		name, in, want string
	}{
		{"plain", "nothing to change", "nothing to change"},
		{"login", "review by RealReviewer", "review by sandbox-reviewer"},
		{"login any case", "realreviewer and REAL-AUTHOR", "sandbox-reviewer and sandbox-author"},
		{"login in url", "https://github.com/real-author/repo", "https://github.com/sandbox-author/repo"},
		{"adjacent", "RealReviewer,RealReviewer", "sandbox-reviewer,sandbox-reviewer"},
		{"longer login untouched", "RealReviewer2 and xreal-author and real-author-bot", "RealReviewer2 and xreal-author and real-author-bot"},
		{"email", "mail me at someone@mail.test.org now", "mail me at redacted@example.com now"},
		{"noreply email", "1234+real-author@users.noreply.github.com", "redacted@example.com"},
		{"example email kept", "author@example.com", "author@example.com"},
		{"git ssh url kept", "git@github.com:acr-plugin-sandbox/core-lib.git", "git@github.com:acr-plugin-sandbox/core-lib.git"},
		{"classic token", "token ghp_" + strings.Repeat("a1", 18) + " end", "token [REDACTED] end"},
		{"fine-grained token", "github_pat_" + strings.Repeat("Ab3_", 10), "[REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Text(tt.in); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeJSON(t *testing.T) {
	s := testSanitizer(t)
	tests := []struct {
		name, in, want string
	}{
		{
			name: "user object",
			in: `{"user":{"login":"real-author","id":987654,"node_id":"MDQ6VXNlcjk4NzY1NA==",` +
				`"avatar_url":"https://avatars.githubusercontent.com/u/987654?v=4","gravatar_id":"",` +
				`"html_url":"https://github.com/real-author","type":"User"}}`,
			want: `{
  "user": {
    "avatar_url": "https://avatars.githubusercontent.com/u/1002?v=4",
    "gravatar_id": "",
    "html_url": "https://github.com/sandbox-author",
    "id": 1002,
    "login": "sandbox-author",
    "node_id": "U_sandbox_1002",
    "type": "User"
  }
}
`,
		},
		{
			name: "nested arrays and unknown users",
			in:   `[{"requested_reviewers":[{"login":"RealReviewer","id":5}]},{"login":"someone-else","id":7},12.50,null,true]`,
			want: `[
  {
    "requested_reviewers": [
      {
        "id": 1001,
        "login": "sandbox-reviewer"
      }
    ]
  },
  {
    "id": 7,
    "login": "someone-else"
  },
  12.50,
  null,
  true
]
`,
		},
		{
			name: "strings everywhere",
			in:   `{"body":"cc @RealReviewer","commit":{"author":{"email":"x@corp.test"}}}`,
			want: `{
  "body": "cc @sandbox-reviewer",
  "commit": {
    "author": {
      "email": "redacted@example.com"
    }
  }
}
`,
		},
		{
			name: "html characters are not escaped",
			in:   `{"body":"a <b> & c"}`,
			want: "{\n  \"body\": \"a <b> & c\"\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.JSON([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("JSON(%s) =\n%s\nwant\n%s", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeJSONReplacesUnknownUsers(t *testing.T) {
	s := testSanitizer(t)
	first := `{"z":{"html_url":"https://github.com/Stranger"},` +
		`"a":{"login":"Stranger","id":555,"type":"User","avatar_url":"https://avatars.githubusercontent.com/u/555?v=4"},` +
		`"b":{"login":"another-one","id":556,"type":"User"},` +
		`"app":{"login":"github","id":9919,"type":"Organization"},` +
		`"bot":{"login":"github-actions[bot]","id":41898282,"type":"Bot"}}`
	want := `{
  "a": {
    "avatar_url": "https://avatars.githubusercontent.com/u/1102?v=4",
    "id": 1102,
    "login": "sandbox-user-2",
    "type": "User"
  },
  "app": {
    "id": 9919,
    "login": "github",
    "type": "Organization"
  },
  "b": {
    "id": 1101,
    "login": "sandbox-user-1",
    "type": "User"
  },
  "bot": {
    "id": 41898282,
    "login": "github-actions[bot]",
    "type": "Bot"
  },
  "z": {
    "html_url": "https://github.com/sandbox-user-2"
  }
}
`
	got, err := s.JSON([]byte(first))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("JSON =\n%s\nwant\n%s", got, want)
	}
	// A later document keeps the names given earlier.
	got, err = s.JSON([]byte(`[{"login":"stranger","id":555,"type":"User"},{"login":"third","type":"User"}]`))
	if err != nil {
		t.Fatal(err)
	}
	wantLater := `[
  {
    "id": 1102,
    "login": "sandbox-user-2",
    "type": "User"
  },
  {
    "login": "sandbox-user-3",
    "type": "User"
  }
]
`
	if string(got) != wantLater {
		t.Errorf("JSON =\n%s\nwant\n%s", got, wantLater)
	}
}

func TestSanitizeJSONRejectsInvalidInput(t *testing.T) {
	s := testSanitizer(t)
	for _, in := range []string{``, `{`, `{"a":1} {"b":2}`, `not json`} {
		if _, err := s.JSON([]byte(in)); err == nil {
			t.Errorf("JSON(%q) succeeded, want error", in)
		}
	}
}

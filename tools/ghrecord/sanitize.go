// Package ghrecord records read-only GitHub REST API responses for offline
// replay in tests. It is a development tool and never ships.
package ghrecord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Identity maps a real account to the synthetic identity that replaces it in
// recorded data.
type Identity struct {
	Login     string
	Synthetic string
	ID        int64
}

// Sanitizer replaces real logins, user ids, avatar URLs, email addresses and
// token-like strings in recorded data. It remembers the unknown users it has
// seen, so one Sanitizer serves one recording run and is not safe for
// concurrent use.
type Sanitizer struct {
	ids     []Identity
	unknown int
}

var (
	tokenPattern = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})`)
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
)

const (
	redactedToken = "[REDACTED]"
	redactedEmail = "redacted@example.com"
)

// NewSanitizer returns a Sanitizer for the given identities.
func NewSanitizer(ids []Identity) (*Sanitizer, error) {
	if len(ids) == 0 {
		return nil, errors.New("no identities to sanitize")
	}
	for i, id := range ids {
		if id.Login == "" || id.Synthetic == "" {
			return nil, fmt.Errorf("identity %d: login and synthetic login are required", i)
		}
		for j, other := range ids {
			if strings.Contains(strings.ToLower(id.Synthetic), strings.ToLower(other.Login)) {
				return nil, fmt.Errorf("identity %d: synthetic login contains a real login", i)
			}
			if j < i && strings.EqualFold(id.Login, other.Login) {
				return nil, fmt.Errorf("identity %d: duplicate login", i)
			}
		}
	}
	return &Sanitizer{ids: append([]Identity(nil), ids...)}, nil
}

// Text sanitizes a single string.
func (s *Sanitizer) Text(in string) string {
	out := tokenPattern.ReplaceAllString(in, redactedToken)
	out = emailPattern.ReplaceAllStringFunc(out, func(addr string) string {
		lower := strings.ToLower(addr)
		if strings.HasSuffix(lower, "@example.com") || lower == "git@github.com" {
			return addr
		}
		return redactedEmail
	})
	return s.replaceLogins(out)
}

// replaceLogins replaces every real login that is not part of a longer login.
func (s *Sanitizer) replaceLogins(in string) string {
	var b strings.Builder
	for i := 0; i < len(in); {
		if id, ok := s.loginAt(in, i); ok {
			b.WriteString(id.Synthetic)
			i += len(id.Login)
			continue
		}
		b.WriteByte(in[i])
		i++
	}
	return b.String()
}

func (s *Sanitizer) loginAt(in string, i int) (Identity, bool) {
	if i > 0 && isLoginChar(in[i-1]) {
		return Identity{}, false
	}
	for _, id := range s.ids {
		end := i + len(id.Login)
		if end > len(in) || !strings.EqualFold(in[i:end], id.Login) {
			continue
		}
		if end < len(in) && isLoginChar(in[end]) {
			continue
		}
		return id, true
	}
	return Identity{}, false
}

func isLoginChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-'
}

// JSON sanitizes a JSON document and returns it indented, with a trailing
// newline.
func (s *Sanitizer) JSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("decode JSON: trailing data after the document")
	}
	s.registerUnknownUsers(v)
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.value(v)); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// unknownUserID is added to the number of an unknown user to form its
// synthetic id.
const unknownUserID = 1100

// registerUnknownUsers gives every user account in v that is not a known
// identity a synthetic one, sandbox-user-<n>. GitHub links commits to any
// account that verified the commit email, so strangers can appear in
// recorded data. Names are assigned in sorted order within a document and
// kept for the rest of the run.
func (s *Sanitizer) registerUnknownUsers(v any) {
	found := map[string]string{}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case []any:
			for _, e := range v {
				walk(e)
			}
		case map[string]any:
			login, _ := v["login"].(string)
			if typ, _ := v["type"].(string); typ == "User" && login != "" && !s.known(login) {
				found[strings.ToLower(login)] = login
			}
			for _, e := range v {
				walk(e)
			}
		}
	}
	walk(v)
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s.unknown++
		s.ids = append(s.ids, Identity{
			Login:     found[k],
			Synthetic: "sandbox-user-" + strconv.Itoa(s.unknown),
			ID:        int64(unknownUserID + s.unknown),
		})
	}
}

func (s *Sanitizer) known(login string) bool {
	for _, id := range s.ids {
		if strings.EqualFold(id.Login, login) {
			return true
		}
	}
	return false
}

func (s *Sanitizer) value(v any) any {
	switch v := v.(type) {
	case string:
		return s.Text(v)
	case []any:
		for i := range v {
			v[i] = s.value(v[i])
		}
	case map[string]any:
		if login, ok := v["login"].(string); ok {
			s.replaceUser(v, login)
		}
		for k, e := range v {
			v[k] = s.value(e)
		}
	}
	return v
}

// replaceUser rewrites the identifying fields of a user object whose login
// belongs to a real identity. Fields that are absent stay absent.
func (s *Sanitizer) replaceUser(user map[string]any, login string) {
	for _, id := range s.ids {
		if !strings.EqualFold(login, id.Login) {
			continue
		}
		n := strconv.FormatInt(id.ID, 10)
		replacements := map[string]any{
			"id":          json.Number(n),
			"node_id":     "U_sandbox_" + n,
			"avatar_url":  "https://avatars.githubusercontent.com/u/" + n + "?v=4",
			"gravatar_id": "",
		}
		for k, r := range replacements {
			if _, ok := user[k]; ok {
				user[k] = r
			}
		}
		return
	}
}

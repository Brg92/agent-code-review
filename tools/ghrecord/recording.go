package ghrecord

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Recording is one recorded API response, stored as one JSON file.
type Recording struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
}

// recordedHeaders are the only response headers kept in a recording.
var recordedHeaders = []string{"Link", "ETag"}

// maxSlug bounds the readable part of a file name, so that paths stay short
// on Windows checkouts.
const maxSlug = 80

// FileName returns the file name of the recording of a request: a readable
// slug of the request followed by a short hash that keeps names unique.
func FileName(method, path string, query map[string]string) string {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := url.Values{}
	slug := method + path
	for _, k := range keys {
		values.Set(k, query[k])
		slug += "_" + k + "_" + query[k]
	}
	slug = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, slug)
	if len(slug) > maxSlug {
		slug = slug[:maxSlug]
	}
	sum := sha256.Sum256([]byte(method + " " + path + "?" + values.Encode()))
	return slug + "-" + hex.EncodeToString(sum[:4]) + ".json"
}

// RecordedHeaders returns the response headers that are kept in a recording.
func RecordedHeaders(h http.Header) map[string]string {
	kept := map[string]string{}
	for _, name := range recordedHeaders {
		if v := h.Get(name); v != "" {
			kept[name] = v
		}
	}
	return kept
}

// Encode returns the recording as indented JSON with a trailing newline.
func (r Recording) Encode() ([]byte, error) {
	if r.Query == nil {
		r.Query = map[string]string{}
	}
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	if len(r.Body) == 0 {
		r.Body = json.RawMessage("null")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

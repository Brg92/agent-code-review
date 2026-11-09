package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDecodeDocumentRoundTrip decodes every valid findings example of the
// contracts and encodes it again: nothing is lost or added.
func TestDecodeDocumentRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", "findings", "valid", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := DecodeDocument(data)
			if err != nil {
				t.Fatal(err)
			}
			again, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(again, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the document:\n%s", again)
			}
		})
	}
}

func TestDecodeDocumentRejects(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "findings", "valid", "s4-not-verified.json"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, data, want string }{
		{"unknown field", strings.Replace(string(data), `"title"`, `"name":"x","title"`, 1), "unknown field"},
		{"trailing data", string(data) + "{}", "trailing"},
		{"bad key", strings.Replace(string(data), `"github:github.com/`, `"github.com/`, 1), "cr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeDocument([]byte(tt.data)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v, want one that mentions %q", err, tt.want)
			}
		})
	}
}

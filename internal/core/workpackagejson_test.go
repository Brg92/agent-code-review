package core_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Brg92/agent-code-review/internal/core"
)

func TestWorkPackageRoundTrip(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "contracts", "workpackage", "valid", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			wp, err := core.DecodeWorkPackage(data)
			if err != nil {
				t.Fatal(err)
			}
			out, err := core.EncodeWorkPackage(wp)
			if err != nil {
				t.Fatal(err)
			}
			again, err := core.DecodeWorkPackage(out)
			if err != nil {
				t.Fatal(err)
			}
			out2, _ := core.EncodeWorkPackage(again)
			if !bytes.Equal(out, out2) {
				t.Error("encoding is not stable")
			}
			if !strings.HasSuffix(string(out), "}\n") {
				t.Error("no final newline")
			}
		})
	}
}

func TestEncodeWorkPackageDoesNotEscapeHTML(t *testing.T) {
	cr, err := core.ParseKey("github:github.com/acme/app#7")
	if err != nil {
		t.Fatal(err)
	}
	out, err := core.EncodeWorkPackage(core.WorkPackage{CR: cr, Title: "a <b> & c"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"a <b> & c"`) {
		t.Errorf("output %s", out)
	}
}

func TestDecodeWorkPackageRefuses(t *testing.T) {
	tests := map[string]string{
		"not json":       `{`,
		"unknown field":  `{"schema":1,"nope":true}`,
		"trailing data":  `{"schema":1} {}`,
		"wrong schema":   `{"schema":2}`,
		"missing schema": `{}`,
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := core.DecodeWorkPackage([]byte(in)); err == nil {
				t.Error("no error")
			}
		})
	}
}

// TestDecodeWorkPackageRefusesASignalWithoutItsLineText: a work package
// written before every signal carried the text of its line cannot be
// finalized, so it is refused when it is read.
func TestDecodeWorkPackageRefusesASignalWithoutItsLineText(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "contracts", "workpackage", "valid", "s1-signals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.DecodeWorkPackage(data); err != nil {
		t.Fatal(err)
	}
	old := regexp.MustCompile(`(?m)^\s*"lineText": .*\n`).ReplaceAll(data, nil)
	if bytes.Equal(old, data) {
		t.Fatal("the example has no lineText")
	}
	_, err = core.DecodeWorkPackage(old)
	if err == nil || !strings.Contains(err.Error(), "signal s1 has no lineText") {
		t.Errorf("error %v", err)
	}
}

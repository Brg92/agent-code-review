package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFromTemplate(t *testing.T) {
	m, err := fromTemplate([]byte(`{"name": "p", "version": "@VERSION@"}`), "1.2.3", true)
	if err != nil {
		t.Fatal(err)
	}
	if m["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", m["version"])
	}

	if _, err := fromTemplate([]byte(`{"name": "p"}`), "1.2.3", false); err != nil {
		t.Errorf("template without a version: %v", err)
	}

	tests := []struct {
		name        string
		tmpl        string
		wantVersion bool
		want        string
	}{
		{"not JSON", `{`, true, "template"},
		{"no placeholder", `{"name": "p", "version": "1.0.0"}`, true, "placeholder"},
		{"no version key", `{"name": "p"}`, true, "placeholder"},
		{"version where none belongs", `{"version": "@VERSION@"}`, false, "must not have a version"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fromTemplate([]byte(tt.tmpl), "1.2.3", tt.wantVersion)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("fromTemplate error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestValidVersion(t *testing.T) {
	for _, v := range []string{"0.0.0-dev", "1.0.0", "1.2.3-rc.1", "1.0.0+build.5", "10.20.30"} {
		if !validVersion(v) {
			t.Errorf("validVersion(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "1", "1.0", "v1.0.0", "01.0.0", "1.0.0-", "1.0.0 ", "1.0.0-01", "dev"} {
		if validVersion(v) {
			t.Errorf("validVersion(%q) = true, want false", v)
		}
	}
}

func TestMarshalJSON(t *testing.T) {
	got, err := marshalJSON(map[string]any{"b": "<&>", "a": []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	const want = "{\n  \"a\": [\n    \"x\"\n  ],\n  \"b\": \"<&>\"\n}\n"
	if string(got) != want {
		t.Errorf("marshalJSON = %q, want %q", got, want)
	}
}

func TestShippedTemplates(t *testing.T) {
	for _, name := range []string{"agent-plugins.json", "claude-plugin.json", "claude-marketplace.json"} {
		data, err := os.ReadFile(filepath.Join(pluginDir, "manifests", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fromTemplate(data, "1.0.0", name != "claude-marketplace.json"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

var testMCP = mcpServer{Name: "agent-code-review", Command: "bin/agent-code-review", Args: []string{"mcp"}}

func TestMCPEntries(t *testing.T) {
	claude := map[string]any{"name": "p"}
	if err := addClaudeMCP(claude, testMCP); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name": "p",
		"mcpServers": map[string]any{
			"agent-code-review": map[string]any{
				"command": "${CLAUDE_PLUGIN_ROOT}/bin/agent-code-review",
				"args":    []string{"mcp"},
			},
		},
	}
	if !reflect.DeepEqual(claude, want) {
		t.Errorf("claude manifest = %v, want %v", claude, want)
	}

	got, err := agentPluginsMCP(testMCP)
	if err != nil {
		t.Fatal(err)
	}
	const wantFile = `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "agent-code-review": {
      "args": [
        "mcp"
      ],
      "command": "./bin/agent-code-review",
      "type": "stdio"
    }
  }
}
`
	if string(got) != wantFile {
		t.Errorf("mcp.json =\n%s\nwant\n%s", got, wantFile)
	}
}

func TestMCPServerIsChecked(t *testing.T) {
	bad := []mcpServer{
		{Name: "", Command: "bin/x"},
		{Name: "x", Command: ""},
		{Name: "x", Command: "/abs/x"},
		{Name: "x", Command: "../x"},
		{Name: "x", Command: "bin/../x"},
		{Name: "x", Command: `bin\x`},
		{Name: "x", Command: "bin/x y"},
	}
	for _, s := range bad {
		if _, err := agentPluginsMCP(s); err == nil {
			t.Errorf("agentPluginsMCP(%+v) = nil error, want one", s)
		}
		if err := addClaudeMCP(map[string]any{}, s); err == nil {
			t.Errorf("addClaudeMCP(%+v) = nil error, want one", s)
		}
	}
}

package main

import (
	"strings"
	"testing"
)

// goodAgentPlugins is a manifest that passes checkAgentPlugins.
func goodAgentPlugins() map[string]any {
	return map[string]any{
		"$schema":     agentPluginsSchema,
		"name":        "agent-code-review",
		"version":     "1.2.3",
		"description": "d",
		"author":      map[string]any{"name": "n", "email": "e", "url": "u"},
		"homepage":    "h",
		"repository":  "r",
		"license":     "Apache-2.0",
		"keywords":    []any{"a", "b"},
		"extensions":  map[string]any{"com.example": map[string]any{}},
	}
}

func goodClaude() map[string]any {
	return map[string]any{
		"name":        "agent-code-review",
		"version":     "1.2.3",
		"description": "d",
		"agents":      []any{"./agents/code-reviewer.md"},
		"mcpServers":  map[string]any{"s": map[string]any{"command": "c", "args": []any{"a"}}},
	}
}

func goodMarketplace() map[string]any {
	return map[string]any{
		"name":  "agent-code-review",
		"owner": map[string]any{"name": "o"},
		"plugins": []any{
			map[string]any{"name": "agent-code-review", "source": "./", "description": "d"},
		},
	}
}

func goodMCPFile() map[string]any {
	return map[string]any{
		"$schema": mcpSchema,
		"mcpServers": map[string]any{
			"s": map[string]any{
				"type": "stdio", "command": "./bin/x", "args": []any{"a"},
				"env": map[string]any{"K": "v"}, "cwd": "${PLUGIN_ROOT}",
			},
		},
	}
}

func TestCheckersAcceptGoodManifests(t *testing.T) {
	files := map[string][]byte{"agents/code-reviewer.md": nil}
	for name, got := range map[string][]string{
		"agent plugins": checkAgentPlugins(goodAgentPlugins()),
		"claude":        checkClaudePlugin(goodClaude(), files),
		"marketplace":   checkMarketplace(goodMarketplace(), "agent-code-review"),
		"mcp":           checkMCPFile(goodMCPFile()),
	} {
		if len(got) != 0 {
			t.Errorf("%s: problems %v, want none", name, got)
		}
	}
}

type mutation struct {
	name string
	edit func(m map[string]any)
	want string
}

func runMutations(t *testing.T, good func() map[string]any, check func(map[string]any) []string, tests []mutation) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := good()
			tt.edit(m)
			got := strings.Join(check(m), "\n")
			if !strings.Contains(got, tt.want) {
				t.Errorf("problems = %q, want one containing %q", got, tt.want)
			}
		})
	}
}

func TestCheckAgentPlugins(t *testing.T) {
	runMutations(t, goodAgentPlugins, checkAgentPlugins, []mutation{
		{"unknown field", func(m map[string]any) { m["agents"] = "x" }, `unknown field "agents"`},
		{"missing schema", func(m map[string]any) { delete(m, "$schema") }, "$schema"},
		{"wrong schema", func(m map[string]any) { m["$schema"] = "https://x/1.1.0.json" }, "$schema"},
		{"missing name", func(m map[string]any) { delete(m, "name") }, "name"},
		{"upper case name", func(m map[string]any) { m["name"] = "Agent" }, "name"},
		{"name ends with dash", func(m map[string]any) { m["name"] = "agent-" }, "name"},
		{"name has double dash", func(m map[string]any) { m["name"] = "a--b" }, "name"},
		{"name has double dot", func(m map[string]any) { m["name"] = "a..b" }, "name"},
		{"name too long", func(m map[string]any) { m["name"] = strings.Repeat("a", 65) }, "name"},
		{"version not semver", func(m map[string]any) { m["version"] = "1.0" }, "version"},
		{"missing description", func(m map[string]any) { delete(m, "description") }, "description"},
		{"author with other key", func(m map[string]any) { m["author"] = map[string]any{"nick": "x"} }, "author"},
		{"author not an object", func(m map[string]any) { m["author"] = "x" }, "author"},
		{"keywords not strings", func(m map[string]any) { m["keywords"] = []any{1} }, "keywords"},
		{"homepage not a string", func(m map[string]any) { m["homepage"] = 1 }, "homepage"},
		{"extensions not an object", func(m map[string]any) { m["extensions"] = "x" }, "extensions"},
	})
}

func TestCheckClaudePlugin(t *testing.T) {
	files := map[string][]byte{"agents/code-reviewer.md": nil}
	runMutations(t, goodClaude, func(m map[string]any) []string { return checkClaudePlugin(m, files) }, []mutation{
		{"missing name", func(m map[string]any) { delete(m, "name") }, "name"},
		{"not kebab case", func(m map[string]any) { m["name"] = "Agent_Review" }, "name"},
		{"reserved prefix", func(m map[string]any) { m["name"] = "claude-review" }, "reserved"},
		{"reserved name", func(m map[string]any) { m["name"] = "anthropic" }, "reserved"},
		{"version not semver", func(m map[string]any) { m["version"] = "x" }, "version"},
		{"missing description", func(m map[string]any) { delete(m, "description") }, "description"},
		{"agent path without ./", func(m map[string]any) { m["agents"] = []any{"agents/code-reviewer.md"} }, `must start with "./"`},
		{"agent path not markdown", func(m map[string]any) { m["agents"] = []any{"./agents"} }, ".md"},
		{"agent file missing", func(m map[string]any) { m["agents"] = []any{"./agents/other.md"} }, "does not exist"},
		{"agents not a list", func(m map[string]any) { m["agents"] = "x" }, "agents"},
		{"mcp server without command", func(m map[string]any) { m["mcpServers"] = map[string]any{"s": map[string]any{}} }, "command"},
		{"mcp args not strings", func(m map[string]any) {
			m["mcpServers"] = map[string]any{"s": map[string]any{"command": "c", "args": []any{1}}}
		}, "args"},
	})
}

func TestCheckMarketplace(t *testing.T) {
	runMutations(t, goodMarketplace, func(m map[string]any) []string { return checkMarketplace(m, "agent-code-review") }, []mutation{
		{"missing name", func(m map[string]any) { delete(m, "name") }, "name"},
		{"bad name", func(m map[string]any) { m["name"] = "-x" }, "name"},
		{"missing owner name", func(m map[string]any) { m["owner"] = map[string]any{} }, "owner.name"},
		{"no plugins", func(m map[string]any) { m["plugins"] = []any{} }, "plugins"},
		{"entry without source", func(m map[string]any) {
			m["plugins"] = []any{map[string]any{"name": "agent-code-review"}}
		}, "source"},
		{"source without ./", func(m map[string]any) {
			m["plugins"] = []any{map[string]any{"name": "agent-code-review", "source": "plugins/x"}}
		}, `must start with "./"`},
		{"source goes up", func(m map[string]any) {
			m["plugins"] = []any{map[string]any{"name": "agent-code-review", "source": "./../x"}}
		}, `".."`},
		{"entry carries a version", func(m map[string]any) {
			m["plugins"] = []any{map[string]any{"name": "agent-code-review", "source": "./", "version": "1.0.0"}}
		}, "version"},
		{"other plugin", func(m map[string]any) {
			m["plugins"] = []any{map[string]any{"name": "other", "source": "./"}}
		}, "does not list"},
	})
}

func TestCheckMCPFile(t *testing.T) {
	server := func(m map[string]any) map[string]any {
		return m["mcpServers"].(map[string]any)["s"].(map[string]any)
	}
	runMutations(t, goodMCPFile, checkMCPFile, []mutation{
		{"extra top-level key", func(m map[string]any) { m["servers"] = 1 }, `unknown field "servers"`},
		{"wrong schema", func(m map[string]any) { m["$schema"] = "x" }, "$schema"},
		{"no servers key", func(m map[string]any) { delete(m, "mcpServers") }, "mcpServers"},
		{"wrong type", func(m map[string]any) { server(m)["type"] = "sse" }, "type"},
		{"missing command", func(m map[string]any) { delete(server(m), "command") }, "command"},
		{"shell string command", func(m map[string]any) { server(m)["command"] = "sh -c x" }, "command"},
		{"absolute command", func(m map[string]any) { server(m)["command"] = "/bin/x" }, "command"},
		{"command goes up", func(m map[string]any) { server(m)["command"] = "../x" }, "command"},
		{"extra server key", func(m map[string]any) { server(m)["url"] = "x" }, `unknown field "url"`},
		{"env not strings", func(m map[string]any) { server(m)["env"] = map[string]any{"K": 1} }, "env"},
		{"env named PLUGIN_ROOT", func(m map[string]any) { server(m)["env"] = map[string]any{"PLUGIN_ROOT": "x"} }, "env"},
		{"cwd outside the plugin", func(m map[string]any) { server(m)["cwd"] = "/tmp" }, "cwd"},
		{"args not strings", func(m map[string]any) { server(m)["args"] = []any{1} }, "args"},
	})
}

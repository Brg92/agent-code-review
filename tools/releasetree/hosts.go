package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type manifestFormat int

const (
	// formatAgentPlugins is the root plugin.json of Agent Plugins 1.0.
	formatAgentPlugins manifestFormat = iota
	// formatClaude is .claude-plugin/plugin.json.
	formatClaude
)

// host is one place the plugin is installed in. docs/plugin-formats.md says
// where each fact comes from.
type host struct {
	Name string
	// Verified is false for the hosts that are only structurally checked.
	Verified bool
	Format   manifestFormat
	// Manifest is the manifest path in the release tree. Hosts that read the
	// same manifest share the file.
	Manifest string
	// Agent is nil for a host with no documented slot for an agent in a plugin.
	Agent *agentSlot
}

// agentSlot is where a host takes an agent and how it names the tools.
type agentSlot struct {
	Path        string
	Tools       map[capability]string
	frontmatter func(name, description string, tools []string) string
}

var hosts = []host{
	{
		Name: "claude-code", Verified: true, Format: formatClaude,
		Manifest: ".claude-plugin/plugin.json",
		Agent: &agentSlot{
			Path:        "agents/code-reviewer.md",
			Tools:       map[capability]string{capShell: "Bash", capRead: "Read", capWrite: "Write"},
			frontmatter: claudeFrontmatter,
		},
	},
	{
		Name: "copilot", Verified: true, Format: formatAgentPlugins,
		Manifest: "plugin.json",
		Agent: &agentSlot{
			Path:        "com.github.copilot/agents/code-reviewer.agent.md",
			Tools:       map[capability]string{capShell: "execute", capRead: "read", capWrite: "edit"},
			frontmatter: copilotFrontmatter,
		},
	},
	{Name: "kiro", Format: formatAgentPlugins, Manifest: "plugin.json"},
	{Name: "codex", Format: formatAgentPlugins, Manifest: "plugin.json"},
	{Name: "cursor", Format: formatAgentPlugins, Manifest: "plugin.json"},
}

// render returns the agent file of a host: its frontmatter, then the neutral
// body unchanged.
func (s agentSlot) render(a agentSource) (string, error) {
	tools := make([]string, 0, len(a.Capabilities))
	for _, c := range a.Capabilities {
		tool, ok := s.Tools[c]
		if !ok {
			return "", fmt.Errorf("no tool for capability %q", c)
		}
		tools = append(tools, tool)
	}
	return "---\n" + s.frontmatter(a.Name, a.Description, tools) + "---\n" + a.Body, nil
}

func claudeFrontmatter(name, description string, tools []string) string {
	return "name: " + quote(name) + "\n" +
		"description: " + quote(description) + "\n" +
		"tools: " + strings.Join(tools, ", ") + "\n" +
		"skills:\n  - " + skillName + "\n"
}

func copilotFrontmatter(name, description string, tools []string) string {
	quoted := make([]string, len(tools))
	for i, t := range tools {
		quoted[i] = quote(t)
	}
	return "name: " + quote(name) + "\n" +
		"description: " + quote(description) + "\n" +
		"tools: [" + strings.Join(quoted, ", ") + "]\n"
}

// quote writes s as a JSON string, which is also a valid YAML string.
func quote(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(buf.String(), "\n")
}

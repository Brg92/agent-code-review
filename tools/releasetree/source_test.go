package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validAgent = `---
name: code-reviewer
description: Reviews pull requests.
capabilities: [shell, read, write]
---

Body line.
`

func TestParseAgent(t *testing.T) {
	got, err := parseAgent([]byte(validAgent))
	if err != nil {
		t.Fatal(err)
	}
	want := agentSource{
		Name:         "code-reviewer",
		Description:  "Reviews pull requests.",
		Capabilities: []capability{capShell, capRead, capWrite},
		Body:         "\nBody line.\n",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseAgent = %+v, want %+v", got, want)
	}
}

func TestParseAgentRejects(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"no frontmatter", "Body only.\n", "must start with"},
		{"not closed", "---\nname: a\n", "not closed"},
		{"carriage return", strings.ReplaceAll(validAgent, "\n", "\r\n"), "carriage return"},
		{"pinned model", strings.Replace(validAgent, "capabilities:", "model: x\ncapabilities:", 1), `unknown key "model"`},
		{"duplicate key", strings.Replace(validAgent, "name: code-reviewer", "name: a\nname: b", 1), `duplicate key "name"`},
		{"line without value", strings.Replace(validAgent, "name: code-reviewer", "name", 1), "key: value"},
		{"missing name", strings.Replace(validAgent, "name: code-reviewer\n", "", 1), `missing "name"`},
		{"missing description", strings.Replace(validAgent, "description: Reviews pull requests.\n", "", 1), `missing "description"`},
		{"missing capabilities", strings.Replace(validAgent, "capabilities: [shell, read, write]\n", "", 1), `missing "capabilities"`},
		{"capabilities not a list", strings.Replace(validAgent, "[shell, read, write]", "shell", 1), "list in brackets"},
		{"empty capabilities", strings.Replace(validAgent, "[shell, read, write]", "[]", 1), "at least one"},
		{"unknown capability", strings.Replace(validAgent, "write]", "web]", 1), `unknown capability "web"`},
		{"repeated capability", strings.Replace(validAgent, "write]", "read]", 1), `repeats capability "read"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAgent([]byte(tt.src))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("parseAgent error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestCheckSkill(t *testing.T) {
	const valid = "---\nname: code-review\ndescription: Reviews.\n---\n\n# Body\n"
	if err := checkSkill([]byte(valid)); err != nil {
		t.Fatalf("checkSkill(valid) = %v", err)
	}
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"too big", valid + strings.Repeat("x", maxSkillBytes), "over the limit"},
		{"other name", strings.Replace(valid, "code-review", "other", 1), "must be named"},
		{"extra key", strings.Replace(valid, "description:", "model: x\ndescription:", 1), `unknown key "model"`},
		{"missing description", strings.Replace(valid, "description: Reviews.\n", "", 1), `missing "description"`},
		{"empty body", "---\nname: code-review\ndescription: Reviews.\n---\n\n", "no body"},
		{"no frontmatter", "# Body\n", "must start with"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkSkill([]byte(tt.src))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("checkSkill error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// pluginDir is the plugin sources, relative to this package's test directory.
var pluginDir = filepath.Join("..", "..", "plugin")

func TestShippedSources(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join(pluginDir, "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkSkill(skill); err != nil {
		t.Errorf("skill: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(pluginDir, "agents", "code-reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxAgentBytes {
		t.Errorf("agent is %d bytes, limit %d", len(data), maxAgentBytes)
	}
	agent, err := parseAgent(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(agent.Capabilities, []capability{capShell, capRead, capWrite}) {
		t.Errorf("capabilities = %v, want shell, read, write", agent.Capabilities)
	}
	if !strings.Contains(agent.Body, "`code-review` skill") {
		t.Error("the agent body does not point at the code-review skill")
	}
	for _, flow := range []string{"agent-code-review next", "agent-code-review queue", "finalize"} {
		if strings.Contains(agent.Body, flow) {
			t.Errorf("the agent body repeats the flow of the skill (%q)", flow)
		}
	}
}

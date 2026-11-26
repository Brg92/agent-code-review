package main

import (
	"strings"
	"testing"
)

func testAgent() agentSource {
	return agentSource{
		Name:         "code-reviewer",
		Description:  `Reviews "pull requests": all of them.`,
		Capabilities: []capability{capShell, capRead, capWrite},
		Body:         "\nBody line.\n",
	}
}

func hostByName(t *testing.T, name string) host {
	t.Helper()
	for _, h := range hosts {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("no host %q", name)
	return host{}
}

func TestRenderAgent(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"claude-code", `---
name: "code-reviewer"
description: "Reviews \"pull requests\": all of them."
tools: Bash, Read, Write
skills:
  - code-review
---

Body line.
`},
		{"copilot", `---
name: "code-reviewer"
description: "Reviews \"pull requests\": all of them."
tools: ["execute", "read", "edit"]
---

Body line.
`},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got, err := hostByName(t, tt.host).Agent.render(testAgent())
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("render =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestRenderAgentNeedsEveryTranslation(t *testing.T) {
	slot := agentSlot{Path: "a.md", Tools: map[capability]string{capShell: "Bash"}, frontmatter: claudeFrontmatter}
	_, err := slot.render(testAgent())
	if err == nil || !strings.Contains(err.Error(), `no tool for capability "read"`) {
		t.Errorf("render error = %v, want a missing translation for read", err)
	}
}

func TestHostTable(t *testing.T) {
	wantVerified := map[string]bool{"claude-code": true, "copilot": true, "kiro": false, "codex": false, "cursor": false}
	if len(hosts) != len(wantVerified) {
		t.Fatalf("%d hosts, want %d", len(hosts), len(wantVerified))
	}
	for _, h := range hosts {
		verified, known := wantVerified[h.Name]
		if !known || h.Verified != verified {
			t.Errorf("host %q: verified = %v, want %v (known %v)", h.Name, h.Verified, verified, known)
		}
		if h.Manifest == "" {
			t.Errorf("host %q has no manifest", h.Name)
		}
		if h.Agent == nil {
			if h.Verified {
				t.Errorf("verified host %q gets no agent", h.Name)
			}
			continue
		}
		for _, c := range []capability{capShell, capRead, capWrite} {
			if h.Agent.Tools[c] == "" {
				t.Errorf("host %q has no tool for capability %q", h.Name, c)
			}
		}
		if len(h.Agent.Tools) != 3 {
			t.Errorf("host %q translates %d capabilities, want exactly shell, read and write", h.Name, len(h.Agent.Tools))
		}
	}
}

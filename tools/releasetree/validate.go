package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Brg92/agent-code-review/internal/release"
)

const (
	skillPath       = "skills/" + skillName + "/SKILL.md"
	marketplacePath = ".claude-plugin/marketplace.json"
	mcpPath         = "mcp.json"
)

// validateTree checks the release tree as a whole and returns every problem at
// once: what ships, what the manifests say, and that they agree.
func validateTree(files map[string][]byte, allowlist []string) error {
	var problems []string
	add := func(path string, msgs ...string) {
		for _, m := range msgs {
			problems = append(problems, path+": "+m)
		}
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if bad := release.DisallowedPaths(allowlist, paths); len(bad) != 0 {
		problems = append(problems, "outside the allowlist: "+strings.Join(bad, ", "))
	}

	if skill, ok := files[skillPath]; !ok {
		add(skillPath, "missing")
	} else if err := checkSkill(skill); err != nil {
		add(skillPath, err.Error())
	}

	manifests := map[string]manifestFormat{}
	for _, h := range hosts {
		manifests[h.Manifest] = h.Format
		if h.Agent == nil {
			continue
		}
		agent, ok := files[h.Agent.Path]
		switch {
		case !ok:
			add(h.Agent.Path, "missing")
		case len(agent) > maxAgentBytes:
			add(h.Agent.Path, fmt.Sprintf("%d bytes, over the limit of %d", len(agent), maxAgentBytes))
		}
	}

	versions := map[string]string{}
	claudeName := ""
	for _, p := range sortedKeys(manifests) {
		m, ok := manifestJSON(files, p, add)
		if !ok {
			continue
		}
		if manifests[p] == formatClaude {
			add(p, checkClaudePlugin(m, files)...)
			claudeName, _ = m["name"].(string)
		} else {
			add(p, checkAgentPlugins(m)...)
		}
		if v, ok := m["version"].(string); ok {
			versions[p] = v
		}
	}
	if m, ok := manifestJSON(files, marketplacePath, add); ok {
		add(marketplacePath, checkMarketplace(m, claudeName)...)
	}
	if distinct(versions) > 1 {
		problems = append(problems, fmt.Sprintf("versions differ between manifests: %v", versions))
	}

	if _, ok := files[mcpPath]; ok {
		if m, ok := manifestJSON(files, mcpPath, add); ok {
			add(mcpPath, checkMCPFile(m)...)
		}
	}

	for _, p := range paths {
		if strings.Contains(string(files[p]), versionPlaceholder) {
			add(p, "still has the version placeholder")
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "\n"))
}

func manifestJSON(files map[string][]byte, p string, add func(string, ...string)) (map[string]any, bool) {
	data, ok := files[p]
	if !ok {
		add(p, "missing")
		return nil, false
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		add(p, "not a JSON object: "+err.Error())
		return nil, false
	}
	return m, true
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func distinct(m map[string]string) int {
	seen := map[string]bool{}
	for _, v := range m {
		seen[v] = true
	}
	return len(seen)
}

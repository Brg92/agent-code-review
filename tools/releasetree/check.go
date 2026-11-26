package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// The checkers return one line per problem. They hold the rules of the
// formats in docs/plugin-formats.md: the standard library has no JSON Schema
// validator, so the constraints of the Agent Plugins schemas are written out.

var (
	kebab       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	marketplace = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// str returns m[key] when it is a non-empty string.
func str(m map[string]any, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok && s != ""
}

func isStrings(v any) bool {
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, e := range list {
		if _, ok := e.(string); !ok {
			return false
		}
	}
	return true
}

func isStringMap(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, e := range m {
		if _, ok := e.(string); !ok {
			return false
		}
	}
	return true
}

// unknownKeys reports the keys of m that are not in allowed, sorted.
func unknownKeys(m map[string]any, where string, allowed ...string) []string {
	var out []string
	for key := range m {
		if !slices.Contains(allowed, key) {
			out = append(out, fmt.Sprintf("%sunknown field %q", where, key))
		}
	}
	sort.Strings(out)
	return out
}

// validAgentPluginsName is the name rule of Agent Plugins 1.0.
func validAgentPluginsName(s string) bool {
	if len(s) < 1 || len(s) > 64 || strings.Contains(s, "--") || strings.Contains(s, "..") {
		return false
	}
	for i := range len(s) {
		c := s[i]
		alnum := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !alnum && c != '-' && c != '.' {
			return false
		}
		if !alnum && (i == 0 || i == len(s)-1) {
			return false
		}
	}
	return true
}

// checkAgentPlugins checks a root plugin.json against Agent Plugins 1.0.
func checkAgentPlugins(m map[string]any) []string {
	out := unknownKeys(m, "", "$schema", "name", "version", "description", "author",
		"homepage", "repository", "license", "keywords", "extensions")
	if m["$schema"] != agentPluginsSchema {
		out = append(out, "$schema must be "+agentPluginsSchema)
	}
	if name, ok := str(m, "name"); !ok || !validAgentPluginsName(name) {
		out = append(out, "name is missing or does not follow the Agent Plugins name rule")
	}
	if v, ok := str(m, "version"); !ok || !validVersion(v) {
		out = append(out, "version is missing or not a SemVer string")
	}
	if _, ok := str(m, "description"); !ok {
		out = append(out, "description is missing")
	}
	if a, has := m["author"]; has {
		am, ok := a.(map[string]any)
		if !ok || !isStringMap(am) {
			out = append(out, "author must be an object of strings")
		} else {
			out = append(out, unknownKeys(am, "author: ", "name", "email", "url")...)
		}
	}
	for _, key := range []string{"homepage", "repository", "license"} {
		if v, has := m[key]; has {
			if _, ok := v.(string); !ok {
				out = append(out, key+" must be a string")
			}
		}
	}
	if k, has := m["keywords"]; has && !isStrings(k) {
		out = append(out, "keywords must be a list of strings")
	}
	if e, has := m["extensions"]; has {
		if em, ok := e.(map[string]any); !ok {
			out = append(out, "extensions must be an object")
		} else {
			for ns, v := range em {
				if _, ok := v.(map[string]any); !ok {
					out = append(out, "extensions."+ns+" must be an object")
				}
			}
		}
	}
	return out
}

var reservedClaudeNames = []string{"claude", "anthropic", "claude-code", "claude-mods"}

// checkClaudePlugin checks .claude-plugin/plugin.json. files is the release
// tree, to see that the paths of the manifest exist.
func checkClaudePlugin(m map[string]any, files map[string][]byte) []string {
	var out []string
	name, ok := str(m, "name")
	switch {
	case !ok || !kebab.MatchString(name):
		out = append(out, "name is missing or not kebab-case")
	case slices.Contains(reservedClaudeNames, name) || strings.HasPrefix(name, "claude-") ||
		strings.HasPrefix(name, "anthropic-") || strings.HasPrefix(name, "anthropics-") || strings.HasPrefix(name, "cc-plugin-"):
		out = append(out, "name "+name+" is reserved")
	}
	if v, ok := str(m, "version"); !ok || !validVersion(v) {
		out = append(out, "version is missing or not a SemVer string")
	}
	if _, ok := str(m, "description"); !ok {
		out = append(out, "description is missing")
	}
	if a, has := m["agents"]; has {
		if !isStrings(a) {
			out = append(out, "agents must be a list of paths")
		} else {
			for _, p := range a.([]any) {
				s := p.(string)
				rel, ok := strings.CutPrefix(s, "./")
				switch {
				case !ok:
					out = append(out, fmt.Sprintf(`agents path %q must start with "./"`, s))
				case path.Ext(rel) != ".md":
					out = append(out, fmt.Sprintf("agents path %q must be a .md file", s))
				default:
					if _, exists := files[rel]; !exists {
						out = append(out, fmt.Sprintf("agents path %q does not exist in the tree", s))
					}
				}
			}
		}
	}
	if s, has := m["mcpServers"]; has {
		servers, ok := s.(map[string]any)
		if !ok {
			return append(out, "mcpServers must be an object")
		}
		for n, v := range servers {
			srv, ok := v.(map[string]any)
			if !ok {
				out = append(out, "mcpServers."+n+" must be an object")
				continue
			}
			if _, ok := str(srv, "command"); !ok {
				out = append(out, "mcpServers."+n+" has no command")
			}
			if a, has := srv["args"]; has && !isStrings(a) {
				out = append(out, "mcpServers."+n+" args must be a list of strings")
			}
		}
	}
	return out
}

// checkMarketplace checks .claude-plugin/marketplace.json, which Claude Code
// and Copilot both read. plugin is the plugin it must list.
func checkMarketplace(m map[string]any, plugin string) []string {
	var out []string
	if name, ok := str(m, "name"); !ok || !marketplace.MatchString(name) || strings.Contains(name, "..") {
		out = append(out, "name is missing or not a valid marketplace name")
	}
	if owner, ok := m["owner"].(map[string]any); !ok {
		out = append(out, "owner.name is missing")
	} else if _, ok := str(owner, "name"); !ok {
		out = append(out, "owner.name is missing")
	}
	plugins, _ := m["plugins"].([]any)
	if len(plugins) == 0 {
		return append(out, "plugins must list at least one plugin")
	}
	listed := false
	for i, p := range plugins {
		entry, ok := p.(map[string]any)
		if !ok {
			out = append(out, fmt.Sprintf("plugins[%d] must be an object", i))
			continue
		}
		name, ok := str(entry, "name")
		if !ok {
			out = append(out, fmt.Sprintf("plugins[%d] has no name", i))
		}
		listed = listed || name == plugin
		source, ok := str(entry, "source")
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("plugins[%d] has no source", i))
		case !strings.HasPrefix(source, "./"):
			out = append(out, fmt.Sprintf(`plugins[%d] source %q must start with "./"`, i, source))
		case slices.Contains(strings.Split(source, "/"), ".."):
			out = append(out, fmt.Sprintf(`plugins[%d] source %q must not contain ".."`, i, source))
		}
		if _, has := entry["version"]; has {
			out = append(out, fmt.Sprintf("plugins[%d] must not carry a version: plugin.json has it", i))
		}
	}
	if !listed {
		out = append(out, "plugins does not list "+plugin)
	}
	return out
}

// checkMCPFile checks an mcp.json against the Agent Plugins 1.0 schema, for
// the stdio servers the plugin declares.
func checkMCPFile(m map[string]any) []string {
	out := unknownKeys(m, "", "$schema", "mcpServers")
	if m["$schema"] != mcpSchema {
		out = append(out, "$schema must be "+mcpSchema)
	}
	servers, ok := m["mcpServers"].(map[string]any)
	if !ok {
		return append(out, "mcpServers must be an object")
	}
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		srv, ok := servers[n].(map[string]any)
		if !ok {
			out = append(out, "mcpServers."+n+" must be an object")
			continue
		}
		where := "mcpServers." + n + ": "
		out = append(out, unknownKeys(srv, where, "type", "command", "args", "env", "cwd")...)
		if srv["type"] != "stdio" {
			out = append(out, where+`type must be "stdio"`)
		}
		if c, ok := str(srv, "command"); !ok || strings.ContainsAny(c, " \\") || strings.Contains(c, "${") ||
			path.IsAbs(c) || (strings.Contains(c, "/") && !strings.HasPrefix(c, "./")) || slices.Contains(strings.Split(c, "/"), "..") {
			out = append(out, where+"command must be one executable name or a ./ path inside the plugin")
		}
		if a, has := srv["args"]; has && !isStrings(a) {
			out = append(out, where+"args must be a list of strings")
		}
		if e, has := srv["env"]; has {
			if !isStringMap(e) {
				out = append(out, where+"env must be an object of strings")
			} else {
				for k := range e.(map[string]any) {
					if k == "PLUGIN_ROOT" || k == "PLUGIN_DATA" {
						out = append(out, where+"env must not set "+k)
					}
				}
			}
		}
		if c, has := srv["cwd"]; has {
			s, _ := c.(string)
			if !strings.HasPrefix(s, "./") && !strings.HasPrefix(s, "${PLUGIN_ROOT}") && !strings.HasPrefix(s, "${PLUGIN_DATA}") {
				out = append(out, where+"cwd must start with ./, ${PLUGIN_ROOT} or ${PLUGIN_DATA}")
			}
		}
	}
	return out
}

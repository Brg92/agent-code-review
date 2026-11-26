package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	// versionPlaceholder stands for the version in the manifest templates.
	versionPlaceholder = "@VERSION@"

	agentPluginsSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	mcpSchema          = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

// semver is the expression published on semver.org.
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func validVersion(v string) bool { return semver.MatchString(v) }

// fromTemplate reads a manifest template. A template that carries a version
// has the placeholder in it, replaced by version; one that does not, has none.
func fromTemplate(data []byte, version string, wantVersion bool) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("template is not a JSON object: %w", err)
	}
	v, has := m["version"]
	switch {
	case wantVersion && v != versionPlaceholder:
		return nil, fmt.Errorf("template has no version placeholder %s", versionPlaceholder)
	case wantVersion:
		m["version"] = version
	case has:
		return nil, errors.New("template must not have a version")
	}
	return m, nil
}

// marshalJSON writes a manifest with sorted keys, two spaces of indent and a
// final newline, so the same input always gives the same bytes.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mcpServer is an MCP server the plugin declares. It is off by default and the
// release does not emit it; tests turn it on to check the shape of each host.
type mcpServer struct {
	Name string
	// Command is a path inside the plugin, such as bin/launcher.
	Command string
	Args    []string
}

func (s mcpServer) check() error {
	if s.Name == "" {
		return errors.New("mcp server has no name")
	}
	c := s.Command
	if c == "" || path.Clean(c) != c || path.IsAbs(c) || c == ".." || strings.HasPrefix(c, "../") ||
		strings.ContainsAny(c, ` \`) {
		return fmt.Errorf("mcp command %q is not a clean relative path inside the plugin", c)
	}
	return nil
}

// addClaudeMCP declares the server inline in the Claude Code manifest.
func addClaudeMCP(manifest map[string]any, s mcpServer) error {
	if err := s.check(); err != nil {
		return err
	}
	manifest["mcpServers"] = map[string]any{
		s.Name: map[string]any{
			"command": "${CLAUDE_PLUGIN_ROOT}/" + s.Command,
			"args":    append([]string{}, s.Args...),
		},
	}
	return nil
}

// agentPluginsMCP writes the mcp.json of Agent Plugins 1.0. The standard has
// no manifest field for it.
func agentPluginsMCP(s mcpServer) ([]byte, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return marshalJSON(map[string]any{
		"$schema": mcpSchema,
		"mcpServers": map[string]any{
			s.Name: map[string]any{
				"type":    "stdio",
				"command": "./" + s.Command,
				"args":    append([]string{}, s.Args...),
			},
		},
	})
}

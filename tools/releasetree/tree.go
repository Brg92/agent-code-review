package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Brg92/agent-code-review/internal/release"
)

// options are the inputs of the release tree.
type options struct {
	// Src is the repository root.
	Src     fs.FS
	Version string
	// Allowlist holds the patterns of docs/release-allowlist.txt; a file the
	// tree would hold outside them is an error.
	Allowlist []string
	// MCP, when not nil, adds an MCP server entry to every host. The release
	// never sets it.
	MCP *mcpServer
}

const (
	manifestsDir = "plugin/manifests/"
	agentSrcPath = "plugin/agents/code-reviewer.md"
	skillSrcPath = "plugin/skills/" + skillName + "/SKILL.md"
)

// lf turns CRLF line endings into LF in text files, so a checkout on any
// system gives the same tree. A file with a NUL byte is binary and unchanged.
func lf(data []byte) []byte {
	if bytes.IndexByte(data, 0) >= 0 {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

// buildTree returns the files of the release branch, by slash-separated path,
// without binaries and launchers.
func buildTree(o options) (map[string][]byte, error) {
	if !validVersion(o.Version) {
		return nil, fmt.Errorf("version %q is not a SemVer string", o.Version)
	}
	read := func(p string) ([]byte, error) {
		data, err := fs.ReadFile(o.Src, p)
		return lf(data), err
	}
	files := map[string][]byte{}

	skill, err := read(skillSrcPath)
	if err != nil {
		return nil, err
	}
	if err := checkSkill(skill); err != nil {
		return nil, fmt.Errorf("skill %s: %w", skillSrcPath, err)
	}
	files[skillPath] = skill

	agentData, err := read(agentSrcPath)
	if err != nil {
		return nil, err
	}
	agent, err := parseAgent(agentData)
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", agentSrcPath, err)
	}
	for _, h := range hosts {
		if h.Agent == nil {
			continue
		}
		text, err := h.Agent.render(agent)
		if err != nil {
			return nil, fmt.Errorf("agent for %s: %w", h.Name, err)
		}
		if len(text) > maxAgentBytes {
			return nil, fmt.Errorf("agent for %s is %d bytes, over the limit of %d", h.Name, len(text), maxAgentBytes)
		}
		files[h.Agent.Path] = []byte(text)
	}

	if err := addManifests(files, o, read); err != nil {
		return nil, err
	}

	for _, dir := range []string{"packs", "schemas"} {
		if err := copyDir(files, o.Src, dir); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"LICENSE", "README.md"} {
		data, err := read(name)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}

	if err := validateTree(files, o.Allowlist); err != nil {
		return nil, fmt.Errorf("the release tree is not valid:\n%w", err)
	}
	return files, nil
}

func addManifests(files map[string][]byte, o options, read func(string) ([]byte, error)) error {
	template := func(name string, wantVersion bool) (map[string]any, error) {
		data, err := read(manifestsDir + name)
		if err != nil {
			return nil, err
		}
		m, err := fromTemplate(data, o.Version, wantVersion)
		if err != nil {
			return nil, fmt.Errorf("%s%s: %w", manifestsDir, name, err)
		}
		return m, nil
	}
	put := func(p string, m map[string]any) error {
		data, err := marshalJSON(m)
		files[p] = data
		return err
	}

	for _, h := range hosts {
		if _, done := files[h.Manifest]; done {
			continue
		}
		var m map[string]any
		var err error
		switch h.Format {
		case formatClaude:
			if m, err = template("claude-plugin.json", true); err != nil {
				return err
			}
			if h.Agent != nil {
				m["agents"] = []string{"./" + h.Agent.Path}
			}
			if o.MCP != nil {
				if err := addClaudeMCP(m, *o.MCP); err != nil {
					return err
				}
			}
		case formatAgentPlugins:
			if m, err = template("agent-plugins.json", true); err != nil {
				return err
			}
		}
		if err := put(h.Manifest, m); err != nil {
			return err
		}
	}

	market, err := template("claude-marketplace.json", false)
	if err != nil {
		return err
	}
	if err := put(marketplacePath, market); err != nil {
		return err
	}

	if o.MCP != nil {
		data, err := agentPluginsMCP(*o.MCP)
		if err != nil {
			return err
		}
		files[mcpPath] = data
	}
	return nil
}

// copyDir adds every file below dir, which must hold only regular files.
func copyDir(files map[string][]byte, src fs.FS, dir string) error {
	return fs.WalkDir(src, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		files[p] = lf(data)
		return nil
	})
}

// readAllowlist reads docs/release-allowlist.txt of the repository.
func readAllowlist(src fs.FS) ([]string, error) {
	data, err := fs.ReadFile(src, "docs/release-allowlist.txt")
	if err != nil {
		return nil, err
	}
	return release.ParseAllowlist(string(data)), nil
}

// writeTree writes the files below dir, which must not exist or be empty.
// Files are written in path order, with the same modes everywhere.
func writeTree(dir string, files map[string][]byte) error {
	for p := range files {
		if !fs.ValidPath(p) || p == "." || strings.Contains(p, `\`) {
			return fmt.Errorf("%q is not a clean relative path", p)
		}
	}
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil && len(entries) > 0:
		return fmt.Errorf("%s is not empty", dir)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	for _, p := range sortedKeys(files) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil {
			return err
		}
	}
	return nil
}

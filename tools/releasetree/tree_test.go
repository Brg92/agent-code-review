package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Brg92/agent-code-review/internal/release"
)

const testVersion = "1.2.3"

// repoRoot is the module root, relative to this package's test directory.
var repoRoot = filepath.Join("..", "..")

func realAllowlist(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "release-allowlist.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return release.ParseAllowlist(string(data))
}

func realOptions(t *testing.T) options {
	return options{Src: os.DirFS(repoRoot), Version: testVersion, Allowlist: realAllowlist(t)}
}

func buildReal(t *testing.T, mutate func(*options)) map[string][]byte {
	t.Helper()
	o := realOptions(t)
	if mutate != nil {
		mutate(&o)
	}
	files, err := buildTree(o)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func parseJSON(t *testing.T, files map[string][]byte, p string) map[string]any {
	t.Helper()
	data, ok := files[p]
	if !ok {
		t.Fatalf("%s is not in the tree", p)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return m
}

func sortedPaths(files map[string][]byte) []string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func TestBuildTreeHasWhatShips(t *testing.T) {
	files := buildReal(t, nil)
	for _, p := range []string{
		"plugin.json",
		".claude-plugin/plugin.json",
		".claude-plugin/marketplace.json",
		"skills/code-review/SKILL.md",
		"agents/code-reviewer.md",
		"com.github.copilot/agents/code-reviewer.agent.md",
		"packs/android/manifest.json",
		"schemas/judgement.schema.json",
		"LICENSE",
		"README.md",
	} {
		if _, ok := files[p]; !ok {
			t.Errorf("%s is not in the tree", p)
		}
	}
	for _, p := range sortedPaths(files) {
		if strings.HasPrefix(p, "bin/") || strings.HasPrefix(p, "hooks/") {
			t.Errorf("%s: launchers and hooks are not part of this tree", p)
		}
	}
}

// splitGenerated splits a generated agent file into its frontmatter and body.
// The frontmatter of a generated file may hold a list, which the strict reader
// of the neutral source does not take.
func splitGenerated(t *testing.T, data []byte) (head, body string) {
	t.Helper()
	rest, ok := strings.CutPrefix(string(data), "---\n")
	if !ok {
		t.Fatalf("no frontmatter in %q", data)
	}
	head, body, ok = strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatalf("frontmatter not closed in %q", data)
	}
	return head, body
}

func TestAgentBodyIsIdenticalInEveryAgent(t *testing.T) {
	files := buildReal(t, nil)
	src, err := os.ReadFile(filepath.Join(pluginDir, "agents", "code-reviewer.md"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := parseAgent(src)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, h := range hosts {
		if h.Agent == nil {
			if _, ok := files[h.Name]; ok {
				t.Errorf("host %s has no agent but the tree has a file named after it", h.Name)
			}
			continue
		}
		n++
		_, body := splitGenerated(t, files[h.Agent.Path])
		if body != want.Body {
			t.Errorf("%s: body differs from plugin/agents/code-reviewer.md", h.Agent.Path)
		}
	}
	if n != 2 {
		t.Errorf("%d agents generated, want 2 (Claude Code and Copilot)", n)
	}
}

func TestSizeLimits(t *testing.T) {
	files := buildReal(t, nil)
	if n := len(files["skills/code-review/SKILL.md"]); n > maxSkillBytes {
		t.Errorf("skill is %d bytes, limit %d", n, maxSkillBytes)
	}
	for _, h := range hosts {
		if h.Agent == nil {
			continue
		}
		if n := len(files[h.Agent.Path]); n > maxAgentBytes {
			t.Errorf("%s is %d bytes, limit %d", h.Agent.Path, n, maxAgentBytes)
		}
	}
}

func TestEveryAgentUsesOnlyTheThreeCapabilities(t *testing.T) {
	files := buildReal(t, nil)
	for _, h := range hosts {
		if h.Agent == nil {
			continue
		}
		head, _ := splitGenerated(t, files[h.Agent.Path])
		toolsLine := ""
		for line := range strings.SplitSeq(head, "\n") {
			if v, ok := strings.CutPrefix(line, "tools: "); ok {
				toolsLine = v
			}
		}
		raw := strings.Trim(toolsLine, "[]")
		var tools []string
		for tool := range strings.SplitSeq(raw, ",") {
			tools = append(tools, strings.Trim(strings.TrimSpace(tool), `"`))
		}
		var want []string
		for _, c := range []capability{capShell, capRead, capWrite} {
			want = append(want, h.Agent.Tools[c])
		}
		if !reflect.DeepEqual(tools, want) {
			t.Errorf("%s: tools = %v, want the translation of shell, read, write: %v", h.Name, tools, want)
		}
	}
}

func TestVersionIsTheSameInEveryManifest(t *testing.T) {
	files := buildReal(t, nil)
	seen := 0
	for _, h := range hosts {
		m := parseJSON(t, files, h.Manifest)
		if m["version"] != testVersion {
			t.Errorf("%s: version = %v, want %s", h.Manifest, m["version"], testVersion)
		}
		seen++
	}
	if seen != len(hosts) {
		t.Fatalf("checked %d manifests", seen)
	}
	if v, has := parseJSON(t, files, ".claude-plugin/marketplace.json")["plugins"].([]any)[0].(map[string]any)["version"]; has {
		t.Errorf("the marketplace entry carries version %v", v)
	}
	for p, data := range files {
		if bytes.Contains(data, []byte(versionPlaceholder)) {
			t.Errorf("%s still has the version placeholder", p)
		}
	}
}

func TestEveryPathAManifestReferencesExists(t *testing.T) {
	files := buildReal(t, nil)
	claude := parseJSON(t, files, ".claude-plugin/plugin.json")
	agents, _ := claude["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("agents = %v, want one", agents)
	}
	for _, a := range agents {
		rel := strings.TrimPrefix(a.(string), "./")
		if _, ok := files[rel]; !ok {
			t.Errorf("manifest references %s, which is not in the tree", a)
		}
	}
	market := parseJSON(t, files, ".claude-plugin/marketplace.json")
	for _, p := range market["plugins"].([]any) {
		if src := p.(map[string]any)["source"].(string); src != "./" {
			t.Errorf("marketplace source = %q, want the plugin at the root", src)
		}
	}
}

func TestRequiredManifestFields(t *testing.T) {
	files := buildReal(t, nil)
	for _, h := range hosts {
		m := parseJSON(t, files, h.Manifest)
		required := []string{"name", "version", "description"}
		if h.Format == formatAgentPlugins {
			required = append(required, "$schema")
		}
		for _, f := range required {
			if s, _ := m[f].(string); s == "" {
				t.Errorf("%s (%s): required field %q is missing", h.Manifest, h.Name, f)
			}
		}
	}
	if owner := parseJSON(t, files, ".claude-plugin/marketplace.json")["owner"].(map[string]any); owner["name"] == "" {
		t.Error("marketplace owner.name is missing")
	}
}

func TestUnverifiedHostsAreOnlyCheckedStructurally(t *testing.T) {
	files := buildReal(t, nil)
	for _, h := range hosts {
		if h.Verified {
			continue
		}
		if h.Agent != nil {
			t.Errorf("%s is not verified and must not get an agent", h.Name)
		}
		if p := checkAgentPlugins(parseJSON(t, files, h.Manifest)); len(p) != 0 {
			t.Errorf("%s: manifest problems %v", h.Name, p)
		}
	}
}

func TestNothingOutsideTheAllowlistIsInTheTree(t *testing.T) {
	files := buildReal(t, nil)
	if bad := release.DisallowedPaths(realAllowlist(t), sortedPaths(files)); len(bad) != 0 {
		t.Errorf("outside the allowlist: %v", bad)
	}
}

func TestBuildRefusesAPathOutsideTheAllowlist(t *testing.T) {
	_, err := buildTree(options{Src: os.DirFS(repoRoot), Version: testVersion, Allowlist: []string{"plugin.json"}})
	if err == nil || !strings.Contains(err.Error(), "outside the allowlist") {
		t.Errorf("buildTree error = %v, want one about the allowlist", err)
	}
}

func TestMCPIsOffByDefault(t *testing.T) {
	files := buildReal(t, nil)
	for p := range files {
		if p == "mcp.json" || p == ".mcp.json" {
			t.Errorf("%s is in the tree", p)
		}
	}
	for _, h := range hosts {
		if _, has := parseJSON(t, files, h.Manifest)["mcpServers"]; has {
			t.Errorf("%s declares an MCP server", h.Manifest)
		}
	}
}

func TestMCPOnGivesEachHostItsShape(t *testing.T) {
	files := buildReal(t, func(o *options) {
		o.MCP = &testMCP
		o.Allowlist = append(slices.Clone(o.Allowlist), "mcp.json")
	})
	claude := parseJSON(t, files, ".claude-plugin/plugin.json")
	servers, _ := claude["mcpServers"].(map[string]any)
	if _, ok := servers["agent-code-review"]; !ok {
		t.Errorf("claude manifest mcpServers = %v", claude["mcpServers"])
	}
	file := parseJSON(t, files, "mcp.json")
	if p := checkMCPFile(file); len(p) != 0 {
		t.Errorf("mcp.json problems %v", p)
	}
	for _, h := range hosts {
		if h.Format != formatAgentPlugins {
			continue
		}
		if _, has := parseJSON(t, files, h.Manifest)["mcpServers"]; has {
			t.Errorf("%s: Agent Plugins has no manifest field for MCP", h.Name)
		}
	}
}

func TestMCPIsNeverEmittedWithTheRealAllowlist(t *testing.T) {
	o := realOptions(t)
	o.MCP = &testMCP
	if _, err := buildTree(o); err == nil || !strings.Contains(err.Error(), "mcp.json") {
		t.Errorf("buildTree error = %v, want mcp.json refused by the allowlist", err)
	}
}

func TestBuildIsDeterministicAndUsesLF(t *testing.T) {
	first := buildReal(t, nil)
	second := buildReal(t, nil)
	if !reflect.DeepEqual(first, second) {
		t.Error("two builds differ")
	}
	for p, data := range first {
		if bytes.Contains(data, []byte("\r")) {
			t.Errorf("%s has a carriage return", p)
		}
	}
}

// crlfSources is a minimal repository whose files have CRLF line endings, as a
// checkout on Windows without the repository's .gitattributes would give.
func crlfSources(t *testing.T) fstest.MapFS {
	t.Helper()
	src := fstest.MapFS{}
	for _, p := range []string{
		"plugin/skills/code-review/SKILL.md",
		"plugin/agents/code-reviewer.md",
		"plugin/manifests/agent-plugins.json",
		"plugin/manifests/claude-plugin.json",
		"plugin/manifests/claude-marketplace.json",
		"LICENSE",
		"README.md",
		"packs/p/data.json",
		"schemas/s.json",
	} {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(p)))
		if err != nil {
			data = []byte("{}\n")
		}
		src[p] = &fstest.MapFile{Data: bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))}
	}
	return src
}

func TestBuildNormalisesLineEndings(t *testing.T) {
	files, err := buildTree(options{Src: crlfSources(t), Version: testVersion, Allowlist: realAllowlist(t)})
	if err != nil {
		t.Fatal(err)
	}
	for p, data := range files {
		if bytes.Contains(data, []byte("\r")) {
			t.Errorf("%s has a carriage return", p)
		}
	}
	if !bytes.Equal(files["packs/p/data.json"], []byte("{}\n")) {
		t.Errorf("packs/p/data.json = %q", files["packs/p/data.json"])
	}
}

func TestBuildErrors(t *testing.T) {
	good := crlfSources(t)
	tests := []struct {
		name string
		mut  func(fstest.MapFS)
		opt  func(*options)
		want string
	}{
		{"bad version", nil, func(o *options) { o.Version = "v1" }, "SemVer"},
		{"no skill", func(s fstest.MapFS) { delete(s, "plugin/skills/code-review/SKILL.md") }, nil, "SKILL.md"},
		{"bad skill", func(s fstest.MapFS) { s["plugin/skills/code-review/SKILL.md"] = &fstest.MapFile{Data: []byte("x")} }, nil, "skill"},
		{"no agent", func(s fstest.MapFS) { delete(s, "plugin/agents/code-reviewer.md") }, nil, "code-reviewer.md"},
		{"bad agent", func(s fstest.MapFS) { s["plugin/agents/code-reviewer.md"] = &fstest.MapFile{Data: []byte("x")} }, nil, "agent"},
		{"no template", func(s fstest.MapFS) { delete(s, "plugin/manifests/claude-plugin.json") }, nil, "claude-plugin.json"},
		{"template without placeholder", func(s fstest.MapFS) {
			s["plugin/manifests/claude-plugin.json"] = &fstest.MapFile{Data: []byte(`{"name": "x", "version": "1.0.0"}`)}
		}, nil, "placeholder"},
		{"no licence", func(s fstest.MapFS) { delete(s, "LICENSE") }, nil, "LICENSE"},
		{"no packs", func(s fstest.MapFS) { delete(s, "packs/p/data.json") }, nil, "packs"},
		{"mcp command outside the plugin", nil, func(o *options) { o.MCP = &mcpServer{Name: "x", Command: "../x"} }, "mcp command"},
		{"agent too big", func(s fstest.MapFS) {
			s["plugin/agents/code-reviewer.md"].Data = append(s["plugin/agents/code-reviewer.md"].Data, bytes.Repeat([]byte("x"), maxAgentBytes)...)
		}, nil, "over the limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{}
			for p, f := range good {
				src[p] = &fstest.MapFile{Data: slices.Clone(f.Data)}
			}
			if tt.mut != nil {
				tt.mut(src)
			}
			o := options{Src: src, Version: testVersion, Allowlist: realAllowlist(t)}
			if tt.opt != nil {
				tt.opt(&o)
			}
			_, err := buildTree(o)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("buildTree error = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestBuildRefusesAnythingButRegularFiles(t *testing.T) {
	src := crlfSources(t)
	src["packs/link"] = &fstest.MapFile{Mode: fs.ModeSymlink, Data: []byte("p/data.json")}
	if _, err := buildTree(options{Src: src, Version: testVersion, Allowlist: realAllowlist(t)}); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("buildTree error = %v, want a refusal of the link", err)
	}
}

func TestValidateTreeReportsEveryProblem(t *testing.T) {
	files := buildReal(t, nil)
	delete(files, "skills/code-review/SKILL.md")
	delete(files, "agents/code-reviewer.md")
	files["extra.txt"] = []byte("x")
	files["plugin.json"] = []byte(`{"name": "A"}`)
	err := validateTree(files, realAllowlist(t))
	if err == nil {
		t.Fatal("validateTree = nil, want problems")
	}
	for _, want := range []string{"extra.txt", "skills/code-review/SKILL.md", "agents/code-reviewer.md", "plugin.json:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestValidateTreeNeedsOneVersion(t *testing.T) {
	files := buildReal(t, nil)
	files[".claude-plugin/plugin.json"] = bytes.ReplaceAll(files[".claude-plugin/plugin.json"], []byte(testVersion), []byte("9.9.9"))
	err := validateTree(files, realAllowlist(t))
	if err == nil || !strings.Contains(err.Error(), "versions differ") {
		t.Errorf("validateTree error = %v, want the versions to differ", err)
	}
}

func TestWriteTree(t *testing.T) {
	files := buildReal(t, nil)
	out := filepath.Join(t.TempDir(), "tree")
	if err := writeTree(out, files); err != nil {
		t.Fatal(err)
	}
	for p, want := range files {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(p)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: read back %d bytes, err %v, want %d bytes", p, len(got), err, len(want))
		}
	}

	if err := writeTree(out, files); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Errorf("writing into a tree that exists: error = %v, want a refusal", err)
	}

	empty := t.TempDir()
	if err := writeTree(empty, map[string][]byte{"a/b.txt": []byte("x\n")}); err != nil {
		t.Errorf("writing into an empty directory: %v", err)
	}
	for _, p := range []string{"../x", "/x", "a/../../x", `a\b`, ""} {
		if err := writeTree(filepath.Join(t.TempDir(), "o"), map[string][]byte{p: nil}); err == nil {
			t.Errorf("writeTree accepted the path %q", p)
		}
	}
}

func TestWrittenTreesAreIdentical(t *testing.T) {
	read := func(dir string) map[string][]byte {
		got := map[string][]byte{}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			got[filepath.ToSlash(rel)], err = os.ReadFile(p)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	a, b := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, dir := range []string{a, b} {
		if err := writeTree(dir, buildReal(t, nil)); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(read(a), read(b)) {
		t.Error("two written trees differ")
	}
}

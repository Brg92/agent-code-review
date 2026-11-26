package main

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// skillName is the one skill of the plugin.
	skillName = "code-review"

	// maxSkillBytes and maxAgentBytes keep the text an agent has to read
	// small. The agent limit applies to the source and to every generated
	// agent file.
	maxSkillBytes = 3 * 1024
	maxAgentBytes = 2 * 1024
)

// capability is what an agent may do, named for no host in particular.
type capability string

const (
	capShell capability = "shell"
	capRead  capability = "read"
	capWrite capability = "write"
)

// agentSource is the neutral agent of plugin/agents: the same body for every
// host, and the capabilities each host turns into its own tool names.
type agentSource struct {
	Name         string
	Description  string
	Capabilities []capability
	// Body is everything after the closing delimiter of the frontmatter,
	// byte for byte.
	Body string
}

type frontmatter struct {
	keys   []string
	values map[string]string
}

// splitFrontmatter separates the frontmatter of a Markdown file from its body.
// The frontmatter is a block of "key: value" lines between two --- lines.
func splitFrontmatter(text string) (frontmatter, string, error) {
	fm := frontmatter{values: map[string]string{}}
	if strings.Contains(text, "\r") {
		return fm, "", errors.New("has a carriage return, use LF line endings")
	}
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return fm, "", errors.New("must start with a --- line")
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return fm, "", errors.New("frontmatter is not closed by a --- line")
	}
	for line := range strings.SplitSeq(head, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || key == "" {
			return fm, "", fmt.Errorf("frontmatter line %q is not key: value", line)
		}
		if _, dup := fm.values[key]; dup {
			return fm, "", fmt.Errorf("duplicate key %q", key)
		}
		fm.keys = append(fm.keys, key)
		fm.values[key] = strings.TrimSpace(value)
	}
	return fm, body, nil
}

// only returns an error for the first key that is not in allowed, and for a
// required key that is missing or empty.
func (fm frontmatter) only(allowed ...string) error {
	for _, key := range fm.keys {
		known := false
		for _, a := range allowed {
			known = known || a == key
		}
		if !known {
			return fmt.Errorf("unknown key %q", key)
		}
	}
	for _, key := range allowed {
		if fm.values[key] == "" {
			return fmt.Errorf("missing %q", key)
		}
	}
	return nil
}

// parseAgent reads the neutral agent source.
func parseAgent(src []byte) (agentSource, error) {
	fm, body, err := splitFrontmatter(string(src))
	if err != nil {
		return agentSource{}, err
	}
	if err := fm.only("name", "description", "capabilities"); err != nil {
		return agentSource{}, err
	}
	caps, err := parseCapabilities(fm.values["capabilities"])
	if err != nil {
		return agentSource{}, err
	}
	return agentSource{
		Name:         fm.values["name"],
		Description:  fm.values["description"],
		Capabilities: caps,
		Body:         body,
	}, nil
}

func parseCapabilities(value string) ([]capability, error) {
	inner, ok := strings.CutPrefix(value, "[")
	if inner, ok2 := strings.CutSuffix(inner, "]"); ok && ok2 {
		value = inner
	} else {
		return nil, errors.New(`"capabilities" must be a list in brackets`)
	}
	if strings.TrimSpace(value) == "" {
		return nil, errors.New(`"capabilities" needs at least one capability`)
	}
	var caps []capability
	seen := map[capability]bool{}
	for item := range strings.SplitSeq(value, ",") {
		c := capability(strings.TrimSpace(item))
		switch c {
		case capShell, capRead, capWrite:
		default:
			return nil, fmt.Errorf("unknown capability %q", c)
		}
		if seen[c] {
			return nil, fmt.Errorf("repeats capability %q", c)
		}
		seen[c] = true
		caps = append(caps, c)
	}
	return caps, nil
}

// checkSkill checks the skill file: its size, its two frontmatter keys, its
// name and that it has a body.
func checkSkill(src []byte) error {
	if len(src) > maxSkillBytes {
		return fmt.Errorf("skill is %d bytes, over the limit of %d", len(src), maxSkillBytes)
	}
	fm, body, err := splitFrontmatter(string(src))
	if err != nil {
		return err
	}
	if err := fm.only("name", "description"); err != nil {
		return err
	}
	if fm.values["name"] != skillName {
		return fmt.Errorf("skill must be named %q, it is %q", skillName, fm.values["name"])
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("skill has no body")
	}
	return nil
}

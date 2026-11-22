package render

import (
	"fmt"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
)

// The texts that publish writes to a platform. They are in English, written
// as a reviewer writes, and say nothing of how they were made. Each ends
// with the fingerprint that finds it again, and has no trailing newline.

// InlineComment is the text of the inline comment of a finding. suggestionInfo
// is the info string of the code fence of a suggestion on the platform, or
// empty for a platform that has none, where the suggestion is a plain code
// block.
func InlineComment(f core.Finding, suggestionInfo string) string {
	var parts []string
	if f.Anchor != nil {
		if lead := f.Anchor.Lead(f.Path); lead != "" {
			parts = append(parts, lead)
		}
	}
	parts = append(parts,
		fmt.Sprintf("**%s** · `%s` — %s", f.Severity, f.Rule, strings.TrimSpace(f.Title)),
		strings.TrimSpace(f.Why),
		"**Fix:** "+strings.TrimSpace(f.Fix))
	if f.Suggestion != nil {
		parts = append(parts, suggestionBlock(*f.Suggestion, suggestionInfo))
	}
	if len(f.References) > 0 {
		var b strings.Builder
		b.WriteString("**See also:**")
		for _, r := range f.References {
			b.WriteString(fmt.Sprintf("\n- `%s` `%s:%d` on `%s`", projectName(r.Project), r.Path, r.Line, r.Ref))
		}
		parts = append(parts, b.String())
	}
	parts = append(parts, core.Embed(f.ID))
	return strings.Join(parts, "\n\n")
}

// projectName is the owner and name of a project key, as people write them.
func projectName(key string) string {
	if k, err := core.ParseProjectKey(key); err == nil {
		return k.Project
	}
	return key
}

// suggestionBlock fences a suggestion, or a plain code block where the
// platform has no suggestions.
func suggestionBlock(text, info string) string {
	if info == "" {
		return core.CodeBlock(text)
	}
	if text == "" {
		return "```" + info + "\n```"
	}
	block := core.CodeBlock(text)
	fence, _, _ := strings.Cut(block, "\n")
	return fence + info + block[len(fence):]
}

// OKNote is the one general comment publish writes: nothing blocks the
// head commit.
func OKNote(head string) string {
	return "Review completed: no blocking issues found!\n\n" + core.Embed(core.OKFingerprint(head))
}

// IssueTitle is the title of the issue of a severity of change request cr.
func IssueTitle(cr core.Key, s core.Severity) string {
	if s == core.Blocker {
		return fmt.Sprintf("Review of #%d: BLOCKER findings in existing code", cr.Number)
	}
	return fmt.Sprintf("Review of #%d: %s findings", cr.Number, s)
}

// IssueBody is the body of the issue of severity s of change request cr at
// head: the findings of the change request first, then those that were
// already in the code, each with the fingerprint of its item, and the
// fingerprint of the issue last. Both lists are in the order given.
func IssueBody(cr core.Key, head string, s core.Severity, own, existing []core.Finding) string {
	var b strings.Builder
	what := fmt.Sprintf("%s findings", s)
	if s == core.Blocker {
		what += " in existing code"
	}
	fmt.Fprintf(&b, "Review of #%d at `%s`: %s.\n", cr.Number, head8(head), what)
	if len(own) > 0 {
		b.WriteString("\n")
		for _, f := range own {
			issueItem(&b, f)
		}
	}
	if len(existing) > 0 {
		b.WriteString("\n### Existing code (not written in this change request)\n\n")
		for _, f := range existing {
			issueItem(&b, f)
		}
	}
	fp, err := core.IssueFingerprint(cr, s)
	if err != nil {
		// Only BLOCKER, MAJOR and MINOR findings are collected in issues.
		panic("render: " + err.Error())
	}
	b.WriteString("\n" + core.Embed(fp))
	return b.String()
}

// issueItem writes one occurrence of a finding as an item of an issue.
func issueItem(b *strings.Builder, f core.Finding) {
	crash := ""
	if f.Crash {
		crash = " — crash"
	}
	fmt.Fprintf(b, "- **%s** (`%s`) — `%s:%d`%s\n", strings.TrimSpace(f.Title), f.Rule, f.Path, f.Line, crash)
	b.WriteString(indentLines(strings.TrimSpace(f.Why)) + "\n")
	b.WriteString(indentLines("**Fix:** "+strings.TrimSpace(f.Fix)) + "\n")
	b.WriteString("  " + core.Embed(core.ItemFingerprint(f.ID)) + "\n")
}

// indentLines indents every line of s, so that it stays inside its list
// item.
func indentLines(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "\n")
}

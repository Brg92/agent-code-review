// Package github holds what is specific to GitHub. This file reads
// references to pull requests in descriptions; it does no I/O.
package github

import (
	"regexp"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
)

// Registry recognises GitHub pull request references:
//
//   - links, https://<host>/<owner>/<repo>/pull/N with any tab, anchor,
//     query or trailing slash, on any host; an /issues/N link is read too,
//     because it may be a tracking issue, which is reported as not a change
//     request;
//   - owner/repo#N
//   - #N, in the repository of the description.
type Registry struct{}

// Forge implements refs.Registry.
func (Registry) Forge() core.Forge { return core.GitHub }

const segment = `[A-Za-z0-9_.-]+`

var (
	urlRe       = regexp.MustCompile(`(?i)https?://([a-z0-9.-]+(?::[0-9]+)?)/(` + segment + `)/(` + segment + `)/(?:pull|issues)/([0-9]+)`)
	linkRe      = regexp.MustCompile(`\[[^\]]*\]\([^)\s]*\)`)
	repoRe      = regexp.MustCompile(`(^|[^A-Za-z0-9_./#&-])(` + segment + `)/(` + segment + `)#([0-9]+)`)
	sameRepoRe  = regexp.MustCompile(`(^|[^A-Za-z0-9_/#&])#([0-9]+)`)
	notNumberRe = regexp.MustCompile(`[A-Za-z0-9_]`)
)

// Parse implements refs.Registry.
func (Registry) Parse(line string, from core.Key) []refs.Ref {
	var out []refs.Ref
	add := func(host, project, number string, pos int, url bool) {
		k, err := core.ParseKey(string(core.GitHub) + ":" + strings.ToLower(host) + "/" + project + "#" + number)
		if err == nil {
			out = append(out, refs.Ref{Key: k, Pos: pos, URL: url})
		}
	}
	// masked is line with what was read blanked out, so that nothing is
	// read twice and the text of a link is not read as a short form.
	masked := []byte(line)
	blank := func(from, to int) {
		for i := from; i < to; i++ {
			masked[i] = ' '
		}
	}
	for _, m := range urlRe.FindAllStringSubmatchIndex(line, -1) {
		if endsInWord(line, m[1]) {
			continue
		}
		add(line[m[2]:m[3]], line[m[4]:m[5]]+"/"+line[m[6]:m[7]], line[m[8]:m[9]], m[0], true)
	}
	for _, m := range linkRe.FindAllStringIndex(line, -1) {
		blank(m[0], m[1])
	}
	for _, m := range urlRe.FindAllStringIndex(line, -1) {
		blank(m[0], m[1])
	}
	rest := string(masked)
	for _, m := range repoRe.FindAllStringSubmatchIndex(rest, -1) {
		if endsInWord(rest, m[1]) {
			continue
		}
		add(from.Host, rest[m[4]:m[5]]+"/"+rest[m[6]:m[7]], rest[m[8]:m[9]], m[4], false)
		blank(m[4], m[1])
	}
	rest = string(masked)
	for _, m := range sameRepoRe.FindAllStringSubmatchIndex(rest, -1) {
		if endsInWord(rest, m[1]) {
			continue
		}
		add(from.Host, from.Project, rest[m[4]:m[5]], m[4]-1, false)
	}
	return refs.Normalize(out)
}

// endsInWord reports whether the character at i continues a word, which
// means that a number ending at i is not a whole number.
func endsInWord(s string, i int) bool {
	return i < len(s) && notNumberRe.MatchString(s[i:i+1])
}

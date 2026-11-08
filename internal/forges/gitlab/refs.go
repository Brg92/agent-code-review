// Package gitlab holds what is specific to GitLab. It is prepared before
// the first stable release. This file reads references to merge requests
// in descriptions; it does no I/O.
package gitlab

import (
	"regexp"
	"strings"

	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/refs"
)

// Registry recognises GitLab merge request references:
//
//   - links, https://<host>/<group>/.../<project>/-/merge_requests/N with
//     any tab, anchor, query or trailing slash, on any host;
//   - group/sub/project!N
//   - project!N, in the namespace of the description's project;
//   - !N, in the project of the description.
//
// #N is an issue on GitLab and is not read.
type Registry struct{}

// Forge implements refs.Registry.
func (Registry) Forge() core.Forge { return core.GitLab }

const segment = `[A-Za-z0-9_.-]+`

var (
	urlRe      = regexp.MustCompile(`(?i)https?://([a-z0-9.-]+(?::[0-9]+)?)/(` + segment + `(?:/` + segment + `)+)/-/merge_requests/([0-9]+)`)
	linkRe     = regexp.MustCompile(`\[[^\]]*\]\([^)\s]*\)`)
	refRe      = regexp.MustCompile(`(^|[^A-Za-z0-9_./!-])(` + segment + `(?:/` + segment + `)*)?!([0-9]+)`)
	wordCharRe = regexp.MustCompile(`[A-Za-z0-9_]`)
)

// Parse implements refs.Registry.
func (Registry) Parse(line string, from core.Key) []refs.Ref {
	var out []refs.Ref
	add := func(host, project, number string, pos int, url bool) {
		k, err := core.ParseKey(string(core.GitLab) + ":" + strings.ToLower(host) + "/" + project + "#" + number)
		if err == nil {
			out = append(out, refs.Ref{Key: k, Pos: pos, URL: url})
		}
	}
	// masked is line with the links blanked out, so that nothing is read
	// twice and the text of a link is not read as a reference.
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
		add(line[m[2]:m[3]], line[m[4]:m[5]], line[m[6]:m[7]], m[0], true)
	}
	for _, m := range linkRe.FindAllStringIndex(line, -1) {
		blank(m[0], m[1])
	}
	for _, m := range urlRe.FindAllStringIndex(line, -1) {
		blank(m[0], m[1])
	}
	rest := string(masked)
	namespace := from.Project[:strings.LastIndex(from.Project, "/")]
	for _, m := range refRe.FindAllStringSubmatchIndex(rest, -1) {
		if endsInWord(rest, m[1]) {
			continue
		}
		var project string
		var pos int
		switch {
		case m[4] < 0: // !N
			project, pos = from.Project, m[6]-1
		case strings.Contains(rest[m[4]:m[5]], "/"): // group/sub/project!N
			project, pos = rest[m[4]:m[5]], m[4]
		default: // project!N
			project, pos = namespace+"/"+rest[m[4]:m[5]], m[4]
		}
		add(from.Host, project, rest[m[6]:m[7]], pos, false)
	}
	return refs.Normalize(out)
}

// endsInWord reports whether the character at i continues a word, which
// means that a number ending at i is not a whole number.
func endsInWord(s string, i int) bool {
	return i < len(s) && wordCharRe.MatchString(s[i:i+1])
}

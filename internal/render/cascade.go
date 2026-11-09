package render

import (
	"fmt"
	"strings"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
)

// Reviewed is a change request of a cascade that was reviewed: its
// findings document and the path of its report.
type Reviewed struct {
	Doc    core.Document
	Report string
}

// skipWords say why an entry of the cascade was not reviewed.
var skipWords = map[core.SkipReason]string{
	core.AlreadyDone:       "already done",
	core.NotAChangeRequest: "not a change request",
	core.NotFound:          "not found",
	core.OtherPlatform:     "other platform",
	core.OverDepthLimit:    "over depth limit",
	core.OverCountLimit:    "over count limit",
	core.Cycle:             "cycle",
	core.SelfReference:     "self-reference",
}

// counts are the numbers of a change request that its report headers
// show and its summary row repeats.
type counts struct {
	blocker, major, minor, existing, crash int
	verdict                                core.Verdict
}

func countsOf(d core.Document) counts {
	var c counts
	for _, f := range d.Findings {
		switch f.Severity {
		case core.Blocker:
			c.blocker++
		case core.Major:
			c.major++
		case core.Minor:
			c.minor++
		}
	}
	c.existing = len(d.PreExisting)
	for _, f := range d.PreExisting {
		if f.Crash {
			c.crash++
		}
	}
	c.verdict = core.ChangeRequestVerdict(d.Findings, d.PreExisting)
	return c
}

func (c counts) String() string {
	s := fmt.Sprintf("%d blocker · %d major · %d minor (+%d existing", c.blocker, c.major, c.minor, c.existing)
	if c.crash > 0 {
		s += fmt.Sprintf(", %d crash", c.crash)
	}
	return s + ")"
}

// Cascade renders the summary of a cascade: one row per reviewed change
// request in review order, what was skipped or not followed, and the
// overall verdict. Every change request of the review order needs its
// findings document.
func Cascade(res cascade.Result, reviewed []Reviewed) (string, error) {
	docs, err := inOrder(res, reviewed)
	if err != nil {
		return "", err
	}
	var skipped []core.Entry
	for _, e := range res.Entries {
		if e.Status == core.Skipped {
			skipped = append(skipped, e)
		}
	}
	var b builder
	var root core.Key
	if len(res.Entries) > 0 {
		root = res.Entries[0].CR
	}
	name := func(k core.Key) string { return refFrom(root, k) }
	b.line(fmt.Sprintf("Cascade — %s · %d reviewed · %d skipped · %d not followed", ref(root), len(res.Order), len(skipped), len(res.NotFollowed)))
	b.line("")

	verdicts := map[core.Key]core.Verdict{}
	width := 0
	for _, k := range res.Order {
		width = max(width, len(name(k)))
	}
	number := len(fmt.Sprint(len(res.Order)))
	for i, k := range res.Order {
		c := countsOf(docs[i])
		verdicts[k] = c.verdict
		b.line(fmt.Sprintf("  %*d. %-*s   %-7s   %s", number, i+1, width, name(k), c.verdict, c))
	}

	if len(skipped) > 0 {
		b.line("Skipped:")
		width := 0
		for _, e := range skipped {
			width = max(width, len(name(e.CR)))
		}
		for _, e := range skipped {
			b.line(fmt.Sprintf("  %-*s   %s", width, name(e.CR), skipWords[e.SkipReason]))
		}
	}
	if len(res.NotFollowed) > 0 {
		var refs []string
		for _, l := range res.NotFollowed {
			refs = append(refs, reference(l.Text)+" (no checkbox)")
		}
		b.line("Not followed: " + strings.Join(refs, ", "))
	}
	if len(res.Unparsed) > 0 {
		var lines []string
		for _, l := range res.Unparsed {
			lines = append(lines, `"`+strings.TrimSpace(l.Text)+`"`)
		}
		b.line("Unparsed: " + strings.Join(lines, ", "))
	}

	overall, causes, err := res.Verdict(verdicts)
	if err != nil {
		return "", err
	}
	b.line("Overall: " + overallText(overall, causes, name))
	return b.String(), nil
}

// refFrom names a change request as seen from the root of its cascade:
// <project>#<number>, with the host in front when it lives on another
// platform or host, so that two change requests never read the same.
func refFrom(root, k core.Key) string {
	if k.Forge != root.Forge || k.Host != root.Host {
		return k.Host + "/" + ref(k)
	}
	return ref(k)
}

// inOrder returns the findings document of each change request of the
// review order.
func inOrder(res cascade.Result, reviewed []Reviewed) ([]core.Document, error) {
	docs := make([]core.Document, len(res.Order))
	for i, k := range res.Order {
		found := false
		for _, r := range reviewed {
			if r.Doc.CR.Same(k) {
				docs[i], found = r.Doc, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no findings for %s", k)
		}
	}
	return docs, nil
}

// reference is the reference on a Related Changes line, without its list
// marker.
func reference(text string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(text), "-*+"))
}

// overallText is the overall verdict; an incomplete cascade names what
// was not reviewed and why.
func overallText(v core.Verdict, causes []cascade.Cause, name func(core.Key) string) string {
	if v != core.Incomplete {
		return string(v)
	}
	var missing []string
	for _, c := range causes {
		if c.Reason == cascade.Unparsed {
			missing = append(missing, fmt.Sprintf("%s line %d (unparsed)", name(c.Parent), c.Line))
			continue
		}
		missing = append(missing, fmt.Sprintf("%s (%s)", name(c.CR), skipWords[core.SkipReason(c.Reason)]))
	}
	return "INCOMPLETE — not reviewed: " + strings.Join(missing, ", ")
}

// platforms name the forges as the final message says them.
var platforms = map[core.Forge]string{core.GitHub: "GitHub", core.GitLab: "GitLab"}

// Final renders what the agent prints in chat at the end of a review: that
// nothing was written to the platform, where each report is, in review
// order, and the summary of the cascade.
func Final(res cascade.Result, reviewed []Reviewed) (string, error) {
	summary, err := Cascade(res, reviewed)
	if err != nil {
		return "", err
	}
	var b builder
	forge := core.GitHub
	if len(res.Entries) > 0 {
		forge = res.Entries[0].CR.Forge
	}
	b.line("Review completed. Nothing was written to " + platforms[forge] + ".")
	b.line("")
	b.line("Reports:")
	b.line("")
	for _, k := range res.Order {
		for _, r := range reviewed {
			if r.Doc.CR.Same(k) {
				b.line("- " + ref(k) + " — " + r.Report)
				break
			}
		}
	}
	b.line("")
	return b.String() + summary, nil
}

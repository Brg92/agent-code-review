package core

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/rules"
)

// ReviewOf rebuilds the review of a work package, which is what a judgement
// is validated and finalized against, from the work package alone and the
// loaded packs: the command that finalizes runs apart from the one that
// built the work package, and reads nothing from the platform.
//
// The changed files come from the diffs of the work package. A file shown
// whole has all its lines at head; a file shown in regions has those, and
// the text of any other line is unknown, so the end of such a file is not
// checked. The rules are those of the work package: a rule of a pack is the
// one of set, with the options of the work package and the severity the
// brief gave it; a repository rule is made from its text. A rule that no
// loaded pack defines is an error.
func ReviewOf(wp WorkPackage, set *packs.Set) (Review, error) {
	byID, err := rulesOfWorkPackage(wp, set)
	if err != nil {
		return Review{}, err
	}
	files := make([]ChangedFile, 0, len(wp.Files))
	for _, f := range wp.Files {
		d, err := NewFileDiff(f.Path, f.PreviousPath, f.Status, f.Diff)
		if err != nil {
			return Review{}, fmt.Errorf("work package: %w", err)
		}
		file := ChangedFile{Diff: d}
		if c := f.Code; c != nil {
			switch {
			case c.Whole && len(c.Regions) == 1 && c.Regions[0].Start == 1:
				file.Head = slices.Clone(c.Regions[0].Lines)
			default:
				for _, r := range c.Regions {
					file.HeadRegions = append(file.HeadRegions, Region{Start: r.Start, Lines: slices.Clone(r.Lines)})
				}
			}
		}
		files = append(files, file)
	}
	return Review{CR: wp.CR, HeadSHA: wp.HeadSHA, Files: files, Rules: byID, Signals: slices.Clone(wp.Signals)}, nil
}

func rulesOfWorkPackage(wp WorkPackage, set *packs.Set) (map[string]*rules.Rule, error) {
	catalog := map[string]*rules.Rule{}
	if set != nil {
		for _, r := range set.Rules {
			catalog[r.ID] = r
		}
	}
	out := make(map[string]*rules.Rule, len(wp.Rules))
	var missing []string
	for _, pr := range wp.Rules {
		if pr.Source == RepositorySource {
			out[pr.ID] = &rules.Rule{ID: pr.ID, Title: pr.Title, Severity: string(pr.Severity), Description: pr.Title}
			continue
		}
		r, ok := catalog[pr.ID]
		if !ok {
			missing = append(missing, fmt.Sprintf("%q of %s", pr.ID, pr.Source))
			continue
		}
		if len(pr.Options) > 0 {
			var err error
			if r, err = r.WithOptions(pr.Options); err != nil {
				return nil, fmt.Errorf("work package: %w", err)
			}
		}
		// The brief gave the rule this severity: that is what the
		// judgement is checked against.
		own := *r
		own.Severity, own.NeedsJudgement = string(pr.Severity), pr.NeedsJudgement
		out[pr.ID] = &own
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("work package: the loaded packs do not define the rules %s", strings.Join(missing, ", "))
	}
	return out, nil
}

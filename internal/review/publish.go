package review

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Brg92/agent-code-review/internal/auth"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// PublishDeps are what publish needs from the outside.
type PublishDeps struct {
	// Provider is the platform of the cascade: what publish reads to plan
	// and writes to apply.
	Provider base.ForgeProvider
	// Registries are the reference registries, as for review.
	Registries []refs.Registry
	// Now is the clock.
	Now func() time.Time
}

// Planned is what publish --plan made.
type Planned struct {
	// Text is what the command prints: the summary of each plan.
	Text string
	// Plans are the plans, in review order.
	Plans []core.Plan
	// Paths are the plan files, in the same order.
	Paths []string
}

// crView is a reviewed change request of a run, as publish sees it: its
// findings, and the change request as the platform shows it now.
type crView struct {
	loaded
	doc  core.Document
	meta core.ChangeRequest
	// block is why nothing is written for the change request, if so.
	block  core.PlanStatus
	detail string
}

func (v *crView) key() core.Key { return v.Entry.CR }

// publishable checks that the run can be planned or applied from.
func publishable(o *Opened, applying bool) error {
	switch p := o.State.Phase; {
	case p == state.Planned || p == state.Finalized && !applying:
		return nil
	case p == state.Published:
		return &Failure{Kind: KindState, What: "the run is already published", Where: o.Dir, Why: "its phase is published",
			Hint: "a published run is not planned or applied again; run `agent-code-review review <pull request>` for a new one"}
	case applying && p == state.Finalized:
		return &Failure{Kind: KindState, What: "the run has no plan", Where: o.Dir, Why: "its phase is finalized",
			Hint: "run `agent-code-review publish --plan <comments|issues|both>` first, and apply it once the user said yes"}
	default:
		return &Failure{Kind: KindState, What: "the run is not ready to be published", Where: o.Dir, Why: "its phase is " + string(p),
			Hint: "publish needs a finalized run: run `agent-code-review finalize` once every change request is judged"}
	}
}

// platformFailure turns a failed read or write into a failure of the
// command: a token that is refused or lacks a permission is an
// authentication failure, anything else a failure of the platform.
func platformFailure(err error, host, what, where string) *Failure {
	f := classify(err, host, what, where)
	if f.Kind == KindAuth && len(base.PermissionsOf(err)) == 0 && !errors.Is(err, auth.ErrNoToken) {
		f.Hint = "the token cannot do this: a fine-grained token needs Pull requests and Issues set to read and write; check it, or run " + loginCommand(host) + " to store another"
	}
	return f
}

// readViews reads, for every reviewed change request of the run in review
// order, its findings and the change request as the platform shows it now,
// and says whether anything may be written for it.
func readViews(ctx context.Context, o *Opened, d PublishDeps, f *finished, self string, log *runlog.Log) ([]*crView, error) {
	var views []*crView
	for i, l := range f.loads {
		v := &crView{loaded: l, doc: f.docs[i]}
		log.SetCR(l.Entry.CR)
		meta, err := d.Provider.ChangeRequest(ctx, l.Entry.CR)
		if err != nil {
			log.Event("read the change request", "error")
			return nil, platformFailure(err, o.State.Root.Host, "could not read "+l.Entry.CR.String(), l.Entry.CR.String())
		}
		v.meta = meta
		v.block, v.detail = blockOf(meta, v.doc.HeadSHA, self)
		log.Event("read the change request", "ok")
		views = append(views, v)
	}
	log.SetCR(core.Key{})
	return views, nil
}

// blockOf says why nothing may be written for a change request that was
// reviewed at head: it is closed or merged, it has new commits, or it was
// opened by the account of the token.
func blockOf(meta core.ChangeRequest, head, self string) (core.PlanStatus, string) {
	switch {
	case meta.State != core.StateOpen:
		return core.StatusNotOpen, "the change request is " + string(meta.State)
	case meta.HeadSHA != head:
		return core.StatusHeadChanged, fmt.Sprintf("reviewed at %s, now at %s", shortSHA(head), shortSHA(meta.HeadSHA))
	case strings.EqualFold(meta.Author, self):
		return core.StatusOwnCR, "the change request was opened by the account of the token"
	}
	return "", ""
}

func shortSHA(sha string) string { return sha[:min(8, len(sha))] }

// PlanPublish plans what to write for every reviewed change request of a
// finalized run: it reads the platform and writes only plan.json in the
// directory of each change request, and the hash of each in state.json. It
// is all or nothing: a read that fails writes no plan. When the head of
// some change request has changed, the plans are written and the failure
// of kind KindHeadChanged says which to review again, next to Planned.
func PlanPublish(ctx context.Context, o *Opened, d PublishDeps, choice core.Choice, log *runlog.Log) (*Planned, error) {
	log.SetCR(core.Key{})
	if err := publishable(o, false); err != nil {
		return nil, err
	}
	f, err := readFinished(o, d.Registries)
	if err != nil {
		return nil, err
	}
	host := o.State.Root.Host
	self, err := d.Provider.Self(ctx)
	if err != nil {
		log.Event("read the account of the token", "error")
		return nil, platformFailure(err, host, "could not read the account of the token", "token")
	}
	views, err := readViews(ctx, o, d, f, self, log)
	if err != nil {
		return nil, err
	}
	reg, err := composeRegistry(d.Registries, o.State.Root.Forge, o.State.Root.String())
	if err != nil {
		return nil, err
	}
	p := &planner{ctx: ctx, d: d, o: o, choice: choice, caps: d.Provider.Capabilities(), reg: reg, now: d.Now(), views: views, host: host, log: log}
	for _, v := range views {
		log.SetCR(v.key())
		plan, err := p.plan(v)
		if err != nil {
			log.Event("plan", "error")
			return nil, err
		}
		p.plans = append(p.plans, plan)
		log.Event("plan", "ok")
	}
	log.SetCR(core.Key{})
	return p.write(f)
}

// planner makes the plans of a run.
type planner struct {
	ctx    context.Context
	d      PublishDeps
	o      *Opened
	choice core.Choice
	caps   base.Capabilities
	reg    refs.Registry
	now    time.Time
	views  []*crView
	host   string
	log    *runlog.Log
	plans  []core.Plan
}

// view returns the view of a change request of the run.
func (p *planner) view(k core.Key) *crView {
	for _, v := range p.views {
		if v.key().Same(k) {
			return v
		}
	}
	return nil
}

// builder collects the actions and the skips of one plan.
type builder struct {
	p       *planner
	v       *crView
	actions []core.Action
	skipped []core.PlanSkip
}

func (b *builder) skip(s core.PlanSkip) { b.skipped = append(b.skipped, s) }

// admit reports whether a text may be written, and notes why not in the
// plan: it matches a secret pattern, or it is longer than the platform
// accepts. The text that matched is never kept, only the name of the
// pattern.
func (b *builder) admit(a core.Action, s core.PlanSkip) bool {
	s.Kind = a.Kind
	if names := core.ScanSecrets(a.Title + "\n" + a.Body); len(names) > 0 {
		s.Status, s.Pattern = core.StatusSecretBlocked, strings.Join(names, ",")
		b.skip(s)
		return false
	}
	if limit := b.p.caps.Writes.MaxBodyLength; limit > 0 {
		if n := utf8.RuneCountInString(a.Body); n > limit {
			s.Status, s.Detail = core.StatusTooLong, fmt.Sprintf("%d characters, the platform accepts %d", n, limit)
			b.skip(s)
			return false
		}
	}
	return true
}

// plan makes the plan of one change request.
func (p *planner) plan(v *crView) (core.Plan, error) {
	b := &builder{p: p, v: v}
	plan := core.Plan{Schema: core.PlanSchemaVersion, RunID: p.o.ID, CR: v.key(), HeadSHA: v.doc.HeadSHA, CreatedAt: p.now, Publish: p.choice}
	if v.block != "" {
		b.skip(core.PlanSkip{Status: v.block, Target: v.key().String(), Detail: v.detail})
		return b.finish(plan), nil
	}
	var notes []core.ReviewNote
	if p.choice.WantsComments() || p.choice.WantsIssues() {
		var err error
		if notes, err = p.d.Provider.Notes(p.ctx, v.key()); err != nil {
			return core.Plan{}, platformFailure(err, p.host, "could not read the comments of "+v.key().String(), v.key().String())
		}
	}
	fps := core.ReadFingerprints(notes, v.meta.Author)
	var issueActions, commentActions, noteActions []core.Action
	if p.choice.WantsIssues() {
		var err error
		if issueActions, err = p.issues(b); err != nil {
			return core.Plan{}, err
		}
	}
	if p.choice.WantsComments() {
		commentActions = p.comments(b, fps)
		var err error
		if noteActions, err = p.noteAndTick(b, fps); err != nil {
			return core.Plan{}, err
		}
	}
	if len(issueActions) > 0 {
		has, err := p.d.Provider.HasLabel(p.ctx, v.key(), core.ReviewLabel.Name)
		if err != nil {
			return core.Plan{}, platformFailure(err, p.host, "could not read the labels of "+v.key().ProjectKey(), v.key().String())
		}
		if !has {
			b.actions = append(b.actions, core.Action{Kind: core.ActionLabelCreate, Project: v.key().ProjectKey(), Label: &core.Label{
				Name: core.ReviewLabel.Name, Color: core.ReviewLabel.Color, Description: core.ReviewLabel.Description}})
		}
	}
	b.actions = append(b.actions, issueActions...)
	b.actions = append(b.actions, commentActions...)
	b.actions = append(b.actions, noteActions...)
	return b.finish(plan), nil
}

// finish numbers the actions and makes the plan.
func (b *builder) finish(plan core.Plan) core.Plan {
	plan.Actions, plan.Skipped = b.actions, b.skipped
	if plan.Actions == nil {
		plan.Actions = []core.Action{}
	}
	if plan.Skipped == nil {
		plan.Skipped = []core.PlanSkip{}
	}
	for i := range plan.Actions {
		plan.Actions[i].ID = i + 1
	}
	return plan
}

// posted reports whether a trusted fingerprint is already there, in the
// current form or the legacy one; when only an account that is not trusted
// wrote it, the plan says so and the write goes ahead.
func (b *builder) posted(fps []core.NoteFingerprint, current string, legacy []string, s core.PlanSkip) bool {
	var untrusted string
	for _, fp := range fps {
		if fp.Kind == core.FingerprintItem || (fp.Value != current && !slices.Contains(legacy, fp.Value)) {
			continue
		}
		if fp.Trusted {
			s.Status, s.Target = core.StatusAlreadyPosted, ""
			b.skip(s)
			return true
		}
		untrusted = fp.Value
	}
	if untrusted != "" {
		s.Status, s.Target, s.Detail = core.StatusIgnoredUntrusted, untrusted, "written by an account that is not trusted, so it does not count"
		b.skip(s)
	}
	return false
}

// comments plans the inline comments: one for each BLOCKER of the change
// request that has a line to be on.
func (p *planner) comments(b *builder, fps []core.NoteFingerprint) []core.Action {
	var actions []core.Action
	for _, f := range b.v.doc.Findings {
		if f.Severity != core.Blocker {
			continue
		}
		s := core.PlanSkip{Kind: core.ActionReviewComment, FindingID: f.ID, Rule: f.Rule, Path: f.Path, Line: f.Line}
		if f.Placement != core.Inline || f.Anchor == nil {
			s.Status, s.Detail = core.StatusNoAnchor, "no line of the diff can carry it; it stays in the report"
			b.skip(s)
			continue
		}
		legacy := fmt.Sprintf("mr-review:v1:%s:%s:%d", f.Rule, f.Path, f.Line)
		if b.posted(fps, f.ID, []string{legacy}, s) {
			continue
		}
		a := core.Action{Kind: core.ActionReviewComment, Severity: f.Severity, Rules: []string{f.Rule}, FindingID: f.ID,
			Body: render.InlineComment(f, p.caps.Writes.SuggestionInfo), Path: f.Path, Side: f.Anchor.Side, Line: f.Anchor.Line}
		if f.Suggestion != nil && f.StartLine > 0 && f.Anchor.Kind != core.AnchorRelocated && f.Anchor.Side == f.Side {
			a.StartLine = f.StartLine
		}
		if b.admit(a, s) {
			actions = append(actions, a)
		}
	}
	return actions
}

// issues plans the issue of each severity: a new one, or the rewrite of the
// open one that carries a trusted fingerprint of the severity.
func (p *planner) issues(b *builder) ([]core.Action, error) {
	v := b.v
	existing, err := p.d.Provider.Issues(p.ctx, v.key(), core.ReviewLabel.Name)
	if err != nil {
		return nil, platformFailure(err, p.host, "could not read the issues of "+v.key().ProjectKey(), v.key().String())
	}
	var actions []core.Action
	for _, sev := range []core.Severity{core.Blocker, core.Major, core.Minor} {
		var own []core.Finding
		if sev != core.Blocker {
			own = withSeverity(v.doc.Findings, sev)
		}
		old := withSeverity(v.doc.PreExisting, sev)
		if len(own)+len(old) == 0 {
			continue
		}
		fp, err := core.IssueFingerprint(v.key(), sev)
		if err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not name the issue of " + v.key().String(), Where: v.key().String(), Why: err.Error(), Err: err, Hint: "report it"}
		}
		body := render.IssueBody(v.key(), v.doc.HeadSHA, sev, own, old)
		var rules []string
		for _, f := range append(slices.Clone(own), old...) {
			rules = append(rules, f.Rule)
		}
		s := core.PlanSkip{Target: fp}
		var open *core.Issue
		untrusted := false
		for _, is := range existing {
			for _, f := range core.ReadFingerprints([]core.ReviewNote{is.Note()}, v.meta.Author) {
				switch {
				case f.Kind != core.FingerprintIssue || f.Value != fp:
				case !f.Trusted:
					untrusted = true
				case is.Open && open == nil:
					c := is
					open = &c
				}
			}
		}
		a := core.Action{Kind: core.ActionIssueCreate, Severity: sev, Rules: rules, Body: body, Fingerprint: fp, Project: v.key().ProjectKey(),
			Title: render.IssueTitle(v.key(), sev), Assignee: v.meta.Author}
		switch {
		case open != nil && open.Body == body:
			s.Status, s.Kind, s.Detail = core.StatusAlreadyPosted, core.ActionIssueUpdate, fmt.Sprintf("issue #%d is up to date", open.Number)
			b.skip(s)
			continue
		case open != nil:
			a.Kind, a.Number, a.Title, a.Assignee = core.ActionIssueUpdate, open.Number, open.Title, ""
		case untrusted:
			b.skip(core.PlanSkip{Status: core.StatusIgnoredUntrusted, Kind: core.ActionIssueCreate, Target: fp, Detail: "written by an account that is not trusted, so it does not count"})
		}
		if b.admit(a, s) {
			actions = append(actions, a)
		}
	}
	return actions, nil
}

// withSeverity returns the findings of a severity, in order.
func withSeverity(list []core.Finding, s core.Severity) []core.Finding {
	var out []core.Finding
	for _, f := range list {
		if f.Severity == s {
			out = append(out, f)
		}
	}
	return out
}

// noteAndTick plans the note that nothing blocks, and the tick of the
// Related Changes entry, for a change request whose verdict is OK.
func (p *planner) noteAndTick(b *builder, fps []core.NoteFingerprint) ([]core.Action, error) {
	var actions []core.Action
	v := b.v
	if v.doc.Verdict == core.OK {
		fp := core.OKFingerprint(v.doc.HeadSHA)
		legacy := "mr-review:v1:ok:" + fp[len(fp)-8:]
		s := core.PlanSkip{Kind: core.ActionSummaryComment, Target: fp}
		if !b.posted(fps, fp, []string{legacy}, s) {
			a := core.Action{Kind: core.ActionSummaryComment, Body: render.OKNote(v.doc.HeadSHA), Fingerprint: fp}
			if b.admit(a, s) {
				actions = append(actions, a)
			}
		}
	}
	if v.Entry.Parent != nil {
		tick, ok, err := p.tick(b)
		if err != nil {
			return nil, err
		}
		if ok {
			actions = append(actions, tick)
		}
	}
	return actions, nil
}

// tickBlock says why the entry of a change request cannot be ticked in its
// parent: the checks that do not need the parent's description.
func tickBlock(v *crView, parent *crView) string {
	switch {
	case parent == nil:
		return "its parent is not in the run"
	case parent.block != "":
		return fmt.Sprintf("its parent %s is %s", parent.key(), parent.block)
	case v.doc.Verdict != core.OK:
		return "its verdict is " + string(v.doc.Verdict)
	}
	return ""
}

// tick plans the tick of the entry of the change request in its parent.
func (p *planner) tick(b *builder) (core.Action, bool, error) {
	v := b.v
	parent := p.view(*v.Entry.Parent)
	notEligible := func(why string) (core.Action, bool, error) {
		b.skip(core.PlanSkip{Status: core.StatusNotEligible, Kind: core.ActionRelatedTick, Target: v.key().String(), Detail: why})
		return core.Action{}, false, nil
	}
	if why := tickBlock(v, parent); why != "" {
		return notEligible(why)
	}
	why, err := p.blockerThreads(v)
	if err != nil {
		return core.Action{}, false, err
	}
	if why != "" {
		return notEligible(why)
	}
	switch _, found := findEntry(p.reg, parent.meta.Description, parent.key(), v.key()); found {
	case entryTicked:
		return notEligible("the entry is already ticked")
	case entryMissing:
		return notEligible("the Related Changes of its parent have no checkbox for it")
	}
	parentKey, entry := parent.key(), v.key()
	return core.Action{Kind: core.ActionRelatedTick, Parent: &parentKey, Entry: &entry}, true, nil
}

// blockerThreads says whether the change request has an unresolved review
// thread that starts with a trusted comment of a BLOCKER of ours.
func (p *planner) blockerThreads(v *crView) (string, error) {
	threads, err := p.d.Provider.ReviewThreads(p.ctx, v.key())
	if err != nil {
		return "", platformFailure(err, p.host, "could not read the review threads of "+v.key().String(), v.key().String())
	}
	return unresolvedBlocker(threads, v.meta.Author), nil
}

// unresolvedBlocker is the reason a change request cannot be ticked when
// one of its review threads is unresolved and its first comment carries a
// trusted fingerprint of a finding: only BLOCKERs are inline comments.
func unresolvedBlocker(threads []core.ReviewThread, crAuthor string) string {
	n := 0
	for _, t := range threads {
		if t.Resolved {
			continue
		}
		for _, fp := range core.ParseFingerprints([]core.ReviewNote{t.Note()}, crAuthor) {
			if fp.Kind == core.FingerprintFinding {
				n++
				break
			}
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("it has %d unresolved review thread(s) of blockers", n)
}

// entryState is what findEntry says of the entry of a change request.
type entryState int

const (
	entryMissing entryState = iota
	entryOpen
	entryTicked
)

// findEntry finds the line of the Related Changes of the description of
// parent that names child: the first with an empty checkbox, else the
// first with a ticked one.
func findEntry(reg refs.Registry, description string, parent, child core.Key) (refs.RelatedEntry, entryState) {
	var ticked *refs.RelatedEntry
	for _, e := range refs.Related(description, reg, parent) {
		if !slices.ContainsFunc(e.Refs, child.Same) {
			continue
		}
		switch e.Box {
		case refs.Open:
			return e, entryOpen
		case refs.Ticked:
			if ticked == nil {
				ticked = &e
			}
		}
	}
	if ticked != nil {
		return *ticked, entryTicked
	}
	return refs.RelatedEntry{}, entryMissing
}

// write writes the plans and the hash of each, and moves the run to
// planned. The state is saved last.
func (p *planner) write(f *finished) (*Planned, error) {
	st := *p.o.State
	st.ChangeRequests = slices.Clone(p.o.State.ChangeRequests)
	out := &Planned{Plans: p.plans}
	var views []render.PlanView
	for i, v := range p.views {
		data, err := core.EncodePlan(p.plans[i])
		if err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not write the plan of " + v.key().String(), Where: v.key().String(), Why: err.Error(), Err: err, Hint: "report it"}
		}
		path := filepath.Join(v.Dir, state.PlanFile)
		if err := writeAtomic(path, data); err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not write " + state.PlanFile, Where: path, Why: err.Error(), Err: err,
				Hint: "check that the work directory can be written, then run `agent-code-review publish --plan` again"}
		}
		for j := range st.ChangeRequests {
			if st.ChangeRequests[j].Status != core.Skipped && st.ChangeRequests[j].CR.Same(v.key()) {
				st.ChangeRequests[j].PlanSHA256 = core.Hash(data)
			}
		}
		out.Paths = append(out.Paths, path)
		views = append(views, render.PlanView{CR: v.key(), Title: v.doc.Title, Head: v.doc.HeadSHA, Plan: p.plans[i], Path: path, Blocked: v.block,
			Parent: v.Entry.Parent})
	}
	if err := st.Advance(state.Planned, p.now); err != nil {
		return nil, damaged("the run cannot be planned", p.o.Dir, err.Error(), err)
	}
	if err := state.Save(p.o.Dir, &st); err != nil {
		return nil, &Failure{Kind: KindUnexpected, What: "could not save the state of the run", Where: filepath.Join(p.o.Dir, state.FileName), Why: err.Error(), Err: err,
			Hint: "check that the work directory can be written, then run `agent-code-review publish --plan` again"}
	}
	p.log.Event("plan the run", "ok")
	p.o.State = &st
	out.Text = render.PlanText(p.o.ID, p.choice, p.o.State.Forge, views)
	var changed []string
	for _, v := range p.views {
		if v.block == core.StatusHeadChanged {
			changed = append(changed, fmt.Sprintf("%s — %s", v.key(), v.detail))
		}
	}
	if len(changed) > 0 {
		return out, headChanged(changed, "planned")
	}
	return out, nil
}

// headChanged is the failure of change requests that have new commits.
func headChanged(details []string, what string) *Failure {
	return &Failure{Kind: KindHeadChanged, What: fmt.Sprintf("%s changed since %s: nothing is written for %s", plural(len(details), "change request"), what, pronoun(len(details))),
		Where: plural(len(details), "change request"), Why: "their head commit is not the one that was reviewed", Details: details,
		Hint: "run `agent-code-review review <pull request>` again; the other change requests are not affected"}
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

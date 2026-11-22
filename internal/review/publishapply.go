package review

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/render"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// Applied is what publish --apply did.
type Applied struct {
	// Text is what the command prints: for each change request what was
	// written, what was not and why, and what is left.
	Text string
	// Done is true when every action of every plan has a final outcome and
	// the run is published.
	Done bool
}

// item is a change request being applied: its plan, and the outcomes the
// actions have so far.
type item struct {
	*crView
	plan core.Plan
	hash string
	pub  core.Published
	byID map[int]core.Outcome
}

func (it *item) final(id int) bool {
	o, ok := it.byID[id]
	return ok && o.Status.Final()
}

// pending returns the actions of the kinds that have no final outcome.
func (it *item) pending(kinds ...core.ActionKind) []core.Action {
	var out []core.Action
	for _, a := range it.plan.Actions {
		if slices.Contains(kinds, a.Kind) && !it.final(a.ID) {
			out = append(out, a)
		}
	}
	return out
}

// complete reports whether every action has a final outcome.
func (it *item) complete() bool {
	for _, a := range it.plan.Actions {
		if !it.final(a.ID) {
			return false
		}
	}
	return true
}

// applier executes the stored plans of a run.
type applier struct {
	ctx   context.Context
	d     PublishDeps
	o     *Opened
	log   *runlog.Log
	host  string
	items []*item
	// failure is the first failure of the platform; once it is set nothing
	// more is tried.
	failure error
}

// ApplyPublish executes exactly the plans publish --plan stored. It checks
// every plan against the hash in state.json before it writes anything, and
// every change request again before it writes for it: one whose head changed
// is closed or is the account's own gets nothing. The outcome of each action
// is written to published.json as soon as it is known, so that running it
// again does what is left and nothing twice. The run is published only when
// every action has a final outcome.
func ApplyPublish(ctx context.Context, o *Opened, d PublishDeps, log *runlog.Log) (*Applied, error) {
	log.SetCR(core.Key{})
	if err := publishable(o, true); err != nil {
		return nil, err
	}
	f, err := readFinished(o, d.Registries)
	if err != nil {
		return nil, err
	}
	plans, hashes, err := storedPlans(o, f)
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
	a := &applier{ctx: ctx, d: d, o: o, log: log, host: host}
	for i, v := range views {
		it := &item{crView: v, plan: plans[i], hash: hashes[i], byID: map[int]core.Outcome{}}
		it.pub = core.Published{Schema: core.PlanSchemaVersion, RunID: o.ID, CR: v.key(), PlanSHA256: hashes[i], Outcomes: []core.Outcome{}}
		if err := a.resume(it); err != nil {
			return nil, err
		}
		a.items = append(a.items, it)
	}
	for _, it := range a.items {
		log.SetCR(it.key())
		if err := a.apply(it); err != nil {
			return nil, err
		}
		if a.failure != nil {
			break
		}
	}
	log.SetCR(core.Key{})
	return a.finish()
}

// storedPlans reads the plan of each change request in review order, and
// checks it against the hash in state.json. Every problem is listed; if
// there is one, nothing was written.
func storedPlans(o *Opened, f *finished) ([]core.Plan, []string, error) {
	var plans []core.Plan
	var hashes, problems []string
	for _, l := range f.loads {
		cr := l.Entry.CR.String()
		path := filepath.Join(l.Dir, state.PlanFile)
		fail := func(format string, args ...any) { problems = append(problems, cr+" — "+fmt.Sprintf(format, args...)) }
		data, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			fail("the plan %s is missing", path)
		case err != nil:
			fail("the plan %s cannot be read: %v", path, err)
		case l.Entry.PlanSHA256 == "":
			fail("state.json records no plan for it")
		case core.Hash(data) != l.Entry.PlanSHA256:
			fail("the plan %s is not the one that was made: its hash is not the one in state.json", path)
		default:
			plan, err := core.DecodePlan(data)
			switch {
			case err != nil:
				fail("the plan %s is not valid: %v", path, err)
			case !plan.CR.Same(l.Entry.CR) || plan.RunID != o.ID || plan.HeadSHA != l.Entry.HeadSHA:
				fail("the plan %s is for another change request, run or head", path)
			default:
				plans = append(plans, plan)
				hashes = append(hashes, l.Entry.PlanSHA256)
				continue
			}
		}
		plans, hashes = append(plans, core.Plan{}), append(hashes, "")
	}
	if len(problems) > 0 {
		return nil, nil, &Failure{Kind: KindState, What: "the plans are not the ones that were confirmed, so nothing was written", Where: o.Dir,
			Why: plural(len(problems), "problem"), Details: problems,
			Hint: "run `agent-code-review publish --plan <comments|issues|both>` again, and apply it once the user said yes"}
	}
	return plans, hashes, nil
}

// resume reads the outcomes an earlier apply wrote, if they are for this
// plan.
func (a *applier) resume(it *item) error {
	path := filepath.Join(it.Dir, state.PublishedFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return &Failure{Kind: KindUnexpected, What: "could not read " + state.PublishedFile, Where: path, Why: err.Error(), Err: err, Hint: "check that the work directory can be read"}
	}
	pub, err := core.DecodePublished(data)
	if err != nil || pub.PlanSHA256 != it.hash || !pub.CR.Same(it.key()) {
		// Outcomes of another plan, or that cannot be read: the actions are
		// tried again, and each looks at the platform before it writes.
		return nil
	}
	it.pub = pub
	for _, o := range pub.Outcomes {
		it.byID[o.ID] = o
	}
	return nil
}

// record stores the outcome of an action and writes published.json.
func (a *applier) record(it *item, id int, status core.PlanStatus, number int, detail string) error {
	o := core.Outcome{ID: id, Status: status, Number: number, Detail: detail}
	it.byID[id] = o
	replaced := false
	for i := range it.pub.Outcomes {
		if it.pub.Outcomes[i].ID == id {
			it.pub.Outcomes[i], replaced = o, true
		}
	}
	if !replaced {
		it.pub.Outcomes = append(it.pub.Outcomes, o)
	}
	return a.save(it)
}

func (a *applier) save(it *item) error {
	it.pub.UpdatedAt = a.d.Now()
	data, err := core.EncodePublished(it.pub)
	if err == nil {
		err = writeAtomic(filepath.Join(it.Dir, state.PublishedFile), data)
	}
	if err != nil {
		return &Failure{Kind: KindUnexpected, What: "could not write " + state.PublishedFile, Where: filepath.Join(it.Dir, state.PublishedFile), Why: err.Error(), Err: err,
			Hint: "check that the work directory can be written, then run `agent-code-review publish --apply` again: nothing is posted twice"}
	}
	return nil
}

// fail records the failure of the actions with those ids and stops the
// apply: the platform is not asked for anything else.
func (a *applier) fail(it *item, ids []int, what string, err error) error {
	detail := "the platform did not answer"
	if why := whyOf(err); why != "" {
		detail = why
	}
	for _, id := range ids {
		if rerr := a.record(it, id, core.StatusFailed, 0, detail); rerr != nil {
			return rerr
		}
	}
	a.log.Event(what, "error")
	a.failure = platformFailure(err, a.host, "could not "+what, it.key().String())
	return nil
}

func idsOf(actions []core.Action) []int {
	ids := make([]int, len(actions))
	for i, a := range actions {
		ids[i] = a.ID
	}
	return ids
}

// apply executes the actions of one change request that have no final
// outcome yet.
func (a *applier) apply(it *item) error {
	if it.block != "" {
		it.pub.Blocked = it.block
		for _, act := range it.plan.Actions {
			if !it.final(act.ID) {
				if err := a.record(it, act.ID, it.block, 0, it.detail); err != nil {
					return err
				}
			}
		}
		a.log.Event("nothing is written: "+string(it.block), "ok")
		return a.save(it)
	}
	if err := a.rescan(it); err != nil {
		return err
	}
	for _, step := range []func(*item) error{a.labels, a.issues, a.review, a.note, a.ticks} {
		if err := step(it); err != nil || a.failure != nil {
			return err
		}
	}
	return nil
}

// rescan looks again at the text of every action that has no outcome: a
// secret blocks it, as it did when the plan was made.
func (a *applier) rescan(it *item) error {
	limit := a.d.Provider.Capabilities().Writes.MaxBodyLength
	for _, act := range it.pending(core.ActionReviewComment, core.ActionSummaryComment, core.ActionIssueCreate, core.ActionIssueUpdate) {
		if names := core.ScanSecrets(act.Title + "\n" + act.Body); len(names) > 0 {
			if err := a.record(it, act.ID, core.StatusSecretBlocked, 0, "matches "+strings.Join(names, ", ")); err != nil {
				return err
			}
		} else if n := utf8.RuneCountInString(act.Body); limit > 0 && n > limit {
			if err := a.record(it, act.ID, core.StatusTooLong, 0, fmt.Sprintf("%d characters, the platform accepts %d", n, limit)); err != nil {
				return err
			}
		}
	}
	return nil
}

// labels creates the label of the issues, unless it is there.
func (a *applier) labels(it *item) error {
	for _, act := range it.pending(core.ActionLabelCreate) {
		project, _ := core.ParseProjectKey(act.Project)
		has, err := a.d.Provider.HasLabel(a.ctx, project, act.Label.Name)
		if err != nil {
			return a.fail(it, []int{act.ID}, "read the labels of "+act.Project, err)
		}
		if has {
			if err := a.record(it, act.ID, core.StatusAlreadyPosted, 0, ""); err != nil {
				return err
			}
			continue
		}
		if err := a.d.Provider.CreateLabel(a.ctx, project, *act.Label); err != nil {
			return a.fail(it, []int{act.ID}, "create the label "+act.Label.Name, err)
		}
		if err := a.record(it, act.ID, core.StatusPosted, 0, ""); err != nil {
			return err
		}
	}
	return nil
}

// issues opens or rewrites the issues. Before it writes it reads the issues
// again: one that an earlier apply opened is not opened twice.
func (a *applier) issues(it *item) error {
	todo := it.pending(core.ActionIssueCreate, core.ActionIssueUpdate)
	if len(todo) == 0 {
		return nil
	}
	project, _ := core.ParseProjectKey(todo[0].Project)
	existing, err := a.d.Provider.Issues(a.ctx, project, core.ReviewLabel.Name)
	if err != nil {
		return a.fail(it, idsOf(todo), "read the issues of "+todo[0].Project, err)
	}
	for _, act := range todo {
		var open *core.Issue
		var byNumber *core.Issue
		for _, is := range existing {
			if is.Number == act.Number {
				c := is
				byNumber = &c
			}
			for _, f := range core.ParseFingerprints([]core.ReviewNote{is.Note()}, it.meta.Author) {
				if f.Kind == core.FingerprintIssue && f.Value == act.Fingerprint && is.Open && open == nil {
					c := is
					open = &c
				}
			}
		}
		switch act.Kind {
		case core.ActionIssueUpdate:
			switch {
			case byNumber == nil || !byNumber.Open:
				err = a.record(it, act.ID, core.StatusNotEligible, act.Number, "the issue is closed or gone: plan again")
			case byNumber.Body == act.Body:
				err = a.record(it, act.ID, core.StatusAlreadyPosted, act.Number, "")
			default:
				if uerr := a.d.Provider.UpdateIssue(a.ctx, project, act.Number, act.Title, act.Body); uerr != nil {
					return a.fail(it, []int{act.ID}, fmt.Sprintf("rewrite issue #%d", act.Number), uerr)
				}
				err = a.record(it, act.ID, core.StatusPosted, act.Number, "")
			}
		default:
			if open != nil {
				err = a.record(it, act.ID, core.StatusAlreadyPosted, open.Number, "an open issue with this fingerprint is there")
				break
			}
			assignee, status := act.Assignee, core.StatusPosted
			if assignee != "" {
				ok, aerr := a.d.Provider.Assignable(a.ctx, project, assignee)
				if aerr != nil {
					return a.fail(it, []int{act.ID}, "check who can be assigned in "+act.Project, aerr)
				}
				if !ok {
					assignee, status = "", core.StatusPostedUnassigned
				}
			} else {
				status = core.StatusPostedUnassigned
			}
			n, cerr := a.d.Provider.CreateIssue(a.ctx, project, base.NewIssue{Title: act.Title, Body: act.Body, Label: core.ReviewLabel.Name, Assignee: assignee})
			if cerr != nil {
				return a.fail(it, []int{act.ID}, "open an issue in "+act.Project, cerr)
			}
			detail := ""
			if status == core.StatusPostedUnassigned {
				detail = "the author of the change request cannot be assigned in the project"
			}
			err = a.record(it, act.ID, status, n, detail)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// fingerprints reads the fingerprints in the notes of the change request,
// now, each with whether it counts.
func (a *applier) fingerprints(it *item) ([]core.NoteFingerprint, error) {
	notes, err := a.d.Provider.Notes(a.ctx, it.key())
	if err != nil {
		return nil, err
	}
	return core.ReadFingerprints(notes, it.meta.Author), nil
}

func hasTrusted(fps []core.NoteFingerprint, values ...string) bool {
	for _, fp := range fps {
		if fp.Trusted && fp.Kind != core.FingerprintItem && slices.Contains(values, fp.Value) {
			return true
		}
	}
	return false
}

// review posts the inline comments in one review, once each.
func (a *applier) review(it *item) error {
	todo := it.pending(core.ActionReviewComment)
	if len(todo) == 0 {
		return nil
	}
	fps, err := a.fingerprints(it)
	if err != nil {
		return a.fail(it, idsOf(todo), "read the comments of "+it.key().String(), err)
	}
	var send []core.Action
	for _, act := range todo {
		legacy := fmt.Sprintf("mr-review:v1:%s:%s:%d", act.Rules[0], act.Path, act.Line)
		if hasTrusted(fps, act.FindingID, legacy) {
			if err := a.record(it, act.ID, core.StatusAlreadyPosted, 0, ""); err != nil {
				return err
			}
			continue
		}
		send = append(send, act)
	}
	if len(send) == 0 {
		return nil
	}
	return a.postReview(it, send, true)
}

func reviewComments(actions []core.Action) []base.ReviewComment {
	comments := make([]base.ReviewComment, len(actions))
	for i, act := range actions {
		comments[i] = base.ReviewComment{Path: act.Path, Side: act.Side, StartLine: act.StartLine, Line: act.Line, Body: act.Body}
	}
	return comments
}

// postReview sends the comments as one review. When the platform refuses
// the position of some, it names them or it does not: those it names are
// recorded as rejected, never tried on another line, and the others are
// sent once more in a review of their own; when it does not say which, none
// was posted and all are recorded as rejected. A second refusal is handled
// the same way, without a third try.
func (a *applier) postReview(it *item, send []core.Action, first bool) error {
	err := a.d.Provider.CreateReview(a.ctx, it.key(), it.plan.HeadSHA, reviewComments(send))
	if err == nil {
		for _, act := range send {
			if err := a.record(it, act.ID, core.StatusPosted, 0, ""); err != nil {
				return err
			}
		}
		return nil
	}
	named, refused := base.RejectedOf(err)
	if !refused {
		return a.fail(it, idsOf(send), "post the review of "+it.key().String(), err)
	}
	var rest []core.Action
	for i, act := range send {
		switch {
		case named == nil:
			if rerr := a.record(it, act.ID, core.StatusRejected, 0, "the platform refused the review and did not say which comment: none was posted"); rerr != nil {
				return rerr
			}
		case slices.Contains(named, i):
			if rerr := a.record(it, act.ID, core.StatusRejected, 0, fmt.Sprintf("the platform refused line %d of %s", act.Line, act.Path)); rerr != nil {
				return rerr
			}
		default:
			rest = append(rest, act)
		}
	}
	switch {
	case len(rest) == 0:
		return nil
	case first:
		return a.postReview(it, rest, false)
	}
	for _, act := range rest {
		if rerr := a.record(it, act.ID, core.StatusFailed, 0, "the review was refused twice; apply again"); rerr != nil {
			return rerr
		}
	}
	a.failure = platformFailure(err, a.host, "post the review of "+it.key().String(), it.key().String())
	return nil
}

// note posts the note that nothing blocks, once.
func (a *applier) note(it *item) error {
	todo := it.pending(core.ActionSummaryComment)
	if len(todo) == 0 {
		return nil
	}
	fps, err := a.fingerprints(it)
	if err != nil {
		return a.fail(it, idsOf(todo), "read the comments of "+it.key().String(), err)
	}
	for _, act := range todo {
		if hasTrusted(fps, act.Fingerprint, "mr-review:v1:ok:"+act.Fingerprint[len(act.Fingerprint)-8:]) {
			if err := a.record(it, act.ID, core.StatusAlreadyPosted, 0, ""); err != nil {
				return err
			}
			continue
		}
		if err := a.d.Provider.CreateComment(a.ctx, it.key(), act.Body); err != nil {
			return a.fail(it, []int{act.ID}, "post the note on "+it.key().String(), err)
		}
		if err := a.record(it, act.ID, core.StatusPosted, 0, ""); err != nil {
			return err
		}
	}
	return nil
}

// ticks ticks the entry of the change request in the description of its
// parent, as the parent is now, if the change request is still eligible.
func (a *applier) ticks(it *item) error {
	for _, act := range it.pending(core.ActionRelatedTick) {
		notEligible := func(why string) error { return a.record(it, act.ID, core.StatusNotEligible, 0, why) }
		var parent *item
		for _, other := range a.items {
			if other.key().Same(*act.Parent) {
				parent = other
			}
		}
		if why := tickBlock(it.crView, parentView(parent)); why != "" {
			if err := notEligible(why); err != nil {
				return err
			}
			continue
		}
		threads, err := a.d.Provider.ReviewThreads(a.ctx, it.key())
		if err != nil {
			return a.fail(it, []int{act.ID}, "read the review threads of "+it.key().String(), err)
		}
		if why := unresolvedBlocker(threads, it.meta.Author); why != "" {
			if err := notEligible(why); err != nil {
				return err
			}
			continue
		}
		meta, err := a.d.Provider.ChangeRequest(a.ctx, *act.Parent)
		if err != nil {
			return a.fail(it, []int{act.ID}, "read "+act.Parent.String(), err)
		}
		reg, err := composeRegistry(a.d.Registries, a.o.State.Root.Forge, a.o.State.Root.String())
		if err != nil {
			return err
		}
		entry, st := findEntry(reg, meta.Description, *act.Parent, it.key())
		switch st {
		case entryTicked:
			err = a.record(it, act.ID, core.StatusAlreadyPosted, 0, "the entry is already ticked")
		case entryMissing:
			err = notEligible("the Related Changes of its parent have no checkbox for it")
		default:
			description, terr := cascade.Tick(meta.Description, entry)
			if terr != nil {
				err = notEligible(terr.Error())
				break
			}
			if uerr := a.d.Provider.UpdateDescription(a.ctx, *act.Parent, description); uerr != nil {
				return a.fail(it, []int{act.ID}, "tick the entry in "+act.Parent.String(), uerr)
			}
			err = a.record(it, act.ID, core.StatusPosted, 0, "")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func parentView(p *item) *crView {
	if p == nil {
		return nil
	}
	return p.crView
}

// finish decides whether the run is published, and says what was done.
func (a *applier) finish() (*Applied, error) {
	done := a.failure == nil
	var views []render.ApplyView
	var changed []string
	for _, it := range a.items {
		if !it.complete() {
			done = false
		}
		views = append(views, render.ApplyView{CR: it.key(), Title: it.doc.Title, Head: it.doc.HeadSHA, Plan: it.plan, Outcomes: it.byID, Blocked: it.block, Detail: it.detail})
		if it.block == core.StatusHeadChanged {
			changed = append(changed, fmt.Sprintf("%s — %s", it.key(), it.detail))
		}
	}
	if done {
		st := *a.o.State
		st.ChangeRequests = slices.Clone(a.o.State.ChangeRequests)
		if err := st.Advance(state.Published, a.d.Now()); err != nil {
			return nil, damaged("the run cannot be published", a.o.Dir, err.Error(), err)
		}
		if err := state.Save(a.o.Dir, &st); err != nil {
			return nil, &Failure{Kind: KindUnexpected, What: "could not save the state of the run", Where: filepath.Join(a.o.Dir, state.FileName), Why: err.Error(), Err: err,
				Hint: "check that the work directory can be written, then run `agent-code-review publish --apply` again: nothing is posted twice"}
		}
		a.o.State = &st
		a.log.Event("publish the run", "ok")
	}
	out := &Applied{Done: done, Text: render.ApplyText(a.o.ID, a.o.State.Forge, views, done)}
	switch {
	case a.failure != nil:
		return out, a.failure
	case len(changed) > 0:
		return out, headChanged(changed, "they were reviewed")
	}
	return out, nil
}

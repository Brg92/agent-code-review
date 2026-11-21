// Package review runs the commands of a review on the core: Collect
// resolves the cascade of a change request and collects every change
// request of it, up to the collected phase, and Next serves the agent the
// next part to judge. It reads through a base.Reader and writes only the
// run's work directory; it never writes to a platform.
package review

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Brg92/agent-code-review/internal/cascade"
	"github.com/Brg92/agent-code-review/internal/core"
	"github.com/Brg92/agent-code-review/internal/forges/base"
	"github.com/Brg92/agent-code-review/internal/packs"
	"github.com/Brg92/agent-code-review/internal/refs"
	"github.com/Brg92/agent-code-review/internal/runlog"
	"github.com/Brg92/agent-code-review/internal/state"
)

// Deps are what a review needs from the outside. The command line builds
// them; tests fill them with replays and fakes.
type Deps struct {
	// Reader reads the platform of the root.
	Reader base.Reader
	// Registries are the reference registries of every platform: the one
	// of the root's platform reads its Related Changes, the others only
	// recognise links to be skipped as other-platform.
	Registries []refs.Registry
	// Packs are the loaded packs.
	Packs *packs.Set
	// WorkDir is the base of the work directories.
	WorkDir string
	// KeepDays is how many days old runs are kept.
	KeepDays int
	// Now is the clock.
	Now func() time.Time
	// Random gives the run id its random part; nil is crypto/rand.
	Random io.Reader
	// Log is the run log of the command; its transport events are already
	// wired to it by whoever built Reader.
	Log *runlog.Log
}

// Package is the work package of one change request of a run.
type Package struct {
	CR    core.Key
	Title string
	// Dir is the directory of the change request in the run's work
	// directory, File its work package, Judgement where its judgement is
	// written.
	Dir, File, Judgement string
	// Parts is how many parts the work package is served in.
	Parts int
	// NotVerified is how many files could not be checked.
	NotVerified int
}

// Run is a collected review.
type Run struct {
	ID string
	// Dir is the work directory of the run.
	Dir   string
	State *state.State
	// Cascade is the resolved cascade: skipped entries, causes and lines
	// not followed.
	Cascade cascade.Result
	// Packages are the work packages in review order: related change
	// requests first, the root last.
	Packages []Package
}

// Collect resolves the cascade of root and collects every change request in
// it: the criteria of its target branch, the packs, the rules, the signals
// and the checks of its head become its work package, written in the run's
// work directory. The run ends in phase collected. It starts by removing
// the old runs the user configuration asks to.
//
// A read that fails because of the platform, the token or the root being
// unreadable stops the review with a *Failure. A read that the core turns
// into Not verified (the tree, a search, the checks, an invalid criteria
// file) does not: it is listed in the work package.
func Collect(ctx context.Context, d Deps, root core.Key) (*Run, error) {
	d.Log.SetCommand("review")
	c := &collector{d: d, root: root, metas: map[string]core.ChangeRequest{}, changes: map[string][]core.FileChange{}, analyses: map[string]*analysis{}, gone: map[string]bool{}}
	run, err := c.run(ctx)
	if err != nil {
		var f *Failure
		if errors.As(err, &f) {
			f.LogPath = d.Log.Path()
			return nil, f
		}
		return nil, &Failure{Kind: KindUnexpected, What: "the review failed", Why: err.Error(), Hint: "run it again; if it fails the same way, report it with the run log", LogPath: d.Log.Path(), Err: err}
	}
	return run, nil
}

type collector struct {
	d    Deps
	root core.Key
	// metas are the change requests read, by key.
	metas    map[string]core.ChangeRequest
	changes  map[string][]core.FileChange
	analyses map[string]*analysis
	// gone are the related change requests that cannot be read, and
	// written the directories the attempt in progress made.
	gone    map[string]bool
	written []string
	runDir  string
	runID   string
	entries []core.Entry
}

func (c *collector) host() string { return c.root.Host }

func (c *collector) registry() (refs.Registry, error) {
	return composeRegistry(c.d.Registries, c.root.Forge, c.root.String())
}

// composeRegistry makes the registry of the platform of a cascade: the one
// of its forge reads the Related Changes, the others only recognise links
// to be skipped.
func composeRegistry(registries []refs.Registry, forge core.Forge, where string) (refs.Registry, error) {
	var primary refs.Registry
	var others []refs.Registry
	for _, r := range registries {
		if primary == nil && r.Forge() == forge {
			primary = r
			continue
		}
		others = append(others, r)
	}
	if primary == nil {
		return nil, &Failure{Kind: KindUsage, What: "the platform of the change request is not supported", Where: where,
			Why: "no reference registry for " + string(forge), Hint: "give a pull request of a platform this release reads"}
	}
	return refs.Compose(primary, others...), nil
}

func (c *collector) run(ctx context.Context) (*Run, error) {
	reg, err := c.registry()
	if err != nil {
		return nil, err
	}
	now := c.d.Now()
	if c.runID, err = state.NewRunID(now, c.random()); err != nil {
		return nil, err
	}
	if c.runDir, err = state.CreateRunDir(c.d.WorkDir, c.runID); err != nil {
		return nil, err
	}
	if err := c.d.Log.Attach(c.runDir); err != nil {
		return nil, err
	}
	if cleaned := state.CleanRuns(c.d.WorkDir, c.d.KeepDays, now, c.runID); len(cleaned.Errors) > 0 {
		c.d.Log.Event("remove old runs", "error")
	}

	// The root first: the criteria of its target branch may set the
	// limits of the cascade.
	rootMeta, err := c.meta(ctx, c.root, "the root change request")
	if err != nil {
		return nil, err
	}
	rootCrit, err := c.criteria(ctx, rootMeta)
	if err != nil {
		return nil, err
	}
	limits := rootCrit.Cascade.Limits()
	if err := limits.Validate(); err != nil {
		limits = core.DefaultLimits()
	}

	// A related change request that cannot be read is skipped as not-found
	// and the cascade is resolved again without it, so that what it listed
	// and the count of the change requests reviewed stay consistent.
	for {
		run, err := c.attempt(ctx, reg, limits, rootCrit, now)
		var gone *unreadableError
		if !errors.As(err, &gone) {
			return run, err
		}
		c.drop(gone)
	}
}

// drop records a related change request that cannot be read, and throws
// away what the attempt that met it wrote.
func (c *collector) drop(gone *unreadableError) {
	c.gone[lower(gone.key)] = true
	c.d.Log.SetCR(gone.key)
	c.d.Log.NotVerified("read the change request", base.ReasonOf(gone.err))
	for _, dir := range c.written {
		_ = os.RemoveAll(dir)
	}
	c.written = nil
	c.analyses = map[string]*analysis{}
}

// unreadableError is a related change request the platform says is gone or
// that the token cannot read. The root is never one: it stops the review.
type unreadableError struct {
	key core.Key
	err error
}

func (e *unreadableError) Error() string {
	return "cannot read " + e.key.String() + ": " + e.err.Error()
}

func (e *unreadableError) Unwrap() error { return e.err }

// skipGone is the cascade source with the change requests that cannot be
// read left out: they are nothing, as the cascade defines it.
type skipGone struct {
	src  cascade.Source
	gone map[string]bool
}

func (s skipGone) Get(ctx context.Context, k core.Key) (cascade.Item, error) {
	if s.gone[lower(k)] {
		return cascade.Item{Kind: cascade.KindNotFound}, nil
	}
	return s.src.Get(ctx, k)
}

// attempt resolves the cascade and collects every change request in it.
func (c *collector) attempt(ctx context.Context, reg refs.Registry, limits core.Limits, rootCrit core.RepoCriteria, now time.Time) (*Run, error) {
	c.d.Log.SetCR(core.Key{})
	res, err := cascade.Resolve(ctx, skipGone{c.d.Reader.Changes(), c.gone}, reg, c.root, limits)
	if err != nil {
		return nil, classify(err, c.host(), "could not resolve the cascade of "+c.root.String(), c.root.String()+", Related Changes")
	}
	c.entries = slices.Clone(res.Entries)
	for i, e := range c.entries {
		if e.Status == core.Skipped {
			continue
		}
		m, err := c.meta(ctx, e.CR, "a change request of the cascade")
		if err != nil {
			return nil, err
		}
		c.entries[i].HeadSHA, c.entries[i].Title = m.HeadSHA, m.Title
	}
	st, err := state.New(c.runID, c.root, limits, c.entries, now)
	if err != nil {
		return nil, err
	}
	if err := state.Save(c.runDir, st); err != nil {
		return nil, err
	}

	run := &Run{ID: c.runID, Dir: c.runDir, State: st, Cascade: res}
	byKey := map[string]Package{}
	for i, e := range c.entries {
		if e.Status == core.Skipped {
			continue
		}
		pkg, err := c.collectOne(ctx, e, rootCritFor(e.CR, c.root, rootCrit))
		if err != nil {
			return nil, err
		}
		byKey[strings.ToLower(e.CR.String())] = pkg
		st.ChangeRequests[i].Status = core.Collected
	}
	for _, k := range res.Order {
		run.Packages = append(run.Packages, byKey[strings.ToLower(k.String())])
	}
	c.d.Log.SetCR(core.Key{})
	if err := st.Advance(state.Collected, c.d.Now()); err != nil {
		return nil, err
	}
	if err := state.Save(c.runDir, st); err != nil {
		return nil, err
	}
	return run, nil
}

// rootCritFor returns the criteria already read for the root, so that they
// are not read twice; nil for any other change request.
func rootCritFor(k, root core.Key, crit core.RepoCriteria) *core.RepoCriteria {
	if k.Same(root) {
		return &crit
	}
	return nil
}

func (c *collector) random() io.Reader {
	if c.d.Random != nil {
		return c.d.Random
	}
	return rand.Reader
}

// meta reads the metadata of a change request once.
func (c *collector) meta(ctx context.Context, k core.Key, what string) (core.ChangeRequest, error) {
	id := strings.ToLower(k.String())
	if m, ok := c.metas[id]; ok {
		return m, nil
	}
	c.d.Log.SetCR(k)
	m, err := c.d.Reader.ChangeRequest(ctx, k)
	if err != nil {
		if c.unreadable(k, err) {
			return core.ChangeRequest{}, &unreadableError{key: k, err: err}
		}
		return core.ChangeRequest{}, classify(err, c.host(), "could not read "+what+" "+k.String(), k.String()+", read the change request")
	}
	c.metas[id] = m
	return m, nil
}

// unreadable reports whether err says that the related change request k is
// gone or cannot be read by this token, which skips it. Any other failure,
// and any failure of the root, stops the review.
func (c *collector) unreadable(k core.Key, err error) bool {
	return !k.Same(c.root) && (errors.Is(err, base.ErrNotFound) || errors.Is(err, base.ErrForbidden))
}

// writeFile writes data to path for the user only.
func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

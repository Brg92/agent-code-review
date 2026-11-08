package cascade

import (
	"fmt"

	"github.com/Brg92/agent-code-review/internal/core"
)

// Blocked is the Reason of the cause of a reviewed change request that is
// BLOCKED.
const Blocked = "blocked"

// Verdict decides the overall verdict of the cascade from the verdict of
// each reviewed change request: BLOCKED when any of them is BLOCKED;
// otherwise INCOMPLETE when part of the cascade was not reviewed (see
// Causes); otherwise OK. The causes list the blocked change requests in
// review order, then the reasons the cascade is incomplete, which a
// BLOCKED cascade reports too. Every reviewed change request needs a
// verdict, BLOCKED or OK.
func (r Result) Verdict(verdicts map[core.Key]core.Verdict) (core.Verdict, []Cause, error) {
	var blocked []Cause
	for _, cr := range r.Order {
		v, ok := verdictOf(verdicts, cr)
		switch {
		case !ok:
			return "", nil, fmt.Errorf("no verdict for %s", cr)
		case v == core.Blocked:
			blocked = append(blocked, Cause{Reason: Blocked, CR: cr})
		case v != core.OK:
			return "", nil, fmt.Errorf("%s: %q is not the verdict of a change request", cr, string(v))
		}
	}
	incomplete := r.Causes()
	causes := append(blocked, incomplete...)
	switch {
	case len(blocked) > 0:
		return core.Blocked, causes, nil
	case len(incomplete) > 0:
		return core.Incomplete, causes, nil
	}
	return core.OK, nil, nil
}

// verdictOf looks a change request up the way platforms compare keys.
func verdictOf(verdicts map[core.Key]core.Verdict, cr core.Key) (core.Verdict, bool) {
	if v, ok := verdicts[cr]; ok {
		return v, true
	}
	for k, v := range verdicts {
		if k.Same(cr) {
			return v, true
		}
	}
	return "", false
}

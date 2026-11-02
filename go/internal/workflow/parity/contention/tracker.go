// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"fmt"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Trigger names one REMOVAL_CONTENTION_SPEC.md §7 abort trigger.
type Trigger string

// Abort triggers. The first four are the grant's own (§7 first sentence);
// the rest are the §7 list and the DP-A window additions.
const (
	TriggerDualClaim         Trigger = "dual_claim"
	TriggerFencingRegression Trigger = "fencing_regression"
	TriggerLostItem          Trigger = "lost_item"
	TriggerNonSynthetic      Trigger = "non_synthetic_id_touched"
	TriggerExactlyOnce       Trigger = "exactly_once"
	TriggerStaleWriteAccept  Trigger = "stale_write_accepted"
	TriggerResidue           Trigger = "residue_nonzero"
	TriggerDeadlock          Trigger = "deadlock_detected"
	TriggerLockWait          Trigger = "lock_wait_above_bound"
	TriggerHeadroom          Trigger = "pg_headroom"
	TriggerWallClock         Trigger = "wall_clock_cap"
	TriggerExternal          Trigger = "external_abort"
	// TriggerQueueDepth fires when synthetic rows exceed the planned
	// maximum: the run never grows the queue past what it seeded.
	TriggerQueueDepth Trigger = "queue_depth_cap"
)

// Violation is one detected abort condition.
type Violation struct {
	Trigger Trigger   `json:"trigger"`
	Detail  string    `json:"detail"`
	At      time.Time `json:"at"`
}

func violation(trigger Trigger, format string, args ...any) *Violation {
	return &Violation{Trigger: trigger, Detail: fmt.Sprintf(format, args...), At: time.Now().UTC()}
}

// endKind is how a claim epoch ended, as acknowledged by the backend.
type endKind int

const (
	endComplete endKind = iota
	endRelease
	endFailRetryable
	endFailTerminal
)

func (k endKind) terminal() bool { return k == endComplete || k == endFailTerminal }

// epoch is one granted claim as the client saw it. Times are client
// observations: reqStart is when the claim request left, respAt when the
// grant arrived, endedAt when the ending call left (the earliest instant
// the server could have applied it).
type epoch struct {
	token     int64
	reqStart  time.Time
	respAt    time.Time
	expiresAt time.Time
	endedAt   time.Time
	ended     bool
	abandoned bool
	// rejected marks an epoch whose own holder was fenced away while its
	// lease was still live: the client never released it.
	rejected bool
}

type itemState struct {
	epochs        []epoch
	terminalToken int64
	terminalAt    time.Time
}

// tracker is the client-side detector. It orders events only by
// happens-before (a response that arrived before a request left), so
// concurrent responses never produce a false regression or dual claim. The
// database sampler is the second, authoritative detector for the same
// properties.
type tracker struct {
	lease time.Duration
	skew  time.Duration

	mu       sync.Mutex
	items    map[string]*itemState
	terminal int
}

// newTracker builds a tracker for one leg. skew is the clock tolerance
// between harness and server on a lease boundary; local runs share a clock.
func newTracker(lease, skew time.Duration) *tracker {
	return &tracker{lease: lease, skew: skew, items: map[string]*itemState{}}
}

func (t *tracker) state(item string) *itemState {
	st, ok := t.items[item]
	if !ok {
		st = &itemState{}
		t.items[item] = st
	}
	return st
}

// onGrant records one issued claim and returns the first violation it
// proves: a non-synthetic id, a grant after an acknowledged terminal, a
// token not above every token granted before this request left, or a grant
// that landed while an earlier epoch was provably still held.
func (t *tracker) onGrant(item workflow.WorkItem, claim workflow.Claim, reqStart, respAt time.Time) *Violation {
	for _, id := range []string{item.WorkItemID, item.RunID, item.ScopeID, claim.ClaimID, claim.WorkItemID} {
		if !IsSynthetic(id) {
			return violation(TriggerNonSynthetic, "grant returned non-synthetic id %q (item %q)", id, item.WorkItemID)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(item.WorkItemID)
	if st.terminalToken > 0 && st.terminalAt.Before(reqStart) {
		return violation(TriggerExactlyOnce, "item %s granted token %d after terminal ack at token %d",
			item.WorkItemID, claim.FencingToken, st.terminalToken)
	}
	next := epoch{token: claim.FencingToken, reqStart: reqStart, respAt: respAt, expiresAt: reqStart.Add(t.lease)}
	for _, prior := range st.epochs {
		if prior.token == claim.FencingToken {
			return violation(TriggerFencingRegression, "item %s granted duplicate fencing token %d",
				item.WorkItemID, claim.FencingToken)
		}
		if prior.respAt.Before(reqStart) && prior.token > claim.FencingToken {
			return violation(TriggerFencingRegression, "item %s granted token %d after token %d was already issued",
				item.WorkItemID, claim.FencingToken, prior.token)
		}
		if v := t.overlap(item.WorkItemID, prior, next); v != nil {
			return v
		}
	}
	st.epochs = append(st.epochs, next)
	return nil
}

// overlap proves a dual claim between an earlier epoch held and a later
// grant: the later request left after held was granted, the later grant
// arrived while held's lease was live (so no reaper could have expired it),
// and held had not ended by then (it was abandoned, or its ending call left
// only after the later grant arrived). An epoch whose ending call is still
// unknown is checked again from onEnd.
func (t *tracker) overlap(item string, held, later epoch) *Violation {
	if !held.respAt.Before(later.reqStart) {
		return nil
	}
	if !later.respAt.Before(held.expiresAt.Add(-t.skew)) {
		return nil
	}
	switch {
	case held.abandoned, held.rejected:
	case held.ended && held.endedAt.After(later.respAt):
	default:
		return nil
	}
	return violation(TriggerDualClaim, "item %s granted token %d while token %d still held its lease until %s",
		item, later.token, held.token, held.expiresAt.Format(time.RFC3339Nano))
}

// onAbandon marks an epoch the worker stopped heartbeating on purpose (§5
// step 4): it never ends, so only the reaper may free it.
func (t *tracker) onAbandon(item string, token int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(item)
	for i := range st.epochs {
		if st.epochs[i].token == token {
			st.epochs[i].abandoned = true
		}
	}
}

// onRejected records a fence rejection of the holder's own write on an
// epoch the client never released. If the write left before the lease ran
// out (less the skew), the server took a live lease away: any grant that
// landed inside that lease is a dual claim, and the check runs against
// every grant already seen and every later one.
func (t *tracker) onRejected(item string, token int64, reqStart time.Time) *Violation {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(item)
	for i := range st.epochs {
		e := &st.epochs[i]
		if e.token != token || e.ended || !reqStart.Before(e.expiresAt.Add(-t.skew)) {
			continue
		}
		e.rejected = true
		for _, other := range st.epochs {
			if v := t.overlap(item, *e, other); v != nil {
				return v
			}
		}
	}
	return nil
}

// onRenew extends the client's view of a lease after an acknowledged
// heartbeat observed at observedAt.
func (t *tracker) onRenew(item string, token int64, observedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(item)
	for i := range st.epochs {
		if st.epochs[i].token == token {
			if exp := observedAt.Add(t.lease); exp.After(st.epochs[i].expiresAt) {
				st.epochs[i].expiresAt = exp
			}
		}
	}
}

// onEnd records an acknowledged ending mutation. A second terminal ack for
// the same item under a different token breaks exactly-once; an identical
// replay (same token) is idempotent and allowed.
func (t *tracker) onEnd(item string, token int64, kind endKind, reqStart, respAt time.Time) *Violation {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.state(item)
	for i := range st.epochs {
		if st.epochs[i].token != token || st.epochs[i].ended {
			continue
		}
		st.epochs[i].ended = true
		st.epochs[i].endedAt = reqStart
		for _, other := range st.epochs {
			if v := t.overlap(item, st.epochs[i], other); v != nil {
				return v
			}
		}
	}
	if !kind.terminal() {
		return nil
	}
	if st.terminalToken > 0 {
		if st.terminalToken == token {
			return nil
		}
		return violation(TriggerExactlyOnce, "item %s acknowledged terminal twice (tokens %d and %d)",
			item, st.terminalToken, token)
	}
	st.terminalToken = token
	st.terminalAt = respAt
	t.terminal++
	return nil
}

// hasNewerGrant reports whether the item was re-granted above token, the
// condition that arms a §5 step-9 stale write.
func (t *tracker) hasNewerGrant(item string, token int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.items[item]
	if !ok {
		return false
	}
	for _, e := range st.epochs {
		if e.token > token {
			return true
		}
	}
	return false
}

// isTerminal reports whether the client holds a terminal ack for item.
func (t *tracker) isTerminal(item string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.items[item]
	return ok && st.terminalToken > 0
}

// terminalCount is the number of items with an acknowledged terminal.
func (t *tracker) terminalCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.terminal
}

// grantedScopes counts grants per scope id; used for the starvation check.
type scopeGrants struct {
	mu     sync.Mutex
	counts map[string]int
}

func (s *scopeGrants) add(scopeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[scopeID]++
}

func (s *scopeGrants) snapshot() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.counts))
	for k, v := range s.counts {
		out[k] = v
	}
	return out
}

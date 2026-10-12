// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import "strings"

// The two reconcile reasons that may skip the parse (ruling a9-143 D1). They
// equal the git collector's reason constants; a test in that package pins the
// equality. Every other reason re-reads the repository: an explicit reindex
// stores a fresh parse (a9-125), a stale re-projection request stores
// (a9-117 Q3), and the remaining reasons have no active full to re-project.
const (
	// ReasonIntervalElapsed is the daily sweep reason.
	ReasonIntervalElapsed = "interval_elapsed"
	// ReasonGraphDirty is the uncovered-graph-writer repair reason (#7389).
	ReasonGraphDirty = "graph_dirty"
)

// Outcome is the bounded result of one pre-parse decision; it is the label
// value of eshu_dp_collector_reconcile_parse_skip_decisions_total.
type Outcome string

// The closed outcome set. Decide returns the first twelve. Run replaces
// OutcomeSkipped with the store's answer: OutcomeSkipped (a real re-open) and
// OutcomeReprojectionPending (a re-open already in flight) do not parse; every
// other outcome parses as before #7978's follow-up.
const (
	// OutcomeSkipped: identity equal at the active commit; the parse is skipped
	// and the active generation is re-projected.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeReasonIneligible: the reconcile reason must parse (D1).
	OutcomeReasonIneligible Outcome = "reason_ineligible"
	// OutcomeDisabled: the valve is 0, so the scope always parses.
	OutcomeDisabled Outcome = "disabled"
	// OutcomeNoActiveFull: the scope has no active generation.
	OutcomeNoActiveFull Outcome = "no_active_full"
	// OutcomeDeltaActive: the active generation is a delta; a full snapshot
	// never equals it, so the store stores a full (parity with #7978).
	OutcomeDeltaActive Outcome = "delta_active"
	// OutcomeHeadMoved: the remote head is not the active generation's commit.
	OutcomeHeadMoved Outcome = "head_moved"
	// OutcomeIdentityAbsent: no stored or no current identity, for example the
	// first sweep after deploy; the parse stamps one.
	OutcomeIdentityAbsent Outcome = "identity_absent"
	// OutcomeIdentityMismatch: the content version or the configuration moved.
	OutcomeIdentityMismatch Outcome = "identity_mismatch"
	// OutcomeSubmodules: the repository has .gitmodules; it always parses.
	OutcomeSubmodules Outcome = "submodules_present"
	// OutcomeDirtyTree: the managed checkout has untracked or modified files.
	OutcomeDirtyTree Outcome = "dirty_tree"
	// OutcomeValveDue: the scope skipped MaxSkips-1 times; this interval parses.
	OutcomeValveDue Outcome = "valve_due"
	// OutcomeReadError: the tree check failed, the store call failed, or the
	// store returned a reason Run does not know; parse to stay correct.
	OutcomeReadError Outcome = "read_error"
	// OutcomeReprojectionPending: the store found the active generation's
	// re-projection already open or leased (reconcile_reprojection_pending), so
	// nothing new was stamped and the collector does not parse (review F-3).
	OutcomeReprojectionPending Outcome = "reprojection_pending"
	// OutcomeReprojectionSuperseded: the store's fence refused the re-open
	// (reconcile_superseded: another generation is active, the active one is a
	// delta, or the commit or identity changed), so the collector parses in the
	// same cycle (review F-3).
	OutcomeReprojectionSuperseded Outcome = "reprojection_superseded"
)

// Outcomes returns the closed outcome set in a stable order.
func Outcomes() []Outcome {
	return []Outcome{
		OutcomeSkipped, OutcomeReasonIneligible, OutcomeDisabled, OutcomeNoActiveFull,
		OutcomeDeltaActive, OutcomeHeadMoved, OutcomeIdentityAbsent, OutcomeIdentityMismatch,
		OutcomeSubmodules, OutcomeDirtyTree, OutcomeValveDue, OutcomeReadError,
		OutcomeReprojectionPending, OutcomeReprojectionSuperseded,
	}
}

// ActiveGeneration is what the decision needs to know about the scope's
// active generation (the ingestion_scopes.active_generation_id row).
type ActiveGeneration struct {
	// Present reports an active generation exists.
	Present bool
	// GenerationID is the active generation id.
	GenerationID string
	// CommitSHA is its source commit.
	CommitSHA string
	// Identity is its stored pre-parse identity; empty when none was stamped.
	Identity string
	// IsDelta reports a delta generation.
	IsDelta bool
	// Skips counts consecutive skipped-parse re-projections since the last
	// parse of this generation's content.
	Skips int
}

// Input is one forced reconcile, read after the fetch.
type Input struct {
	// Reason is the bounded reconcile reason.
	Reason string
	// Active is the scope's active generation.
	Active ActiveGeneration
	// RemoteCommit is the fetched default-branch head.
	RemoteCommit string
	// Identity is the identity of a snapshot of RemoteCommit now.
	Identity string
	// HasSubmodules reports a .gitmodules file in the checkout.
	HasSubmodules bool
	// MaxSkips is the valve: 0 never skips; N > 0 parses at least every N
	// intervals (N-1 skips after each parse).
	MaxSkips int
}

// Decide returns whether a forced reconcile may skip the parse. The rules run
// cheapest first; clean (the git status check) runs only when every other rule
// allows the skip. Any doubt parses: a wrong skip keeps stale facts, a wrong
// parse costs CPU.
func Decide(in Input, clean func() (bool, error)) Outcome {
	switch {
	case in.MaxSkips <= 0:
		return OutcomeDisabled
	case in.Reason != ReasonIntervalElapsed && in.Reason != ReasonGraphDirty:
		return OutcomeReasonIneligible
	case !in.Active.Present:
		return OutcomeNoActiveFull
	case in.Active.IsDelta:
		return OutcomeDeltaActive
	case strings.TrimSpace(in.RemoteCommit) == "" || in.RemoteCommit != in.Active.CommitSHA:
		return OutcomeHeadMoved
	case in.Active.Identity == "" || in.Identity == "":
		return OutcomeIdentityAbsent
	case in.Active.Identity != in.Identity:
		return OutcomeIdentityMismatch
	case in.HasSubmodules:
		return OutcomeSubmodules
	case in.Active.Skips >= in.MaxSkips-1:
		return OutcomeValveDue
	}
	ok, err := clean()
	if err != nil {
		return OutcomeReadError
	}
	if !ok {
		return OutcomeDirtyTree
	}
	return OutcomeSkipped
}

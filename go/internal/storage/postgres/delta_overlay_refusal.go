// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "fmt"

// CrossplaneDeltaBaseRefusedFailureClass is the durable failure_class of a
// Crossplane SATISFIED_BY read that refused a delta-active scope's base
// (#10005). The materialization retracts every edge of its scope before it
// writes, so it cannot use the delta-only rows the other overlay readers
// fall back to: the read fails before the retract, and the intent
// dead-letters at once. The state heals only at the refused scope's next
// full generation (the reconcile sweep, the growth valve, or the reindex
// watermark the projector's own refusal arms), not on a retry.
const CrossplaneDeltaBaseRefusedFailureClass = "crossplane_satisfied_by_delta_base_refused"

// deltaOverlayRefusal is one reached delta-active scope whose base the
// projector refuses too: union_base_missing or union_scope_mismatch.
// probeErr is set when the refusal came from a failed retention-probe
// statement rather than a missing or pruned base.
type deltaOverlayRefusal struct {
	target   deltaOverlayTarget
	outcome  string
	probeErr error
}

// CrossplaneDeltaBaseRefusedError reports a Crossplane SATISFIED_BY read
// (#10005) that reached a delta-active scope whose base is missing, pruned,
// or whose delta carries no delta_generation repository fact. It is not
// retryable and self-classifies as CrossplaneDeltaBaseRefusedFailureClass.
type CrossplaneDeltaBaseRefusedError struct {
	// ScopeID is the refused delta-active scope: the intent's own scope for
	// the claim read, any scope the XRD read reached for the XRD read.
	ScopeID string
	// GenerationID is the refused scope's active delta generation.
	GenerationID string
	// Read is the leg that refused: claim or xrd.
	Read string
	// Outcome is union_base_missing or union_scope_mismatch.
	Outcome string
	// BaselineCommitSHA is the delta's #7319 fence commit. It travels in
	// logs and failure details, never in the error string or a label.
	BaselineCommitSHA string
	// UnionBaseCommitSHA is the commit the base was looked up at (#8780).
	UnionBaseCommitSHA string
}

// Error names the scope, generation, leg, and outcome without the commit
// SHAs: SHAs stay out of error strings that metric classifiers may label.
func (e *CrossplaneDeltaBaseRefusedError) Error() string {
	return "crossplane satisfied-by " + e.Read + " read refused delta-active scope " + e.ScopeID +
		" generation " + e.GenerationID + " (" + e.Outcome + "): failing before the retract"
}

// Retryable reports false: a retry reads the same refused base.
func (*CrossplaneDeltaBaseRefusedError) Retryable() bool { return false }

// FailureClass returns CrossplaneDeltaBaseRefusedFailureClass.
func (*CrossplaneDeltaBaseRefusedError) FailureClass() string {
	return CrossplaneDeltaBaseRefusedFailureClass
}

// crossplaneDeltaBaseProbeError reports a Crossplane SATISFIED_BY read whose
// base resolution failed on the retention-probe statement (#10005). The base
// may be intact, so the read fails before the retract and retries. It
// carries no failure class of its own.
type crossplaneDeltaBaseProbeError struct {
	read         string
	scopeID      string
	generationID string
	err          error
}

// Error names the leg, scope, and generation and wraps the probe failure.
func (e *crossplaneDeltaBaseProbeError) Error() string {
	return fmt.Sprintf("crossplane satisfied-by %s read: delta base retention probe failed for scope %s generation %s: %v",
		e.read, e.scopeID, e.generationID, e.err)
}

// Unwrap exposes the probe failure.
func (e *crossplaneDeltaBaseProbeError) Unwrap() error { return e.err }

// Retryable reports true: a failed probe statement is a transient fault.
func (*crossplaneDeltaBaseProbeError) Retryable() bool { return true }

// crossplaneDeltaOverlayRefusal turns one Crossplane leg's refusals into the
// read's error, or nil when there are none. A probe failure on any refused
// scope wins and retries, because the base may be intact. Otherwise the first
// refusal recorded is returned as the non-retryable
// CrossplaneDeltaBaseRefusedError.
func crossplaneDeltaOverlayRefusal(read string, refusals []deltaOverlayRefusal) error {
	if len(refusals) == 0 {
		return nil
	}
	for _, refusal := range refusals {
		if refusal.probeErr != nil {
			return &crossplaneDeltaBaseProbeError{
				read: read, scopeID: refusal.target.scopeID,
				generationID: refusal.target.generationID, err: refusal.probeErr,
			}
		}
	}
	first := refusals[0]
	return &CrossplaneDeltaBaseRefusedError{
		ScopeID:            first.target.scopeID,
		GenerationID:       first.target.generationID,
		Read:               read,
		Outcome:            first.outcome,
		BaselineCommitSHA:  first.target.commits.Baseline,
		UnionBaseCommitSHA: first.target.commits.UnionBase,
	}
}

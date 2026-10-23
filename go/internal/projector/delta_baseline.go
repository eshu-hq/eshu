// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The delta-baseline fence (#7319) enforces one invariant: a delta generation
// activates only while the scope's active generation commit equals the commit
// the delta was diffed from. It does not prove that the graph content equals
// that commit; a generation that wrote the graph and never activated is a
// separate hole (#7389).

// Closed fence phases. Preflight runs before a claimed generation loads facts
// or writes the graph; Ack runs inside the Ack transaction and is
// authoritative.
const (
	DeltaBaselinePhasePreflight = "preflight"
	DeltaBaselinePhaseAck       = "ack"
)

// Failure classes written on a refused projector work row. A preflight
// refusal leaves the graph untouched. An Ack refusal happens after the
// generation wrote its overlay, which is an invariant breach: it needs a
// second valid claim in the scope.
const (
	DeltaBaselineMismatchClass                = "projector_delta_baseline_mismatch"
	DeltaBaselineMismatchAfterProjectionClass = "projector_delta_baseline_mismatch_after_projection"
	// DeltaBaselineBaseMissingClass is the failure class of a delta refused
	// because its base full generation is missing (#7797). It is distinct
	// from the baseline-mismatch classes so operators can tell "the baseline
	// moved" (re-diff and proceed) from "the base is gone" (a full re-parse
	// is the only repair).
	DeltaBaselineBaseMissingClass = "projector_delta_base_missing"
)

// DeltaBaseMissingError fails a delta fact load closed when no projected
// base full generation exists at the delta's baseline commit: the generation
// was never projected or retention pruned it. The service routes it through
// the delta-baseline fence as DeltaBaselineRefusedBaseMissing, which marks
// the generation superseded and records a reindex watermark; it must never
// be treated as a retryable load failure that re-projects a partial delta.
type DeltaBaseMissingError struct {
	// ScopeID is the scope the delta belongs to.
	ScopeID string
	// GenerationID is the delta generation that cannot project.
	GenerationID string
	// BaselineCommitSHA is the commit the delta was diffed from, whose base
	// full generation is missing. It travels in logs and failure details,
	// never in metric labels.
	BaselineCommitSHA string
}

// Error reports the missing base without embedding the commit SHA: SHAs stay
// out of error strings that metric classifiers may label.
func (e *DeltaBaseMissingError) Error() string {
	return "delta base full generation is missing for scope " + e.ScopeID +
		" generation " + e.GenerationID + ": refusing delta projection"
}

// DeltaUnionOutcome is the closed result of one delta-base union read. The
// values are the outcome label of eshu_dp_projector_delta_union_total.
type DeltaUnionOutcome string

const (
	// DeltaUnionBaseFound means the delta overlaid its base full generation.
	DeltaUnionBaseFound DeltaUnionOutcome = "union_base_found"
	// DeltaUnionBaseMissingRefused means no projected base full generation
	// exists at the delta's baseline, so the load refused.
	DeltaUnionBaseMissingRefused DeltaUnionOutcome = "union_base_missing_refused"
	// DeltaUnionResolveFailed means the union target/base lookup failed
	// with an error that is not a missing base (a transient query or scan
	// failure). The load returns the error and the work retries through
	// the generic fail path; it must never count as a refusal.
	DeltaUnionResolveFailed DeltaUnionOutcome = "union_resolve_failed"
	// DeltaUnionScopeMismatch means the store resolved a union (the
	// generation row says delta with a baseline) but the loaded delta
	// carries no delta_generation repository fact, so the overlay ran
	// additively: base plus delta concatenation with no dedup. The load
	// still returns the facts for gate and replay readers, with a WARN log
	// and this outcome count, but the projector refuses it before any write
	// (#8779): projecting an additive set would run a full retract over
	// duplicate stable keys. Such a delta never projected before #8779
	// either, because the fact boundary rejected its base rows.
	DeltaUnionScopeMismatch DeltaUnionOutcome = "union_scope_mismatch"
)

// DeltaBaselineOutcome is the closed result of one fence decision. The counted
// values are the outcome label of eshu_dp_projector_delta_baseline_fence_total.
type DeltaBaselineOutcome string

// Fence outcomes. Full and TargetMissing are never counted.
const (
	// DeltaBaselineFull is a full generation (no baseline, not a delta).
	DeltaBaselineFull DeltaBaselineOutcome = "full"
	// DeltaBaselineUnfenced is a delta written before baselines were recorded.
	DeltaBaselineUnfenced DeltaBaselineOutcome = "unfenced"
	// DeltaBaselineAlreadyActive is a delta whose own generation is active.
	DeltaBaselineAlreadyActive DeltaBaselineOutcome = "already_active"
	// DeltaBaselineMatched is a delta whose baseline is the active commit.
	DeltaBaselineMatched DeltaBaselineOutcome = "matched"
	// DeltaBaselineRefusedActiveDiffers is a delta whose baseline is not the
	// active commit, including an active generation with no commit.
	DeltaBaselineRefusedActiveDiffers DeltaBaselineOutcome = "refused_active_differs"
	// DeltaBaselineRefusedNoActive is a delta in a scope with no active
	// generation, so the graph content is unknown.
	DeltaBaselineRefusedNoActive DeltaBaselineOutcome = "refused_no_active"
	// DeltaBaselineRefusedBaseMissing is a delta whose base full generation
	// at its baseline commit was never projected or was retention-pruned, so
	// the delta-base union (#7797) has no base to overlay. It is produced by
	// the union read path, never by DecideDeltaBaseline (the fence read does
	// not fetch the base). The projector refuses it through the fence, which
	// marks the generation superseded and records a reindex watermark, so the
	// next selection forces the full re-parse that restores the base.
	DeltaBaselineRefusedBaseMissing DeltaBaselineOutcome = "refused_base_missing"
	// DeltaBaselineTargetMissing means the claimed generation row was not
	// found. The caller fails closed on its own path.
	DeltaBaselineTargetMissing DeltaBaselineOutcome = "target_missing"
	// DeltaBaselineTargetSuperseded means the claimed generation is already
	// superseded by a newer generation's activation. This is not an invariant
	// breach: a newer generation activating and superseding this one is the
	// normal, expected outcome of projector lag, and the caller's own
	// superseded-generation path already owns it with its own classification
	// (Ack's activation predicate refuses a superseded target the same way it
	// would for a full generation raced this way; preflight proceeds and lets
	// Ack or a heartbeat catch it, exactly as an unfenced full generation
	// already does). Comparing baseline commits against a target that can
	// never activate again would misreport routine supersession as the #7130
	// two-valid-claims signal this fence exists to catch.
	DeltaBaselineTargetSuperseded DeltaBaselineOutcome = "target_superseded"
)

// Refused reports whether the outcome refuses the delta.
func (o DeltaBaselineOutcome) Refused() bool {
	return o == DeltaBaselineRefusedActiveDiffers || o == DeltaBaselineRefusedNoActive ||
		o == DeltaBaselineRefusedBaseMissing
}

// counted reports whether the outcome is a label of the fence counter.
func (o DeltaBaselineOutcome) counted() bool {
	switch o {
	case DeltaBaselineUnfenced, DeltaBaselineAlreadyActive, DeltaBaselineMatched,
		DeltaBaselineRefusedActiveDiffers, DeltaBaselineRefusedNoActive,
		DeltaBaselineRefusedBaseMissing:
		return true
	default:
		return false
	}
}

// DeltaBaselineState is what the fence reads for one claimed generation: the
// target row and the scope's active generation, in one statement.
type DeltaBaselineState struct {
	// TargetFound is false when the claimed generation row does not exist.
	TargetFound bool
	// TargetStatus is the claimed generation's lifecycle status.
	TargetStatus scope.GenerationStatus
	// IsDelta marks a delta generation.
	IsDelta bool
	// BaselineCommitSHA is the commit the delta was diffed from. Empty for a
	// full generation or a delta written before #7319.
	BaselineCommitSHA string
	// ActiveGenerationID is the scope's active generation, empty when none.
	ActiveGenerationID string
	// ActiveCommitSHA is the active generation's source commit.
	ActiveCommitSHA string
}

// DecideDeltaBaseline applies the #7319 decision table. Both fence phases use
// it, so they cannot disagree:
//
//   - target row missing: target_missing (the caller fails closed)
//   - no baseline, not a delta: full
//   - no baseline, delta: unfenced (legacy row, proceeds)
//   - target already active: already_active (proceeds)
//   - target already superseded: target_superseded (not a breach; the
//     caller's own superseded-generation path proceeds to own it)
//   - no active generation: refused_no_active
//   - active commit equals baseline: matched (proceeds)
//   - otherwise: refused_active_differs
//
// The target-superseded case is checked before comparing commits: a delta
// that can never activate again was already retired by a newer generation,
// so a baseline mismatch there reflects the newer generation's commit having
// moved on, not a second claim racing this one to activate. Only commits are
// compared, trimmed, never generation ids: a full generation at the same
// commit that activated in between holds the same tree.
func DecideDeltaBaseline(state DeltaBaselineState) DeltaBaselineOutcome {
	baseline := strings.TrimSpace(state.BaselineCommitSHA)
	switch {
	case !state.TargetFound:
		return DeltaBaselineTargetMissing
	case baseline == "" && !state.IsDelta:
		return DeltaBaselineFull
	case baseline == "":
		return DeltaBaselineUnfenced
	case state.TargetStatus == scope.GenerationStatusActive:
		return DeltaBaselineAlreadyActive
	case state.TargetStatus == scope.GenerationStatusSuperseded:
		return DeltaBaselineTargetSuperseded
	case strings.TrimSpace(state.ActiveGenerationID) == "":
		return DeltaBaselineRefusedNoActive
	case strings.TrimSpace(state.ActiveCommitSHA) == baseline:
		return DeltaBaselineMatched
	default:
		return DeltaBaselineRefusedActiveDiffers
	}
}

// DeltaBaselineRefusal describes one refusal for the fence's mark statement
// and for the operator log.
type DeltaBaselineRefusal struct {
	// Phase is DeltaBaselinePhasePreflight or DeltaBaselinePhaseAck.
	Phase string
	// Outcome is the refusing outcome.
	Outcome DeltaBaselineOutcome
	// State is the read the decision was made on.
	State DeltaBaselineState
}

// FailureClass returns the failure_class the refusal writes on the work row.
func (r DeltaBaselineRefusal) FailureClass() string {
	if r.Outcome == DeltaBaselineRefusedBaseMissing {
		return DeltaBaselineBaseMissingClass
	}
	if r.Phase == DeltaBaselinePhaseAck {
		return DeltaBaselineMismatchAfterProjectionClass
	}
	return DeltaBaselineMismatchClass
}

// ActiveCommitForLog returns the active commit, or "none" when the scope has
// no active generation, for failure_details and logs.
func (r DeltaBaselineRefusal) ActiveCommitForLog() string {
	if strings.TrimSpace(r.State.ActiveGenerationID) == "" {
		return "none"
	}
	return strings.TrimSpace(r.State.ActiveCommitSHA)
}

// DeltaBaselineFence reads and enforces the delta-baseline fence for claimed
// projector work. The Postgres projector queue implements it. Every projection
// loop takes it as an explicitly wired, required dependency, never discovered
// by a type assertion, so a wrapper cannot silently drop it.
type DeltaBaselineFence interface {
	// ReadDeltaBaseline reads the claimed generation and the scope's active
	// generation without taking a lock.
	ReadDeltaBaseline(context.Context, ScopeGenerationWork) (DeltaBaselineState, error)
	// RefuseDeltaBaseline marks the claimed work and its generation superseded
	// with the refusal's failure class. It returns an error wrapping
	// failure.ErrWorkSuperseded when it marked the work, and one wrapping
	// failure.ErrWorkClaimLost when this attempt no longer owns it.
	RefuseDeltaBaseline(context.Context, ScopeGenerationWork, DeltaBaselineRefusal) error
}

// RecordDeltaBaselineFence counts one fence decision on
// eshu_dp_projector_delta_baseline_fence_total. Outcomes that are not counter
// labels (full, target_missing) and nil instruments are a no-op. Commit SHAs
// never become labels.
func RecordDeltaBaselineFence(
	ctx context.Context,
	instruments *telemetry.Instruments,
	phase string,
	outcome DeltaBaselineOutcome,
) {
	if instruments == nil || instruments.ProjectorDeltaBaselineFence == nil || !outcome.counted() {
		return
	}
	instruments.ProjectorDeltaBaselineFence.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(
		telemetry.AttrPhase(phase),
		telemetry.AttrOutcome(string(outcome)),
	))
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The holder fan-out (#9695, item 1.5b) closes the convergence gap a source
// retraction leaves: a finding is derived by every scope that holds evidence
// for its CVE, and the winners table keeps one copy per canonical key, so a
// scope whose own facts did not change keeps a stale copy until something reruns
// it. After the writer commit, and before the queue acknowledgement, a pass that
// changed or retracted finding rows reopens the supply_chain_impact item of
// every scope that holds an active copy of those CVEs. The reopened pass
// re-derives its copy from current evidence, so the answer converges without a
// precedence rule in the winners query.
const (
	// fanOutCVEChunk bounds the CVE ids one origin stamp carries, through
	// FanOutOriginCVEIDCap. It no longer sizes holder-read statements.
	fanOutCVEChunk = 200
	// fanOutHolderReadChunk bounds one holder-read statement to this many
	// lookup ids (#10564 H). Fixture scale measured 1.34 s warm at 20 ids,
	// but production hot scopes match ~138k finding rows per 25-id chunk
	// (6.44 s cold, 72% I/O read) and timed out at the 10 s guard; the
	// hottest 5 ids measured 810.8 ms; the post-deploy bar is per-statement
	// p95 under 2 s with max under 5 s (see docs/internal/evidence/
	// 10564-chunk5-production.md). The handler still reads every lookup id
	// in sequential statements, so the admitted id set and concurrency are
	// unchanged; statement count grows about 5x while per-statement heap
	// work dominates.
	fanOutHolderReadChunk = 5
	// fanOutHolderCap bounds how many holders one claim reopens. The remainder
	// is carried on the committing item and drained by its next claims.
	fanOutHolderCap = 256

	// PayloadKeyFanOutOrigin is the work-item payload key a fan-out reopen
	// stamps. A pass that finds it is one hop from the pass that changed the
	// write and does not fan out again; it consumes the stamp.
	PayloadKeyFanOutOrigin = "fanout_origin"
	// PayloadKeyFanOutRemaining is the work-item payload key holding the sorted
	// holder scope ids a claim did not reach.
	PayloadKeyFanOutRemaining = "fanout_remaining"
	// PayloadKeyFanOutOwed is the work-item payload key the writer sets, inside
	// its own transaction, when a pass's commit changed finding rows. It says the
	// commit happened and the fan-out it owes has not finished, so a retry knows
	// whether an earlier attempt committed. The fan-out clears it when the last
	// holder is reopened.
	PayloadKeyFanOutOwed = "fanout_owed"
	// Stamp keys, the origin shape, and the replay-flag clear live in
	// fanout_stamp.go beside the #9740 set they implement.
)

// FanOutOutcome is what reopening one holder did.
type FanOutOutcome string

// The closed set of per-holder reopen outcomes.
const (
	// FanOutReopened means a succeeded item went back to pending.
	FanOutReopened FanOutOutcome = "reopened"
	// FanOutReplayFlagged means a claimed or running item was marked so its
	// success acknowledgement returns it to pending once.
	FanOutReplayFlagged FanOutOutcome = "replay_flagged"
	// FanOutPendingNoop means the item was already queued or already flagged.
	FanOutPendingNoop FanOutOutcome = "pending_noop"
	// FanOutEnqueued means the holder's active generation had no queue item and
	// one was created.
	FanOutEnqueued FanOutOutcome = "enqueued"
)

// Pass-level counter outcomes that are not a per-holder result.
const (
	fanOutOutcomeCrashRecovery = "crash_recovery"
	fanOutOutcomeDeferred      = "deferred"
	fanOutOutcomeReadError     = "read_error"
	fanOutOutcomeReadTimeout   = "read_timeout"
	fanOutOutcomeReopenError   = "reopen_error"
	fanOutOutcomeStateError    = "state_error"
)

// The closed fan-out cause set (#9878): the evidence family of the
// committing pass's own generation, classified from its scope-fact kinds.
// The label names the generation, not the per-CVE cause — a digest flipped
// by cross-scope evidence during an EPSS-generation pass still reads epss.
// Enrichment generations are
// single-source by construction (one collector target per scope
// generation), so epss and kev name an enrichment re-trigger exactly. A
// generation mixing families reads mixed rather than guessing which family
// flipped a given CVE; the per-CVE truth stays in the findings themselves.
const (
	fanOutCauseEPSS            = "epss"
	fanOutCauseKEV             = "kev"
	fanOutCauseAdvisory        = "advisory"
	fanOutCauseSuppression     = "suppression"
	fanOutCausePackageEvidence = "package_evidence"
	fanOutCauseMixed           = "mixed"
	fanOutCauseOther           = "other"
)

// fanOutCauseManifestDependency labels a #9874 git manifest marker pass.
const fanOutCauseManifestDependency = "manifest_dependency"

// fanOutCause classifies the committing generation's scope-fact kinds into
// the closed cause set. Scope facts only ever carry
// supplyChainImpactFactKinds members (the scope load asks for exactly those
// kinds), so a kind outside the named families is package, image, alert, or
// deployment evidence.
func fanOutCause(kinds []string) string {
	seen := map[string]struct{}{}
	for _, kind := range kinds {
		switch kind {
		case chain.VulnerabilityEPSSScoreFactKind:
			seen[fanOutCauseEPSS] = struct{}{}
		case chain.VulnerabilityKnownExploitedFactKind:
			seen[fanOutCauseKEV] = struct{}{}
		case chain.VulnerabilityCVEFactKind,
			chain.VulnerabilityAffectedPackageFactKind,
			chain.VulnerabilityAffectedProductFactKind:
			seen[fanOutCauseAdvisory] = struct{}{}
		case chain.VulnerabilitySuppressionFactKind:
			seen[fanOutCauseSuppression] = struct{}{}
		case "":
			continue
		default:
			seen[fanOutCausePackageEvidence] = struct{}{}
		}
	}
	switch len(seen) {
	case 0:
		return fanOutCauseOther
	case 1:
		for cause := range seen {
			return cause
		}
	}
	return fanOutCauseMixed
}

// FanOutOwedRecorder records, inside the writer's transaction, that the commit
// owes a fan-out. Recording it in the same transaction as the rows makes "an
// attempt committed" and "the rows are there" one fact: a crash cannot leave one
// without the other. A lost lease fails the record, and the writer rolls back.
type FanOutOwedRecorder interface {
	RecordFanOutOwed(ctx context.Context, exec factwrite.Execer) error
}

// FanOutState is the one lease-fenced update a pass makes to its own queue
// item: rewrite (or clear) the list of holders it did not reach, and consume the
// origin stamp it saw. The list is touched only when ReplaceRemaining is set, so
// an empty Remaining never means "clear" by accident; with ReplaceRemaining set
// an empty Remaining clears the list, and a non-empty one also marks the item so
// its acknowledgement returns it to pending and the next claim drains the list.
// The stamp is consumed only while the stored stamp still equals ExpectedOrigin,
// so a newer stamp written since the claim survives.
type FanOutState struct {
	Remaining        []string
	ReplaceRemaining bool
	ConsumeOrigin    bool
	ExpectedOrigin   map[string]any
	// ClearOwed removes the owed marker: every holder this commit owes has been
	// reopened, or the owed work moved to the continuation list.
	ClearOwed bool
}

// SupplyChainImpactFanOut is the queue-side port of the holder fan-out. The
// Postgres implementation lives in storage/postgres; the handler owns the
// decision of whether and whom to fan out to.
type SupplyChainImpactFanOut interface {
	// FindHolderScopes returns the scopes, other than ownScopeID, that hold an
	// active finding row matching any of the lookup ids: a CVE id, an advisory
	// id, or an advisory-group member id (#10408). A holder row matches on its
	// cve_id, its advisory_id, or its stored advisory-group member set. It is
	// one bounded statement per call and fails with ErrFanOutReadTimeout when
	// it exceeds its timeout.
	FindHolderScopes(ctx context.Context, ownScopeID string, lookupIDs []string) ([]string, error)
	// ReopenHolder reopens the holder's supply_chain_impact item at the scope's
	// current active generation, stamping origin, or enqueues one when that
	// generation has none.
	ReopenHolder(ctx context.Context, holderScopeID string, origin FanOutOrigin) (FanOutOutcome, error)
	// FindInFlightScopes returns the scopes, other than ownScopeID, whose
	// supply_chain_impact item at the scope's active generation is claimed or
	// running. Such a pass may have loaded its evidence before the asking pass's
	// source facts were visible, and a scope whose new generation has no finding
	// rows yet is invisible to FindHolderScopes. Same bounded timeout and error
	// contract as FindHolderScopes.
	FindInFlightScopes(ctx context.Context, ownScopeID string) ([]string, error)
	// FindPackageHolderCandidates returns active package-holder identities outside
	// ownScopeID for query (#9874, #10061; see manifest_trigger.go).
	FindPackageHolderCandidates(ctx context.Context, ownScopeID string, query PackageHolderQuery) ([]PackageHolderCandidate, error)
	// FindRepositoryFindingHolders returns the scopes, other than ownScopeID,
	// that hold an active finding row whose payload repository_id is one of
	// repositoryIDs. The #9874 marker calls it when its diff may have missed a
	// removal (an unreadable prior view, a walk over the depth cap, or a
	// skipped pruned intermediate), so a stale finding for a package the
	// repository no longer declares is re-derived (#10059). Same bounded
	// timeout and error contract as FindHolderScopes.
	FindRepositoryFindingHolders(ctx context.Context, ownScopeID string, repositoryIDs []string) ([]string, error)
	// OwedRecorder returns the recorder the writer runs inside its transaction for
	// the calling pass's own item.
	OwedRecorder(intent reducercontract.Intent) FanOutOwedRecorder
	// UpdateFanOutState applies state to the calling pass's own item, fenced on
	// its lease.
	UpdateFanOutState(ctx context.Context, intent reducercontract.Intent, state FanOutState) error
	// ClearReplayFlagIfStampSeen clears the calling pass's
	// cross_scope_replay_required when its fan-out stamp still equals the one
	// it saw at claim time (#9740). A newer stamp turns the clear into a
	// no-op (reported false) so the pass reruns once under the newer change.
	// It is fenced on the lease exactly like UpdateFanOutState: a lost lease
	// is an error, because another worker now owns the item and will redo it.
	ClearReplayFlagIfStampSeen(ctx context.Context, intent reducercontract.Intent, seen map[string]any) (bool, error)
}

// fanOutResult is what one pass's fan-out did, for sub-signals and the log.
type fanOutResult struct {
	// cause is the committing generation's evidence family (fanOutCause),
	// carried on every counter outcome this pass emits.
	cause         string
	holders       int
	reopened      int
	replayFlagged int
	pendingNoop   int
	enqueued      int
	// deferred is the number of holders still owed after this claim, a gauge
	// the sub-signal reports. newlyDeferred is the part that was not already
	// owed when the claim started, which is what the counter adds, so draining
	// one backlog across several claims counts each holder once.
	deferred      int
	newlyDeferred int
	crashRecovery bool
	// replayCleared is true when the pass proved its rewrite clear of the
	// flagged change and its replay flag was cleared (#9740), so the ack
	// succeeds instead of rerunning it.
	replayCleared bool
}

// fanOutHolders runs the holder fan-out for one committed pass. It returns an
// error for a failed holder read or reopen, so the pass fails before its
// acknowledgement and the queue retries it: a holder left unreopened would be a
// silently stale copy. A handler with no port wired does nothing.
func (h SupplyChainImpactHandler) fanOutHolders(
	ctx context.Context,
	intent reducercontract.Intent,
	write SupplyChainImpactWriteResult,
	cause string,
) (fanOutResult, error) {
	if h.FanOut == nil {
		return fanOutResult{cause: cause}, nil
	}
	stamped := fanOutOriginStamped(intent)
	owed := fanOutOwed(intent)
	oneHop := fanOutOneHop(intent)
	remaining := fanOutRemaining(intent)
	// stampCVEs is the change that reopened or flagged this pass, as it saw it
	// at claim time. A stamp without the key predates the set, and a corrupt
	// one parses as no set: both fail closed to a full pass and a rerun.
	stampCVEs := fanOutStampCVEIDs(intent)
	var lookupIDs []string
	recoveryOnly := false
	if !oneHop {
		// A continuation drain does not add the pass's own changes: they were
		// fanned out by the claim that wrote them. The touched group set joins
		// the member-expanded changed set (#10408): a merge or split retires
		// group ids and member ids no replacement row names, and only the
		// touched set still names them, so the holders of those rows reopen.
		lookupIDs = unionSortedStrings(write.ChangedCVEIDs, write.TouchedGroupIDs)
		if owed && len(remaining) == 0 && len(write.RecoveryCVEIDs) > 0 {
			// An earlier attempt committed and died before it finished the
			// fan-out, so this rerun's own rewrite shows no change. Fan out for
			// every CVE the scope names.
			recoveryOnly = len(write.ChangedCVEIDs) == 0
			lookupIDs = unionSortedStrings(lookupIDs, write.RecoveryCVEIDs)
		}
	} else {
		// A one-hop re-derivation fans out nothing of its own rewrite, but
		// the suppression diff's ids pass through regardless of the stamp:
		// a stamped first pass of an operator generation would otherwise
		// admit without fanning out (#10019).
		lookupIDs = write.SuppressionCVEIDs
	}
	// The stamp the reopened holders carry names what this pass fanned out
	// plus the change that caused this pass, so a continuation drain keeps
	// scoping the original change instead of widening to a full pass.
	stampOut := CapFanOutStampCVEIDs(unionSortedStrings(lookupIDs, stampCVEs))
	state := FanOutState{ConsumeOrigin: stamped}
	if stamped {
		state.ExpectedOrigin = fanOutOriginOf(intent)
	}
	if len(lookupIDs) == 0 && len(remaining) == 0 {
		result := fanOutResult{cause: cause}
		if stamped || owed || len(write.ChangedCVEIDs) > 0 || len(write.TouchedGroupIDs) > 0 {
			// Nothing is owed after all; consume the stamp so a later reopen by
			// another path is judged fresh, and drop a marker with nothing behind it.
			// This attempt's own commit set the marker for a changed CVE or a
			// touched advisory group (#10411), one-hop included; the group
			// enqueue already ran before this fan-out, so clearing is safe.
			state.ClearOwed = owed || len(write.ChangedCVEIDs) > 0 || len(write.TouchedGroupIDs) > 0
			if clear, err := h.clearReplayFlag(ctx, intent, write, stampCVEs, state.ExpectedOrigin); err != nil {
				return fanOutResult{cause: cause}, err
			} else {
				result.replayCleared = clear
			}
			if err := h.FanOut.UpdateFanOutState(ctx, intent, state); err != nil {
				return result, fanOutError{fmt.Errorf("clear supply chain impact fan-out state: %w", err)}
			}
		}
		return result, nil
	}

	ctx, span := h.startFanOutSpan(ctx, intent)
	defer span.End()
	holders := make(map[string]struct{}, len(remaining))
	for _, id := range remaining {
		holders[id] = struct{}{}
	}
	// The in-flight read comes FIRST, then the row read. A holder still claimed or
	// running at the in-flight read is flagged. One that acknowledged before it has
	// committed its rows (a pass commits before it acknowledges), so the row read
	// that follows sees them. The other order leaves a gap: a holder with no rows
	// at the first read that commits and acknowledges before the second is missed
	// by both. A pass claimed after the in-flight read loads its evidence after the
	// source's facts became visible, so it needs no flag.
	if len(lookupIDs) > 0 {
		inFlight, err := h.FanOut.FindInFlightScopes(ctx, intent.ScopeID)
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteInFlight)
			span.RecordError(err)
			return fanOutResult{cause: cause}, fanOutError{fmt.Errorf("find in-flight supply chain impact scopes: %w", err)}
		}
		for _, id := range inFlight {
			holders[id] = struct{}{}
		}
	}
	for _, chunk := range chunkStrings(lookupIDs, fanOutHolderReadChunk) {
		found, err := h.FanOut.FindHolderScopes(ctx, intent.ScopeID, chunk)
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolders)
			span.RecordError(err)
			return fanOutResult{cause: cause}, fanOutError{fmt.Errorf("find supply chain impact holders: %w", err)}
		}
		for _, id := range found {
			holders[id] = struct{}{}
		}
	}
	delete(holders, intent.ScopeID)
	ordered := make([]string, 0, len(holders))
	for id := range holders {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)

	batch, rest := nextFanOutChunk(ordered, fanOutHolderCap)
	result := fanOutResult{cause: cause, holders: len(ordered), deferred: len(rest), crashRecovery: recoveryOnly}
	if len(remaining) == 0 {
		result.newlyDeferred = len(rest)
	}
	origin := FanOutOrigin{
		SourceScopeID:      intent.ScopeID,
		SourceGenerationID: intent.GenerationID,
		SourceIntentID:     intent.IntentID,
		CVEIDs:             stampOut,
	}
	for _, holder := range batch {
		outcome, err := h.FanOut.ReopenHolder(ctx, holder, origin)
		if err != nil {
			h.countFanOut(ctx, cause, fanOutOutcomeReopenError, 1)
			span.RecordError(err)
			return result, fanOutError{fmt.Errorf("reopen supply chain impact holder %q: %w", holder, err)}
		}
		result.record(outcome)
		h.logFanOutHolder(ctx, intent, holder, outcome)
	}
	if len(rest) > 0 || len(remaining) > 0 {
		state.ReplaceRemaining = true
		state.Remaining = rest
	}
	// The marker stays while holders are still owed: the continuation list carries
	// the debt across claims and the last drain clears both.
	// This attempt's own commit set the marker inside its transaction, after the
	// claim snapshot the intent carries, so a changed commit clears it too.
	// The writer records the marker for a touched advisory group as well as
	// a changed CVE (#10411), on genuine and one-hop passes alike, so the
	// clear condition mirrors the set condition exactly.
	markerSet := len(write.ChangedCVEIDs) > 0 || len(write.TouchedGroupIDs) > 0
	state.ClearOwed = (owed || markerSet) && len(rest) == 0
	if len(rest) == 0 && len(remaining) == 0 {
		// No continuation debt rides to the next claim, so a pass that proved
		// its rewrite clear of the flagged change clears its replay flag
		// before the stamp is consumed below: the ack then succeeds instead
		// of rerunning it. A newer stamp between the compare and the clear
		// turns the clear into a no-op and the pass reruns once.
		if clear, err := h.clearReplayFlag(ctx, intent, write, stampCVEs, state.ExpectedOrigin); err != nil {
			h.countFanOut(ctx, cause, fanOutOutcomeStateError, 1)
			span.RecordError(err)
			return result, err
		} else {
			result.replayCleared = clear
		}
	}
	if state.ReplaceRemaining || state.ConsumeOrigin || state.ClearOwed {
		if err := h.FanOut.UpdateFanOutState(ctx, intent, state); err != nil {
			h.countFanOut(ctx, cause, fanOutOutcomeStateError, 1)
			span.RecordError(err)
			return result, fanOutError{fmt.Errorf("record supply chain impact fan-out state: %w", err)}
		}
	}
	h.emitFanOut(ctx, intent, result)
	span.SetAttributes(
		attribute.String("cause", result.cause),
		attribute.Int("holders", result.holders),
		attribute.Int("reopened", result.reopened),
		attribute.Int("replay_flagged", result.replayFlagged),
		attribute.Int("deferred", result.deferred),
	)
	return result, nil
}

func (r *fanOutResult) record(outcome FanOutOutcome) {
	switch outcome {
	case FanOutReopened:
		r.reopened++
	case FanOutReplayFlagged:
		r.replayFlagged++
	case FanOutEnqueued:
		r.enqueued++
	default:
		r.pendingNoop++
	}
}

// fanOutOriginStamped reports whether the intent's item carries a fan-out
// origin stamp.
func fanOutOriginStamped(intent reducercontract.Intent) bool {
	value, ok := intent.Payload[PayloadKeyFanOutOrigin]
	return ok && value != nil
}

// fanOutOwed reports whether the intent's item carries the owed marker: a
// commit of this item changed rows and its fan-out has not finished.
func fanOutOwed(intent reducercontract.Intent) bool {
	owed, _ := intent.Payload[PayloadKeyFanOutOwed].(bool)
	return owed
}

// fanOutOneHop reports whether this pass is the single re-derivation a fan-out
// asked for, which re-derives its copy and stops. That is the attempt the replay
// created: a reopen of a succeeded item and the replay of a flagged one both
// leave attempt_count at 0, so the claim is attempt 1. A stamp that reaches a
// later attempt was written while a genuine pass was running and that pass then
// failed or crashed, so the retry is a genuine pass again. An owed marker means
// this item committed changes it has not finished fanning out, so it is genuine
// too, whatever stamp it carries.
func fanOutOneHop(intent reducercontract.Intent) bool {
	return fanOutOriginStamped(intent) && intent.AttemptCount <= 1 && !fanOutOwed(intent)
}

// fanOutOriginOf returns the stamp map the intent carries, or nil.
func fanOutOriginOf(intent reducercontract.Intent) map[string]any {
	stamp, _ := intent.Payload[PayloadKeyFanOutOrigin].(map[string]any)
	return stamp
}

// owedRecorder returns the recorder the writer runs inside its transaction,
// or nil when there is no port. A one-hop re-derivation records too: it fans
// out nothing of its own rewrite, but since #10411 it owes the advisory-group
// enqueue that runs after the commit, and the marker is the only durable
// record that the commit happened if the enqueue (or the process) fails
// first. The fan-out's clear conditions mirror the writer's set condition,
// so a completed one-hop pass leaves no marker behind.
func (h SupplyChainImpactHandler) owedRecorder(intent reducercontract.Intent) FanOutOwedRecorder {
	if h.FanOut == nil {
		return nil
	}
	return h.FanOut.OwedRecorder(intent)
}

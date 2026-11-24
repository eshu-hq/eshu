// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
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
	// p95 under 2 s with max under 5 s (see (#10564)). The handler still reads every lookup id
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
	// FanOutStampWidened means a queued item, or an in-flight one already
	// flagged, had a stamp that did not name this change, and the reopen merged
	// the change into it without touching anything else (#10972).
	FanOutStampWidened FanOutOutcome = "stamp_widened"
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
	// ReopenHolderDetailed reopens the holder's supply_chain_impact item at the
	// scope's current active generation, stamping origin, or enqueues one when
	// that generation has none. A dead letter there is reopened as a full
	// genuine pass, without the stamp, unless its failure class is unsafe to
	// replay (#11147); the result names the dead letter's prior class.
	ReopenHolderDetailed(ctx context.Context, holderScopeID string, origin FanOutOrigin) (FanOutReopen, error)
	// FindQueuedHolderScopes returns, from holders, those whose supply_chain_impact
	// items at the scope's active generation are all pending or retrying with a
	// next pass that covers stampOut, the canonical stamp set this fan-out
	// writes: a full pass, or a scoped one whose stamp names every id (#10972).
	// An empty or sentinel stampOut is unbounded, so only full next passes cover
	// it. One set read at one snapshot. Same bounded timeout and error contract
	// as FindHolderScopes.
	FindQueuedHolderScopes(ctx context.Context, holders, stampOut []string) ([]string, error)
	// FindInFlightNonMarkerScopes returns the scopes, other than ownScopeID,
	// whose findings item at the active generation is claimed or running; a
	// manifest marker is left out (#10972: it loads no evidence, so flagging it
	// only re-pends it). Such a pass may have loaded its evidence before the
	// asking pass's source facts were visible, and a scope whose new generation
	// has no finding rows yet is invisible to FindHolderScopes. Same timeout contract.
	FindInFlightNonMarkerScopes(ctx context.Context, ownScopeID string) ([]string, error)
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
	// stampWidened counts holders whose queued or already-flagged item got
	// this change merged into its stamp (#10972).
	stampWidened int
	// prefiltered counts holders the queued-holder prefilter dropped before
	// the cap slice: their item was already pending or retrying (#10972).
	prefiltered int
	// deadLetterReopened, deadLetterSkipped, and noActiveGeneration count the
	// #11147 holder outcomes for the summary line and sub-signals; their
	// counter series are written per holder (recordHolderReopen) or with the
	// pass (emitFanOut).
	deadLetterReopened int
	deadLetterSkipped  int
	noActiveGeneration int
	// holderStatements, splitChunks, hotIDs, and maxIDMassRows record what
	// adaptive splitting did (#10643): statements run, chunks that split,
	// singleton statements (hot by mass, or isolated by packing/unknown
	// mass — read hotIDs as singletons), and the largest measured per-id
	// mass.
	holderStatements int
	splitChunks      int
	hotIDs           int
	maxIDMassRows    int64
	// keyRangedIDs and keyRangeStatements record the hot singleton ids read
	// as fact_id key ranges and the key-range statements they ran (#10643).
	keyRangedIDs       int
	keyRangeStatements int
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
	ordered, split, err := h.collectFanOutHolders(ctx, span, cause, intent, lookupIDs, remaining)
	if err != nil {
		return fanOutResult{cause: cause}, err
	}

	// One set read drops the holders whose queued pass already covers stampOut
	// BEFORE the cap slice, so the cap and the continuation list count only
	// holders that still need a reopen statement (#10972).
	holderTotal := len(ordered)
	ordered, prefiltered, err := h.dropQueuedHolders(ctx, cause, ordered, stampOut)
	if err != nil {
		span.RecordError(err)
		return fanOutResult{cause: cause}, err
	}
	batch, rest := nextFanOutChunk(ordered, fanOutHolderCap)
	result := fanOutResult{
		cause:              cause,
		holders:            holderTotal,
		prefiltered:        prefiltered,
		deferred:           len(rest),
		crashRecovery:      recoveryOnly,
		holderStatements:   split.statements,
		splitChunks:        split.splitChunks,
		hotIDs:             split.hotIDs,
		maxIDMassRows:      split.maxMass,
		keyRangedIDs:       split.keyRangedIDs,
		keyRangeStatements: split.keyRangeStatements,
	}
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
		reopen, err := h.FanOut.ReopenHolderDetailed(ctx, holder, origin)
		if err != nil {
			h.countFanOut(ctx, cause, fanOutOutcomeReopenError, 1)
			span.RecordError(err)
			return result, fanOutError{fmt.Errorf("reopen supply chain impact holder %q: %w", holder, err)}
		}
		h.recordHolderReopen(ctx, intent, &result, holder, reopen)
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
		attribute.Int("stamp_widened", result.stampWidened),
		attribute.Int("deferred", result.deferred),
		attribute.Int("prefiltered_pending", result.prefiltered),
		attribute.Int("holder_statements", result.holderStatements),
		attribute.Int("split_chunks", result.splitChunks),
		attribute.Int("hot_ids", result.hotIDs),
		attribute.Int64("max_id_mass_rows", result.maxIDMassRows),
		attribute.Int("key_ranged_ids", result.keyRangedIDs),
		attribute.Int("holder_key_range_statements", result.keyRangeStatements),
	)
	return result, nil
}

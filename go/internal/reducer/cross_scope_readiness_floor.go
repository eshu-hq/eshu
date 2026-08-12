// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
	"strings"
	"time"

	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// crossScopeProducerReadinessMaxWait bounds the deferral by ELAPSED TIME since
// the current repair cycle began, so a consumer converges instead of waiting
// forever on a producer scope that never activates.
//
// The bound is not optional and it MUST NOT be a retry-count comparison.
// CrossScopeProducerNotReadyFailureClass is enrolled in
// nonCountingReducerRetryFailureClasses (reducer_queue_readiness_sql.go), which
// FREEZES fact_work_items.attempt_count for exactly this class -- by design, so
// a waiting consumer is never dead-lettered. That freeze means Intent.AttemptCount
// stops advancing after the first defer and reads identically on every later
// claim, so a bound compared against it can never fire. The sibling AWS gate
// shipped that mistake first and had to be re-proven against the real queue.
//
// Without a bound, a producer scope that is permanently absent, permanently
// failed, or stuck leaves its consumers deferring forever in a class that never
// counts and never dead-letters. That is a worse failure than the durable empty
// answer this floor exists to prevent: the empty answer is at least visible and
// repairable, where an eternal defer is silent.
//
// 30 minutes matches the sibling gate rather than a separate measurement:
// generous enough that ordinary asynchronous producer ingestion never reaches
// it, short enough that a genuinely stuck producer converges to a terminal
// answer inside an operator's normal triage window.
const crossScopeProducerReadinessMaxWait = 30 * time.Minute

// CrossScopeProducerReadiness answers whether the producer scopes a consumer
// depends on have finished publishing: their generation is active and their
// projector work has drained.
//
// This is the correctness floor for the cross-scope dependency contract
// (#5709), and it closes one specific window. The consumer that was ALREADY
// CLAIMED when the producer finished is handled elsewhere and needs no floor:
// the completion fanout marks such a consumer with cross_scope_replay_required
// (go/internal/storage/postgres/cross_scope_completion_fanout.go) and the
// trigger installed by migration 093 rewrites that row's 'succeeded'
// acknowledgement back to 'pending', so it runs again.
//
// What remains is the ACTIVATION window. A producer's reducer row reaches
// 'succeeded' before its scope generation is activated -- activation happens at
// projector acknowledgement. A consumer running inside that window reads
// through container_image_identity_current_support_facts_for (migration 092c),
// which returns a row only when scope.active_generation_id matches the state
// row's generation, that generation's status is 'active', and the identity
// domain's scope-state row carries an active_set_id. So the consumer resolves
// nothing, writes a durable "no correlation" decision, and no later event
// disturbs it. The floor is what makes such a consumer defer instead.
//
// The readiness signal is a proxy for those conditions, not a restatement of
// them. The store checks that a producer scope has an active generation and no
// live projector work; it does not evaluate the 092c join. A producer that has
// activated and drained its projector but whose identity reducer has not yet
// written a support set reads as ready and still joins to nothing. That
// residual window is narrower than the one this closes, and it is not closed
// here.
//
// Deliberately an interface consumed here rather than a concrete store: the
// reducer package already takes its readiness seams this way (see
// GraphProjectionReadinessLookup), and it keeps this package free of SQL.
type CrossScopeProducerReadiness interface {
	// CrossScopeProducersReady reports whether the producer scopes the
	// consumer declares are quiescent-active, so an empty cross-scope join is
	// the true answer rather than a timing artefact.
	//
	// Returning (false, nil) means "not yet" and is the deferrable case.
	// Returning an error means the readiness question itself could not be
	// answered, which is NOT a readiness miss and must not be reported as one.
	CrossScopeProducersReady(
		ctx context.Context,
		consumer Domain,
		scopeID string,
		generationID string,
	) (bool, error)
}

// crossScopeProducerReadinessSignal is the floor's answer, captured BEFORE the
// consumer's cross-scope load runs. See
// checkCrossScopeProducerReadinessBeforeLoad for why the ordering matters.
type crossScopeProducerReadinessSignal struct {
	// gateDisabled is true when there is nothing to gate on: an unwired seam,
	// a domain outside the cross-scope catalog, a pass with nothing to look
	// up, or an elapsed bound already reached. When true the handler must
	// never defer, whatever the load returns.
	gateDisabled bool
	// ready is the readiness store's answer, captured before the load.
	// Meaningless when gateDisabled is true.
	ready bool
	// producerDomains is the bounded producer set the deferral error names.
	// Empty when gateDisabled is true.
	producerDomains []Domain
}

// checkCrossScopeProducerReadinessBeforeLoad captures the readiness signal the
// handler must use for its defer decision. It MUST be called BEFORE the
// consumer's cross-scope load, never after (#5875 P1 caught the same ordering
// bug on the sibling AWS gate).
//
// Sampling readiness after the load reintroduces the very bug the floor
// prevents, through a narrower window: a producer generation that activates
// between the load and a post-load readiness check makes the store report
// "ready" while the evidence already in hand predates that activation. The
// handler then durably writes an empty correlation computed from a snapshot a
// fresher read would have filled in. The repair path cannot save it either --
// the fanout's reopen selects 'succeeded' rows, and a maintenance pass racing
// this intent while it is still claimed skips it.
//
// Checking first closes that window. The reverse race -- a producer activating
// BETWEEN this check and the load -- is benign: the load simply reads fresher
// data than this signal assumed, which can only add evidence, never remove it,
// and the post-load resolved count is what decides whether to defer.
//
// crossScopeLookupPlanned reports whether this pass will actually ask the
// cross-scope loader anything. It is false when the consumer has no filter
// values to look up or no loader implementing the cross-scope seam, and it
// disables the gate: see crossScopeProducerDeferral for why an unasked question
// must not be answered with a deferral.
//
// now must be the SAME clock reading the handler uses for the rest of the pass,
// passed in rather than read again here, so a slow load cannot itself push the
// intent past the elapsed bound.
func checkCrossScopeProducerReadinessBeforeLoad(
	ctx context.Context,
	readiness CrossScopeProducerReadiness,
	intent Intent,
	now time.Time,
	crossScopeLookupPlanned bool,
) (crossScopeProducerReadinessSignal, error) {
	if readiness == nil {
		// Unwired means "no floor", not "not ready". Defaulting to defer would
		// strand every consumer in a deployment that has not adopted the seam.
		return crossScopeProducerReadinessSignal{gateDisabled: true}, nil
	}
	if !crossScopeLookupPlanned {
		return crossScopeProducerReadinessSignal{gateDisabled: true}, nil
	}
	dependencies := crossScopeDependenciesForRegistration(intent.Domain)
	if len(dependencies) == 0 {
		// Not a registered cross-scope consumer. A domain absent from the
		// catalog must behave exactly as it did before this floor existed.
		return crossScopeProducerReadinessSignal{gateDisabled: true}, nil
	}
	if anchor := crossScopeReadinessCycleAnchor(intent); !anchor.IsZero() &&
		now.Sub(anchor) >= crossScopeProducerReadinessMaxWait {
		return crossScopeProducerReadinessSignal{gateDisabled: true}, nil
	}
	ready, err := readiness.CrossScopeProducersReady(ctx, intent.Domain, intent.ScopeID, intent.GenerationID)
	if err != nil {
		// Never converted into a readiness miss. A store that cannot answer is a
		// real failure; reporting it as cross_scope_producer_not_ready would
		// hide it in a class that never counts against the retry budget, so it
		// could retry forever without surfacing.
		//
		// It does go through the same transient classifier the consumer's own
		// cross-scope load uses, which promotes a torn database stream
		// ("unexpected EOF") to the non-counting fact_load_transient class.
		// Without that, a connection reset during this probe burns an attempt
		// where the identical fault one call later would not -- the probe runs
		// against the same Postgres, in the same handler pass, over the same
		// connection pool. classifyFactLoadError returns every other error
		// unchanged, so nothing else about the classification moves.
		return crossScopeProducerReadinessSignal{}, classifyFactLoadError(err)
	}
	return crossScopeProducerReadinessSignal{
		ready:           ready,
		producerDomains: dependencies[0].ProducerDomains,
	}, nil
}

// crossScopeProducerDeferral combines the PRE-load readiness signal with the
// POST-load resolved count and returns the non-counting readiness error when
// the consumer must defer. It returns nil in every other case, so a caller can
// wrap an existing load site without changing its success path.
//
// resolved is how many cross-scope envelopes the load returned for the WHOLE
// BATCH, and that distinction is the floor's widest gap. One
// CICDRunCorrelationHandler.Handle pass classifies every CI run in a scope
// generation, and it issues one cross-scope load filtered by the union of every
// run's artifact digests (ciArtifactDigests). So one digest resolving anywhere
// in the batch makes this count non-zero and disarms the floor for every other
// run in the same pass.
//
// What that costs: a batch carrying run A, whose digest an already-activated OCI
// scope published, and run B, whose identity is still inside its producer's
// activation window, does not defer. B commits a durable answer computed without
// its producer's evidence -- derived or unresolved where the evidence would have
// made it exact -- which is the #5709 defect. Where registry activity is steady
// a fully empty batch join is uncommon, so the floor fires less often than
// reading this line per-run would suggest.
//
// It stays batch-wide anyway. Deferring the batch would hold back the runs that
// already have their evidence, and the fix is not a tighter comparison here but
// per-digest readiness -- a different contract than #5709 specifies, and not
// built. TestCICDRunCorrelationDoesNotDeferABatchWhereAnotherRunResolved pins
// the behavior so it stays a known property;
// docs/internal/evidence/5709-cross-scope-readiness-floor.md discloses it as a
// residual window.
//
// A ready signal with an empty join means the empty answer is the true one:
// the producer scopes have activated and still say nothing about these runs, so
// the handler converges on a durable unresolved decision rather than deferring
// forever.
func crossScopeProducerDeferral(
	signal crossScopeProducerReadinessSignal,
	intent Intent,
	resolved int,
) error {
	if signal.gateDisabled || signal.ready || resolved > 0 {
		return nil
	}
	return newCrossScopeProducerNotReadyError(
		intent.Domain,
		intent.ScopeID,
		intent.GenerationID,
		signal.producerDomains,
	)
}

// crossScopeReadinessCycleAnchor returns intent.CycleStartedAt when set,
// falling back to intent.EnqueuedAt.
//
// CycleStartedAt is COALESCE(reopened_at, created_at) from the claim query, so
// it is the only one of the two that gets a fresh value when a maintenance
// pass reopens the row. Anchoring on EnqueuedAt alone would read as "already
// past the bound" on the first claim of any reopened row, skipping the
// readiness lookup entirely and committing a possibly-early answer with no
// grace window -- the regression a round-3 review caught on the sibling AWS
// gate (see awsCloudRuntimeDriftStatePendingMaxWait).
//
// A zero anchor means elapsed time is unknown, not infinite. Subtracting a
// zero time.Time from now reads as tens of thousands of hours and would fire
// the terminal fallback on the very first defer. Returning zero here keeps the
// caller deferring, which costs a bounded delay; the other direction commits a
// possibly-wrong answer for a reason unrelated to elapsed time.
func crossScopeReadinessCycleAnchor(intent Intent) time.Time {
	if !intent.CycleStartedAt.IsZero() {
		return intent.CycleStartedAt
	}
	return intent.EnqueuedAt
}

// logCrossScopeProducerNotReadyDefer records a floor deferral as its own
// structured log line, distinct from a normal correlation pass and from a
// handler error.
//
// It logs elapsed time against the bound, never attempt_count. This defer's own
// failure class freezes attempt_count (see crossScopeProducerReadinessMaxWait),
// so attempt_count reads as the same constant on every occurrence and tells an
// operator nothing about how close the intent is to its terminal fallback.
// elapsed_since_cycle_start against max_wait is the only pair that answers
// "how much longer will this wait".
//
// producer_domains comes from the bounded cross-scope catalog, so it is a small
// closed set, not a high-cardinality label.
func logCrossScopeProducerNotReadyDefer(
	ctx context.Context,
	logger *slog.Logger,
	intent Intent,
	now time.Time,
	producerDomains []Domain,
) {
	if logger == nil {
		return
	}
	producers := make([]string, 0, len(producerDomains))
	for _, producer := range producerDomains {
		producers = append(producers, string(producer))
	}
	attrs := []slog.Attr{
		log.Domain(string(intent.Domain)),
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		slog.String("producer_domains", strings.Join(producers, ",")),
		slog.Duration("max_wait", crossScopeProducerReadinessMaxWait),
	}
	if anchor := crossScopeReadinessCycleAnchor(intent); !anchor.IsZero() {
		attrs = append(attrs, slog.Duration("elapsed_since_cycle_start", now.Sub(anchor)))
	}
	logger.LogAttrs(
		ctx, slog.LevelInfo,
		"cross-scope consumer deferred: producer scopes have not activated", attrs...,
	)
}

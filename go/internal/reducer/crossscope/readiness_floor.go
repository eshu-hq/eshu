// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"context"
	"log/slog"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ProducerReadinessMaxWait bounds the deferral by ELAPSED TIME since the
// current repair cycle began, so a consumer converges instead of waiting
// forever on a producer scope that never activates.
//
// The bound is not optional and it MUST NOT be a retry-count comparison.
// ProducerNotReadyFailureClass is enrolled in nonCountingReducerRetryFailureClasses
// (reducer_queue_readiness_sql.go), which
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
const ProducerReadinessMaxWait = 30 * time.Minute

// ProducerReadiness answers whether the producer scopes a consumer depends on
// have finished publishing: their generation is active and their projector
// work has drained.
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
type ProducerReadiness interface {
	// CrossScopeProducersReady reports, PER DECLARED PRODUCER, whether that
	// producer's scopes are quiescent-active, so an empty cross-scope join is
	// the true answer rather than a timing artefact.
	//
	// A false entry means "not yet" for that producer and is the deferrable
	// case. Returning an error means the readiness question itself could not be
	// answered, which is NOT a readiness miss and must not be reported as one.
	CrossScopeProducersReady(
		ctx context.Context,
		consumer reducercontract.Domain,
		scopeID string,
		generationID string,
	) (ProducerReadinessByDomain, error)
}

// ProducerReadinessByDomain answers readiness for each producer domain
// separately.
//
// Per producer, and not one bool for the set, because the consumer's resolved
// evidence is also per producer and the two are compared pairwise. A single
// aggregate bool paired with a single aggregate count cannot tell "both
// producers answered once" from "one producer answered twice", and
// supply_chain_impact shipped exactly that arithmetic until a round of review on
// #6093 caught it: two container_image_identity envelopes disarmed the floor for
// ci_cd_run_correlation, which had published nothing.
//
// An implementation MUST carry an entry for every producer domain the consumer
// declares in the dependency catalog. A missing entry reads as NOT ready,
// which is the safe direction — the consumer defers, bounded by
// ProducerReadinessMaxWait, rather than committing an answer for a
// producer nobody asked about.
type ProducerReadinessByDomain map[reducercontract.Domain]bool

// ProducerReadinessSignal is the floor's answer, captured BEFORE the
// consumer's cross-scope load runs. See CheckProducerReadinessBeforeLoad for
// why the ordering matters.
type ProducerReadinessSignal struct {
	// gateDisabled is true when there is nothing to gate on: an unwired seam,
	// a domain outside the cross-scope catalog, a pass with nothing to look
	// up, or an elapsed bound already reached. When true the handler must
	// never defer, whatever the load returns.
	gateDisabled bool
	// readyByProducer is the readiness store's per-producer answer, captured
	// before the load. Meaningless when gateDisabled is true.
	readyByProducer ProducerReadinessByDomain
	// ProducerDomains is the bounded producer set, in catalog order. Every one
	// of them is evaluated on its own; the deferral error names the subset
	// actually holding the pass back. Empty when gateDisabled is true.
	//
	// Exported so the reducer root can read the batch-wide producer set back
	// off the signal it just captured, to build its own resolved-count map
	// (see ci_cd_run_correlation.go). See the Compatibility section of
	// README.md.
	ProducerDomains []reducercontract.Domain
}

// CheckProducerReadinessBeforeLoad captures the readiness signal the handler
// must use for its defer decision. It MUST be called BEFORE the consumer's
// cross-scope load, never after (#5875 P1 caught the same ordering bug on the
// sibling AWS gate).
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
// disables the gate: see UnreadyProducers for why an unasked question must not
// be answered with a deferral.
//
// now must be the SAME clock reading the handler uses for the rest of the pass,
// passed in rather than read again here, so a slow load cannot itself push the
// intent past the elapsed bound.
func CheckProducerReadinessBeforeLoad(
	ctx context.Context,
	readiness ProducerReadiness,
	intent reducercontract.Intent,
	now time.Time,
	crossScopeLookupPlanned bool,
) (ProducerReadinessSignal, error) {
	if readiness == nil {
		// Unwired means "no floor", not "not ready". Defaulting to defer would
		// strand every consumer in a deployment that has not adopted the seam.
		return ProducerReadinessSignal{gateDisabled: true}, nil
	}
	if !crossScopeLookupPlanned {
		return ProducerReadinessSignal{gateDisabled: true}, nil
	}
	dependencies := DependenciesForRegistration(intent.Domain)
	if len(dependencies) == 0 {
		// Not a registered cross-scope consumer. A domain absent from the
		// catalog must behave exactly as it did before this floor existed.
		return ProducerReadinessSignal{gateDisabled: true}, nil
	}
	if anchor := ReadinessCycleAnchor(intent); !anchor.IsZero() &&
		now.Sub(anchor) >= ProducerReadinessMaxWait {
		return ProducerReadinessSignal{gateDisabled: true}, nil
	}
	readyByProducer, err := readiness.CrossScopeProducersReady(
		ctx, intent.Domain, intent.ScopeID, intent.GenerationID,
	)
	if err != nil {
		// Never converted into a readiness miss. A store that cannot answer is a
		// real failure; reporting it as cross_scope_producer_not_ready would
		// hide it in a class that never counts against the retry budget, so it
		// could retry forever without surfacing.
		//
		// It does go through the same transient classifier the consumer's own
		// cross-scope load uses. ClassifyFactLoadError promotes a torn database
		// stream ("unexpected EOF") to the retryable fact_load_transient class.
		// Retryable is a weaker property than non-counting: fact_load_transient
		// is deliberately absent from nonCountingReducerRetryFailureClasses, so
		// attempt_count still increments and the row can still dead-letter. Do
		// not enroll it there to "match" the class above; that would let every
		// transient fact-load failure retry forever without surfacing.
		//
		// Without the promotion a connection reset during this probe is not
		// retryable at all and fails the row outright, where the identical fault
		// one call later would retry -- the probe runs against the same
		// Postgres, in the same handler pass, over the same connection pool.
		// ClassifyFactLoadError returns every other error unchanged, so nothing
		// else about the classification moves.
		return ProducerReadinessSignal{}, factload.ClassifyFactLoadError(err)
	}
	return ProducerReadinessSignal{
		readyByProducer: readyByProducer,
		ProducerDomains: dependencies[0].ProducerDomains,
	}, nil
}

// UnreadyProducers is the floor's decision rule: it returns the declared
// producers that are BOTH unready in the pre-load signal AND absent from the
// post-load resolved evidence, in catalog order. An empty result means this
// pass may commit.
//
// Pairwise, per producer. The readiness answer and the resolved count are both
// keyed by producer domain and compared one producer at a time, because an
// aggregate on either side loses the association: an envelope carries the
// identity of the producer that wrote it, and a bare count does not. The
// version of this rule that used one bool and one int let two
// container_image_identity envelopes stand in for the ci_cd_run_correlation
// output that never arrived, so supply_chain_impact committed findings with no
// deployment context (found reviewing #6093).
//
// A producer missing from readyByProducer reads as unready, so a store that
// forgets a declared producer costs a bounded deferral rather than a wrong
// answer. Resolved evidence still disarms it — a producer that demonstrably
// wrote output on this pass has answered, whatever the readiness probe thought.
//
// A producer that reads ready with an empty join has given the true answer:
// its scopes have activated and still say nothing, so the consumer converges on
// a durable unresolved decision rather than deferring forever.
//
// resolvedByProducer counts what the load returned for the WHOLE BATCH, and
// that distinction is the floor's widest gap. One
// CICDRunCorrelationHandler.Handle pass classifies every CI run in a scope
// generation, and it issues one cross-scope load filtered by the union of every
// run's artifact digests (ciArtifactDigests). So one digest resolving anywhere
// in the batch makes that producer's count non-zero and disarms the floor for
// every other run in the same pass.
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
// residual window. Splitting the count per producer narrows the OTHER axis of
// the same gap -- which producer the evidence came from -- and leaves this one
// exactly where it was.
func UnreadyProducers(
	signal ProducerReadinessSignal,
	resolvedByProducer map[reducercontract.Domain]int,
) []reducercontract.Domain {
	if signal.gateDisabled {
		return nil
	}
	unready := make([]reducercontract.Domain, 0, len(signal.ProducerDomains))
	for _, producer := range signal.ProducerDomains {
		if signal.readyByProducer[producer] {
			continue
		}
		if resolvedByProducer[producer] > 0 {
			continue
		}
		unready = append(unready, producer)
	}
	if len(unready) == 0 {
		return nil
	}
	return unready
}

// SingleProducerResolvedCounts credits one whole-batch resolved count to the
// consumer's single declared producer.
//
// It is the adapter for a consumer whose cross-scope load asks a DEDICATED
// producer reader, so every envelope it returns is that producer's output and a
// plain count carries no ambiguity about which producer wrote it.
// ci_cd_run_correlation is the only such consumer: it declares
// container_image_identity and reads container-image-identity support facts and
// nothing else.
//
// A consumer that grows a second producer gets zero credited counts here rather
// than a count spread across producers it did not come from, so the drift costs
// a bounded deferral instead of the aggregate-count defect this whole rule
// exists to remove. TestCICDRunCorrelationDeclaresExactlyOneProducer fails first
// if that day comes.
func SingleProducerResolvedCounts(
	producers []reducercontract.Domain,
	resolved int,
) map[reducercontract.Domain]int {
	if len(producers) != 1 || resolved <= 0 {
		return nil
	}
	return map[reducercontract.Domain]int{producers[0]: resolved}
}

// ReadinessCycleAnchor returns intent.CycleStartedAt when set, falling back to
// intent.EnqueuedAt.
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
func ReadinessCycleAnchor(intent reducercontract.Intent) time.Time {
	if !intent.CycleStartedAt.IsZero() {
		return intent.CycleStartedAt
	}
	return intent.EnqueuedAt
}

// LogProducerNotReadyDefer records a floor deferral as its own structured log
// line, distinct from a normal correlation pass and from a handler error.
//
// It logs elapsed time against the bound, never attempt_count. This defer's own
// failure class freezes attempt_count (see ProducerReadinessMaxWait),
// so attempt_count reads as the same constant on every occurrence and tells an
// operator nothing about how close the intent is to its terminal fallback.
// elapsed_since_cycle_start against max_wait is the only pair that answers
// "how much longer will this wait".
//
// producer_domains comes from the bounded cross-scope catalog, so it is a small
// closed set, not a high-cardinality label.
func LogProducerNotReadyDefer(
	ctx context.Context,
	logger *slog.Logger,
	intent reducercontract.Intent,
	now time.Time,
	producerDomains []reducercontract.Domain,
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
		slog.Duration("max_wait", ProducerReadinessMaxWait),
	}
	if anchor := ReadinessCycleAnchor(intent); !anchor.IsZero() {
		attrs = append(attrs, slog.Duration("elapsed_since_cycle_start", now.Sub(anchor)))
	}
	logger.LogAttrs(
		ctx, slog.LevelInfo,
		"cross-scope consumer deferred: producer scopes have not activated", attrs...,
	)
}

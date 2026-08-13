// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fixedCrossScopeReadiness answers the readiness question with a canned result
// and counts calls, so a test can prove the lookup was consulted — or skipped.
type fixedCrossScopeReadiness struct {
	ready bool
	// readyByProducer overrides ready for the named producer domains, so a test
	// can express the shape the aggregate bool could not: one producer ready
	// while another is still inside its activation window.
	readyByProducer map[Domain]bool
	err             error
	calls           int
}

func (r *fixedCrossScopeReadiness) CrossScopeProducersReady(
	_ context.Context,
	consumer Domain,
	_ string,
	_ string,
) (CrossScopeProducerReadinessByDomain, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	// Built from the real catalog, so a canned answer always covers exactly the
	// producers the floor is about to ask about — the same contract the
	// production store owes.
	readiness := CrossScopeProducerReadinessByDomain{}
	for _, dependency := range crossScopeDependenciesForRegistration(consumer) {
		for _, producer := range dependency.ProducerDomains {
			ready := r.ready
			if override, named := r.readyByProducer[producer]; named {
				ready = override
			}
			readiness[producer] = ready
		}
	}
	return readiness, nil
}

// scopeOnlyCICDRunFactLoader implements the base FactLoader and the by-kind
// load the handler needs, but NOT activeCICDRunCorrelationFactLoader. It stands
// in for a deployment or an alternative adapter with no cross-scope identity
// seam at all.
type scopeOnlyCICDRunFactLoader struct {
	scopeFacts []facts.Envelope
}

func (s *scopeOnlyCICDRunFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *scopeOnlyCICDRunFactLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

// testCrossScopeNow is a fixed clock reading so the elapsed-time bound is
// exercised deterministically rather than against the wall clock.
var testCrossScopeNow = time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

// freshCrossScopeIntent builds an intent whose repair cycle just began, so the
// elapsed bound is nowhere near reached and the readiness lookup governs.
func freshCrossScopeIntent(domain Domain) Intent {
	return Intent{
		Domain:         domain,
		ScopeID:        "scope:ci",
		GenerationID:   "gen:ci",
		EnqueuedAt:     testCrossScopeNow.Add(-time.Minute),
		CycleStartedAt: testCrossScopeNow.Add(-time.Minute),
	}
}

// runCrossScopeFloor drives BOTH halves of the production floor in the order
// the handler drives them: sample readiness, then combine with the post-load
// resolved count. Every helper-level test below goes through this, so none of
// them can pass against a re-implementation of the logic.
func runCrossScopeFloor(
	t *testing.T,
	readiness CrossScopeProducerReadiness,
	intent Intent,
	now time.Time,
	lookupPlanned bool,
	resolved int,
) error {
	t.Helper()

	signal, err := checkCrossScopeProducerReadinessBeforeLoad(
		context.Background(), readiness, intent, now, lookupPlanned,
	)
	if err != nil {
		return err
	}
	unready := crossScopeUnreadyProducers(
		signal, singleProducerResolvedCounts(signal.producerDomains, resolved),
	)
	if len(unready) == 0 {
		return nil
	}
	return newCrossScopeProducerNotReadyError(intent.Domain, intent.ScopeID, intent.GenerationID, unready)
}

// TestReadinessFloorDefersConsumerWhoseProducerScopesHaveNotActivated is the
// #5709 correctness floor.
//
// The already-claimed consumer is handled elsewhere: the completion fanout
// marks it cross_scope_replay_required and migration 093's trigger rewrites its
// 'succeeded' acknowledgement back to 'pending'. What this floor closes is the
// ACTIVATION window — the producer's reducer row succeeded, but its scope
// generation is activated later, at projector acknowledgement, so the
// consumer's cross-scope read still resolves nothing and would record a durable
// "no answer" nothing later disturbs.
func TestReadinessFloorDefersConsumerWhoseProducerScopesHaveNotActivated(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	err := runCrossScopeFloor(
		t, readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, true, 0,
	)
	if err == nil {
		t.Fatal("want a readiness error: an empty cross-scope join under unactivated producer scopes must not stand as an answer")
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness lookup calls = %d, want 1", readiness.calls)
	}

	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %#v, want crossScopeProducerNotReadyError", err)
	}
	if got := notReady.FailureClass(); got != CrossScopeProducerNotReadyFailureClass {
		t.Fatalf("FailureClass() = %q, want %q", got, CrossScopeProducerNotReadyFailureClass)
	}
	// Retryable is what keeps the queue re-running the consumer instead of
	// dead-lettering it, and the class is enrolled non-counting so the deferral
	// cannot erode the attempt budget.
	if !notReady.Retryable() {
		t.Fatal("Retryable() = false: a deferred consumer must retry, not dead-letter")
	}
	// The message names the bounded producer set, never a uid — a uid could be
	// a redacted identifier.
	if got := notReady.Error(); !strings.Contains(got, string(DomainContainerImageIdentity)) {
		t.Fatalf("Error() = %q, want it to name the declared producer domain", got)
	}
}

// TestReadinessFloorLeavesResolvedConsumersAlone pins the condition that stops
// this becoming a retry loop. A consumer that HAS evidence already has its
// answer, so producer readiness cannot change it.
func TestReadinessFloorLeavesResolvedConsumersAlone(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	if err := runCrossScopeFloor(
		t, readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, true, 3,
	); err != nil {
		t.Fatalf("err = %v, want nil: a consumer with evidence must not defer", err)
	}
}

// TestReadinessFloorDoesNotApplyWhenThereWasNothingToLookUp is the #5709 P1-6
// guard.
//
// FactStore.ListActiveCICDRunCorrelationFacts short-circuits an empty
// digest/image-ref filter to no rows, so a CI run that published no container
// artifacts — normal for any repository whose CI never builds images — has a
// resolved count of zero forever. Without this gate it would defer for the full
// 30-minute bound on a 30-second retry that never backs off, because its own
// failure class freezes attempt_count so the exponential term never grows.
func TestReadinessFloorDoesNotApplyWhenThereWasNothingToLookUp(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	if err := runCrossScopeFloor(
		t, readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, false, 0,
	); err != nil {
		t.Fatalf("err = %v, want nil: a pass with nothing to look up must not defer", err)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0: an unasked question needs no answer", readiness.calls)
	}
}

// TestReadinessFloorConvergesOnceProducerScopesAreQuiescent is the other half
// of correctness. Deferring forever would be its own bug: once the producer
// scopes are active with their projector work drained and the join is STILL
// empty, the empty answer is the true one and the consumer must record it.
func TestReadinessFloorConvergesOnceProducerScopesAreQuiescent(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: true}
	if err := runCrossScopeFloor(
		t, readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, true, 0,
	); err != nil {
		t.Fatalf("err = %v, want nil: a genuinely empty join under quiescent producers is a real answer", err)
	}
}

// TestReadinessFloorIgnoresDomainsOutsideTheCatalog keeps the floor from
// changing any domain that never declared a cross-scope dependency.
func TestReadinessFloorIgnoresDomainsOutsideTheCatalog(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	// DomainContainerImageIdentity is a PRODUCER in the catalog, not a
	// consumer, so it declares no dependency and must not defer.
	if err := runCrossScopeFloor(
		t, readiness, freshCrossScopeIntent(DomainContainerImageIdentity), testCrossScopeNow, true, 0,
	); err != nil {
		t.Fatalf("err = %v, want nil for a domain with no declared dependency", err)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0", readiness.calls)
	}
}

// TestReadinessFloorWithoutASeamDoesNotDefer proves the floor is opt-in by
// wiring. Treating an unwired seam as "not ready" would strand every consumer
// in a deployment that has not adopted it.
func TestReadinessFloorWithoutASeamDoesNotDefer(t *testing.T) {
	t.Parallel()

	if err := runCrossScopeFloor(
		t, nil, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, true, 0,
	); err != nil {
		t.Fatalf("err = %v, want nil when no readiness seam is wired", err)
	}
}

// TestReadinessFloorSurfacesLookupErrorsAsThemselves keeps a broken readiness
// store out of the non-counting class.
//
// cross_scope_producer_not_ready is exempt from the retry budget, so a store
// failure reported as a readiness miss would retry forever without ever
// dead-lettering or surfacing. It must stay classified on its own merits.
func TestReadinessFloorSurfacesLookupErrorsAsThemselves(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("readiness store unavailable")
	err := runCrossScopeFloor(
		t,
		&fixedCrossScopeReadiness{err: sentinel},
		freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, true, 0,
	)
	// Identity, not errors.Is: classifyFactLoadError returns every non-EOF error
	// verbatim, and errors.Is would pass just as happily if the floor started
	// wrapping the store failure in a class of its own.
	if err != sentinel {
		t.Fatalf("err = %#v, want the lookup error returned verbatim", err)
	}
	var notReady crossScopeProducerNotReadyError
	if errors.As(err, &notReady) {
		t.Fatal("a readiness-store failure must not be reported as a readiness miss: that class never counts against the retry budget")
	}
}

// TestReadinessFloorClassifiesATornStreamAsTransient keeps the readiness probe's
// retry accounting matching the cross-scope load that runs immediately after it.
//
// Both talk to the same Postgres over the same pool inside one handler pass, and
// the load routes its errors through classifyFactLoadError, which promotes a
// torn database stream to the retryable fact_load_transient class. That class
// still counts against the retry budget -- it is not enrolled in
// nonCountingReducerRetryFailureClasses. Left raw, a connection reset during the
// probe is not retryable at all and fails the row outright, where the identical
// fault one call later would retry.
func TestReadinessFloorClassifiesATornStreamAsTransient(t *testing.T) {
	t.Parallel()

	torn := fmt.Errorf("query producer scope quiescence: %w", io.ErrUnexpectedEOF)
	err := runCrossScopeFloor(
		t,
		&fixedCrossScopeReadiness{err: torn},
		freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, true, 0,
	)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %#v, want the torn-stream cause preserved", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("err = %#v, want a classified failure", err)
	}
	if got := classified.FailureClass(); got != "fact_load_transient" {
		t.Fatalf("FailureClass() = %q, want %q", got, "fact_load_transient")
	}
	// Still not a readiness miss: that class never counts against the retry
	// budget at all, which is a different and much stronger exemption.
	var notReady crossScopeProducerNotReadyError
	if errors.As(err, &notReady) {
		t.Fatal("a transient store fault must not be reported as a readiness miss")
	}
}

// TestReadinessFloorConvergesOnceTheElapsedBoundIsReached is the terminal
// fallback, and it is not optional.
//
// CrossScopeProducerNotReadyFailureClass is enrolled non-counting, so the queue
// freezes attempt_count and never dead-letters a row in this class. A producer
// scope that is permanently absent or permanently stuck would therefore leave
// its consumers deferring silently forever. That is worse than the durable
// empty answer this floor prevents — an empty answer is visible and repairable.
func TestReadinessFloorConvergesOnceTheElapsedBoundIsReached(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	intent := freshCrossScopeIntent(DomainCICDRunCorrelation)
	intent.CycleStartedAt = testCrossScopeNow.Add(-crossScopeProducerReadinessMaxWait)
	intent.EnqueuedAt = intent.CycleStartedAt

	if err := runCrossScopeFloor(t, readiness, intent, testCrossScopeNow, true, 0); err != nil {
		t.Fatalf("err = %v, want nil at the bound: the consumer must commit its best available answer", err)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0 past the bound", readiness.calls)
	}
}

// TestReadinessFloorBoundIsNotDrivenByAttemptCount pins the reason the bound is
// time-based. The sibling AWS gate shipped an attempt-count bound first and had
// to be re-proven against the real queue: this class freezes attempt_count, so
// a count-based bound reads the same value forever and can never fire.
func TestReadinessFloorBoundIsNotDrivenByAttemptCount(t *testing.T) {
	t.Parallel()

	intent := freshCrossScopeIntent(DomainCICDRunCorrelation)
	// The frozen value a retrying row in a non-counting class reports forever.
	intent.AttemptCount = 1

	err := runCrossScopeFloor(
		t, &fixedCrossScopeReadiness{ready: false}, intent, testCrossScopeNow, true, 0,
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %#v, want a deferral: a high attempt count must not substitute for elapsed time", err)
	}
}

// TestReadinessFloorGivesAReopenedRowAFreshWindow proves the anchor choice.
//
// A maintenance pass reopens these rows routinely. EnqueuedAt comes from
// fact_work_items.created_at and is immutable across a reopen, so anchoring on
// it alone would read as "already past the bound" on the first claim of any
// reopened row — skipping the readiness lookup and committing immediately, with
// no grace window at all.
func TestReadinessFloorGivesAReopenedRowAFreshWindow(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	intent := freshCrossScopeIntent(DomainCICDRunCorrelation)
	// Originally enqueued days ago; reopened one minute ago.
	intent.EnqueuedAt = testCrossScopeNow.Add(-72 * time.Hour)
	intent.CycleStartedAt = testCrossScopeNow.Add(-time.Minute)

	err := runCrossScopeFloor(t, readiness, intent, testCrossScopeNow, true, 0)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %#v, want a deferral: a reopened row gets a fresh window", err)
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness lookup calls = %d, want 1: the bound must not skip the lookup on a reopened row", readiness.calls)
	}
}

// TestReadinessFloorTreatsAZeroAnchorAsUnknownNotInfinite guards the arithmetic.
// Subtracting a zero time.Time from now reads as tens of thousands of hours and
// would fire the terminal fallback on the very first defer.
func TestReadinessFloorTreatsAZeroAnchorAsUnknownNotInfinite(t *testing.T) {
	t.Parallel()

	intent := Intent{Domain: DomainCICDRunCorrelation, ScopeID: "scope:ci", GenerationID: "gen:ci"}
	err := runCrossScopeFloor(
		t, &fixedCrossScopeReadiness{ready: false}, intent, testCrossScopeNow, true, 0,
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %#v, want a deferral: an unknown anchor must not read as infinitely elapsed", err)
	}
}

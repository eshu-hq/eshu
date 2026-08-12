// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fixedCrossScopeReadiness answers the readiness question with a canned result
// and counts calls, so a test can prove the lookup was consulted — or skipped.
type fixedCrossScopeReadiness struct {
	ready bool
	err   error
	calls int
}

func (r *fixedCrossScopeReadiness) CrossScopeProducersReady(
	context.Context,
	Domain,
	string,
	string,
) (bool, error) {
	r.calls++
	return r.ready, r.err
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

// TestReadinessFloorDefersConsumerWhoseProducersHaveNotCommitted is the #5709
// correctness floor.
//
// The producer-completion fanout re-runs a consumer after a producer
// acknowledges. It cannot help a consumer that was ALREADY claimed when the
// producer finished: that consumer resolves nothing, records a durable "no
// answer", and nothing later disturbs it. Deferring is what closes that gap.
func TestReadinessFloorDefersConsumerWhoseProducersHaveNotCommitted(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	err := deferWhenCrossScopeProducersNotReady(
		context.Background(), readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, 0,
	)
	if err == nil {
		t.Fatal("want a readiness error: an empty cross-scope join with uncommitted producers must not stand as an answer")
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
	if got := notReady.Error(); got == "" {
		t.Fatal("Error() is empty; it must name the consumer, producers, scope and generation")
	}
}

// TestReadinessFloorLeavesResolvedConsumersAlone pins the condition that stops
// this becoming a retry loop. A consumer that HAS evidence already has its
// answer, so producer readiness is moot and must not even be consulted.
func TestReadinessFloorLeavesResolvedConsumersAlone(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	if err := deferWhenCrossScopeProducersNotReady(
		context.Background(), readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, 3,
	); err != nil {
		t.Fatalf("err = %v, want nil: a consumer with evidence must not defer", err)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0: readiness is moot once the join resolved", readiness.calls)
	}
}

// TestReadinessFloorConvergesOnceProducersAreQuiescent is the other half of
// correctness. Deferring forever would be its own bug: once producers have
// committed and the join is STILL empty, the empty answer is the true one and
// the consumer must record it.
func TestReadinessFloorConvergesOnceProducersAreQuiescent(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: true}
	if err := deferWhenCrossScopeProducersNotReady(
		context.Background(), readiness, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, 0,
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
	if err := deferWhenCrossScopeProducersNotReady(
		context.Background(), readiness, freshCrossScopeIntent(DomainContainerImageIdentity), testCrossScopeNow, 0,
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

	if err := deferWhenCrossScopeProducersNotReady(
		context.Background(), nil, freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, 0,
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
	err := deferWhenCrossScopeProducersNotReady(
		context.Background(),
		&fixedCrossScopeReadiness{err: sentinel},
		freshCrossScopeIntent(DomainCICDRunCorrelation), testCrossScopeNow, 0,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %#v, want the lookup error unwrapped", err)
	}
	var notReady crossScopeProducerNotReadyError
	if errors.As(err, &notReady) {
		t.Fatal("a readiness-store failure must not be reported as a readiness miss: that class never counts against the retry budget")
	}
}

// TestCICDRunCorrelationDefersWhenIdentityProducerHasNotCommitted proves the
// floor at the HANDLER, not just in the helper. A unit test of the helper alone
// would pass while the handler never called it — which is exactly the state
// #5709 found the codebase in: the error type existed and nothing returned it.
//
// The shape is the racy one the issue describes. A CI run and its artifact are
// present in this scope, but the container_image_identity output the
// correlation needs lives in the OCI scope and has not committed yet, so the
// cross-scope load returns nothing. Without the floor the handler writes a
// durable "no correlation" that no later producer completion disturbs.
func TestCICDRunCorrelationDefersWhenIdentityProducerHasNotCommitted(t *testing.T) {
	t.Parallel()

	loader := &stubCICDRunCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-early", "github_actions", "repo-api", "abc123"),
			ciArtifactFact("artifact-early", "run-early", testCICDDigest),
		},
		// The producer has not committed: the cross-scope load resolves nothing.
		active: nil,
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cicd-early",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-early:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed before identity committed",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want a readiness deferral: an early correlation must not record an empty answer")
	}
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("Handle() error = %#v, want crossScopeProducerNotReadyError", err)
	}
	if got := notReady.FailureClass(); got != CrossScopeProducerNotReadyFailureClass {
		t.Fatalf("FailureClass() = %q, want %q", got, CrossScopeProducerNotReadyFailureClass)
	}
	// Nothing durable may be written on a deferral. Writing then deferring
	// would leave the empty answer behind for the retry to find.
	if writer.calls != 0 {
		t.Fatalf("WriteCICDRunCorrelations() calls = %d, want 0 on a deferral", writer.calls)
	}
}

// TestCICDRunCorrelationStillWritesWhenIdentityHasCommitted is the companion
// guard. The floor must not turn a working correlation into a retry loop.
func TestCICDRunCorrelationStillWritesWhenIdentityHasCommitted(t *testing.T) {
	t.Parallel()

	loader := &stubCICDRunCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-exact", "github_actions", "repo-api", "abc123"),
			ciArtifactFact("artifact-exact", "run-exact", testCICDDigest),
		},
		active: []facts.Envelope{
			containerImageIdentityFact("image-identity", "repo-api", "registry.example.com/team/api@"+testCICDDigest, testCICDDigest),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	readiness := &fixedCrossScopeReadiness{ready: false}
	handler := CICDRunCorrelationHandler{
		FactLoader: loader,
		Writer:     writer,
		// Deliberately "not ready": a resolved join must win over readiness,
		// so this proves the order of the checks rather than trusting it.
		ProducerReadiness: readiness,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-cicd-ready",
		ScopeID:      "ci://github-actions/acme/api",
		GenerationID: "run-exact:1",
		SourceSystem: "ci_cd_run",
		Domain:       DomainCICDRunCorrelation,
		Cause:        "ci run observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil when the cross-scope join resolved", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0: a resolved join makes readiness moot", readiness.calls)
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

	if err := deferWhenCrossScopeProducersNotReady(
		context.Background(), readiness, intent, testCrossScopeNow, 0,
	); err != nil {
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

	err := deferWhenCrossScopeProducersNotReady(
		context.Background(), &fixedCrossScopeReadiness{ready: false}, intent, testCrossScopeNow, 0,
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

	err := deferWhenCrossScopeProducersNotReady(
		context.Background(), readiness, intent, testCrossScopeNow, 0,
	)
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
	err := deferWhenCrossScopeProducersNotReady(
		context.Background(), &fixedCrossScopeReadiness{ready: false}, intent, testCrossScopeNow, 0,
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %#v, want a deferral: an unknown anchor must not read as infinitely elapsed", err)
	}
}

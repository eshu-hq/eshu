// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// Handler-level proof for the #5709 cross-scope readiness floor. The helper
// tests live in cross_scope_readiness_floor_test.go, along with the shared
// stubs both files use; these drive CICDRunCorrelationHandler.Handle end to
// end, because a floor that is correct in isolation and never called by the
// handler is exactly the state #5709 found the codebase in.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestCICDRunCorrelationDefersWhenIdentityProducerScopeHasNotActivated proves
// the floor at the HANDLER, not just in the helper. A unit test of the helper
// alone would pass while the handler never called it — which is exactly the
// state #5709 found the codebase in: the error type existed and nothing
// returned it.
//
// A CI run and its artifact are present in this scope, but the
// container_image_identity output the correlation needs is published by the OCI
// registry scope, whose generation has not activated, so the cross-scope load
// returns nothing. Without the floor the handler writes a durable "no
// correlation" that no later producer completion disturbs.
func TestCICDRunCorrelationDefersWhenIdentityProducerScopeHasNotActivated(t *testing.T) {
	t.Parallel()

	loader := &stubCICDRunCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-early", "github_actions", "repo-api", "abc123"),
			ciArtifactFact("artifact-early", "run-early", testCICDDigest),
		},
		// The producer scope has not activated: the cross-scope load resolves
		// nothing.
		active: nil,
	}
	writer := &recordingCICDRunCorrelationWriter{}
	logs := &bytes.Buffer{}
	handler := CICDRunCorrelationHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
		Logger:            slog.New(slog.NewTextHandler(logs, nil)),
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:       "intent-cicd-early",
		ScopeID:        "ci://github-actions/acme/api",
		GenerationID:   "run-early:1",
		SourceSystem:   "ci_cd_run",
		Domain:         DomainCICDRunCorrelation,
		Cause:          "ci run observed before the identity scope activated",
		CycleStartedAt: time.Now().Add(-time.Minute),
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
	// The deferral must be legible without the metric an operator does not
	// have: attempt_count is frozen for this class, so elapsed-versus-bound is
	// the only pair that says how much longer this consumer will wait.
	for _, want := range []string{
		"cross-scope consumer deferred",
		"producer_domains=" + string(DomainContainerImageIdentity),
		"elapsed_since_cycle_start=",
		"max_wait=",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("defer log missing %q:\n%s", want, logs.String())
		}
	}
}

// TestCICDRunCorrelationDefersDespiteProducerActivatingDuringTheLoad is the
// #5709 P1-3 ordering guard.
//
// The readiness signal is sampled BEFORE the cross-scope load. If it were
// sampled after, a producer generation activating in the window between the
// load and the check would make the store answer "ready" about a snapshot the
// load had already taken without it, and the handler would durably write an
// empty correlation nothing later repairs.
//
// The stub flips readiness to "ready" while the load runs, exactly like a
// producer activating mid-pass. The handler must still defer, because the
// evidence it holds predates that activation.
func TestCICDRunCorrelationDefersDespiteProducerActivatingDuringTheLoad(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	loader := &stubCICDRunCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-race", "github_actions", "repo-api", "abc123"),
			ciArtifactFact("artifact-race", "run-race", testCICDDigest),
		},
		active: nil,
		onActiveLoad: func() {
			// The producer scope activates here, after the readiness sample and
			// during the load that already missed it.
			readiness.ready = true
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readiness,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:       "intent-cicd-race",
		ScopeID:        "ci://github-actions/acme/api",
		GenerationID:   "run-race:1",
		SourceSystem:   "ci_cd_run",
		Domain:         DomainCICDRunCorrelation,
		Cause:          "producer activates mid-pass",
		CycleStartedAt: time.Now().Add(-time.Minute),
	})
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("Handle() error = %#v, want a deferral: readiness must be sampled before the load, not after", err)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteCICDRunCorrelations() calls = %d, want 0: a stale empty answer must not be written", writer.calls)
	}
}

// TestCICDRunCorrelationDoesNotDeferARunWithNoImageArtifacts is the handler-level
// #5709 P1-6 guard: a run with no artifact digests and no workflow image refs
// has nothing to look up, so the empty join says nothing about the producer.
func TestCICDRunCorrelationDoesNotDeferARunWithNoImageArtifacts(t *testing.T) {
	t.Parallel()

	loader := &stubCICDRunCorrelationFactLoader{
		scopeFacts: []facts.Envelope{
			ciRunFact("run-no-image", "github_actions", "repo-api", "abc123"),
		},
	}
	writer := &recordingCICDRunCorrelationWriter{}
	readiness := &fixedCrossScopeReadiness{ready: false}
	handler := CICDRunCorrelationHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readiness,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:       "intent-cicd-no-image",
		ScopeID:        "ci://github-actions/acme/api",
		GenerationID:   "run-no-image:1",
		SourceSystem:   "ci_cd_run",
		Domain:         DomainCICDRunCorrelation,
		Cause:          "ci run with no container artifacts",
		CycleStartedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil: a run that publishes no images must not defer", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteCICDRunCorrelations() calls = %d, want 1", writer.calls)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0 when there is nothing to look up", readiness.calls)
	}
	if loader.activeCall != 0 {
		t.Fatalf("cross-scope load calls = %d, want 0 for an empty filter", loader.activeCall)
	}
}

// TestCICDRunCorrelationDoesNotDeferWithoutTheCrossScopeLoaderSeam is the
// #5709 P2-5 guard. A FactLoader that does not implement the cross-scope seam
// returns no envelopes without querying anything, so the empty result is a
// missing capability, not a producer that has not activated.
func TestCICDRunCorrelationDoesNotDeferWithoutTheCrossScopeLoaderSeam(t *testing.T) {
	t.Parallel()

	writer := &recordingCICDRunCorrelationWriter{}
	readiness := &fixedCrossScopeReadiness{ready: false}
	handler := CICDRunCorrelationHandler{
		FactLoader: &scopeOnlyCICDRunFactLoader{
			scopeFacts: []facts.Envelope{
				ciRunFact("run-no-seam", "github_actions", "repo-api", "abc123"),
				ciArtifactFact("artifact-no-seam", "run-no-seam", testCICDDigest),
			},
		},
		Writer:            writer,
		ProducerReadiness: readiness,
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:       "intent-cicd-no-seam",
		ScopeID:        "ci://github-actions/acme/api",
		GenerationID:   "run-no-seam:1",
		SourceSystem:   "ci_cd_run",
		Domain:         DomainCICDRunCorrelation,
		Cause:          "fact loader without the cross-scope seam",
		CycleStartedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil: a missing loader seam is not a producer readiness miss", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteCICDRunCorrelations() calls = %d, want 1", writer.calls)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0 without a cross-scope loader", readiness.calls)
	}
}

// testCICDWaitingDigest belongs to a run whose container image identity has not
// been published yet, so no cross-scope envelope in the batch matches it.
const testCICDWaitingDigest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

// TestCICDRunCorrelationDoesNotDeferABatchWhereAnotherRunResolved pins the
// floor's widest known gap, so it stays a documented property instead of
// something the next maintainer rediscovers from a wrong correlation.
//
// Handle classifies every CI run in a scope generation in one pass, and asks the
// cross-scope loader one question filtered by the union of all their artifact
// digests. crossScopeProducerDeferral then reads that one batch-wide resolved
// count. One run resolving therefore disarms the floor for every other run in
// the same pass.
//
// Here run-resolved's digest has a published identity and run-waiting's does
// not. The batch does not defer, and run-waiting takes a durable `derived`
// correlation. The second half of the test shows that is the wrong answer: with
// its identity present the same run is `exact` and carries a canonical write.
// So the floor let a run commit a weaker answer than its producer's evidence
// would have given it, which is the #5709 defect.
//
// Closing this needs per-digest readiness, a different contract than #5709
// specifies and not built; see the residual-window section of
// docs/internal/evidence/5709-cross-scope-readiness-floor.md.
//
// If the batch-wide count were ever narrowed to a per-run predicate, this test
// fails: run-waiting would defer and Handle would return
// crossScopeProducerNotReadyError instead of writing.
func TestCICDRunCorrelationDoesNotDeferABatchWhereAnotherRunResolved(t *testing.T) {
	t.Parallel()

	batchFacts := []facts.Envelope{
		ciRunFact("run-resolved", "github_actions", "repo-api", "abc123"),
		ciArtifactFact("artifact-resolved", "run-resolved", testCICDDigest),
		ciRunFact("run-waiting", "github_actions", "repo-api", "def456"),
		ciArtifactFact("artifact-waiting", "run-waiting", testCICDWaitingDigest),
	}
	resolvedIdentity := containerImageIdentityFact(
		"image-identity-resolved", "repo-api",
		"registry.example.com/team/api@"+testCICDDigest, testCICDDigest,
	)
	waitingIdentity := containerImageIdentityFact(
		"image-identity-waiting", "repo-api",
		"registry.example.com/team/worker@"+testCICDWaitingDigest, testCICDWaitingDigest,
	)

	// The load is filtered by BOTH digests and comes back with identity for only
	// the first. That is a resolved count of 1 for the whole batch.
	partial := runCICDBatchUnderTheFloor(t, batchFacts, []facts.Envelope{resolvedIdentity})
	if got := partial["github_actions:run-resolved:1"].Outcome; got != CICDRunCorrelationExact {
		t.Fatalf("run-resolved outcome = %q, want %q", got, CICDRunCorrelationExact)
	}
	if got := partial["github_actions:run-waiting:1"].Outcome; got != CICDRunCorrelationDerived {
		t.Fatalf(
			"run-waiting outcome = %q, want %q: the batch-wide resolved count disarms the floor for every run in the pass",
			got, CICDRunCorrelationDerived,
		)
	}

	// Same batch, same "not ready" signal, with the waiting run's identity also
	// published. This is what run-waiting's answer above should have been.
	complete := runCICDBatchUnderTheFloor(t, batchFacts, []facts.Envelope{resolvedIdentity, waitingIdentity})
	if got := complete["github_actions:run-waiting:1"].Outcome; got != CICDRunCorrelationExact {
		t.Fatalf("run-waiting outcome with its identity present = %q, want %q", got, CICDRunCorrelationExact)
	}
	if got := complete["github_actions:run-waiting:1"].CanonicalWrites; got != 1 {
		t.Fatalf("run-waiting canonical writes with its identity present = %d, want 1", got)
	}
}

// runCICDBatchUnderTheFloor drives one Handle pass with the floor armed and the
// producer reported NOT ready, and returns the decisions it committed keyed by
// run. It fails the test if the pass defers, so every caller is asserting on a
// batch the floor let through.
func runCICDBatchUnderTheFloor(
	t *testing.T,
	scopeFacts []facts.Envelope,
	active []facts.Envelope,
) map[string]CICDRunCorrelationDecision {
	t.Helper()

	writer := &recordingCICDRunCorrelationWriter{}
	handler := CICDRunCorrelationHandler{
		FactLoader: &stubCICDRunCorrelationFactLoader{
			scopeFacts: scopeFacts,
			active:     active,
		},
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
	}

	if _, err := handler.Handle(context.Background(), Intent{
		IntentID:       "intent-cicd-mixed-batch",
		ScopeID:        "ci://github-actions/acme/api",
		GenerationID:   "run-mixed:1",
		SourceSystem:   "ci_cd_run",
		Domain:         DomainCICDRunCorrelation,
		Cause:          "one run resolved, one still waiting on its producer",
		CycleStartedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Handle() error = %v, want nil: the floor is armed per batch, and this batch resolved something", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteCICDRunCorrelations() calls = %d, want 1", writer.calls)
	}
	return cicdDecisionsByRun(writer.write.Decisions)
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
		IntentID:       "intent-cicd-ready",
		ScopeID:        "ci://github-actions/acme/api",
		GenerationID:   "run-exact:1",
		SourceSystem:   "ci_cd_run",
		Domain:         DomainCICDRunCorrelation,
		Cause:          "ci run observed",
		CycleStartedAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil when the cross-scope join resolved", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
}

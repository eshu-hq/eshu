// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// Handler-level proof for the #5709 cross-scope readiness floor on the SECOND
// registered consumer, supply_chain_impact. The floor helpers and their unit
// tests live in cross_scope_readiness_floor.go and
// cross_scope_readiness_floor_test.go; these drive
// SupplyChainImpactHandler.Handle end to end, because a floor that is correct
// in isolation and never called by the handler is exactly the state #5709 found
// the ci_cd_run_correlation consumer in, and the state this consumer was left
// in when the floor first shipped.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// scopeOnlySupplyChainImpactFactLoader implements the base FactLoader but NOT
// activeSupplyChainImpactFactLoader. It stands in for a deployment or an
// alternative adapter with no cross-scope active-evidence seam at all.
type scopeOnlySupplyChainImpactFactLoader struct {
	scopeFacts []facts.Envelope
}

func (s *scopeOnlySupplyChainImpactFactLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

func (s *scopeOnlySupplyChainImpactFactLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), s.scopeFacts...), nil
}

// supplyChainImpactIntentUnderTheFloor builds an intent whose repair cycle just
// began, so the 30-minute elapsed bound is nowhere near reached and the
// readiness lookup is what governs the decision.
func supplyChainImpactIntentUnderTheFloor(cause string) Intent {
	return Intent{
		IntentID:       "intent-supply-chain-floor",
		ScopeID:        "vuln://osv/acme",
		GenerationID:   "gen:vuln:1",
		SourceSystem:   "osv",
		Domain:         DomainSupplyChainImpact,
		Cause:          cause,
		CycleStartedAt: time.Now().Add(-time.Minute),
		EnqueuedAt:     time.Now().Add(-time.Minute),
	}
}

// readinessWithOnlyUnready reports every declared producer ready EXCEPT the
// named one.
//
// The counting tests each isolate ONE producer's rule, and since the floor
// evaluates producers separately, a test that left the other producer unready
// would defer for a reason it is not about. Naming the producer under test here
// keeps the assertion honest: the pass commits or defers on that producer's
// evidence alone.
func readinessWithOnlyUnready(unready Domain) *fixedCrossScopeReadiness {
	return &fixedCrossScopeReadiness{
		ready:           true,
		readyByProducer: map[Domain]bool{unready: false},
	}
}

// supplyChainImpactArmedScopeFacts is a vulnerability generation whose filter
// carries a repository ID, which is one of the three filter dimensions
// listActiveSupplyChainImpactFactsQuery can match a producer fact on. That is
// what arms the floor for this pass.
func supplyChainImpactArmedScopeFacts() []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
		vulnerabilityAffectedPackageFact(
			"affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0",
		),
		packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
	}
}

// TestSupplyChainImpactDefersWhenProducerScopesHaveNotActivated is the #5709
// floor at the supply_chain_impact HANDLER.
//
// This consumer reads container_image_identity output for repository anchoring
// and ci_cd_run_correlation output for deployment context, both published by
// scopes other than its own vulnerability-intelligence scope. Its cross-scope
// read (listActiveSupplyChainImpactFactsQuery) joins ingestion_scopes on
// active_generation_id and requires generation.status = 'active', so a producer
// generation that has not activated yet contributes nothing. Without the floor
// the handler writes durable findings computed without that evidence, and no
// later producer completion disturbs them.
func TestSupplyChainImpactDefersWhenProducerScopesHaveNotActivated(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		// The producer scopes have not activated: the cross-scope load returns
		// no producer-owned envelope.
		active: nil,
	}
	writer := &recordingSupplyChainImpactWriter{}
	logs := &bytes.Buffer{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
		Logger:            slog.New(slog.NewTextHandler(logs, nil)),
	}

	_, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("vulnerability observed before the identity scope activated"),
	)
	if err == nil {
		t.Fatal("Handle() error = nil, want a readiness deferral: an early impact pass must not record findings computed without its producers")
	}
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("Handle() error = %#v, want crossScopeProducerNotReadyError", err)
	}
	if got := notReady.FailureClass(); got != CrossScopeProducerNotReadyFailureClass {
		t.Fatalf("FailureClass() = %q, want %q", got, CrossScopeProducerNotReadyFailureClass)
	}
	// Nothing durable may be written on a deferral. Writing then deferring
	// would leave the under-evidenced findings behind for the retry to find.
	if writer.calls != 0 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 0 on a deferral", writer.calls)
	}
	// attempt_count is frozen for this failure class, so elapsed-versus-bound is
	// the only pair that tells an operator how much longer this consumer waits.
	// Both declared producers are named, because either one can hold it back.
	for _, want := range []string{
		"cross-scope consumer deferred",
		string(DomainContainerImageIdentity),
		string(DomainCICDRunCorrelation),
		"elapsed_since_cycle_start=",
		"max_wait=",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("defer log missing %q:\n%s", want, logs.String())
		}
	}
}

// TestSupplyChainImpactDefersDespiteProducerActivatingDuringTheLoad is the
// ordering guard, and it is the same window #5875 P1 found on the sibling AWS
// gate and #5709 P1-3 found on the CI/CD consumer.
//
// The readiness signal is sampled BEFORE the cross-scope active-evidence load.
// Sampled after, a producer generation activating in between makes the store
// answer "ready" about a snapshot the load had already taken without it, and
// the handler durably writes findings nothing later repairs. The reopen path
// cannot save them either: the completion fanout's reopen selects 'succeeded'
// rows, and a maintenance pass racing this intent while it is still claimed
// skips it.
//
// The stub flips readiness to "ready" while the load runs, exactly like a
// producer activating mid-pass.
func TestSupplyChainImpactDefersDespiteProducerActivatingDuringTheLoad(t *testing.T) {
	t.Parallel()

	readiness := &fixedCrossScopeReadiness{ready: false}
	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		activeForFilter: func(SupplyChainImpactFactFilter) []facts.Envelope {
			// The producer scope activates here, after the readiness sample and
			// during the load that already missed it.
			readiness.ready = true
			return nil
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readiness,
	}

	_, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("producer activates mid-pass"),
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("Handle() error = %#v, want a deferral: readiness must be sampled before the load, not after", err)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 0: a stale answer must not be written", writer.calls)
	}
}

// TestSupplyChainImpactDoesNotDeferWhenThereIsNothingToLookUp is the
// nothing-to-look-up guard.
//
// listActiveSupplyChainImpactFactsQuery can only match a
// reducer_container_image_identity or reducer_ci_cd_run_correlation row on a
// subject digest, an image ref, or a repository ID. A pass whose filter carries
// none of those three cannot resolve a producer fact however long it waits, so
// its empty producer count says nothing about the producer.
//
// Without this gate such a pass defers for the full 30-minute bound on a
// 30-second retry that never backs off, because this failure class freezes
// attempt_count so the exponential term never grows. That is roughly 60 no-op
// claims per row per repair cycle against the write-hot fact_work_items table.
func TestSupplyChainImpactDoesNotDeferWhenThereIsNothingToLookUp(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		// CVE and affected-package evidence only: the filter carries package
		// IDs, purls, CVE IDs and advisory IDs, and no digest, image ref, or
		// repository ID.
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
			vulnerabilityAffectedPackageFact(
				"affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0",
			),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	readiness := &fixedCrossScopeReadiness{ready: false}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: readiness,
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("no digest, image ref, or repository to look a producer up by"),
	); err != nil {
		t.Fatalf("Handle() error = %v, want nil: a pass that cannot reach a producer fact must not defer", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0 when there is nothing to look up", readiness.calls)
	}
}

// TestSupplyChainImpactDoesNotDeferWithoutTheCrossScopeLoaderSeam proves the
// floor stays opt-in by capability. A FactLoader that does not implement
// activeSupplyChainImpactFactLoader returns no envelopes without querying
// anything, so the empty result is a missing capability, not a producer that has
// not activated.
func TestSupplyChainImpactDoesNotDeferWithoutTheCrossScopeLoaderSeam(t *testing.T) {
	t.Parallel()

	writer := &recordingSupplyChainImpactWriter{}
	readiness := &fixedCrossScopeReadiness{ready: false}
	handler := SupplyChainImpactHandler{
		FactLoader: &scopeOnlySupplyChainImpactFactLoader{
			scopeFacts: supplyChainImpactArmedScopeFacts(),
		},
		Writer:            writer,
		ProducerReadiness: readiness,
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("fact loader without the cross-scope seam"),
	); err != nil {
		t.Fatalf("Handle() error = %v, want nil: a missing loader seam is not a producer readiness miss", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
	if readiness.calls != 0 {
		t.Fatalf("readiness lookup calls = %d, want 0 without a cross-scope loader", readiness.calls)
	}
}

// TestSupplyChainImpactWithoutAReadinessSeamDoesNotDefer keeps a deployment that
// has not adopted the floor working exactly as it did before. A nil seam means
// "no floor", never "not ready".
func TestSupplyChainImpactWithoutAReadinessSeamDoesNotDefer(t *testing.T) {
	t.Parallel()

	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader: &stubSupplyChainImpactFactLoader{scopeFacts: supplyChainImpactArmedScopeFacts()},
		Writer:     writer,
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("no readiness seam wired"),
	); err != nil {
		t.Fatalf("Handle() error = %v, want nil when no readiness seam is wired", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
}

// TestSupplyChainImpactStillWritesWhenTheProducerHasCommitted is the companion
// guard: the floor must not turn a working impact pass into a retry loop.
//
// Readiness is deliberately withheld from container_image_identity, so its
// resolved envelope has to disarm that producer on its own rather than because
// the readiness store and the load agreed. The OTHER declared producer,
// ci_cd_run_correlation, reads ready — which is what leaves this a
// no-deferral case rather than the one-producer-short case below.
func TestSupplyChainImpactStillWritesWhenTheProducerHasCommitted(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		active: []facts.Envelope{
			containerImageIdentityImpactFact("image-identity", testImpactSubjectDigest, testImpactRepositoryID),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader: loader,
		Writer:     writer,
		ProducerReadiness: &fixedCrossScopeReadiness{readyByProducer: map[Domain]bool{
			DomainContainerImageIdentity: false,
			DomainCICDRunCorrelation:     true,
		}},
	}

	if _, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("identity producer has committed"),
	); err != nil {
		t.Fatalf("Handle() error = %v, want nil when the cross-scope join resolved a producer fact", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 1", writer.calls)
	}
}

// TestSupplyChainImpactDefersWhenOnlyOneProducerResolved is the regression guard
// for the aggregate-count defect a round of review on #6093 found.
//
// supply_chain_impact declares two producers. The floor used to hold ONE
// readiness bool and ONE resolved count for both of them, so an envelope from
// either producer satisfied the pair: a pass where container_image_identity had
// published twice and ci_cd_run_correlation had published nothing read exactly
// like a pass where both had answered once. The consumer then durably wrote
// findings with no deployment context at all, which is the #5709 defect wearing
// a different hat.
//
// Two identity envelopes is the shape that makes the arithmetic visible. A
// count of two "covers" two producers only if you never ask which producer each
// envelope came from.
func TestSupplyChainImpactDefersWhenOnlyOneProducerResolved(t *testing.T) {
	t.Parallel()

	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: supplyChainImpactArmedScopeFacts(),
		active: []facts.Envelope{
			containerImageIdentityImpactFact("image-identity-1", testImpactSubjectDigest, testImpactRepositoryID),
			containerImageIdentityImpactFact("image-identity-2", testImpactSubjectDigest, testImpactRepositoryID),
		},
	}
	writer := &recordingSupplyChainImpactWriter{}
	logs := &bytes.Buffer{}
	handler := SupplyChainImpactHandler{
		FactLoader:        loader,
		Writer:            writer,
		ProducerReadiness: &fixedCrossScopeReadiness{ready: false},
		Logger:            slog.New(slog.NewTextHandler(logs, nil)),
	}

	_, err := handler.Handle(
		context.Background(),
		supplyChainImpactIntentUnderTheFloor("only the identity producer answered"),
	)
	var notReady crossScopeProducerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf(
			"Handle() error = %#v, want a deferral: two envelopes from one producer do not make two producers ready",
			err,
		)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteSupplyChainImpactFindings() calls = %d, want 0 on a deferral", writer.calls)
	}
	// The error and the log name the producer that is actually holding this
	// pass back, and not the one that already answered. An operator reading
	// either has to be able to tell which upstream to go look at.
	if !slices.Contains(notReady.ProducerDomains, DomainCICDRunCorrelation) {
		t.Fatalf("producerDomains = %v, want the unresolved ci_cd_run_correlation producer", notReady.ProducerDomains)
	}
	if slices.Contains(notReady.ProducerDomains, DomainContainerImageIdentity) {
		t.Fatalf(
			"producerDomains = %v, want container_image_identity omitted: it resolved output on this pass",
			notReady.ProducerDomains,
		)
	}
	if got := logs.String(); !strings.Contains(got, string(DomainCICDRunCorrelation)) ||
		strings.Contains(got, string(DomainContainerImageIdentity)) {
		t.Fatalf("defer log must name only the blocking producer:\n%s", got)
	}
}

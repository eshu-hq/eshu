// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyChainImpactCrossScopeFloor carries the #5709 floor's state across the
// active-evidence stages: the readiness signal sampled BEFORE the first load,
// the single clock reading both halves share, and the producer-fact count taken
// at the same moment.
//
// One clock reading rather than two, because the elapsed bound must measure how
// long the consumer has waited on its producer, not how long its own loads took.
// Re-reading the clock after the load would let a slow pass push its own intent
// past the 30-minute bound and commit an answer it should have deferred.
type supplyChainImpactCrossScopeFloor struct {
	signal              crossScopeProducerReadinessSignal
	sampledAt           time.Time
	producerFactsBefore int
}

// armCrossScopeProducerFloor samples producer readiness before the first
// active-evidence load.
//
// The order is the whole point. Sampling after the load would let a producer
// generation activate in between, so the readiness store reports "ready" about a
// snapshot the load had already taken without it, and the handler durably writes
// findings computed from that snapshot. Nothing later repairs them: the
// completion fanout's reopen selects 'succeeded' rows, and a maintenance pass
// racing this intent while it is still claimed skips it.
//
// The reverse race is benign. A producer activating BETWEEN this sample and the
// load only means the load reads fresher data than the signal assumed, which can
// add evidence and never remove it, and the post-load producer count is what
// decides.
func (h SupplyChainImpactHandler) armCrossScopeProducerFloor(
	ctx context.Context,
	intent Intent,
	envelopes []facts.Envelope,
	filter SupplyChainImpactFactFilter,
) (supplyChainImpactCrossScopeFloor, error) {
	sampledAt := time.Now()
	signal, err := checkCrossScopeProducerReadinessBeforeLoad(
		ctx, h.ProducerReadiness, intent, sampledAt, h.crossScopeProducerLookupPlanned(filter),
	)
	if err != nil {
		return supplyChainImpactCrossScopeFloor{}, err
	}
	return supplyChainImpactCrossScopeFloor{
		signal:              signal,
		sampledAt:           sampledAt,
		producerFactsBefore: countSupplyChainImpactCrossScopeProducerFacts(envelopes),
	}, nil
}

// crossScopeProducerDeferralAfterLoad combines the pre-load readiness signal
// with the producer facts the cross-scope stages actually resolved, and returns
// the non-counting readiness error when this pass must defer. It returns nil in
// every other case.
//
// It must be called after EVERY stage that can return producer output, which for
// this consumer is three: the until-stable loop, the resolved-digest re-run
// (#5464), and the peer-identity pass (#5468). The resolved-digest stage is
// where a pure OS-package finding's container image identity arrives, long after
// the loop settles, so a count taken earlier defers exactly the findings that
// read exists to serve.
//
// The count is a DELTA, because supplyChainImpactFactKinds also asks the
// intent's OWN scope for both producer fact kinds. An absolute count would let a
// producer fact sitting in the consumer's own vulnerability scope stand in for
// one the cross-scope read resolved, and that says nothing about whether the
// producer's other scopes have activated.
//
// The returned error is deliberately unwrapped so the durable queue reads the
// non-counting failure class off it; SupplyChainImpactHandler.Handle passes it
// through without wrapping.
func (h SupplyChainImpactHandler) crossScopeProducerDeferralAfterLoad(
	ctx context.Context,
	floor supplyChainImpactCrossScopeFloor,
	intent Intent,
	envelopes []facts.Envelope,
) error {
	resolved := countSupplyChainImpactCrossScopeProducerFacts(envelopes) - floor.producerFactsBefore
	deferral := crossScopeProducerDeferral(floor.signal, intent, resolved)
	if deferral == nil {
		return nil
	}
	logCrossScopeProducerNotReadyDefer(ctx, h.Logger, intent, floor.sampledAt, floor.signal.producerDomains)
	return deferral
}

// supplyChainImpactProducerFactKindByDomain maps each producer domain
// crossScopeDependencyCatalog declares for supply_chain_impact to the fact kind
// that producer writes.
//
// The floor needs this because supply_chain_impact's cross-scope read is the
// SHARED active-evidence reader (listActiveSupplyChainImpactFactsQuery), which
// returns twenty-odd fact kinds — sbom.component, vulnerability.*,
// package_registry.*, file. The CI/CD consumer does not need an equivalent: its
// cross-scope load asks a dedicated reader that returns container-image-identity
// support facts and nothing else, so for it any returned envelope is producer
// output and a plain envelope count is the right signal.
//
// Counting every envelope here instead would let a pass that resolved a pile of
// SBOM components and zero producer facts disarm the floor, which is the exact
// case the floor exists for.
//
// The two entries are the two writer constants, not restatements of them:
// container_image_identity_writer.go and ci_cd_run_correlation_writer.go define
// the kinds these producers publish under.
// TestSupplyChainImpactProducerFactKindsCoverEveryDeclaredProducer fails if a
// third producer joins the catalog without an entry here, because that
// producer's output would silently never disarm the floor.
var supplyChainImpactProducerFactKindByDomain = map[Domain]string{
	DomainContainerImageIdentity: containerImageIdentityFactKind,
	DomainCICDRunCorrelation:     cicdRunCorrelationFactKind,
}

// countSupplyChainImpactCrossScopeProducerFacts counts the envelopes written by
// a declared cross-scope producer. The floor reads the DELTA across the
// active-evidence stages rather than this absolute number, so a producer fact
// that happened to live in the consumer's own vulnerability scope cannot stand
// in for one the cross-scope read actually resolved.
func countSupplyChainImpactCrossScopeProducerFacts(envelopes []facts.Envelope) int {
	producerKinds := make(map[string]struct{}, len(supplyChainImpactProducerFactKindByDomain))
	for _, dependency := range crossScopeDependenciesForRegistration(DomainSupplyChainImpact) {
		for _, producer := range dependency.ProducerDomains {
			if kind, ok := supplyChainImpactProducerFactKindByDomain[producer]; ok {
				producerKinds[kind] = struct{}{}
			}
		}
	}
	resolved := 0
	for _, envelope := range envelopes {
		if _, ok := producerKinds[envelope.FactKind]; ok {
			resolved++
		}
	}
	return resolved
}

// crossScopeProducerLookupPlanned reports whether this pass can reach a producer
// fact at all, which is what arms the #5709 readiness floor.
//
// Both false cases produce an empty producer count that says nothing about the
// producer, so neither may be answered with a deferral:
//
//   - No loader implementing the cross-scope seam.
//     loadActiveSupplyChainImpactFacts returns no envelopes without querying,
//     exactly like an unwired readiness seam.
//   - A filter with no dimension listActiveSupplyChainImpactFactsQuery matches a
//     producer row on. That query reaches
//     reducer_container_image_identity and reducer_ci_cd_run_correlation
//     through exactly three predicates: the digest branch (subject_digest,
//     digest, artifact_digest, referrer_digest, resolved_digest against
//     SubjectDigests), image_ref against ImageRefs, and the repository branch,
//     whose fact-kind list names both producer kinds, against RepositoryIDs.
//     Package IDs, purls, CVE IDs, advisory IDs, product criteria, and document
//     IDs cannot match either producer's payload, so a pass carrying only those
//     can never resolve one however long it waits.
//
// Gating on this keeps such a pass from deferring for the full
// crossScopeProducerReadinessMaxWait on a retry that does not back off: this
// deferral's failure class freezes attempt_count, so the exponential term never
// grows and the row re-claims roughly every 30 seconds for 30 minutes, per
// repair cycle, to look up nothing.
//
// The filter is the INITIAL one, computed before the first active-evidence
// load. Later stages can seed dimensions this one lacks — the resolved-digest
// stage (#5464) learns an os_package's scanned digest only after the
// scanner-analysis stage — so a pass whose only producer-reachable dimension
// appears in a later stage reads as "no floor" here. That is the safe
// direction: a missed gate leaves the pre-floor behaviour, where a wrong gate
// would defer a pass that could never resolve anything. It is recorded as a
// residual window in docs/internal/evidence/5709-supply-chain-consumer.md.
func (h SupplyChainImpactHandler) crossScopeProducerLookupPlanned(filter SupplyChainImpactFactFilter) bool {
	if _, ok := h.FactLoader.(activeSupplyChainImpactFactLoader); !ok {
		return false
	}
	return len(filter.SubjectDigests) > 0 || len(filter.ImageRefs) > 0 || len(filter.RepositoryIDs) > 0
}

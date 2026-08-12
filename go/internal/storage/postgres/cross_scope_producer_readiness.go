// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// crossScopeProducerCollectorKindByDomain maps a producer REDUCER domain to the
// collector kind of the ingestion scopes whose ACTIVATION publishes that
// domain's output to a cross-scope consumer.
//
// The mapping is the load-bearing translation in this file, so both entries are
// grounded in code rather than in a plausible-looking string:
//
//   - container_image_identity -> oci_registry. The identity domain's canonical
//     support rows are read back through container_image_identity_current_support_facts_for
//     (migration 092c), which only returns a row once the owning scope's
//     generation is the scope's active generation AND that generation's status
//     is 'active'. The scopes carrying image manifests are the OCI registry
//     collector's, registered with scope.CollectorOCIRegistry by
//     internal/coordinator/oci_registry_scheduler.go and projected by
//     internal/projector/oci_registry_canonical.go.
//   - ci_cd_run_correlation -> ci_cd_run. Its intent is triggered by ci.run
//     evidence (internal/projector/ci_cd_run_correlation_intents.go), emitted by
//     the hosted CI collectors under scope.CollectorCICDRun
//     (internal/collector/cicdrun/ghactionsruntime and .../gitlabciruntime).
//
// A producer domain absent from this map resolves to no collector kind and is
// SKIPPED rather than guessed. That is the known gap, not a silent pass: a
// guessed kind that a deployment never registers would hold every consumer of
// that producer at "not ready" until the elapsed bound, once per repair cycle,
// for a producer scope that was never going to appear.
//
// What this mapping does NOT capture: container_image_identity intents are also
// enqueued in aws, azure, gcp, git, and sbom_attestation scopes (see
// containerImageIdentityCandidateFactKinds in
// internal/projector/container_image_identity_intents.go), so identity output
// can be published by a scope this map does not name. The floor therefore does
// not wait for those. That is under-inclusive by design -- widening it would
// make any mid-ingestion cloud scope anywhere block every CI correlation -- and
// it is recorded in docs/internal/evidence/5709-cross-scope-readiness-floor.md.
var crossScopeProducerCollectorKindByDomain = map[reducer.Domain]scope.CollectorKind{
	reducer.DomainContainerImageIdentity: scope.CollectorOCIRegistry,
	reducer.DomainCICDRunCorrelation:     scope.CollectorCICDRun,
}

// CrossScopeProducerReadinessStore implements
// reducer.CrossScopeProducerReadiness over the shared fact store.
//
// It resolves a consumer's producers from reducer.CrossScopeCompletionEdges --
// the same exported catalog the completion fanout is built from -- rather than
// from a second hand-maintained list here, so the two halves of the contract
// (the re-trigger and the floor) cannot drift apart on WHO the producers are.
//
// Adding a consumer to that catalog does not by itself gate it. Each consumer
// handler opts in explicitly by calling the reducer-side floor helper;
// supply_chain_impact is in the catalog today and is not gated.
type CrossScopeProducerReadinessStore struct {
	DB Queryer
}

// CrossScopeProducersReady reports whether every producer collector kind the
// consumer declares has at least one quiescent-active scope: a scope with an
// active generation and no projector work item still pending, retrying,
// claimed, or running.
//
// That is the question the #5709 residual gap turns on. The already-claimed
// consumer window is closed elsewhere -- cross_scope_completion_fanout.go marks
// a consumer in 'claimed'/'running' with cross_scope_replay_required, and the
// trigger from migration 093 rewrites that row's 'succeeded' acknowledgement
// back to 'pending'. What remains is the ACTIVATION window: the producer's
// reducer row reaches 'succeeded', but its scope generation is activated later,
// at projector acknowledgement. Until that activation lands, the consumer's
// cross-scope read resolves nothing, because
// container_image_identity_current_support_facts_for requires
// scope.active_generation_id = generation.generation_id AND
// generation.status = 'active' (migration 092c). Scope quiescence is the
// observable that closes exactly that window.
//
// The probe runs once per declared producer collector kind and stops at the
// first kind with no quiescent-active scope, so the wired consumer
// (ci_cd_run_correlation, one producer kind) costs exactly one query.
//
// scopeID and generationID are accepted for the interface and for future
// scope-narrowed readiness, but are deliberately not filtered on: the producer
// runs in a DIFFERENT scope from the consumer, which is the whole point of a
// cross-scope dependency, so filtering producer scopes by the consumer's scope
// would match nothing and report ready every time -- a false green that would
// leave the floor silently inert.
func (s CrossScopeProducerReadinessStore) CrossScopeProducersReady(
	ctx context.Context,
	consumer reducer.Domain,
	_ string,
	_ string,
) (bool, error) {
	producerKinds := crossScopeProducerCollectorKindsFor(consumer)
	if len(producerKinds) == 0 {
		// Not a registered cross-scope consumer, or every declared producer is
		// a domain with no resolvable scope kind. Nothing to wait for.
		return true, nil
	}
	if s.DB == nil {
		return false, fmt.Errorf("cross-scope producer readiness database is required")
	}

	for _, kind := range producerKinds {
		quiescent, err := ProducerScopeQuiescence(ctx, s.DB, []string{kind})
		if err != nil {
			return false, fmt.Errorf(
				"check producer scope quiescence for consumer %s producer kind %s: %w", consumer, kind, err,
			)
		}
		if len(quiescent) == 0 {
			return false, nil
		}
	}
	return true, nil
}

// crossScopeProducerCollectorKindsFor returns the producer collector kinds the
// consumer's declared producer domains resolve to, deduplicated and sorted.
func crossScopeProducerCollectorKindsFor(consumer reducer.Domain) []string {
	return crossScopeProducerCollectorKinds(crossScopeProducerDomainsFor(consumer))
}

// crossScopeProducerDomainsFor returns the producer domains the consumer
// declares, deduplicated, derived from the shared completion-edge catalog.
func crossScopeProducerDomainsFor(consumer reducer.Domain) []reducer.Domain {
	seen := make(map[reducer.Domain]struct{})
	producers := make([]reducer.Domain, 0, 2)
	for _, edge := range reducer.CrossScopeCompletionEdges() {
		if edge.Consumer != consumer {
			continue
		}
		if _, ok := seen[edge.Producer]; ok {
			continue
		}
		seen[edge.Producer] = struct{}{}
		producers = append(producers, edge.Producer)
	}
	return producers
}

// crossScopeProducerCollectorKinds resolves producer domains to collector
// kinds, dropping any domain with no mapping, then deduplicates and sorts.
//
// Sorting makes the probe sequence deterministic, which matters because the
// caller stops at the first non-quiescent kind: without a stable order the
// number of queries a consumer runs would depend on map iteration.
func crossScopeProducerCollectorKinds(producers []reducer.Domain) []string {
	kinds := make([]string, 0, len(producers))
	for _, producer := range producers {
		kind, ok := crossScopeProducerCollectorKindByDomain[producer]
		if !ok {
			continue
		}
		kinds = append(kinds, string(kind))
	}
	slices.Sort(kinds)
	return slices.Compact(kinds)
}

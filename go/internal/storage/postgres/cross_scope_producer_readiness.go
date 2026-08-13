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
// SKIPPED rather than guessed. That is a known gap, not a silent pass: the
// consumer commits its best available answer instead of waiting on a scope kind
// nobody has shown it depends on.
//
// What this mapping does NOT capture: container_image_identity intents are also
// enqueued in aws, azure, gcp, git, and sbom_attestation scopes (see
// containerImageIdentityCandidateFactKinds in
// internal/projector/container_image_identity_intents.go), so identity output
// can be published by a scope this map does not name. The floor does not wait
// for those.
//
// The map stays narrow for two reasons, and neither is "a busy cloud scope
// would block everything". CrossScopeProducersReady asks for AT LEAST ONE
// quiescent scope per kind, not all of them, so adding a kind with many scopes
// -- git, with one per repository -- would block almost nothing: one quiescent
// git scope satisfies the whole kind, and there almost always is one. The real
// reasons are narrower.
//
// First, only these two mappings are grounded in code. Each entry above cites
// the scheduler that registers the scope and the projector that publishes it. A
// kind added on a plausible-looking name, or one a deployment never registers,
// is a guess -- and this file used to turn such a guess into a 30-minute
// deferral. (An unregistered kind is now answered as ready rather than blocked,
// so the cost of a wrong guess is a silently missing wait rather than a stall.
// Still a wrong answer.)
//
// Second, per-kind blast radius. Every kind added here becomes a condition
// every consumer of that producer must satisfy on every pass, including one
// more probe query, so the set should grow only with evidence that the missing
// kind actually publishes the output a consumer reads.
//
// The consequence is stated plainly rather than defended: a digest whose
// identity is published by an aws/ECR scope is still answered early, so #5709 is
// narrowed on that path, not closed. See
// docs/internal/evidence/5709-cross-scope-readiness-floor.md.
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
// handler opts in explicitly by calling the reducer-side floor helper. Both
// catalog consumers have now opted in: ci_cd_run_correlation and
// supply_chain_impact.
type CrossScopeProducerReadinessStore struct {
	DB Queryer
}

// CrossScopeProducersReady reports, for EACH producer domain the consumer
// declares, whether that producer's collector kind -- AND THAT THIS DEPLOYMENT
// ACTUALLY REGISTERS -- has at least one quiescent-active scope: a scope with an
// active generation and no projector work item still pending, retrying, claimed,
// or running.
//
// Per producer, not one bool for the set. The consumer compares each answer
// against the evidence that producer actually wrote, and a single bool paired
// with a single evidence count cannot tell "both producers answered once" from
// "one producer answered twice" (see reducer.CrossScopeProducerReadinessByDomain,
// found reviewing #6093).
//
// A kind with no registered scope at all is ready, not blocked. Not every
// deployment runs every collector -- the OCI registry collector needs registry
// credentials, so a deployment indexing repositories whose CI publishes image
// digests may well have none. Treating that absence as "not ready" defers every
// ci_cd_run_correlation intent to the full 30-minute bound, re-claiming roughly
// every 30 seconds with no backoff (this failure class freezes attempt_count),
// which is about 60 no-op claims per row per repair cycle against the write-hot
// fact_work_items table. The sibling gate answers the same way:
// HasPendingStateSnapshotEvidence reports false, meaning ready, when no
// state_snapshot scope exists (aws_cloud_runtime_drift_readiness.go).
//
// Absence and non-quiescence come back from ONE query.
// ProducerScopeQuiescence returns the registered scopes alongside the quiescent
// subset, so telling them apart costs no extra round trip.
//
// The window this targets: the already-claimed consumer is handled elsewhere --
// cross_scope_completion_fanout.go marks a consumer in 'claimed'/'running' with
// cross_scope_replay_required, and the trigger from migration 093 rewrites that
// row's 'succeeded' acknowledgement back to 'pending'. What remains is the
// ACTIVATION window: the producer's reducer row reaches 'succeeded', but its
// scope generation is activated later, at projector acknowledgement, and until
// then the consumer's cross-scope read resolves nothing.
//
// Quiescence is a PROXY for that window, not the same predicate, and the
// difference is worth knowing before relying on it. The consumer reads through
// container_image_identity_current_support_facts_for (migration 092c), which
// requires three things: scope.active_generation_id = the state row's
// generation, generation.status = 'active', and the identity domain's own
// scope-state row carrying an active_set_id. This probe checks only that
// active_generation_id is set and that the scope's PROJECTOR work has drained.
// So it proves the producer scope has published a generation and finished
// projecting it. It does not prove any of the three 092c conditions directly,
// and a producer whose identity reducer has not yet written its support set
// still reads as ready here. That residual window is recorded in
// docs/internal/evidence/5709-cross-scope-readiness-floor.md.
//
// The probe runs at most once per DISTINCT declared producer collector kind,
// and every kind is probed: a per-producer answer cannot short-circuit at the
// first miss the way the old aggregate bool did, because the producers that
// come after it still need answers of their own. So the cost is one query per
// distinct declared kind: ci_cd_run_correlation declares one producer and costs
// one query, supply_chain_impact declares two and costs two. The removed
// short-circuit was worth at most one saved query on a deferring
// supply_chain_impact pass, against a per-producer answer the correctness rule
// needs. Two kinds also means two chances to be held back, so
// supply_chain_impact is strictly more likely to defer than the CI/CD consumer.
//
// A producer domain with no mapped collector kind answers ready rather than
// blocking, for the reason crossScopeProducerCollectorKindByDomain gives: a kind
// nobody has shown the consumer depends on is a guess, and a guessed 30-minute
// deferral is worse than a missing wait.
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
) (reducer.CrossScopeProducerReadinessByDomain, error) {
	producers := crossScopeProducerDomainsFor(consumer)
	readiness := make(reducer.CrossScopeProducerReadinessByDomain, len(producers))
	if len(crossScopeProducerCollectorKinds(producers)) > 0 && s.DB == nil {
		return nil, fmt.Errorf("cross-scope producer readiness database is required")
	}

	// Cached per collector kind so two producers mapping to one kind cost one
	// query, which is what the deduplicating helper used to buy before the
	// answers had to stay per producer.
	readyByKind := make(map[string]bool, len(producers))
	for _, producer := range producers {
		kind, mapped := crossScopeProducerCollectorKindByDomain[producer]
		if !mapped {
			// No resolvable scope kind, so there is no activation to wait for.
			readiness[producer] = true
			continue
		}
		ready, cached := readyByKind[string(kind)]
		if !cached {
			report, err := ProducerScopeQuiescence(ctx, s.DB, []string{string(kind)})
			if err != nil {
				return nil, fmt.Errorf(
					"check producer scope quiescence for consumer %s producer kind %s: %w", consumer, kind, err,
				)
			}
			// No registered scope of this kind means this deployment runs no
			// such collector, so none will ever activate and waiting is waiting
			// for nothing. A registered kind with nothing quiescent is the
			// #5709 case and holds this producer back.
			ready = len(report.Registered) == 0 || len(report.Quiescent) > 0
			readyByKind[string(kind)] = ready
		}
		readiness[producer] = ready
	}
	return readiness, nil
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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// packageConsumptionEvidenceSource labels repo-to-repo DEPENDS_ON edges derived
// from package consumption-to-owner correlation joins. It is deliberately
// distinct from crossRepoEvidenceSource ("resolver/cross-repo") so the
// package-consumption edges are independently retractable and so query surfaces
// can attribute the edge to package-registry provenance (issue #3579).
const packageConsumptionEvidenceSource = "projection/package-consumption"

// packageConsumptionRepoEdgeConfidence is the fixed confidence for
// consumption-derived edges. It sits below the resolver/cross-repo and runtime
// service-list confidence because the owning repository is resolved from a
// package source hint (exact/derived URL match) rather than an explicit
// cross-repo runtime reference.
const packageConsumptionRepoEdgeConfidence = 0.6

// PackageConsumptionRepoDependencyInput carries the already-computed package
// correlation decisions for one package-registry reducer intent plus the
// acceptance identity used to enqueue durable repo-dependency projection
// intents.
type PackageConsumptionRepoDependencyInput struct {
	ScopeID              string
	GenerationID         string
	SourceRunID          string
	CreatedAt            time.Time
	ConsumptionDecisions []PackageConsumptionDecision
	OwnershipDecisions   []PackageSourceCorrelationDecision
	PublicationDecisions []PackagePublicationDecision
}

// packageOwnerResolution holds the resolved owning repository for a package id.
type packageOwnerResolution struct {
	repoID string
}

// BuildPackageConsumptionRepoDependencyIntents joins consumption decisions to
// owner/publisher decisions on package id and returns deduplicated
// consumer-repo DEPENDS_ON owner-repo upsert intents for the shared
// repo-dependency projection lane.
//
// Only exact/derived owner resolutions are projected: ambiguous, unresolved,
// stale, or rejected outcomes carry no single indexed owner repository and are
// dropped. Self-references (consumer == owner) are dropped. Multiple packages
// resolving to the same consumer/owner pair collapse to one edge whose
// evidence_count records how many packages backed it.
//
// The returned rows reuse BuildSharedProjectionIntent so the intent id is a
// deterministic function of the acceptance identity and partition key; running
// the join twice over the same input yields identical intent ids, which keeps
// the downstream DEPENDS_ON MERGE idempotent under retries and re-projection.
func BuildPackageConsumptionRepoDependencyIntents(
	input PackageConsumptionRepoDependencyInput,
) []SharedProjectionIntentRow {
	owners := resolvePackageOwners(input.OwnershipDecisions, input.PublicationDecisions)
	if len(owners) == 0 {
		return nil
	}

	type edgeAccumulator struct {
		consumerRepoID string
		ownerRepoID    string
		packageIDs     map[string]struct{}
		ecosystems     map[string]struct{}
	}

	edges := make(map[string]*edgeAccumulator)
	order := make([]string, 0)

	for _, consumption := range input.ConsumptionDecisions {
		consumerRepoID := strings.TrimSpace(consumption.RepositoryID)
		packageID := strings.TrimSpace(consumption.PackageID)
		if consumerRepoID == "" || packageID == "" {
			continue
		}
		owner, ok := owners[packageID]
		if !ok || owner.repoID == "" {
			continue
		}
		if owner.repoID == consumerRepoID {
			continue
		}

		edgeKey := consumerRepoID + "\x00" + owner.repoID
		acc, exists := edges[edgeKey]
		if !exists {
			acc = &edgeAccumulator{
				consumerRepoID: consumerRepoID,
				ownerRepoID:    owner.repoID,
				packageIDs:     make(map[string]struct{}),
				ecosystems:     make(map[string]struct{}),
			}
			edges[edgeKey] = acc
			order = append(order, edgeKey)
		}
		acc.packageIDs[packageID] = struct{}{}
		if ecosystem := strings.TrimSpace(consumption.Ecosystem); ecosystem != "" {
			acc.ecosystems[ecosystem] = struct{}{}
		}
	}

	if len(edges) == 0 {
		return nil
	}

	sort.Strings(order)
	rows := make([]SharedProjectionIntentRow, 0, len(order))
	for _, edgeKey := range order {
		acc := edges[edgeKey]
		rows = append(rows, buildPackageConsumptionRepoEdgeIntent(input, acc.consumerRepoID, acc.ownerRepoID, len(acc.packageIDs), sortedKeys(acc.ecosystems)))
	}
	return rows
}

// BuildPackageConsumptionRepoEdgeRefreshIntents returns one retract intent per
// consumer repository that declares package dependencies in this generation but
// for which no package resolves to an owning repository (owner gone, ambiguous,
// unresolved, stale, or rejected), and which therefore projects no upsert edge.
//
// Without these refresh intents a consumer that had a package-consumption
// DEPENDS_ON edge in a previous generation would keep that stale edge forever:
// BuildPackageConsumptionRepoDependencyIntents emits nothing for the consumer,
// so the shared repo-dependency lane never reprocesses it and never retracts the
// edge. The refresh intent reuses the same stable acceptance identity (scope and
// scope-only source-run id) as the upsert path, so it lands on the same
// acceptance unit the prior edge wrote and the lane's per-consumer refresh-first
// reconstruction retracts the now-unsupported package-consumption edges.
//
// Each refresh row carries the packageConsumptionEvidenceSource so the lane
// retracts only package-consumption edges and leaves resolver/cross-repo or
// other-source edges for the same consumer untouched. A consumer that produces
// at least one distinct-owner upsert is excluded: its upsert intent already
// drives the refresh-first reconstruction for this evidence source. A
// self-referential package (consumer == owner) yields no upsert, so a consumer
// whose only resolved package is a self-reference is still retracted: if it held
// a real cross-repo package-consumption edge in a prior generation that edge
// must be removed now (issue #3579, review comment 3455350032).
func BuildPackageConsumptionRepoEdgeRefreshIntents(
	input PackageConsumptionRepoDependencyInput,
) []SharedProjectionIntentRow {
	owners := resolvePackageOwners(input.OwnershipDecisions, input.PublicationDecisions)

	// A consumer is "covered" by an upsert when at least one of its declared
	// packages resolves to a distinct owning repository. Those consumers are
	// handled by BuildPackageConsumptionRepoDependencyIntents and must not also
	// emit a retraction, which would wipe the edge the upsert just established.
	// Every other consumer that declared a package dependency produces no upsert
	// this generation, so it must refresh to drop any stale package-consumption
	// edge it carried before.
	covered := make(map[string]struct{})
	candidateOrder := make([]string, 0)
	candidates := make(map[string]struct{})
	for _, consumption := range input.ConsumptionDecisions {
		consumerRepoID := strings.TrimSpace(consumption.RepositoryID)
		packageID := strings.TrimSpace(consumption.PackageID)
		if consumerRepoID == "" || packageID == "" {
			continue
		}
		owner, ok := owners[packageID]
		if ok && owner.repoID != "" && owner.repoID != consumerRepoID {
			covered[consumerRepoID] = struct{}{}
		}
		if _, seen := candidates[consumerRepoID]; !seen {
			candidates[consumerRepoID] = struct{}{}
			candidateOrder = append(candidateOrder, consumerRepoID)
		}
	}

	rows := make([]SharedProjectionIntentRow, 0, len(candidateOrder))
	sort.Strings(candidateOrder)
	for _, consumerRepoID := range candidateOrder {
		if _, ok := covered[consumerRepoID]; ok {
			continue
		}
		rows = append(rows, buildPackageConsumptionRepoEdgeRefreshIntent(input, consumerRepoID))
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// buildPackageConsumptionRepoEdgeRefreshIntent builds one retract-only intent
// that triggers the shared lane to drop a consumer's package-consumption edges.
// It carries no target_repo_id: the lane retracts every package-consumption
// edge owned by the consumer acceptance unit for this evidence source.
func buildPackageConsumptionRepoEdgeRefreshIntent(
	input PackageConsumptionRepoDependencyInput,
	consumerRepoID string,
) SharedProjectionIntentRow {
	payload := map[string]any{
		"action":          "retract",
		"repo_id":         consumerRepoID,
		"evidence_source": packageConsumptionEvidenceSource,
		"generation_id":   input.GenerationID,
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     fmt.Sprintf("retract:repo:%s|%s", consumerRepoID, packageConsumptionEvidenceSource),
		ScopeID:          input.ScopeID,
		AcceptanceUnitID: consumerRepoID,
		RepositoryID:     consumerRepoID,
		SourceRunID:      strings.TrimSpace(input.SourceRunID),
		GenerationID:     input.GenerationID,
		Payload:          payload,
		CreatedAt:        input.CreatedAt,
	})
}

// resolvePackageOwners builds the package-id to owning-repository map from
// exact/derived ownership and publication decisions. Ownership decisions win
// over publication when both resolve the same package id.
func resolvePackageOwners(
	ownership []PackageSourceCorrelationDecision,
	publication []PackagePublicationDecision,
) map[string]packageOwnerResolution {
	owners := make(map[string]packageOwnerResolution)

	// Publication first so ownership can override it.
	for _, decision := range publication {
		if !packageOwnerOutcomeAdmits(decision.Outcome) {
			continue
		}
		packageID := strings.TrimSpace(decision.PackageID)
		repoID := strings.TrimSpace(decision.RepositoryID)
		if packageID == "" || repoID == "" {
			continue
		}
		owners[packageID] = packageOwnerResolution{repoID: repoID}
	}
	for _, decision := range ownership {
		if !packageOwnerOutcomeAdmits(decision.Outcome) {
			continue
		}
		packageID := strings.TrimSpace(decision.PackageID)
		repoID := strings.TrimSpace(decision.RepositoryID)
		if packageID == "" || repoID == "" {
			continue
		}
		owners[packageID] = packageOwnerResolution{repoID: repoID}
	}
	return owners
}

// packageOwnerOutcomeAdmits reports whether a source-correlation outcome
// carries a single resolved indexed owner repository. Only exact and derived
// outcomes do; ambiguous/unresolved/stale/rejected do not.
func packageOwnerOutcomeAdmits(outcome PackageSourceCorrelationOutcome) bool {
	return outcome == PackageSourceCorrelationExact || outcome == PackageSourceCorrelationDerived
}

func buildPackageConsumptionRepoEdgeIntent(
	input PackageConsumptionRepoDependencyInput,
	consumerRepoID string,
	ownerRepoID string,
	packageCount int,
	ecosystems []string,
) SharedProjectionIntentRow {
	partitionKey := fmt.Sprintf("repo:%s->%s", consumerRepoID, ownerRepoID)
	rationale := fmt.Sprintf(
		"Consumer repository declares %d package dependency(ies) owned by target repository",
		packageCount,
	)
	payload := map[string]any{
		"action":            "upsert",
		"repo_id":           consumerRepoID,
		"target_repo_id":    ownerRepoID,
		"relationship_type": "DEPENDS_ON",
		"evidence_source":   packageConsumptionEvidenceSource,
		"evidence_type":     "package_consumption",
		"resolution_source": packageConsumptionEvidenceSource,
		"confidence":        packageConsumptionRepoEdgeConfidence,
		"evidence_count":    packageCount,
		"evidence_kinds":    ecosystems,
		"generation_id":     input.GenerationID,
		"rationale":         rationale,
		"resolved_id":       packageConsumptionResolvedID(consumerRepoID, ownerRepoID),
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     partitionKey,
		ScopeID:          input.ScopeID,
		AcceptanceUnitID: consumerRepoID,
		RepositoryID:     consumerRepoID,
		SourceRunID:      strings.TrimSpace(input.SourceRunID),
		GenerationID:     input.GenerationID,
		Payload:          payload,
		CreatedAt:        input.CreatedAt,
	})
}

// packageConsumptionResolvedID is the stable provenance id for one
// consumption-derived edge, distinct from resolver/cross-repo resolved ids.
func packageConsumptionResolvedID(consumerRepoID, ownerRepoID string) string {
	return "package-consumption:" + consumerRepoID + "->" + ownerRepoID
}

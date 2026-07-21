// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// Catalog workload assembly resolves bounded workload handles from the graph
// using backend-portable scalar queries. The canonical graph backend
// (NornicDB) does not reliably implicit-group an aggregation whose source is an
// OPTIONAL MATCH against a plain label anchor: such a query collapses every
// workload into a single row with empty collections and a zero count. To stay
// correct on that backend, each enrichment is read through its own bound-anchor
// query and joined in Go by workload id, mirroring the scalar-assembly approach
// in entity_workload_context.go.
//
// Per-workload environments are the union of two graph-edge sources:
//
//   - WorkloadInstance.environment for materialized instances, and
//   - Environment nodes reached through the workload's defining repository
//     deployment evidence, traversed as one connected path:
//     (w)<-[:DEFINES]-(repo)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(:Environment).
//
// A workload that materializes no WorkloadInstance still reports the
// environments resolved through its deployment evidence. An empty result means
// no environment edge exists; environments are never inferred from names.
// Environment normalization follows the canonical alias contract
// (go/internal/environment, docs/public/reference/environment-alias-contract.md).

const catalogWorkloadBaseCypher = `
	MATCH (w:Workload)
	RETURN w.id AS id,
	       w.name AS name,
	       coalesce(w.kind, 'workload') AS kind
	ORDER BY name, id
	LIMIT $limit
`

// catalogWorkloadRepoCypher and the instance/evidence enrichments below are
// bounded to the workload ids returned by catalogWorkloadBaseCypher via the
// $ids parameter. The base query already applies $limit, but the enrichments
// must not aggregate over the entire Workload, WorkloadInstance, or Environment
// populations: at ~500k-node scale that whole-graph aggregation timed the
// catalog endpoint out regardless of the requested limit (issue #3389). Each
// enrichment anchors on `(w:Workload) WHERE w.id IN $ids`, the bounded-id lookup
// shape the query-plan gate enforces (see repository_name_lookup.go).
// catalogWorkloadRepoCypher resolves each bounded workload's defining repository
// through a single connected path anchored on the workload id set. The earlier
// shape used two MATCH clauses -- `MATCH (w:Workload) WHERE w.id IN $ids` then a
// separate `MATCH (repo:Repository)-[:DEFINES]->(w)`. On NornicDB that second
// MATCH cold-plans as a full Repository label scan with per-repository DEFINES
// fanout re-joined to `w`, taking ~36s at the console's catalog limit (issue
// #3466) even though it returns only a few dozen rows. Expressing the same
// (workload)<-[:DEFINES]-(repository) relationship as one workload-anchored path
// keeps the result rows identical while cold-planning in single-digit
// milliseconds, mirroring the single-chain fix applied to
// catalogWorkloadEvidenceEnvironmentCypher for issue #1731.
const catalogWorkloadRepoCypher = `
	MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)
	WHERE w.id IN $ids
	RETURN w.id AS id,
	       repo.id AS repo_id,
	       repo.name AS repo_name
	ORDER BY id
`

const catalogWorkloadInstanceEnvironmentCypher = `
	MATCH (w:Workload) WHERE w.id IN $ids
	MATCH (inst:WorkloadInstance)-[:INSTANCE_OF]->(w)
	RETURN w.id AS id,
	       count(inst) AS instance_count,
	       collect(DISTINCT inst.environment) AS environments
	ORDER BY id
`

// catalogWorkloadEvidenceEnvironmentCypher resolves per-workload deployment
// environments through a single connected path. The earlier shape used two
// MATCH clauses that both anchored on `repo`; on NornicDB that re-anchor
// cold-plans as a per-repository fanout and takes ~21s at the console's catalog
// limit (issue #1731) despite returning only a few dozen rows. Expressing the
// same (workload)<-(repo)<-(artifact)->(environment) relationship as one path
// keeps the result rows identical while cold-planning in single-digit
// milliseconds.
const catalogWorkloadEvidenceEnvironmentCypher = `
	MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(env:Environment)
	WHERE w.id IN $ids
	RETURN w.id AS id,
	       env.name AS environment
	ORDER BY id, environment
`

// catalogWorkloadEnrichment carries per-workload graph facts joined by id.
type catalogWorkloadEnrichment struct {
	repoID        string
	repoName      string
	instanceCount int
	instanceEnvs  []string
	evidenceEnvs  []string
}

// assembleCatalogWorkloadsFromGraph reads the bounded workload set and joins
// repository, instance, and deployment-evidence environment facts in Go. It
// trims to limit+1 detection so the caller can report truncation consistently
// with the repository catalog path.
func (h *RepositoryHandler) assembleCatalogWorkloadsFromGraph(
	ctx context.Context,
	limit int,
) ([]catalogWorkload, bool, error) {
	baseRows, err := h.Neo4j.Run(ctx, catalogWorkloadBaseCypher, map[string]any{"limit": limit + 1})
	if err != nil {
		return nil, false, err
	}
	baseRows, truncated := trimCatalogRows(baseRows, limit)

	ids := make([]string, 0, len(baseRows))
	for _, row := range baseRows {
		if id := StringVal(row, "id"); id != "" {
			ids = append(ids, id)
		}
	}

	enrichments, err := h.catalogWorkloadEnrichments(ctx, ids)
	if err != nil {
		return nil, false, err
	}

	workloads := make([]catalogWorkload, 0, len(baseRows))
	for _, row := range baseRows {
		id := StringVal(row, "id")
		workload := catalogWorkload{
			ID:   id,
			Name: StringVal(row, "name"),
			Kind: normalizedCatalogWorkloadKind(StringVal(row, "kind")),
		}
		if enrichment, ok := enrichments[id]; ok {
			workload.RepoID = enrichment.repoID
			workload.RepoName = enrichment.repoName
			workload.InstanceCount = enrichment.instanceCount
			workload.Environments = mergeCatalogEnvironments(
				enrichment.instanceEnvs,
				enrichment.evidenceEnvs,
			)
		} else {
			workload.Environments = mergeCatalogEnvironments(nil)
		}
		if workload.Name == "" {
			workload.Name = workload.ID
		}
		workloads = append(workloads, workload)
	}
	return workloads, truncated, nil
}

// catalogWorkloadEnrichments reads repository, instance, and evidence
// environment facts through bound-anchor queries and indexes them by workload
// id for Go-side joining. Every enrichment is bounded to ids, the workload set
// the bounded base query returned, so the endpoint never aggregates over the
// whole graph (issue #3389). An empty id set short-circuits all graph round
// trips because no workload can be enriched.
func (h *RepositoryHandler) catalogWorkloadEnrichments(
	ctx context.Context,
	ids []string,
) (map[string]*catalogWorkloadEnrichment, error) {
	enrichments := make(map[string]*catalogWorkloadEnrichment)
	boundedIDs := sortedUniqueStrings(ids)
	if len(boundedIDs) == 0 {
		return enrichments, nil
	}
	params := map[string]any{"ids": boundedIDs}

	repoRows, err := h.Neo4j.Run(ctx, catalogWorkloadRepoCypher, params)
	if err != nil {
		return nil, err
	}
	for _, row := range repoRows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		entry := catalogWorkloadEnrichmentFor(enrichments, id)
		if entry.repoID == "" {
			entry.repoID = StringVal(row, "repo_id")
			entry.repoName = StringVal(row, "repo_name")
		}
	}

	instanceRows, err := h.Neo4j.Run(ctx, catalogWorkloadInstanceEnvironmentCypher, params)
	if err != nil {
		return nil, err
	}
	for _, row := range instanceRows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		entry := catalogWorkloadEnrichmentFor(enrichments, id)
		entry.instanceCount = IntVal(row, "instance_count")
		entry.instanceEnvs = StringSliceVal(row, "environments")
	}

	evidenceRows, err := h.Neo4j.Run(ctx, catalogWorkloadEvidenceEnvironmentCypher, params)
	if err != nil {
		return nil, err
	}
	for _, row := range evidenceRows {
		id := StringVal(row, "id")
		if id == "" {
			continue
		}
		environment := StringVal(row, "environment")
		if environment == "" {
			continue
		}
		entry := catalogWorkloadEnrichmentFor(enrichments, id)
		entry.evidenceEnvs = append(entry.evidenceEnvs, environment)
	}

	return enrichments, nil
}

// catalogWorkloadEnrichmentFor returns the enrichment entry for id, creating it
// on first use.
func catalogWorkloadEnrichmentFor(
	enrichments map[string]*catalogWorkloadEnrichment,
	id string,
) *catalogWorkloadEnrichment {
	entry, ok := enrichments[id]
	if !ok {
		entry = &catalogWorkloadEnrichment{}
		enrichments[id] = entry
	}
	return entry
}

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
//     deployment evidence:
//     (repo)-[:DEFINES]->(w) and
//     (repo)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(:Environment).
//
// A workload that materializes no WorkloadInstance still reports the
// environments resolved through its deployment evidence. An empty result means
// no environment edge exists; environments are never inferred from names.

const catalogWorkloadBaseCypher = `
	MATCH (w:Workload)
	RETURN w.id AS id,
	       w.name AS name,
	       coalesce(w.kind, 'workload') AS kind
	ORDER BY name, id
	LIMIT $limit
`

const catalogWorkloadRepoCypher = `
	MATCH (repo:Repository)-[:DEFINES]->(w:Workload)
	RETURN w.id AS id,
	       repo.id AS repo_id,
	       repo.name AS repo_name
	ORDER BY id
`

const catalogWorkloadInstanceEnvironmentCypher = `
	MATCH (inst:WorkloadInstance)-[:INSTANCE_OF]->(w:Workload)
	RETURN w.id AS id,
	       count(inst) AS instance_count,
	       collect(DISTINCT inst.environment) AS environments
	ORDER BY id
`

const catalogWorkloadEvidenceEnvironmentCypher = `
	MATCH (repo:Repository)-[:DEFINES]->(w:Workload)
	MATCH (repo)<-[:EVIDENCES_REPOSITORY_RELATIONSHIP]-(:EvidenceArtifact)-[:TARGETS_ENVIRONMENT]->(env:Environment)
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

	enrichments, err := h.catalogWorkloadEnrichments(ctx)
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
// id for Go-side joining.
func (h *RepositoryHandler) catalogWorkloadEnrichments(
	ctx context.Context,
) (map[string]*catalogWorkloadEnrichment, error) {
	enrichments := make(map[string]*catalogWorkloadEnrichment)

	repoRows, err := h.Neo4j.Run(ctx, catalogWorkloadRepoCypher, map[string]any{})
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

	instanceRows, err := h.Neo4j.Run(ctx, catalogWorkloadInstanceEnvironmentCypher, map[string]any{})
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

	evidenceRows, err := h.Neo4j.Run(ctx, catalogWorkloadEvidenceEnvironmentCypher, map[string]any{})
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

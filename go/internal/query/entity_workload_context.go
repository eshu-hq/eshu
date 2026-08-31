// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// fetchWorkloadContext queries graph-backed workload context with a custom
// WHERE clause and enriches linked repositories with local context evidence.
func (h *EntityHandler) fetchWorkloadContext(ctx context.Context, whereClause string, params map[string]any) (map[string]any, error) {
	return h.fetchWorkloadContextForOperation(ctx, whereClause, params, "workload_context")
}

// fetchServiceWorkloadContext avoids a backend-sensitive OR predicate by
// trying exact service-name lookup before exact workload-id lookup.
func (h *EntityHandler) fetchServiceWorkloadContext(ctx context.Context, serviceName string, operation string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, nil
	}
	result, err := h.fetchWorkloadContextForOperation(
		ctx,
		"w.name = $service_name",
		map[string]any{"service_name": serviceName},
		operation,
	)
	if err != nil || result != nil {
		return result, err
	}
	result, err = h.fetchWorkloadContextForOperation(
		ctx,
		"w.id = $service_name",
		map[string]any{"service_name": serviceName},
		operation,
	)
	if err != nil || result != nil {
		return result, err
	}
	return h.fetchServiceReadModelWorkloadContext(ctx, serviceName)
}

// fetchWorkloadContextForOperation queries workload context and tags timing
// logs with the caller operation that will render the context.
func (h *EntityHandler) fetchWorkloadContextForOperation(ctx context.Context, whereClause string, params map[string]any, operation string) (map[string]any, error) {
	access := repositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return nil, nil
	}
	serviceName := StringVal(params, "service_name")
	if serviceName == "" {
		serviceName = StringVal(params, "workload_id")
	}
	if operation == "" {
		operation = "workload_context"
	}
	timer := startServiceQueryStage(ctx, h.Logger, operation, serviceName, "", "workload_lookup")
	params = access.GraphParams(params)
	whereClause = scopedWorkloadWhereClause(whereClause, access)
	baseCypher := fmt.Sprintf(`
		MATCH (w:Workload) WHERE %s
		RETURN w.id as id, w.name as name, w.kind as kind, w.repo_id as repo_id
		LIMIT 1
	`, whereClause)

	row, err := h.Neo4j.RunSingle(ctx, baseCypher, params)
	timer.Done(ctx, slog.Bool("found", row != nil))
	if err != nil {
		return nil, err
	}

	if row == nil {
		return nil, nil
	}

	workloadID := StringVal(row, "id")
	followupWhereClause := whereClause
	followupParams := params
	if workloadID != "" {
		followupWhereClause = "w.id = $workload_id" // #nosec G101 -- Cypher parameterised query template, not a hardcoded credential
		followupParams = map[string]any{"workload_id": workloadID}
	}

	preferredRepoID := StringVal(row, "repo_id")
	if !access.AllowsRepositoryID(preferredRepoID) {
		preferredRepoID = ""
	}
	timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), preferredRepoID, "repository_lookup")
	repoID, repoName, err := h.fetchWorkloadRepositoryForAccess(
		ctx, workloadID, access, preferredRepoID,
	)
	timer.Done(ctx, slog.String("resolved_repo_id", repoID))
	if err != nil {
		return nil, err
	}
	if repoName == "" {
		repoName = StringVal(row, "repo_name")
	}

	timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "instance_lookup")
	topology, err := h.fetchWorkloadDeploymentTopology(
		ctx, followupWhereClause, followupParams, repoID, operation == "deployment_trace",
	)
	timer.Done(ctx, slog.Int("row_count", len(topology.instances)))
	if err != nil {
		return nil, err
	}
	instances := topology.instances
	if len(instances) == 0 {
		instances = extractInstances(row)
	}

	result := map[string]any{
		"id":                    StringVal(row, "id"),
		"name":                  StringVal(row, "name"),
		"kind":                  StringVal(row, "kind"),
		"repo_id":               repoID,
		"repo_name":             repoName,
		"instances":             instances,
		"topology_edges":        topology.topologyEdges,
		"provisioned_platforms": topology.provisionedPlatforms,
		"runtime_topology_limits": map[string]any{
			"instances":             topology.instanceLimits,
			"platform_edges":        topology.platformLimits,
			"provisioned_platforms": topology.provisionedPlatformLimits,
		},
	}
	if deploymentEvidence := mapValue(row, "deployment_evidence"); len(deploymentEvidence) > 0 {
		result["deployment_evidence"] = deploymentEvidence
	}

	if repoID != "" {
		repoParams := map[string]any{"repo_id": repoID}
		timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repo_dependencies")
		result["dependencies"] = queryRepoDependencies(ctx, h.Neo4j, repoParams)
		timer.Done(ctx, slog.Int("row_count", len(mapSliceValue(result, "dependencies"))))
		timer = startServiceQueryStage(ctx, h.Logger, operation, StringVal(row, "name"), repoID, "repo_infrastructure")
		infrastructure, infrastructureDegraded, infrastructureTruncated := queryRepoInfrastructure(ctx, h.Neo4j, h.Content, repoParams)
		result["infrastructure"] = infrastructure
		if infrastructureDegraded {
			// Surface the degradation on the result map itself (#5764 follow-up):
			// this map is returned verbatim as the /services/{name}/context and
			// /workloads/{id}/context response body. /services/{name}/story
			// surfaces this reason through TWO independent copies on the
			// dossier response (P3 review follow-up, correcting the prior
			// wrong description here): buildServiceIdentity
			// (service_story_dossier.go:57-74) writes it into
			// response["service_identity"]["limitations"], and the sibling
			// whitelist loop (service_story_dossier.go:23-36) separately
			// mirrors workloadContext["limitations"] onto the response's own
			// top-level "limitations" key. Either copy alone is sufficient:
			// answerMetadataLimitations (answer_metadata.go) reads both
			// data["limitations"] and
			// mapValue(data, "service_identity")["limitations"] before
			// deduping by reason, and that dedup is how a single degrade
			// reaches answer_metadata.partial_reasons exactly once.
			// /workloads/{id}/story builds a fresh response with no
			// "limitations" key at all (entity_workload_handlers.go's
			// getWorkloadStory) -- there the reason reaches callers only through
			// "partial_reasons" (contextPartialReasons reads this same
			// "limitations" slice off ctx). Without appending here, a degraded
			// read was distinguishable only via the stage log, so
			// "infrastructure": [] looked identical to "no infrastructure" to
			// every caller of this function.
			result["limitations"] = append(StringSliceVal(result, "limitations"), infrastructureReadDegradedReason)
		}
		if infrastructureTruncated {
			// Same visibility mechanism, for a healthy read that landed past
			// its LIMIT bound -- more rows exist beyond it (P2-2 follow-up to
			// #5764) -- instead of failing.
			result["limitations"] = append(StringSliceVal(result, "limitations"), infrastructureTruncatedReason)
		}
		timer.Done(ctx, infrastructureDegradeLogAttrs(len(infrastructure), infrastructureDegraded, infrastructureTruncated)...)
	}

	return result, nil
}

// fetchServiceReadModelWorkloadContext exposes repositories with workload
// identity facts even when no graph Workload node has been materialized yet.
func (h *EntityHandler) fetchServiceReadModelWorkloadContext(ctx context.Context, serviceName string) (map[string]any, error) {
	if h.Content == nil {
		return nil, nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return nil, nil
	}
	repo, err := h.Content.ResolveRepository(ctx, serviceName)
	if err != nil || repo == nil {
		return nil, err
	}
	if !access.AllowsRepositoryID(repo.ID) {
		return nil, nil
	}

	summary := loadRepositoryReadModelSummary(ctx, h.Content, repo.ID)
	if summary == nil {
		return nil, nil
	}
	workloadName := matchingRepositoryWorkloadIdentity(serviceName, *repo, summary.WorkloadNames)
	if workloadName == "" {
		return nil, nil
	}

	repoParams := map[string]any{"repo_id": repo.ID}
	limitations := []string{"workload_identity_not_materialized"}
	infrastructure, infrastructureTruncated := queryRepoInfrastructureFromContent(ctx, h.Content, repo.ID)
	if len(infrastructure) == 0 && h.Neo4j != nil {
		// A graph-read failure here degrades to an empty infrastructure list
		// rather than propagating: this is a read-model-only path serving
		// repositories with no materialized graph, and propagating a
		// 503/504 would fail a request Postgres can fully answer (#5764).
		// The degradation still stays visible via the existing limitations
		// slot rather than being silent.
		graphInfrastructure, graphTruncated, err := queryRepoInfrastructureFromGraph(ctx, h.Neo4j, repoParams)
		if err != nil {
			limitations = append(limitations, infrastructureReadDegradedReason)
		} else {
			infrastructure = graphInfrastructure
			infrastructureTruncated = graphTruncated
		}
	}
	if len(infrastructure) == 0 {
		// An empty panel never carries a truncation signal (#5764 P2 review
		// follow-up, widened by the round-7 P3 finding). infrastructureTruncated
		// describes rows that were clipped by a LIMIT bound, so it is
		// meaningless about a panel with no rows in it, and pairing it with
		// infrastructureReadDegradedReason would put two limitations that
		// assert mutually exclusive facts about the same read
		// (repository_infrastructure_degrade.go) on one response: "more rows
		// may exist" attached to an EMPTY infrastructure panel.
		//
		// The guard sits AFTER the graph attempt, not inside its error branch,
		// because a SUCCEEDING graph read reaches the same wrong state: it
		// reports truncated on its raw row count, before
		// isRepositoryInfrastructureType drops rows, so an over-returning
		// backend can hand back a full limit+1 window that classifies to an
		// empty panel. Both that case and the graph-error case need a read
		// that bounded rows the classifier then discarded -- the type-list
		// drift repositoryInfrastructureEntityTypes' own doc comment warns
		// about, simulated on the content side by
		// nonFilteringInfrastructureContentStore and on the graph side by the
		// over-returning reader, both in
		// service_read_model_workload_context_test.go.
		infrastructureTruncated = false
	}
	if infrastructureTruncated {
		limitations = append(limitations, infrastructureTruncatedReason)
	}
	dependencies := []map[string]any{}
	if h.Neo4j != nil {
		dependencies = queryRepoDependencies(ctx, h.Neo4j, repoParams)
	}
	return map[string]any{
		"id":                     "workload:" + workloadName,
		"name":                   workloadName,
		"kind":                   "service",
		"repo_id":                repo.ID,
		"repo_name":              repo.Name,
		"instances":              []map[string]any{},
		"dependencies":           dependencies,
		"infrastructure":         infrastructure,
		"materialization_status": "identity_only",
		"query_basis":            "repository_read_model",
		"limitations":            limitations,
	}, nil
}

func matchingRepositoryWorkloadIdentity(serviceName string, repo RepositoryCatalogEntry, workloadNames []string) string {
	selector := strings.TrimSpace(serviceName)
	if selector == "" {
		return ""
	}
	plainSelector := strings.TrimPrefix(selector, "workload:")
	for _, workloadName := range workloadNames {
		normalized := strings.TrimSpace(workloadName)
		if normalized == "" {
			continue
		}
		if selector == normalized || plainSelector == normalized || selector == "workload:"+normalized {
			return normalized
		}
	}
	if selector != repo.Name && plainSelector != repo.Name {
		return ""
	}
	if len(workloadNames) != 1 {
		return ""
	}
	return strings.TrimSpace(workloadNames[0])
}

const workloadRepositoryCandidateLimit = contextStoryItemLimit

// fetchWorkloadRepositoryForAccess resolves a bounded repository candidate set
// from one exact Workload anchor while preserving scoped authorization. It
// sorts the complete bounded set in Go because NornicDB can re-plan backend
// ORDER BY/CASE relationship reads as global scans. A stored workload repo_id
// is preferred only after the DEFINES relationship proves it is a candidate.
func (h *EntityHandler) fetchWorkloadRepositoryForAccess(
	ctx context.Context,
	workloadID string,
	access repositoryAccessFilter,
	preferredRepoID string,
) (string, string, error) {
	if strings.TrimSpace(workloadID) == "" {
		return "", "", nil
	}
	queryLimit := workloadRepositoryCandidateLimit + 1
	params := access.GraphParams(map[string]any{
		"workload_id":      workloadID,
		"repository_limit": queryLimit,
	})
	cypher := fmt.Sprintf(`
		MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)
		%s
		RETURN DISTINCT r.id as repo_id, r.name as repo_name
		LIMIT $repository_limit
	`, access.GraphWhereClause("r"))
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return "", "", err
	}
	if len(rows) > workloadRepositoryCandidateLimit {
		return "", "", fmt.Errorf(
			"workload repository candidates exceed bound: returned %d, limit %d",
			len(rows), workloadRepositoryCandidateLimit,
		)
	}
	candidates := make([]map[string]any, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		repoID := StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		if _, exists := seen[repoID]; exists {
			continue
		}
		seen[repoID] = struct{}{}
		candidates = append(candidates, row)
	}
	if len(candidates) == 0 {
		return "", "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return StringVal(candidates[i], "repo_id") < StringVal(candidates[j], "repo_id")
	})
	selected := candidates[0]
	for _, candidate := range candidates {
		if StringVal(candidate, "repo_id") == preferredRepoID {
			selected = candidate
			break
		}
	}
	return StringVal(selected, "repo_id"), StringVal(selected, "repo_name"), nil
}

func scopedWorkloadWhereClause(whereClause string, access repositoryAccessFilter) string {
	if !access.Scoped() {
		return whereClause
	}
	return whereClause + `
			AND (
				w.repo_id IN $allowed_repository_ids
				OR w.repo_id IN $allowed_scope_ids
				OR EXISTS {
					MATCH (scopeRepo:Repository)-[:DEFINES]->(w)
					WHERE ` + access.GraphCondition("scopeRepo") + `
				}
			)`
}

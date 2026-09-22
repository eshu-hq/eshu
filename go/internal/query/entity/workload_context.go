// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// fetchWorkloadContext queries graph-backed workload context with a custom
// WHERE clause and enriches linked repositories with local context evidence.
func (h *Handler) fetchWorkloadContext(ctx context.Context, whereClause string, params map[string]any) (map[string]any, error) {
	return h.FetchWorkloadContextForOperation(ctx, whereClause, params, "workload_context")
}

// fetchServiceWorkloadContext avoids a backend-sensitive OR predicate by
// trying exact service-name lookup before exact workload-id lookup, then the
// repository read model.
//
// A grant denial is counted once, and only when no lookup produced a
// workload: a name lookup that finds only an ungranted workload followed by
// an id lookup that admits one is a successful request, not a denial.
func (h *Handler) fetchServiceWorkloadContext(ctx context.Context, serviceName string, operation string) (map[string]any, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, nil
	}
	operation = workloadContextOperation(operation)
	params := map[string]any{"service_name": serviceName}
	result, nameDenied, err := h.fetchWorkloadContextDecision(ctx, "w.name = $service_name", params, operation)
	if err != nil || result != nil {
		return result, err
	}
	result, idDenied, err := h.fetchWorkloadContextDecision(ctx, "w.id = $service_name", params, operation)
	if err != nil || result != nil {
		return result, err
	}
	result, err = h.FetchServiceReadModelWorkloadContext(ctx, serviceName)
	if err == nil && result == nil && (nameDenied || idDenied) {
		h.recordScopedGrantDenied(ctx, operation, "grant_denied")
	}
	return result, err
}

// FetchWorkloadContextForOperation queries workload context and tags timing
// logs with the caller operation that will render the context. A workload
// that matched but that the caller's grant does not admit reads as not found
// and counts one reason=grant_denied.
func (h *Handler) FetchWorkloadContextForOperation(ctx context.Context, whereClause string, params map[string]any, operation string) (map[string]any, error) {
	operation = workloadContextOperation(operation)
	result, denied, err := h.fetchWorkloadContextDecision(ctx, whereClause, params, operation)
	if err == nil && result == nil && denied {
		h.recordScopedGrantDenied(ctx, operation, "grant_denied")
	}
	return result, err
}

// workloadContextOperation returns operation, defaulting an empty value to
// "workload_context".
func workloadContextOperation(operation string) string {
	if operation == "" {
		return "workload_context"
	}
	return operation
}

// fetchWorkloadContextDecision builds workload context for the row
// whereClause selects. denied reports that a matched workload was refused by
// the caller's grant; callers decide whether and when to count it.
func (h *Handler) fetchWorkloadContextDecision(ctx context.Context, whereClause string, params map[string]any, operation string) (map[string]any, bool, error) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return nil, false, nil
	}
	serviceName := querycontract.StringVal(params, "service_name")
	if serviceName == "" {
		serviceName = querycontract.StringVal(params, "workload_id")
	}
	timer := service.StartServiceQueryStage(ctx, h.Logger, operation, serviceName, "", "workload_lookup")
	params = access.GraphParams(params)
	// #6786: the grant is decided in Go (lookupWorkloadRows and
	// firstGrantedWorkload's DEFINES read), not by a multi-line group in
	// this read's WHERE. A multi-line
	// `AND ( ... OR EXISTS {...} )` grant group is unreliable on the pinned
	// NornicDB v1.3.3 image and can drop the whole WHERE, including the
	// id/name anchor callers pass in whereClause.
	candidates, denied, err := h.lookupWorkloadRows(ctx, access, whereClause, params, serviceName, operation)
	timer.Done(ctx, slog.Bool("found", len(candidates) > 0))
	if err != nil {
		return nil, false, err
	}
	row, repoID, repoName, rejected, err := h.firstGrantedWorkload(ctx, access, candidates, operation)
	if err != nil {
		return nil, false, err
	}
	if row == nil {
		return nil, denied || rejected, nil
	}

	workloadID := querycontract.StringVal(row, "id")
	followupWhereClause := whereClause
	followupParams := params
	if workloadID != "" {
		followupWhereClause = "w.id = $workload_id" // #nosec G101 -- Cypher parameterised query template, not a hardcoded credential
		followupParams = map[string]any{"workload_id": workloadID}
	}

	if repoName == "" {
		repoName = querycontract.StringVal(row, "repo_name")
	}

	timer = service.StartServiceQueryStage(ctx, h.Logger, operation, querycontract.StringVal(row, "name"), repoID, "instance_lookup")
	topology, err := h.FetchWorkloadDeploymentTopology(
		ctx, followupWhereClause, followupParams, repoID, operation == "deployment_trace",
	)
	timer.Done(ctx, slog.Int("row_count", len(topology.instances)))
	if err != nil {
		return nil, false, err
	}
	instances := topology.instances
	if len(instances) == 0 {
		instances = extractInstances(row)
	}

	result := map[string]any{
		"id":                    querycontract.StringVal(row, "id"),
		"name":                  querycontract.StringVal(row, "name"),
		"kind":                  querycontract.StringVal(row, "kind"),
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
	if deploymentEvidence := querycontract.MapValue(row, "deployment_evidence"); len(deploymentEvidence) > 0 {
		result["deployment_evidence"] = deploymentEvidence
	}

	if repoID != "" {
		repoParams := map[string]any{"repo_id": repoID}
		timer = service.StartServiceQueryStage(ctx, h.Logger, operation, querycontract.StringVal(row, "name"), repoID, "repo_dependencies")
		dependencies, dependenciesDegraded := repository.QueryRepoDependencies(ctx, h.Neo4j, repoParams)
		result["dependencies"] = dependencies
		dependencyAttrs := []slog.Attr{slog.Int("row_count", len(dependencies))}
		if dependenciesDegraded {
			dependencyAttrs = append(dependencyAttrs, slog.String("failure_class", repository.RelationshipsReadDegradedReason))
			// Same discipline as the infrastructure read below (#6810): a failed
			// dependencies read is a named limitation, not an empty list.
			result["limitations"] = append(querycontract.StringSliceVal(result, "limitations"), repository.RelationshipsReadDegradedReason)
		}
		timer.Done(ctx, dependencyAttrs...)
		timer = service.StartServiceQueryStage(ctx, h.Logger, operation, querycontract.StringVal(row, "name"), repoID, "repo_infrastructure")
		infrastructure, infrastructureDegraded, infrastructureTruncated := repository.QueryRepoInfrastructure(ctx, h.Neo4j, h.Content, repoParams)
		result["infrastructure"] = infrastructure
		if infrastructureDegraded {
			// Surface the degradation on the result map itself (#5764 follow-up):
			// this map is returned verbatim as the /services/{name}/context and
			// /workloads/{id}/context response body. /services/{name}/story
			// surfaces this reason through TWO independent copies on the
			// dossier response (P3 review follow-up, correcting the prior
			// wrong description here): buildServiceIdentity
			// (service/story_dossier.go:64) writes it into
			// response["service_identity"]["limitations"], and the sibling
			// whitelist loop (service/story_dossier.go:29) separately
			// mirrors workloadContext["limitations"] onto the response's own
			// top-level "limitations" key. Either copy alone is sufficient:
			// answerMetadataLimitations (answer_metadata.go) reads both
			// data["limitations"] and
			// mapValue(data, "service_identity")["limitations"] before
			// deduping by reason, and that dedup is how a single degrade
			// reaches answer_metadata.partial_reasons exactly once.
			// /workloads/{id}/story builds a fresh response with no
			// "limitations" key at all (workload_handlers.go's
			// getWorkloadStory) -- there the reason reaches callers only through
			// "partial_reasons" (contextPartialReasons reads this same
			// "limitations" slice off ctx). Without appending here, a degraded
			// read was distinguishable only via the stage log, so
			// "infrastructure": [] looked identical to "no infrastructure" to
			// every caller of this function.
			result["limitations"] = append(querycontract.StringSliceVal(result, "limitations"), repository.InfrastructureReadDegradedReason)
		}
		if infrastructureTruncated {
			// Same visibility mechanism, for a healthy read that landed past
			// its LIMIT bound -- more rows exist beyond it (P2-2 follow-up to
			// #5764) -- instead of failing.
			result["limitations"] = append(querycontract.StringSliceVal(result, "limitations"), repository.InfrastructureTruncatedReason)
		}
		timer.Done(ctx, repository.InfrastructureDegradeLogAttrs(len(infrastructure), infrastructureDegraded, infrastructureTruncated)...)
	}

	return result, false, nil
}

// FetchServiceReadModelWorkloadContext exposes repositories with workload
// identity facts even when no graph Workload node has been materialized yet.
func (h *Handler) FetchServiceReadModelWorkloadContext(ctx context.Context, serviceName string) (map[string]any, error) {
	if h.Content == nil {
		return nil, nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
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

	summary := querycontract.LoadRepositoryReadModelSummary(ctx, h.Content, repo.ID)
	if summary == nil {
		return nil, nil
	}
	workloadName := matchingRepositoryWorkloadIdentity(serviceName, *repo, summary.WorkloadNames)
	if workloadName == "" {
		return nil, nil
	}

	repoParams := map[string]any{"repo_id": repo.ID}
	limitations := []string{"workload_identity_not_materialized"}
	infrastructure, infrastructureTruncated := repository.QueryRepoInfrastructureFromContent(ctx, h.Content, repo.ID)
	if len(infrastructure) == 0 && h.Neo4j != nil {
		// A graph-read failure here degrades to an empty infrastructure list
		// rather than propagating: this is a read-model-only path serving
		// repositories with no materialized graph, and propagating a
		// 503/504 would fail a request Postgres can fully answer (#5764).
		// The degradation still stays visible via the existing limitations
		// slot rather than being silent.
		graphInfrastructure, graphTruncated, err := repository.QueryRepoInfrastructureFromGraph(ctx, h.Neo4j, repoParams)
		if err != nil {
			limitations = append(limitations, repository.InfrastructureReadDegradedReason)
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
		// repository.InfrastructureReadDegradedReason would put two limitations that
		// assert mutually exclusive facts about the same read
		// (repository/infrastructure_degrade.go) on one response: "more rows
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
		limitations = append(limitations, repository.InfrastructureTruncatedReason)
	}
	dependencies := []map[string]any{}
	if h.Neo4j != nil {
		var dependenciesDegraded bool
		dependencies, dependenciesDegraded = repository.QueryRepoDependencies(ctx, h.Neo4j, repoParams)
		if dependenciesDegraded {
			limitations = append(limitations, repository.RelationshipsReadDegradedReason)
		}
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

func matchingRepositoryWorkloadIdentity(serviceName string, repo querycontract.RepositoryCatalogEntry, workloadNames []string) string {
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

const workloadRepositoryCandidateLimit = querycontract.ContextStoryItemLimit

// FetchWorkloadRepositoryForAccess resolves a bounded repository candidate set
// from one exact Workload anchor while preserving scoped authorization. It
// sorts the complete bounded set in Go because NornicDB can re-plan backend
// ORDER BY/CASE relationship reads as global scans. A stored workload repo_id
// is preferred only after the DEFINES relationship proves it is a candidate.
func (h *Handler) FetchWorkloadRepositoryForAccess(
	ctx context.Context,
	workloadID string,
	access querycontract.RepositoryAccessFilter,
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
		repoID := querycontract.StringVal(row, "repo_id")
		if repoID == "" {
			continue
		}
		if _, exists := seen[repoID]; exists {
			continue
		}
		// #6786 review follow-up (F1): re-check every candidate against the
		// grant in Go rather than trusting the backend's WHERE alone. This
		// query's `<-[:DEFINES]-` MATCH pattern is itself a backward pattern
		// with an inner WHERE, the same shape class this PR already proved
		// NornicDB v1.3.3 can silently fail to apply; a candidate that slips
		// through must not be admitted just because it reached this loop.
		if !access.AllowsRepositoryID(repoID) {
			continue
		}
		seen[repoID] = struct{}{}
		candidates = append(candidates, row)
	}
	if len(candidates) == 0 {
		return "", "", nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return querycontract.StringVal(candidates[i], "repo_id") < querycontract.StringVal(candidates[j], "repo_id")
	})
	selected := candidates[0]
	for _, candidate := range candidates {
		if querycontract.StringVal(candidate, "repo_id") == preferredRepoID {
			selected = candidate
			break
		}
	}
	return querycontract.StringVal(selected, "repo_id"), querycontract.StringVal(selected, "repo_name"), nil
}

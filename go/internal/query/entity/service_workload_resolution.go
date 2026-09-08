// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"

	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// ServiceWorkloadCandidateLimit bounds service-workload candidate selection per read. Exported for the staying legacy queryplan test via the root alias; see #6060.
const ServiceWorkloadCandidateLimit = 10

// ServiceWorkloadSelector selects service workloads by identity. Its home is
// service/ (B4); this alias keeps the moved resolution file spelling the
// established name. See #6060.
type ServiceWorkloadSelector = service.ServiceWorkloadSelector

// ServiceWorkloadCandidate is one service-workload candidate row. Exported as the result element of QueryServiceWorkloadCandidates; see #6060.
type ServiceWorkloadCandidate struct {
	ID          string
	Name        string
	Kind        string
	RepoID      string
	RepoName    string
	Environment string
	MatchBasis  string
}

type serviceWorkloadAmbiguousError struct {
	Selector   string
	Candidates []ServiceWorkloadCandidate
	Truncated  bool
}

func (e serviceWorkloadAmbiguousError) Error() string {
	return fmt.Sprintf(
		"service selector %q matched multiple services; add service_id, repo, or environment",
		e.Selector,
	)
}

func (h *EntityHandler) fetchServiceWorkloadContextWithSelector(
	ctx context.Context,
	selector ServiceWorkloadSelector,
	operation string,
) (map[string]any, error) {
	selector.ServiceName = strings.TrimSpace(selector.ServiceName)
	selector.ServiceID = strings.TrimSpace(selector.ServiceID)
	selector.Repository = strings.TrimSpace(selector.Repository)
	selector.Environment = strings.TrimSpace(selector.Environment)
	if selector.ServiceName == "" && selector.ServiceID == "" {
		return nil, nil
	}

	candidate, err := h.resolveServiceWorkloadCandidate(ctx, selector, operation)
	if err != nil {
		return nil, err
	}
	if candidate == nil {
		if selector.ServiceID == "" && selector.Repository == "" && selector.Environment == "" {
			return h.FetchServiceReadModelWorkloadContext(ctx, selector.ServiceName)
		}
		return nil, nil
	}
	return h.FetchWorkloadContextForOperation(
		ctx,
		"w.id = $workload_id",
		map[string]any{"workload_id": candidate.ID},
		operation,
	)
}

func (h *EntityHandler) resolveServiceWorkloadCandidate(
	ctx context.Context,
	selector ServiceWorkloadSelector,
	operation string,
) (*ServiceWorkloadCandidate, error) {
	repoID, err := h.resolveServiceTraceRepoSelector(ctx, selector.Repository)
	if err != nil {
		return nil, err
	}

	timer := service.StartServiceQueryStage(ctx, h.Logger, operation, traceServiceSelectorDisplay(selector), repoID, "service_candidate_lookup")
	candidates, truncated, err := h.collectServiceWorkloadCandidates(ctx, selector, repoID)
	timer.Done(ctx, slog.Int("row_count", len(candidates)), slog.Bool("truncated", truncated))
	if err != nil {
		return nil, err
	}
	switch len(candidates) {
	case 0:
		return nil, nil
	case 1:
		return &candidates[0], nil
	default:
		return nil, serviceWorkloadAmbiguousError{
			Selector:   traceServiceSelectorDisplay(selector),
			Candidates: candidates,
			Truncated:  truncated,
		}
	}
}

func (h *EntityHandler) resolveServiceTraceRepoSelector(ctx context.Context, repoSelector string) (string, error) {
	if strings.TrimSpace(repoSelector) == "" {
		return "", nil
	}
	return queryselector.ResolveExactForAccess(
		ctx,
		h.Neo4j,
		h.Content,
		repoSelector,
		querycontract.RepositoryAccessFilterFromContext(ctx),
	)
}

func (h *EntityHandler) collectServiceWorkloadCandidates(
	ctx context.Context,
	selector ServiceWorkloadSelector,
	repoID string,
) ([]ServiceWorkloadCandidate, bool, error) {
	if querycontract.RepositoryAccessFilterFromContext(ctx).Empty() {
		return nil, false, nil
	}
	limit := ServiceWorkloadCandidateLimit + 1
	all := make([]ServiceWorkloadCandidate, 0, limit)
	if selector.ServiceID != "" {
		rows, err := h.QueryServiceWorkloadCandidates(ctx, "w.id = $service_id", "service_id", selector.ServiceID, selector, repoID, limit, "workload_id")
		if err != nil {
			return nil, false, err
		}
		all = append(all, rows...)
	} else {
		if strings.HasPrefix(selector.ServiceName, "workload:") {
			rows, err := h.QueryServiceWorkloadCandidates(ctx, "w.id = $service_name", "service_name", selector.ServiceName, selector, repoID, limit, "workload_id")
			if err != nil {
				return nil, false, err
			}
			all = append(all, rows...)
		}
		rows, err := h.QueryServiceWorkloadCandidates(ctx, "w.name = $service_name", "service_name", selector.ServiceName, selector, repoID, limit, "workload_name")
		if err != nil {
			return nil, false, err
		}
		all = append(all, rows...)
		if len(all) == 0 {
			rows, err = h.queryServiceInstanceCandidates(ctx, "i.id = $service_name", selector, repoID, limit, "workload_instance_id")
			if err != nil {
				return nil, false, err
			}
			all = append(all, rows...)
		}
		if len(all) == 0 {
			rows, err = h.queryServiceInstanceCandidates(ctx, "i.name = $service_name", selector, repoID, limit, "workload_instance_name")
			if err != nil {
				return nil, false, err
			}
			all = append(all, rows...)
		}
	}

	candidates := dedupeServiceWorkloadCandidates(all)
	truncated := len(candidates) > ServiceWorkloadCandidateLimit
	if truncated {
		candidates = candidates[:ServiceWorkloadCandidateLimit]
	}
	if err := h.hydrateServiceWorkloadCandidateRepoNames(ctx, candidates); err != nil {
		return nil, false, err
	}
	return candidates, truncated, nil
}

func (h *EntityHandler) hydrateServiceWorkloadCandidateRepoNames(ctx context.Context, candidates []ServiceWorkloadCandidate) error {
	repoIDSet := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.RepoID == "" || candidate.RepoName != "" {
			continue
		}
		repoIDSet[candidate.RepoID] = struct{}{}
	}
	if len(repoIDSet) == 0 {
		return nil
	}

	repoIDs := make([]string, 0, len(repoIDSet))
	for repoID := range repoIDSet {
		repoIDs = append(repoIDs, repoID)
	}
	slices.Sort(repoIDs)

	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		WHERE r.id IN $repo_ids
		RETURN r.id as repo_id,
		       r.name as repo_name
		ORDER BY repo_id
		LIMIT %d
	`, len(repoIDs))
	rows, err := h.Neo4j.Run(ctx, cypher, map[string]any{"repo_ids": repoIDs})
	if err != nil {
		return err
	}
	repoNames := make(map[string]string, len(rows))
	for _, row := range rows {
		repoID := querycontract.StringVal(row, "repo_id")
		repoName := querycontract.StringVal(row, "repo_name")
		if repoID != "" && repoName != "" {
			repoNames[repoID] = repoName
		}
	}
	for i := range candidates {
		if candidates[i].RepoName == "" {
			candidates[i].RepoName = repoNames[candidates[i].RepoID]
		}
	}
	return nil
}

// QueryServiceWorkloadCandidates selects service-workload candidate rows. Exported so the staying legacy queryplan test keeps driving the handler; see #6060.
func (h *EntityHandler) QueryServiceWorkloadCandidates(
	ctx context.Context,
	whereClause string,
	paramName string,
	paramValue string,
	selector ServiceWorkloadSelector,
	repoID string,
	limit int,
	matchBasis string,
) ([]ServiceWorkloadCandidate, error) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	params := access.GraphParams(map[string]any{paramName: paramValue})
	whereParts := []string{whereClause}
	if repoID != "" {
		whereParts = append(whereParts, "w.repo_id = $repo_id")
		params["repo_id"] = repoID
	} else if access.Scoped() {
		whereParts = append(whereParts, querycontract.WorkloadScopePredicate("w", access))
	}

	var cypher string
	if selector.Environment != "" {
		params["environment"] = selector.Environment
		cypher = fmt.Sprintf(`
			MATCH (i:WorkloadInstance)
			WHERE i.environment = $environment
			MATCH (w:Workload)
			WHERE %s AND w.id = i.workload_id
			RETURN w.id as id,
			       w.name as name,
			       w.kind as kind,
			       w.repo_id as repo_id,
			       i.environment as environment
			ORDER BY repo_id, id
			LIMIT %d
		`, strings.Join(whereParts, " AND "), limit)
	} else {
		cypher = fmt.Sprintf(`
			MATCH (w:Workload)
			WHERE %s
			RETURN w.id as id,
			       w.name as name,
			       w.kind as kind,
			       w.repo_id as repo_id,
			       '' as environment
			ORDER BY repo_id, id
			LIMIT %d
		`, strings.Join(whereParts, " AND "), limit)
	}
	return h.serviceWorkloadCandidatesFromQuery(ctx, cypher, params, matchBasis)
}

func (h *EntityHandler) queryServiceInstanceCandidates(
	ctx context.Context,
	instanceWhere string,
	selector ServiceWorkloadSelector,
	repoID string,
	limit int,
	matchBasis string,
) ([]ServiceWorkloadCandidate, error) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	params := access.GraphParams(map[string]any{"service_name": selector.ServiceName})
	whereParts := []string{"w.id = i.workload_id"}
	if repoID != "" {
		whereParts = append(whereParts, "w.repo_id = $repo_id")
		params["repo_id"] = repoID
	} else if access.Scoped() {
		whereParts = append(whereParts, querycontract.WorkloadScopePredicate("w", access))
	}
	if selector.Environment != "" {
		whereParts = append(whereParts, "i.environment = $environment")
		params["environment"] = selector.Environment
	}
	cypher := fmt.Sprintf(`
		MATCH (i:WorkloadInstance)
		WHERE %s
		MATCH (w:Workload)
		WHERE %s
		RETURN w.id as id,
		       w.name as name,
		       w.kind as kind,
		       w.repo_id as repo_id,
		       i.environment as environment
		ORDER BY repo_id, id
		LIMIT %d
	`, instanceWhere, strings.Join(whereParts, " AND "), limit)
	return h.serviceWorkloadCandidatesFromQuery(ctx, cypher, params, matchBasis)
}

func (h *EntityHandler) serviceWorkloadCandidatesFromQuery(
	ctx context.Context,
	cypher string,
	params map[string]any,
	matchBasis string,
) ([]ServiceWorkloadCandidate, error) {
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	candidates := make([]ServiceWorkloadCandidate, 0, len(rows))
	for _, row := range rows {
		candidate := ServiceWorkloadCandidate{
			ID:          querycontract.StringVal(row, "id"),
			Name:        querycontract.StringVal(row, "name"),
			Kind:        querycontract.StringVal(row, "kind"),
			RepoID:      querycontract.StringVal(row, "repo_id"),
			RepoName:    querycontract.StringVal(row, "repo_name"),
			Environment: querycontract.StringVal(row, "environment"),
			MatchBasis:  matchBasis,
		}
		if candidate.ID == "" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func dedupeServiceWorkloadCandidates(input []ServiceWorkloadCandidate) []ServiceWorkloadCandidate {
	seen := make(map[string]int, len(input))
	output := make([]ServiceWorkloadCandidate, 0, len(input))
	for _, candidate := range input {
		if candidate.ID == "" {
			continue
		}
		if index, ok := seen[candidate.ID]; ok {
			if output[index].Environment == "" {
				output[index].Environment = candidate.Environment
			}
			continue
		}
		seen[candidate.ID] = len(output)
		output = append(output, candidate)
	}
	slices.SortFunc(output, func(a, b ServiceWorkloadCandidate) int {
		switch {
		case a.RepoID != b.RepoID:
			return strings.Compare(a.RepoID, b.RepoID)
		case a.ID != b.ID:
			return strings.Compare(a.ID, b.ID)
		default:
			return strings.Compare(a.Environment, b.Environment)
		}
	})
	return output
}

func traceServiceSelectorDisplay(selector ServiceWorkloadSelector) string {
	if selector.ServiceID != "" {
		return selector.ServiceID
	}
	return selector.ServiceName
}

func serviceWorkloadCandidateMaps(candidates []ServiceWorkloadCandidate) []map[string]any {
	rows := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, map[string]any{
			"service_id":   candidate.ID,
			"service_name": candidate.Name,
			"kind":         candidate.Kind,
			"repo_id":      candidate.RepoID,
			"repo_name":    candidate.RepoName,
			"environment":  candidate.Environment,
			"match_basis":  candidate.MatchBasis,
		})
	}
	return rows
}

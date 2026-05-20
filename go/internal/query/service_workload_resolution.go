package query

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

const serviceWorkloadCandidateLimit = 10

type serviceWorkloadSelector struct {
	ServiceName string
	ServiceID   string
	Repository  string
	Environment string
}

type serviceWorkloadCandidate struct {
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
	Candidates []serviceWorkloadCandidate
	Truncated  bool
}

func (e serviceWorkloadAmbiguousError) Error() string {
	return fmt.Sprintf(
		"service selector %q matched multiple services; add --service-id, --repo, or --env",
		e.Selector,
	)
}

func (h *EntityHandler) fetchServiceWorkloadContextWithSelector(
	ctx context.Context,
	selector serviceWorkloadSelector,
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
			return h.fetchServiceReadModelWorkloadContext(ctx, selector.ServiceName)
		}
		return nil, nil
	}
	return h.fetchWorkloadContextForOperation(
		ctx,
		"w.id = $workload_id",
		map[string]any{"workload_id": candidate.ID},
		operation,
	)
}

func (h *EntityHandler) resolveServiceWorkloadCandidate(
	ctx context.Context,
	selector serviceWorkloadSelector,
	operation string,
) (*serviceWorkloadCandidate, error) {
	repoID, err := h.resolveServiceTraceRepoSelector(ctx, selector.Repository)
	if err != nil {
		return nil, err
	}

	timer := startServiceQueryStage(ctx, h.Logger, operation, traceServiceSelectorDisplay(selector), repoID, "service_candidate_lookup")
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
	return resolveRepositorySelectorExact(ctx, h.Neo4j, h.Content, repoSelector)
}

func (h *EntityHandler) collectServiceWorkloadCandidates(
	ctx context.Context,
	selector serviceWorkloadSelector,
	repoID string,
) ([]serviceWorkloadCandidate, bool, error) {
	limit := serviceWorkloadCandidateLimit + 1
	all := make([]serviceWorkloadCandidate, 0, limit)
	if selector.ServiceID != "" {
		rows, err := h.queryServiceWorkloadCandidates(ctx, "w.id = $service_id", "service_id", selector.ServiceID, selector, repoID, limit, "workload_id")
		if err != nil {
			return nil, false, err
		}
		all = append(all, rows...)
	} else {
		if strings.HasPrefix(selector.ServiceName, "workload:") {
			rows, err := h.queryServiceWorkloadCandidates(ctx, "w.id = $service_name", "service_name", selector.ServiceName, selector, repoID, limit, "workload_id")
			if err != nil {
				return nil, false, err
			}
			all = append(all, rows...)
		}
		rows, err := h.queryServiceWorkloadCandidates(ctx, "w.name = $service_name", "service_name", selector.ServiceName, selector, repoID, limit, "workload_name")
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
	truncated := len(candidates) > serviceWorkloadCandidateLimit
	if truncated {
		candidates = candidates[:serviceWorkloadCandidateLimit]
	}
	return candidates, truncated, nil
}

func (h *EntityHandler) queryServiceWorkloadCandidates(
	ctx context.Context,
	whereClause string,
	paramName string,
	paramValue string,
	selector serviceWorkloadSelector,
	repoID string,
	limit int,
	matchBasis string,
) ([]serviceWorkloadCandidate, error) {
	params := map[string]any{paramName: paramValue}
	whereParts := []string{whereClause}
	if repoID != "" {
		whereParts = append(whereParts, "w.repo_id = $repo_id")
		params["repo_id"] = repoID
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
	selector serviceWorkloadSelector,
	repoID string,
	limit int,
	matchBasis string,
) ([]serviceWorkloadCandidate, error) {
	params := map[string]any{"service_name": selector.ServiceName}
	whereParts := []string{"w.id = i.workload_id"}
	if repoID != "" {
		whereParts = append(whereParts, "w.repo_id = $repo_id")
		params["repo_id"] = repoID
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
) ([]serviceWorkloadCandidate, error) {
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	candidates := make([]serviceWorkloadCandidate, 0, len(rows))
	for _, row := range rows {
		candidate := serviceWorkloadCandidate{
			ID:          StringVal(row, "id"),
			Name:        StringVal(row, "name"),
			Kind:        StringVal(row, "kind"),
			RepoID:      StringVal(row, "repo_id"),
			RepoName:    StringVal(row, "repo_name"),
			Environment: StringVal(row, "environment"),
			MatchBasis:  matchBasis,
		}
		if candidate.ID == "" {
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func dedupeServiceWorkloadCandidates(input []serviceWorkloadCandidate) []serviceWorkloadCandidate {
	seen := make(map[string]int, len(input))
	output := make([]serviceWorkloadCandidate, 0, len(input))
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
	slices.SortFunc(output, func(a, b serviceWorkloadCandidate) int {
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

func traceServiceSelectorDisplay(selector serviceWorkloadSelector) string {
	if selector.ServiceID != "" {
		return selector.ServiceID
	}
	return selector.ServiceName
}

func serviceWorkloadCandidateMaps(candidates []serviceWorkloadCandidate) []map[string]any {
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

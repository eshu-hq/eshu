package reducer

import (
	"context"
	"slices"
	"strings"
)

// RepoDependencyEdge is one canonical repository DEPENDS_ON edge.
type RepoDependencyEdge struct {
	SourceRepoID string
	TargetRepoID string
}

// RepoWorkload captures one workload currently defined by a repository.
type RepoWorkload struct {
	RepoID     string
	WorkloadID string
}

// WorkloadDependencyEdgeRow is one canonical workload dependency edge payload.
type WorkloadDependencyEdgeRow struct {
	RepoID           string
	WorkloadID       string
	TargetRepoID     string
	TargetWorkloadID string
}

// WorkloadDependencyGraphLookup resolves canonical repo dependencies and
// repository-owned workloads from the graph.
type WorkloadDependencyGraphLookup interface {
	ListRepoDependencyEdges(ctx context.Context, repoIDs []string) ([]RepoDependencyEdge, error)
	ListRepoWorkloads(ctx context.Context, repoIDs []string) ([]RepoWorkload, error)
	// ListWorkloadDependencyEdges returns existing workload dependency edges
	// owned by the given repositories for the specified evidence source.
	ListWorkloadDependencyEdges(ctx context.Context, repoIDs []string, evidenceSource string) ([]ExistingWorkloadDependencyEdge, error)
}

// ReconcileWorkloadDependencyEdges builds workload dependency edges for the
// current materialized repositories. Current repositories own authoritative
// retraction of their outgoing workload dependencies; incoming rows are
// opportunistically upserted so late target materialization can complete a
// previously blocked edge without inventing ambiguity.
func ReconcileWorkloadDependencyEdges(
	ctx context.Context,
	descriptors []RepoDescriptor,
	lookup WorkloadDependencyGraphLookup,
) ([]WorkloadDependencyEdgeRow, []SharedProjectionIntentRow, error) {
	if len(descriptors) == 0 || lookup == nil {
		return nil, nil, nil
	}

	currentWorkloadsByRepo := make(map[string][]string)
	currentRepoIDs := make([]string, 0, len(descriptors))
	seenCurrentRepos := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		repoID := strings.TrimSpace(descriptor.RepoID)
		workloadID := strings.TrimSpace(descriptor.WorkloadID)
		if repoID == "" || workloadID == "" {
			continue
		}
		currentWorkloadsByRepo[repoID] = appendUniqueWorkloadString(currentWorkloadsByRepo[repoID], workloadID)
		if _, ok := seenCurrentRepos[repoID]; ok {
			continue
		}
		seenCurrentRepos[repoID] = struct{}{}
		currentRepoIDs = append(currentRepoIDs, repoID)
	}
	if len(currentRepoIDs) == 0 {
		return nil, nil, nil
	}
	slices.Sort(currentRepoIDs)

	repoEdges, err := lookup.ListRepoDependencyEdges(ctx, currentRepoIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(repoEdges) == 0 {
		retractRows, err := buildExistingWorkloadDependencyRetractRows(ctx, lookup, currentRepoIDs)
		if err != nil {
			return nil, nil, err
		}
		return nil, retractRows, nil
	}

	workloadRepoIDs := make([]string, 0, len(currentRepoIDs))
	seenWorkloadRepos := make(map[string]struct{}, len(currentRepoIDs))
	for _, repoID := range currentRepoIDs {
		workloadRepoIDs = append(workloadRepoIDs, repoID)
		seenWorkloadRepos[repoID] = struct{}{}
	}
	for _, edge := range repoEdges {
		for _, repoID := range []string{strings.TrimSpace(edge.SourceRepoID), strings.TrimSpace(edge.TargetRepoID)} {
			if repoID == "" {
				continue
			}
			if _, ok := seenWorkloadRepos[repoID]; ok {
				continue
			}
			seenWorkloadRepos[repoID] = struct{}{}
			workloadRepoIDs = append(workloadRepoIDs, repoID)
		}
	}
	slices.Sort(workloadRepoIDs)

	workloads, err := lookup.ListRepoWorkloads(ctx, workloadRepoIDs)
	if err != nil {
		return nil, nil, err
	}

	workloadsByRepo := make(map[string][]string, len(currentWorkloadsByRepo))
	for repoID, workloadIDs := range currentWorkloadsByRepo {
		workloadsByRepo[repoID] = append([]string(nil), workloadIDs...)
	}
	for _, workload := range workloads {
		repoID := strings.TrimSpace(workload.RepoID)
		workloadID := strings.TrimSpace(workload.WorkloadID)
		if repoID == "" || workloadID == "" {
			continue
		}
		workloadsByRepo[repoID] = appendUniqueWorkloadString(workloadsByRepo[repoID], workloadID)
	}

	seenRows := make(map[string]struct{})
	rows := make([]WorkloadDependencyEdgeRow, 0, len(repoEdges))
	for _, edge := range repoEdges {
		sourceRepoID := strings.TrimSpace(edge.SourceRepoID)
		targetRepoID := strings.TrimSpace(edge.TargetRepoID)
		if sourceRepoID == "" || targetRepoID == "" {
			continue
		}

		sourceWorkloads := workloadsByRepo[sourceRepoID]
		targetWorkloads := workloadsByRepo[targetRepoID]
		if len(sourceWorkloads) != 1 || len(targetWorkloads) != 1 {
			continue
		}

		if _, currentSource := seenCurrentRepos[sourceRepoID]; !currentSource {
			if _, currentTarget := seenCurrentRepos[targetRepoID]; !currentTarget {
				continue
			}
		}

		row := WorkloadDependencyEdgeRow{
			RepoID:           sourceRepoID,
			WorkloadID:       sourceWorkloads[0],
			TargetRepoID:     targetRepoID,
			TargetWorkloadID: targetWorkloads[0],
		}
		key := row.RepoID + "|" + row.WorkloadID + "|" + row.TargetWorkloadID
		if _, ok := seenRows[key]; ok {
			continue
		}
		seenRows[key] = struct{}{}
		rows = append(rows, row)
	}

	slices.SortFunc(rows, func(left, right WorkloadDependencyEdgeRow) int {
		if left.RepoID != right.RepoID {
			return strings.Compare(left.RepoID, right.RepoID)
		}
		if left.WorkloadID != right.WorkloadID {
			return strings.Compare(left.WorkloadID, right.WorkloadID)
		}
		return strings.Compare(left.TargetWorkloadID, right.TargetWorkloadID)
	})

	retractRows, err := buildExistingWorkloadDependencyRetractRows(ctx, lookup, currentRepoIDs)
	if err != nil {
		return nil, nil, err
	}
	return rows, retractRows, nil
}

// BuildWorkloadDependencyIntentRowsFromEdges converts workload dependency edge
// rows into shared edge-writer payloads.
func BuildWorkloadDependencyIntentRowsFromEdges(
	rows []WorkloadDependencyEdgeRow,
) []SharedProjectionIntentRow {
	if len(rows) == 0 {
		return nil
	}

	result := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.RepoID) == "" || strings.TrimSpace(row.WorkloadID) == "" || strings.TrimSpace(row.TargetWorkloadID) == "" {
			continue
		}
		result = append(result, SharedProjectionIntentRow{
			RepositoryID: row.RepoID,
			Payload: map[string]any{
				"repo_id":            row.RepoID,
				"workload_id":        row.WorkloadID,
				"target_repo_id":     row.TargetRepoID,
				"target_workload_id": row.TargetWorkloadID,
			},
		})
	}
	return result
}

func buildWorkloadDependencyRetractRows(repoIDs []string) []SharedProjectionIntentRow {
	rows := make([]SharedProjectionIntentRow, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		rows = append(rows, SharedProjectionIntentRow{
			RepositoryID: repoID,
			Payload:      map[string]any{"repo_id": repoID},
		})
	}
	return rows
}

func buildExistingWorkloadDependencyRetractRows(
	ctx context.Context,
	lookup WorkloadDependencyGraphLookup,
	repoIDs []string,
) ([]SharedProjectionIntentRow, error) {
	existingRows, err := lookup.ListWorkloadDependencyEdges(ctx, repoIDs, EvidenceSourceWorkloads)
	if err != nil {
		return nil, err
	}
	if len(existingRows) == 0 {
		return nil, nil
	}
	return buildWorkloadDependencyRetractRows(currentWorkloadDependencyRepoIDs(repoIDs, existingRows)), nil
}

// currentWorkloadDependencyRepoIDs narrows retract ownership to current
// repositories that actually have workload dependency edges in the graph.
func currentWorkloadDependencyRepoIDs(
	currentRepoIDs []string,
	existingRows []ExistingWorkloadDependencyEdge,
) []string {
	current := make(map[string]struct{}, len(currentRepoIDs))
	for _, repoID := range currentRepoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID != "" {
			current[repoID] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(currentRepoIDs))
	repoIDs := make([]string, 0, len(currentRepoIDs))
	for _, row := range existingRows {
		repoID := strings.TrimSpace(row.RepoID)
		if repoID == "" {
			continue
		}
		if _, ok := current[repoID]; !ok {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	slices.Sort(repoIDs)
	return repoIDs
}

func appendUniqueWorkloadString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

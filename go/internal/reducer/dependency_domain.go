package reducer

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ExistingRepoDependencyEdge represents one existing DEPENDS_ON edge between repositories.
type ExistingRepoDependencyEdge struct {
	RepoID           string
	TargetRepoID     string
	RelationshipType string
}

// ExistingWorkloadDependencyEdge represents one existing DEPENDS_ON edge between workloads.
type ExistingWorkloadDependencyEdge struct {
	RepoID           string
	WorkloadID       string
	TargetWorkloadID string
}

// BuildRepoDependencyIntentRows diffs desired repo dependency rows against
// existing edges and returns authoritative SharedProjectionIntentRow with
// upsert/retract actions in their Payload.
func BuildRepoDependencyIntentRows(
	repoDependencyRows []map[string]any,
	existingRows []ExistingRepoDependencyEdge,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	// Normalize rows by adding partition_key
	normalizedRows := make([]map[string]any, 0, len(repoDependencyRows))
	for _, row := range repoDependencyRows {
		repoID := anyToString(row["repo_id"])
		targetRepoID := anyToString(row["target_repo_id"])
		relationshipType := anyToString(row["relationship_type"])
		partitionKey := ""
		if repoID != "" && targetRepoID != "" {
			if relationshipType != "" && relationshipType != "DEPENDS_ON" {
				partitionKey = fmt.Sprintf("repo:%s->%s|%s", repoID, targetRepoID, relationshipType)
			} else {
				partitionKey = fmt.Sprintf("repo:%s->%s", repoID, targetRepoID)
			}
		}
		normalizedRow := make(map[string]any)
		for k, v := range row {
			normalizedRow[k] = v
		}
		normalizedRow["partition_key"] = partitionKey
		normalizedRows = append(normalizedRows, normalizedRow)
	}

	// Build existing pairs set
	existingPairs := make(map[string]struct{})
	for _, edge := range existingRows {
		partitionKey := ""
		if edge.RepoID != "" && edge.TargetRepoID != "" {
			if edge.RelationshipType != "" && edge.RelationshipType != "DEPENDS_ON" {
				partitionKey = fmt.Sprintf("repo:%s->%s|%s", edge.RepoID, edge.TargetRepoID, edge.RelationshipType)
			} else {
				partitionKey = fmt.Sprintf("repo:%s->%s", edge.RepoID, edge.TargetRepoID)
			}
		}
		key := fmt.Sprintf("%s|%s|%s|%s", edge.RepoID, edge.TargetRepoID, edge.RelationshipType, partitionKey)
		existingPairs[key] = struct{}{}
	}

	return buildDependencyIntents(
		DomainRepoDependency,
		normalizedRows,
		existingPairs,
		contextByRepoID,
		[]string{"repo_id", "target_repo_id", "relationship_type", "partition_key"},
		createdAt,
	)
}

// BuildWorkloadDependencyIntentRows diffs desired workload dependency rows
// against existing edges and returns authoritative SharedProjectionIntentRow
// with upsert/retract actions in their Payload.
func BuildWorkloadDependencyIntentRows(
	workloadDependencyRows []map[string]any,
	existingRows []ExistingWorkloadDependencyEdge,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	// Normalize rows by adding partition_key
	normalizedRows := make([]map[string]any, 0, len(workloadDependencyRows))
	for _, row := range workloadDependencyRows {
		workloadID := anyToString(row["workload_id"])
		targetWorkloadID := anyToString(row["target_workload_id"])
		partitionKey := ""
		if workloadID != "" && targetWorkloadID != "" {
			partitionKey = fmt.Sprintf("workload:%s->%s", workloadID, targetWorkloadID)
		}
		normalizedRow := make(map[string]any)
		for k, v := range row {
			normalizedRow[k] = v
		}
		normalizedRow["partition_key"] = partitionKey
		normalizedRows = append(normalizedRows, normalizedRow)
	}

	// Build existing pairs set
	existingPairs := make(map[string]struct{})
	for _, edge := range existingRows {
		partitionKey := ""
		if edge.WorkloadID != "" && edge.TargetWorkloadID != "" {
			partitionKey = fmt.Sprintf("workload:%s->%s", edge.WorkloadID, edge.TargetWorkloadID)
		}
		key := fmt.Sprintf("%s|%s|%s|%s", edge.RepoID, edge.WorkloadID, edge.TargetWorkloadID, partitionKey)
		existingPairs[key] = struct{}{}
	}

	return buildDependencyIntents(
		DomainWorkloadDependency,
		normalizedRows,
		existingPairs,
		contextByRepoID,
		[]string{"repo_id", "workload_id", "target_workload_id", "partition_key"},
		createdAt,
	)
}

// SharedDependencyProjectionMetrics returns completion-fencing metadata for
// authoritative dependency cutover.
func SharedDependencyProjectionMetrics(
	intentRows []SharedProjectionIntentRow,
	contextByRepoID map[string]ProjectionContext,
) map[string]any {
	// Collect touched repositories
	touchedRepos := make(map[string]struct{})
	for _, row := range intentRows {
		if strings.TrimSpace(row.RepositoryID) != "" {
			touchedRepos[row.RepositoryID] = struct{}{}
		}
	}

	if len(touchedRepos) == 0 || len(contextByRepoID) == 0 {
		return map[string]any{}
	}

	// Collect generation IDs
	generationIDs := make(map[string]struct{})
	for repoID := range touchedRepos {
		if context, ok := contextByRepoID[repoID]; ok {
			generationIDs[context.GenerationID] = struct{}{}
		}
	}

	// Collect unique domains and sort them
	domainSet := make(map[string]struct{})
	for _, row := range intentRows {
		if row.ProjectionDomain != "" {
			domainSet[row.ProjectionDomain] = struct{}{}
		}
	}
	domains := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	// Build metrics
	metrics := map[string]any{
		"authoritative_domains": domains,
		"intent_count":          len(intentRows),
	}

	// Only set accepted_generation_id if exactly one generation
	if len(generationIDs) == 1 {
		for genID := range generationIDs {
			metrics["accepted_generation_id"] = genID
			break
		}
	} else {
		metrics["accepted_generation_id"] = nil
	}

	return metrics
}

// buildDependencyIntents returns authoritative upsert and retract intents for
// one dependency domain.
func buildDependencyIntents(
	projectionDomain string,
	desiredRows []map[string]any,
	existingPairs map[string]struct{},
	contextByRepoID map[string]ProjectionContext,
	desiredPairFields []string,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	if len(contextByRepoID) == 0 {
		return []SharedProjectionIntentRow{}
	}

	// Build desired pairs set
	desiredPairs := make(map[string]struct{})
	for _, row := range desiredRows {
		fields := make([]string, len(desiredPairFields))
		for i, field := range desiredPairFields {
			fields[i] = anyToString(row[field])
		}
		key := strings.Join(fields, "|")
		desiredPairs[key] = struct{}{}
	}

	var rows []SharedProjectionIntentRow

	// Create upsert intents for desired rows
	for _, row := range desiredRows {
		repositoryID := anyToString(row["repo_id"])
		partitionKey := anyToString(row["partition_key"])

		context, hasContext := contextByRepoID[repositoryID]
		if repositoryID == "" || !hasContext || partitionKey == "" {
			continue
		}

		payload := make(map[string]any)
		for k, v := range row {
			payload[k] = v
		}
		payload["action"] = "upsert"

		intent := BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: projectionDomain,
			PartitionKey:     partitionKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		})
		rows = append(rows, intent)
	}

	// Create retract intents for (existing - desired)
	retractPairs := make([]string, 0)
	for pair := range existingPairs {
		if _, desired := desiredPairs[pair]; !desired {
			retractPairs = append(retractPairs, pair)
		}
	}
	sort.Strings(retractPairs)

	for _, pair := range retractPairs {
		fields := strings.Split(pair, "|")
		if len(fields) != len(desiredPairFields) {
			continue
		}

		repositoryID := fields[0]
		context, hasContext := contextByRepoID[repositoryID]
		if !hasContext {
			continue
		}

		payload := make(map[string]any)
		for i, field := range desiredPairFields {
			payload[field] = fields[i]
		}
		payload["action"] = "retract"

		partitionKey := fields[len(fields)-1] // last field is always partition_key

		intent := BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: projectionDomain,
			PartitionKey:     partitionKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          payload,
			CreatedAt:        createdAt,
		})
		rows = append(rows, intent)
	}

	return rows
}

// anyToString extracts a string from an any value, returning empty string if nil.
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

package reducer

import (
	"fmt"
	"strings"
	"time"
)

// ProjectionContext holds the bounded-unit freshness context for one shared
// projection repository slice.
type ProjectionContext struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
}

// EmitPlatformInfraIntents builds platform_infra intent rows from descriptor rows.
// Rows missing repo_id, platform_id, or projection context are skipped.
func EmitPlatformInfraIntents(
	descriptorRows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	return intentRowsForPlatformDomain(
		descriptorRows,
		contextByRepoID,
		"platform_infra",
		createdAt,
		false,
	)
}

// EmitPlatformRuntimeIntents builds shadow_platform_runtime intent rows from
// runtime platform rows, marking them completed immediately.
func EmitPlatformRuntimeIntents(
	runtimePlatformRows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	return intentRowsForPlatformDomain(
		runtimePlatformRows,
		contextByRepoID,
		"shadow_platform_runtime",
		createdAt,
		true,
	)
}

// EmitDependencyIntents builds shadow_repo_dependency and
// shadow_workload_dependency intent rows, marking them completed immediately.
func EmitDependencyIntents(
	repoDependencyRows []map[string]any,
	workloadDependencyRows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	createdAt time.Time,
) []SharedProjectionIntentRow {
	var rows []SharedProjectionIntentRow

	// Process repo dependencies
	for _, row := range repoDependencyRows {
		repositoryID := getString(row, "repo_id")
		targetRepoID := getString(row, "target_repo_id")
		if repositoryID == "" || targetRepoID == "" {
			continue
		}

		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}

		partitionKey := fmt.Sprintf("repo:%s->%s", repositoryID, targetRepoID)
		intent := BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: "shadow_repo_dependency",
			PartitionKey:     partitionKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          copyPayload(row),
			CreatedAt:        createdAt,
		})
		intent.CompletedAt = &createdAt
		rows = append(rows, intent)
	}

	// Process workload dependencies
	for _, row := range workloadDependencyRows {
		repositoryID := getString(row, "repo_id")
		workloadID := getString(row, "workload_id")
		targetWorkloadID := getString(row, "target_workload_id")
		if repositoryID == "" || workloadID == "" || targetWorkloadID == "" {
			continue
		}

		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}

		partitionKey := fmt.Sprintf("workload:%s->%s", workloadID, targetWorkloadID)
		intent := BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: "shadow_workload_dependency",
			PartitionKey:     partitionKey,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          copyPayload(row),
			CreatedAt:        createdAt,
		})
		intent.CompletedAt = &createdAt
		rows = append(rows, intent)
	}

	return rows
}

// intentRowsForPlatformDomain builds intent rows for platform-domain projections.
// It skips rows missing repo_id, platform_id, or projection context.
func intentRowsForPlatformDomain(
	descriptorRows []map[string]any,
	contextByRepoID map[string]ProjectionContext,
	projectionDomain string,
	createdAt time.Time,
	markCompleted bool,
) []SharedProjectionIntentRow {
	var rows []SharedProjectionIntentRow

	for _, descriptor := range descriptorRows {
		repositoryID := getString(descriptor, "repo_id")
		platformID := getString(descriptor, "platform_id")
		if repositoryID == "" || platformID == "" {
			continue
		}

		context, ok := contextByRepoID[repositoryID]
		if !ok {
			continue
		}

		intent := BuildSharedProjectionIntent(SharedProjectionIntentInput{
			ProjectionDomain: projectionDomain,
			PartitionKey:     platformID,
			ScopeID:          context.ScopeID,
			AcceptanceUnitID: context.acceptanceUnitID(repositoryID),
			RepositoryID:     repositoryID,
			SourceRunID:      context.SourceRunID,
			GenerationID:     context.GenerationID,
			Payload:          copyPayload(descriptor),
			CreatedAt:        createdAt,
		})

		if markCompleted {
			intent.CompletedAt = &createdAt
		}

		rows = append(rows, intent)
	}

	return rows
}

// getString extracts a string value from a map, returning empty string if the
// key is missing or the value is not a string.
func getString(m map[string]any, key string) string {
	val, ok := m[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

// copyPayload creates a shallow copy of the payload map to match Python's
// {key: value for key, value in descriptor.items()} behavior.
func copyPayload(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func (c ProjectionContext) acceptanceUnitID(repositoryID string) string {
	if unitID := strings.TrimSpace(c.AcceptanceUnitID); unitID != "" {
		return unitID
	}
	return strings.TrimSpace(repositoryID)
}

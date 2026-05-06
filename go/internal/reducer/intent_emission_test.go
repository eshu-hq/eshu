package reducer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestEmitPlatformInfraIntents_Empty verifies empty inputs return empty results.
func TestEmitPlatformInfraIntents_Empty(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{}

	rows := EmitPlatformInfraIntents(nil, contextMap, createdAt)
	assert.Empty(t, rows)

	rows = EmitPlatformInfraIntents([]map[string]any{}, contextMap, createdAt)
	assert.Empty(t, rows)
}

// TestEmitPlatformInfraIntents_MissingContext verifies rows without matching
// context are skipped.
func TestEmitPlatformInfraIntents_MissingContext(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	descriptorRows := []map[string]any{
		{
			"repo_id":     "repo-2", // no context for this repo
			"platform_id": "platform-a",
			"region":      "us-west-2",
		},
	}

	rows := EmitPlatformInfraIntents(descriptorRows, contextMap, createdAt)
	assert.Empty(t, rows)
}

// TestEmitPlatformInfraIntents_MissingRequiredFields verifies rows with
// missing repo_id or platform_id are skipped.
func TestEmitPlatformInfraIntents_MissingRequiredFields(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	descriptorRows := []map[string]any{
		{
			"platform_id": "platform-a",
			"region":      "us-west-2",
			// missing repo_id
		},
		{
			"repo_id": "repo-1",
			"region":  "us-west-2",
			// missing platform_id
		},
		{
			"repo_id":     "",
			"platform_id": "platform-a",
			"region":      "us-west-2",
		},
	}

	rows := EmitPlatformInfraIntents(descriptorRows, contextMap, createdAt)
	assert.Empty(t, rows)
}

// TestEmitPlatformInfraIntents_HappyPath verifies platform_infra intent rows
// are built correctly.
func TestEmitPlatformInfraIntents_HappyPath(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	descriptorRows := []map[string]any{
		{
			"repo_id":     "repo-1",
			"platform_id": "platform-a",
			"region":      "us-west-2",
			"account_id":  "123456789012",
		},
	}

	rows := EmitPlatformInfraIntents(descriptorRows, contextMap, createdAt)

	assert.Len(t, rows, 1)
	assert.Equal(t, "platform_infra", rows[0].ProjectionDomain)
	assert.Equal(t, "platform-a", rows[0].PartitionKey)
	assert.Equal(t, "repo-1", rows[0].RepositoryID)
	assert.Equal(t, "run-1", rows[0].SourceRunID)
	assert.Equal(t, "gen-1", rows[0].GenerationID)
	assert.Equal(t, createdAt, rows[0].CreatedAt)
	assert.Nil(t, rows[0].CompletedAt)
	assert.Equal(t, "repo-1", rows[0].Payload["repo_id"])
	assert.Equal(t, "platform-a", rows[0].Payload["platform_id"])
	assert.Equal(t, "us-west-2", rows[0].Payload["region"])
	assert.Equal(t, "123456789012", rows[0].Payload["account_id"])
}

// TestEmitPlatformInfraIntents_MixedValidInvalid verifies mixed rows return
// only valid intents.
func TestEmitPlatformInfraIntents_MixedValidInvalid(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	descriptorRows := []map[string]any{
		{
			"repo_id":     "repo-1",
			"platform_id": "platform-a",
			"region":      "us-west-2",
		},
		{
			"platform_id": "platform-b", // missing repo_id
			"region":      "us-east-1",
		},
		{
			"repo_id":     "repo-1",
			"platform_id": "platform-c",
			"region":      "eu-west-1",
		},
		{
			"repo_id":     "repo-2", // no context
			"platform_id": "platform-d",
			"region":      "ap-south-1",
		},
	}

	rows := EmitPlatformInfraIntents(descriptorRows, contextMap, createdAt)

	assert.Len(t, rows, 2)
	assert.Equal(t, "platform-a", rows[0].PartitionKey)
	assert.Equal(t, "platform-c", rows[1].PartitionKey)
}

// TestEmitPlatformRuntimeIntents_Empty verifies empty inputs return empty results.
func TestEmitPlatformRuntimeIntents_Empty(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{}

	rows := EmitPlatformRuntimeIntents(nil, contextMap, createdAt)
	assert.Empty(t, rows)

	rows = EmitPlatformRuntimeIntents([]map[string]any{}, contextMap, createdAt)
	assert.Empty(t, rows)
}

// TestEmitPlatformRuntimeIntents_HappyPath verifies shadow_platform_runtime
// intent rows are built with completed_at set.
func TestEmitPlatformRuntimeIntents_HappyPath(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	runtimePlatformRows := []map[string]any{
		{
			"repo_id":     "repo-1",
			"platform_id": "platform-runtime-a",
			"runtime":     "python3.11",
		},
	}

	rows := EmitPlatformRuntimeIntents(runtimePlatformRows, contextMap, createdAt)

	assert.Len(t, rows, 1)
	assert.Equal(t, "shadow_platform_runtime", rows[0].ProjectionDomain)
	assert.Equal(t, "platform-runtime-a", rows[0].PartitionKey)
	assert.Equal(t, "repo-1", rows[0].RepositoryID)
	assert.Equal(t, "run-1", rows[0].SourceRunID)
	assert.Equal(t, "gen-1", rows[0].GenerationID)
	assert.Equal(t, createdAt, rows[0].CreatedAt)
	assert.NotNil(t, rows[0].CompletedAt)
	assert.Equal(t, createdAt, *rows[0].CompletedAt)
	assert.Equal(t, "python3.11", rows[0].Payload["runtime"])
}

// TestEmitDependencyIntents_Empty verifies empty inputs return empty results.
func TestEmitDependencyIntents_Empty(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{}

	rows := EmitDependencyIntents(nil, nil, contextMap, createdAt)
	assert.Empty(t, rows)

	rows = EmitDependencyIntents([]map[string]any{}, []map[string]any{}, contextMap, createdAt)
	assert.Empty(t, rows)
}

// TestEmitDependencyIntents_MissingRequiredFields verifies rows with missing
// required fields are skipped.
func TestEmitDependencyIntents_MissingRequiredFields(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	repoDependencyRows := []map[string]any{
		{
			"target_repo_id": "repo-2", // missing repo_id
			"version":        "1.0.0",
		},
		{
			"repo_id": "repo-1", // missing target_repo_id
			"version": "1.0.0",
		},
		{
			"repo_id":        "",
			"target_repo_id": "repo-2",
		},
	}
	workloadDependencyRows := []map[string]any{
		{
			"workload_id":        "wl-1",
			"target_workload_id": "wl-2",
			// missing repo_id
		},
		{
			"repo_id":            "repo-1",
			"target_workload_id": "wl-2",
			// missing workload_id
		},
		{
			"repo_id":     "repo-1",
			"workload_id": "wl-1",
			// missing target_workload_id
		},
	}

	rows := EmitDependencyIntents(repoDependencyRows, workloadDependencyRows, contextMap, createdAt)
	assert.Empty(t, rows)
}

// TestEmitDependencyIntents_HappyPath verifies both repo and workload
// dependency intent rows are built correctly with completed_at set.
func TestEmitDependencyIntents_HappyPath(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
		"repo-2": {
			SourceRunID:  "run-2",
			GenerationID: "gen-2",
		},
	}
	repoDependencyRows := []map[string]any{
		{
			"repo_id":        "repo-1",
			"target_repo_id": "repo-2",
			"version":        "1.0.0",
		},
	}
	workloadDependencyRows := []map[string]any{
		{
			"repo_id":            "repo-2",
			"workload_id":        "wl-1",
			"target_workload_id": "wl-2",
			"dep_type":           "runtime",
		},
	}

	rows := EmitDependencyIntents(repoDependencyRows, workloadDependencyRows, contextMap, createdAt)

	assert.Len(t, rows, 2)

	// First row should be repo dependency
	repoDep := rows[0]
	assert.Equal(t, "shadow_repo_dependency", repoDep.ProjectionDomain)
	assert.Equal(t, "repo:repo-1->repo-2", repoDep.PartitionKey)
	assert.Equal(t, "repo-1", repoDep.RepositoryID)
	assert.Equal(t, "run-1", repoDep.SourceRunID)
	assert.Equal(t, "gen-1", repoDep.GenerationID)
	assert.Equal(t, createdAt, repoDep.CreatedAt)
	assert.NotNil(t, repoDep.CompletedAt)
	assert.Equal(t, createdAt, *repoDep.CompletedAt)
	assert.Equal(t, "repo-1", repoDep.Payload["repo_id"])
	assert.Equal(t, "repo-2", repoDep.Payload["target_repo_id"])
	assert.Equal(t, "1.0.0", repoDep.Payload["version"])

	// Second row should be workload dependency
	workloadDep := rows[1]
	assert.Equal(t, "shadow_workload_dependency", workloadDep.ProjectionDomain)
	assert.Equal(t, "workload:wl-1->wl-2", workloadDep.PartitionKey)
	assert.Equal(t, "repo-2", workloadDep.RepositoryID)
	assert.Equal(t, "run-2", workloadDep.SourceRunID)
	assert.Equal(t, "gen-2", workloadDep.GenerationID)
	assert.Equal(t, createdAt, workloadDep.CreatedAt)
	assert.NotNil(t, workloadDep.CompletedAt)
	assert.Equal(t, createdAt, *workloadDep.CompletedAt)
	assert.Equal(t, "repo-2", workloadDep.Payload["repo_id"])
	assert.Equal(t, "wl-1", workloadDep.Payload["workload_id"])
	assert.Equal(t, "wl-2", workloadDep.Payload["target_workload_id"])
	assert.Equal(t, "runtime", workloadDep.Payload["dep_type"])
}

// TestEmitDependencyIntents_MixedValidInvalid verifies mixed rows return only
// valid intents.
func TestEmitDependencyIntents_MixedValidInvalid(t *testing.T) {
	createdAt := time.Now().UTC()
	contextMap := map[string]ProjectionContext{
		"repo-1": {
			SourceRunID:  "run-1",
			GenerationID: "gen-1",
		},
	}
	repoDependencyRows := []map[string]any{
		{
			"repo_id":        "repo-1",
			"target_repo_id": "repo-2",
			"version":        "1.0.0",
		},
		{
			"target_repo_id": "repo-3", // missing repo_id
			"version":        "2.0.0",
		},
		{
			"repo_id":        "repo-99", // no context
			"target_repo_id": "repo-100",
			"version":        "3.0.0",
		},
	}
	workloadDependencyRows := []map[string]any{
		{
			"repo_id":            "repo-1",
			"workload_id":        "wl-1",
			"target_workload_id": "wl-2",
		},
		{
			"repo_id":     "repo-1",
			"workload_id": "wl-3",
			// missing target_workload_id
		},
	}

	rows := EmitDependencyIntents(repoDependencyRows, workloadDependencyRows, contextMap, createdAt)

	assert.Len(t, rows, 2)
	assert.Equal(t, "shadow_repo_dependency", rows[0].ProjectionDomain)
	assert.Equal(t, "shadow_workload_dependency", rows[1].ProjectionDomain)
}

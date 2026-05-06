package reducer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRepoDependencyIntentRows_Empty(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	intents := BuildRepoDependencyIntentRows(
		[]map[string]any{},
		[]ExistingRepoDependencyEdge{},
		contextByRepoID,
		createdAt,
	)

	assert.Empty(t, intents)
}

func TestBuildRepoDependencyIntentRows_AllUpserts(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
		"repo2": {SourceRunID: "run2", GenerationID: "gen1"},
	}

	desiredRows := []map[string]any{
		{"repo_id": "repo1", "target_repo_id": "repo2"},
		{"repo_id": "repo2", "target_repo_id": "repo1"},
	}

	intents := BuildRepoDependencyIntentRows(
		desiredRows,
		[]ExistingRepoDependencyEdge{},
		contextByRepoID,
		createdAt,
	)

	require.Len(t, intents, 2)

	// All should be upserts
	for _, intent := range intents {
		assert.Equal(t, DomainRepoDependency, intent.ProjectionDomain)
		assert.Equal(t, "upsert", intent.Payload["action"])
		assert.NotEmpty(t, intent.IntentID)
		assert.NotEmpty(t, intent.PartitionKey)
		assert.Equal(t, createdAt, intent.CreatedAt)
		assert.Nil(t, intent.CompletedAt)
	}

	// Check partition keys
	assert.Equal(t, "repo:repo1->repo2", intents[0].PartitionKey)
	assert.Equal(t, "repo:repo2->repo1", intents[1].PartitionKey)
}

func TestBuildRepoDependencyIntentRows_AllRetracts(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	existingRows := []ExistingRepoDependencyEdge{
		{RepoID: "repo1", TargetRepoID: "repo2"},
		{RepoID: "repo1", TargetRepoID: "repo3"},
	}

	intents := BuildRepoDependencyIntentRows(
		[]map[string]any{},
		existingRows,
		contextByRepoID,
		createdAt,
	)

	require.Len(t, intents, 2)

	// All should be retracts
	for _, intent := range intents {
		assert.Equal(t, DomainRepoDependency, intent.ProjectionDomain)
		assert.Equal(t, "retract", intent.Payload["action"])
		assert.Equal(t, "repo1", intent.RepositoryID)
		assert.Equal(t, "run1", intent.SourceRunID)
		assert.Equal(t, "gen1", intent.GenerationID)
	}
}

func TestBuildRepoDependencyIntentRows_MixedUpsertAndRetract(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	desiredRows := []map[string]any{
		{"repo_id": "repo1", "target_repo_id": "repo2"},
		{"repo_id": "repo1", "target_repo_id": "repo4"},
	}

	existingRows := []ExistingRepoDependencyEdge{
		{RepoID: "repo1", TargetRepoID: "repo2"}, // keep this
		{RepoID: "repo1", TargetRepoID: "repo3"}, // retract this
	}

	intents := BuildRepoDependencyIntentRows(
		desiredRows,
		existingRows,
		contextByRepoID,
		createdAt,
	)

	require.Len(t, intents, 3)

	upserts := 0
	retracts := 0
	for _, intent := range intents {
		switch intent.Payload["action"] {
		case "upsert":
			upserts++
		case "retract":
			retracts++
		}
	}

	assert.Equal(t, 2, upserts, "expected 2 upserts (repo2 already exists, repo4 is new)")
	assert.Equal(t, 1, retracts, "expected 1 retract (repo3 no longer desired)")
}

func TestBuildRepoDependencyIntentRows_MissingContext(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
		// repo2 context missing
	}

	desiredRows := []map[string]any{
		{"repo_id": "repo1", "target_repo_id": "repo2"},
		{"repo_id": "repo2", "target_repo_id": "repo3"}, // should be skipped
	}

	intents := BuildRepoDependencyIntentRows(
		desiredRows,
		[]ExistingRepoDependencyEdge{},
		contextByRepoID,
		createdAt,
	)

	// Only repo1 should emit an intent
	require.Len(t, intents, 1)
	assert.Equal(t, "repo1", intents[0].RepositoryID)
}

func TestBuildRepoDependencyIntentRows_PreservesTypedRelationshipsForSamePair(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	desiredRows := []map[string]any{
		{
			"repo_id":           "repo1",
			"target_repo_id":    "repo2",
			"relationship_type": "DEPLOYS_FROM",
		},
		{
			"repo_id":           "repo1",
			"target_repo_id":    "repo2",
			"relationship_type": "DISCOVERS_CONFIG_IN",
		},
	}

	intents := BuildRepoDependencyIntentRows(
		desiredRows,
		nil,
		contextByRepoID,
		createdAt,
	)

	require.Len(t, intents, 2)

	gotTypes := map[string]bool{}
	for _, intent := range intents {
		assert.Equal(t, "repo1", intent.RepositoryID)
		assert.NotEmpty(t, intent.PartitionKey)
		gotTypes[anyToString(intent.Payload["relationship_type"])] = true
	}

	assert.True(t, gotTypes["DEPLOYS_FROM"])
	assert.True(t, gotTypes["DISCOVERS_CONFIG_IN"])
	assert.NotEqual(t, intents[0].PartitionKey, intents[1].PartitionKey)
}

func TestBuildWorkloadDependencyIntentRows_Empty(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	intents := BuildWorkloadDependencyIntentRows(
		[]map[string]any{},
		[]ExistingWorkloadDependencyEdge{},
		contextByRepoID,
		createdAt,
	)

	assert.Empty(t, intents)
}

func TestBuildWorkloadDependencyIntentRows_AllUpserts(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	desiredRows := []map[string]any{
		{
			"repo_id":            "repo1",
			"workload_id":        "wl1",
			"target_workload_id": "wl2",
		},
		{
			"repo_id":            "repo1",
			"workload_id":        "wl1",
			"target_workload_id": "wl3",
		},
	}

	intents := BuildWorkloadDependencyIntentRows(
		desiredRows,
		[]ExistingWorkloadDependencyEdge{},
		contextByRepoID,
		createdAt,
	)

	require.Len(t, intents, 2)

	// All should be upserts
	for _, intent := range intents {
		assert.Equal(t, DomainWorkloadDependency, intent.ProjectionDomain)
		assert.Equal(t, "upsert", intent.Payload["action"])
		assert.Equal(t, "repo1", intent.RepositoryID)
		assert.NotEmpty(t, intent.PartitionKey)
	}

	// Check partition keys
	assert.Equal(t, "workload:wl1->wl2", intents[0].PartitionKey)
	assert.Equal(t, "workload:wl1->wl3", intents[1].PartitionKey)
}

func TestBuildWorkloadDependencyIntentRows_MixedUpsertAndRetract(t *testing.T) {
	createdAt := time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
	}

	desiredRows := []map[string]any{
		{
			"repo_id":            "repo1",
			"workload_id":        "wl1",
			"target_workload_id": "wl2",
		},
	}

	existingRows := []ExistingWorkloadDependencyEdge{
		{RepoID: "repo1", WorkloadID: "wl1", TargetWorkloadID: "wl2"}, // keep
		{RepoID: "repo1", WorkloadID: "wl1", TargetWorkloadID: "wl3"}, // retract
	}

	intents := BuildWorkloadDependencyIntentRows(
		desiredRows,
		existingRows,
		contextByRepoID,
		createdAt,
	)

	require.Len(t, intents, 2)

	upserts := 0
	retracts := 0
	for _, intent := range intents {
		switch intent.Payload["action"] {
		case "upsert":
			upserts++
		case "retract":
			retracts++
		}
	}

	assert.Equal(t, 1, upserts)
	assert.Equal(t, 1, retracts)
}

func TestSharedDependencyProjectionMetrics_Empty(t *testing.T) {
	metrics := SharedDependencyProjectionMetrics(
		[]SharedProjectionIntentRow{},
		map[string]ProjectionContext{},
	)

	assert.Empty(t, metrics)
}

func TestSharedDependencyProjectionMetrics_SingleGeneration(t *testing.T) {
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
		"repo2": {SourceRunID: "run2", GenerationID: "gen1"},
	}

	intents := []SharedProjectionIntentRow{
		{
			RepositoryID:     "repo1",
			ProjectionDomain: DomainRepoDependency,
		},
		{
			RepositoryID:     "repo2",
			ProjectionDomain: DomainRepoDependency,
		},
	}

	metrics := SharedDependencyProjectionMetrics(intents, contextByRepoID)

	require.NotEmpty(t, metrics)
	assert.Equal(t, []string{DomainRepoDependency}, metrics["authoritative_domains"])
	assert.Equal(t, "gen1", metrics["accepted_generation_id"])
	assert.Equal(t, 2, metrics["intent_count"])
}

func TestSharedDependencyProjectionMetrics_MultipleGenerations(t *testing.T) {
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
		"repo2": {SourceRunID: "run2", GenerationID: "gen2"},
	}

	intents := []SharedProjectionIntentRow{
		{
			RepositoryID:     "repo1",
			ProjectionDomain: DomainRepoDependency,
		},
		{
			RepositoryID:     "repo2",
			ProjectionDomain: DomainWorkloadDependency,
		},
	}

	metrics := SharedDependencyProjectionMetrics(intents, contextByRepoID)

	require.NotEmpty(t, metrics)
	assert.ElementsMatch(t, []string{DomainRepoDependency, DomainWorkloadDependency}, metrics["authoritative_domains"])
	assert.Nil(t, metrics["accepted_generation_id"], "should be nil with multiple generation IDs")
	assert.Equal(t, 2, metrics["intent_count"])
}

func TestSharedDependencyProjectionMetrics_MissingContext(t *testing.T) {
	contextByRepoID := map[string]ProjectionContext{
		"repo1": {SourceRunID: "run1", GenerationID: "gen1"},
		// repo2 missing
	}

	intents := []SharedProjectionIntentRow{
		{
			RepositoryID:     "repo1",
			ProjectionDomain: DomainRepoDependency,
		},
		{
			RepositoryID:     "repo2",
			ProjectionDomain: DomainRepoDependency,
		},
	}

	metrics := SharedDependencyProjectionMetrics(intents, contextByRepoID)

	// Should only count repo1's generation
	require.NotEmpty(t, metrics)
	assert.Equal(t, "gen1", metrics["accepted_generation_id"])
}

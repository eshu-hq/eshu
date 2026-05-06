package reducer

import (
	"testing"
	"time"
)

func TestBuildSharedProjectionIntentDeterministicID(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	input := SharedProjectionIntentInput{
		ProjectionDomain: DomainPlatformInfra,
		PartitionKey:     "pk-1",
		RepositoryID:     "repo-1",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{"key": "value"},
		CreatedAt:        now,
	}

	row1 := BuildSharedProjectionIntent(input)
	row2 := BuildSharedProjectionIntent(input)

	if row1.IntentID != row2.IntentID {
		t.Errorf("same inputs produced different IDs: %q vs %q", row1.IntentID, row2.IntentID)
	}
}

func TestBuildSharedProjectionIntentDifferentInputsDifferentIDs(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := SharedProjectionIntentInput{
		ProjectionDomain: DomainPlatformInfra,
		PartitionKey:     "pk-1",
		RepositoryID:     "repo-1",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{"key": "value"},
		CreatedAt:        now,
	}

	modified := base
	modified.RepositoryID = "repo-2"

	row1 := BuildSharedProjectionIntent(base)
	row2 := BuildSharedProjectionIntent(modified)

	if row1.IntentID == row2.IntentID {
		t.Errorf("different inputs produced same ID: %q", row1.IntentID)
	}
}

func TestBuildSharedProjectionIntentSetsAllFields(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	input := SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     "pk-test",
		ScopeID:          "scope-test",
		AcceptanceUnitID: "unit-test",
		RepositoryID:     "repo-test",
		SourceRunID:      "run-test",
		GenerationID:     "gen-test",
		Payload:          map[string]any{"hello": "world"},
		CreatedAt:        now,
	}

	row := BuildSharedProjectionIntent(input)

	if row.IntentID == "" {
		t.Error("IntentID is empty")
	}
	if row.ProjectionDomain != DomainRepoDependency {
		t.Errorf("ProjectionDomain = %q, want %q", row.ProjectionDomain, DomainRepoDependency)
	}
	if row.PartitionKey != "pk-test" {
		t.Errorf("PartitionKey = %q", row.PartitionKey)
	}
	if row.ScopeID != "scope-test" {
		t.Errorf("ScopeID = %q", row.ScopeID)
	}
	if row.AcceptanceUnitID != "unit-test" {
		t.Errorf("AcceptanceUnitID = %q", row.AcceptanceUnitID)
	}
	if row.RepositoryID != "repo-test" {
		t.Errorf("RepositoryID = %q", row.RepositoryID)
	}
	if row.SourceRunID != "run-test" {
		t.Errorf("SourceRunID = %q", row.SourceRunID)
	}
	if row.GenerationID != "gen-test" {
		t.Errorf("GenerationID = %q", row.GenerationID)
	}
	if row.Payload["hello"] != "world" {
		t.Errorf("Payload = %v", row.Payload)
	}
	if !row.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", row.CreatedAt, now)
	}
	if row.CompletedAt != nil {
		t.Errorf("CompletedAt should be nil, got %v", row.CompletedAt)
	}
}

func TestBuildSharedProjectionIntentDifferentAcceptanceIdentityDifferentIDs(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := SharedProjectionIntentInput{
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     "caller->callee",
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		CreatedAt:        now,
	}

	modifiedScope := base
	modifiedScope.ScopeID = "scope-b"
	modifiedUnit := base
	modifiedUnit.AcceptanceUnitID = "repo-b"

	baseRow := BuildSharedProjectionIntent(base)
	scopeRow := BuildSharedProjectionIntent(modifiedScope)
	unitRow := BuildSharedProjectionIntent(modifiedUnit)

	if baseRow.IntentID == scopeRow.IntentID {
		t.Fatal("different scope_id produced same intent ID")
	}
	if baseRow.IntentID == unitRow.IntentID {
		t.Fatal("different acceptance_unit_id produced same intent ID")
	}
}

func TestSharedProjectionIntentRowAcceptanceKeyPrefersExplicitFields(t *testing.T) {
	t.Parallel()

	row := SharedProjectionIntentRow{
		ScopeID:          "scope-explicit",
		AcceptanceUnitID: "unit-explicit",
		RepositoryID:     "repo-fallback",
		SourceRunID:      "run-1",
		Payload: map[string]any{
			"scope_id":           "scope-payload",
			"acceptance_unit_id": "unit-payload",
		},
	}

	key, ok := row.AcceptanceKey()
	if !ok {
		t.Fatal("AcceptanceKey() ok = false, want true")
	}
	if got, want := key.ScopeID, "scope-explicit"; got != want {
		t.Fatalf("key.ScopeID = %q, want %q", got, want)
	}
	if got, want := key.AcceptanceUnitID, "unit-explicit"; got != want {
		t.Fatalf("key.AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := key.SourceRunID, "run-1"; got != want {
		t.Fatalf("key.SourceRunID = %q, want %q", got, want)
	}
}

func TestBuildSharedProjectionIntentCrossLanguageParity(t *testing.T) {
	t.Parallel()

	// Verified against Python:
	// build_shared_projection_intent(
	//   projection_domain="platform_infra", partition_key="pk-1",
	//   repository_id="repo-1", source_run_id="run-1",
	//   generation_id="gen-1", scope_id="", acceptance_unit_id="repo-1",
	//   payload={}, created_at=...)
	// produces intent_id = "0200325eedd43adccebc04f7f0711824935c1cf9f1f09dee568e9b80ea194cbe"
	now := time.Now().UTC()
	input := SharedProjectionIntentInput{
		ProjectionDomain: DomainPlatformInfra,
		PartitionKey:     "pk-1",
		RepositoryID:     "repo-1",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{},
		CreatedAt:        now,
	}

	row := BuildSharedProjectionIntent(input)

	want := "0200325eedd43adccebc04f7f0711824935c1cf9f1f09dee568e9b80ea194cbe"
	if row.IntentID != want {
		t.Errorf("IntentID = %q, want %q (Python parity)", row.IntentID, want)
	}
}

func TestRowsForPartitionFiltersCorrectly(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		{IntentID: "i-1", PartitionKey: "pk-a"},
		{IntentID: "i-2", PartitionKey: "pk-b"},
		{IntentID: "i-3", PartitionKey: "pk-c"},
		{IntentID: "i-4", PartitionKey: "pk-d"},
	}

	partitionCount := 4
	var totalCollected int
	for pid := 0; pid < partitionCount; pid++ {
		filtered := RowsForPartition(rows, pid, partitionCount)
		totalCollected += len(filtered)
		for _, r := range filtered {
			got, err := PartitionForKey(r.PartitionKey, partitionCount)
			if err != nil {
				t.Fatalf("PartitionForKey(%q): %v", r.PartitionKey, err)
			}
			if got != pid {
				t.Errorf("row %q assigned to partition %d but RowsForPartition returned it for %d",
					r.IntentID, got, pid)
			}
		}
	}
	if totalCollected != len(rows) {
		t.Errorf("total collected = %d, want %d", totalCollected, len(rows))
	}
}

func TestRowsForPartitionEmptyForNoMatch(t *testing.T) {
	t.Parallel()

	result := RowsForPartition(nil, 0, 4)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}

	result = RowsForPartition([]SharedProjectionIntentRow{}, 0, 4)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(result))
	}
}

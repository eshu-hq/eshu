// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestBuildSharedProjectionIntentDeterministicID(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	input := sharedintent.Input{
		ProjectionDomain: DomainWorkloadDependency,
		PartitionKey:     "pk-1",
		RepositoryID:     "repo-1",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{"key": "value"},
		CreatedAt:        now,
	}

	row1 := sharedintent.Build(input)
	row2 := sharedintent.Build(input)

	if row1.IntentID != row2.IntentID {
		t.Errorf("same inputs produced different IDs: %q vs %q", row1.IntentID, row2.IntentID)
	}
}

func TestBuildSharedProjectionIntentDifferentInputsDifferentIDs(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := sharedintent.Input{
		ProjectionDomain: DomainWorkloadDependency,
		PartitionKey:     "pk-1",
		RepositoryID:     "repo-1",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{"key": "value"},
		CreatedAt:        now,
	}

	modified := base
	modified.RepositoryID = "repo-2"

	row1 := sharedintent.Build(base)
	row2 := sharedintent.Build(modified)

	if row1.IntentID == row2.IntentID {
		t.Errorf("different inputs produced same ID: %q", row1.IntentID)
	}
}

func TestBuildSharedProjectionIntentIdentityKeyPreservesStoredPartition(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := sharedintent.Input{
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     "code-calls:v1:files:repo-a:partition",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload:          map[string]any{"key": "value"},
		CreatedAt:        now,
	}
	first := base
	first.IdentityKey = base.PartitionKey + ":edge:caller->callee"
	second := base
	second.IdentityKey = base.PartitionKey + ":edge:caller->other"

	firstRow := sharedintent.Build(first)
	secondRow := sharedintent.Build(second)

	if got, want := firstRow.PartitionKey, base.PartitionKey; got != want {
		t.Fatalf("first PartitionKey = %q, want %q", got, want)
	}
	if got, want := secondRow.PartitionKey, base.PartitionKey; got != want {
		t.Fatalf("second PartitionKey = %q, want %q", got, want)
	}
	if firstRow.IntentID == secondRow.IntentID {
		t.Fatalf("IdentityKey rows produced duplicate IntentID %q", firstRow.IntentID)
	}
}

func TestBuildSharedProjectionIntentSetsAllFields(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Microsecond)
	input := sharedintent.Input{
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

	row := sharedintent.Build(input)

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
	base := sharedintent.Input{
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

	baseRow := sharedintent.Build(base)
	scopeRow := sharedintent.Build(modifiedScope)
	unitRow := sharedintent.Build(modifiedUnit)

	if baseRow.IntentID == scopeRow.IntentID {
		t.Fatal("different scope_id produced same intent ID")
	}
	if baseRow.IntentID == unitRow.IntentID {
		t.Fatal("different acceptance_unit_id produced same intent ID")
	}
}

func TestSharedProjectionIntentRowAcceptanceKeyPrefersExplicitFields(t *testing.T) {
	t.Parallel()

	row := sharedintent.Row{
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

func TestRowsForPartitionFiltersCorrectly(t *testing.T) {
	t.Parallel()

	rows := []sharedintent.Row{
		{IntentID: "i-1", PartitionKey: "pk-a"},
		{IntentID: "i-2", PartitionKey: "pk-b"},
		{IntentID: "i-3", PartitionKey: "pk-c"},
		{IntentID: "i-4", PartitionKey: "pk-d"},
	}

	partitionCount := 4
	var totalCollected int
	for pid := 0; pid < partitionCount; pid++ {
		filtered := sharedintent.RowsForPartition(rows, pid, partitionCount)
		totalCollected += len(filtered)
		for _, r := range filtered {
			got, err := sharedintent.PartitionForKey(r.PartitionKey, partitionCount)
			if err != nil {
				t.Fatalf("sharedintent.PartitionForKey(%q): %v", r.PartitionKey, err)
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

	result := sharedintent.RowsForPartition(nil, 0, 4)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}

	result = sharedintent.RowsForPartition([]sharedintent.Row{}, 0, 4)
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d", len(result))
	}
}

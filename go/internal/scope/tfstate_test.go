package scope

import (
	"testing"
	"time"
)

func TestTerraformStateSnapshotScopeIsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := NewTerraformStateSnapshotScope(
		"repo-scope-123",
		"s3",
		"s3://tfstate-prod/envs/prod.tfstate",
		map[string]string{"workspace": "prod"},
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	second, err := NewTerraformStateSnapshotScope(
		"repo-scope-123",
		"s3",
		"s3://tfstate-prod/envs/prod.tfstate",
		nil,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() second error = %v, want nil", err)
	}

	if first.ScopeID != second.ScopeID {
		t.Fatalf("ScopeID mismatch: %q != %q", first.ScopeID, second.ScopeID)
	}
	if first.PartitionKey != second.PartitionKey {
		t.Fatalf("PartitionKey mismatch: %q != %q", first.PartitionKey, second.PartitionKey)
	}
	if got, want := first.SourceSystem, string(CollectorTerraformState); got != want {
		t.Fatalf("SourceSystem = %q, want %q", got, want)
	}
	if first.ScopeKind != KindStateSnapshot {
		t.Fatalf("ScopeKind = %q, want %q", first.ScopeKind, KindStateSnapshot)
	}
	if first.CollectorKind != CollectorTerraformState {
		t.Fatalf("CollectorKind = %q, want %q", first.CollectorKind, CollectorTerraformState)
	}
	if first.ParentScopeID != "repo-scope-123" {
		t.Fatalf("ParentScopeID = %q, want repo-scope-123", first.ParentScopeID)
	}
	if got, want := first.Metadata["backend_kind"], "s3"; got != want {
		t.Fatalf("Metadata[backend_kind] = %q, want %q", got, want)
	}
	if got, want := first.Metadata["workspace"], "prod"; got != want {
		t.Fatalf("Metadata[workspace] = %q, want %q", got, want)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestTerraformStateSnapshotScopeRejectsBlankLocator(t *testing.T) {
	t.Parallel()

	if _, err := NewTerraformStateSnapshotScope("repo-scope-123", "s3", "", nil); err == nil {
		t.Fatal("NewTerraformStateSnapshotScope() error = nil, want non-nil")
	}
}

func TestTerraformStateSnapshotGenerationCarriesSerialAndLineage(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	scopeID := "state_snapshot:s3:locator-hash"
	generation, err := NewTerraformStateSnapshotGeneration(
		scopeID,
		17,
		"lineage-123",
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}

	if got, want := generation.GenerationID, "terraform_state:state_snapshot:s3:locator-hash:lineage-123:serial:17"; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := generation.ScopeID, scopeID; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := generation.FreshnessHint, "lineage=lineage-123 serial=17"; got != want {
		t.Fatalf("FreshnessHint = %q, want %q", got, want)
	}
	if generation.Status != GenerationStatusPending {
		t.Fatalf("Status = %q, want %q", generation.Status, GenerationStatusPending)
	}
	if generation.TriggerKind != TriggerKindSnapshot {
		t.Fatalf("TriggerKind = %q, want %q", generation.TriggerKind, TriggerKindSnapshot)
	}
	if !generation.ObservedAt.Equal(observedAt) {
		t.Fatalf("ObservedAt = %v, want %v", generation.ObservedAt, observedAt)
	}
	if err := generation.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestTerraformStateSnapshotGenerationIDIsScopeUnique(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	first, err := NewTerraformStateSnapshotGeneration(
		"state_snapshot:s3:first-locator-hash",
		17,
		"lineage-123",
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() first error = %v, want nil", err)
	}
	second, err := NewTerraformStateSnapshotGeneration(
		"state_snapshot:s3:second-locator-hash",
		17,
		"lineage-123",
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() second error = %v, want nil", err)
	}

	if first.GenerationID == second.GenerationID {
		t.Fatalf("GenerationID = %q for both scopes, want scope-unique identities", first.GenerationID)
	}
}

func TestTerraformStateSnapshotGenerationRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		scopeID     string
		serial      int64
		lineageUUID string
	}{
		{
			name:        "blank scope",
			scopeID:     " ",
			serial:      1,
			lineageUUID: "lineage-123",
		},
		{
			name:        "negative serial",
			scopeID:     "state_snapshot:s3:locator-hash",
			serial:      -1,
			lineageUUID: "lineage-123",
		},
		{
			name:        "blank lineage",
			scopeID:     "state_snapshot:s3:locator-hash",
			serial:      1,
			lineageUUID: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewTerraformStateSnapshotGeneration(
				test.scopeID,
				test.serial,
				test.lineageUUID,
				observedAt,
			)
			if err == nil {
				t.Fatal("NewTerraformStateSnapshotGeneration() error = nil, want non-nil")
			}
		})
	}
}

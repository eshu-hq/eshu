package scope

import (
	"testing"
	"time"
)

func TestIngestionScopeValidate(t *testing.T) {
	t.Parallel()

	got := IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     KindRepository,
		ParentScopeID: "parent-456",
		CollectorKind: CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repository": "eshu",
		},
	}

	if err := got.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestIngestionScopeHasPriorGenerationDoesNotInferFromActiveGenerationID(t *testing.T) {
	t.Parallel()

	scope := IngestionScope{ActiveGenerationID: "generation-active"}
	if scope.HasPriorGeneration() {
		t.Fatal("HasPriorGeneration() = true, want false without explicit PreviousGenerationExists")
	}

	scope.PreviousGenerationExists = true
	if !scope.HasPriorGeneration() {
		t.Fatal("HasPriorGeneration() = false, want true from explicit PreviousGenerationExists")
	}
}

func TestIngestionScopeValidateAllowsAdditionalCollectorKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceSystem  string
		collectorKind CollectorKind
	}{
		{
			name:          "aws",
			sourceSystem:  "aws",
			collectorKind: CollectorAWS,
		},
		{
			name:          "terraform_state",
			sourceSystem:  "terraform_state",
			collectorKind: CollectorTerraformState,
		},
		{
			name:          "webhook",
			sourceSystem:  "webhook",
			collectorKind: CollectorWebhook,
		},
		{
			name:          "documentation",
			sourceSystem:  "documentation",
			collectorKind: CollectorDocumentation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scope := IngestionScope{
				ScopeID:       "scope-123",
				SourceSystem:  tt.sourceSystem,
				ScopeKind:     KindRepository,
				CollectorKind: tt.collectorKind,
				PartitionKey:  "partition-123",
			}

			if err := scope.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestIngestionScopeValidateAllowsAdditionalScopeKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceSystem  string
		scopeID       string
		scopeKind     ScopeKind
		collectorKind CollectorKind
		partitionKey  string
	}{
		{
			name:          "account",
			sourceSystem:  "aws",
			scopeID:       "account-123456789012",
			scopeKind:     KindAccount,
			collectorKind: CollectorAWS,
			partitionKey:  "account-123456789012",
		},
		{
			name:          "region",
			sourceSystem:  "aws",
			scopeID:       "region-us-east-1",
			scopeKind:     KindRegion,
			collectorKind: CollectorAWS,
			partitionKey:  "account-123456789012",
		},
		{
			name:          "cluster",
			sourceSystem:  "aws",
			scopeID:       "cluster-prod-use1",
			scopeKind:     KindCluster,
			collectorKind: CollectorAWS,
			partitionKey:  "account-123456789012",
		},
		{
			name:          "state_snapshot",
			sourceSystem:  "terraform_state",
			scopeID:       "state-snapshot-prod",
			scopeKind:     KindStateSnapshot,
			collectorKind: CollectorTerraformState,
			partitionKey:  "terraform-state-prod",
		},
		{
			name:          "event_trigger",
			sourceSystem:  "webhook",
			scopeID:       "event-github-actions-123",
			scopeKind:     KindEventTrigger,
			collectorKind: CollectorWebhook,
			partitionKey:  "org-456",
		},
		{
			name:          "documentation_source",
			sourceSystem:  "documentation",
			scopeID:       "documentation-source-confluence-platform",
			scopeKind:     KindDocumentationSource,
			collectorKind: CollectorDocumentation,
			partitionKey:  "confluence-platform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scope := IngestionScope{
				ScopeID:       tt.scopeID,
				SourceSystem:  tt.sourceSystem,
				ScopeKind:     tt.scopeKind,
				CollectorKind: tt.collectorKind,
				PartitionKey:  tt.partitionKey,
			}

			if err := scope.Validate(); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

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

func TestDocumentationScopeAndCollectorValidate(t *testing.T) {
	t.Parallel()

	scope := IngestionScope{
		ScopeID:       "documentation-source-confluence-platform",
		SourceSystem:  "documentation",
		ScopeKind:     KindDocumentationSource,
		CollectorKind: CollectorDocumentation,
		PartitionKey:  "confluence-platform",
	}

	if err := scope.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestTerraformStateSnapshotScopeRejectsBlankLocator(t *testing.T) {
	t.Parallel()

	if _, err := NewTerraformStateSnapshotScope("repo-scope-123", "s3", "", nil); err == nil {
		t.Fatal("NewTerraformStateSnapshotScope() error = nil, want non-nil")
	}
}

func TestIngestionScopeValidateRejectsBlankIdentifiers(t *testing.T) {
	t.Parallel()

	got := IngestionScope{
		SourceSystem:  "git",
		ScopeKind:     KindRepository,
		CollectorKind: CollectorGit,
		PartitionKey:  "repo-123",
	}

	if err := got.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestIngestionScopeValidateRejectsBlankPartitionKey(t *testing.T) {
	t.Parallel()

	got := IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     KindRepository,
		CollectorKind: CollectorGit,
	}

	if err := got.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestScopeGenerationValidate(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC)
	ingestedAt := observedAt.Add(5 * time.Minute)

	got := ScopeGeneration{
		GenerationID:  "generation-123",
		ScopeID:       "scope-123",
		ObservedAt:    observedAt,
		IngestedAt:    ingestedAt,
		Status:        GenerationStatusPending,
		TriggerKind:   TriggerKindSnapshot,
		FreshnessHint: "fresh",
	}

	if err := got.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestScopeGenerationValidateRejectsUnknownStatus(t *testing.T) {
	t.Parallel()

	got := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       GenerationStatus("mystery"),
		TriggerKind:  TriggerKindSnapshot,
	}

	if err := got.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestScopeGenerationValidateRejectsBackwardsTimestamps(t *testing.T) {
	t.Parallel()

	got := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		Status:       GenerationStatusPending,
		TriggerKind:  TriggerKindSnapshot,
	}

	if err := got.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestScopeGenerationTransitionTo(t *testing.T) {
	t.Parallel()

	base := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       GenerationStatusPending,
		TriggerKind:  TriggerKindSnapshot,
	}

	activated, err := base.TransitionTo(GenerationStatusActive)
	if err != nil {
		t.Fatalf("TransitionTo(active) error = %v, want nil", err)
	}

	if activated.Status != GenerationStatusActive {
		t.Fatalf("Status = %q, want %q", activated.Status, GenerationStatusActive)
	}

	completed, err := activated.TransitionTo(GenerationStatusCompleted)
	if err != nil {
		t.Fatalf("TransitionTo(completed) error = %v, want nil", err)
	}

	if completed.Status != GenerationStatusCompleted {
		t.Fatalf("Status = %q, want %q", completed.Status, GenerationStatusCompleted)
	}
}

func TestScopeGenerationTransitionRejectsTerminalToActive(t *testing.T) {
	t.Parallel()

	base := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       GenerationStatusCompleted,
		TriggerKind:  TriggerKindSnapshot,
	}

	if _, err := base.TransitionTo(GenerationStatusActive); err == nil {
		t.Fatal("TransitionTo(active) error = nil, want non-nil")
	}
}

func TestScopeGenerationMarkSuperseded(t *testing.T) {
	t.Parallel()

	base := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       GenerationStatusActive,
		TriggerKind:  TriggerKindSnapshot,
	}

	superseded, err := base.MarkSuperseded()
	if err != nil {
		t.Fatalf("MarkSuperseded() error = %v, want nil", err)
	}

	if superseded.Status != GenerationStatusSuperseded {
		t.Fatalf("Status = %q, want %q", superseded.Status, GenerationStatusSuperseded)
	}
}

func TestScopeGenerationValidateForScope(t *testing.T) {
	t.Parallel()

	scope := IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     KindRepository,
		CollectorKind: CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       GenerationStatusPending,
		TriggerKind:  TriggerKindSnapshot,
	}

	if err := generation.ValidateForScope(scope); err != nil {
		t.Fatalf("ValidateForScope() error = %v, want nil", err)
	}
}

func TestScopeGenerationValidateForScopeRejectsMismatch(t *testing.T) {
	t.Parallel()

	scope := IngestionScope{
		ScopeID:       "scope-999",
		SourceSystem:  "git",
		ScopeKind:     KindRepository,
		CollectorKind: CollectorGit,
		PartitionKey:  "repo-123",
	}
	generation := ScopeGeneration{
		GenerationID: "generation-123",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 11, 30, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.April, 12, 11, 35, 0, 0, time.UTC),
		Status:       GenerationStatusPending,
		TriggerKind:  TriggerKindSnapshot,
	}

	if err := generation.ValidateForScope(scope); err == nil {
		t.Fatal("ValidateForScope() error = nil, want non-nil")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		{
			name:          "azure_subscription",
			sourceSystem:  "azure",
			scopeID:       "azure:tenant-abc:subscription:11111111:microsoft.compute:eastus:resource_graph",
			scopeKind:     KindAccount,
			collectorKind: CollectorAzure,
			partitionKey:  "tenant-abc:subscription:11111111",
		},
		{
			name:          "container_registry_repository",
			sourceSystem:  "oci_registry",
			scopeID:       "oci-registry-dockerhub-library-busybox",
			scopeKind:     KindContainerRegistryRepository,
			collectorKind: CollectorOCIRegistry,
			partitionKey:  "docker.io/library/busybox",
		},
		{
			name:          "package_registry",
			sourceSystem:  "package_registry",
			scopeID:       "package-registry-jfrog-generic-team-api",
			scopeKind:     KindPackageRegistry,
			collectorKind: CollectorPackageRegistry,
			partitionKey:  "jfrog:generic",
		},
		{
			name:          "vulnerability_intelligence",
			sourceSystem:  "vulnerability_intelligence",
			scopeID:       "vuln-intel://osv/npm",
			scopeKind:     KindVulnerabilityIntelligence,
			collectorKind: CollectorVulnerabilityIntelligence,
			partitionKey:  "osv:npm",
		},
		{
			name:          "security_alert",
			sourceSystem:  "security_alert",
			scopeID:       "security-alert:github:example-org/example-repo",
			scopeKind:     KindSecurityAlert,
			collectorKind: CollectorSecurityAlert,
			partitionKey:  "github_dependabot",
		},
		{
			name:          "pagerduty_account",
			sourceSystem:  "pagerduty",
			scopeID:       "pagerduty:account:example",
			scopeKind:     KindPagerDutyAccount,
			collectorKind: CollectorPagerDuty,
			partitionKey:  "example",
		},
		{
			name:          "jira_site",
			sourceSystem:  "jira",
			scopeID:       "jira:site:example",
			scopeKind:     KindJiraSite,
			collectorKind: CollectorJira,
			partitionKey:  "example.atlassian.net",
		},
		{
			name:          "scanner_worker",
			sourceSystem:  "scanner_worker",
			scopeID:       "scanner-worker://repository/repo-123",
			scopeKind:     KindScannerWorker,
			collectorKind: CollectorScannerWorker,
			partitionKey:  "repository",
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

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesMultiCloudRuntimeDriftIntentForGCPScope(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "gcp:project:demo",
		ScopeKind:    "gcp_cloud",
		SourceSystem: "gcp",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "gcp-generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		multiCloudGCPResourceEnvelope("fact-gcp-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainMultiCloudRuntimeDrift)
	if got, want := intent.EntityKey, "multi_cloud_runtime_drift:gcp:project:demo"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-gcp-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first gcp_cloud_resource fact", got)
	}
	if got, want := intent.SourceSystem, "gcp"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
	if got, want := intent.Reason, "gcp or azure cloud resource facts observed"; got != want {
		t.Fatalf("intent.Reason = %q, want %q", got, want)
	}
}

func TestBuildProjectionQueuesMultiCloudRuntimeDriftIntentForAzureScope(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "azure:sub-1:rg",
		ScopeKind:    "azure_cloud",
		SourceSystem: "azure",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "azure-generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		multiCloudAzureResourceEnvelope("fact-azure-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	intent := intentForDomain(t, projection.reducerIntents, reducer.DomainMultiCloudRuntimeDrift)
	if got, want := intent.EntityKey, "multi_cloud_runtime_drift:azure:sub-1:rg"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-azure-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first azure_cloud_resource fact", got)
	}
	if got, want := intent.SourceSystem, "azure"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

// TestBuildProjectionDoesNotQueueMultiCloudRuntimeDriftForAWSOnlyScope proves
// the provider-partitioning decision (#5759) at the trigger layer: an AWS-only
// scope generation must not enqueue DomainMultiCloudRuntimeDrift at all.
// DomainAWSCloudRuntimeDrift already owns AWS runtime-drift findings
// end-to-end; enqueuing the multi-cloud domain for AWS-only evidence would be
// pure overhead (the handler would load evidence, evaluate, and then filter
// every candidate away) with no GCP/Azure coverage to show for it.
func TestBuildProjectionDoesNotQueueMultiCloudRuntimeDriftForAWSOnlyScope(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "aws:123456789012:us-east-1:lambda",
		ScopeKind:    "aws_cloud",
		SourceSystem: "aws",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "aws-generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		awsResourceEnvelope("fact-aws-1", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainMultiCloudRuntimeDrift {
			t.Fatalf("unexpected multi_cloud_runtime_drift intent for an AWS-only scope generation")
		}
	}
}

func TestBuildProjectionDoesNotQueueMultiCloudRuntimeDriftWithoutCloudResourceFacts(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "gcp:project:demo",
		ScopeKind:    "gcp_cloud",
		SourceSystem: "gcp",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "gcp-generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}

	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	if got := len(projection.reducerIntents); got != 0 {
		t.Fatalf("len(reducerIntents) = %d, want 0", got)
	}
}

func multiCloudGCPResourceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.GCPCloudResourceFactKind,
		SchemaVersion: facts.GCPCloudResourceSchemaVersion,
		CollectorKind: "gcp_cloud",
		SourceRef:     facts.Ref{SourceSystem: "gcp"},
		Payload: map[string]any{
			"project_id":         "demo",
			"asset_type":         "compute.googleapis.com/Instance",
			"full_resource_name": "//compute.googleapis.com/projects/demo/zones/us-central1-a/instances/demo-instance",
		},
	}
}

func multiCloudAzureResourceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      facts.AzureCloudResourceFactKind,
		SchemaVersion: facts.AzureCloudResourceSchemaVersion,
		CollectorKind: "azure",
		SourceRef:     facts.Ref{SourceSystem: "azure"},
		Payload: map[string]any{
			"arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
			"resource_type":   "microsoft.compute/virtualmachines",
		},
	}
}

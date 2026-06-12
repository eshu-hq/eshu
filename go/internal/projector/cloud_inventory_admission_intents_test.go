package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func cloudInventoryAdmissionScopeAndGeneration(provider string) (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{
		ScopeID:      provider + ":acct:demo",
		ScopeKind:    scope.ScopeKind(provider + "_cloud"),
		SourceSystem: provider,
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: provider + "-generation-1",
		ObservedAt:   time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 6, 9, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	return scopeValue, generation
}

func cloudInventorySourceEnvelope(factID, scopeID, generationID, factKind, sourceSystem string) facts.Envelope {
	return facts.Envelope{
		FactID:        factID,
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      factKind,
		CollectorKind: sourceSystem,
		SourceRef:     facts.Ref{SourceSystem: sourceSystem},
		ObservedAt:    time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC),
	}
}

// TestBuildProjectionQueuesCloudInventoryAdmissionForEachProvider proves the
// projector enqueues a single scope-keyed cloud_inventory_admission intent when
// any provider cloud-inventory source fact is present, so the canonical
// GET /api/v0/cloud/inventory readback is populated (#2209).
func TestBuildProjectionQueuesCloudInventoryAdmissionForEachProvider(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		provider string
		factKind string
	}{
		{"gcp", facts.GCPCloudResourceFactKind},
		{"azure", facts.AzureCloudResourceFactKind},
	} {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()

			scopeValue, generation := cloudInventoryAdmissionScopeAndGeneration(tc.provider)
			envelopes := []facts.Envelope{
				cloudInventorySourceEnvelope("fact-1", scopeValue.ScopeID, generation.GenerationID, tc.factKind, tc.provider),
				cloudInventorySourceEnvelope("fact-2", scopeValue.ScopeID, generation.GenerationID, tc.factKind, tc.provider),
			}

			projection, err := buildProjection(scopeValue, generation, envelopes)
			if err != nil {
				t.Fatalf("buildProjection() error = %v, want nil", err)
			}
			intent := intentForDomain(t, projection.reducerIntents, reducer.DomainCloudInventoryAdmission)
			if got, want := intent.EntityKey, "cloud_inventory_admission:"+scopeValue.ScopeID; got != want {
				t.Fatalf("intent.EntityKey = %q, want %q", got, want)
			}
			if got, want := intent.FactID, "fact-1"; got != want {
				t.Fatalf("intent.FactID = %q, want first source fact", got)
			}
			if got, want := intent.GenerationID, generation.GenerationID; got != want {
				t.Fatalf("intent.GenerationID = %q, want %q", got, want)
			}
			if got, want := intent.SourceSystem, tc.provider; got != want {
				t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
			}
		})
	}
}

// TestBuildProjectionDoesNotQueueCloudInventoryAdmissionWithoutSourceFacts proves
// no admission intent is enqueued when the generation carries no provider
// cloud-inventory source fact.
func TestBuildProjectionDoesNotQueueCloudInventoryAdmissionWithoutSourceFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generation := cloudInventoryAdmissionScopeAndGeneration("gcp")
	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	for _, intent := range projection.reducerIntents {
		if intent.Domain == reducer.DomainCloudInventoryAdmission {
			t.Fatal("unexpected cloud_inventory_admission intent without cloud-inventory source facts")
		}
	}
}

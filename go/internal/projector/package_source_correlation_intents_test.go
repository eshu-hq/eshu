package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSinglePackageSourceCorrelationIntent(t *testing.T) {
	t.Parallel()

	scopeValue := scope.IngestionScope{
		ScopeID:      "package-registry:npm:team-api",
		ScopeKind:    "package_registry",
		SourceSystem: "package_registry",
	}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "generation-1",
		ObservedAt:   time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, 5, 14, 10, 0, 1, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
	}
	envelopes := []facts.Envelope{
		packageSourceHintEnvelope("fact-source-1", scopeValue.ScopeID, generation.GenerationID),
		packageSourceHintEnvelope("fact-source-2", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	if got, want := len(projection.reducerIntents), 1; got != want {
		t.Fatalf("len(reducerIntents) = %d, want %d", got, want)
	}
	intent := projection.reducerIntents[0]
	if got, want := intent.Domain, reducer.DomainPackageSourceCorrelation; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "package_source_correlation:package-registry:npm:team-api"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-source-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first source hint fact", got)
	}
}

func packageSourceHintEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.PackageRegistrySourceHintFactKind,
		SchemaVersion:    facts.PackageRegistrySourceHintSchemaVersion,
		CollectorKind:    "package_registry",
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "package_registry",
		},
		Payload: map[string]any{
			"package_id":     "pkg:npm://registry.example/team-api",
			"hint_kind":      "repository",
			"normalized_url": "https://github.com/acme/team-api",
		},
	}
}

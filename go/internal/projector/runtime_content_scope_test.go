package projector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestRuntimeProjectSkipsContentMaterializationForNonRepositoryScopes(t *testing.T) {
	t.Parallel()

	contentWriter := &recordingContentWriter{result: content.Result{EntityCount: 1}}
	intentWriter := &recordingIntentWriter{result: IntentResult{Count: 1}}
	runtime := Runtime{
		ContentWriter: contentWriter,
		IntentWriter:  intentWriter,
	}

	scopeValue := scope.IngestionScope{
		ScopeID:       "aws:123456789012:aws-global:iam",
		SourceSystem:  "aws",
		ScopeKind:     scope.KindAccount,
		CollectorKind: scope.CollectorAWS,
		PartitionKey:  "aws:123456789012",
	}
	generationValue := scope.ScopeGeneration{
		GenerationID: "generation-aws",
		ScopeID:      "aws:123456789012:aws-global:iam",
		ObservedAt:   time.Date(2026, time.June, 2, 22, 40, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.June, 2, 22, 41, 0, 0, time.UTC),
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}

	result, err := runtime.Project(context.Background(), scopeValue, generationValue, []facts.Envelope{
		{
			FactID:        "aws-resource-1",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generationValue.GenerationID,
			FactKind:      facts.AWSResourceFactKind,
			SchemaVersion: facts.AWSResourceSchemaVersion,
			ObservedAt:    generationValue.ObservedAt,
			Payload: map[string]any{
				"account_id":    "123456789012",
				"region":        "aws-global",
				"resource_id":   "arn:aws:iam::123456789012:role/eshu-runtime",
				"resource_type": "iam_role",
				"name":          "eshu-runtime",
				"path":          "/service/",
			},
		},
	})
	if err != nil {
		t.Fatalf("Project() error = %v, want nil", err)
	}
	if got := len(contentWriter.calls); got != 0 {
		t.Fatalf("content writer call count = %d, want 0 for non-repository scope", got)
	}
	if got, want := result.Content.RecordCount, 0; got != want {
		t.Fatalf("result.Content.RecordCount = %d, want %d", got, want)
	}
	if got, want := result.Content.EntityCount, 0; got != want {
		t.Fatalf("result.Content.EntityCount = %d, want %d", got, want)
	}
	if got := len(intentWriter.calls); got != 1 {
		t.Fatalf("intent writer call count = %d, want 1", got)
	}
	intentForDomain(t, intentWriter.calls[0], reducer.DomainAWSResourceMaterialization)
}

package projector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestBuildProjectionQueuesSingleAWSCloudRuntimeDriftIntent(t *testing.T) {
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
		awsResourceEnvelope("fact-aws-2", scopeValue.ScopeID, generation.GenerationID),
	}

	projection, err := buildProjection(scopeValue, generation, envelopes)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	if got, want := len(projection.reducerIntents), 1; got != want {
		t.Fatalf("len(reducerIntents) = %d, want %d", got, want)
	}
	intent := projection.reducerIntents[0]
	if got, want := intent.Domain, reducer.DomainAWSCloudRuntimeDrift; got != want {
		t.Fatalf("intent.Domain = %q, want %q", got, want)
	}
	if got, want := intent.EntityKey, "aws_cloud_runtime_drift:aws:123456789012:us-east-1:lambda"; got != want {
		t.Fatalf("intent.EntityKey = %q, want %q", got, want)
	}
	if got, want := intent.FactID, "fact-aws-1"; got != want {
		t.Fatalf("intent.FactID = %q, want first aws_resource fact", got)
	}
	if got, want := intent.SourceSystem, "aws"; got != want {
		t.Fatalf("intent.SourceSystem = %q, want %q", got, want)
	}
}

func TestBuildProjectionDoesNotQueueAWSCloudRuntimeDriftWithoutAWSResource(t *testing.T) {
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

	projection, err := buildProjection(scopeValue, generation, nil)
	if err != nil {
		t.Fatalf("buildProjection() error = %v, want nil", err)
	}
	if got := len(projection.reducerIntents); got != 0 {
		t.Fatalf("len(reducerIntents) = %d, want 0", got)
	}
}

func awsResourceEnvelope(factID, scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.AWSResourceFactKind,
		SchemaVersion:    facts.AWSResourceSchemaVersion,
		CollectorKind:    "aws_cloud",
		SourceConfidence: "reported",
		ObservedAt:       time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SourceRef: facts.Ref{
			SourceSystem: "aws",
		},
		Payload: map[string]any{
			"arn":           "arn:aws:lambda:us-east-1:123456789012:function:team-api",
			"resource_id":   "team-api",
			"resource_type": "aws_lambda_function",
			"tags": map[string]any{
				"Environment": "prod",
			},
		},
	}
}

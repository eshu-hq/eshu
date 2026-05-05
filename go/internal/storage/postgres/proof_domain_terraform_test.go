package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestProofDomainTerraformSchemaEvidenceFlowsCollectorToStorage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 13, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	targetScope := scope.IngestionScope{
		ScopeID:       "scope-target",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-target",
		Metadata: map[string]string{
			"repo_id": "repo-target",
		},
	}
	targetGeneration := scope.ScopeGeneration{
		GenerationID: "generation-target",
		ScopeID:      targetScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 12, 12, 20, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	targetEnvelopes := []facts.Envelope{
		{
			FactID:        "target-repo",
			ScopeID:       targetScope.ScopeID,
			GenerationID:  targetGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-target",
			ObservedAt:    targetGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-target",
				"graph_kind": "repository",
				"name":       "payments-service",
				"repo_id":    "repo-target",
				"repo_slug":  "payments-service",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "repo-target",
			},
		},
	}

	infraScope := scope.IngestionScope{
		ScopeID:       "scope-infra",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-infra",
		Metadata: map[string]string{
			"repo_id": "repo-infra",
		},
	}
	infraGeneration := scope.ScopeGeneration{
		GenerationID: "generation-infra",
		ScopeID:      infraScope.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 12, 12, 40, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	infraEnvelopes := []facts.Envelope{
		{
			FactID:        "infra-repo",
			ScopeID:       infraScope.ScopeID,
			GenerationID:  infraGeneration.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-infra",
			ObservedAt:    infraGeneration.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-infra",
				"graph_kind": "repository",
				"name":       "infra-repo",
				"repo_id":    "repo-infra",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "repo-infra",
			},
		},
		{
			FactID:        "infra-tf",
			ScopeID:       infraScope.ScopeID,
			GenerationID:  infraGeneration.GenerationID,
			FactKind:      "content",
			StableFactKey: "content:main.tf",
			ObservedAt:    infraGeneration.ObservedAt,
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content": `resource "aws_s3_bucket" "logs" {
  bucket = "payments-service"
}`,
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "main.tf",
			},
		},
	}

	ingestionStore := NewIngestionStore(db)
	ingestionStore.Now = func() time.Time { return now }
	collectorCtx, cancelCollector := context.WithCancel(context.Background())
	defer cancelCollector()
	collectorService := collector.Service{
		Source: &proofCollectorSource{
			collected: []collector.CollectedGeneration{
				collector.FactsFromSlice(targetScope, targetGeneration, targetEnvelopes),
				collector.FactsFromSlice(infraScope, infraGeneration, infraEnvelopes),
			},
		},
		Committer: collectorCommitterFunc(func(
			ctx context.Context,
			scopeValue scope.IngestionScope,
			generationValue scope.ScopeGeneration,
			factStream <-chan facts.Envelope,
		) error {
			defer cancelCollector()
			return ingestionStore.CommitScopeGeneration(ctx, scopeValue, generationValue, factStream)
		}),
		PollInterval: time.Millisecond,
	}
	if err := collectorService.Run(collectorCtx); err != nil {
		t.Fatalf("collector service Run() error = %v, want nil", err)
	}

	if got, want := len(db.state.evidenceFacts), 1; got != want {
		t.Fatalf("evidence facts = %d, want %d", got, want)
	}
	var record evidenceRecord
	for _, value := range db.state.evidenceFacts {
		record = value
	}
	if got, want := record.generationID, infraGeneration.GenerationID; got != want {
		t.Fatalf("evidence generation = %q, want %q", got, want)
	}
	if got, want := record.evidenceKind, "TERRAFORM_S3_BUCKET"; got != want {
		t.Fatalf("evidence kind = %q, want %q", got, want)
	}
	if got, want := record.targetRepoID, "repo-target"; got != want {
		t.Fatalf("evidence target = %q, want %q", got, want)
	}
	if got, want := record.details["schema_driven"], true; got != want {
		t.Fatalf("evidence schema_driven = %#v, want %#v", got, want)
	}
}

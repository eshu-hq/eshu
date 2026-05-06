package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

func TestProofDomainCloudAssetResolutionFlowsCollectorToReducerIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 45, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-cloud-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-aws-123",
		Metadata: map[string]string{
			"repo_id":    "repo-aws-123",
			"source_key": "repo-aws-123",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-cloud-456",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   time.Date(2026, time.April, 12, 12, 15, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	envelopes := []facts.Envelope{
		{
			FactID:        "cloud-fact-1",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "repository",
			StableFactKey: "repository:repo-aws-123",
			ObservedAt:    generation.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-aws-123",
				"graph_kind": "repository",
				"name":       "aws-repo",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "repo-aws-123",
			},
		},
		{
			FactID:        "cloud-fact-2",
			ScopeID:       scopeValue.ScopeID,
			GenerationID:  generation.GenerationID,
			FactKind:      "shared_follow_up",
			StableFactKey: "shared:cloud_asset_resolution",
			ObservedAt:    generation.ObservedAt,
			Payload: map[string]any{
				"reducer_domain": "cloud_asset_resolution",
				"entity_key":     "aws:s3:bucket:logs-prod",
				"reason":         "repo snapshot emitted shared cloud asset work",
				"fact_id":        "cloud-fact-2",
				"source_system":  "git",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "cloud_asset_resolution",
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
				collector.FactsFromSlice(scopeValue, generation, envelopes),
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

	projectorQueue := ProjectorQueue{
		db:            db,
		LeaseOwner:    "projector-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	factStore := NewFactStore(db)
	canonicalWriter := &recordingCanonicalWriter{}
	contentWriter := &recordingContentWriter{}
	reducerQueue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-1",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	projectorRuntime := projector.Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   contentWriter,
		IntentWriter:    reducerQueue,
	}
	projectorService := projector.Service{
		PollInterval: time.Millisecond,
		WorkSource:   projectorQueue,
		FactStore:    factStore,
		Runner:       projectorRuntime,
		WorkSink:     projectorQueue,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := projectorService.Run(context.Background()); err != nil {
		t.Fatalf("projector service Run() error = %v, want nil", err)
	}

	reducerRegistry := reducer.NewRegistry()
	if err := reducerRegistry.Register(reducer.DomainDefinition{
		Domain:  reducer.DomainCloudAssetResolution,
		Summary: "resolve canonical cloud asset identity across sources",
		Ownership: reducer.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "cloud_asset",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerAppliedDeclaration,
				truth.LayerObservedResource,
			},
		},
		Handler: reducer.HandlerFunc(func(context.Context, reducer.Intent) (reducer.Result, error) {
			return reducer.Result{
				Status:          reducer.ResultStatusSucceeded,
				EvidenceSummary: "cloud asset canonicalized",
				CanonicalWrites: 1,
				CompletedAt:     now,
			}, nil
		}),
	}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
	reducerRuntime, err := reducer.NewRuntime(reducerRegistry)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v, want nil", err)
	}
	reducerService := reducer.Service{
		PollInterval: time.Millisecond,
		WorkSource:   reducerQueue,
		Executor:     reducerRuntime,
		WorkSink:     reducerQueue,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := reducerService.Run(context.Background()); err != nil {
		t.Fatalf("reducer service Run() error = %v, want nil", err)
	}

	if got, want := db.reducerClaims, 1; got != want {
		t.Fatalf("reducer claims = %d, want %d", got, want)
	}
	if got, want := db.reducerAcked, 1; got != want {
		t.Fatalf("reducer acknowledgements = %d, want %d", got, want)
	}
}

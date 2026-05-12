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
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// testFactChannel converts a slice of envelopes to a closed channel for testing.
func testFactChannel(envelopes []facts.Envelope) <-chan facts.Envelope {
	ch := make(chan facts.Envelope, len(envelopes))
	for _, e := range envelopes {
		ch <- e
	}
	close(ch)
	return ch
}

func TestProofDomainWorkloadIdentityFlowsCollectorToReducerIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 12, 30, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id":    "repo-123",
			"source_key": "repo-123",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-456",
		ScopeID:      "scope-123",
		ObservedAt:   time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		IngestedAt:   now,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	envelopes := []facts.Envelope{
		{
			FactID:        "fact-1",
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "repository",
			StableFactKey: "repository:repo-123",
			ObservedAt:    generation.ObservedAt,
			Payload: map[string]any{
				"graph_id":   "repo-123",
				"graph_kind": "repository",
				"name":       "eshu",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "repo-123",
			},
		},
		{
			FactID:        "fact-2",
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "content",
			StableFactKey: "content:README.md",
			ObservedAt:    generation.ObservedAt,
			Payload: map[string]any{
				"content_path":   "README.md",
				"content_body":   "# Eshu\n",
				"content_digest": "digest-1",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "README.md",
			},
		},
		{
			FactID:        "fact-3",
			ScopeID:       "scope-123",
			GenerationID:  "generation-456",
			FactKind:      "shared_follow_up",
			StableFactKey: "shared:workload_identity",
			ObservedAt:    generation.ObservedAt,
			Payload: map[string]any{
				"reducer_domain": "workload_identity",
				"entity_key":     "workload:eshu",
				"reason":         "repo snapshot emitted shared workload identity work",
				"fact_id":        "fact-3",
				"source_system":  "git",
			},
			SourceRef: facts.Ref{
				SourceSystem: "git",
				FactKey:      "workload_identity",
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
			return ingestionStore.CommitScopeGeneration(
				ctx,
				scopeValue,
				generationValue,
				factStream,
			)
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
		Domain:  reducer.DomainWorkloadIdentity,
		Summary: "resolve canonical workload identity across sources",
		Ownership: reducer.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: testReducerTruthContract("workload_identity"),
		Handler: reducer.HandlerFunc(func(context.Context, reducer.Intent) (reducer.Result, error) {
			return reducer.Result{
				Status:          reducer.ResultStatusSucceeded,
				EvidenceSummary: "workload identity canonicalized",
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

	if got, want := len(contentWriter.calls), 1; got != want {
		t.Fatalf("content writes = %d, want %d", got, want)
	}
	if got, want := len(contentWriter.calls[0].Records), 1; got != want {
		t.Fatalf("content record count = %d, want %d", got, want)
	}
	if got, want := db.reducerClaims, 1; got != want {
		t.Fatalf("reducer claims = %d, want %d", got, want)
	}
	if got, want := db.reducerAcked, 1; got != want {
		t.Fatalf("reducer acknowledgements = %d, want %d", got, want)
	}

	statusStore := NewStatusStore(db)
	rawStatus, err := statusStore.ReadStatusSnapshot(context.Background(), now)
	if err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v, want nil", err)
	}
	report := statuspkg.BuildReport(rawStatus, statuspkg.DefaultOptions())
	if got, want := report.Health.State, "healthy"; got != want {
		t.Fatalf("status health = %q, want %q", got, want)
	}
	if got, want := report.Queue.Succeeded, 2; got != want {
		t.Fatalf("status queue succeeded = %d, want %d", got, want)
	}
	if got, want := report.Queue.Outstanding, 0; got != want {
		t.Fatalf("status queue outstanding = %d, want %d", got, want)
	}
	if got, want := report.ScopeTotals["active"], 1; got != want {
		t.Fatalf("status active scope count = %d, want %d", got, want)
	}
	if got, want := report.GenerationTotals["active"], 1; got != want {
		t.Fatalf("status active generation count = %d, want %d", got, want)
	}
	if got, want := len(report.FlowSummaries), 3; got != want {
		t.Fatalf("status flow summary count = %d, want %d", got, want)
	}
}

func testReducerTruthContract(canonicalKind string) truth.Contract {
	return truth.Contract{
		CanonicalKind: canonicalKind,
		SourceLayers: []truth.Layer{
			truth.LayerSourceDeclaration,
		},
	}
}

func TestProofDomainIncrementalRefreshLeavesActiveGenerationUnchangedForIdenticalRerun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 13, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id": "repo-123",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID:  "generation-aaa",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-time.Minute),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-aaa",
	}

	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generation,
		testFactChannel(proofRepositoryFacts(scopeValue.ScopeID, generation.GenerationID, "fact-1", "digest-aaa", generation.ObservedAt)),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generation.GenerationID; got != want {
		t.Fatalf("active generation after first run = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generation.GenerationID), scope.GenerationStatusActive; got != want {
		t.Fatalf("generation status after first run = %q, want %q", got, want)
	}
	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generation,
		testFactChannel(proofRepositoryFacts(scopeValue.ScopeID, generation.GenerationID, "fact-1", "digest-aaa", generation.ObservedAt)),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() rerun error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generation.GenerationID; got != want {
		t.Fatalf("active generation after identical rerun = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generation.GenerationID), scope.GenerationStatusActive; got != want {
		t.Fatalf("generation status after identical rerun = %q, want %q", got, want)
	}
	if got, want := db.projectorWorkItemCount(), 1; got != want {
		t.Fatalf("total projector work items after identical rerun = %d, want %d", got, want)
	}
}

func TestProofDomainIncrementalRefreshSupersedesActiveGenerationOnChangedRerun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 14, 0, 0, 0, time.UTC)
	db := newProofDomainDB(t, now)
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	scopeValue := scope.IngestionScope{
		ScopeID:       "scope-123",
		SourceSystem:  "git",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "repo-123",
		Metadata: map[string]string{
			"repo_id": "repo-123",
		},
	}
	generationA := scope.ScopeGeneration{
		GenerationID:  "generation-aaa",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-2 * time.Minute),
		IngestedAt:    now,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-aaa",
	}
	generationB := scope.ScopeGeneration{
		GenerationID:  "generation-bbb",
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    now.Add(-time.Minute),
		IngestedAt:    now.Add(time.Minute),
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: "fingerprint-bbb",
	}

	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generationA,
		testFactChannel(proofRepositoryFacts(scopeValue.ScopeID, generationA.GenerationID, "fact-1", "digest-aaa", generationA.ObservedAt)),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() generation A error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generationA.GenerationID; got != want {
		t.Fatalf("active generation after generation A = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generationA.GenerationID), scope.GenerationStatusActive; got != want {
		t.Fatalf("generation A status = %q, want %q", got, want)
	}

	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generationB,
		testFactChannel(proofRepositoryFacts(scopeValue.ScopeID, generationB.GenerationID, "fact-2", "digest-bbb", generationB.ObservedAt)),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() generation B error = %v, want nil", err)
	}
	runProofProjectorCycle(t, db, now)

	if got, want := db.activeGenerationID(scopeValue.ScopeID), generationB.GenerationID; got != want {
		t.Fatalf("active generation after generation B = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generationA.GenerationID), scope.GenerationStatusSuperseded; got != want {
		t.Fatalf("generation A status after rerun = %q, want %q", got, want)
	}
	if got, want := db.generationStatus(generationB.GenerationID), scope.GenerationStatusActive; got != want {
		t.Fatalf("generation B status after rerun = %q, want %q", got, want)
	}
	if got, want := db.projectorWorkItemCount(), 2; got != want {
		t.Fatalf("total projector work items after changed rerun = %d, want %d", got, want)
	}
}

type proofCollectorSource struct {
	collected []collector.CollectedGeneration
	index     int
}

func (s *proofCollectorSource) Next(
	ctx context.Context,
) (collector.CollectedGeneration, bool, error) {
	if s.index >= len(s.collected) {
		<-ctx.Done()
		return collector.CollectedGeneration{}, false, ctx.Err()
	}

	item := s.collected[s.index]
	s.index++
	return item, true, nil
}

type collectorCommitterFunc func(
	context.Context,
	scope.IngestionScope,
	scope.ScopeGeneration,
	<-chan facts.Envelope,
) error

func (f collectorCommitterFunc) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	return f(ctx, scopeValue, generationValue, factStream)
}

func collectorCommitterCancelAfter(
	store IngestionStore,
	cancel context.CancelFunc,
	commitCount int,
) collectorCommitterFunc {
	remaining := commitCount
	return collectorCommitterFunc(func(
		ctx context.Context,
		scopeValue scope.IngestionScope,
		generationValue scope.ScopeGeneration,
		factStream <-chan facts.Envelope,
	) error {
		if err := store.CommitScopeGeneration(ctx, scopeValue, generationValue, factStream); err != nil {
			return err
		}
		remaining--
		if remaining <= 0 {
			cancel()
		}
		return nil
	})
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// stubGraphExecutor is a no-op executor for tests that do not exercise graph writes.
type stubGraphExecutor struct{}

func (stubGraphExecutor) Execute(_ context.Context, _ sourcecypher.Statement) error { return nil }

// stubCypherExecutor is a no-op CypherExecutor for tests that do not exercise graph writes.
type stubCypherExecutor struct{}

func (stubCypherExecutor) ExecuteCypher(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

// stubCypherReader always reports no canonical nodes exist (safe no-op for tests).
type stubCypherReader struct{}

func (stubCypherReader) QueryCypherExists(_ context.Context, _ string, _ map[string]any) (bool, error) {
	return false, nil
}

func (stubCypherReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (stubCypherReader) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestBuildReducerServiceWiresDefaultRuntimeAndQueue(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.PollInterval <= 0 {
		t.Fatalf("buildReducerService() poll interval = %v, want positive", service.PollInterval)
	}
	if service.WorkSource == nil {
		t.Fatal("buildReducerService() work source = nil, want non-nil")
	}
	if service.Executor == nil {
		t.Fatal("buildReducerService() executor = nil, want non-nil")
	}
	if service.WorkSink == nil {
		t.Fatal("buildReducerService() work sink = nil, want non-nil")
	}
	if service.SharedProjectionRunner == nil {
		t.Fatal("buildReducerService() shared projection runner = nil, want non-nil")
	}
	if service.SharedProjectionRunner.ReadinessLookup == nil {
		t.Fatal("buildReducerService() shared projection readiness lookup = nil, want non-nil")
	}
	if service.SharedProjectionRunner.ReadinessPrefetch == nil {
		t.Fatal("buildReducerService() shared projection readiness prefetch = nil, want non-nil")
	}
	if service.CodeCallProjectionRunner == nil {
		t.Fatal("buildReducerService() code call projection runner = nil, want non-nil")
	}
	if service.CodeCallProjectionRunner.ReadinessLookup == nil {
		t.Fatal("buildReducerService() code call readiness lookup = nil, want non-nil")
	}
	if service.CodeCallProjectionRunner.ReadinessPrefetch == nil {
		t.Fatal("buildReducerService() code call readiness prefetch = nil, want non-nil")
	}
	if got := service.CodeCallProjectionRunner.Config.PollInterval; got <= 0 {
		t.Fatalf("buildReducerService() code call poll interval = %v, want positive", got)
	}
	if got := service.CodeCallProjectionRunner.Config.LeaseOwner; got != defaultCodeCallProjectionLeaseOwner {
		t.Fatalf("buildReducerService() code call lease owner = %q, want %q", got, defaultCodeCallProjectionLeaseOwner)
	}
	if got := service.CodeCallProjectionRunner.Config.LeaseTTL; got <= 0 {
		t.Fatalf("buildReducerService() code call lease TTL = %v, want positive", got)
	}
	if got := service.CodeCallProjectionRunner.Config.BatchLimit; got <= 0 {
		t.Fatalf("buildReducerService() code call batch limit = %d, want positive", got)
	}
	if service.RepoDependencyProjectionRunner == nil {
		t.Fatal("buildReducerService() repo dependency projection runner = nil, want non-nil")
	}
	if got := service.RepoDependencyProjectionRunner.Config.PollInterval; got <= 0 {
		t.Fatalf("buildReducerService() repo dependency poll interval = %v, want positive", got)
	}
	if got := service.RepoDependencyProjectionRunner.Config.LeaseOwner; !strings.HasPrefix(got, defaultRepoDependencyProjectionLeaseOwner+":") {
		t.Fatalf("buildReducerService() repo dependency lease owner = %q, want per-process %q prefix", got, defaultRepoDependencyProjectionLeaseOwner)
	}
	if got := service.RepoDependencyProjectionRunner.Config.LeaseTTL; got <= 0 {
		t.Fatalf("buildReducerService() repo dependency lease TTL = %v, want positive", got)
	}
	if got := service.RepoDependencyProjectionRunner.Config.BatchLimit; got <= 0 {
		t.Fatalf("buildReducerService() repo dependency batch limit = %d, want positive", got)
	}
	if got := service.RepoDependencyProjectionRunner.Config.Workers; got != 4 {
		t.Fatalf("buildReducerService() repo dependency workers = %d, want proven default 4", got)
	}
	if service.CodeReachabilityProjectionRunner == nil {
		t.Fatal("buildReducerService() code reachability projection runner = nil, want non-nil")
	}
	if service.SearchVectorBuildRunner != nil {
		t.Fatal("buildReducerService() search vector build runner = non-nil, want disabled by default")
	}
	if service.CodeReachabilityProjectionRunner.InputLoader == nil {
		t.Fatal("buildReducerService() code reachability input loader = nil, want non-nil")
	}
	if service.CodeReachabilityProjectionRunner.RowWriter == nil {
		t.Fatal("buildReducerService() code reachability row writer = nil, want non-nil")
	}
	if got := service.CodeReachabilityProjectionRunner.Config.PollInterval; got <= 0 {
		t.Fatalf("buildReducerService() code reachability poll interval = %v, want positive", got)
	}
	if got := service.CodeReachabilityProjectionRunner.Config.BatchLimit; got <= 0 {
		t.Fatalf("buildReducerService() code reachability batch limit = %d, want positive", got)
	}
	codeCallEdgeWriter, ok := service.CodeCallProjectionRunner.EdgeWriter.(*sourcecypher.EdgeWriter)
	if !ok {
		t.Fatalf("code call edge writer type = %T, want *cypher.EdgeWriter", service.CodeCallProjectionRunner.EdgeWriter)
	}
	if got, want := codeCallEdgeWriter.CodeCallBatchSize, defaultCodeCallEdgeBatchSize; got != want {
		t.Fatalf("code call edge batch size = %d, want %d", got, want)
	}
	if got, want := codeCallEdgeWriter.CodeCallGroupBatchSize, defaultCodeCallEdgeGroupBatchSize; got != want {
		t.Fatalf("code call edge group batch size = %d, want %d", got, want)
	}
	assertSharedEdgeWriterConfig(t, codeCallEdgeWriter, defaultInheritanceEdgeGroupBatchSize, defaultSQLRelationshipEdgeGroupBatchSize, true)
	if codeCallEdgeWriter.RepoDependencyRetractStatementTiming {
		t.Fatal("repo dependency retract statement timing = true, want default false")
	}
	if service.GraphProjectionPhaseRepairer == nil {
		t.Fatal("buildReducerService() graph projection repairer = nil, want non-nil")
	}
	if service.GenerationRetentionRunner == nil {
		t.Fatal("buildReducerService() generation retention runner = nil, want non-nil")
	}
	if service.GraphOrphanSweepRunner == nil {
		t.Fatal("buildReducerService() graph orphan sweep runner = nil, want non-nil")
	}
	if service.GraphOrphanSweepRunner.LeaseManager == nil {
		t.Fatal("buildReducerService() graph orphan sweep lease manager = nil, want non-nil")
	}
	if got := service.GraphOrphanSweepRunner.Config.LeaseOwner; !strings.HasPrefix(got, "graph-orphan-sweep-runner:") {
		t.Fatalf("buildReducerService() graph orphan sweep lease owner = %q, want per-process owner", got)
	}
	if got := service.GraphOrphanSweepRunner.Config.LeaseTTL; got <= 0 {
		t.Fatalf("buildReducerService() graph orphan sweep lease TTL = %v, want positive", got)
	}
	if service.GraphProjectionPhaseRepairer.Queue == nil {
		t.Fatal("buildReducerService() graph projection repair queue = nil, want non-nil")
	}
	if service.GraphProjectionPhaseRepairer.StateLookup == nil {
		t.Fatal("buildReducerService() graph projection repair state lookup = nil, want non-nil")
	}
	if service.GraphProjectionPhaseRepairer.Publisher == nil {
		t.Fatal("buildReducerService() graph projection repair publisher = nil, want non-nil")
	}
	if got := service.GraphProjectionPhaseRepairer.Config.BatchLimit; got <= 0 {
		t.Fatalf("buildReducerService() graph projection repair batch limit = %d, want positive", got)
	}
	if got := service.GraphProjectionPhaseRepairer.Config.PollInterval; got <= 0 {
		t.Fatalf("buildReducerService() graph projection repair poll interval = %v, want positive", got)
	}
	if got := service.GraphProjectionPhaseRepairer.Config.RetryDelay; got <= 0 {
		t.Fatalf("buildReducerService() graph projection repair retry delay = %v, want positive", got)
	}
}

func TestBuildReducerServiceWiresSearchVectorBuildRunnerWhenLocalEmbedderEnabled(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		func(key string) string {
			if key == envSemanticSearchLocalEmbedder {
				return "hash"
			}
			return ""
		},
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.SearchVectorBuildRunner == nil {
		t.Fatal("buildReducerService() search vector build runner = nil, want enabled local vector builder")
	}
}

func TestPlatformGraphLockerForReducerSkipsDBWithoutTransactions(t *testing.T) {
	t.Parallel()

	if locker := platformGraphLockerForReducer(&fakeReducerDB{}); locker != nil {
		t.Fatalf("platformGraphLockerForReducer() = %T, want nil", locker)
	}
}

func TestPlatformGraphLockerForReducerUsesTransactionalDB(t *testing.T) {
	t.Parallel()

	locker := platformGraphLockerForReducer(&fakeReducerTransactionalDB{})
	if locker == nil {
		t.Fatal("platformGraphLockerForReducer() = nil, want locker")
	}
	if _, ok := locker.(postgres.PlatformGraphLocker); !ok {
		t.Fatalf("platformGraphLockerForReducer() type = %T, want postgres.PlatformGraphLocker", locker)
	}
}

func TestBuildReducerServiceWiresSharedEdgeGroupBatchOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"ESHU_GRAPH_BACKEND":                 "neo4j",
		inheritanceEdgeGroupBatchSizeEnv:     "3",
		sqlRelationshipEdgeGroupBatchSizeEnv: "4",
	}
	getenv := func(key string) string { return env[key] }

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		getenv,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v", err)
	}

	edgeWriter, ok := service.SharedProjectionRunner.EdgeWriter.(*sourcecypher.EdgeWriter)
	if !ok {
		t.Fatalf("shared projection edge writer type = %T, want *cypher.EdgeWriter", service.SharedProjectionRunner.EdgeWriter)
	}
	assertSharedEdgeWriterConfig(t, edgeWriter, 3, 4, false)
}

func TestBuildReducerServiceWiresRepoDependencyRetractStatementTiming(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		repoDependencyRetractStatementTimingEnv: "true",
	}
	getenv := func(key string) string { return env[key] }

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		getenv,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v", err)
	}

	edgeWriter, ok := service.RepoDependencyProjectionRunner.EdgeWriter.(*sourcecypher.EdgeWriter)
	if !ok {
		t.Fatalf("repo dependency edge writer type = %T, want *cypher.EdgeWriter", service.RepoDependencyProjectionRunner.EdgeWriter)
	}
	if !edgeWriter.RepoDependencyRetractStatementTiming {
		t.Fatal("repo dependency retract statement timing = false, want true")
	}
}

func TestBuildReducerServiceWiresPostgresWorkloadIdentityWriter(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	// Startup backfills already wrote MarkComplete rows; reset so the
	// assertions below count only exec calls from processing the intent.
	db.execs = nil

	intent := reducer.Intent{
		IntentID:        "intent-1",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          reducer.DomainWorkloadIdentity,
		Cause:           "shared follow-up",
		EntityKeys:      []string{"repo-a"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          reducer.IntentStatusPending,
	}

	result, err := service.Executor.Execute(context.Background(), intent)
	if err != nil {
		t.Fatalf("Executor.Execute() error = %v, want nil", err)
	}
	if got, want := result.Status, reducer.ResultStatusSucceeded; got != want {
		t.Fatalf("Executor.Execute().Status = %q, want %q", got, want)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if got := db.execs[0].query; !strings.Contains(got, "INSERT INTO fact_records") {
		t.Fatalf("ExecContext query = %q, want fact_records insert", got)
	}
	if got := db.execs[1].query; !strings.Contains(got, "INSERT INTO graph_projection_phase_state") {
		t.Fatalf("ExecContext query = %q, want graph_projection_phase_state insert", got)
	}
}

func TestBuildReducerServiceWiresPostgresCloudAssetResolutionWriter(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	// Startup backfills already wrote MarkComplete rows; reset so the
	// assertions below count only exec calls from processing the intent.
	db.execs = nil

	intent := reducer.Intent{
		IntentID:        "intent-2",
		ScopeID:         "scope-123",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Domain:          reducer.DomainCloudAssetResolution,
		Cause:           "shared follow-up",
		EntityKeys:      []string{"aws:s3:bucket:logs-prod"},
		RelatedScopeIDs: []string{"scope-123"},
		EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
		Status:          reducer.IntentStatusPending,
	}

	result, err := service.Executor.Execute(context.Background(), intent)
	if err != nil {
		t.Fatalf("Executor.Execute() error = %v, want nil", err)
	}
	if got, want := result.Status, reducer.ResultStatusSucceeded; got != want {
		t.Fatalf("Executor.Execute().Status = %q, want %q", got, want)
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	if got := db.execs[0].query; !strings.Contains(got, "INSERT INTO fact_records") {
		t.Fatalf("ExecContext query = %q, want fact_records insert", got)
	}
	if got := db.execs[1].query; !strings.Contains(got, "INSERT INTO graph_projection_phase_state") {
		t.Fatalf("ExecContext query = %q, want graph_projection_phase_state insert", got)
	}
}

func TestBuildReducerServiceWiresRetryConfigFromEnv(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case reducerRetryDelayEnv:
			return "2m"
		case reducerMaxAttemptsEnv:
			return "5"
		default:
			return ""
		}
	}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if got, want := queue.RetryDelay, 2*time.Minute; got != want {
		t.Fatalf("RetryDelay = %v, want %v", got, want)
	}
	if got, want := queue.MaxAttempts, 5; got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

type fakeReducerDB struct {
	execs []fakeReducerExecCall
}

type fakeReducerTransactionalDB struct {
	fakeReducerDB
}

func (f *fakeReducerTransactionalDB) Begin(context.Context) (postgres.Transaction, error) {
	return nil, nil
}

type fakeReducerExecCall struct {
	query string
	args  []any
}

func (f *fakeReducerDB) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeReducerExecCall{query: query, args: args})
	return fakeReducerResult{}, nil
}

func (f *fakeReducerDB) QueryContext(_ context.Context, query string, args ...any) (postgres.Rows, error) {
	// countFailedGenerationRepositoryScopesSQL (SeedSearchVectorScopeState):
	// report zero failed scopes so startup wiring tests, which only exercise
	// runner construction, aren't coupled to seed-count fixtures. Checked
	// first because this query also matches the broader
	// active_generation_id/ingestion_scopes substring check below.
	if strings.Contains(query, "SELECT count(*)") && strings.Contains(query, "ingestion_scopes") {
		return &fakeCountRows{value: 0}, nil
	}
	// Generation freshness check: return a row matching the intent's generation
	// so the guard treats the intent as current.
	if strings.Contains(query, "active_generation_id") && strings.Contains(query, "ingestion_scopes") {
		scopeGenID := ""
		if len(args) > 0 {
			// Look up what generation the intent carries — fake DB always reports
			// the intent's generation as active so execution proceeds.
			scopeGenID = "generation-456"
		}
		return &fakeGenerationRows{value: &scopeGenID, read: false}, nil
	}
	if strings.Contains(query, "FROM fact_records") {
		return &fakeEmptyRows{}, nil
	}
	if strings.Contains(query, "FROM scope_generations") {
		return &fakeExistsRows{value: false}, nil
	}
	// ProjectedSourceEdgeBackfiller has no count guard, so it always checks
	// backfill-state completion at startup; report "not complete".
	if strings.Contains(query, "code_value_flow_backfill_state") {
		return &fakeExistsRows{value: false}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}

type fakeReducerResult struct{}

func (fakeReducerResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeReducerResult) RowsAffected() (int64, error) { return 1, nil }

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
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
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil)
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
	if got := service.RepoDependencyProjectionRunner.Config.LeaseOwner; got != defaultRepoDependencyProjectionLeaseOwner {
		t.Fatalf("buildReducerService() repo dependency lease owner = %q, want %q", got, defaultRepoDependencyProjectionLeaseOwner)
	}
	if got := service.RepoDependencyProjectionRunner.Config.LeaseTTL; got <= 0 {
		t.Fatalf("buildReducerService() repo dependency lease TTL = %v, want positive", got)
	}
	if got := service.RepoDependencyProjectionRunner.Config.BatchLimit; got <= 0 {
		t.Fatalf("buildReducerService() repo dependency batch limit = %d, want positive", got)
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
	if got, want := codeCallEdgeWriter.InheritanceGroupBatchSize, defaultInheritanceEdgeGroupBatchSize; got != want {
		t.Fatalf("inheritance edge group batch size = %d, want %d", got, want)
	}
	if got, want := codeCallEdgeWriter.SQLRelationshipGroupBatchSize, defaultSQLRelationshipEdgeGroupBatchSize; got != want {
		t.Fatalf("sql relationship edge group batch size = %d, want %d", got, want)
	}
	if service.GraphProjectionPhaseRepairer == nil {
		t.Fatal("buildReducerService() graph projection repairer = nil, want non-nil")
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

func TestBuildReducerServiceWiresSharedEdgeGroupBatchOverrides(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		inheritanceEdgeGroupBatchSizeEnv:     "3",
		sqlRelationshipEdgeGroupBatchSizeEnv: "4",
	}
	getenv := func(key string) string { return env[key] }

	db := &fakeReducerDB{}
	service, err := buildReducerService(
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
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v", err)
	}

	edgeWriter, ok := service.SharedProjectionRunner.EdgeWriter.(*sourcecypher.EdgeWriter)
	if !ok {
		t.Fatalf("shared projection edge writer type = %T, want *cypher.EdgeWriter", service.SharedProjectionRunner.EdgeWriter)
	}
	if got, want := edgeWriter.InheritanceGroupBatchSize, 3; got != want {
		t.Fatalf("inheritance edge group batch size = %d, want %d", got, want)
	}
	if got, want := edgeWriter.SQLRelationshipGroupBatchSize, 4; got != want {
		t.Fatalf("sql relationship edge group batch size = %d, want %d", got, want)
	}
}

func TestBuildReducerServiceWiresPostgresWorkloadIdentityWriter(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

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
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

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
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case reducerRetryDelayEnv:
			return "2m"
		case reducerMaxAttemptsEnv:
			return "5"
		default:
			return ""
		}
	}, nil, nil, nil)
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

func TestBuildReducerServiceWiresNornicDBProjectorDrainGate(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		default:
			return ""
		}
	}, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if !queue.RequireProjectorDrainBeforeClaim {
		t.Fatal("RequireProjectorDrainBeforeClaim = false, want true")
	}
}

func TestBuildReducerServiceWiresExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		case reducerExpectedSourceLocalProjectorsEnv:
			return "878"
		default:
			return ""
		}
	}, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if got, want := queue.ExpectedSourceLocalProjectors, 878; got != want {
		t.Fatalf("ExpectedSourceLocalProjectors = %d, want %d", got, want)
	}
}

func TestBuildReducerServiceWiresSemanticEntityClaimLimit(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		case reducerSemanticEntityClaimLimitEnv:
			return "4"
		default:
			return ""
		}
	}, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if got, want := queue.SemanticEntityClaimLimit, 4; got != want {
		t.Fatalf("SemanticEntityClaimLimit = %d, want %d", got, want)
	}
}

type fakeReducerDB struct {
	execs []fakeReducerExecCall
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
	return nil, fmt.Errorf("unexpected query: %s", query)
}

// fakeGenerationRows returns a single active_generation_id row.
type fakeGenerationRows struct {
	value *string
	read  bool
}

func (r *fakeGenerationRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *fakeGenerationRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scan: got %d dest, want 1", len(dest))
	}
	switch d := dest[0].(type) {
	case *sql.NullString:
		*d = sql.NullString{String: *r.value, Valid: true}
	default:
		return fmt.Errorf("unsupported scan dest type %T", dest[0])
	}
	return nil
}

func (r *fakeGenerationRows) Err() error   { return nil }
func (r *fakeGenerationRows) Close() error { return nil }

type fakeReducerResult struct{}

func (fakeReducerResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeReducerResult) RowsAffected() (int64, error) { return 1, nil }

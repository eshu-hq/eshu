package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerWorkItemIDDeterministic(t *testing.T) {
	t.Parallel()
	intent := projector.ReducerIntent{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       "workload_identity",
		EntityKey:    "entity-1",
	}
	id1 := reducerWorkItemID(intent)
	id2 := reducerWorkItemID(intent)
	if id1 != id2 {
		t.Fatalf("expected deterministic ID, got %q and %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "reducer_") {
		t.Fatalf("expected prefix 'reducer_', got %q", id1)
	}
}

func TestReducerWorkItemIDSanitizesSpecialChars(t *testing.T) {
	t.Parallel()
	intent := projector.ReducerIntent{
		ScopeID:      "org/repo",
		GenerationID: "gen:1",
		Domain:       "workload_identity",
		EntityKey:    "entity/key:value",
	}
	id := reducerWorkItemID(intent)
	if strings.Contains(id, "/") || strings.Contains(id, ":") {
		t.Fatalf("ID contains unsanitized chars: %q", id)
	}
}

func TestReducerConflictDomainKeySplitsCodeAndPlatformGraphFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		domain     reducer.Domain
		wantDomain string
		wantKey    string
	}{
		{
			name:       "semantic entities use code graph conflict family",
			domain:     reducer.DomainSemanticEntityMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "code call edges use code graph conflict family",
			domain:     reducer.DomainCodeCallMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "sql edges use code graph conflict family",
			domain:     reducer.DomainSQLRelationshipMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "inheritance edges use code graph conflict family",
			domain:     reducer.DomainInheritanceMaterialization,
			wantDomain: reducerConflictDomainCodeGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "workload identity uses platform graph conflict family",
			domain:     reducer.DomainWorkloadIdentity,
			wantDomain: reducerConflictDomainPlatformGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "deployment mapping uses platform graph conflict family",
			domain:     reducer.DomainDeploymentMapping,
			wantDomain: reducerConflictDomainPlatformGraph,
			wantKey:    "scope-1",
		},
		{
			name:       "unknown future domains fall back to scope serialization",
			domain:     reducer.DomainOwnership,
			wantDomain: reducerConflictDomainScope,
			wantKey:    "scope-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotDomain, gotKey := reducerConflictDomainKey(projector.ReducerIntent{
				ScopeID: " scope-1 ",
				Domain:  tt.domain,
			})
			if gotDomain != tt.wantDomain {
				t.Fatalf("conflict domain = %q, want %q", gotDomain, tt.wantDomain)
			}
			if gotKey != tt.wantKey {
				t.Fatalf("conflict key = %q, want %q", gotKey, tt.wantKey)
			}
		})
	}
}

func TestReducerQueueBatchEnqueue(t *testing.T) {
	t.Parallel()

	recorder := &reducerRecordingDB{}
	queue := NewReducerQueue(recorder, "test-owner", 30*time.Second)

	// Create 1200 intents to test batching (should use 3 batches: 500 + 500 + 200)
	intents := make([]projector.ReducerIntent, 1200)
	for i := 0; i < 1200; i++ {
		intents[i] = projector.ReducerIntent{
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			Domain:       reducer.DomainWorkloadIdentity,
			EntityKey:    "entity-" + string(rune('a'+i%26)),
			Reason:       "test-reason",
			FactID:       "fact-" + string(rune('a'+i%26)),
			SourceSystem: "test-system",
		}
	}

	ctx := context.Background()
	result, err := queue.Enqueue(ctx, intents)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if result.Count != 1200 {
		t.Errorf("expected count 1200, got %d", result.Count)
	}

	// Should have called ExecContext 3 times (3 batches: 500 + 500 + 200)
	if recorder.execCount != 3 {
		t.Errorf("expected 3 ExecContext calls for 1200 intents, got %d", recorder.execCount)
	}

	// Verify the queries contain multi-row VALUE clauses (presence of multiple value tuples)
	for i, call := range recorder.execs {
		if !strings.Contains(call.query, "INSERT INTO fact_work_items") {
			t.Errorf("exec[%d] missing INSERT INTO fact_work_items", i)
		}
		if !strings.Contains(call.query, "VALUES") {
			t.Errorf("exec[%d] missing VALUES clause", i)
		}
		// Check that it's a batch by looking for multiple value tuples
		valueCount := strings.Count(call.query, "($")
		expectedSize := reducerEnqueueBatchSize
		if i == 2 {
			expectedSize = 200 // last batch
		}
		if valueCount != expectedSize {
			t.Errorf("exec[%d] has %d value tuples, expected %d", i, valueCount, expectedSize)
		}
	}
}

func TestReducerQueueClaimCanWaitForProjectorDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "test-owner",
		LeaseDuration:                    30 * time.Second,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"$5 = false OR NOT EXISTS",
		"projector_work.stage = 'projector'",
		"projector_work.scope_id = fact_work_items.scope_id",
		"projector_work.status IN ('pending', 'retrying', 'claimed', 'running')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing projector drain predicate %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[4], true; got != want {
		t.Fatalf("projector drain arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimGatesSemanticEntitiesOnGlobalProjectorDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "test-owner",
		LeaseDuration:                    30 * time.Second,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	query := db.queries[0].query
	for _, want := range []string{
		"domain <> 'semantic_entity_materialization'",
		"projector_any.stage = 'projector'",
		"projector_any.domain = 'source_local'",
		"projector_any.status IN ('pending', 'retrying', 'claimed', 'running')",
		"projector_done.domain = 'source_local'",
		"projector_done.status = 'succeeded'",
		">= $6",
		"semantic_inflight.domain = 'semantic_entity_materialization'",
		"semantic_inflight.status IN ('claimed', 'running')",
		"semantic_inflight.claim_until > $1",
		"< $7",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("claim query missing semantic global projector gate %q:\n%s", want, query)
		}
	}
	if got, want := db.queries[0].args[6], 1; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimPassesExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 2, 4, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:                            db,
		LeaseOwner:                    "test-owner",
		LeaseDuration:                 30 * time.Second,
		Now:                           func() time.Time { return now },
		ExpectedSourceLocalProjectors: 878,
	}

	_, _, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if got, want := db.queries[0].args[5], 878; got != want {
		t.Fatalf("expected source-local projector arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimPassesSemanticEntityClaimLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 2, 15, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:                               db,
		LeaseOwner:                       "test-owner",
		LeaseDuration:                    30 * time.Second,
		Now:                              func() time.Time { return now },
		RequireProjectorDrainBeforeClaim: true,
		SemanticEntityClaimLimit:         4,
	}

	_, _, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if got, want := db.queries[0].args[6], 4; got != want {
		t.Fatalf("semantic claim limit arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimCanFilterByDomain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 14, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "sql-lane",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainSQLRelationshipMaterialization,
	}

	_, claimed, err := queue.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed {
		t.Fatal("Claim() claimed = true, want false from empty rows")
	}

	if got, want := db.queries[0].args[1], string(reducer.DomainSQLRelationshipMaterialization); got != want {
		t.Fatalf("domain filter arg = %v, want %v", got, want)
	}
}

func TestReducerQueueClaimRejectsUnknownDomainFilter(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseOwner:    "bad-lane",
		LeaseDuration: time.Minute,
		ClaimDomain:   reducer.Domain("not_a_domain"),
	}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "unknown reducer domain") {
		t.Fatalf("Claim() error = %v, want unknown domain validation", err)
	}
}

func TestReducerQueueReopenSucceededResetsSucceededWorkItemToPending(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	queue := ReducerQueue{
		db:  db,
		Now: func() time.Time { return now },
	}

	reopened, err := queue.ReopenSucceeded(context.Background(), "reducer_scope-1_gen-1_deployment_mapping_repo_1")
	if err != nil {
		t.Fatalf("ReopenSucceeded() error = %v, want nil", err)
	}
	if !reopened {
		t.Fatal("ReopenSucceeded() reopened = false, want true")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"status = 'pending'",
		"attempt_count = 0",
		"stage = 'reducer'",
		"status = 'succeeded'",
		"next_attempt_at = NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reopen query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[0], now; got != want {
		t.Fatalf("updated_at arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "reducer_scope-1_gen-1_deployment_mapping_repo_1"; got != want {
		t.Fatalf("work item arg = %v, want %v", got, want)
	}
}

func TestReducerQueueReopenSucceededReturnsFalseWhenNoRowMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{}},
	}
	queue := ReducerQueue{db: db}

	reopened, err := queue.ReopenSucceeded(context.Background(), "missing-work-item")
	if err != nil {
		t.Fatalf("ReopenSucceeded() error = %v, want nil", err)
	}
	if reopened {
		t.Fatal("ReopenSucceeded() reopened = true, want false")
	}
}

func TestReducerQueueReopenSucceededWrapsExecError(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db: &fakeExecQueryer{
			execErrors: []error{errors.New("boom")},
		},
	}

	_, err := queue.ReopenSucceeded(context.Background(), "work-item")
	if err == nil {
		t.Fatal("ReopenSucceeded() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "reopen succeeded reducer work") {
		t.Fatalf("ReopenSucceeded() error = %v, want wrapped reopen context", err)
	}
}

func TestReducerQueueReplayDomainReopensSucceededWorkItem(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
	}
	queue := ReducerQueue{
		db:  db,
		Now: func() time.Time { return now },
	}

	replayed, err := queue.ReplayDomain(context.Background(), "scope-1", "gen-1", reducer.DomainWorkloadMaterialization)
	if err != nil {
		t.Fatalf("ReplayDomain() error = %v, want nil", err)
	}
	if !replayed {
		t.Fatal("ReplayDomain() replayed = false, want true")
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE fact_work_items",
		"scope_id = $2",
		"generation_id = $3",
		"domain = $4",
		"status = 'succeeded'",
		"status = 'pending'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("ReplayDomain query missing %q:\n%s", want, query)
		}
	}
	if got, want := db.execs[0].args[0], now; got != want {
		t.Fatalf("updated_at arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[1], "scope-1"; got != want {
		t.Fatalf("scope arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[2], "gen-1"; got != want {
		t.Fatalf("generation arg = %v, want %v", got, want)
	}
	if got, want := db.execs[0].args[3], string(reducer.DomainWorkloadMaterialization); got != want {
		t.Fatalf("domain arg = %v, want %v", got, want)
	}
}

func TestReducerQueueReplayDomainReturnsFalseWhenNoSucceededRowMatches(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{}},
	}
	queue := ReducerQueue{db: db}

	replayed, err := queue.ReplayDomain(context.Background(), "scope-1", "gen-1", reducer.DomainWorkloadMaterialization)
	if err != nil {
		t.Fatalf("ReplayDomain() error = %v, want nil", err)
	}
	if replayed {
		t.Fatal("ReplayDomain() replayed = true, want false")
	}
}

func TestReducerQueueReplayWorkloadMaterializationEnqueuesReplayWhenNoSucceededRowExists(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 10, 30, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		execResults: []sql.Result{
			rowsAffectedResult{},
			rowsAffectedResult{rowsAffected: 1},
		},
	}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	replayed, err := queue.ReplayWorkloadMaterialization(
		context.Background(),
		"scope-1",
		"gen-1",
		"repo:service-gha",
	)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterialization() error = %v, want nil", err)
	}
	if !replayed {
		t.Fatal("ReplayWorkloadMaterialization() replayed = false, want true")
	}
	if got, want := len(db.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}

	enqueueQuery := db.execs[1].query
	if !strings.Contains(enqueueQuery, "INSERT INTO fact_work_items") {
		t.Fatalf("enqueue query missing insert:\n%s", enqueueQuery)
	}
	if got, want := db.execs[1].args[1], "scope-1"; got != want {
		t.Fatalf("enqueue scope arg = %v, want %v", got, want)
	}
	if got, want := db.execs[1].args[2], "gen-1"; got != want {
		t.Fatalf("enqueue generation arg = %v, want %v", got, want)
	}
	if got, want := db.execs[1].args[3], string(reducer.DomainWorkloadMaterialization); got != want {
		t.Fatalf("enqueue domain arg = %v, want %v", got, want)
	}
	payload, ok := db.execs[1].args[7].([]byte)
	if !ok {
		t.Fatalf("enqueue payload type = %T, want []byte", db.execs[1].args[7])
	}
	if !strings.Contains(string(payload), "deployment mapping resolved stronger evidence") {
		t.Fatalf("enqueue payload = %s, want replay reason", payload)
	}
	if !strings.Contains(string(payload), "repo:service-gha") {
		t.Fatalf("enqueue payload = %s, want replay entity key", payload)
	}
}

func TestReducerQueueCountInFlightByDomainReturnsCount(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{3}}},
		},
	}
	queue := ReducerQueue{db: db}

	count, err := queue.CountInFlightByDomain(
		context.Background(),
		reducer.DomainDeploymentMapping,
	)
	if err != nil {
		t.Fatalf("CountInFlightByDomain() error = %v, want nil", err)
	}
	if got, want := count, 3; got != want {
		t.Fatalf("CountInFlightByDomain() = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if !strings.Contains(db.queries[0].query, "status NOT IN ('succeeded', 'dead_letter')") {
		t.Fatalf("count query = %q, want terminal-status filter", db.queries[0].query)
	}
	if got, want := db.queries[0].args[0], string(reducer.DomainDeploymentMapping); got != want {
		t.Fatalf("domain arg = %v, want %v", got, want)
	}
}

func TestReducerQueueCountInFlightByDomainRejectsUnknownDomain(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{db: &fakeExecQueryer{}}

	_, err := queue.CountInFlightByDomain(context.Background(), reducer.Domain("not-real"))
	if err == nil {
		t.Fatal("CountInFlightByDomain() error = nil, want non-nil")
	}
}

// TestReducerQueueValidateEnqueueAcceptsZeroLeaseFields is the load-bearing
// regression test for issue #170. It proves the enqueue path no longer demands
// LeaseOwner/LeaseDuration placeholder values. Before the validate() split,
// callers like IngestionStore.EnqueueConfigStateDriftIntents had to fabricate
// lease values just to pass the single combined validate() check, even though
// the SQL writes NULL for lease_owner/claim_until on insert. After the split,
// validateEnqueue() omits the lease-side checks; only validateClaim() needs
// them.
func TestReducerQueueValidateEnqueueAcceptsZeroLeaseFields(t *testing.T) {
	t.Parallel()

	recorder := &reducerRecordingDB{}
	queue := ReducerQueue{db: recorder}

	// No-op enqueue with empty intents still runs validateEnqueue().
	if _, err := queue.Enqueue(context.Background(), nil); err != nil {
		t.Fatalf("Enqueue(nil) error = %v, want nil (zero lease fields should be allowed on enqueue)", err)
	}

	// Real enqueue with intents — validateEnqueue() must pass without
	// LeaseOwner / LeaseDuration set.
	intents := []projector.ReducerIntent{{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainConfigStateDrift,
		EntityKey:    "entity-1",
		Reason:       "regression test",
		SourceSystem: "test",
	}}
	if _, err := queue.Enqueue(context.Background(), intents); err != nil {
		t.Fatalf("Enqueue(intents) error = %v, want nil (zero lease fields should be allowed on enqueue)", err)
	}
	if recorder.execCount != 1 {
		t.Fatalf("expected one INSERT exec, got %d", recorder.execCount)
	}
}

// TestReducerQueueValidateEnqueueRequiresDB confirms validateEnqueue() still
// rejects a queue with no database handle and that the error string names the
// enqueue side so stack traces and wrapped errors are self-locating.
func TestReducerQueueValidateEnqueueRequiresDB(t *testing.T) {
	t.Parallel()

	var queue ReducerQueue
	intents := []projector.ReducerIntent{{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainConfigStateDrift,
		EntityKey:    "entity-1",
	}}

	_, err := queue.Enqueue(context.Background(), intents)
	if err == nil {
		t.Fatal("Enqueue() error = nil, want validation error for nil db")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "database is required")
	}
	if !strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, want enqueue-side marker %q", err.Error(), "for enqueue")
	}
}

// TestReducerQueueValidateClaimRequiresLeaseOwner confirms validateClaim()
// rejects a queue missing LeaseOwner with a claim-side error marker.
func TestReducerQueueValidateClaimRequiresLeaseOwner(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseDuration: time.Minute,
	}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want validation error for missing lease owner")
	}
	if !strings.Contains(err.Error(), "lease owner") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "lease owner")
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
}

// TestReducerQueueValidateClaimRequiresPositiveLeaseDuration confirms
// validateClaim() rejects a non-positive LeaseDuration on the heartbeat path.
// Heartbeat is the most lease-sensitive consumer because it renews
// claim_until from now+LeaseDuration.
func TestReducerQueueValidateClaimRequiresPositiveLeaseDuration(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:         &fakeExecQueryer{},
		LeaseOwner: "test-owner",
	}

	err := queue.Heartbeat(context.Background(), reducer.Intent{IntentID: "work-1"})
	if err == nil {
		t.Fatal("Heartbeat() error = nil, want validation error for zero lease duration")
	}
	if !strings.Contains(err.Error(), "lease duration") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "lease duration")
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
}

// TestReducerQueueValidateEnqueueRejectsInvalidClaimDomain proves the shared
// ClaimDomain.Validate() check fires on the enqueue path and that the wrapped
// error carries the enqueue-side marker so callers can self-locate failures
// without parsing call stacks.
func TestReducerQueueValidateEnqueueRejectsInvalidClaimDomain(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:          &fakeExecQueryer{},
		ClaimDomain: reducer.Domain("not_a_domain"),
	}

	_, err := queue.Enqueue(context.Background(), []projector.ReducerIntent{{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainConfigStateDrift,
		EntityKey:    "entity-1",
	}})
	if err == nil {
		t.Fatal("Enqueue() error = nil, want unknown-domain validation error")
	}
	if !strings.Contains(err.Error(), "unknown reducer domain") {
		t.Fatalf("error = %q, want unknown-domain message", err.Error())
	}
	if !strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, want enqueue-side marker %q", err.Error(), "for enqueue")
	}
}

// TestReducerQueueValidateClaimAlsoRejectsInvalidClaimDomain proves
// validateClaim() also fails on an invalid ClaimDomain and that the wrapped
// error carries the claim-side marker (not the enqueue-side marker) even
// though the underlying check is shared with the enqueue path. This is the
// regression guard for Copilot review of PR #196: previously validateClaim
// delegated to validateEnqueue verbatim, so a shared-check failure on the
// claim path bubbled up labeled "for enqueue".
func TestReducerQueueValidateClaimAlsoRejectsInvalidClaimDomain(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{
		db:            &fakeExecQueryer{},
		LeaseOwner:    "test-owner",
		LeaseDuration: time.Minute,
		ClaimDomain:   reducer.Domain("not_a_domain"),
	}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want unknown-domain validation error")
	}
	if !strings.Contains(err.Error(), "unknown reducer domain") {
		t.Fatalf("error = %q, want unknown-domain message", err.Error())
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
	if strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, must not carry enqueue-side marker on claim path", err.Error())
	}
}

// TestReducerQueueValidateClaimRequiresDBWithClaimSideMarker is the
// regression guard for the validateClaim->validateEnqueue composition trap
// caught in Copilot review of PR #196: a db-nil failure on the claim path
// must say "for claim/ack/heartbeat/fail", not "for enqueue". Before the
// validateShared refactor, validateClaim delegated to validateEnqueue
// verbatim, so the shared db-nil check produced an enqueue-marked error on
// the claim path.
func TestReducerQueueValidateClaimRequiresDBWithClaimSideMarker(t *testing.T) {
	t.Parallel()

	queue := ReducerQueue{LeaseOwner: "x", LeaseDuration: time.Minute}

	_, _, err := queue.Claim(context.Background())
	if err == nil {
		t.Fatal("Claim() error = nil, want validation error for nil db")
	}
	if !strings.Contains(err.Error(), "database is required") {
		t.Fatalf("error = %q, want substring %q", err.Error(), "database is required")
	}
	if !strings.Contains(err.Error(), "for claim/ack/heartbeat/fail") {
		t.Fatalf("error = %q, want claim-side marker %q", err.Error(), "for claim/ack/heartbeat/fail")
	}
	if strings.Contains(err.Error(), "for enqueue") {
		t.Fatalf("error = %q, must not carry enqueue-side marker on claim path", err.Error())
	}
}

// reducerRecordingDB records ExecContext calls for verification.
type reducerRecordingDB struct {
	execCount int
	execs     []reducerRecordedExec
}

type reducerRecordedExec struct {
	query string
	args  []any
}

func (r *reducerRecordingDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execCount++
	r.execs = append(r.execs, reducerRecordedExec{
		query: query,
		args:  append([]any(nil), args...),
	})
	return reducerProofResult{}, nil
}

func (r *reducerRecordingDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, nil
}

// reducerProofResult is a minimal sql.Result implementation for testing.
type reducerProofResult struct{}

func (reducerProofResult) LastInsertId() (int64, error) { return 0, nil }
func (reducerProofResult) RowsAffected() (int64, error) { return 1, nil }

type rowsAffectedResult struct {
	rowsAffected int64
}

func (r rowsAffectedResult) LastInsertId() (int64, error) { return 0, nil }
func (r rowsAffectedResult) RowsAffected() (int64, error) { return r.rowsAffected, nil }

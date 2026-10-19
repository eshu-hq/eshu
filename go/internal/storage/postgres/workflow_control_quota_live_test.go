// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Per-component quota proofs (#8471, epic #8467).
//
// The harness drives the shipped WorkflowControlStore.ClaimNextEligible with
// a quota lookup configured, on real Postgres. Four behaviors are pinned:
// over-quota throttles on an idle system, racing replicas cannot overshoot
// the shared reservation, quota state survives coordinator restart, and an
// unreadable quota table falls back to a floor rate with a distinct signal.
//
// Run with a throwaway Postgres:
//
//	ESHU_POSTGRES_DSN='postgres://postgres:postgres@localhost:<port>/eshu?sslmode=disable' \
//	  go test ./internal/storage/postgres -run 'ClaimQuota' -count=1 -v

const (
	quotaTestInstance = "quota-test-instance"
	quotaTestScope    = "quota-test-scope"
)

// quotaTestFixture opens the isolated-schema workflow store with a quota
// lookup enforcing limit for scope.CollectorGit, plus real instruments
// backed by a manual reader for signal assertions.
func quotaTestFixture(
	t *testing.T,
	database *sql.DB,
	limit int,
	floor int,
) (*WorkflowControlStore, context.Context, sdkmetric.Reader) {
	t.Helper()
	_, store := openWorkflowControlStoreOnDB(t, database)
	store.QuotaLookup = func(kind scope.CollectorKind) (workflow.ClaimQuota, bool) {
		if kind == scope.CollectorGit {
			return workflow.ClaimQuota{MaxGenerationsPerHour: limit}, true
		}
		return workflow.ClaimQuota{}, false
	}
	store.QuotaFloorGrantsPerHour = floor
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("quota-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store.Instruments = inst
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return store, ctx, reader
}

// openWorkflowControlStoreOnDB builds a store over an existing isolated
// database handle without re-running EnsureSchema, so tests can drop the
// quota table to simulate unreadable quota state.
func openWorkflowControlStoreOnDB(t *testing.T, database *sql.DB) (*sql.DB, *WorkflowControlStore) {
	t.Helper()
	store := NewWorkflowControlStore(SQLDB{DB: database})
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}
	return database, store
}

// seedQuotaBacklog enqueues count pending items sharing one scope under one
// (collector_kind, collector_instance_id): quota state is per
// (component, scope), so the budget binds only when items share a scope.
func seedQuotaBacklog(
	t *testing.T,
	store *WorkflowControlStore,
	ctx context.Context,
	runID string,
	base time.Time,
	count int,
) []string {
	t.Helper()
	mustCreateRun(t, store, ctx, workflow.Run{
		RunID:       runID,
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   base,
		UpdatedAt:   base,
	})
	items := make([]workflow.WorkItem, 0, count)
	seeded := make([]string, 0, count)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("%s:quota:%04d", runID, i)
		created := base.Add(time.Duration(i) * time.Millisecond)
		items = append(items, workflow.WorkItem{
			WorkItemID:          id,
			RunID:               runID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: quotaTestInstance,
			SourceSystem:        string(scope.CollectorGit),
			ScopeID:             quotaTestScope,
			AcceptanceUnitID:    quotaTestScope,
			SourceRunID:         id + "-generation",
			GenerationID:        id + "-generation",
			// One draw slot per item: the fair-claim lottery serves one
			// head per fairness key, so a shared key would collapse all
			// racing replicas onto a single head (SKIP LOCKED skips the
			// rest) and the race would never reach the reservation.
			FairnessKey: fmt.Sprintf("quota-key-%04d", i),
			Status:      workflow.WorkItemStatusPending,
			CreatedAt:   created,
			UpdatedAt:   created,
		})
		seeded = append(seeded, id)
	}
	if err := store.EnqueueWorkItems(ctx, items); err != nil {
		t.Fatalf("EnqueueWorkItems(%d items) error = %v, want nil", len(items), err)
	}
	return seeded
}

// quotaClaim performs one claim with a unique claim id.
func quotaClaim(
	t *testing.T,
	store *WorkflowControlStore,
	ctx context.Context,
	owner string,
	nonce int,
) (workflow.WorkItem, workflow.Claim, bool) {
	t.Helper()
	item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: quotaTestInstance,
		OwnerID:             owner,
		ClaimID:             fmt.Sprintf("quota-claim-%s-%d", owner, nonce),
	}, time.Now().UTC(), 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	return item, claim, found
}

// quotaWindowGrants sums current-window grants for the component across
// scopes, so the assertion holds regardless of scope fan-out.
func quotaWindowGrants(t *testing.T, database *sql.DB, ctx context.Context) int {
	t.Helper()
	var total sql.NullInt64
	err := database.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(grants), 0) FROM component_quota_windows
		 WHERE component = $1 AND window_start = date_trunc('hour', now())`,
		string(scope.CollectorGit)).Scan(&total)
	if err != nil {
		t.Fatalf("quota window grants error = %v, want nil", err)
	}
	return int(total.Int64)
}

// quotaCounterTotal collects the reader and sums the named counter.
func quotaCounterTotal(t *testing.T, reader sdkmetric.Reader, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return counterTotal(rm, name)
}

// TestClaimQuotaOverBudgetThrottlesOnIdleSystem proves an over-quota
// component is throttled even with no contention: two grants succeed under
// a limit of two, the third claim finds nothing despite pending work, and
// rolling the hour window releases the throttle.
func TestClaimQuotaOverBudgetThrottlesOnIdleSystem(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	postgresproof.RaisePoolForConcurrentClaimers(database, 1)
	store, ctx, reader := quotaTestFixture(t, database, 2, 60)
	base := time.Now().UTC().Truncate(time.Millisecond)
	seedQuotaBacklog(t, store, ctx, "quota-idle-run", base, 3)

	if _, _, found := quotaClaim(t, store, ctx, "quota-idle-owner", 1); !found {
		t.Fatal("first claim found = false, want true (budget remains)")
	}
	if _, _, found := quotaClaim(t, store, ctx, "quota-idle-owner", 2); !found {
		t.Fatal("second claim found = false, want true (budget remains)")
	}
	if _, _, found := quotaClaim(t, store, ctx, "quota-idle-owner", 3); found {
		t.Fatal("third claim found = true, want false (over quota on an idle system)")
	}
	if got := quotaWindowGrants(t, database, ctx); got != 2 {
		t.Fatalf("window grants = %d, want 2", got)
	}
	if got := quotaCounterTotal(t, reader, "eshu_dp_workflow_claim_quota_exceeded_total"); got != 1 {
		t.Fatalf("quota_exceeded_total = %d, want 1", got)
	}

	// Roll the database-side hour window: the exhausted bucket ages out and
	// the pending item becomes grantable again.
	if _, err := database.ExecContext(ctx,
		`UPDATE component_quota_windows SET window_start = window_start - INTERVAL '2 hours'
		  WHERE component = $1`, string(scope.CollectorGit)); err != nil {
		t.Fatalf("age quota window error = %v, want nil", err)
	}
	if _, _, found := quotaClaim(t, store, ctx, "quota-idle-owner", 4); !found {
		t.Fatal("post-window claim found = false, want true (new hour bucket)")
	}
	if got := quotaWindowGrants(t, database, ctx); got != 1 {
		t.Fatalf("new window grants = %d, want 1", got)
	}
	// The aged bucket is pruned by the granting statement itself: no
	// elapsed rows may remain.
	var stale int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM component_quota_windows
		  WHERE window_start < date_trunc('hour', now()) - INTERVAL '1 hour'`).Scan(&stale); err != nil {
		t.Fatalf("stale bucket count error = %v, want nil", err)
	}
	if stale != 0 {
		t.Fatalf("stale quota buckets = %d, want 0 (pruned with the grant)", stale)
	}
}

// TestClaimQuotaRacingReplicasCannotOvershoot proves the reservation —
// not the selection pre-filter — is the atomic gate. Eight replicas fire
// one claim each while a blocker holds the usage-row lock with one grant
// of budget left: every replica's snapshot shows room, so all eight reach
// the reserve, pile on the row lock, and serialize. Exactly one wins; the
// seven losers see the winner's committed count in their EvalPlanQual
// recheck, reserve nothing, and grant nothing. A check-then-grant shape
// (or an unconditional reserve) grants all eight here and fails this test;
// it was verified to do so by mutation during development.
func TestClaimQuotaRacingReplicasCannotOvershoot(t *testing.T) {
	const claimers = 8
	const limit = 5
	const backlog = 20
	database, _ := openWorkflowControlIntegrationStore(t)
	postgresproof.RaisePoolForConcurrentClaimers(database, claimers+2)
	seedStore, ctx, _ := quotaTestFixture(t, database, limit, 60)
	base := time.Now().UTC().Truncate(time.Millisecond)
	seedQuotaBacklog(t, seedStore, ctx, "quota-race-run", base, backlog)

	// Pre-fill the window to one grant below the limit, then hold the
	// usage row locked so every replica's snapshot shows room.
	if _, err := database.ExecContext(ctx,
		`INSERT INTO component_quota_windows (component, scope_id, window_start, grants)
		 VALUES ($1, $2, date_trunc('hour', now()), $3)`,
		string(scope.CollectorGit), quotaTestScope, limit-1); err != nil {
		t.Fatalf("prefill quota window error = %v, want nil", err)
	}
	blocker, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("blocker pin connection: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	blockerTx, err := blocker.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("blocker begin: %v", err)
	}
	var locked int
	if err := blockerTx.QueryRowContext(ctx,
		`SELECT grants FROM component_quota_windows WHERE component = $1 AND scope_id = $2
		 AND window_start = date_trunc('hour', now()) FOR UPDATE`,
		string(scope.CollectorGit), quotaTestScope).Scan(&locked); err != nil {
		t.Fatalf("blocker lock usage row: %v", err)
	}

	type outcome struct {
		claimer int
		itemID  string
		claimID string
		found   bool
		err     error
	}
	results := make(chan outcome, claimers)
	started := make(chan struct{}, claimers)
	start := make(chan struct{})
	for c := 0; c < claimers; c++ {
		go func(claimer int) {
			conn, err := database.Conn(ctx)
			if err != nil {
				results <- outcome{claimer: claimer, err: fmt.Errorf("pin connection: %w", err)}
				started <- struct{}{}
				return
			}
			defer func() { _ = conn.Close() }()
			// claimDepthConn pins this claimer to its connection, as in
			// the #8468 harness, so all eight replicas hold distinct
			// backends for the whole race.
			store := NewWorkflowControlStore(claimDepthConn{conn: conn})
			store.QuotaLookup = seedStore.QuotaLookup
			owner := fmt.Sprintf("quota-race-owner-%d", claimer)
			started <- struct{}{}
			<-start
			item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
				CollectorKind:       scope.CollectorGit,
				CollectorInstanceID: quotaTestInstance,
				OwnerID:             owner,
				ClaimID:             fmt.Sprintf("quota-race-%d", claimer),
			}, time.Now().UTC(), 5*time.Minute)
			results <- outcome{claimer: claimer, itemID: item.WorkItemID, claimID: claim.ClaimID, found: found, err: err}
		}(c)
	}
	for c := 0; c < claimers; c++ {
		<-started
	}
	close(start)
	// Every replica is inside its claim statement (snapshot taken, showing
	// one grant of room) with a margin three orders of magnitude above
	// goroutine wakeup skew before the blocker releases the row.
	time.Sleep(time.Second)
	if err := blockerTx.Commit(); err != nil {
		t.Fatalf("blocker commit: %v", err)
	}

	granted := make(map[string]string)
	for c := 0; c < claimers; c++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("claimer %d error = %v, want nil", r.claimer, r.err)
		}
		if !r.found {
			continue
		}
		if other, dup := granted[r.itemID]; dup {
			t.Fatalf("work item %q granted twice (%q and %q)", r.itemID, other, r.claimID)
		}
		granted[r.itemID] = r.claimID
	}
	if got, want := len(granted), 1; got != want {
		t.Fatalf("racing grants = %d, want %d (one winner, seven serialized losers)", got, want)
	}
	if got := quotaWindowGrants(t, database, ctx); got != limit {
		t.Fatalf("window grants = %d, want %d", got, limit)
	}
	// The budget is now exactly exhausted: further claims grant nothing.
	if _, _, found := quotaClaim(t, seedStore, ctx, "quota-race-owner-tail", 1); found {
		t.Fatal("post-race claim found = true, want false (budget exhausted)")
	}
}

// TestClaimQuotaStateSurvivesCoordinatorRestart proves the budget lives in
// shared storage, not coordinator memory: a fresh store over the same
// database sees the grants the pre-restart store made.
func TestClaimQuotaStateSurvivesCoordinatorRestart(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	preRestart, ctx, _ := quotaTestFixture(t, database, 3, 60)
	base := time.Now().UTC().Truncate(time.Millisecond)
	seedQuotaBacklog(t, preRestart, ctx, "quota-restart-run", base, 4)

	for i := 1; i <= 2; i++ {
		if _, _, found := quotaClaim(t, preRestart, ctx, "quota-restart-owner", i); !found {
			t.Fatalf("pre-restart claim %d found = false, want true", i)
		}
	}

	// Simulate a coordinator restart: a new store with fresh in-memory
	// state over the same database.
	postRestart, _, _ := quotaTestFixture(t, database, 3, 60)
	if _, _, found := quotaClaim(t, postRestart, ctx, "quota-restart-owner", 3); !found {
		t.Fatal("post-restart claim found = false, want true (one grant remains)")
	}
	if _, _, found := quotaClaim(t, postRestart, ctx, "quota-restart-owner", 4); found {
		t.Fatal("post-restart over-budget claim found = true, want false (budget survived restart)")
	}
	if got := quotaWindowGrants(t, database, ctx); got != 3 {
		t.Fatalf("window grants = %d, want 3", got)
	}
}

// TestClaimQuotaUnreadableFallsBackToFloorRate drops the quota state table
// to simulate a storage fault, then proves claims still flow at the
// configured floor rate with one fallback signal per degraded call, and
// throttle (not error) once the floor is exhausted.
func TestClaimQuotaUnreadableFallsBackToFloorRate(t *testing.T) {
	database, _ := openWorkflowControlIntegrationStore(t)
	store, ctx, reader := quotaTestFixture(t, database, 100, 2)
	base := time.Now().UTC().Truncate(time.Millisecond)
	seedQuotaBacklog(t, store, ctx, "quota-floor-run", base, 4)

	// Shield the unreadable simulation from a polluted search_path: other
	// suites have been observed bootstrapping stray tables into public,
	// and unqualified quota references would fall through to them after
	// the schema-local drop. Drop the public leftover (if any), then the
	// schema-local table, then prove unqualified resolution fails before
	// any claim runs.
	if _, err := database.ExecContext(ctx, `DROP TABLE IF EXISTS public.component_quota_windows`); err != nil {
		t.Fatalf("drop public quota leftover error = %v, want nil", err)
	}
	if _, err := database.ExecContext(ctx, `DROP TABLE component_quota_windows`); err != nil {
		t.Fatalf("drop quota table error = %v, want nil", err)
	}
	var probe int
	if err := database.QueryRowContext(ctx, `SELECT 1 FROM component_quota_windows LIMIT 1`).Scan(&probe); err == nil {
		t.Fatal("quota table still resolves after drop, want unreadable (polluted search_path)")
	}

	for i := 1; i <= 2; i++ {
		if _, _, found := quotaClaim(t, store, ctx, "quota-floor-owner", i); !found {
			t.Fatalf("floor claim %d found = false, want true (floor budget remains)", i)
		}
	}
	if _, _, found := quotaClaim(t, store, ctx, "quota-floor-owner", 3); found {
		t.Fatal("floor-exhausted claim found = true, want false (floor throttles, never errors)")
	}
	if got := quotaCounterTotal(t, reader, "eshu_dp_workflow_claim_quota_unreadable_fallback_total"); got != 3 {
		t.Fatalf("quota_unreadable_fallback_total = %d, want 3 (one signal per degraded call)", got)
	}
}

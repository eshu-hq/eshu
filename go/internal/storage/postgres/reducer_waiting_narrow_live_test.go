// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Per-item read-set waits (#10877): narrowed waiting live proofs.
//
// On the full production bootstrap schema, on a disposable database: a
// narrowed corpus wait parks with the verdict-time blocking set in its key,
// stays parked while a blocker is open, wakes selectively when its blocker
// activates (a wait naming only other scopes stays parked), the shared audit
// reopens a satisfied narrowed wait, and concurrent activations and parks
// converge without a lost wake. Gated on ESHU_REDUCER_FAIRNESS_PROOF_DSN or
// ESHU_POSTGRES_DSN.

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10877 narrowed corpus waits: park, selective activation wake, audit reopen, and concurrent convergence on the production schema
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// narrowWaitTestCause scripts the Fail-path narrowing: the verdict-time read
// set and blocking set the parked wait keys on.
type narrowWaitTestCause struct {
	repos    []string
	blocking []string
}

func (e narrowWaitTestCause) Error() string { return "narrow wait test cause" }

func (e narrowWaitTestCause) CorpusWaitRepoIDs() []string { return e.repos }

func (e narrowWaitTestCause) CorpusWaitBlockingScopeIDs() []string { return e.blocking }

// seedNarrowWaitScope seeds an active scope with the relationship generation
// status given: active for the waiter, pending for a holder.
func seedNarrowWaitScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID, rgStatus string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scopeID, at, genID); err != nil {
		t.Fatalf("seed narrow wait scope %s: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
VALUES ($1, $2, $3, $4, $4)`, genID, scopeID, rgStatus, at); err != nil {
		t.Fatalf("seed narrow wait relationship generation %s: %v", genID, err)
	}
	// The queue row's generation FK needs the scope generation row the
	// fence predicate itself never reads.
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, is_delta,
    observed_at, ingested_at, status
) VALUES ($2, $1, 'synthetic', FALSE, $3, $3, 'active')
ON CONFLICT DO NOTHING`, scopeID, genID, at); err != nil {
		t.Fatalf("seed narrow wait scope generation %s: %v", genID, err)
	}
}

// seedNarrowRetryRow seeds one retrying reducer row in a non-counting corpus
// class, the state a Fail leaves before the wait parks.
func seedNarrowRetryRow(t *testing.T, ctx context.Context, db *sql.DB, workItemID, scopeID, genID, domain, failureClass string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
    last_attempt_at, visible_at, failure_class, failure_message, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', $4, 'retrying', 1, $5::timestamptz, $5::timestamptz + interval '10 minutes', $6, 'narrow wait proof', '{}'::jsonb, $5::timestamptz, $5::timestamptz)`,
		workItemID, scopeID, genID, domain, at, failureClass); err != nil {
		t.Fatalf("seed narrow retry row %s: %v", workItemID, err)
	}
}

func narrowWaitIntent(workItemID, scopeID, genID string) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     workItemID,
		ScopeID:      scopeID,
		GenerationID: genID,
		Domain:       "workload_materialization",
	}
}

func narrowWaitEntry() WaitingDeferral {
	return WaitingDeferral{
		FailureClass:  "workload_materialization_resolution_not_ready",
		WaitsForKind:  WaitingKindRelationshipGenerationActive,
		WakeSite:      WaitingWakeSiteInTx,
		ProvidingSave: "test activation",
	}
}

// countNarrowWaits returns the parked wait rows for one queue row.
func countNarrowWaits(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM reducer_waiting_items WHERE work_item_id = $1`, workItemID).Scan(&count); err != nil {
		t.Fatalf("count narrow waits for %s: %v", workItemID, err)
	}
	return count
}

// narrowQueueVisibleAt returns one queue row's status, attempt count, and
// visibility horizon.
func narrowQueueState(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) (string, int, time.Time) {
	t.Helper()
	var status string
	var attempts int
	var visibleAt time.Time
	if err := db.QueryRowContext(ctx,
		`SELECT status, attempt_count, visible_at FROM fact_work_items WHERE work_item_id = $1`, workItemID).
		Scan(&status, &attempts, &visibleAt); err != nil {
		t.Fatalf("read narrow queue row %s: %v", workItemID, err)
	}
	return status, attempts, visibleAt
}

func TestNarrowedWaitSelectiveWakeLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: db})

	// Waiter scope active; two holders pending.
	seedNarrowWaitScope(t, ctx, db, "scope-waiter", "gen-waiter", "active", at)
	seedNarrowWaitScope(t, ctx, db, "scope-holder-a", "gen-holder-a", "pending", at)
	seedNarrowWaitScope(t, ctx, db, "scope-holder-b", "gen-holder-b", "pending", at)

	seedNarrowRetryRow(t, ctx, db, "wait-a", "scope-waiter", "gen-waiter",
		"workload_materialization", "workload_materialization_resolution_not_ready", at)
	seedNarrowRetryRow(t, ctx, db, "wait-b", "scope-waiter", "gen-waiter",
		"workload_materialization", "workload_materialization_resolution_not_ready", at)

	now := time.Now().UTC()
	parked, err := setAsideWaitingForCause(ctx, SQLDB{DB: db}, nil,
		narrowWaitIntent("wait-a", "scope-waiter", "gen-waiter"), narrowWaitEntry(),
		narrowWaitTestCause{repos: []string{"repo-a"}, blocking: []string{"scope-holder-a"}}, nil, now)
	if err != nil || !parked {
		t.Fatalf("park wait-a = %v, %v, want true, nil", parked, err)
	}
	parked, err = setAsideWaitingForCause(ctx, SQLDB{DB: db}, nil,
		narrowWaitIntent("wait-b", "scope-waiter", "gen-waiter"), narrowWaitEntry(),
		narrowWaitTestCause{repos: []string{"repo-a"}, blocking: []string{"scope-holder-b"}}, nil, now)
	if err != nil || !parked {
		t.Fatalf("park wait-b = %v, %v, want true, nil", parked, err)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-a"); got != 1 {
		t.Fatalf("parked waits for wait-a = %d, want 1", got)
	}

	// Activating holder A reopens only the wait naming A: the wake is
	// selective on the verdict-time blocking set, never a fleet fanout.
	if err := store.ActivateResolutionGeneration(ctx, "gen-holder-a", "scope-holder-a"); err != nil {
		t.Fatalf("activate holder A: %v", err)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-a"); got != 0 {
		t.Fatalf("waits for wait-a after A activation = %d, want 0", got)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-b"); got != 1 {
		t.Fatalf("waits for wait-b after A activation = %d, want 1: a wait naming only B stays parked", got)
	}
	status, attempts, _ := narrowQueueState(t, ctx, db, "wait-a")
	if status != "retrying" || attempts != 1 {
		t.Fatalf("wait-a queue row = %s attempts %d, want retrying/1: the wake must not burn the attempt budget", status, attempts)
	}
	status, attempts, _ = narrowQueueState(t, ctx, db, "wait-b")
	if status != "retrying" || attempts != 1 {
		t.Fatalf("wait-b queue row = %s attempts %d, want retrying/1", status, attempts)
	}

	// Activating holder B reopens the second wait.
	if err := store.ActivateResolutionGeneration(ctx, "gen-holder-b", "scope-holder-b"); err != nil {
		t.Fatalf("activate holder B: %v", err)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-b"); got != 0 {
		t.Fatalf("waits for wait-b after B activation = %d, want 0", got)
	}
}

func TestNarrowedWaitAuditReopensSatisfiedLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)

	// Everything the planted wait names is already active: the audit must
	// reopen it within its bound even though no activation will ever fire.
	seedNarrowWaitScope(t, ctx, db, "scope-quiet", "gen-quiet", "active", at)
	seedNarrowRetryRow(t, ctx, db, "wait-quiet", "scope-quiet", "gen-quiet",
		"workload_materialization", "workload_materialization_resolution_not_ready", at)
	narrowed, ok := narrowedWaitingKey("scope-quiet", "gen-quiet", []string{"scope-quiet"})
	keyJSON := narrowed.JSON
	if !ok {
		t.Fatal("narrowedWaitingKey() = false, want a key for a non-empty blocking set")
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (
    work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at
) VALUES ('wait-quiet', 'relationship_generation_active', $1::jsonb,
    'scope-quiet', 'gen-quiet', 'workload_materialization',
    'workload_materialization_resolution_not_ready', $2, $2)`,
		keyJSON, at); err != nil {
		t.Fatalf("plant satisfied narrowed wait: %v", err)
	}
	reopened, reconciled, _, err := AuditWaitingItems(ctx, SQLDB{DB: db}, nil, time.Now().UTC(), DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("AuditWaitingItems() error = %v", err)
	}
	if reopened != 1 {
		t.Fatalf("audit reopened = %d, want 1: the satisfied narrowed wait must reopen", reopened)
	}
	if reconciled != 0 {
		t.Fatalf("audit reconciled = %d, want 0", reconciled)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-quiet"); got != 0 {
		t.Fatalf("waits for wait-quiet after audit = %d, want 0", got)
	}
}

func TestNarrowedWaitConcurrentConvergenceLive(t *testing.T) {
	ctx := context.Background()
	db, dsn := openNarrowFenceDB(t, ctx)
	_ = dsn
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: db})

	const workers = 4
	holderScopes := make([]string, workers)
	holderGens := make([]string, workers)
	seedNarrowWaitScope(t, ctx, db, "scope-waiter-race", "gen-waiter-race", "active", at)
	for i := 0; i < workers; i++ {
		holderScopes[i] = fmt.Sprintf("scope-race-%d", i)
		holderGens[i] = fmt.Sprintf("gen-race-%d", i)
		seedNarrowWaitScope(t, ctx, db, holderScopes[i], holderGens[i], "pending", at)
		seedNarrowRetryRow(t, ctx, db, fmt.Sprintf("wait-race-%d", i), "scope-waiter-race", "gen-waiter-race",
			"workload_materialization", "workload_materialization_resolution_not_ready", at)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2*workers)
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(2)
		// Parker: parks one narrowed wait naming its own holder.
		go func() {
			defer wg.Done()
			_, err := setAsideWaitingForCause(ctx, SQLDB{DB: db}, nil,
				narrowWaitIntent(fmt.Sprintf("wait-race-%d", i), "scope-waiter-race", "gen-waiter-race"),
				narrowWaitEntry(),
				narrowWaitTestCause{repos: []string{"repo-race"}, blocking: []string{holderScopes[i]}},
				nil, time.Now().UTC())
			if err != nil {
				errs <- fmt.Errorf("park %d: %w", i, err)
			}
		}()
		// Activator: commits the holder generation through the production
		// path, which wakes the narrowed waits in the same transaction.
		go func() {
			defer wg.Done()
			if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGeneration(ctx, holderGens[i], holderScopes[i]); err != nil {
				errs <- fmt.Errorf("activate %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent park/activate error: %v", err)
	}

	// Every holder activated, so every narrowed wait must be gone and every
	// queue row still retrying on its original attempt: exactly-once wake,
	// no lost wake beyond the backstop, no burned budget.
	_ = store
	for i := 0; i < workers; i++ {
		id := fmt.Sprintf("wait-race-%d", i)
		if got := countNarrowWaits(t, ctx, db, id); got != 0 {
			t.Fatalf("waits for %s after all activations = %d, want 0", id, got)
		}
		status, attempts, _ := narrowQueueState(t, ctx, db, id)
		if status != "retrying" || attempts != 1 {
			t.Fatalf("%s queue row = %s attempts %d, want retrying/1", id, status, attempts)
		}
	}
}

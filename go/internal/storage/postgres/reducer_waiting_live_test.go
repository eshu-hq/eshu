// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10876 waiting-wake proofs: a deferred intent parks with the 10-minute backstop instead of the backoff delay, past-maxAttempts Fail cycles never count and never dead-letter, both claim orders converge on byte-equal terminal state, and the audit reopens a planted satisfied wait within its bound; disposable database per test, about 30 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// openWaitingDB bootstraps a disposable database with the production schema
// (including migration 316) for one #10876 proof. It returns the handle and
// a DSN on the same database so concurrency proofs can open additional
// independent connections.
func openWaitingDB(t *testing.T, ctx context.Context) (*sql.DB, string) {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #10876 waiting proofs")
	}
	return openReducerFairnessDBWithSchema(t, ctx, dsn)
}

// seedWaitingScope creates an active scope with one active and one pending
// generation.
func seedWaitingScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, activeGen, pendingGen string, at time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scopeID, at, activeGen); err != nil {
		t.Fatalf("seed waiting scope %s: %v", scopeID, err)
	}
	addOwnGateGeneration(t, ctx, db, scopeID, activeGen, at, "active")
	addOwnGateGeneration(t, ctx, db, scopeID, pendingGen, at, "pending")
}

// waitingRowState reads the queue row's observable terminal shape.
type waitingRowState struct {
	status        string
	attempts      int
	failureClass  sql.NullString
	visibleAt     sql.NullTime
	waitRows      int
	waitKind      sql.NullString
	firstDeferred sql.NullTime
}

func readWaitingRow(t *testing.T, ctx context.Context, db *sql.DB, id string) waitingRowState {
	t.Helper()
	var out waitingRowState
	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, failure_class, visible_at FROM fact_work_items WHERE work_item_id = $1`, id).
		Scan(&out.status, &out.attempts, &out.failureClass, &out.visibleAt); err != nil {
		t.Fatalf("read waiting row %s: %v", id, err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT count(*), min(waits_for_kind), min(first_deferred_at) FROM reducer_waiting_items WHERE work_item_id = $1`, id).
		Scan(&out.waitRows, &out.waitKind, &out.firstDeferred); err != nil {
		t.Fatalf("read waiting waits %s: %v", id, err)
	}
	return out
}

// notYetActive is the generation-activation deferral the generation_active
// proofs park on.
func notYetActive(scope, gen string) error {
	return reducercontract.GenerationNotYetActiveError{ScopeID: scope, GenerationID: gen}
}

// claimWaitingRow claims one row through a real ReducerQueue.
func claimWaitingRow(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, domains ...reducercontract.Domain) (ReducerQueue, reducercontract.Intent) {
	t.Helper()
	queue := newOwnGateQueue(db, clock, domains...)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim waiting row: ok=%v err=%v", ok, err)
	}
	return queue, intent
}

// TestWaitingSetAsideParksWithBackstopLive: a generation-activation deferral
// parks with the 10-minute backstop instead of the backoff delay, keeps its
// attempt count, and records exactly one wait row. RED before #10876: no
// wait row exists and visible_at is the ~30s backoff.
func TestWaitingSetAsideParksWithBackstopLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	scopeID := "waiting-scope-park"
	addOwnGateGenerationSeed(t, ctx, base, scopeID, "g0", "g1", clock)
	addOwnGateItem(t, ctx, base, "wait-park-1", scopeID, "g1", reducer.DomainDeployableUnitCorrelation, clock)

	_, intent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainDeployableUnitCorrelation)
	queue := newOwnGateQueue(base, &clock, reducer.DomainDeployableUnitCorrelation)
	if err := queue.Fail(ctx, intent, notYetActive(scopeID, "g1")); err != nil {
		t.Fatalf("fail into wait: %v", err)
	}

	state := readWaitingRow(t, ctx, base, "wait-park-1")
	if state.status != "retrying" {
		t.Fatalf("parked status = %q, want retrying", state.status)
	}
	if !state.failureClass.Valid || state.failureClass.String != reducercontract.GenerationActivationNotReadyFailureClass {
		t.Fatalf("parked class = %v, want %q", state.failureClass, reducercontract.GenerationActivationNotReadyFailureClass)
	}
	if state.attempts != 1 {
		t.Fatalf("parked attempt_count = %d, want 1 (the wait never counts)", state.attempts)
	}
	if !state.visibleAt.Valid {
		t.Fatal("parked visible_at is NULL, want the 10-minute backstop")
	}
	backstopIn := state.visibleAt.Time.Sub(clock)
	if backstopIn < 9*time.Minute || backstopIn > 11*time.Minute {
		t.Fatalf("parked visible_at horizon = %v, want ~10m (the backstop, not the backoff)", backstopIn)
	}
	if state.waitRows != 1 {
		t.Fatalf("wait rows = %d, want 1", state.waitRows)
	}
	if !state.waitKind.Valid || state.waitKind.String != string(WaitingKindGenerationActive) {
		t.Fatalf("wait kind = %v, want generation_active", state.waitKind)
	}

	// Past the retry budget the wait still neither counts nor dead-letters.
	// Each cycle re-claims (visible_at is in the future, so force it past by
	// moving the test clock) and fails again.
	for i := 0; i < 4; i++ {
		clock = clock.Add(WaitingBackstopDelay + time.Minute)
		_, intent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainDeployableUnitCorrelation)
		queue := newOwnGateQueue(base, &clock, reducer.DomainDeployableUnitCorrelation)
		if err := queue.Fail(ctx, intent, notYetActive(scopeID, "g1")); err != nil {
			t.Fatalf("fail cycle %d into wait: %v", i, err)
		}
	}
	state = readWaitingRow(t, ctx, base, "wait-park-1")
	if state.status != "retrying" {
		t.Fatalf("after 5 fail cycles status = %q, want retrying (never dead-letters)", state.status)
	}
	if state.attempts != 1 {
		t.Fatalf("after 5 fail cycles attempt_count = %d, want 1", state.attempts)
	}
	if state.waitRows != 1 {
		t.Fatalf("after 5 fail cycles wait rows = %d, want 1 (idempotent re-park)", state.waitRows)
	}
}

// addOwnGateGenerationSeed seeds a scope with an active and a pending gen.
func addOwnGateGenerationSeed(t *testing.T, ctx context.Context, db *sql.DB, scopeID, activeGen, pendingGen string, at time.Time) {
	t.Helper()
	seedWaitingScope(t, ctx, db, scopeID, activeGen, pendingGen, at)
}

// seedProjectorRunningErr creates the running projector work row a projector
// Ack needs, mirroring the #7127 harness row.
func seedProjectorRunningErr(ctx context.Context, db *sql.DB, scopeID, generationID, leaseOwner string) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
        attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at)
        VALUES ($1, $2, $3, 'projector', 'source_local', 'running', 1, $4, now() + interval '10 minutes', now(), '{}'::jsonb, now(), now())
        ON CONFLICT (work_item_id) DO UPDATE SET status = 'running', lease_owner = EXCLUDED.lease_owner, attempt_count = 1`,
		projectorWorkItemID(scopeID, generationID), scopeID, generationID, leaseOwner); err != nil {
		return fmt.Errorf("seed projector running %s/%s: %w", scopeID, generationID, err)
	}
	return nil
}

// activatePendingGenErr runs a real projector Ack that activates the pending
// generation: the providing save of the generation_active wait kind. clock,
// when non-nil, freezes the Ack's stamps so a frozen-clock claim observes
// the wake at once; nil means real time. It returns errors instead of
// failing the test, so provider goroutines can report through a channel.
func activatePendingGenErr(ctx context.Context, db *sql.DB, scopeID, generationID string, clock *time.Time) error {
	seedProjectorRunningErr(ctx, db, scopeID, generationID, "waiting-proof-projector")
	queue := NewProjectorQueue(SQLDB{DB: db}, "waiting-proof-projector", 10*time.Minute)
	if clock != nil {
		at := *clock
		queue.Now = func() time.Time { return at }
	}
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{GenerationID: generationID},
		AttemptCount: 1,
	}
	if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
		return fmt.Errorf("projector ack %s/%s: %w", scopeID, generationID, err)
	}
	return nil
}

// activatePendingGen runs a real projector Ack that activates the pending
// generation, failing the test on error.
func activatePendingGen(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, clock *time.Time) {
	t.Helper()
	if err := activatePendingGenErr(ctx, db, scopeID, generationID, clock); err != nil {
		t.Fatal(err)
	}
}

// terminalRowImage returns the terminal row's comparable shape: everything
// but timestamps.
func terminalRowImage(t *testing.T, ctx context.Context, db *sql.DB, id string) string {
	t.Helper()
	var status, domain, class string
	var attempts int
	if err := db.QueryRowContext(ctx, `
SELECT status, domain, attempt_count, COALESCE(failure_class, '') FROM fact_work_items WHERE work_item_id = $1`, id).
		Scan(&status, &domain, &attempts, &class); err != nil {
		t.Fatalf("read terminal row %s: %v", id, err)
	}
	return status + "|" + domain + "|" + class + "|" + string(rune('0'+attempts))
}

// TestWaitingBothOrdersEquivalenceLive: waiter-first (claim, park, provide,
// wake, claim, succeed) and input-first (provide, claim, succeed) converge on
// the same terminal row. The wait never counts in either order.
func TestWaitingBothOrdersEquivalenceLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)

	runOrder := func(t *testing.T, name, scopeID string, waiterFirst bool) string {
		clock := time.Now().UTC().Truncate(time.Second)
		// Generation ids are global (scope_generations_pkey), so each order
		// gets its own pair.
		activeGen, pendingGen := name+"-g0", name+"-g1"
		seedWaitingScope(t, ctx, base, scopeID, activeGen, pendingGen, clock)
		addOwnGateItem(t, ctx, base, name, scopeID, pendingGen, reducer.DomainDeployableUnitCorrelation, clock)

		if !waiterFirst {
			activatePendingGen(t, ctx, base, scopeID, pendingGen, &clock)
		}
		_, intent := claimWaitingRow(t, ctx, base, &clock, reducer.DomainDeployableUnitCorrelation)
		if intent.IntentID != name {
			t.Fatalf("%s: claimed %q, want %q", name, intent.IntentID, name)
		}
		if waiterFirst {
			queue := newOwnGateQueue(base, &clock, reducer.DomainDeployableUnitCorrelation)
			if err := queue.Fail(ctx, intent, notYetActive(scopeID, pendingGen)); err != nil {
				t.Fatalf("%s: fail into wait: %v", name, err)
			}
			if got := readWaitingRow(t, ctx, base, name); got.waitRows != 1 {
				t.Fatalf("%s: wait rows = %d, want 1 before the provider commits", name, got.waitRows)
			}
			activatePendingGen(t, ctx, base, scopeID, pendingGen, &clock)
			// The wake reopens at once: the row is claimable now, not after
			// the 10-minute backstop.
			_, intent = claimWaitingRow(t, ctx, base, &clock, reducer.DomainDeployableUnitCorrelation)
			if intent.IntentID != name {
				t.Fatalf("%s: post-wake claimed %q, want %q", name, intent.IntentID, name)
			}
			if intent.AttemptCount != 1 {
				t.Fatalf("%s: post-wake attempt_count = %d, want 1", name, intent.AttemptCount)
			}
		}
		queue := newOwnGateQueue(base, &clock, reducer.DomainDeployableUnitCorrelation)
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			t.Fatalf("%s: ack: %v", name, err)
		}
		if got := readWaitingRow(t, ctx, base, name); got.waitRows != 0 {
			t.Fatalf("%s: wait rows = %d after success, want 0 (reconciled)", name, got.waitRows)
		}
		return terminalRowImage(t, ctx, base, name)
	}

	waiterFirst := runOrder(t, "wait-order-a", "waiting-scope-order-a", true)
	inputFirst := runOrder(t, "wait-order-b", "waiting-scope-order-b", false)
	if waiterFirst != inputFirst {
		t.Fatalf("terminal images differ:\nwaiter-first: %s\ninput-first:  %s", waiterFirst, inputFirst)
	}
	if !strings.HasPrefix(waiterFirst, "succeeded|") {
		t.Fatalf("terminal image = %q, want a succeeded row", waiterFirst)
	}
}

// TestWaitingAuditFindsMissedWakeLive: a wait whose provider already
// committed but whose wake never ran (raw-SQL plant, simulating a crash
// between provider commit and wake) is reopened by the audit, counted as a
// missed wake, and never double-starts a normally woken row.
func TestWaitingAuditFindsMissedWakeLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	scopeID := "waiting-scope-audit"
	seedWaitingScope(t, ctx, base, scopeID, "g0", "g1", clock)
	addOwnGateItem(t, ctx, base, "wait-audit-missed", scopeID, "g1", reducer.DomainDeployableUnitCorrelation, clock)
	addOwnGateItem(t, ctx, base, "wait-audit-clean", scopeID, "g1", reducer.DomainDeployableUnitCorrelation, clock)

	// The provider commits first (generation active), then both waits are
	// planted by raw SQL that bypasses every wake path.
	activatePendingGen(t, ctx, base, scopeID, "g1", &clock)
	now := clock
	plantWait := func(id string, firstSeen time.Time) {
		key := waitingScopeGenerationKey(scopeID, "g1")
		if _, err := base.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at)
VALUES ($1, 'generation_active', $2::jsonb, $3, 'g1', $4, $5, $6, $6)`,
			id, key, scopeID, string(reducer.DomainDeployableUnitCorrelation),
			reducercontract.GenerationActivationNotReadyFailureClass, firstSeen); err != nil {
			t.Fatalf("plant wait %s: %v", id, err)
		}
		// The queue row mirrors a parked Fail: retrying in the class with a
		// future visible_at.
		if _, err := base.ExecContext(ctx, `
UPDATE fact_work_items SET status = 'retrying', failure_class = $2, visible_at = $3, updated_at = $3
WHERE work_item_id = $1`, id, reducercontract.GenerationActivationNotReadyFailureClass, now.Add(WaitingBackstopDelay)); err != nil {
			t.Fatalf("park queue row %s: %v", id, err)
		}
	}
	// Missed long ago: older than two audit intervals at the test's short
	// interval, so the audit counts it as a wake it should have caught.
	plantWait("wait-audit-missed", now.Add(-time.Hour))
	// Satisfied just now: the audit still reopens it, but not as missed.
	plantWait("wait-audit-clean", now.Add(-time.Second))

	interval := 5 * time.Second
	reopened, reconciled, missed, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, now, interval, 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if reopened != 2 {
		t.Fatalf("audit reopened = %d, want 2", reopened)
	}
	if missed != 1 {
		t.Fatalf("audit missed = %d, want 1 (only the hour-old plant)", missed)
	}
	if reconciled != 0 {
		t.Fatalf("audit reconciled = %d, want 0", reconciled)
	}
	for _, id := range []string{"wait-audit-missed", "wait-audit-clean"} {
		if got := readWaitingRow(t, ctx, base, id); got.status != "retrying" || got.waitRows != 0 {
			t.Fatalf("%s after audit: status=%q waits=%d, want retrying/0", id, got.status, got.waitRows)
		}
	}
	// The wake preserves the non-counting class, so the next claim freezes
	// the attempt count instead of burning one.
	_, intent := claimWaitingRow(t, ctx, base, &now, reducer.DomainDeployableUnitCorrelation)
	if intent.AttemptCount != 0 {
		t.Fatalf("post-audit claim attempt_count = %d, want 0 (never tried)", intent.AttemptCount)
	}

	// The audit never double-starts: a second pass finds nothing to reopen.
	reopened, _, _, err = AuditWaitingItems(ctx, SQLDB{DB: base}, nil, now, interval, 100)
	if err != nil {
		t.Fatalf("second audit: %v", err)
	}
	if reopened != 0 {
		t.Fatalf("second audit reopened = %d, want 0", reopened)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// lease11081OldAckSQL is the pre-#10879 single-ack statement, byte-verbatim
// from the parent of commit c74a107c49 (git show
// c74a107c49~1:go/internal/storage/postgres/reducer_queue_ack.go). It
// terminalizes the main row without touching fact_work_item_leases, which is
// how a previous-build writer orphans a mirror row. The assertion below pins
// that it names no lease table: a statement that cleared the mirror would
// not reproduce the #11081 wedge.
const lease11081OldAckSQL = `UPDATE fact_work_items
SET status = 'succeeded', lease_owner = NULL, claim_until = NULL,
    provenance_edge_identity_upgrade_required = FALSE,
    visible_at = NULL, updated_at = $1,
    failure_class = NULL, failure_message = NULL, failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $4
`

// lease11081OldRetrySQL is the pre-#10879 single-retry statement from the
// same parent commit (reducer_queue.go). Same no-mirror property as the ack.
const lease11081OldRetrySQL = `UPDATE fact_work_items
SET status = 'retrying',
    cross_scope_replay_required = FALSE,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $5,
    next_attempt_at = $5,
    updated_at = $1,
    failure_class = $2,
    failure_message = $3,
    failure_details = $4
WHERE work_item_id = $6
  AND stage = 'reducer'
  AND lease_owner = $7
  AND status IN ('claimed', 'running')
  AND claim_until > clock_timestamp()
  AND last_attempt_at = $8
`

// plant11081StaleLeftover claims A through the new-build path (so its mirror
// row exists), finishes A with a previous-build statement that leaves the
// mirror behind, and seeds pending sibling B on the same conflict identity
// with the oldest updated_at so it is the deterministic next candidate. It
// returns B's work item id.
func plant11081StaleLeftover(
	t *testing.T,
	ctx context.Context,
	conn reducerClaimBenchmarkConn,
	schemaName, oldFinishSQL string,
	oldFinishArgs func(now time.Time, intentID, owner string, claimedAt time.Time) []any,
) (queue ReducerQueue, siblingB string) {
	t.Helper()
	if strings.Contains(oldFinishSQL, "fact_work_item_leases") {
		t.Fatal("old-build statement must not name the lease mirror")
	}
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	queue = leaseTestQueue(conn, "lease-11081-owner")
	intent := claimOneSupplyChainItem(t, ctx, queue)

	var scopeID, generationID, domain, conflictDomain string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT scope_id, generation_id, domain, conflict_domain
		 FROM fact_work_items WHERE work_item_id = $1`,
		intent.IntentID).Scan(&scopeID, &generationID, &domain, &conflictDomain); err != nil {
		t.Fatalf("read claimed quad: %v", err)
	}
	var claimedAt time.Time
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT last_attempt_at FROM fact_work_items WHERE work_item_id = $1`,
		intent.IntentID).Scan(&claimedAt); err != nil {
		t.Fatalf("read claimed-at: %v", err)
	}
	if _, err := conn.ExecContext(ctx, oldFinishSQL,
		oldFinishArgs(now, intent.IntentID, queue.LeaseOwner, claimedAt.UTC())...); err != nil {
		t.Fatalf("old-build finish: %v", err)
	}

	old := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	siblingB = "leftover-11081-B"
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES ($1, $2, $3, 'reducer', $4, $5,
	    NULL, 'pending', 1, NULL, NULL,
	    NULL, '{}'::jsonb, $6, $6)`,
		siblingB, scopeID, generationID, domain, conflictDomain, old); err != nil {
		t.Fatalf("insert leftover sibling: %v", err)
	}
	return queue, siblingB
}

// TestReducerQueue11081ClaimBatchClearsStaleLeftover is the #11081 AC1/AC5
// regression: after a previous-build writer terminalizes A without clearing
// its mirror row, one ClaimBatch call must hand out pending sibling B on the
// same conflict key instead of wedging on the orphaned lease row. RED on the
// pre-fix code: the whole batch statement aborts on the mirror unique
// violation and ClaimBatch returns empty.
func TestReducerQueue11081ClaimBatchClearsStaleLeftover(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})

	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	for _, intent := range intents {
		if intent.IntentID == siblingB {
			return
		}
	}
	t.Fatalf("ClaimBatch handed out %d intents without %q: stale lease row still blocks the key",
		len(intents), siblingB)
}

// TestReducerQueue11081ClaimBatchClearsStaleRetryLeftover is the retrying
// twin of the AC1 regression: A finished to retrying (still a live queue
// row, no mirror clear) must not block B either.
func TestReducerQueue11081ClaimBatchClearsStaleRetryLeftover(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldRetrySQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{
				now, "retryable", "leftover-retry", `{"retry":true}`,
				now, intentID, owner, claimedAt,
			}
		})

	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	for _, intent := range intents {
		if intent.IntentID == siblingB {
			return
		}
	}
	t.Fatalf("ClaimBatch handed out %d intents without %q: stale lease row still blocks the key",
		len(intents), siblingB)
}

// TestReducerQueue11081ClaimClearsStaleLeftover is the single-claim twin:
// the same orphaned row must not wedge ReducerQueue.Claim either.
func TestReducerQueue11081ClaimClearsStaleLeftover(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})

	got, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !claimed {
		t.Fatal("Claim returned no work, want the leftover sibling")
	}
	if got.IntentID != siblingB {
		t.Fatalf("Claim took %q, want %q", got.IntentID, siblingB)
	}
}

// TestReducerQueue11081HeartbeatClearsStaleSibling is the #11081 T2
// regression: B claimed main-only (a previous-build claim writes no
// mirror) on a key whose stale sibling lease row A still exists. The
// new-build heartbeat must recreate B's mirror row and clear A's leftover
// in the same statement instead of failing on the mirror unique violation.
// RED on the pre-fix code: heartbeat returns the raw 23505.
func TestReducerQueue11081HeartbeatClearsStaleSibling(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})
	_ = schemaName

	// Simulate a previous-build claim of B: main row only, no mirror.
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items
		SET status = 'claimed', lease_owner = $2, claim_until = $3,
		    last_attempt_at = $4, updated_at = $4
		WHERE work_item_id = $1`,
		siblingB, queue.LeaseOwner, now.Add(queue.LeaseDuration), now); err != nil {
		t.Fatalf("old-build claim of sibling: %v", err)
	}
	intent := reducercontract.Intent{IntentID: siblingB, ClaimedAt: &now}

	if err := queue.Heartbeat(ctx, intent); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	rows := liveLeaseRows(t, ctx, conn)
	if len(rows) != 1 || rows[siblingB] != queue.LeaseOwner {
		t.Fatalf("lease rows after heartbeat = %v, want only the sibling row", rows)
	}
}

// TestReducerQueue11081CleanupMigration is the #11081 T6 regression: the
// 326 cleanup migration deletes every stale-variant lease row (terminal,
// pending, expired, owner-mismatched, attempt-mismatched main) while
// preserving live triple-matching rows — including the version-skew case
// where a main-only heartbeat extended main.claim_until past a stale
// mirror.claim_until — and replays idempotently. It also live-proves the
// AC4 orphan gauge: the count reads 6 before the migration and 0 after.
// A NULL-liveness main (claimed with NULL owner, claim_until, and
// last_attempt_at) must lose its lease row like every other non-live shape.
// A live projector-stage pair (phase-1b reservation per 322) must survive
// the cleanup untouched: 326 touches reducer rows only.
func TestReducerQueue11081CleanupMigration(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	now := time.Now().UTC()
	if seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now); seeded < 8000 {
		t.Fatalf("seed = %d, want >= 8000", seeded)
	}
	var scopeID, generationID, domain, conflictDomain string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT scope_id, generation_id, domain, conflict_domain
		 FROM fact_work_items LIMIT 1`,
	).Scan(&scopeID, &generationID, &domain, &conflictDomain); err != nil {
		t.Fatalf("read seed identity: %v", err)
	}
	liveUntil := now.Add(time.Hour).UTC().Truncate(time.Microsecond)
	past := now.Add(-time.Hour).UTC().Truncate(time.Microsecond)
	stamp := now.Truncate(time.Microsecond)
	otherStamp := now.Add(-time.Minute).UTC().Truncate(time.Microsecond)
	type planted struct {
		id, status, owner       string
		claimUntil, lastAttempt time.Time
	}
	mains := []planted{
		{"m-live", "claimed", "owner-O", liveUntil, stamp},
		{"m-skew", "claimed", "owner-O", liveUntil, stamp},
		{"m-terminal", "succeeded", "owner-O", liveUntil, stamp},
		{"m-pending", "pending", "owner-O", liveUntil, stamp},
		{"m-expired", "claimed", "owner-O", past, stamp},
		{"m-owner", "claimed", "owner-O2", liveUntil, stamp},
		{"m-attempt", "claimed", "owner-O", liveUntil, stamp},
	}
	for i, m := range mains {
		var owner, claimUntil any
		owner, claimUntil = m.owner, m.claimUntil
		if m.status == "succeeded" || m.status == "pending" {
			owner, claimUntil = nil, nil
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
		    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
		    conflict_key, status, attempt_count, lease_owner, claim_until,
		    last_attempt_at, payload, created_at, updated_at
		) VALUES ($1, $2, $3, 'reducer', $4, $5, $6, $7, 1, $8, $9, $10, '{}'::jsonb, $11, $11)`,
			m.id, scopeID, generationID, domain, conflictDomain,
			m.id, m.status, owner, claimUntil, m.lastAttempt, now); err != nil {
			t.Fatalf("insert main %s: %v", m.id, err)
		}
		leaseClaimUntil := m.claimUntil
		leaseOwner, leaseAttempt := m.owner, m.lastAttempt
		if i == 1 { // version skew: main heartbeat-extended, mirror stale
			leaseClaimUntil = past
		}
		if i == 5 {
			leaseOwner = "owner-O"
		}
		if i == 6 {
			leaseAttempt = otherStamp
		}
		var leaseUntil any = leaseClaimUntil
		if m.status == "succeeded" || m.status == "pending" {
			leaseUntil = past
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_item_leases (
		    work_item_id, stage, scope_id, conflict_domain, conflict_key,
		    lease_owner, claim_until, last_attempt_at
		) VALUES ($1, 'reducer', $2, $3, $4, $5, $6, $7)`,
			m.id, scopeID, conflictDomain, m.id,
			leaseOwner, leaseUntil, leaseAttempt); err != nil {
			t.Fatalf("insert lease %s: %v", m.id, err)
		}
	}

	// NULL-liveness main: a claimed row whose owner, claim_until, and
	// last_attempt_at are all NULL is not live. The claim CTEs and the orphan
	// gauge read it through NOT EXISTS, so the cleanup must delete its lease
	// row too; a NOT (a AND b ...) form evaluates NULL there and keeps it.
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES ($1, $2, $3, 'reducer', $4, $5, $1, 'claimed', 1, NULL, NULL, NULL,
	    '{}'::jsonb, $6, $6)`,
		"m-null", scopeID, generationID, domain, conflictDomain, now); err != nil {
		t.Fatalf("insert NULL-liveness main: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_item_leases (
	    work_item_id, stage, scope_id, conflict_domain, conflict_key,
	    lease_owner, claim_until, last_attempt_at
	) VALUES ($1, 'reducer', $2, $3, $1, 'owner-O', $4, $5)`,
		"m-null", scopeID, conflictDomain, liveUntil, stamp); err != nil {
		t.Fatalf("insert NULL-liveness lease: %v", err)
	}

	// Live projector-stage pair: reserved for phase-1b per 322, never a
	// reducer leftover. The 326 cleanup must leave both rows untouched.
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
	    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
	    conflict_key, status, attempt_count, lease_owner, claim_until,
	    last_attempt_at, payload, created_at, updated_at
	) VALUES ($1, $2, $3, 'projector', $4, 'projector-probe', $5, 'claimed', 1,
	    'owner-P', $6, $7, '{}'::jsonb, $8, $8)`,
		"m-projector", scopeID, generationID, domain, "m-projector",
		liveUntil, stamp, now); err != nil {
		t.Fatalf("insert projector main: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_item_leases (
	    work_item_id, stage, scope_id, conflict_domain, conflict_key,
	    lease_owner, claim_until, last_attempt_at
	) VALUES ($1, 'projector', $2, 'projector-probe', $3, 'owner-P', $4, $5)`,
		"m-projector", scopeID, "m-projector", liveUntil, stamp); err != nil {
		t.Fatalf("insert projector lease: %v", err)
	}

	observer := NewQueueObserverStore(conn)
	if got, err := observer.ReducerLeaseOrphanRows(ctx); err != nil {
		t.Fatalf("orphan rows before: %v", err)
	} else if got != 6 {
		t.Fatalf("orphan rows before = %d, want 6", got)
	}

	cleanup := MigrationSQL("fact_work_item_leases_11081_cleanup")
	if _, err := conn.ExecContext(ctx, cleanup); err != nil {
		t.Fatalf("apply 326 cleanup: %v", err)
	}
	rows := liveLeaseRows(t, ctx, conn)
	if len(rows) != 3 || rows["m-live"] != "owner-O" || rows["m-skew"] != "owner-O" ||
		rows["m-projector"] != "owner-P" {
		t.Fatalf("lease rows after 326 = %v, want m-live, m-skew, and the projector pair", rows)
	}
	var projectorOwner any
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT lease_owner FROM fact_work_items WHERE work_item_id = 'm-projector'`,
	).Scan(&projectorOwner); err != nil {
		t.Fatalf("read projector main after 326: %v", err)
	}
	if projectorOwner != "owner-P" {
		t.Fatalf("projector main owner after 326 = %v, want owner-P", projectorOwner)
	}
	if got, err := observer.ReducerLeaseOrphanRows(ctx); err != nil {
		t.Fatalf("orphan rows after: %v", err)
	} else if got != 0 {
		t.Fatalf("orphan rows after = %d, want 0", got)
	}
	// Idempotent replay: a second application changes nothing.
	if _, err := conn.ExecContext(ctx, cleanup); err != nil {
		t.Fatalf("replay 326 cleanup: %v", err)
	}
	if rows := liveLeaseRows(t, ctx, conn); len(rows) != 3 {
		t.Fatalf("lease rows after replay = %v, want 3", rows)
	}
}

// conflict11081StubDB fails every claim statement with the given Postgres
// error, proving the counter/log path without a live wedge.
type conflict11081StubDB struct{ err error }

func (s conflict11081StubDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func (s conflict11081StubDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, s.err
}

// liveLeaseConflictCounterSum collects the 11081 conflict counter total for
// one claim_mode/constraint label pair from a manual reader.
func liveLeaseConflictCounterSum(
	t *testing.T,
	ctx context.Context,
	reader *sdkmetric.ManualReader,
	claimMode, constraint string,
) int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok || metric.Name != "eshu_dp_reducer_claim_live_lease_conflicts_total" {
				continue
			}
			for _, dp := range sum.DataPoints {
				mode, _ := dp.Attributes.Value("claim_mode")
				cstr, _ := dp.Attributes.Value("constraint")
				if mode.AsString() == claimMode && cstr.AsString() == constraint {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// TestReducerQueue11081LiveLeaseConflictIsObserved is the #11081 AC2
// regression: a claim deferred by a live lease conflict must count the new
// counter (with closed claim_mode/constraint labels) and log one Warn line,
// for both the batch and single paths. RED on the pre-fix code: the defer
// returns are silent.
func TestReducerQueue11081LiveLeaseConflictIsObserved(t *testing.T) {
	ctx := context.Background()
	mirrorErr := &pgconn.PgError{Code: "23505", ConstraintName: "fact_work_item_leases_live_lease_uniq"}

	for _, tc := range []struct {
		name      string
		claimMode string
		claim     func(q ReducerQueue) error
	}{
		{"batch", "batch", func(q ReducerQueue) error {
			_, err := q.ClaimBatch(ctx, 8)
			return err
		}},
		{"single", "single", func(q ReducerQueue) error {
			_, _, err := q.Claim(ctx)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(
				sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("live-lease-conflict-proof"))
			require.NoError(t, err)
			require.NotNil(t, instruments.ReducerClaimLiveLeaseConflicts,
				"ReducerClaimLiveLeaseConflicts counter should be registered")
			var logs bytes.Buffer
			queue := ReducerQueue{
				database:      conflict11081StubDB{err: mirrorErr},
				LeaseOwner:    "conflict-proof-owner",
				LeaseDuration: time.Minute,
				Instruments:   instruments,
				Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
			}
			if err := tc.claim(queue); err != nil {
				t.Fatalf("deferred claim err = %v, want nil deferral", err)
			}
			if got := liveLeaseConflictCounterSum(t, ctx, reader, tc.claimMode, "mirror"); got != 1 {
				t.Fatalf("conflict counter = %d, want 1", got)
			}
			if !strings.Contains(logs.String(), "reducer claim deferred by live lease conflict") {
				t.Fatalf("missing defer Warn line, got: %q", logs.String())
			}
		})
	}
}

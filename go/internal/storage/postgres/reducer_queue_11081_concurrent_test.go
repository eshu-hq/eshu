// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// secondLeaseTestConn opens an independent connection pinned to the same
// disposable lease-test schema. The shared test connection is a single
// *sql.Conn, which serializes at the driver; a true two-claimant race
// needs a second session so both claim statements can be in flight (and
// block on each other's row locks) at once.
func secondLeaseTestConn(
	t *testing.T,
	ctx context.Context,
	schemaName string,
) reducerClaimBenchmarkConn {
	t.Helper()
	database, err := sql.Open("pgx", reducerClaimBenchmarkDSN())
	if err != nil {
		t.Fatalf("open second postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open second dedicated connection: %v", err)
	}
	t.Cleanup(func() { _ = sqlConn.Close() })
	conn := reducerClaimBenchmarkConn{conn: sqlConn}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set second search_path: %v", err)
	}
	return conn
}

// TestReducerQueue11081ConcurrentClaimsRaceStaleLeftover is the #11081
// concurrency proof: two ClaimBatch calls with distinct owners race on a
// key carrying one stale leftover lease row. Exactly one must hand out
// sibling B, neither may surface an error (no 23505/40P01 escape — the
// loser defers to empty or claims other keys), and the final state must
// hold exactly one lease row for B, owned by the winner, with A's
// leftover gone. Lock order stays main-then-lease in both statements, so
// the race resolves by row lock + the main unique fence, never deadlock.
func TestReducerQueue11081ConcurrentClaimsRaceStaleLeftover(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	_, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})

	conn2 := secondLeaseTestConn(t, ctx, schemaName)
	queues := []ReducerQueue{
		leaseTestQueue(conn, "lease-11081-racer-1"),
		leaseTestQueue(conn2, "lease-11081-racer-2"),
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([][]reducercontract.Intent, len(queues))
	errs := make([]error, len(queues))
	for i, queue := range queues {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = queue.ClaimBatch(ctx, 8)
		}()
	}
	close(start)
	wg.Wait()

	winner := ""
	handouts := 0
	for i := range queues {
		if errs[i] != nil {
			t.Fatalf("racer %d ClaimBatch escaped an error: %v", i, errs[i])
		}
		for _, intent := range results[i] {
			if intent.IntentID == siblingB {
				handouts++
				winner = queues[i].LeaseOwner
			}
		}
	}
	if handouts != 1 {
		t.Fatalf("sibling %q handed out %d times, want exactly 1", siblingB, handouts)
	}

	rows := liveLeaseRows(t, ctx, conn)
	if rows[siblingB] != winner {
		t.Fatalf("lease rows = %v, want sibling row owned by winner %q", rows, winner)
	}
	var mainOwner, mainStatus string
	if err := conn.conn.QueryRowContext(ctx,
		`SELECT lease_owner, status FROM fact_work_items WHERE work_item_id = $1`,
		siblingB).Scan(&mainOwner, &mainStatus); err != nil {
		t.Fatalf("read sibling main row: %v", err)
	}
	if mainOwner != winner || (mainStatus != "claimed" && mainStatus != "running") {
		t.Fatalf("sibling main = owner %q status %q, want winner %q claimed/running",
			mainOwner, mainStatus, winner)
	}
	// The stale leftover row for A must be gone: no lease row may remain
	// whose main row is not claimed/running under the same owner.
	if got, err := NewQueueObserverStore(conn).ReducerLeaseOrphanRows(ctx); err != nil {
		t.Fatalf("orphan rows after race: %v", err)
	} else if got != 0 {
		t.Fatalf("orphan lease rows after race = %d, want 0 (rows: %v)", got, rows)
	}
}

// TestReducerQueue11081ClaimBatchClearsExplicitKeyLeftover is the
// explicit-conflict-key twin of the AC1 regression: the seed plants use
// conflict_key=NULL (scope identity via COALESCE). Here both the leftover
// lease row and sibling B carry the same non-NULL conflict_key, so the
// heal must match on the explicit-key arm of the identity and still hand
// out B in one claim.
func TestReducerQueue11081ClaimBatchClearsExplicitKeyLeftover(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})

	const explicitKey = "explicit-11081-key"
	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_item_leases SET conflict_key = $1`, explicitKey); err != nil {
		t.Fatalf("stamp explicit key on leftover lease: %v", err)
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE fact_work_items SET conflict_key = $1 WHERE work_item_id = $2`,
		explicitKey, siblingB); err != nil {
		t.Fatalf("stamp explicit key on sibling: %v", err)
	}

	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	for _, intent := range intents {
		if intent.IntentID == siblingB {
			if got := liveLeaseRows(t, ctx, conn)[siblingB]; got != queue.LeaseOwner {
				t.Fatalf("sibling lease owner = %q, want %q", got, queue.LeaseOwner)
			}
			return
		}
	}
	t.Fatalf("ClaimBatch handed out %d intents without %q: explicit-key leftover still blocks",
		len(intents), siblingB)
}

// rowsErr11081Rows is an empty result whose iteration ends in err, proving
// the rows.Err deferral branch of a claim.
type rowsErr11081Rows struct{ err error }

func (r rowsErr11081Rows) Next() bool        { return false }
func (r rowsErr11081Rows) Scan(...any) error { return r.err }
func (r rowsErr11081Rows) Err() error        { return r.err }
func (r rowsErr11081Rows) Close() error      { return nil }

// rowsErr11081StubDB returns a fresh rowsErr11081Rows per query.
type rowsErr11081StubDB struct{ err error }

func (s rowsErr11081StubDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return rowsErr11081Rows(s), nil
}

func (s rowsErr11081StubDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, s.err
}

// TestReducerQueue11081RowsErrConflictIsObserved covers the two AC2
// branches the QueryContext-error stub cannot reach: a live-lease
// conflict surfacing through rows.Err after a clean query start, and the
// constraint=main label (genuine contention on the main-table fence
// rather than mirror residue). The batch claim must defer to empty with
// a nil error, count once, and log once.
func TestReducerQueue11081RowsErrConflictIsObserved(t *testing.T) {
	ctx := context.Background()
	mainErr := &pgconn.PgError{
		Code:           "23505",
		ConstraintName: reducerLiveLeaseUniqueConstraint,
	}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("rows-err-conflict-proof"))
	require.NoError(t, err)
	var logs bytes.Buffer
	queue := ReducerQueue{
		database:      rowsErr11081StubDB{err: mainErr},
		LeaseOwner:    "rows-err-proof-owner",
		LeaseDuration: time.Minute,
		Instruments:   instruments,
		Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	intents, err := queue.ClaimBatch(ctx, 8)
	require.NoError(t, err, "rows.Err conflict must defer, not fail")
	require.Empty(t, intents)
	if got := liveLeaseConflictCounterSum(t, ctx, reader, "batch", "main"); got != 1 {
		t.Fatalf("conflict counter (batch, main) = %d, want 1", got)
	}
	if !strings.Contains(logs.String(), "reducer claim deferred by live lease conflict") {
		t.Fatalf("missing defer Warn line, got: %q", logs.String())
	}
}

// TestReducerQueue11081ClaimExplainUsesLeaseIndex is the claim-path
// performance guard for the heal: on the 8000-row lease-test schema the
// batch claim plan must probe the lease mirror through its live-lease
// unique index and never sequentially scan fact_work_item_leases, so the
// per-claim stale-leases clear stays an index probe per claimed key
// rather than a table scan whose cost grows with the mirror.
func TestReducerQueue11081ClaimExplainUsesLeaseIndex(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, _ := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})
	// Scale the mirror so the planner faces the production choice: with a
	// one-row table a sequential scan is legitimately cheapest and proves
	// nothing about the probe. 4000 rows on distinct explicit keys (one
	// per seeded main, FK-safe) make the unique-index probe the plan a
	// repo-scale mirror gets.
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_item_leases (
	    work_item_id, stage, scope_id, conflict_domain, conflict_key,
	    lease_owner, claim_until, last_attempt_at
	)
	SELECT work_item_id, 'reducer', scope_id, conflict_domain, work_item_id,
	    'bulk-plan-owner', $1, $1
	FROM fact_work_items
	WHERE stage = 'reducer'
	  AND work_item_id NOT IN (SELECT work_item_id FROM fact_work_item_leases)
	LIMIT 4000`, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("bulk-seed lease mirror: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `ANALYZE fact_work_items`); err != nil {
		t.Fatalf("analyze fact_work_items: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `ANALYZE fact_work_item_leases`); err != nil {
		t.Fatalf("analyze fact_work_item_leases: %v", err)
	}

	now := time.Now().UTC()
	rows, err := conn.QueryContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+claimReducerWorkBatchQuery,
		now,
		queue.claimDomainFilters(),
		queue.LeaseOwner,
		now.Add(queue.LeaseDuration),
		false,
		queue.ExpectedSourceLocalProjectors,
		queue.semanticEntityClaimLimit(),
		8,
	)
	if err != nil {
		t.Fatalf("explain batch claim: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	text := plan.String()
	t.Logf("batch claim plan:\n%s", text)
	if strings.Contains(text, "Seq Scan on fact_work_item_leases") {
		t.Fatal("batch claim plan sequentially scans fact_work_item_leases")
	}
	if !strings.Contains(text, reducerLeaseMirrorUniqueConstraint) {
		t.Fatalf("batch claim plan never probes %s", reducerLeaseMirrorUniqueConstraint)
	}
}

// TestReducerQueue11081HeartbeatExplainUsesIndexes is the heartbeat-path
// performance guard for the heal. Heartbeat renews run about twice per claim,
// and the chained stale_leases_cleared CTE adds a join plus a DELETE probe to
// every renewal. On the 8000-row lease-test schema with a 4000-row mirror the
// plan must find the sibling lease rows through the live-lease unique index
// and look up the holder and work rows by primary key, never a sequential
// scan of either table, so the added work stays a few index probes per
// renewal.
func TestReducerQueue11081HeartbeatExplainUsesIndexes(t *testing.T) {
	ctx := context.Background()
	conn, schemaName := setupLeaseTestSchema(t, ctx)
	queue, siblingB := plant11081StaleLeftover(t, ctx, conn, schemaName,
		lease11081OldAckSQL,
		func(now time.Time, intentID, owner string, claimedAt time.Time) []any {
			return []any{now, intentID, owner, claimedAt}
		})
	if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_item_leases (
	    work_item_id, stage, scope_id, conflict_domain, conflict_key,
	    lease_owner, claim_until, last_attempt_at
	)
	SELECT work_item_id, 'reducer', scope_id, conflict_domain, work_item_id,
	    'bulk-plan-owner', $1, $1
	FROM fact_work_items
	WHERE stage = 'reducer'
	  AND work_item_id NOT IN (SELECT work_item_id FROM fact_work_item_leases)
	LIMIT 4000`, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatalf("bulk-seed lease mirror: %v", err)
	}
	// Sibling B is claimed by the previous build (main row only), so the
	// heartbeat takes its mirror-recreate path with the stale clear in front.
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items
		SET status = 'claimed', lease_owner = $2, claim_until = $3,
		    last_attempt_at = $4, updated_at = $4
		WHERE work_item_id = $1`,
		siblingB, queue.LeaseOwner, now.Add(queue.LeaseDuration), now); err != nil {
		t.Fatalf("old-build claim of sibling: %v", err)
	}
	for _, table := range []string{"fact_work_items", "fact_work_item_leases"} {
		if _, err := conn.ExecContext(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}

	rows, err := conn.QueryContext(ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+heartbeatReducerWorkQuery,
		now.Add(queue.LeaseDuration), now, siblingB, queue.LeaseOwner, now,
	)
	if err != nil {
		t.Fatalf("explain heartbeat: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate plan: %v", err)
	}
	text := plan.String()
	t.Logf("heartbeat plan:\n%s", text)
	for _, table := range []string{"fact_work_items", "fact_work_item_leases"} {
		if strings.Contains(text, "Seq Scan on "+table) {
			t.Fatalf("heartbeat plan sequentially scans %s", table)
		}
	}
	if !strings.Contains(text, reducerLeaseMirrorUniqueConstraint) {
		t.Fatalf("heartbeat plan never probes %s", reducerLeaseMirrorUniqueConstraint)
	}
}

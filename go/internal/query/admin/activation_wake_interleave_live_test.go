// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7597 200-iteration interleave of the claim-fenced activation wake, a concurrent final Fail, the public admin replay (retried on 40P01) and a claim-time supersede sweep on independent connections: zero 40P01 on the activation, zero lost wakes, every consumer reopened exactly once, attributed by writer pid through a test-only audit trigger (#9320: both the wake and the admin replay stamp reopened_at); plus a sequential admin replay after a committed wake that must be a no-op; about 6 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestActivationWakeInterleaveLive races, on independent connections and for
// 200 iterations, the four writers that can touch an own-gate consumer row
// while its producer activates the relationship generation (#7597):
//
//   - A: the claim-fenced activation (ActivateResolutionGenerationForClaim),
//     which wakes the generation's dead-lettered own-gate consumers;
//   - B: a consumer's final counted Fail in the producer-terminal class;
//   - C: the public admin replay (store.ReplayFailedWorkItems) of another
//     dead-lettered consumer, retried on 40P01 with exact accounting;
//   - D: a consumer-domain claim, whose claim-time sweep supersedes the
//     scope's older-generation row.
//
// It requires zero 40P01 on the activation (the wake) and zero lost wakes:
// once A commits, no consumer of the generation is left dead-lettered, and
// each one is reopened exactly once (the admin replay reported it, or the
// wake did; never both, never neither). Since #9320 both writers stamp
// reopened_at, so the stamp no longer says which writer reopened a row. A
// test-only trigger in the isolated schema records every move into pending
// with the backend pid of the writer, and each actor owns one connection, so
// the oracle counts reopens per row and attributes each one by pid, in both
// directions: a wake after the admin replay and an admin replay after the
// wake are each a second reopen. The wake has two sites: the activation (A)
// and B's own wakeAfterProducerTerminalDeadLetter, which reopens the row B
// just dead-lettered when A committed first.
func TestActivationWakeInterleaveLive(t *testing.T) {
	ctx, base, conns, pids := openInterleaveHarness(t)

	const iterations = 200
	var total interleaveOutcome
	for i := 0; i < iterations; i++ {
		total.add(runInterleaveIteration(t, ctx, base, conns, pids, i))
	}
	t.Logf("%d iterations: 40P01 activation=%d fail=%d admin=%d claim=%d; admin 40P01 retries=%d; "+
		"lost wakes=%d; consumers reopened by the wake=%d, by the admin replay=%d",
		iterations, total.deadlock[0], total.deadlock[1], total.deadlock[2], total.deadlock[3],
		total.adminRetries, total.lost, total.byWake, total.byAdmin)
	if total.deadlock[0] != 0 {
		t.Fatalf("the activation (the wake) hit %d deadlocks", total.deadlock[0])
	}
	if total.lost != 0 {
		t.Fatalf("%d lost wakes", total.lost)
	}
}

// TestAdminReplayAfterCommittedWakeIsANoOpLive pins the ordering the race
// above cannot reach (#9320): there the admin replay runs once, concurrently,
// and skips a row the wake holds (FOR UPDATE SKIP LOCKED), so it never sees a
// row the wake already committed. Here the wake commits first; the admin
// replay must then find nothing, and the pid audit must show one reopen, by
// the wake. Admitting 'pending' to the replay's status guard turns this red.
func TestAdminReplayAfterCommittedWakeIsANoOpLive(t *testing.T) {
	ctx, base, conns, pids := openInterleaveHarness(t)
	scope, gen := "scope-seq", "gen-seq"
	dmID, consumer := scope+"-dm", scope+"-wm"
	seedInterleaveScope(t, ctx, base, scope, gen, gen+"-old", []interleaveRow{
		{dmID, gen, string(reducer.DomainDeploymentMapping), "pending", "", 0},
		{consumer, gen, string(reducer.DomainWorkloadMaterialization), "dead_letter", crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass, 3},
	})
	dm := interleaveClaim(t, ctx, interleaveQueue(base, reducer.DomainDeploymentMapping), dmID)
	if err := postgres.NewRelationshipStore(postgres.SQLDB{DB: conns[0]}).
		ActivateResolutionGenerationForClaim(ctx, gen, scope, dm.IntentID, *dm.ClaimedAt); err != nil {
		t.Fatalf("activation: %v", err)
	}
	replayed, err := store.NewStore(conns[2]).ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
		WorkItemIDs: []string{consumer}, Stage: "reducer", OperatorNote: "after wake #9320", Limit: 10,
	})
	if err != nil {
		t.Fatalf("admin replay: %v", err)
	}
	wake, adminReopens, other := reopenWriters(t, ctx, base, consumer, pids)
	if len(replayed) != 0 || wake != 1 || adminReopens != 0 || other != 0 {
		t.Fatalf("admin replay after a committed wake replayed %d rows; reopens by wake=%d admin=%d other=%d; "+
			"want no replay and one reopen, by the wake; audit %s", len(replayed), wake, adminReopens, other,
			reopenAuditTrail(t, ctx, base, consumer))
	}
}

// openInterleaveHarness bootstraps an isolated schema with the reopen audit
// and opens one single-connection handle per actor.
func openInterleaveHarness(t *testing.T) (context.Context, *sql.DB, []*sql.DB, []int) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_REDUCER_FAIRNESS_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 interleave proof")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("wake_interleave_%d", time.Now().UnixNano())
	base := interleaveConn(t, ctx, dsn, schema, true)
	if err := postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: base}); err != nil {
		t.Fatalf("bootstrap schema: %v", err)
	}
	conns := make([]*sql.DB, 4)
	for i := range conns {
		conns[i] = interleaveConn(t, ctx, dsn, schema, false)
	}
	installReopenAudit(t, ctx, base)
	return ctx, base, conns, interleavePIDs(t, ctx, conns)
}

// interleaveOutcome counts one iteration's (or the run's) results.
type interleaveOutcome struct {
	deadlock     [4]int
	adminRetries int
	lost         int
	byWake       int
	byAdmin      int
}

func (o *interleaveOutcome) add(other interleaveOutcome) {
	for i := range o.deadlock {
		o.deadlock[i] += other.deadlock[i]
	}
	o.adminRetries += other.adminRetries
	o.lost += other.lost
	o.byWake += other.byWake
	o.byAdmin += other.byAdmin
}

// interleaveRow is one seeded fact_work_items row.
type interleaveRow struct {
	id, gen, domain, status, class string
	attempts                       int
}

func seedInterleaveScope(t *testing.T, ctx context.Context, db *sql.DB, scope, gen, oldGen string, rows []interleaveRow) {
	t.Helper()
	at := time.Now().UTC().Add(-time.Hour)
	interleaveExec(t, ctx, db, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
 collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
 VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`, scope, at, gen)
	for g, status := range map[string]string{gen: "active", oldGen: "superseded"} {
		interleaveExec(t, ctx, db, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint,
 observed_at, ingested_at, status, activated_at, superseded_at, payload)
 VALUES ($1, $2, 'snapshot', 'x', $3, $3, $4, $3, NULL, '{}'::jsonb)`, g, scope, at, status)
	}
	for _, row := range rows {
		interleaveExec(t, ctx, db, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain,
 conflict_domain, conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at)
 VALUES ($1, $2, $3, 'reducer', $4, 'scope', $1, $5, $6, NULLIF($7, ''), jsonb_build_object('entity_key', $1::text), $8, $8)`,
			row.id, scope, row.gen, row.domain, row.status, row.attempts, row.class, at)
	}
}

func interleaveQueue(conn *sql.DB, domain reducercontract.Domain) postgres.ReducerQueue {
	q := postgres.NewReducerQueue(postgres.SQLDB{DB: conn}, "interleave-7597", 5*time.Minute)
	q.ClaimDomains = []reducercontract.Domain{domain}
	q.MaxAttempts = 3
	q.RetryDelay = time.Second
	return q
}

func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func runInterleaveIteration(t *testing.T, ctx context.Context, base *sql.DB, conns []*sql.DB, pids []int, i int) interleaveOutcome {
	t.Helper()
	scope, gen, oldGen := fmt.Sprintf("scope-%d", i), fmt.Sprintf("gen-%d", i), fmt.Sprintf("gen-%d-old", i)
	dmID, c1, c2, c3 := scope+"-dm", scope+"-duc", scope+"-wm", scope+"-duc-old"
	seedInterleaveScope(t, ctx, base, scope, gen, oldGen, []interleaveRow{
		{dmID, gen, string(reducer.DomainDeploymentMapping), "pending", "", 0},
		{c1, gen, string(reducer.DomainDeployableUnitCorrelation), "pending", "", 2},
		{c2, gen, string(reducer.DomainWorkloadMaterialization), "dead_letter", crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass, 3},
		{c3, oldGen, string(reducer.DomainDeployableUnitCorrelation), "pending", "", 0},
	})
	dm := interleaveClaim(t, ctx, interleaveQueue(base, reducer.DomainDeploymentMapping), dmID)
	consumer := interleaveClaim(t, ctx, interleaveQueue(base, reducer.DomainDeployableUnitCorrelation), c1)
	deadErr := crossrepo.EvaluateOwnGate(ctx, func(string) (bool, error) { return false, nil },
		func(context.Context, string, string) (crossrepo.ProducerState, error) {
			return crossrepo.ProducerState{ScopeActiveGenerationID: gen, WorkItemID: dmID, Status: "dead_letter"}, nil
		}, reducercontract.Intent{ScopeID: scope, GenerationID: gen, Domain: reducer.DomainDeployableUnitCorrelation}, true, time.Now()).Err

	var (
		out      interleaveOutcome
		mu       sync.Mutex
		wg       sync.WaitGroup
		start    = make(chan struct{})
		actErr   error
		replayed []admin.WorkItem
	)
	note := func(actor int, err error) {
		if isDeadlock(err) {
			mu.Lock()
			out.deadlock[actor]++
			mu.Unlock()
		}
	}
	wg.Add(4)
	go func() {
		defer wg.Done()
		<-start
		actErr = postgres.NewRelationshipStore(postgres.SQLDB{DB: conns[0]}).
			ActivateResolutionGenerationForClaim(ctx, gen, scope, dm.IntentID, *dm.ClaimedAt)
		note(0, actErr)
	}()
	go func() {
		defer wg.Done()
		<-start
		err := interleaveQueue(conns[1], reducer.DomainDeployableUnitCorrelation).Fail(ctx, consumer, deadErr)
		note(1, err)
		if err != nil {
			t.Errorf("iteration %d: final Fail: %v", i, err)
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for {
			items, err := store.NewStore(conns[2]).ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
				WorkItemIDs: []string{c2}, Stage: "reducer", OperatorNote: "interleave #7597", Limit: 10,
			})
			note(2, err)
			if isDeadlock(err) {
				mu.Lock()
				out.adminRetries++
				mu.Unlock()
				continue
			}
			if err != nil {
				t.Errorf("iteration %d: admin replay: %v", i, err)
			}
			replayed = items
			return
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, err := interleaveQueue(conns[3], reducer.DomainDeployableUnitCorrelation).Claim(ctx)
		note(3, err)
		if err != nil {
			t.Errorf("iteration %d: claim sweep: %v", i, err)
		}
	}()
	close(start)
	wg.Wait()
	if actErr != nil {
		t.Fatalf("iteration %d: activation: %v", i, actErr)
	}

	byAdmin := false
	for _, item := range replayed {
		byAdmin = byAdmin || item.WorkItemID == c2
	}
	for _, id := range []string{c1, c2} {
		var status string
		var reopened sql.NullTime
		if err := base.QueryRowContext(ctx, `SELECT status, reopened_at FROM fact_work_items WHERE work_item_id = $1`, id).
			Scan(&status, &reopened); err != nil {
			t.Fatalf("iteration %d read %s: %v", i, id, err)
		}
		if status == "dead_letter" {
			out.lost++
			continue
		}
		admitted := id == c2 && byAdmin
		wake, adminReopens, other := reopenWriters(t, ctx, base, id, pids)
		if other != 0 || wake+adminReopens != 1 || adminReopens == 1 != admitted || !reopened.Valid {
			t.Errorf("iteration %d: %s status=%s reported by admin=%v; reopens by wake=%d admin=%d other=%d, reopened_at valid=%v; "+
				"want reopened exactly once, by the writer that reported it, with a stamp; audit %s (actor pids %v)",
				i, id, status, admitted, wake, adminReopens, other, reopened.Valid, reopenAuditTrail(t, ctx, base, id), pids)
		}
		out.byWake += wake
		out.byAdmin += adminReopens
	}
	interleaveExec(t, ctx, base, `DELETE FROM fact_work_items WHERE scope_id = $1`, scope)
	interleaveExec(t, ctx, base, `DELETE FROM interleave_reopen_audit WHERE work_item_id LIKE $1 || '-%'`, scope)
	return out
}

// installReopenAudit adds a test-only trigger to the isolated schema's
// fact_work_items that records every move into pending, and every rewrite of
// a pending row that moves its reopen stamp, with the writer's backend pid. A rolled-back writer (an admin replay retried on 40P01) leaves
// no audit row.
func installReopenAudit(t *testing.T, ctx context.Context, base *sql.DB) {
	t.Helper()
	interleaveExec(t, ctx, base, `CREATE TABLE interleave_reopen_audit (
 work_item_id TEXT NOT NULL, old_status TEXT NOT NULL, writer_pid INT NOT NULL)`)
	interleaveExec(t, ctx, base, `CREATE FUNCTION interleave_reopen_audit_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO interleave_reopen_audit VALUES (NEW.work_item_id, OLD.status, pg_backend_pid());
  RETURN NEW;
END $$`)
	interleaveExec(t, ctx, base, `CREATE TRIGGER interleave_reopen_audit AFTER UPDATE ON fact_work_items
 FOR EACH ROW WHEN (NEW.status = 'pending' AND (OLD.status IS DISTINCT FROM 'pending'
   OR NEW.reopened_at IS DISTINCT FROM OLD.reopened_at))
 EXECUTE FUNCTION interleave_reopen_audit_row()`)
}

// interleavePIDs reads the backend pid of each actor's single connection.
func interleavePIDs(t *testing.T, ctx context.Context, conns []*sql.DB) []int {
	t.Helper()
	pids := make([]int, len(conns))
	for i, conn := range conns {
		if err := conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pids[i]); err != nil {
			t.Fatalf("actor %d backend pid: %v", i, err)
		}
	}
	return pids
}

// reopenWriters counts the audited moves of id into pending by the wake (the
// activation, actor 0, or the final Fail's producer-terminal wake, actor 1),
// by the admin replay (actor 2), and by any other backend.
func reopenWriters(t *testing.T, ctx context.Context, base *sql.DB, id string, pids []int) (wake, adminReplay, other int) {
	t.Helper()
	if err := base.QueryRowContext(ctx, `SELECT
  count(*) FILTER (WHERE writer_pid IN ($2, $3)),
  count(*) FILTER (WHERE writer_pid = $4),
  count(*) FILTER (WHERE writer_pid NOT IN ($2, $3, $4))
FROM interleave_reopen_audit WHERE work_item_id = $1`, id, pids[0], pids[1], pids[2]).Scan(&wake, &adminReplay, &other); err != nil {
		t.Fatalf("read reopen audit for %s: %v", id, err)
	}
	return wake, adminReplay, other
}

func interleaveClaim(t *testing.T, ctx context.Context, q postgres.ReducerQueue, want string) reducercontract.Intent {
	t.Helper()
	intent, ok, err := q.Claim(ctx)
	if err != nil || !ok || intent.IntentID != want {
		t.Fatalf("claim %s: ok=%v got=%s err=%v", want, ok, intent.IntentID, err)
	}
	return intent
}

func interleaveExec(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %.60s: %v", query, err)
	}
}

// interleaveConn opens a one-connection handle pinned to schema; create makes
// the schema and drops it at cleanup.
func interleaveConn(t *testing.T, ctx context.Context, dsn, schema string, create bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if create {
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
			t.Fatalf("create schema: %v", err)
		}
		t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	}
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema+", public"); err != nil {
		t.Fatalf("search_path: %v", err)
	}
	return db
}

// reopenAuditTrail renders id's audited moves into pending as
// old_status@writer_pid, for a failure message.
func reopenAuditTrail(t *testing.T, ctx context.Context, base *sql.DB, id string) string {
	t.Helper()
	var trail string
	if err := base.QueryRowContext(ctx, `SELECT COALESCE(string_agg(old_status || '@' || writer_pid, ', '), 'none')
FROM interleave_reopen_audit WHERE work_item_id = $1`, id).Scan(&trail); err != nil {
		t.Fatalf("read reopen audit trail for %s: %v", id, err)
	}
	return trail
}

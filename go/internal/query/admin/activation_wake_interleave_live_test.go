// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7597 200-iteration interleave of the claim-fenced activation wake, a concurrent final Fail, the public admin replay (retried on 40P01) and a claim-time supersede sweep on independent connections: zero 40P01 on the activation, zero lost wakes, every consumer reopened exactly once; about 4 s locally; enrolled in the live-postgres-readiness runner
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
// wake stamped reopened_at; never both, never neither).
func TestActivationWakeInterleaveLive(t *testing.T) {
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

	const iterations = 200
	var total interleaveOutcome
	for i := 0; i < iterations; i++ {
		total.add(runInterleaveIteration(t, ctx, base, conns, i))
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

func interleaveQueue(conn *sql.DB, domain reducer.Domain) postgres.ReducerQueue {
	q := postgres.NewReducerQueue(postgres.SQLDB{DB: conn}, "interleave-7597", 5*time.Minute)
	q.ClaimDomains = []reducer.Domain{domain}
	q.MaxAttempts = 3
	q.RetryDelay = time.Second
	return q
}

func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

func runInterleaveIteration(t *testing.T, ctx context.Context, base *sql.DB, conns []*sql.DB, i int) interleaveOutcome {
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
		if admitted == reopened.Valid {
			t.Errorf("iteration %d: %s status=%s admin=%v wake=%v; want reopened exactly once", i, id, status, admitted, reopened.Valid)
		}
		if reopened.Valid {
			out.byWake++
		} else {
			out.byAdmin++
		}
	}
	interleaveExec(t, ctx, base, `DELETE FROM fact_work_items WHERE scope_id = $1`, scope)
	return out
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

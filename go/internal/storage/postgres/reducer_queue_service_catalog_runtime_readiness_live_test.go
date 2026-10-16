// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Real-queue proofs for the #7816 service catalog runtime fence. Everything on
// the queue side is production code against real PostgreSQL: ReducerQueue
// Claim/Fail/Ack, the real fact_work_items DDL and claim SQL, the real
// producer readiness store, and the real ServiceCatalogCorrelationHandler.
// The graph is the one fake: runtimeFenceGraph returns the WorkloadInstance
// rows only after the producer "commits" them, which is the ordering the
// production workload_materialization handler has (graph write, then Ack).
//
// The TestReducerContentionGate prefix puts these in the reducer contention
// gate's -run filter (.github/workflows/reducer-contention-gate.yml).

const (
	runtimeFenceRepoID       = "repo-checkout"
	runtimeFenceRepoScope    = "git-repository-scope:repo-checkout"
	runtimeFenceRepoGen      = "gen-checkout-7816"
	runtimeFenceCatalogScope = "git-repository-scope:repo-catalog-7816"
	runtimeFenceCatalogGen   = "gen-catalog-7816"
	runtimeFenceProducerItem = "wm-checkout-7816"
	runtimeFenceCatalogItem  = "catalog-7816"
)

// runtimeFenceGraph models the canonical graph: the repository's two
// WorkloadInstance rows exist only once the producer committed them.
type runtimeFenceGraph struct {
	committed atomic.Bool
	reads     atomic.Int64
}

func (g *runtimeFenceGraph) GetRuntimeInstancesForRepos(
	_ context.Context,
	repoIDs []string,
) (map[string][]servicecatalog.ServiceRuntimeInstance, error) {
	g.reads.Add(1)
	if !g.committed.Load() {
		return map[string][]servicecatalog.ServiceRuntimeInstance{}, nil
	}
	out := map[string][]servicecatalog.ServiceRuntimeInstance{}
	for _, repoID := range repoIDs {
		if repoID == runtimeFenceRepoID {
			out[repoID] = []servicecatalog.ServiceRuntimeInstance{
				{PlatformKind: "kubernetes", Environment: "prod", WorkloadRef: "workload-instance:checkout:prod"},
				{PlatformKind: "kubernetes", Environment: "stage", WorkloadRef: "workload-instance:checkout:stage"},
			}
		}
	}
	return out, nil
}

// runtimeFenceMaterializations records every committed service generation.
type runtimeFenceMaterializations struct {
	mu     sync.Mutex
	writes []servicecatalog.ServiceMaterializationWrite
}

func (w *runtimeFenceMaterializations) WriteServiceMaterialization(
	_ context.Context,
	write servicecatalog.ServiceMaterializationWrite,
) (servicecatalog.ServiceMaterializationWriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, write)
	return servicecatalog.ServiceMaterializationWriteResult{Committed: true}, nil
}

func (w *runtimeFenceMaterializations) snapshot() []servicecatalog.ServiceMaterializationWrite {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]servicecatalog.ServiceMaterializationWrite(nil), w.writes...)
}

type runtimeFenceProof struct {
	db              *sql.DB
	schema          string
	graph           *runtimeFenceGraph
	materialization *runtimeFenceMaterializations
	correlation     *serviceCatalogFenceNoopCorrelationWriter
}

// newRuntimeFenceProof seeds the repository scope (active generation with a
// pending workload_materialization row) and the catalog scope (one pending
// service_catalog_correlation row created catalogCreated ago). The producer
// row's cycle started one minute ago.
func newRuntimeFenceProof(t *testing.T, ctx context.Context, dsn string, catalogCreated time.Duration) *runtimeFenceProof {
	t.Helper()
	installRuntimeFenceTrigram(t, ctx, dsn)
	db, schema := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Now().UTC()
	for _, seed := range []struct{ scope, gen string }{
		{runtimeFenceRepoScope, runtimeFenceRepoGen},
		{runtimeFenceCatalogScope, runtimeFenceCatalogGen},
	} {
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
			    partition_key, observed_at, ingested_at, status, payload)
			 VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', '{}'::jsonb)`, []any{seed.scope, now}},
			{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
			 VALUES ($2, $1, 'snapshot', $3, $3, 'active', $3)`, []any{seed.scope, seed.gen, now}},
			{`UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, []any{seed.scope, seed.gen}},
		} {
			if _, err := db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
				t.Fatalf("seed %s: %v", seed.scope, err)
			}
		}
	}
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: runtimeFenceProducerItem, scopeID: runtimeFenceRepoScope, generationID: runtimeFenceRepoGen,
		domain: string(reducer.DomainWorkloadMaterialization), conflictDomain: "platform_graph",
		conflictKey: runtimeFenceRepoScope, sourceSystem: "git", updatedAt: now.Add(-time.Minute),
	})
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: runtimeFenceCatalogItem, scopeID: runtimeFenceCatalogScope, generationID: runtimeFenceCatalogGen,
		domain: string(reducer.DomainServiceCatalogCorrelation), conflictDomain: "scope",
		conflictKey: runtimeFenceCatalogScope, sourceSystem: "git", updatedAt: now.Add(-catalogCreated),
	})
	return &runtimeFenceProof{
		db: db, schema: schema, graph: &runtimeFenceGraph{},
		materialization: &runtimeFenceMaterializations{}, correlation: &serviceCatalogFenceNoopCorrelationWriter{},
	}
}

// installRuntimeFenceTrigram pins pg_trgm in public before any proof schema
// bootstraps. Otherwise the first bootstrap's own CREATE EXTENSION lands in
// that proof's schema, and a second proof schema opened in the same test on a
// fresh database fails with "operator class gin_trgm_ops does not exist".
func installRuntimeFenceTrigram(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()
	postgresproof.InstallTrigramExtension(ctx, t, admin)
}

func (p *runtimeFenceProof) handler(db *sql.DB) servicecatalog.ServiceCatalogCorrelationHandler {
	return servicecatalog.ServiceCatalogCorrelationHandler{
		FactLoader:               serviceCatalogFenceFactLoader{},
		Writer:                   p.correlation,
		MaterializationWriter:    p.materialization,
		RuntimeInstanceLoader:    p.graph,
		RuntimeProducerReadiness: producerreadiness.Store{DB: SQLDB{DB: db}},
	}
}

func runtimeFenceQueue(db *sql.DB, owner string, domain reducer.Domain, clock func() time.Time) ReducerQueue {
	return ReducerQueue{
		database: SQLDB{DB: db}, LeaseOwner: owner, LeaseDuration: time.Minute, RetryDelay: time.Second,
		MaxAttempts: 3, ClaimDomains: []reducer.Domain{domain}, Now: clock,
	}
}

func readRuntimeFenceItem(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) (string, string, int) {
	t.Helper()
	var status string
	var failureClass sql.NullString
	var attempts int
	if err := db.QueryRowContext(ctx,
		`SELECT status, failure_class, attempt_count FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status, &failureClass, &attempts); err != nil {
		t.Fatalf("read %s: %v", workItemID, err)
	}
	return status, failureClass.String, attempts
}

// TestReducerContentionGateServiceCatalogRuntimeFenceKeepsItsAttemptBudget is
// proof 4: while the repository's workload_materialization row is pending and
// then claimed, the catalog row defers through more cycles than MaxAttempts
// with attempt_count frozen at 1 and nothing written. Once the producer
// commits its graph write and acks, the next claim commits the service
// generation with both runtime rows.
func TestReducerContentionGateServiceCatalogRuntimeFenceKeepsItsAttemptBudget(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
	ctx := context.Background()
	p := newRuntimeFenceProof(t, ctx, dsn, time.Minute)
	clock := time.Now().UTC()
	now := func() time.Time { return clock }
	catalogQueue := runtimeFenceQueue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation, now)
	producerQueue := runtimeFenceQueue(p.db, "workload-worker", reducer.DomainWorkloadMaterialization, now)
	handler := p.handler(p.db)

	var producerIntent reducer.Intent
	for cycle := 1; cycle <= 5; cycle++ {
		if cycle == 3 {
			// The producer is claimed from here on: claimed counts as in flight.
			intent, claimed, err := producerQueue.Claim(ctx)
			if err != nil || !claimed {
				t.Fatalf("producer Claim() = %v, %v", claimed, err)
			}
			producerIntent = intent
		}
		intent, claimed, err := catalogQueue.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("cycle %d: catalog Claim() claimed=%v err=%v, want the deferred row back", cycle, claimed, err)
		}
		if intent.AttemptCount != 1 {
			t.Fatalf("cycle %d: AttemptCount = %d, want 1 frozen under %s", cycle, intent.AttemptCount, servicecatalog.RuntimeNotReadyFailureClass)
		}
		_, handleErr := handler.Handle(ctx, intent)
		var classified interface{ FailureClass() string }
		if !errors.As(handleErr, &classified) || classified.FailureClass() != servicecatalog.RuntimeNotReadyFailureClass {
			t.Fatalf("cycle %d: Handle() error = %v, want the runtime readiness deferral", cycle, handleErr)
		}
		if err := catalogQueue.Fail(ctx, intent, handleErr); err != nil {
			t.Fatalf("cycle %d: Fail() error = %v", cycle, err)
		}
		status, class, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceCatalogItem)
		if status != "retrying" || class != servicecatalog.RuntimeNotReadyFailureClass || attempts != 1 {
			t.Fatalf("cycle %d: catalog row = %s/%s/attempts=%d, want retrying/%s/1", cycle, status, class, attempts, servicecatalog.RuntimeNotReadyFailureClass)
		}
		if p.correlation.calls != 0 || len(p.materialization.snapshot()) != 0 || p.graph.reads.Load() != 0 {
			t.Fatalf("cycle %d: correlation=%d materialization=%d graph reads=%d, want 0 while the fence is open",
				cycle, p.correlation.calls, len(p.materialization.snapshot()), p.graph.reads.Load())
		}
		clock = clock.Add(2 * time.Second)
	}

	// The producer commits its graph write, then acks (production order).
	p.graph.committed.Store(true)
	if err := producerQueue.Ack(ctx, producerIntent, reducer.Result{}); err != nil {
		t.Fatalf("producer Ack() error = %v", err)
	}
	intent, claimed, err := catalogQueue.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("post-producer Claim() = %v, %v", claimed, err)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("post-producer Handle() error = %v, want nil once the producer is terminal", err)
	}
	if err := catalogQueue.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("catalog Ack() error = %v", err)
	}
	writes := p.materialization.snapshot()
	if len(writes) != 1 || len(writes[0].Runtime) != 2 {
		t.Fatalf("materialization writes = %d (runtime rows %v), want 1 write with 2 runtime rows", len(writes), runtimeRowCounts(writes))
	}
	if status, _, attempts := readRuntimeFenceItem(t, ctx, p.db, runtimeFenceCatalogItem); status != "succeeded" || attempts != 1 {
		t.Fatalf("catalog terminal = %s attempts=%d, want succeeded at 1", status, attempts)
	}
}

func runtimeRowCounts(writes []servicecatalog.ServiceMaterializationWrite) []int {
	counts := make([]int, 0, len(writes))
	for _, write := range writes {
		counts = append(counts, len(write.Runtime))
	}
	return counts
}

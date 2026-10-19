// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// Real-queue proofs for the #8169 re-trigger. The queue, the producer
// readiness store, the correlation fact writer, and the service
// materialization writer are all production code against real PostgreSQL.
// The graph is the one fake (runtimeFenceGraph from the #7816 proofs): it
// serves the WorkloadInstance rows only after the producer "commits" them.

const (
	retriggerBillingRepoID = "repo-billing"
	retriggerCheckoutSvc   = "svc-checkout"
	retriggerBillingSvc    = "svc-billing"
)

// retriggerFactLoader serves two catalog services in one catalog scope:
// checkout in repo-checkout (which gains runtime instances) and billing in
// repo-billing (which never does: the unchanged repository).
type retriggerFactLoader struct{}

func (retriggerFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	var out []facts.Envelope
	for _, svc := range []struct{ ref, id, repo, name string }{
		{"component:default/checkout", retriggerCheckoutSvc, runtimeFenceRepoID, "Checkout"},
		{"component:default/billing", retriggerBillingSvc, retriggerBillingRepoID, "Billing"},
	} {
		out = append(out,
			facts.Envelope{
				FactID: "entity-" + svc.id, FactKind: facts.ServiceCatalogEntityFactKind,
				SchemaVersion: facts.ServiceCatalogSchemaVersionV1, SourceConfidence: facts.SourceConfidenceReported,
				Payload: map[string]any{
					"provider": "backstage", "entity_ref": svc.ref, "entity_type": "service",
					"display_name": svc.name, "lifecycle": "production", "tier": "tier_1", "service_id": svc.id,
				},
			},
			facts.Envelope{
				FactID: "ownership-" + svc.id, FactKind: facts.ServiceCatalogOwnershipFactKind,
				SchemaVersion: facts.ServiceCatalogSchemaVersionV1, SourceConfidence: facts.SourceConfidenceReported,
				Payload: map[string]any{"provider": "backstage", "entity_ref": svc.ref, "owner_ref": "team-payments"},
			},
			facts.Envelope{
				FactID: "repo-link-" + svc.id, FactKind: facts.ServiceCatalogRepositoryLinkFactKind,
				SchemaVersion: facts.ServiceCatalogSchemaVersionV1, SourceConfidence: facts.SourceConfidenceReported,
				Payload: map[string]any{"provider": "backstage", "entity_ref": svc.ref, "repository_id": svc.repo},
			},
		)
	}
	return out, nil
}

func (retriggerFactLoader) ListActiveRepositoryFacts(context.Context) ([]facts.Envelope, error) {
	return []facts.Envelope{
		{
			FactID: runtimeFenceRepoID, FactKind: factload.FactKindRepository,
			Payload: map[string]any{"repo_id": runtimeFenceRepoID, "name": "checkout", "remote_url": "https://github.com/acme/checkout.git"},
		},
		{
			FactID: retriggerBillingRepoID, FactKind: factload.FactKindRepository,
			Payload: map[string]any{"repo_id": retriggerBillingRepoID, "name": "billing", "remote_url": "https://github.com/acme/billing.git"},
		},
	}, nil
}

// retriggerProof seeds the repository scope (active generation, no producer
// row yet) and the catalog scope (one pending catalog row).
type retriggerProof struct {
	db     *sql.DB
	schema string
	dsn    string
	graph  *runtimeFenceGraph
	clock  time.Time
}

func newRetriggerProof(t *testing.T, ctx context.Context) *retriggerProof {
	t.Helper()
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}
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
		workItemID: runtimeFenceCatalogItem, scopeID: runtimeFenceCatalogScope, generationID: runtimeFenceCatalogGen,
		domain: string(reducer.DomainServiceCatalogCorrelation), conflictDomain: "scope",
		conflictKey: runtimeFenceCatalogScope, sourceSystem: "git", updatedAt: now.Add(-time.Minute),
	})
	return &retriggerProof{db: db, schema: schema, dsn: dsn, graph: &runtimeFenceGraph{}, clock: now}
}

// addProducer inserts a pending workload_materialization row for the
// repository scope, as a later projector cycle would.
func (p *retriggerProof) addProducer(t *testing.T, ctx context.Context, workItemID string) {
	t.Helper()
	insertReducerFairnessWorkItem(t, ctx, p.db, reducerFairnessWorkItem{
		workItemID: workItemID, scopeID: runtimeFenceRepoScope, generationID: runtimeFenceRepoGen,
		domain: string(reducer.DomainWorkloadMaterialization), conflictDomain: "platform_graph",
		conflictKey: workItemID, sourceSystem: "git", updatedAt: p.clock.Add(-time.Minute),
	})
}

func (p *retriggerProof) now() time.Time { return p.clock }

func (p *retriggerProof) handler(db *sql.DB) servicecatalog.ServiceCatalogCorrelationHandler {
	return servicecatalog.ServiceCatalogCorrelationHandler{
		FactLoader: retriggerFactLoader{},
		Writer:     servicecatalog.PostgresServiceCatalogCorrelationWriter{DB: SQLDB{DB: db}, Now: p.now},
		MaterializationWriter: servicecatalog.PostgresServiceMaterializationWriter{
			DB: servicecatalog.ServiceMaterializationSQLBeginner{DB: db}, Now: p.now,
		},
		RuntimeInstanceLoader:    p.graph,
		RuntimeProducerReadiness: producerreadiness.Store{DB: SQLDB{DB: db}},
		Now:                      p.now,
	}
}

func (p *retriggerProof) queue(db *sql.DB, owner string, domain reducer.Domain) ReducerQueue {
	return runtimeFenceQueue(db, owner, domain, p.now)
}

// runCatalog claims, handles, and acks the catalog row once and fails the
// test unless the handler commits.
func (p *retriggerProof) runCatalog(t *testing.T, ctx context.Context, step string) {
	t.Helper()
	q := p.queue(p.db, "catalog-worker", reducer.DomainServiceCatalogCorrelation)
	intent, claimed, err := q.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("%s: catalog Claim() claimed=%v err=%v", step, claimed, err)
	}
	if _, err := p.handler(p.db).Handle(ctx, intent); err != nil {
		t.Fatalf("%s: catalog Handle() error = %v", step, err)
	}
	if err := q.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("%s: catalog Ack() error = %v", step, err)
	}
}

// runProducer claims and acks one workload_materialization row after the
// graph write, the production order.
func (p *retriggerProof) runProducer(t *testing.T, ctx context.Context, db *sql.DB, owner string) {
	t.Helper()
	q := p.queue(db, owner, reducer.DomainWorkloadMaterialization)
	intent, claimed, err := q.Claim(ctx)
	if err != nil || !claimed {
		t.Fatalf("producer Claim() claimed=%v err=%v", claimed, err)
	}
	p.graph.committed.Store(true)
	if err := q.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("producer Ack() error = %v", err)
	}
}

// serviceGenerations returns generation_id -> status for one service.
func (p *retriggerProof) serviceGenerations(t *testing.T, ctx context.Context, serviceID string) map[string]string {
	t.Helper()
	rows, err := p.db.QueryContext(ctx,
		`SELECT generation_id, status FROM service_materialization_generations WHERE service_id = $1`, serviceID)
	if err != nil {
		t.Fatalf("read service generations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			t.Fatalf("scan service generation: %v", err)
		}
		out[id] = status
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate service generations: %v", err)
	}
	return out
}

func retriggerActiveGeneration(generations map[string]string) string {
	for id, status := range generations {
		if status == "active" {
			return id
		}
	}
	return ""
}

// runtimeSnapshotCount counts the runtime evidence snapshot rows of one
// service generation.
func (p *retriggerProof) runtimeSnapshotCount(t *testing.T, ctx context.Context, generationID string) int {
	t.Helper()
	var n int
	if err := p.db.QueryRowContext(ctx,
		`SELECT count(*) FROM service_evidence_snapshots WHERE generation_id = $1 AND evidence_family = 'runtime'`,
		generationID).Scan(&n); err != nil {
		t.Fatalf("count runtime snapshots: %v", err)
	}
	return n
}

// runtimeAdded returns the runtime added count service_changed_since reports
// for serviceID since sinceGenerationID.
func (p *retriggerProof) runtimeAdded(t *testing.T, ctx context.Context, serviceID, sinceGenerationID string) int {
	t.Helper()
	summary, err := NewStatusStore(SQLDB{DB: p.db}).ComputeServiceChangedSinceDelta(ctx, changedsince.ServiceFilter{
		ServiceID: serviceID, ScopeID: runtimeFenceCatalogScope, SinceGenerationID: sinceGenerationID,
	})
	if err != nil {
		t.Fatalf("ComputeServiceChangedSinceDelta(%s): %v", serviceID, err)
	}
	for _, category := range summary.Categories {
		if category.Category == changedsince.CategoryRuntime {
			return category.Counts.Added
		}
	}
	return 0
}

// readRetriggerItem reads a work item's status and replay flag.
func readRetriggerItem(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) (string, bool) {
	t.Helper()
	var status string
	var flagged bool
	if err := db.QueryRowContext(ctx,
		`SELECT status, cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`, workItemID,
	).Scan(&status, &flagged); err != nil {
		t.Fatalf("read %s: %v", workItemID, err)
	}
	return status, flagged
}

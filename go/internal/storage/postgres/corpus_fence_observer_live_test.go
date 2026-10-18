// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8247 disposable PostgreSQL proof that the corpus-fence gauge statement counts exactly the fence holders and the active-generation retrying resolution-not-ready consumers with the oldest deferred age, and plans on the fact_work_items stage/domain/status index at 20k filler rows; isolated schema; 3.89s locally on PostgreSQL 16; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestQueueObserverStoreCorpusFenceCountsRunOnPostgres is the real-Postgres
// proof for the #8247 fence gauges: one statement against the production
// bootstrap schema counts exactly the scopes holding the fence (the fence
// predicate itself), counts only retrying resolution-not-ready rows of the
// active generation as deferred, and the deferred probe plans on
// fact_work_items_stage_domain_status_idx rather than a sequential scan at
// repo scale (20k filler rows). The production call site is the background
// gauge snapshot refresher, so this cost is paid once per refresh interval,
// never per claim.
func TestQueueObserverStoreCorpusFenceCountsRunOnPostgres(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the corpus fence observer Postgres proof")
	}

	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)

	seedCorpusFenceGaugeScope(t, ctx, db, "holder-scope", "gen-holder", now)
	seedCorpusFenceGaugeScope(t, ctx, db, "open-scope", "gen-open", now)
	// The open scope's retired relationship generation would hold the fence
	// if it were current; only the holder scope's superseded current
	// generation counts.
	seedRelationshipGeneration(t, ctx, db, "gen-holder", "holder-scope", "superseded", now)
	seedRelationshipGeneration(t, ctx, db, "gen-open", "open-scope", "active", now)
	// An older superseded scope generation whose deferred row must not count.
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ('gen-old', 'open-scope', 'snapshot', 'fair', $1, $1, 'superseded', $1, $1, '{}'::jsonb)`,
		now.Add(-72*time.Hour)); err != nil {
		t.Fatalf("insert old scope generation: %v", err)
	}

	insertCorpusFenceWorkItem(t, ctx, db, corpusFenceWorkItem{
		id: "deferred-deployable", scopeID: "open-scope", generationID: "gen-open",
		domain: string(reducer.DomainDeployableUnitCorrelation), status: "retrying",
		failureClass: reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		createdAt:    now.Add(-48 * time.Hour),
	})
	insertCorpusFenceWorkItem(t, ctx, db, corpusFenceWorkItem{
		id: "deferred-workload", scopeID: "open-scope", generationID: "gen-open",
		domain: string(reducer.DomainWorkloadMaterialization), status: "retrying",
		failureClass: reducer.WorkloadMaterializationResolutionNotReadyFailureClass,
		createdAt:    now.Add(-2 * time.Hour),
	})
	insertCorpusFenceWorkItem(t, ctx, db, corpusFenceWorkItem{
		id: "pending-not-deferred", scopeID: "open-scope", generationID: "gen-open",
		domain: string(reducer.DomainDeployableUnitCorrelation), status: "pending",
		failureClass: reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		createdAt:    now.Add(-96 * time.Hour),
	})
	insertCorpusFenceWorkItem(t, ctx, db, corpusFenceWorkItem{
		id: "stale-generation-deferred", scopeID: "open-scope", generationID: "gen-old",
		domain: string(reducer.DomainDeployableUnitCorrelation), status: "retrying",
		failureClass: reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		createdAt:    now.Add(-100 * time.Hour),
	})
	insertCorpusFenceWorkItem(t, ctx, db, corpusFenceWorkItem{
		id: "other-class-retrying", scopeID: "open-scope", generationID: "gen-open",
		domain: string(reducer.DomainDeployableUnitCorrelation), status: "retrying",
		failureClass: "reducer_retryable",
		createdAt:    now.Add(-90 * time.Hour),
	})
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, payload, created_at, updated_at
)
SELECT 'filler-' || g, 'open-scope', 'gen-open', 'reducer', 'runtime_truth',
       'scope', 'filler-' || g, 'succeeded', 1, '{}'::jsonb, $1, $1
FROM generate_series(1, 20000) AS g`, now); err != nil {
		t.Fatalf("insert filler work items: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
		t.Fatalf("analyze fact_work_items: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE ingestion_scopes"); err != nil {
		t.Fatalf("analyze ingestion_scopes: %v", err)
	}

	observer := NewQueueObserverStore(SQLDB{DB: db})
	observer.Now = func() time.Time { return now }

	holders, deferred, oldest, err := observer.CorpusFenceCounts(ctx)
	if err != nil {
		t.Fatalf("CorpusFenceCounts() error = %v", err)
	}
	if holders != 1 {
		t.Fatalf("holders = %d, want 1 (only holder-scope)", holders)
	}
	if got := deferred[string(reducer.DomainDeployableUnitCorrelation)]; got != 1 {
		t.Fatalf("deferred deployable = %d, want 1 (pending, stale-generation, and other-class rows excluded)", got)
	}
	if got := deferred[string(reducer.DomainWorkloadMaterialization)]; got != 1 {
		t.Fatalf("deferred workload = %d, want 1", got)
	}
	if got := oldest[string(reducer.DomainDeployableUnitCorrelation)]; got != 48*3600 {
		t.Fatalf("oldest deployable age = %v, want %v", got, float64(48*3600))
	}

	plan := explainCorpusFenceGauges(t, ctx, db, now)
	t.Logf("corpus fence gauges plan:\n%s", plan)
	if !strings.Contains(plan, "fact_work_items_stage_domain_status_idx") {
		t.Fatalf("deferred probe does not plan on the stage/domain/status index:\n%s", plan)
	}
}

func explainCorpusFenceGauges(t *testing.T, ctx context.Context, db *sql.DB, now time.Time) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+corpusFenceGaugesQuery, now)
	if err != nil {
		t.Fatalf("explain corpus fence gauges: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	return strings.Join(lines, "\n")
}

func seedCorpusFenceGaugeScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, 'repository', 'git', $1, NULL, 'git', $1, $3, $3, 'active', $2, '{}'::jsonb)`,
		scopeID, generationID, now); err != nil {
		t.Fatalf("insert corpus fence scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($2, $1, 'snapshot', 'fair', $3, $3, 'active', $3, NULL, '{}'::jsonb)`,
		scopeID, generationID, now); err != nil {
		t.Fatalf("insert corpus fence generation %q: %v", generationID, err)
	}
}

func seedRelationshipGeneration(t *testing.T, ctx context.Context, db *sql.DB, generationID, scopeID, status string, now time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
VALUES ($1, $2, $3, $4, $4)`, generationID, scopeID, status, now); err != nil {
		t.Fatalf("insert relationship generation %q: %v", generationID, err)
	}
}

type corpusFenceWorkItem struct {
	id           string
	scopeID      string
	generationID string
	domain       string
	status       string
	failureClass string
	createdAt    time.Time
}

func insertCorpusFenceWorkItem(t *testing.T, ctx context.Context, db *sql.DB, item corpusFenceWorkItem) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, failure_class, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', $4, 'scope', $1, $5, 1, $6, '{}'::jsonb, $7, $7)`,
		item.id, item.scopeID, item.generationID, item.domain, item.status, item.failureClass, item.createdAt); err != nil {
		t.Fatalf("insert corpus fence work item %q: %v", item.id, err)
	}
}

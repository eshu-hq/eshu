// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/support"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	incidentCorrelationIndexName     = "fact_records_incident_repository_correlation_service_idx"
	incidentRoutingAppliedIndexName  = "fact_records_incident_routing_applied_service_idx"
	incidentRoutingObservedIndexName = "fact_records_incident_routing_observed_service_idx"

	// incidentRoutingBufferBound is the shared-buffer ceiling for the routing read
	// at one million facts. The repository-first shape measures about 2,070 hits;
	// the plain join measured about 60,000 (30 to 64 ms), so the bound catches a
	// regression to it without being a timing assertion.
	incidentRoutingBufferBound = 10_000
	// sourceOnlyBufferBound is the ceiling for the source-only count with the
	// routing half. The materialized shape measures about 166,000 hits (81 to
	// 85 ms); the per-scope rescan measured about 382,000 (1.3 to 1.7 s).
	sourceOnlyBufferBound = 250_000
)

// TestServiceStoryIncidentRoutingUsesLookupIndexesLive is the #7463 plan proof,
// on the corpus the #7138 proof used (about one million fact_records, three
// generations per scope, only the last active) plus a PagerDuty corpus: 40
// scopes of 2,000 applied resources or observed services, 20,000 reducer
// correlation facts across 5,000 repositories, and repo-x correlated to two
// provider services whose applied and observed facts the read must find.
//
// The routing read must start from the repository's correlation facts and reach
// the routing facts through the applied and observed service indexes, in custom
// and generic plans. The source-only count must still use migration 123's index
// and stay under its buffer bound with the correlation set folded in.
//
// Set ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN (an administrative
// postgres-database DSN) and ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1.
func TestServiceStoryIncidentRoutingUsesLookupIndexesLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		30*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedStorySupportScaleCorpus(t, ctx, db)
	seedStorySupportPagerDutyCorpus(t, ctx, db)

	routingSQL, routingArgs := support.IncidentRoutingSQL("repo-x", 10)
	emptySQL, emptyArgs := support.IncidentRoutingSQL("repo-none", 10)
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		plan := explainPreparedWithMode(t, ctx, db, routingSQL, routingArgs, mode)
		for _, index := range []string{incidentCorrelationIndexName, incidentRoutingAppliedIndexName, incidentRoutingObservedIndexName} {
			if !plan.indexes[index] {
				t.Fatalf("routing/%s: plan did not use %s: indexes=%v", mode, index, plan.indexNames())
			}
		}
		if !planIndexCondMentions(plan, incidentCorrelationIndexName, "repository_id") {
			t.Fatalf("routing/%s: the correlation probe is not keyed on repository_id: conds=%v", mode, plan.indexConds[incidentCorrelationIndexName])
		}
		if !planIndexCondMentions(plan, incidentRoutingAppliedIndexName, "provider_object_id") {
			t.Fatalf("routing/%s: the applied service probe is a heap filter, not an index condition: conds=%v", mode, plan.indexConds[incidentRoutingAppliedIndexName])
		}
		if !planIndexCondMentions(plan, incidentRoutingObservedIndexName, "service_id") {
			t.Fatalf("routing/%s: the observed service probe is a heap filter, not an index condition: conds=%v", mode, plan.indexConds[incidentRoutingObservedIndexName])
		}
		if plan.sharedHit+plan.sharedRead > incidentRoutingBufferBound {
			t.Fatalf("routing/%s: %d shared buffers, want under %d; the read has left its repository-first shape", mode, plan.sharedHit+plan.sharedRead, incidentRoutingBufferBound)
		}
		t.Logf("STORY_SUPPORT_PLAN routing repo-x %s ms=%.2f buffers=hit:%d/read:%d", mode, plan.executionMS, plan.sharedHit, plan.sharedRead)

		empty := explainPreparedWithMode(t, ctx, db, emptySQL, emptyArgs, mode)
		if !empty.indexes[incidentCorrelationIndexName] {
			t.Fatalf("routing-empty/%s: plan did not probe %s: indexes=%v", mode, incidentCorrelationIndexName, empty.indexNames())
		}
		if empty.sharedHit+empty.sharedRead > 100 {
			t.Fatalf("routing-empty/%s: %d shared buffers for a repository with no correlation, want a handful", mode, empty.sharedHit+empty.sharedRead)
		}
		t.Logf("STORY_SUPPORT_PLAN routing repo-none %s ms=%.2f buffers=hit:%d/read:%d", mode, empty.executionMS, empty.sharedHit, empty.sharedRead)
	}

	rows, err := db.QueryContext(ctx, routingSQL, routingArgs...)
	if err != nil {
		t.Fatalf("routing read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("routing read: %v", err)
	}
	if count != 8 {
		t.Fatalf("routing read returned %d rows, want 8 (applied and observed facts of the two correlated services, two of each)", count)
	}

	sourceSQL, sourceArgs := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		plan := explainPreparedWithMode(t, ctx, db, sourceSQL, sourceArgs, mode)
		if !plan.indexes[storySupportKindsIndexName] {
			t.Fatalf("source-only/%s: plan did not use %s: indexes=%v", mode, storySupportKindsIndexName, plan.indexNames())
		}
		if plan.sharedHit+plan.sharedRead > sourceOnlyBufferBound {
			t.Fatalf("source-only/%s: %d shared buffers, want under %d; the correlation set is being rescanned per scope", mode, plan.sharedHit+plan.sharedRead, sourceOnlyBufferBound)
		}
		t.Logf("STORY_SUPPORT_PLAN source-only %s ms=%.2f buffers=hit:%d/read:%d", mode, plan.executionMS, plan.sharedHit, plan.sharedRead)
	}
	if !strings.Contains(sourceSQL, support.AdmissibleCorrelationsCTE) {
		t.Fatalf("source-only statement lost its correlation set:\n%s", sourceSQL)
	}
}

// seedStorySupportPagerDutyCorpus adds a PagerDuty corpus: 20 scopes of 2,000 applied
// resources and 20 scopes of 2,000 observed services, three generations each
// with only the last active, plus 20,000 reducer correlation facts across 5,000
// repositories. repo-x is correlated to three provider services: exact, derived
// and an ambiguous provenance-only one that must not count.
func seedStorySupportPagerDutyCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
SELECT 'scope:pd:' || s, 'incident_service', 'pagerduty', 'pd:' || s, 'pagerduty', 'pd', clock_timestamp(), clock_timestamp(), 'active', 'gen:pd:' || s || ':3'
FROM generate_series(1, 40) AS s`, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen:pd:' || s || ':' || g, 'scope:pd:' || s, 'proof', clock_timestamp(), clock_timestamp(),
       CASE WHEN g = 3 THEN 'active' ELSE 'superseded' END, clock_timestamp()
FROM generate_series(1, 40) AS s, generate_series(1, 3) AS g`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'pda:' || s || ':' || g || ':' || n, 'scope:pd:' || s, 'gen:pd:' || s || ':' || g,
       'incident_routing.applied_pagerduty_resource',
       'pda:' || s || ':' || g || ':' || n, 'pagerduty', 'pagerduty', 'pda:' || s || ':' || g || ':' || n,
       clock_timestamp() - (n % 1000) * interval '1 minute', clock_timestamp(),
       jsonb_build_object('resource_class', CASE WHEN n % 3 = 0 THEN 'service' WHEN n % 3 = 1 THEN 'team' ELSE 'escalation_policy' END,
                          'provider_object_id', 'PSVC' || ((s * 100000 + n) % 60000))
FROM generate_series(1, 20) AS s, generate_series(1, 3) AS g, generate_series(1, 2000) AS n`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'pdo:' || s || ':' || g || ':' || n, 'scope:pd:' || s, 'gen:pd:' || s || ':' || g,
       'incident_routing.observed_pagerduty_service',
       'pdo:' || s || ':' || g || ':' || n, 'pagerduty', 'pagerduty', 'pdo:' || s || ':' || g || ':' || n,
       clock_timestamp() - (n % 1000) * interval '1 minute', clock_timestamp(),
       CASE WHEN n % 2 = 0 THEN jsonb_build_object('service_id', 'PSVC' || ((s * 100000 + n) % 60000), 'provider_object_id', 'PSVC' || ((s * 100000 + n) % 60000))
            ELSE jsonb_build_object('service_id', 'PSVC' || ((s * 100000 + n) % 60000)) END
FROM generate_series(21, 40) AS s, generate_series(1, 3) AS g, generate_series(1, 2000) AS n`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'ps:' || n, 'scope:pd:1', 'gen:pd:1:3', 'incident_routing.applied_pagerduty_resource',
       'ps:' || n, 'pagerduty', 'pagerduty', 'ps:' || n, clock_timestamp(), clock_timestamp(),
       jsonb_build_object('resource_class', 'service', 'provider_object_id', 'PTARGET' || (n % 3))
FROM generate_series(1, 6) AS n`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'po:' || n, 'scope:pd:21', 'gen:pd:21:3', 'incident_routing.observed_pagerduty_service',
       'po:' || n, 'pagerduty', 'pagerduty', 'po:' || n, clock_timestamp(), clock_timestamp(),
       jsonb_build_object('service_id', 'PTARGET' || (n % 3))
FROM generate_series(1, 6) AS n`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'corr:' || n, 'scope:pd:' || (1 + n % 20), 'gen:pd:' || (1 + n % 20) || ':3', 'reducer_incident_repository_correlation',
       'corr:' || n, 'reducer', 'reducer', 'corr:' || n, clock_timestamp(), clock_timestamp(),
       jsonb_build_object('provider', 'pagerduty', 'provider_service_id', 'PSVC' || n,
                          'repository_id', 'repo-' || (n % 5000),
                          'outcome', CASE WHEN n % 7 = 0 THEN 'ambiguous' WHEN n % 2 = 0 THEN 'exact' ELSE 'derived' END,
                          'provenance_only', (n % 7 = 0)::text)
FROM generate_series(1, 20000) AS n`, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES
 ('corr:x1', 'scope:pd:1', 'gen:pd:1:3', 'reducer_incident_repository_correlation', 'corr:x1', 'reducer', 'reducer', 'corr:x1', clock_timestamp(), clock_timestamp(),
  '{"provider":"pagerduty","provider_service_id":"PTARGET0","repository_id":"repo-x","outcome":"exact","provenance_only":"false"}'::jsonb),
 ('corr:x2', 'scope:pd:1', 'gen:pd:1:3', 'reducer_incident_repository_correlation', 'corr:x2', 'reducer', 'reducer', 'corr:x2', clock_timestamp(), clock_timestamp(),
  '{"provider":"pagerduty","provider_service_id":"PTARGET1","repository_id":"repo-x","outcome":"derived","provenance_only":"false"}'::jsonb),
 ('corr:x3', 'scope:pd:1', 'gen:pd:1:3', 'reducer_incident_repository_correlation', 'corr:x3', 'reducer', 'reducer', 'corr:x3', clock_timestamp(), clock_timestamp(),
  '{"provider":"pagerduty","provider_service_id":"PTARGET2","repository_id":"repo-x","outcome":"ambiguous","provenance_only":"true"}'::jsonb)`,
		`ANALYZE fact_records`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed: %v\n%.200s", err, s)
		}
	}
}

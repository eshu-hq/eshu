// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const storySupportKindsIndexName = "fact_records_story_support_kinds_idx"

// TestServiceStoryTargetSupportUsesSupportKindsIndexLive proves migration 123's
// partial index serves both story target-support statements (the row read and
// the source-only count) in custom and generic plans on a corpus where the
// support kinds are about one percent of fact_records, the production shape.
// The kind list is inlined as literals in the statements so the planner can
// prove `fact_kind IN (...)` implies the index predicate without knowing the
// bound `$1` array.
func TestServiceStoryTargetSupportUsesSupportKindsIndexLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		10*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedStorySupportScaleCorpus(t, ctx, db)

	targetSQL, targetArgs := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{Repository: "repo-x", Limit: 10})
	sourceSQL, sourceArgs := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for name, stmt := range map[string]struct {
		sql  string
		args []any
	}{
		"target":      {targetSQL, targetArgs},
		"source-only": {sourceSQL, sourceArgs},
	} {
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			plan := explainPreparedWithMode(t, ctx, db, stmt.sql, stmt.args, mode)
			if !plan.indexes[storySupportKindsIndexName] {
				t.Fatalf("%s/%s: plan did not use %s: indexes=%v", name, mode, storySupportKindsIndexName, plan.indexNames())
			}
			t.Logf("STORY_SUPPORT_PLAN %s %s ms=%.2f buffers=hit:%d/read:%d", name, mode, plan.executionMS, plan.sharedHit, plan.sharedRead)
		}
	}
}

// seedStorySupportScaleCorpus builds 200 scopes with three generations of 1,000
// facts each; only the last generation is active and about one percent of the
// facts are support kinds, half of them without ref keys.
func seedStorySupportScaleCorpus(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	execProofStatements(t, ctx, db, []proofStatement{{`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
SELECT 'scope:ss:' || s, 'repository', 'git', 'ss:' || s, 'git', 'ss', clock_timestamp(), clock_timestamp(), 'active', 'gen:ss:' || s || ':3'
FROM generate_series(1, 200) AS s`, nil}, {`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen:ss:' || s || ':' || g, 'scope:ss:' || s, 'proof', clock_timestamp(), clock_timestamp(),
       CASE WHEN g = 3 THEN 'active' ELSE 'superseded' END, clock_timestamp()
FROM generate_series(1, 200) AS s, generate_series(1, 3) AS g`, nil}, {`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'ss:' || s || ':' || g || ':' || n, 'scope:ss:' || s, 'gen:ss:' || s || ':' || g,
       CASE WHEN n % 100 = 0 THEN 'work_item.record'
            WHEN n % 100 = 1 THEN 'incident_routing.coverage_warning'
            ELSE 'content_entity' END,
       'ss:' || s || ':' || g || ':' || n, 'jira', 'jira', 'ss:' || s || ':' || g || ':' || n,
       clock_timestamp() - (n % 1000) * interval '1 minute', clock_timestamp(),
       CASE WHEN n % 200 < 2 THEN '{"candidate_refs":[{"kind":"repository","id":"repo-x"}]}'::jsonb ELSE '{"text":"z"}'::jsonb END
FROM generate_series(1, 200) AS s, generate_series(1, 3) AS g, generate_series(1, 1000) AS n`, nil}, {`ANALYZE fact_records`, nil}})
}

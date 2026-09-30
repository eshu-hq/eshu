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

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	storySupportKindsIndexName    = "fact_records_story_support_kinds_idx"
	storySupportLinkRepoIndexName = "fact_records_story_support_link_repo_idx"
)

// TestServiceStoryTargetSupportUsesSupportKindsIndexLive proves the partial
// indexes serve both story target-support statements in custom and generic
// plans on a corpus where the support kinds are about one percent of
// fact_records, the production shape, plus one Jira scope holding 50,000 active
// work_item.external_link facts (about 10% carrying linked_repository_id, 1% the
// target's).
//
// The source-only count must use migration 123's story_support_kinds_idx. The
// row read must use migration 152's link-repo index, keyed on the
// payload->>'linked_repository_id' expression: on a corpus with external links
// the heap filter it replaces dominates (#7138), and on a corpus without any the
// planner would pick the empty external-link URL index instead, which proves
// nothing. Both indexes carry literal predicates in the statements so the planner
// proves them without knowing the bound `$1` values.
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

	targetSQL, targetArgs := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{
		Repository: "repo-x", TargetKind: "repository", TargetID: "repo-x", Limit: 10,
	})
	sourceSQL, sourceArgs := buildServiceStoryTargetSupportSourceOnlySQL(serviceStoryTargetSupportFactKinds())
	for name, stmt := range map[string]struct {
		sql   string
		args  []any
		index string
	}{
		"target":      {targetSQL, targetArgs, storySupportLinkRepoIndexName},
		"source-only": {sourceSQL, sourceArgs, storySupportKindsIndexName},
	} {
		for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
			plan := explainPreparedWithMode(t, ctx, db, stmt.sql, stmt.args, mode)
			if !plan.indexes[stmt.index] {
				t.Fatalf("%s/%s: plan did not use %s: indexes=%v", name, mode, stmt.index, plan.indexNames())
			}
			if name == "target" && !planIndexCondMentions(plan, storySupportLinkRepoIndexName, storySupportLinkPayloadKey) {
				t.Fatalf("%s/%s: %s is scanned by its scope/generation prefix only; the %s predicate is a heap filter, not an index condition: conds=%v",
					name, mode, storySupportLinkRepoIndexName, storySupportLinkPayloadKey, plan.indexConds[storySupportLinkRepoIndexName])
			}
			t.Logf("STORY_SUPPORT_PLAN %s %s ms=%.2f buffers=hit:%d/read:%d", name, mode, plan.executionMS, plan.sharedHit, plan.sharedRead)
		}
	}
}

// planIndexCondMentions reports whether any plan node that used index carries
// needle in its Index Cond.
func planIndexCondMentions(plan targetFactsPlan, index, needle string) bool {
	for _, cond := range plan.indexConds[index] {
		if strings.Contains(cond, needle) {
			return true
		}
	}
	return false
}

// seedStorySupportScaleCorpus builds 200 repository scopes with three
// generations of 1,000 facts each; only the last generation is active and about
// three percent of the facts are support kinds: records, coverage warnings and
// external links, half of the links keyed to repo-x. It then adds one Jira scope
// with three generations of 50,000 external links each (only the last active),
// writer-shaped: about 10% carry linked_repository_id, 1% the target's.
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
            WHEN n % 100 = 2 THEN 'work_item.external_link'
            ELSE 'content_entity' END,
       'ss:' || s || ':' || g || ':' || n, 'jira', 'jira', 'ss:' || s || ':' || g || ':' || n,
       clock_timestamp() - (n % 1000) * interval '1 minute', clock_timestamp(),
       CASE WHEN n % 100 = 2 AND n % 200 = 2 THEN '{"linked_repository_id":"repo-x"}'::jsonb
            WHEN n % 100 = 2 THEN '{"linked_repository_id":"repo-other"}'::jsonb
            ELSE '{"text":"z"}'::jsonb END
FROM generate_series(1, 200) AS s, generate_series(1, 3) AS g, generate_series(1, 1000) AS n`, nil}, {`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('scope:jira:1', 'work_item_project', 'jira', 'jira:1', 'jira', 'jira', clock_timestamp(), clock_timestamp(), 'active', 'gen:jira:1:3')`, nil}, {`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
SELECT 'gen:jira:1:' || g, 'scope:jira:1', 'proof', clock_timestamp(), clock_timestamp(),
       CASE WHEN g = 3 THEN 'active' ELSE 'superseded' END, clock_timestamp()
FROM generate_series(1, 3) AS g`, nil}, {`
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
                          source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'jl:' || g || ':' || n, 'scope:jira:1', 'gen:jira:1:' || g, 'work_item.external_link',
       'jl:' || g || ':' || n, 'jira', 'jira', 'jl:' || g || ':' || n,
       clock_timestamp() - (n % 1000) * interval '1 minute', clock_timestamp(),
       CASE WHEN n <= 500 THEN '{"linked_repository_id":"repo-x"}'::jsonb
            WHEN n % 10 = 0 THEN jsonb_build_object('linked_repository_id', 'repo-' || (n % 300))
            ELSE '{"provider":"jira_cloud","application_name":"Confluence"}'::jsonb END
FROM generate_series(1, 3) AS g, generate_series(1, 50000) AS n`, nil}, {`ANALYZE fact_records`, nil}})
}

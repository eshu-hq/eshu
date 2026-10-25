// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9019 disposable PostgreSQL 18 proofs of security-alert org-target shard retirement: an empty TrustedDeletion generation over a content-bearing shard scope commits past the #7926 guard (and is refused without the attestation), the projector service acks and activates the zero-fact generation, and list, count, and inventory return no row for the scope; a replayed retirement is skipped as unchanged and the planner read stops returning the scope; EXPLAIN of the planner read on more than 10k scopes uses ingestion_scopes_parent_idx; each proof creates and drops its own database from the bootstrap schema; about 1.5 to 4 s per subtest on PostgreSQL 18.6 locally; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts/alertruntime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/retirement"
)

func shardRetirementProofActivatesAnEmptyGenerationAndEmptiesReads(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	reader := retirement.NewReader(postgres.SQLDB{DB: database})
	shardID := shardLiveBaseScopeID + "#shard-0"
	content := shardLiveGeneration{
		scopeID: shardID, parentScopeID: shardLiveBaseScopeID, generationID: "gen-c1", hint: "hint-c1",
		alerts:            []shardLiveAlert{{repository: "acme/a", number: 1}, {repository: "acme/b", number: 2}},
		repositoriesTotal: 2,
	}
	if err := shardLiveCommit(t, ctx, database, store, content); err != nil {
		t.Fatalf("commit content generation: %v", err)
	}
	shardLiveProject(t, ctx, database)
	shardLiveReconcile(t, ctx, database, content)
	if listed, counted, inventory := shardLiveReads(t, ctx, database); len(listed) != 2 || counted != 2 || len(inventory) != 2 {
		t.Fatalf("before retirement: list %v count %d inventory %v, want 2 each", listed, counted, inventory)
	}
	if got, err := reader.RetirementCandidates(ctx, []string{shardLiveBaseScopeID}, nil, 10); err != nil || !slices.Equal(got, []string{shardID}) {
		t.Fatalf("candidates before retirement = %v, %v; want the shard", got, err)
	}
	if got, err := reader.RetirementCandidates(ctx, []string{shardLiveBaseScopeID}, []string{shardID}, 10); err != nil || len(got) != 0 {
		t.Fatalf("candidates with the shard planned = %v, %v; want none", got, err)
	}

	// Control: without the attestation the #7926 guard refuses the empty
	// successor and keeps the content generation active.
	refused := shardLiveGeneration{scopeID: shardID, parentScopeID: shardLiveBaseScopeID, generationID: "gen-r0", hint: alertruntime.RetirementFreshnessHint}
	if err := shardLiveCommit(t, ctx, database, store, refused); err != nil {
		t.Fatalf("refused commit returned %v, want the guard's silent refusal", err)
	}
	if n := readStateCount(t, ctx, database, `SELECT count(*) FROM scope_generations WHERE generation_id = 'gen-r0'`); n != 0 {
		t.Fatalf("unattested empty successor stored %d generation rows, want 0", n)
	}

	retire := shardLiveGeneration{
		scopeID: shardID, parentScopeID: shardLiveBaseScopeID, generationID: "gen-r1",
		hint: alertruntime.RetirementFreshnessHint, trustedDeletion: true,
	}
	start := time.Now()
	if err := shardLiveCommit(t, ctx, database, store, retire); err != nil {
		t.Fatalf("retirement commit: %v", err)
	}
	if n := readStateCount(t, ctx, database, `SELECT repositories_total FROM security_alert_target_read_states WHERE generation_id = 'gen-r1'`); n != 0 {
		t.Fatalf("retirement read state repositories_total = %d, want 0", n)
	}
	shardLiveProject(t, ctx, database)
	if got := shardLiveActive(t, ctx, database, shardID); got != "gen-r1" {
		t.Fatalf("active generation after retirement = %q, want gen-r1", got)
	}
	if n := readStateCount(t, ctx, database, `SELECT count(*) FROM fact_work_items WHERE generation_id = 'gen-r1' AND stage = 'projector' AND status = 'succeeded'`); n != 1 {
		t.Fatalf("projector rows succeeded for the zero-fact generation = %d, want 1", n)
	}
	if listed, counted, inventory := shardLiveReads(t, ctx, database); len(listed) != 0 || counted != 0 || len(inventory) != 0 {
		t.Fatalf("after retirement: list %v count %d inventory %v, want none", listed, counted, inventory)
	}
	t.Logf("retirement commit plus projection: %s", time.Since(start))
	if got, err := reader.RetirementCandidates(ctx, []string{shardLiveBaseScopeID}, nil, 10); err != nil || len(got) != 0 {
		t.Fatalf("candidates after retirement = %v, %v; want none", got, err)
	}

	// A replayed retirement is skipped as unchanged: no row, nothing moves.
	again := retire
	again.generationID = "gen-r2"
	if err := shardLiveCommit(t, ctx, database, store, again); err != nil {
		t.Fatalf("replayed retirement: %v", err)
	}
	if n := readStateCount(t, ctx, database, `SELECT count(*) FROM scope_generations WHERE generation_id = 'gen-r2'`); n != 0 {
		t.Fatalf("replayed retirement stored %d generation rows, want 0", n)
	}
	if got := shardLiveActive(t, ctx, database, shardID); got != "gen-r1" {
		t.Fatalf("active after replay = %q, want gen-r1", got)
	}
}

func shardRetirementProofPlannerReadUsesTheParentIndex(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'repo-scope-' || g, 'repository', 'git', 'k' || g, NULL, 'git', 'p', now(), now(), 'active', NULL, '{}'::jsonb
FROM generate_series(1, 8000) AS g`,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'security-alert:github-org:org' || p || '#shard-' || c, 'security_alert', 'security_alert', 'k',
       'security-alert:github-org:org' || p, 'security_alert', 'github_dependabot', now(), now(), 'active',
       NULL, '{}'::jsonb
FROM generate_series(1, 400) AS p, generate_series(1, 16) AS c`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'sa-gen-' || p || '-' || c, 'security-alert:github-org:org' || p || '#shard-' || c, 'snapshot', now(), now(), 'active'
FROM generate_series(1, 400) AS p, generate_series(1, 16) AS c`,
		`UPDATE ingestion_scopes SET active_generation_id = 'sa-gen-' || split_part(split_part(scope_id, ':org', 2), '#shard-', 1) || '-' || split_part(scope_id, '#shard-', 2)
WHERE scope_id LIKE 'security-alert:github-org:org%#shard-%'`,
		`INSERT INTO security_alert_target_read_states (generation_id, scope_id, org_read_truncated, repositories_total, unread_total, observed_at)
SELECT generation_id, scope_id, false, CASE WHEN right(generation_id, 2) = '-1' THEN 0 ELSE 60 END, 0, now()
FROM scope_generations WHERE generation_id LIKE 'sa-gen-%'`,
		`ANALYZE ingestion_scopes`,
		`ANALYZE security_alert_target_read_states`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed: %v\n%s", err, statement)
		}
	}
	total := readStateCount(t, ctx, database, `SELECT count(*) FROM ingestion_scopes`)
	bases := array.Of([]string{"security-alert:github-org:org7", "security-alert:github-org:org8"})
	planned := array.Of([]string{"security-alert:github-org:org7#shard-2"})
	rows, err := database.QueryContext(ctx, `EXPLAIN (ANALYZE, BUFFERS) `+retirement.CandidatesQuery, bases, planned, 1024)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan = append(plan, line)
	}
	_ = rows.Close()
	t.Logf("PLAN #9019 retirement candidates, %d scopes:\n%s", total, strings.Join(plan, "\n"))
	joined := strings.Join(plan, "\n")
	for _, want := range []string{"ingestion_scopes_parent_idx", "security_alert_target_read_states_pkey"} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan does not use %s", want)
		}
	}
	if strings.Contains(joined, "Seq Scan on ingestion_scopes") {
		t.Error("plan scans ingestion_scopes")
	}
	got, err := retirement.NewReader(postgres.SQLDB{DB: database}).RetirementCandidates(ctx,
		[]string{"security-alert:github-org:org7", "security-alert:github-org:org8"}, []string{"security-alert:github-org:org7#shard-2"}, 1024)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	// 2 bases x 16 shards, minus the planned one, minus the two whose active
	// generation is a retirement (shard 1 of each base).
	if len(got) != 29 {
		t.Fatalf("candidates = %d, want 29", len(got))
	}
}

// TestShardRetirementLive runs the #9019 retirement proofs as subtests, so the
// readiness shards price them as one test.
func TestShardRetirementLive(t *testing.T) {
	t.Run("ActivatesAnEmptyGenerationAndEmptiesReads", shardRetirementProofActivatesAnEmptyGenerationAndEmptiesReads)
	t.Run("PlannerReadUsesTheParentIndex", shardRetirementProofPlannerReadUsesTheParentIndex)
}

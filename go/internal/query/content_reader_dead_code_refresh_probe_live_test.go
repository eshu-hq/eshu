// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	reachabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// refreshProbeRepo seeds one repository scope for the refresh-intent proof
// (#7591): a scope with one active generation, an acceptance row, an optional
// watermark, and one shared_projection_intents row per payload in intents.
type refreshProbeRepo struct {
	repo      string
	isDelta   bool
	domain    string   // projection domain of every intent; code_calls when empty
	watermark string   // "truncated" or "" for none
	intents   []string // raw JSON payloads, one intent row each
}

func seedRefreshProbeRepo(ctx context.Context, t *testing.T, db *sql.DB, r refreshProbeRepo) {
	t.Helper()
	scopeID := "git-repository-scope:" + r.repo
	generationID := "gen-" + r.repo
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed %s: %v\n%s", r.repo, err, query)
		}
	}
	domain := r.domain
	if domain == "" {
		domain = "code_calls"
	}
	exec(`INSERT INTO ingestion_scopes(scope_id, scope_kind, source_system, source_key, collector_kind,
		partition_key, observed_at, ingested_at, status)
		VALUES ($1, 'repository', 'git', $2, 'git', 'p', now(), now(), 'active')`, scopeID, r.repo)
	exec(`INSERT INTO scope_generations(generation_id, scope_id, trigger_kind, is_delta, observed_at, ingested_at,
		status, activated_at) VALUES ($1, $2, 'x', $3, now(), now(), 'active', now())`, generationID, scopeID, r.isDelta)
	exec(`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, generationID, scopeID)
	exec(`INSERT INTO shared_projection_acceptance(scope_id, acceptance_unit_id, source_run_id, generation_id,
		accepted_at, updated_at) VALUES ($1, $2, 'run-new', $3, now(), now())`, scopeID, r.repo, generationID)
	for i, payload := range r.intents {
		exec(`INSERT INTO shared_projection_intents(intent_id, projection_domain, partition_key, scope_id,
			acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
			VALUES ($1, $2, 'k', $3, $4, $4, 'run-new', $5, $6::jsonb, $7, NULL)`,
			"i-"+r.repo+"-"+string(rune('a'+i)), domain, scopeID, r.repo, generationID, payload, time.Now())
	}
	if r.watermark == "truncated" {
		exec(`INSERT INTO code_reachability_repository_watermarks(scope_id, generation_id, repository_id, truncated,
			updated_at, verdict_schema_epoch) VALUES ($1, $2, $3, true, now(), $4)`,
			scopeID, generationID, r.repo, reachabilitystore.CodeReachabilityVerdictSchemaEpoch)
	}
}

// TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive proves both shipped
// coverage statements ignore a refresh intent when they decide whether a
// repository can be a consumer (#7591). A refresh intent
// ({"action":"refresh"}) carries no caller or child entity, and the
// reachability loader never reads it, so on a FULL generation only per-edge
// intents define the consumer universe. On a DELTA generation the intents do
// not describe the repository's edges and no watermark is ever written for it,
// so any code intent keeps it a gap. A legacy row with no action key counts as
// an edge.
//
// Run with a disposable PostgreSQL 18 administrative database, the same
// variables as TestDeadCodeIncomingEntityIDsActiveRunBoundLive.
func TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive(t *testing.T) {
	dsn := os.Getenv("ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DSN")
	optIn := os.Getenv("ESHU_DEAD_CODE_INCOMING_BOUND_PROOF_DISPOSABLE")
	ctx, db := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap(): %v", err)
	}
	const (
		refresh = `{"action":"refresh","intent_type":"repo_refresh"}`
		upsert  = `{"action":"upsert","caller_entity_id":"e1","callee_entity_id":"e2"}`
		legacy  = `{"callee_entity_id":"e2","resolution_method":"scip"}`
	)
	repos := []refreshProbeRepo{
		// (a) full generation, truncated watermark, refresh only: no edge, so
		// it cannot be a consumer.
		{repo: "rp-full-refresh-only", watermark: "truncated", intents: []string{refresh}},
		// The same repository before a watermark exists.
		{repo: "rp-full-refresh-nowm", intents: []string{refresh}},
		// A repository whose only intents are inheritance_edges refreshes.
		{repo: "rp-inherit-refresh-only", domain: "inheritance_edges", watermark: "truncated", intents: []string{refresh}},
		// (b) one real edge beside the refresh keeps the gap.
		{repo: "rp-full-refresh-edge", watermark: "truncated", intents: []string{refresh, upsert}},
		// (c) delta generation, no watermark, refresh only: still a gap.
		{repo: "rp-delta-refresh-only", isDelta: true, intents: []string{refresh}},
		// (d) a legacy row with no action key is an edge.
		{repo: "rp-legacy-no-action", watermark: "truncated", intents: []string{legacy}},
	}
	for _, r := range repos {
		seedRefreshProbeRepo(ctx, t, db, r)
	}
	want := []code.CrossRepoDeadCodeCoverageGap{
		coverageLiveGap("rp-delta-refresh-only", code.CrossRepoDeadCodeCoverageStateNoSnapshotYet, "gen-rp-delta-refresh-only"),
		coverageLiveGap("rp-full-refresh-edge", code.CrossRepoDeadCodeCoverageStateTruncated, "gen-rp-full-refresh-edge"),
		coverageLiveGap("rp-legacy-no-action", code.CrossRepoDeadCodeCoverageStateTruncated, "gen-rp-legacy-no-action"),
	}
	reader := NewContentReader(db)
	for name, request := range map[string]code.CrossRepoDeadCodeCoverageRequest{
		"named statement": {
			RepositoryIDs: []string{
				"rp-full-refresh-only", "rp-full-refresh-nowm", "rp-inherit-refresh-only",
				"rp-full-refresh-edge", "rp-delta-refresh-only", "rp-legacy-no-action",
			},
			RequireActiveScope: true,
		},
		"all-repositories statement": {AllRepositories: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := reader.CrossRepoDeadCodeConsumerCoverage(ctx, request)
			if err != nil {
				t.Fatalf("CrossRepoDeadCodeConsumerCoverage() error = %v", err)
			}
			if !slices.Equal(got.Gaps, want) || got.IncompleteTruncated {
				t.Fatalf("gaps = %#v (truncated=%v), want %#v", got.Gaps, got.IncompleteTruncated, want)
			}
		})
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestCodeCallWholeFenceRefreshNotBlockedByOlderEdgeIntegration is the #3865
// regression: a repo's code-call whole-refresh intent and an OLDER-created edge
// intent for the same repo (the same projection partition in production) must not
// deadlock. The batch query and the in-memory fence rank refresh intents first
// (is_refresh_intent DESC), so the DB whole-fence must too — otherwise the
// refresh is fenced behind the older edge (created_at order) while the edge is
// fenced behind the refresh (refresh-first order), and both are held at terminal.
//
// Set ESHU_CODE_CALL_FENCE_PROOF_DSN to a throwaway Postgres to run it.
func TestCodeCallWholeFenceRefreshNotBlockedByOlderEdgeIntegration(t *testing.T) {
	dsn := os.Getenv("ESHU_CODE_CALL_FENCE_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_CODE_CALL_FENCE_PROOF_DSN to run the code-call fence regression")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	store := NewSharedIntentStore(SQLDB{DB: db})
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	const (
		scope   = "fence-proof-3865:scope"
		au      = "fence-proof-3865:repo"
		repo    = "fence-proof-3865:repo"
		run     = "fence-proof-3865:run"
		gen     = "fence-proof-3865:gen"
		edgeID  = "fence-proof-3865-edge"  // sorts before the whole id
		wholeID = "fence-proof-3865-whole" // created LATER than the edge
	)
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM shared_projection_intents WHERE scope_id = $1`, scope)
	})
	_, _ = db.ExecContext(ctx, `DELETE FROM shared_projection_intents WHERE scope_id = $1`, scope)

	base := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)
	insert := func(intentID, partitionKey string, createdAt time.Time, payload string) {
		t.Helper()
		_, err := db.ExecContext(ctx, `
INSERT INTO shared_projection_intents
    (intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
     repository_id, source_run_id, generation_id, payload, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10)`,
			intentID, reducer.DomainCodeCalls, partitionKey, scope, au,
			repo, run, gen, payload, createdAt)
		if err != nil {
			t.Fatalf("insert %s: %v", intentID, err)
		}
	}
	// The edge is OLDER than the repo-refresh — the exact #3865 ordering.
	insert(edgeID, "content-entity:e_a->content-entity:e_b", base, `{}`)
	insert(wholeID, "code-calls:v1:whole:"+repo, base.Add(time.Second),
		`{"action":"refresh","repo_id":"`+repo+`"}`)

	key := reducer.SharedProjectionAcceptanceKey{ScopeID: scope, AcceptanceUnitID: au, SourceRunID: run}

	wholeRow := reducer.SharedProjectionIntentRow{
		IntentID: wholeID, ProjectionDomain: reducer.DomainCodeCalls,
		PartitionKey: "code-calls:v1:whole:" + repo, ScopeID: scope, AcceptanceUnitID: au,
		RepositoryID: repo, SourceRunID: run, GenerationID: gen,
		Payload: map[string]any{"action": "refresh", "repo_id": repo}, CreatedAt: base.Add(time.Second),
	}
	blocked, err := store.CodeCallProjectionRowBlockedByRepoFence(ctx, key, wholeRow, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("fence(whole): %v", err)
	}
	if blocked {
		t.Fatal("whole-refresh is fenced behind its older edge — #3865 deadlock regression")
	}

	edgeRow := reducer.SharedProjectionIntentRow{
		IntentID: edgeID, ProjectionDomain: reducer.DomainCodeCalls,
		PartitionKey: "content-entity:e_a->content-entity:e_b", ScopeID: scope, AcceptanceUnitID: au,
		RepositoryID: repo, SourceRunID: run, GenerationID: gen,
		Payload: map[string]any{}, CreatedAt: base,
	}
	blocked, err = store.CodeCallProjectionRowBlockedByRepoFence(ctx, key, edgeRow, reducer.DomainCodeCalls)
	if err != nil {
		t.Fatalf("fence(edge): %v", err)
	}
	if !blocked {
		t.Fatal("edge must be fenced behind the repo refresh so the retract runs first")
	}
}

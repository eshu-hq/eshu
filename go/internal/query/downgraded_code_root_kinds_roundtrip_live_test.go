// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestDowngradedCodeRootKindsRoundTripLive is the #5376 P2 drift guard: it writes
// a downgraded (and a confirmed) verdict through the reducer's postgres store,
// then reads it back through the query's DowngradedCodeRootKinds. Because the
// store writes reducer.CodeRootVerdictDowngraded and the query's SQL predicate
// binds rubycontroller.VerdictDowngraded — the SAME shared constant — the
// round-trip returns exactly the downgraded entity. A rename of the verdict
// value on either side would make this return empty (fail-keep), so this test
// fails the moment the reader and writer drift.
func TestDowngradedCodeRootKindsRoundTripLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #5376 verdict round-trip proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// t.Cleanup (not defer) so the row-cleanup registered later runs BEFORE the
	// connection is closed (t.Cleanup is LIFO and runs after the test's defers).
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}

	suffix := fmt.Sprintf("p2-%d", time.Now().UnixNano())
	scopeID := "scope-" + suffix
	generationID := "gen-" + suffix
	repoID := "repo-" + suffix
	now := time.Now().UTC()

	// Clean up every seeded/written row so a persistent dev DB never accumulates
	// pollution across runs (parity with the #5376 P1 live tests).
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		for _, q := range []string{
			`DELETE FROM code_root_verdicts WHERE scope_id=$1`,
			`DELETE FROM code_reachability_repository_watermarks WHERE scope_id=$1`,
			`DELETE FROM scope_generations WHERE scope_id=$1`,
			`DELETE FROM ingestion_scopes WHERE scope_id=$1`,
		} {
			if _, err := db.ExecContext(cctx, q, scopeID); err != nil {
				t.Logf("cleanup %q: %v", q, err)
			}
		}
	})

	seed := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	seed(`INSERT INTO ingestion_scopes
	  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
	   observed_at, ingested_at, status, active_generation_id, payload)
	  VALUES ($1,'repository','git',$1,'git',$1,$2,$2,'active',$3, jsonb_build_object('repo_id',$4::text))`,
		scopeID, now, generationID, repoID)
	seed(`INSERT INTO scope_generations
	  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
	  VALUES ($1,$2,'manual',$3,$3,'active',$3)`, generationID, scopeID, now)

	store := storagepostgres.NewCodeReachabilityStore(storagepostgres.SQLDB{DB: db})
	verdictRow := func(entityID, verdict, reason string) reducer.CodeRootVerdictRow {
		return reducer.CodeRootVerdictRow{
			ScopeID: scopeID, GenerationID: generationID, RepositoryID: repoID,
			EntityID: entityID, RootKind: reducer.CodeRootKindRubyRailsControllerAction,
			Verdict: verdict, Basis: reducer.CodeRootVerdictBasis{Reason: reason},
			ObservedAt: now, UpdatedAt: now,
		}
	}
	if err := store.ReplaceRepositoryRows(ctx, scopeID, generationID, repoID, nil, []reducer.CodeRootVerdictRow{
		verdictRow("e:down", reducer.CodeRootVerdictDowngraded, "rejected_framework_base"),
		verdictRow("e:conf", reducer.CodeRootVerdictConfirmed, "accepted"),
	}, now, false); err != nil {
		t.Fatalf("ReplaceRepositoryRows: %v", err)
	}

	reader := NewContentReader(db)
	got, err := reader.DowngradedCodeRootKinds(ctx, repoID, []string{"e:down", "e:conf"})
	if err != nil {
		t.Fatalf("DowngradedCodeRootKinds: %v", err)
	}
	if _, ok := got["e:down"][reducer.CodeRootKindRubyRailsControllerAction]; !ok {
		t.Fatalf("downgraded entity not returned — reader/writer verdict-value drift: got %#v", got)
	}
	if _, ok := got["e:conf"]; ok {
		t.Fatalf("confirmed entity must NOT be returned by the downgraded query: got %#v", got)
	}
}

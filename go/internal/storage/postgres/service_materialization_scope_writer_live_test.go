// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/servicecatalog"
)

// TestServiceMaterializationWriterKeepsScopedLineagesLive drives the production
// PostgresServiceMaterializationWriter against the bootstrapped schema (#6475):
//
//   - two ingestion scopes writing identical evidence for one service id each
//     commit and keep their own active generation;
//   - a changed write in one scope supersedes only that scope's generation;
//   - an unattributed legacy active row (scope_id NULL) for the same service id
//     is never superseded;
//   - concurrent commits from different scopes for one service id do not
//     conflict (disjoint rows under the (scope_id, service_id) index).
//
// Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test -tags integration ./internal/storage/postgres \
//	  -run TestServiceMaterializationWriterKeepsScopedLineagesLive -count=1 -v
func TestServiceMaterializationWriterKeepsScopedLineagesLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	db, _ := openServiceLineageSchemaLive(ctx, t, "eshu_6475_lineage_writer")
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO service_materialization_generations
  (generation_id, service_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-legacy-unattributed', 'svc-shared', 'service_catalog_correlation', now(), now(), 'active', now())`); err != nil {
		t.Fatalf("seed unattributed legacy active generation: %v", err)
	}

	writer := servicecatalog.PostgresServiceMaterializationWriter{
		DB:  servicecatalog.ServiceMaterializationSQLBeginner{DB: db},
		Now: time.Now,
	}
	write := func(scopeID, tier string) servicecatalog.ServiceMaterializationWriteResult {
		t.Helper()
		result, err := writer.WriteServiceMaterialization(ctx, servicecatalog.ServiceMaterializationWrite{
			IntentID:  "intent-" + scopeID + "-" + tier,
			ScopeID:   scopeID,
			ServiceID: "svc-shared",
			Ownership: []servicecatalog.ServiceOwnershipEvidence{
				{OwnerRef: "team-a", Payload: map[string]any{"tier": tier}},
			},
		})
		if err != nil {
			t.Fatalf("write scope %s tier %s: %v", scopeID, tier, err)
		}
		return result
	}

	scopeA := write("scope-a", "gold")
	scopeB := write("scope-b", "gold")
	if !scopeA.Committed || !scopeB.Committed || scopeA.GenerationID == scopeB.GenerationID {
		t.Fatalf("scope writes = A %+v, B %+v; want two committed generations with distinct ids", scopeA, scopeB)
	}
	if len(scopeB.SupersededIDs) != 0 {
		t.Fatalf("scope B superseded %v, want nothing", scopeB.SupersededIDs)
	}
	changedA := write("scope-a", "platinum")
	if len(changedA.SupersededIDs) != 1 || changedA.SupersededIDs[0] != scopeA.GenerationID {
		t.Fatalf("changed scope A write superseded %v, want exactly [%s]", changedA.SupersededIDs, scopeA.GenerationID)
	}

	assertServiceLineageActiveLive(ctx, t, db, map[string]string{
		"scope-a":      changedA.GenerationID,
		"scope-b":      scopeB.GenerationID,
		"unattributed": "gen-legacy-unattributed",
	})

	// Concurrent commits from two new scopes for the same service id.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, scopeID := range []string{"scope-c", "scope-d"} {
		wg.Add(1)
		go func(scopeID string) {
			defer wg.Done()
			_, err := writer.WriteServiceMaterialization(ctx, servicecatalog.ServiceMaterializationWrite{
				IntentID: "intent-" + scopeID, ScopeID: scopeID, ServiceID: "svc-shared",
				Ownership: []servicecatalog.ServiceOwnershipEvidence{{OwnerRef: "team-a", Payload: map[string]any{"tier": "gold"}}},
			})
			errs <- err
		}(scopeID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent write from a distinct scope failed: %v", err)
		}
	}
	var actives int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM service_materialization_generations WHERE service_id = 'svc-shared' AND status = 'active'",
	).Scan(&actives); err != nil {
		t.Fatalf("count actives: %v", err)
	}
	if actives != 5 {
		t.Fatalf("active generations for svc-shared = %d, want 5 (scopes a, b, c, d and the unattributed legacy row)", actives)
	}
}

// assertServiceLineageActiveLive requires exactly one active generation per
// listed scope ("unattributed" means scope_id IS NULL) with the given id.
func assertServiceLineageActiveLive(ctx context.Context, t *testing.T, db *sql.DB, want map[string]string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
SELECT COALESCE(scope_id, 'unattributed'), generation_id
FROM service_materialization_generations
WHERE service_id = 'svc-shared' AND status = 'active'`)
	if err != nil {
		t.Fatalf("read active generations: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]string{}
	for rows.Next() {
		var scopeID, generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			t.Fatalf("scan active generation: %v", err)
		}
		if previous, dup := got[scopeID]; dup {
			t.Fatalf("scope %s holds two active generations: %s and %s", scopeID, previous, generationID)
		}
		got[scopeID] = generationID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate active generations: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("active generations = %v, want %v", got, want)
	}
	for scopeID, generationID := range want {
		if got[scopeID] != generationID {
			t.Errorf("active generation for %s = %q, want %q", scopeID, got[scopeID], generationID)
		}
	}
}

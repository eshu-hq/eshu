// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openEshuSearchVectorContentionLiveDB skips the test unless
// ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE=1 and ESHU_POSTGRES_DSN
// are both set, then opens a pool sized for maxConns concurrent callers,
// applies the bootstrap schema, and returns a 10-minute context.
//
// sqlDB.Close is registered via t.Cleanup here, before the caller registers
// any fixture-delete cleanup. t.Cleanup callbacks run in LIFO order (and
// after the test function's own deferred calls), so a caller-registered
// delete callback added after this call returns runs first, against a still
// -open connection, and this Close callback runs last. Registering Close via
// a bare `defer` instead -- the bug PR #6039 review flagged -- would close
// the pool before any t.Cleanup callback ever ran, silently failing every
// cleanup query on a closed database and leaking every seeded row.
func openEshuSearchVectorContentionLiveDB(t *testing.T, maxConns int) (SQLDB, *sql.DB, context.Context) {
	t.Helper()
	if os.Getenv("ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_SCOPE_STATE_CAS_CONTENTION_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Every worker needs its own connection or the goroutines serialize in the
	// pool and the race never actually happens in the server.
	sqlDB.SetMaxOpenConns(maxConns)
	sqlDB.SetMaxIdleConns(maxConns)
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	if err := ApplyBootstrap(ctx, db); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	return db, sqlDB, ctx
}

// registerEshuSearchVectorContentionCleanup registers a t.Cleanup callback
// that deletes every scope ID appended to *seededScopeIDs by the time cleanup
// runs, using a context independent of the test's own (already-expired-by-
// then) context. A single DELETE FROM ingestion_scopes is sufficient: every
// other table this fixture writes -- scope_generations,
// eshu_search_document_projection_state, eshu_search_vector_scope_state --
// declares its scope_id (or a generation_id that itself cascades from
// scope_id) FOREIGN KEY ... ON DELETE CASCADE against ingestion_scopes, so
// deleting the parent row cascades through all of them. Errors are logged
// rather than discarded so a real cleanup failure (e.g. an added table that
// does not cascade) is visible instead of silently leaking fixture rows into
// the shared proof database.
func registerEshuSearchVectorContentionCleanup(t *testing.T, sqlDB *sql.DB, seededScopeIDs *[]string) {
	t.Helper()
	t.Cleanup(func() {
		cleanCtx := context.Background()
		for _, scopeID := range *seededScopeIDs {
			if _, err := sqlDB.ExecContext(cleanCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID); err != nil {
				t.Logf("cleanup: delete ingestion_scope %s: %v", scopeID, err)
			}
		}
	})
}

// seedEshuSearchVectorContentionFixture inserts one ingestion_scope, one
// scope_generation, and a 'ready' eshu_search_document_projection_state row
// at projectionRevision so BeginBuilding's join predicate (it only issues a
// fence when the document projection is already 'ready' at the same
// revision) is satisfied without exercising the projection pipeline itself.
// It returns the seeded scope and generation IDs.
func seedEshuSearchVectorContentionFixture(
	t *testing.T,
	ctx context.Context,
	sqlDB *sql.DB,
	label string,
	projectionRevision int64,
	now time.Time,
) (scopeID, genID string) {
	t.Helper()
	scopeID = fmt.Sprintf("%s:scope", label)
	genID = fmt.Sprintf("%s:gen", label)

	if _, err := sqlDB.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'active', $3::text,
		        jsonb_build_object('repo_id', $1::text))
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now, genID,
	); err != nil {
		t.Fatalf("%s: insert ingestion_scope: %v", label, err)
	}
	if _, err := sqlDB.ExecContext(
		ctx, `
		INSERT INTO scope_generations
		  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
		VALUES ($1, $2, 'manual', $3, $3, 'active', $3)
		ON CONFLICT (generation_id) DO NOTHING`,
		genID, scopeID, now,
	); err != nil {
		t.Fatalf("%s: insert scope_generation: %v", label, err)
	}
	if _, err := sqlDB.ExecContext(
		ctx, `
		INSERT INTO eshu_search_document_projection_state
		  (scope_id, generation_id, projection_revision, build_fence, state, document_count, updated_at)
		VALUES ($1, $2, $3, 1, 'ready', 1, $4)
		ON CONFLICT (scope_id, generation_id) DO UPDATE SET
		  projection_revision = EXCLUDED.projection_revision,
		  state = 'ready',
		  updated_at = EXCLUDED.updated_at`,
		scopeID, genID, projectionRevision, now,
	); err != nil {
		t.Fatalf("%s: seed projection state: %v", label, err)
	}
	return scopeID, genID
}

// runReleasedTogether launches n goroutines, each invoking fn(idx), and
// releases all of them together after every one has signaled it is parked at
// the release gate.
//
// A bare sync.WaitGroup Add(1)/Wait()/Done() "gate" (or an unbuffered
// close(start) channel) only stops a goroutine from starting EARLY; it does
// not confirm any goroutine has actually reached the gate before the release
// fires. A goroutine the Go scheduler is slow to run can call Wait() (or
// receive from the already-closed channel) well after release and simply
// proceed alone, moments after the others already finished -- weakening the
// server-side interleaving a contention proof depends on. The ready
// WaitGroup here blocks the release itself until all n goroutines have
// checked in, so the release genuinely happens once and reaches every
// goroutine at (as close as Go's scheduler allows to) the same moment.
func runReleasedTogether(n int, fn func(idx int)) {
	var ready sync.WaitGroup
	ready.Add(n)
	var release sync.WaitGroup
	release.Add(1)
	var done sync.WaitGroup
	done.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer done.Done()
			ready.Done()
			release.Wait()
			fn(idx)
		}(i)
	}
	ready.Wait()
	release.Done()
	done.Wait()
}

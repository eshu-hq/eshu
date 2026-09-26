// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Real-Postgres proofs for #6740: the corpus fence and the by-repos resolved
// read must come from one statement snapshot. With two statements, a foreign
// scope that retires its generation after the fence and re-activates before
// the recheck makes both checks pass while the read misses its rows.
//
// The fence is corpus-wide, so each test bootstraps the real schema into its
// own Postgres schema; rows other tests leave in the shared database would
// otherwise decide the verdict.
//
// Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run CorpusFenceSnapshot -count=1

// openCorpusFenceSnapshotSchema bootstraps an isolated schema and returns a
// pool whose connections resolve unqualified tables to it.
func openCorpusFenceSnapshotSchema(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the corpus-fence snapshot proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	schema := fmt.Sprintf("eshu_6740_fence_%d", time.Now().UnixNano())
	admin := openCorpusFenceSnapshotDB(t, dsn)
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	database := openCorpusFenceSnapshotDB(t, parsed.String())
	database.SetMaxOpenConns(4)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("ApplyBootstrap(isolated schema): %v", err)
	}
	return ctx, database
}

func openCorpusFenceSnapshotDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatalf("ping Postgres: %v", err)
	}
	return database
}

// seedCorpusFenceScope creates one active scope whose current relationship
// generation is active and carries one resolved row into repo-w. The row's
// rationale names its generation version so a reader can tell old from new.
func seedCorpusFenceScope(t *testing.T, ctx context.Context, database *sql.DB, scope, generation, version string) {
	t.Helper()
	now := time.Now().UTC()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{
			`INSERT INTO ingestion_scopes
		   (scope_id, scope_kind, source_system, source_key, collector_kind,
		    partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		  VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
			[]any{scope, now, generation},
		},
		{`INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
		  VALUES ($1, $2, 'active', $3, $3)`, []any{generation, scope, now}},
		{corpusFenceRowInsertSQL, []any{scope + ":" + generation, generation, "repo-" + scope, version}},
	} {
		if _, err := database.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed scope %s: %v", scope, err)
		}
	}
}

const corpusFenceRowInsertSQL = `
INSERT INTO resolved_relationships
  (resolved_id, generation_id, source_repo_id, target_repo_id, relationship_type,
   confidence, evidence_count, rationale, resolution_source, details)
VALUES ($1, $2, $3, 'repo-w', 'DEPLOYS_FROM', 0.9, 1, $4, 'inferred', '{}'::jsonb)`

// corpusFenceRationales returns the sorted rationales of a read, which name
// the generation version each row came from.
func corpusFenceRationales(rows []relationships.ResolvedRelationship) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Rationale)
	}
	slices.Sort(out)
	return out
}

// TestCorpusFenceSnapshotAdvanceAndCompleteIsNeverMixed pins the design's
// two-session contract: while another session holds an uncommitted
// advance-and-complete of scope-x, the fused read returns complete=true with
// exactly the OLD generation's rows; after the commit it returns complete=true
// with exactly the NEW rows; a committed retirement returns complete=false and
// no rows.
func TestCorpusFenceSnapshotAdvanceAndCompleteIsNeverMixed(t *testing.T) {
	ctx, database := openCorpusFenceSnapshotSchema(t)
	seedCorpusFenceScope(t, ctx, database, "scope-x", "x1", "x-v1")
	seedCorpusFenceScope(t, ctx, database, "scope-y", "y1", "y-v1")
	store := NewRelationshipStore(SQLDB{DB: database})

	read := func(label string) ([]string, bool) {
		t.Helper()
		rows, complete, err := store.GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-w"})
		if err != nil {
			t.Fatalf("%s: fused read error = %v", label, err)
		}
		return corpusFenceRationales(rows), complete
	}

	advance, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin advance: %v", err)
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE ingestion_scopes SET active_generation_id = 'x2' WHERE scope_id = 'scope-x'`, nil},
		{`UPDATE relationship_generations SET status = 'superseded' WHERE scope = 'scope-x' AND generation_id <> 'x2'`, nil},
		{`INSERT INTO relationship_generations (generation_id, scope, status, created_at, activated_at)
		  VALUES ('x2', 'scope-x', 'active', now(), now())`, nil},
		{corpusFenceRowInsertSQL, []any{"scope-x:x2", "x2", "repo-scope-x", "x-v2"}},
	} {
		if _, err := advance.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			_ = advance.Rollback()
			t.Fatalf("advance-and-complete: %v", err)
		}
	}

	if got, complete := read("during uncommitted advance"); !complete || !slices.Equal(got, []string{"x-v1", "y-v1"}) {
		t.Fatalf("during uncommitted advance: (%v, complete=%v), want ([x-v1 y-v1], true)", got, complete)
	}
	if err := advance.Commit(); err != nil {
		t.Fatalf("commit advance: %v", err)
	}
	if got, complete := read("after commit"); !complete || !slices.Equal(got, []string{"x-v2", "y-v1"}) {
		t.Fatalf("after commit: (%v, complete=%v), want ([x-v2 y-v1], true)", got, complete)
	}

	if _, err := database.ExecContext(ctx, `UPDATE relationship_generations SET status = 'pending' WHERE generation_id = 'x2'`); err != nil {
		t.Fatalf("retire x2: %v", err)
	}
	if got, complete := read("after retirement"); complete || len(got) != 0 {
		t.Fatalf("after retirement: (%v, complete=%v), want ([], false)", got, complete)
	}
}

// TestCorpusFenceSnapshotConcurrentRetireAndReactivate drives the #6740
// residual concurrently: a writer repeatedly retires scope-x's generation
// (one commit) and re-activates it with a new row version (a second commit)
// while readers run the fused read. Every complete=true result must carry
// exactly one scope-x row and the scope-y row; a two-statement fence would
// admit results with scope-x missing. Rows are never returned with
// complete=false.
func TestCorpusFenceSnapshotConcurrentRetireAndReactivate(t *testing.T) {
	ctx, database := openCorpusFenceSnapshotSchema(t)
	seedCorpusFenceScope(t, ctx, database, "scope-x", "x1", "x-v0")
	seedCorpusFenceScope(t, ctx, database, "scope-y", "y1", "y-v1")
	store := NewRelationshipStore(SQLDB{DB: database})

	const cycles = 200
	const readers = 2
	stop := make(chan struct{})
	writerErr := make(chan error, 1)
	go func() {
		defer close(stop)
		for i := 1; i <= cycles; i++ {
			if _, err := database.ExecContext(ctx, `UPDATE relationship_generations SET status = 'pending' WHERE generation_id = 'x1'`); err != nil {
				writerErr <- fmt.Errorf("retire cycle %d: %w", i, err)
				return
			}
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				writerErr <- fmt.Errorf("begin reactivate cycle %d: %w", i, err)
				return
			}
			if _, err := tx.ExecContext(ctx, `UPDATE resolved_relationships SET rationale = $1 WHERE generation_id = 'x1'`, fmt.Sprintf("x-v%d", i)); err != nil {
				_ = tx.Rollback()
				writerErr <- fmt.Errorf("rewrite cycle %d: %w", i, err)
				return
			}
			if _, err := tx.ExecContext(ctx, `UPDATE relationship_generations SET status = 'active' WHERE generation_id = 'x1'`); err != nil {
				_ = tx.Rollback()
				writerErr <- fmt.Errorf("reactivate cycle %d: %w", i, err)
				return
			}
			if err := tx.Commit(); err != nil {
				writerErr <- fmt.Errorf("commit cycle %d: %w", i, err)
				return
			}
		}
	}()

	var mu sync.Mutex
	var completeReads, incompleteReads int
	var violations []string
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				rows, complete, err := store.GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-w"})
				got := corpusFenceRationales(rows)
				mu.Lock()
				switch {
				case err != nil:
					violations = append(violations, "read error: "+err.Error())
				case !complete && len(got) != 0:
					violations = append(violations, fmt.Sprintf("complete=false with rows %v", got))
				case !complete:
					incompleteReads++
				case len(got) != 2 || !strings.HasPrefix(got[0], "x-v") || got[1] != "y-v1":
					violations = append(violations, fmt.Sprintf("complete=true with mixed or partial rows %v", got))
				default:
					completeReads++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-writerErr:
		t.Fatalf("writer: %v", err)
	default:
	}
	if len(violations) > 0 {
		t.Fatalf("fused read violated the snapshot contract %d times; first: %s", len(violations), violations[0])
	}
	if completeReads == 0 || incompleteReads == 0 {
		t.Fatalf("interleaving not exercised: complete reads = %d, incomplete reads = %d; both must be non-zero", completeReads, incompleteReads)
	}
	t.Logf("writer cycles = %d, complete reads = %d, incomplete reads = %d, violations = 0", cycles, completeReads, incompleteReads)
}

// interleavingQueryer commits one writer step before each read statement after
// the first, so any gap between two statements the store issues is exactly
// where a foreign scope's advance lands. Postgres gives each statement its own
// snapshot under READ COMMITTED; the pg_sleep shim in the #6740 evidence note
// proves a commit INSIDE one statement is invisible to it, so this wrapper
// covers the only remaining gap: between statements.
type interleavingQueryer struct {
	SQLDB
	steps   []func(context.Context) error
	queries int
	stepErr error
}

func (q *interleavingQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if q.queries > 0 && q.queries-1 < len(q.steps) && q.stepErr == nil {
		q.stepErr = q.steps[q.queries-1](ctx)
	}
	q.queries++
	return q.SQLDB.QueryContext(ctx, query, args...)
}

// TestCorpusFenceSnapshotRetireAndReactivateBetweenStatements is the
// deterministic form of the #6740 residual: scope-x retires after the first
// statement the read issues and re-activates after the second. A
// fence/read/recheck composition then reports complete=true with scope-x's
// row missing; the fused read issues one statement, so it reports the state
// that statement saw, whole.
func TestCorpusFenceSnapshotRetireAndReactivateBetweenStatements(t *testing.T) {
	ctx, database := openCorpusFenceSnapshotSchema(t)
	seedCorpusFenceScope(t, ctx, database, "scope-x", "x1", "x-v1")
	seedCorpusFenceScope(t, ctx, database, "scope-y", "y1", "y-v1")
	exec := func(sqlText string) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := database.ExecContext(ctx, sqlText)
			return err
		}
	}
	queryer := &interleavingQueryer{
		SQLDB: SQLDB{DB: database},
		steps: []func(context.Context) error{
			exec(`UPDATE relationship_generations SET status = 'pending' WHERE generation_id = 'x1'`),
			exec(`UPDATE relationship_generations SET status = 'active' WHERE generation_id = 'x1'`),
		},
	}
	store := NewRelationshipStore(queryer)

	rows, complete, err := store.GetResolvedRelationshipsForReposWithCorpusFence(ctx, []string{"repo-w"})
	if err != nil {
		t.Fatalf("fused read error = %v", err)
	}
	if queryer.stepErr != nil {
		t.Fatalf("writer step: %v", queryer.stepErr)
	}
	got := corpusFenceRationales(rows)
	if complete && !slices.Equal(got, []string{"x-v1", "y-v1"}) {
		t.Fatalf("fused read = (%v, complete=true), want both scopes' rows: a passing verdict paired with a partial set is the #6740 residual", got)
	}
	if !complete && len(got) != 0 {
		t.Fatalf("fused read = (%v, complete=false), want no rows with an incomplete verdict", got)
	}
	t.Logf("statements issued = %d, complete = %v, rows = %v", queryer.queries, complete, got)
}

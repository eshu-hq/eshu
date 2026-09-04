// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// codeReachabilityTwoColumnIndexDDL is the statement #5167 batch 1's first
// release shipped as migration 100. That file is deleted -- a create the next
// migration drops rebuilds the index on every bootstrap -- so the DDL is
// repeated here to reconstruct the state those installs are actually in, which
// is the state migration 102's drop has to converge.
const codeReachabilityTwoColumnIndexDDL = `
CREATE INDEX CONCURRENTLY IF NOT EXISTS code_reachability_entity_repository_idx
    ON code_reachability_rows (entity_id, repository_id)`

// TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive proves the
// property the migrations directory's replay model needs and no unit test can
// see: a second bootstrap over a populated store that already holds the
// intended index does no index work at all.
//
// Every file under migrations/ is Exec'd on every bootstrap in filename order,
// with no ledger of what already ran (BootstrapDefinitions and ApplyDefinitions
// in schema.go). So a superseded index left in the tree as a create that a
// later file drops is not a one-time cost: the drop clears the name, the next
// startup's IF NOT EXISTS no longer skips, and the index is rebuilt
// concurrently over a populated table and dropped again, forever.
//
// The store starts where an install of the earlier release stands -- rows on
// disk and the two-column index built -- so the first pass has to converge it,
// and the second has to touch nothing. The assertion is per definition: the set
// of indexes on code_reachability_rows and each one's relfilenode are read
// before and after every statement of the second pass, so a definition that
// builds an index the next definition drops fails here even though the state
// either side of the whole pass is identical.
//
// Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test -tags integration ./internal/storage/postgres \
//	  -run TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive -count=1
func TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive(t *testing.T) {
	const schema = "eshu_5167_code_reachability_index_replay"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live reachability index replay proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	// More than one connection: the bootstrap path runs each definition on a
	// dedicated connection it checks out itself, and the snapshots below run
	// alongside it.
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })

	tables, indexes := codeReachabilityReplayDefinitions(t)
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, tables); err != nil {
		t.Fatalf("apply reachability table definitions: %v", err)
	}
	seedCodeReachabilityReplayRows(ctx, t, db)
	if _, err := db.ExecContext(ctx, codeReachabilityTwoColumnIndexDDL); err != nil {
		t.Fatalf("build the earlier release's two-column index: %v", err)
	}

	before := codeReachabilityIndexState(ctx, t, db, schema)
	if _, ok := before["code_reachability_entity_repository_idx"]; !ok {
		t.Fatal("the earlier release's two-column index is missing before the first bootstrap; the fixture does not stand where the installs it converges stand")
	}

	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, indexes); err != nil {
		t.Fatalf("apply reachability index definitions, first pass: %v", err)
	}
	converged := codeReachabilityIndexState(ctx, t, db, schema)
	if _, ok := converged["code_reachability_entity_repository_scope_generation_idx"]; !ok {
		t.Fatalf("the four-column walk index is missing after the first bootstrap; indexes = %v", codeReachabilityIndexNames(converged))
	}
	if _, ok := converged["code_reachability_entity_repository_idx"]; ok {
		t.Fatal("the two-column index survived the first bootstrap; migration 102 did not converge the earlier release's state")
	}

	recorder := &codeReachabilityIndexRecorder{db: SQLDB{DB: db}, t: t, schema: schema}
	if err := ApplyDefinitions(ctx, recorder, indexes); err != nil {
		t.Fatalf("apply reachability index definitions, second pass: %v", err)
	}
	for _, change := range recorder.changes {
		t.Errorf("the second bootstrap changed the indexes on code_reachability_rows: %s", change)
	}

	after := codeReachabilityIndexState(ctx, t, db, schema)
	if len(after) != len(converged) {
		t.Fatalf("indexes after the second bootstrap = %v, want %v", codeReachabilityIndexNames(after), codeReachabilityIndexNames(converged))
	}
	for name, relfilenode := range converged {
		got, ok := after[name]
		if !ok {
			t.Errorf("index %s disappeared during the second bootstrap", name)
			continue
		}
		if got != relfilenode {
			t.Errorf("index %s relfilenode = %d after the second bootstrap, want %d; it was rebuilt", name, got, relfilenode)
		}
	}
}

// codeReachabilityIndexRecorder applies bootstrap definitions through the
// production executor while reading the index state on code_reachability_rows
// either side of each statement, so a definition whose effect a later
// definition undoes is still caught.
type codeReachabilityIndexRecorder struct {
	db      SQLDB
	t       *testing.T
	schema  string
	changes []string
}

// ExecContext runs one definition through the plain executor path.
func (recorder *codeReachabilityIndexRecorder) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return recorder.record(ctx, query, func() (sql.Result, error) {
		return recorder.db.ExecContext(ctx, query, args...)
	})
}

// execContextWithLockTimeout runs one definition through the same bounded
// lock-timeout path a real bootstrap uses, so this proof exercises the
// production statement path rather than a simplified one.
func (recorder *codeReachabilityIndexRecorder) execContextWithLockTimeout(
	ctx context.Context,
	query string,
	lockTimeout time.Duration,
) (sql.Result, error) {
	return recorder.record(ctx, query, func() (sql.Result, error) {
		return recorder.db.execContextWithLockTimeout(ctx, query, lockTimeout)
	})
}

func (recorder *codeReachabilityIndexRecorder) record(
	ctx context.Context,
	query string,
	exec func() (sql.Result, error),
) (sql.Result, error) {
	before := codeReachabilityIndexState(ctx, recorder.t, recorder.db.DB, recorder.schema)
	result, err := exec()
	after := codeReachabilityIndexState(ctx, recorder.t, recorder.db.DB, recorder.schema)
	for name, relfilenode := range after {
		previous, ok := before[name]
		switch {
		case !ok:
			recorder.changes = append(recorder.changes,
				fmt.Sprintf("%q built index %s", codeReachabilityStatementSummary(query), name))
		case previous != relfilenode:
			recorder.changes = append(recorder.changes,
				fmt.Sprintf("%q rebuilt index %s (relfilenode %d -> %d)",
					codeReachabilityStatementSummary(query), name, previous, relfilenode))
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			recorder.changes = append(recorder.changes,
				fmt.Sprintf("%q dropped index %s", codeReachabilityStatementSummary(query), name))
		}
	}
	return result, err
}

// codeReachabilityStatementSummary reduces one migration file to its executable
// statement so a failure names the statement rather than pages of comment.
func codeReachabilityStatementSummary(query string) string {
	for _, line := range strings.Split(query, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return trimmed
	}
	return strings.TrimSpace(query)
}

// codeReachabilityIndexState maps every index on code_reachability_rows to its
// relfilenode, which a rebuild changes and a plain reapply does not.
func codeReachabilityIndexState(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	schema string,
) map[string]int64 {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
SELECT index_class.relname, index_class.relfilenode
FROM pg_index AS index_entry
JOIN pg_class AS index_class ON index_class.oid = index_entry.indexrelid
JOIN pg_class AS table_class ON table_class.oid = index_entry.indrelid
JOIN pg_namespace AS namespace ON namespace.oid = table_class.relnamespace
WHERE namespace.nspname = $1
  AND table_class.relname = 'code_reachability_rows'
`, schema)
	if err != nil {
		t.Fatalf("read reachability index state: %v", err)
	}
	defer func() { _ = rows.Close() }()

	state := map[string]int64{}
	for rows.Next() {
		var name string
		var relfilenode int64
		if err := rows.Scan(&name, &relfilenode); err != nil {
			t.Fatalf("scan reachability index state: %v", err)
		}
		state[name] = relfilenode
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reachability index state: %v", err)
	}
	return state
}

// codeReachabilityIndexNames renders an index state for a failure message.
func codeReachabilityIndexNames(state map[string]int64) []string {
	names := make([]string, 0, len(state))
	for name := range state {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// codeReachabilityReplayDefinitions returns the shipped bootstrap definitions
// this proof needs, split into the tables it seeds and the index migrations it
// replays. Reading them from BootstrapDefinitions is what stops the proof
// passing against DDL no deployment applies.
func codeReachabilityReplayDefinitions(t *testing.T) (tables, indexes []Definition) {
	t.Helper()

	tableNames := []string{"ingestion_scopes", "scope_generations", "code_reachability"}
	byName := map[string]Definition{}
	for _, definition := range BootstrapDefinitions() {
		byName[definition.Name] = definition
	}
	for _, name := range tableNames {
		definition, ok := byName[name]
		if !ok {
			t.Fatalf("bootstrap definition %q is missing", name)
		}
		tables = append(tables, definition)
	}
	// DISCOVERED, never listed. A fixed list of definition names would apply
	// only the migrations this proof already knows about, so re-adding a create
	// of the superseded index -- the exact defect this test exists for -- would
	// leave the applied set unchanged and the proof green. Every definition
	// naming the walk index family is applied instead, in bootstrap order, so a
	// new one is picked up whether or not anybody updates this test.
	for _, definition := range BootstrapDefinitions() {
		if strings.Contains(definition.SQL, codeReachabilityWalkIndexFamily) {
			indexes = append(indexes, definition)
		}
	}
	// A rename that emptied the scan would make the whole proof vacuous.
	if len(indexes) < 2 {
		t.Fatalf("found %d definition(s) naming %s, want at least the create and the drop",
			len(indexes), codeReachabilityWalkIndexFamily)
	}
	return tables, indexes
}

// codeReachabilityWalkIndexFamily is the shared name prefix of the walk index
// and the two-column index it supersedes, which is how this proof finds every
// migration that acts on either without being told their names.
const codeReachabilityWalkIndexFamily = "code_reachability_entity_repository"

// seedCodeReachabilityReplayRows populates the store so a rebuild is real index
// work rather than a metadata edit on an empty table.
func seedCodeReachabilityReplayRows(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-1', 'repository', 'git', 'key-1', 'code', 'partition-1', now(), now(), 'active', 'gen-active');
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-active', 'scope-1', 'sync', now(), now(), 'active', now());
INSERT INTO code_reachability_rows
  (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state,
   confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at)
SELECT 'scope-1', 'gen-active',
       'repo-' || lpad((value % 50)::text, 4, '0'),
       'caller-' || lpad(value::text, 6, '0'),
       'entity-' || lpad((value % 500)::text, 4, '0'),
       1, 'reachable', 0.95, 'symbol_exact',
       '["CALLS"]'::jsonb, '["Function"]'::jsonb, now(), now()
FROM generate_series(1, 20000) AS value;
ANALYZE code_reachability_rows;
`); err != nil {
		t.Fatalf("seed populated reachability store: %v", err)
	}
}

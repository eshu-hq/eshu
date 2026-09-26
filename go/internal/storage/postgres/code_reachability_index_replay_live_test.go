// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
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
// migration drops rebuilds the index on an untracked replay -- so the DDL is
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
// This test calls ApplyDefinitions directly to simulate an untracked existing
// database's first replay. A superseded index left in the tree as a create
// that a later file drops still wastes a build before receipts exist: the drop
// clears the name, the next replay's IF NOT EXISTS no longer skips, and the
// index is built over a populated table and dropped again.
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

	before := migrationIndexStateLive(ctx, t, db, schema, "code_reachability_rows")
	if _, ok := before["code_reachability_entity_repository_idx"]; !ok {
		t.Fatal("the earlier release's two-column index is missing before the first bootstrap; the fixture does not stand where the installs it converges stand")
	}

	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, indexes); err != nil {
		t.Fatalf("apply reachability index definitions, first pass: %v", err)
	}
	converged := migrationIndexStateLive(ctx, t, db, schema, "code_reachability_rows")
	if _, ok := converged["code_reachability_entity_repository_scope_generation_idx"]; !ok {
		t.Fatalf("the four-column walk index is missing after the first bootstrap; indexes = %v", migrationIndexNamesLive(converged))
	}
	if _, ok := converged["code_reachability_entity_repository_idx"]; ok {
		t.Fatal("the two-column index survived the first bootstrap; migration 102 did not converge the earlier release's state")
	}

	recorder := &migrationIndexReplayRecorder{db: SQLDB{DB: db}, t: t, schema: schema, table: "code_reachability_rows"}
	if err := ApplyDefinitions(ctx, recorder, indexes); err != nil {
		t.Fatalf("apply reachability index definitions, second pass: %v", err)
	}
	for _, change := range recorder.changes {
		t.Errorf("the second bootstrap changed the indexes on code_reachability_rows: %s", change)
	}

	after := migrationIndexStateLive(ctx, t, db, schema, "code_reachability_rows")
	if len(after) != len(converged) {
		t.Fatalf("indexes after the second bootstrap = %v, want %v", migrationIndexNamesLive(after), migrationIndexNamesLive(converged))
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

// migrationIndexNamesLive renders an index state for a failure message.
func migrationIndexNamesLive(state map[string]int64) []string {
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
	// naming one of the reachability index families is applied instead, in
	// bootstrap order, so a new one is picked up whether or not anybody updates
	// this test.
	for _, definition := range BootstrapDefinitions() {
		if slices.ContainsFunc(codeReachabilityIndexFamilies, func(family string) bool {
			return strings.Contains(definition.SQL, family)
		}) {
			indexes = append(indexes, definition)
		}
	}
	// A rename that emptied the scan would make the whole proof vacuous. Three,
	// because the walk family carries a create and a drop and the page-rank
	// family carries a create.
	if len(indexes) < 3 {
		t.Fatalf("found %d definition(s) naming %v, want at least the walk index's create and drop plus the page-rank index's create",
			len(indexes), codeReachabilityIndexFamilies)
	}
	return tables, indexes
}

// codeReachabilityWalkIndexFamily is the shared name prefix of the walk index
// and the two-column index it supersedes, which is how this proof finds every
// migration that acts on either without being told their names.
const codeReachabilityWalkIndexFamily = "code_reachability_entity_repository"

// codeReachabilityPageRankIndexFamily is the name prefix of the index the
// cross-repo consumer-evidence page reads in ORDER BY order (#6527). It is a
// second family rather than a widened prefix because
// code_reachability_entity_lookup_idx lives in the table definition this proof
// applies as a TABLE, and pulling that definition into the index set would make
// the replayed set include one the proof already applied.
const codeReachabilityPageRankIndexFamily = "code_reachability_entity_confidence"

// codeReachabilityIndexFamilies are the index-name prefixes whose migrations
// this proof replays.
var codeReachabilityIndexFamilies = []string{
	codeReachabilityWalkIndexFamily,
	codeReachabilityPageRankIndexFamily,
}

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

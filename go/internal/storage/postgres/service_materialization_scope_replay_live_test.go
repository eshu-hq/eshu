// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// serviceLineageScopeRequiredEnv makes an unset DSN a failure instead of a
// skip. The reducer contention gate sets it, so a renamed DSN variable cannot
// silently disable the #6475 lineage proofs in CI.
const serviceLineageScopeRequiredEnv = "ESHU_REQUIRE_SERVICE_LINEAGE_SCOPE_PROOF"

const (
	serviceLineageTable       = "service_materialization_generations"
	serviceLineageActiveIndex = "service_materialization_generations_active_service_idx"
	// serviceLineageScopeFirstMigration is the first #6475 migration. Every
	// definition sorting before it is what an install of the prior release has
	// already applied and recorded.
	serviceLineageScopeFirstMigration = "go/internal/storage/postgres/migrations/126_service_materialization_generations_scope_column.sql"
)

// serviceLineageIndexSnapshot is what an operator would check to tell a
// no-op replay from a rebuild: the definition, the index OID, and the
// relfilenode (a REINDEX or drop-and-create changes the last two).
type serviceLineageIndexSnapshot struct {
	Definition  string
	IndexOID    int64
	RelFileNode int64
	IdxScan     int64
}

// TestServiceMaterializationActiveIndexReplayConvergesLive proves the #6475
// migrations (126-129) against real Postgres through the production bootstrap:
//
//   - fresh database: ApplyBootstrap leaves the active index on
//     (scope_id, service_id);
//   - upgrading database: an install holding the prior release's schema and
//     legacy lineage rows (one active per service, as the old index forced)
//     converges, the backfill attributes the row whose writing work item
//     survives and leaves the row whose work item is gone NULL;
//   - two ingestion scopes may then each hold an active generation for one
//     service id, which the pre-#6475 index refused;
//   - neither a second tracked ApplyBootstrap nor an untracked full-tree
//     replay (a database without eshu_schema_migrations receipts) drops,
//     rebuilds, or fails on the index while those two actives exist.
//
// It runs in the reducer contention gate. Locally:
//
//	ESHU_POSTGRES_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test ./internal/storage/postgres \
//	  -run TestServiceMaterializationActiveIndexReplayConvergesLive -count=1 -v
func TestServiceMaterializationActiveIndexReplayConvergesLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	t.Run("fresh", func(t *testing.T) {
		db, schema := openServiceLineageSchemaLive(ctx, t, "eshu_6475_lineage_fresh")
		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("first ApplyBootstrap on a fresh schema: %v", err)
		}
		first := serviceLineageIndexLive(ctx, t, db, schema)
		assertServiceLineageIndexScoped(t, first)
		assertServiceLineageReplayIsNoOp(ctx, t, db, schema, first)
	})

	t.Run("upgrade", func(t *testing.T) {
		db, schema := openServiceLineageSchemaLive(ctx, t, "eshu_6475_lineage_upgrade")
		prior, rest := splitServiceLineageDefinitions(t)
		if len(rest) < 3 {
			t.Fatalf("found %d definition(s) at or after %s, want at least the three #6475 migrations", len(rest), serviceLineageScopeFirstMigration)
		}
		if err := applyBootstrapDefinitionsWith(ctx, SQLDB{DB: db}, prior, slog.Default(), schemaBootstrapCoordination{}); err != nil {
			t.Fatalf("apply the prior release's definitions: %v", err)
		}
		seedServiceLineageLegacyRowsLive(ctx, t, db)

		// The pre-#6475 definition is the defect: one active per service id,
		// whatever ingestion scope wrote it.
		if _, err := db.ExecContext(ctx, `
INSERT INTO service_materialization_generations
  (generation_id, service_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-legacy-dup', 'service-a', 'service_catalog_correlation', now(), now(), 'active', now())`); err == nil {
			t.Fatal("the prior release's index accepted a second active row for one service id; the fixture does not stand where upgrading installs stand")
		}

		if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
			t.Fatalf("ApplyBootstrap upgrading a populated prior-release schema: %v", err)
		}
		converged := serviceLineageIndexLive(ctx, t, db, schema)
		assertServiceLineageIndexScoped(t, converged)

		for generationID, want := range map[string]sql.NullString{
			"gen-witnessed":       {String: "scope-a", Valid: true},
			"gen-witness-deleted": {},
			"gen-no-intent":       {},
			"gen-foreign-domain":  {},
		} {
			var got sql.NullString
			if err := db.QueryRowContext(ctx,
				"SELECT scope_id FROM service_materialization_generations WHERE generation_id = $1",
				generationID,
			).Scan(&got); err != nil {
				t.Fatalf("read backfilled scope for %s: %v", generationID, err)
			}
			if got != want {
				t.Errorf("backfilled scope_id for %s = %v, want %v", generationID, got, want)
			}
		}

		// Two scopes, one service id, both active: the #6475 contract.
		if _, err := db.ExecContext(ctx, `
INSERT INTO service_materialization_generations
  (generation_id, service_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES
  ('gen-shared-a', 'service-shared', 'scope-a', 'service_catalog_correlation', now(), now(), 'active', now()),
  ('gen-shared-b', 'service-shared', 'scope-b', 'service_catalog_correlation', now(), now(), 'active', now())`); err != nil {
			t.Fatalf("insert one active generation per scope for one service id: %v", err)
		}
		// Still one active per (scope, service).
		if _, err := db.ExecContext(ctx, `
INSERT INTO service_materialization_generations
  (generation_id, service_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-shared-a2', 'service-shared', 'scope-a', 'service_catalog_correlation', now(), now(), 'active', now())`); err == nil {
			t.Error("the rescoped index accepted a second active row for one (scope_id, service_id)")
		}

		assertServiceLineageReplayIsNoOp(ctx, t, db, schema, converged)

		var actives int
		if err := db.QueryRowContext(ctx,
			"SELECT count(*) FROM service_materialization_generations WHERE service_id = 'service-shared' AND status = 'active'",
		).Scan(&actives); err != nil {
			t.Fatalf("count scoped actives after replay: %v", err)
		}
		if actives != 2 {
			t.Errorf("active generations for service-shared after replay = %d, want 2 (one per scope)", actives)
		}
	})
}

// assertServiceLineageReplayIsNoOp runs a second tracked ApplyBootstrap and
// then an untracked replay of the whole tree through the per-statement
// recorder, and requires the active index to be untouched by both.
func assertServiceLineageReplayIsNoOp(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	schema string,
	before serviceLineageIndexSnapshot,
) {
	t.Helper()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("second (tracked) ApplyBootstrap: %v", err)
	}
	afterTracked := serviceLineageIndexLive(ctx, t, db, schema)
	t.Logf("active index before replay: %+v", before)
	t.Logf("active index after tracked ApplyBootstrap: %+v", afterTracked)

	recorder := &migrationIndexReplayRecorder{db: SQLDB{DB: db}, t: t, schema: schema, table: serviceLineageTable}
	if err := ApplyDefinitions(ctx, recorder, BootstrapDefinitions()); err != nil {
		t.Fatalf("untracked full-tree replay: %v", err)
	}
	for _, change := range recorder.changes {
		t.Errorf("the untracked replay changed an index on %s: %s", serviceLineageTable, change)
	}
	afterUntracked := serviceLineageIndexLive(ctx, t, db, schema)
	t.Logf("active index after untracked full-tree replay: %+v", afterUntracked)

	for label, after := range map[string]serviceLineageIndexSnapshot{
		"tracked ApplyBootstrap":     afterTracked,
		"untracked full-tree replay": afterUntracked,
	} {
		if after.Definition != before.Definition || after.IndexOID != before.IndexOID || after.RelFileNode != before.RelFileNode {
			t.Errorf("%s changed %s: before %+v, after %+v", label, serviceLineageActiveIndex, before, after)
		}
	}
}

func assertServiceLineageIndexScoped(t *testing.T, snapshot serviceLineageIndexSnapshot) {
	t.Helper()
	if !strings.Contains(snapshot.Definition, "(scope_id, service_id)") ||
		!strings.Contains(snapshot.Definition, "UNIQUE") ||
		!strings.Contains(snapshot.Definition, "'active'") {
		t.Fatalf("%s definition = %q, want UNIQUE on (scope_id, service_id) WHERE status = 'active'", serviceLineageActiveIndex, snapshot.Definition)
	}
}

// serviceLineageIndexLive reads the active index's definition, OID,
// relfilenode, and pg_stat_user_indexes scan count. A missing or INVALID index
// fails the test.
func serviceLineageIndexLive(ctx context.Context, t *testing.T, db *sql.DB, schema string) serviceLineageIndexSnapshot {
	t.Helper()
	var snapshot serviceLineageIndexSnapshot
	var valid bool
	if err := db.QueryRowContext(ctx, `
SELECT pg_get_indexdef(index_class.oid), index_class.oid::bigint, index_class.relfilenode::bigint,
       index_entry.indisvalid, COALESCE(stats.idx_scan, 0)
FROM pg_class AS index_class
JOIN pg_index AS index_entry ON index_entry.indexrelid = index_class.oid
JOIN pg_namespace AS namespace ON namespace.oid = index_class.relnamespace
LEFT JOIN pg_stat_user_indexes AS stats ON stats.indexrelid = index_class.oid
WHERE namespace.nspname = $1 AND index_class.relname = $2`,
		schema, serviceLineageActiveIndex,
	).Scan(&snapshot.Definition, &snapshot.IndexOID, &snapshot.RelFileNode, &valid, &snapshot.IdxScan); err != nil {
		t.Fatalf("read %s: %v", serviceLineageActiveIndex, err)
	}
	if !valid {
		t.Fatalf("%s is INVALID: %+v", serviceLineageActiveIndex, snapshot)
	}
	return snapshot
}

// splitServiceLineageDefinitions returns the shipped definitions before the
// first #6475 migration and the ones from it on.
func splitServiceLineageDefinitions(t *testing.T) (prior, rest []Definition) {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Path < serviceLineageScopeFirstMigration {
			prior = append(prior, definition)
			continue
		}
		rest = append(rest, definition)
	}
	return prior, rest
}

// seedServiceLineageLegacyRowsLive writes the lineage a prior release left
// behind: one generation whose writing work item still exists (the witness),
// one whose work item generation retention deleted, one with no intent id,
// and one whose intent id names a work item of another reducer domain.
func seedServiceLineageLegacyRowsLive(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES
  ('scope-a', 'repository', 'git', 'repo-a', 'git', 'partition-a', now(), now(), 'active', 'gen-scope-a'),
  ('scope-b', 'repository', 'git', 'repo-b', 'git', 'partition-b', now(), now(), 'active', 'gen-scope-b');
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES
  ('gen-scope-a', 'scope-a', 'sync', now(), now(), 'active', now()),
  ('gen-scope-b', 'scope-b', 'sync', now(), now(), 'active', now());
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key,
   status, attempt_count, payload, created_at, updated_at)
VALUES
  ('wi-service-a', 'scope-a', 'gen-scope-a', 'reducer', 'service_catalog_correlation', 'scope', 'wi-service-a',
   'succeeded', 1, '{}'::jsonb, now(), now()),
  ('wi-other-domain', 'scope-b', 'gen-scope-b', 'reducer', 'workload_identity', 'scope', 'wi-other-domain',
   'succeeded', 1, '{}'::jsonb, now(), now());
INSERT INTO service_materialization_generations
  (generation_id, service_id, trigger_kind, source_intent_id, observed_at, ingested_at, status, activated_at, superseded_at)
VALUES
  ('gen-witnessed', 'service-a', 'service_catalog_correlation', 'wi-service-a', now(), now(), 'active', now(), NULL),
  ('gen-witness-deleted', 'service-b', 'service_catalog_correlation', 'wi-retained-away', now(), now(), 'active', now(), NULL),
  ('gen-no-intent', 'service-c', 'service_catalog_correlation', NULL, now(), now(), 'superseded', now(), now()),
  ('gen-foreign-domain', 'service-d', 'service_catalog_correlation', 'wi-other-domain', now(), now(), 'active', now(), NULL);
INSERT INTO service_materialization_generations
  (generation_id, service_id, trigger_kind, observed_at, ingested_at, status)
SELECT 'gen-bulk-' || value, 'service-bulk-' || value, 'service_catalog_correlation', now(), now(),
       CASE WHEN value % 2 = 0 THEN 'active' ELSE 'superseded' END
FROM generate_series(1, 2000) AS value;
ANALYZE service_materialization_generations;
`); err != nil {
		t.Fatalf("seed prior-release service lineage: %v", err)
	}
}

// openServiceLineageSchemaLive opens a connection pool whose search_path is a
// fresh isolated schema (then public), dropped when the test ends. The DSN is
// ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN (the reducer contention gate sets
// the latter); with neither set the test skips, unless
// serviceLineageScopeRequiredEnv is "1", when it fails.
func openServiceLineageSchemaLive(ctx context.Context, t *testing.T, prefix string) (*sql.DB, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		if os.Getenv(serviceLineageScopeRequiredEnv) == "1" {
			t.Fatalf("%s=1 but neither ESHU_POSTGRES_TEST_DSN nor ESHU_POSTGRES_DSN is set", serviceLineageScopeRequiredEnv)
		}
		t.Skip("set ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN to run the live service lineage proofs")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	// The bootstrap's CREATE EXTENSION IF NOT EXISTS pg_trgm would otherwise
	// install it into this private schema (first on the search_path) and drop
	// it with the schema, leaving any schema bootstrapped concurrently without
	// gin_trgm_ops. Pin it to public, as the sibling live helpers do.
	if _, err := adminDB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
		t.Fatalf("install pg_trgm in public: %v", err)
	}
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
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
	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db, schema
}

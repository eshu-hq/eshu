// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// identityEpochLegacyIndexDDL is migration 069's statement, the one #6543
// deleted. It is repeated here to reconstruct the state an install of an
// earlier release actually stands in -- the legacy index NAME present on a
// populated fact_records -- which is the state migration 105's drop has to
// converge.
//
// 069's narrower predicate is used deliberately, not 077's. Both shipped under
// the same name, so the field holds installs with either, and the convergence
// is by NAME and therefore indifferent to which. 069's is the one whose
// coverage actually changes: it admits no Dockerfile base-image `file` facts,
// so an install left on it silently drops the epoch probe to a full scan.
const identityEpochLegacyIndexDDL = `
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_identity_epoch_idx
    ON fact_records (observed_at, fact_id)
    WHERE (
        (
            fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')
            AND source_system = 'oci_registry'
        )
        OR (
            fact_kind = 'content_entity'
            AND source_system = 'git'
            AND (
                payload->'entity_metadata' ? 'container_images'
                OR payload->'metadata' ? 'container_images'
            )
        )
    )
    AND is_tombstone = FALSE`

// identityEpochIndexFamily is the shared name prefix of the live identity epoch
// index and the legacy name it supersedes, which is how this proof finds every
// migration acting on either without being told their names.
const identityEpochIndexFamily = "fact_records_identity_epoch_idx"

// TestIdentityEpochIndexMigrationsReapplyWithoutRebuildLive proves for the
// container-image identity epoch index what no unit test can see: a second
// bootstrap over a populated fact_records that already holds the intended index
// does no index work at all.
//
// Every file under migrations/ is Exec'd on every bootstrap in filename order,
// with no ledger of what already ran (BootstrapDefinitions and ApplyDefinitions
// in schema.go). Before #6543 this index was the directory's one remaining
// violation of that model: migrations 069 and 077 both created
// fact_records_identity_epoch_idx, with different predicates, and 076 dropped
// it between them, so the drop cleared the name, the next startup's
// IF NOT EXISTS no longer skipped, and the index was rebuilt concurrently over
// a populated fact_records and dropped again -- on every start, forever.
//
// The store starts where an install of an earlier release stands: rows on disk
// and the legacy index name built. The first pass has to converge it, and the
// second has to touch nothing. The assertion is per definition -- the set of
// indexes on fact_records and each one's relfilenode are read before and after
// every statement of the second pass -- so a definition that builds an index a
// later definition drops fails here even though the state either side of the
// whole pass is identical.
//
// Run with:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://user:pass@localhost:<port>/eshu \
//	go test -tags integration ./internal/storage/postgres \
//	  -run TestIdentityEpochIndexMigrationsReapplyWithoutRebuildLive -count=1
func TestIdentityEpochIndexMigrationsReapplyWithoutRebuildLive(t *testing.T) {
	const (
		schema      = "eshu_6543_identity_epoch_index_replay"
		table       = "fact_records"
		liveIndex   = "fact_records_identity_epoch_idx_v2"
		legacyIndex = "fact_records_identity_epoch_idx"
	)

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live identity epoch index replay proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
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

	tables, indexes := identityEpochReplayDefinitions(t)
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, tables); err != nil {
		t.Fatalf("apply fact_records table definitions: %v", err)
	}
	seedIdentityEpochReplayRows(ctx, t, db)
	if _, err := db.ExecContext(ctx, identityEpochLegacyIndexDDL); err != nil {
		t.Fatalf("build the earlier release's legacy-name index: %v", err)
	}

	before := migrationIndexStateLive(ctx, t, db, schema, table)
	if _, ok := before[legacyIndex]; !ok {
		t.Fatal("the earlier release's legacy-name index is missing before the first bootstrap; the fixture does not stand where the installs it converges stand")
	}
	if _, ok := before[liveIndex]; ok {
		t.Fatal("the replacement index already exists before the first bootstrap; the fixture starts past the state it is supposed to converge")
	}

	// The first pass runs through a recorder too, because "it converges" is a
	// weaker claim than the one an operator needs. Converging an install that
	// holds the legacy index has to cost exactly ONE build of the replacement
	// and exactly ONE drop of the legacy name -- not two builds, and not a build
	// followed by a rebuild -- and only a per-statement record can tell those
	// apart from an end-state comparison.
	firstPass := &migrationIndexReplayRecorder{db: SQLDB{DB: db}, t: t, schema: schema, table: table}
	if err := ApplyDefinitions(ctx, firstPass, indexes); err != nil {
		t.Fatalf("apply identity epoch index definitions, first pass: %v", err)
	}
	builtLive, droppedLegacy := 0, 0
	for _, event := range firstPass.events {
		switch {
		case event.Index == liveIndex && event.Kind == "built":
			builtLive++
		case event.Index == liveIndex:
			t.Errorf("first bootstrap %s %s: %q", event.Kind, liveIndex, event.Statement)
		case event.Index == legacyIndex && event.Kind == "dropped":
			droppedLegacy++
		case event.Index == legacyIndex:
			t.Errorf("first bootstrap %s %s: %q", event.Kind, legacyIndex, event.Statement)
		}
	}
	if builtLive != 1 {
		t.Errorf("first bootstrap built %s %d time(s), want exactly 1; changes = %v", liveIndex, builtLive, firstPass.changes)
	}
	if droppedLegacy != 1 {
		t.Errorf("first bootstrap dropped %s %d time(s), want exactly 1; changes = %v", legacyIndex, droppedLegacy, firstPass.changes)
	}
	converged := migrationIndexStateLive(ctx, t, db, schema, table)
	if _, ok := converged[liveIndex]; !ok {
		t.Fatalf("%s is missing after the first bootstrap; indexes = %v", liveIndex, migrationIndexNamesLive(converged))
	}
	if _, ok := converged[legacyIndex]; ok {
		t.Fatal("the legacy-name index survived the first bootstrap; migration 106 did not converge the earlier release's state")
	}

	recorder := &migrationIndexReplayRecorder{db: SQLDB{DB: db}, t: t, schema: schema, table: table}
	if err := ApplyDefinitions(ctx, recorder, indexes); err != nil {
		t.Fatalf("apply identity epoch index definitions, second pass: %v", err)
	}
	for _, change := range recorder.changes {
		t.Errorf("the second bootstrap changed the indexes on fact_records: %s", change)
	}

	after := migrationIndexStateLive(ctx, t, db, schema, table)
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
	// The legacy name must still be absent, not merely un-rebuilt: migration
	// 105's drop is the statement that has to be a no-op here, and a drop of an
	// absent name leaves no trace in the relfilenode comparison above.
	if _, ok := after[legacyIndex]; ok {
		t.Errorf("%s exists after the second bootstrap; some definition creates the legacy name again", legacyIndex)
	}
}

// identityEpochReplayDefinitions returns the shipped bootstrap definitions this
// proof needs, split into the tables it seeds and the index migrations it
// replays. Reading them from BootstrapDefinitions is what stops the proof
// passing against DDL no deployment applies.
func identityEpochReplayDefinitions(t *testing.T) (tables, indexes []Definition) {
	t.Helper()

	tableNames := []string{"ingestion_scopes", "scope_generations", "fact_records"}
	isTable := map[string]bool{}
	byName := map[string]Definition{}
	for _, definition := range BootstrapDefinitions() {
		byName[definition.Name] = definition
	}
	for _, name := range tableNames {
		definition, ok := byName[name]
		if !ok {
			t.Fatalf("bootstrap definition %q is missing", name)
		}
		isTable[name] = true
		tables = append(tables, definition)
	}
	// DISCOVERED, never listed. A fixed list of definition names would apply
	// only the migrations this proof already knows about, so re-adding a create
	// of the legacy name -- the exact defect this test exists for -- would leave
	// the applied set unchanged and the proof green. Every definition naming the
	// index family is applied instead, in bootstrap order, so a new one is
	// picked up whether or not anybody updates this test. Definitions already
	// applied as tables are skipped so nothing is applied twice.
	for _, definition := range BootstrapDefinitions() {
		if isTable[definition.Name] {
			continue
		}
		if strings.Contains(definition.SQL, identityEpochIndexFamily) {
			indexes = append(indexes, definition)
		}
	}
	// A rename that emptied the scan would make the whole proof vacuous. Two,
	// because the family carries the replacement's create and the legacy name's
	// drop.
	if len(indexes) < 2 {
		t.Fatalf("found %d definition(s) naming %q, want at least the replacement's create and the legacy name's drop",
			len(indexes), identityEpochIndexFamily)
	}
	return tables, indexes
}

// seedIdentityEpochReplayRows populates fact_records so a rebuild is real index
// work rather than a metadata edit on an empty table. Two thirds of the rows
// match the identity filter, split between an OCI registry arm that both the
// legacy and the replacement predicate admit and the Dockerfile base-image
// `file` arm that only the replacement admits; the remaining third matches
// neither, so the partial index stays genuinely partial.
func seedIdentityEpochReplayRows(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-1', 'repository', 'git', 'key-1', 'oci_registry', 'partition-1', now(), now(), 'active', 'gen-active');
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ('gen-active', 'scope-1', 'sync', now(), now(), 'active', now());
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'fact-' || lpad(value::text, 6, '0'),
       'scope-1', 'gen-active',
       CASE value % 3
         WHEN 0 THEN 'oci_registry.image_manifest'
         WHEN 1 THEN 'file'
         ELSE 'repository'
       END,
       'stable-' || lpad(value::text, 6, '0'),
       CASE value % 3 WHEN 0 THEN 'oci_registry' ELSE 'git' END,
       'source-' || lpad(value::text, 6, '0'),
       now(), now(), FALSE,
       CASE value % 3
         WHEN 1 THEN '{"parsed_file_data": {"dockerfile_stages": [{"base_image": "alpine:3.20"}]}}'::jsonb
         ELSE '{}'::jsonb
       END
FROM generate_series(1, 20000) AS value;
ANALYZE fact_records;
`); err != nil {
		t.Fatalf("seed populated fact store: %v", err)
	}
}

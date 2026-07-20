// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	documentationFindingsReadIndexName   = "fact_records_documentation_findings_read_idx"
	documentationFindingsLegacyIndexName = "fact_records_documentation_findings_visible_idx"
)

type documentationIndexState struct {
	oid        int64
	definition string
	valid      bool
	ready      bool
}

func TestDocumentationFindingsIndexRestartSafetyLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		2*time.Minute,
	)
	exec := SQLDB{DB: db}
	preUpgrade, createReplacement, dropLegacy := documentationFindingsUpgradeDefinitions(t)

	if err := ApplyDefinitions(ctx, exec, preUpgrade); err != nil {
		t.Fatalf("apply populated pre-064 schema: %v", err)
	}
	seedDocumentationFindingsIndexProof(t, ctx, db)
	createDocumentationFindingsLegacyIndex(t, ctx, db)
	legacyBefore := readDocumentationIndexState(t, ctx, db, documentationFindingsLegacyIndexName)
	assertIndexReady(t, documentationFindingsLegacyIndexName, legacyBefore)
	assertDocumentationFindingsLegacyIndexDefinition(t, legacyBefore.definition)
	rowsBefore := countDocumentationFindingsProofRows(t, ctx, db)

	if err := ApplyDefinitionsWithLockTimeout(
		ctx,
		exec,
		[]Definition{createReplacement},
		5*time.Second,
	); err != nil {
		t.Fatalf("apply migration 064 replacement create: %v", err)
	}
	first := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, first)
	legacyAfterCreate := readDocumentationIndexState(t, ctx, db, documentationFindingsLegacyIndexName)
	assertIndexReady(t, documentationFindingsLegacyIndexName, legacyAfterCreate)
	if legacyAfterCreate.oid != legacyBefore.oid {
		t.Fatalf(
			"replacement create rebuilt legacy index: before OID=%d after OID=%d",
			legacyBefore.oid,
			legacyAfterCreate.oid,
		)
	}
	if rowsAfterCreate := countDocumentationFindingsProofRows(t, ctx, db); rowsAfterCreate != rowsBefore {
		t.Fatalf("replacement create changed findings: before=%d after=%d", rowsBefore, rowsAfterCreate)
	}

	if err := ApplyDefinitionsWithLockTimeout(
		ctx,
		exec,
		[]Definition{dropLegacy},
		5*time.Second,
	); err != nil {
		t.Fatalf("apply migration 065 legacy drop: %v", err)
	}
	assertIndexAbsent(t, ctx, db, documentationFindingsLegacyIndexName)
	if rowsAfterDrop := countDocumentationFindingsProofRows(t, ctx, db); rowsAfterDrop != rowsBefore {
		t.Fatalf("legacy drop changed findings: before=%d after=%d", rowsBefore, rowsAfterDrop)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("first post-upgrade ApplyBootstrap() error = %v", err)
	}
	second := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, second)
	if second.oid != first.oid {
		t.Fatalf("post-upgrade bootstrap rebuilt index: first OID=%d second OID=%d", first.oid, second.oid)
	}
	if second.definition != first.definition {
		t.Fatalf("post-upgrade bootstrap changed index definition:\nfirst:  %s\nsecond: %s", first.definition, second.definition)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("second post-upgrade ApplyBootstrap() error = %v", err)
	}
	third := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, third)
	if third != second {
		t.Fatalf("repeated bootstrap changed stable index: second=%+v third=%+v", second, third)
	}

	proveDocumentationIndexInvalidBuildRecovery(t, ctx, db)
	recovered := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, recovered)
	assertDocumentationFindingsIndexDefinition(t, recovered.definition)
	assertIndexAbsent(t, ctx, db, documentationFindingsLegacyIndexName)

	beforeConcurrent := recovered
	runConcurrentDocumentationBootstraps(t, ctx, db)
	afterConcurrent := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, afterConcurrent)
	if afterConcurrent != beforeConcurrent {
		t.Fatalf("concurrent bootstrap changed stable index: before=%+v after=%+v", beforeConcurrent, afterConcurrent)
	}
	assertIndexAbsent(t, ctx, db, documentationFindingsLegacyIndexName)
}

func documentationFindingsUpgradeDefinitions(
	t *testing.T,
) ([]Definition, Definition, Definition) {
	t.Helper()
	definitions := BootstrapDefinitions()
	createPosition := -1
	dropPosition := -1
	for i, definition := range definitions {
		switch definition.Name {
		case "create_documentation_findings_read_idx":
			createPosition = i
		case "drop_documentation_findings_visible_idx":
			dropPosition = i
		}
	}
	if createPosition < 0 || dropPosition < 0 {
		t.Fatalf(
			"documentation findings upgrade definitions missing: create=%d drop=%d",
			createPosition,
			dropPosition,
		)
	}
	if createPosition >= dropPosition {
		t.Fatalf(
			"documentation findings upgrade order is unsafe: create=%d drop=%d",
			createPosition,
			dropPosition,
		)
	}
	return definitions[:createPosition], definitions[createPosition], definitions[dropPosition]
}

func createDocumentationFindingsLegacyIndex(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
CREATE INDEX fact_records_documentation_findings_visible_idx
ON fact_records (
  (payload->>'finding_type'),
  (payload->>'source_id'),
  (payload->>'document_id'),
  (payload->>'status'),
  (payload->>'truth_level'),
  (payload->>'freshness_state'),
  observed_at DESC,
  fact_id DESC
)
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
`); err != nil {
		t.Fatalf("create populated legacy documentation findings index: %v", err)
	}
}

func seedDocumentationFindingsIndexProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status
) VALUES (
  'scope:documentation-index-proof', 'repository', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active'
);
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES (
  'generation:documentation-index-proof', 'scope:documentation-index-proof', 'proof',
  clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()
);
UPDATE ingestion_scopes
SET active_generation_id = 'generation:documentation-index-proof'
WHERE scope_id = 'scope:documentation-index-proof';
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'finding:index-proof:' || n,
  'scope:documentation-index-proof',
  'generation:documentation-index-proof',
  'documentation_finding',
  'finding:index-proof:' || n,
  'proof', 'proof', 'finding:index-proof:' || n,
  clock_timestamp(), clock_timestamp(),
  jsonb_build_object(
    'finding_type', 'documentation_drift',
    'source_id', 'source:index-proof',
    'document_id', 'document:index-proof:' || n,
    'status', 'open',
    'truth_level', 'observed',
    'freshness_state', 'fresh',
    'permissions', jsonb_build_object(
      'viewer_can_read_source', n % 2 = 0,
      'source_acl_evaluated', n % 2 = 0
    ),
    'states', jsonb_build_object(
      'permission_decision', CASE WHEN n % 2 = 0 THEN 'allowed' ELSE 'denied' END
    )
  )
FROM generate_series(1, 2000) AS n;
`); err != nil {
		t.Fatalf("seed populated documentation findings: %v", err)
	}
}

func countDocumentationFindingsProofRows(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_records
WHERE fact_kind = 'documentation_finding'
  AND is_tombstone = FALSE
`).Scan(&count); err != nil {
		t.Fatalf("count documentation findings proof rows: %v", err)
	}
	return count
}

func proveDocumentationIndexInvalidBuildRecovery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "DROP INDEX "+documentationFindingsReadIndexName); err != nil {
		t.Fatalf("drop valid documentation findings index: %v", err)
	}
	invalidDDL := `CREATE UNIQUE INDEX CONCURRENTLY ` + documentationFindingsReadIndexName + `
ON fact_records ((payload->>'finding_type'))
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE`
	if _, err := db.ExecContext(ctx, invalidDDL); err == nil {
		t.Fatal("duplicate concurrent unique index build error = nil, want non-nil")
	}
	invalid := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	if invalid.valid {
		t.Fatal("failed concurrent index is valid, want invalid")
	}

	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("recover invalid documentation findings index through bootstrap: %v", err)
	}
	recovered := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, recovered)
	if recovered.oid == invalid.oid {
		t.Fatalf("bootstrap retained invalid documentation index OID %d", invalid.oid)
	}
	assertIndexAbsent(t, ctx, db, documentationFindingsLegacyIndexName)
}

func runConcurrentDocumentationBootstraps(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for range 2 {
		go func() {
			ready.Done()
			<-start
			errs <- ApplyBootstrap(ctx, SQLDB{DB: db})
		}()
	}
	ready.Wait()
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent ApplyBootstrap() error = %v", err)
		}
	}
}

func readDocumentationIndexState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	indexName string,
) documentationIndexState {
	t.Helper()
	var state documentationIndexState
	if err := db.QueryRowContext(ctx, `
SELECT c.oid::bigint, pg_get_indexdef(c.oid), i.indisvalid, i.indisready
FROM pg_class c
JOIN pg_index i ON i.indexrelid = c.oid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relname = $1
`, indexName).Scan(&state.oid, &state.definition, &state.valid, &state.ready); err != nil {
		t.Fatalf("read index state for %s: %v", indexName, err)
	}
	return state
}

func assertDocumentationIndexReady(t *testing.T, state documentationIndexState) {
	t.Helper()
	assertIndexReady(t, documentationFindingsReadIndexName, state)
	assertDocumentationFindingsIndexDefinition(t, state.definition)
}

func assertIndexReady(t *testing.T, indexName string, state documentationIndexState) {
	t.Helper()
	if !state.valid || !state.ready {
		t.Fatalf("index %s is not ready: %+v", indexName, state)
	}
}

func assertDocumentationFindingsLegacyIndexDefinition(t *testing.T, definition string) {
	t.Helper()
	for _, want := range documentationFindingACLIndexPredicatesForTest() {
		if !strings.Contains(definition, want) {
			t.Fatalf("legacy documentation findings index missing %q: %s", want, definition)
		}
	}
}

func assertDocumentationFindingsIndexDefinition(t *testing.T, definition string) {
	t.Helper()
	for _, want := range []string{
		"fact_records_documentation_findings_read_idx",
		"finding_type",
		"observed_at DESC",
		"fact_id DESC",
		"fact_kind = 'documentation_finding'",
		"is_tombstone = false",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("documentation findings index definition missing %q: %s", want, definition)
		}
	}
	for _, forbidden := range documentationFindingACLIndexPredicatesForTest() {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("documentation findings index keeps stale ACL predicate %q: %s", forbidden, definition)
		}
	}
}

func assertIndexAbsent(t *testing.T, ctx context.Context, db *sql.DB, indexName string) {
	t.Helper()
	var present bool
	if err := db.QueryRowContext(
		ctx,
		"SELECT to_regclass('public.' || $1) IS NOT NULL",
		indexName,
	).Scan(&present); err != nil {
		t.Fatalf("read index presence for %s: %v", indexName, err)
	}
	if present {
		t.Fatalf("legacy index %s is still present", indexName)
	}
}

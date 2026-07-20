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
	documentationFindingsReadIndexName      = "fact_records_documentation_findings_read_idx"
	documentationFindingsFilterIndexName    = "fact_records_documentation_findings_filter_idx"
	documentationFindingsAggregateIndexName = "fact_records_documentation_findings_visible_idx"
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
	preUpgrade, createListIndexes := documentationFindingsUpgradeDefinitions(t)

	if err := ApplyDefinitions(ctx, exec, preUpgrade); err != nil {
		t.Fatalf("apply populated pre-064 schema: %v", err)
	}
	seedDocumentationFindingsIndexProof(t, ctx, db)
	aggregateBefore := readDocumentationIndexState(t, ctx, db, documentationFindingsAggregateIndexName)
	assertDocumentationAggregateIndexReady(t, aggregateBefore)
	rowsBefore := countDocumentationFindingsProofRows(t, ctx, db)

	if err := ApplyDefinitionsWithLockTimeout(
		ctx,
		exec,
		createListIndexes,
		5*time.Second,
	); err != nil {
		t.Fatalf("apply list-index migrations: %v", err)
	}
	firstRead := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, firstRead)
	firstFilter := readDocumentationIndexState(t, ctx, db, documentationFindingsFilterIndexName)
	assertDocumentationFilterIndexReady(t, firstFilter)
	aggregateAfterCreate := readDocumentationIndexState(t, ctx, db, documentationFindingsAggregateIndexName)
	assertDocumentationAggregateIndexReady(t, aggregateAfterCreate)
	if aggregateAfterCreate.oid != aggregateBefore.oid {
		t.Fatalf(
			"list-index create rebuilt aggregate-visible index: before OID=%d after OID=%d",
			aggregateBefore.oid,
			aggregateAfterCreate.oid,
		)
	}
	if rowsAfterCreate := countDocumentationFindingsProofRows(t, ctx, db); rowsAfterCreate != rowsBefore {
		t.Fatalf("list-index create changed findings: before=%d after=%d", rowsBefore, rowsAfterCreate)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("first post-upgrade ApplyBootstrap() error = %v", err)
	}
	secondRead := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, secondRead)
	secondFilter := readDocumentationIndexState(t, ctx, db, documentationFindingsFilterIndexName)
	assertDocumentationFilterIndexReady(t, secondFilter)
	aggregateSecond := readDocumentationIndexState(t, ctx, db, documentationFindingsAggregateIndexName)
	assertDocumentationAggregateIndexReady(t, aggregateSecond)
	if secondRead.oid != firstRead.oid {
		t.Fatalf("post-upgrade bootstrap rebuilt read index: first OID=%d second OID=%d", firstRead.oid, secondRead.oid)
	}
	if secondRead.definition != firstRead.definition {
		t.Fatalf("post-upgrade bootstrap changed read index definition:\nfirst:  %s\nsecond: %s", firstRead.definition, secondRead.definition)
	}
	if secondFilter != firstFilter {
		t.Fatalf("post-upgrade bootstrap changed filter index: first=%+v second=%+v", firstFilter, secondFilter)
	}

	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("second post-upgrade ApplyBootstrap() error = %v", err)
	}
	thirdRead := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, thirdRead)
	if thirdRead != secondRead {
		t.Fatalf("repeated bootstrap changed stable read index: second=%+v third=%+v", secondRead, thirdRead)
	}
	thirdFilter := readDocumentationIndexState(t, ctx, db, documentationFindingsFilterIndexName)
	assertDocumentationFilterIndexReady(t, thirdFilter)
	if thirdFilter != secondFilter {
		t.Fatalf("repeated bootstrap changed stable filter index: second=%+v third=%+v", secondFilter, thirdFilter)
	}
	aggregateThird := readDocumentationIndexState(t, ctx, db, documentationFindingsAggregateIndexName)
	assertDocumentationAggregateIndexReady(t, aggregateThird)
	if aggregateThird != aggregateSecond {
		t.Fatalf("repeated bootstrap changed aggregate-visible index: second=%+v third=%+v", aggregateSecond, aggregateThird)
	}

	proveDocumentationIndexInvalidBuildRecovery(t, ctx, db)
	recovered := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, recovered)
	assertDocumentationFindingsIndexDefinition(t, recovered.definition)
	filterRecovered := readDocumentationIndexState(t, ctx, db, documentationFindingsFilterIndexName)
	assertDocumentationFilterIndexReady(t, filterRecovered)
	if filterRecovered != thirdFilter {
		t.Fatalf("read-index recovery changed filter index: before=%+v after=%+v", thirdFilter, filterRecovered)
	}
	aggregateRecovered := readDocumentationIndexState(t, ctx, db, documentationFindingsAggregateIndexName)
	assertDocumentationAggregateIndexReady(t, aggregateRecovered)
	if aggregateRecovered != aggregateThird {
		t.Fatalf("read-index recovery changed aggregate-visible index: before=%+v after=%+v", aggregateThird, aggregateRecovered)
	}

	beforeConcurrentRead, beforeConcurrentFilter, aggregateBeforeConcurrent := recovered, filterRecovered, aggregateRecovered
	runConcurrentDocumentationBootstraps(t, ctx, db)
	afterConcurrentRead := readDocumentationIndexState(t, ctx, db, documentationFindingsReadIndexName)
	assertDocumentationIndexReady(t, afterConcurrentRead)
	if afterConcurrentRead != beforeConcurrentRead {
		t.Fatalf("concurrent bootstrap changed stable read index: before=%+v after=%+v", beforeConcurrentRead, afterConcurrentRead)
	}
	afterConcurrentFilter := readDocumentationIndexState(t, ctx, db, documentationFindingsFilterIndexName)
	assertDocumentationFilterIndexReady(t, afterConcurrentFilter)
	if afterConcurrentFilter != beforeConcurrentFilter {
		t.Fatalf("concurrent bootstrap changed stable filter index: before=%+v after=%+v", beforeConcurrentFilter, afterConcurrentFilter)
	}
	aggregateAfterConcurrent := readDocumentationIndexState(t, ctx, db, documentationFindingsAggregateIndexName)
	assertDocumentationAggregateIndexReady(t, aggregateAfterConcurrent)
	if aggregateAfterConcurrent != aggregateBeforeConcurrent {
		t.Fatalf("concurrent bootstrap changed aggregate-visible index: before=%+v after=%+v", aggregateBeforeConcurrent, aggregateAfterConcurrent)
	}
	assertDocumentationIndexCount(t, ctx, db, 3)
}

func documentationFindingsUpgradeDefinitions(
	t *testing.T,
) ([]Definition, []Definition) {
	t.Helper()
	definitions := BootstrapDefinitions()
	readPosition := -1
	filterPosition := -1
	for i, definition := range definitions {
		if definition.Name == "create_documentation_findings_read_idx" {
			readPosition = i
		}
		if definition.Name == "create_documentation_findings_filter_idx" {
			filterPosition = i
		}
	}
	if readPosition < 0 || filterPosition != readPosition+1 {
		t.Fatalf("documentation findings list-index migrations are missing or non-adjacent: read=%d filter=%d", readPosition, filterPosition)
	}
	return definitions[:readPosition], definitions[readPosition : filterPosition+1]
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

func assertDocumentationFilterIndexReady(t *testing.T, state documentationIndexState) {
	t.Helper()
	assertIndexReady(t, documentationFindingsFilterIndexName, state)
	if err := validateDocumentationFindingFilterIndexForTest(state.definition); err != nil {
		t.Fatalf("selective documentation findings index: %v: %s", err, state.definition)
	}
}

func assertIndexReady(t *testing.T, indexName string, state documentationIndexState) {
	t.Helper()
	if !state.valid || !state.ready {
		t.Fatalf("index %s is not ready: %+v", indexName, state)
	}
}

func assertDocumentationAggregateIndexReady(t *testing.T, state documentationIndexState) {
	t.Helper()
	assertIndexReady(t, documentationFindingsAggregateIndexName, state)
	if err := validateDocumentationFindingAggregateVisibleIndexForTest(state.definition); err != nil {
		t.Fatalf("aggregate-visible documentation findings index: %v: %s", err, state.definition)
	}
}

func assertDocumentationFindingsIndexDefinition(t *testing.T, definition string) {
	t.Helper()
	if !strings.Contains(definition, "fact_records_documentation_findings_read_idx") {
		t.Fatalf("documentation findings index definition has the wrong name: %s", definition)
	}
	if err := validateDocumentationFindingReadIndexForTest(definition); err != nil {
		t.Fatalf("documentation findings index definition: %v: %s", err, definition)
	}
	for _, forbidden := range documentationFindingACLIndexPredicatesForTest() {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("documentation findings index keeps stale ACL predicate %q: %s", forbidden, definition)
		}
	}
}

func assertDocumentationIndexCount(t *testing.T, ctx context.Context, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(
		ctx,
		`SELECT count(*)
FROM pg_class c
JOIN pg_index i ON i.indexrelid = c.oid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relname IN ($1, $2, $3)
  AND i.indisvalid
  AND i.indisready`,
		documentationFindingsReadIndexName,
		documentationFindingsFilterIndexName,
		documentationFindingsAggregateIndexName,
	).Scan(&got); err != nil {
		t.Fatalf("count documentation findings indexes: %v", err)
	}
	if got != want {
		t.Fatalf("valid ready documentation findings index count = %d, want %d", got, want)
	}
}

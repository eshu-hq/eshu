// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/lib/pq"
)

const (
	relationshipFamilyExpectedLoadedFactIDsTable = `relationship_family_expected_loaded_fact_ids`
	relationshipFamilyExpectedEvidenceTable      = `relationship_family_expected_evidence`
	relationshipFamilyExpectedReadinessTable     = `relationship_family_expected_readiness`
	relationshipFamilyExpectedMemosTable         = `relationship_family_expected_memos`
	relationshipFamilyExpectedMetricsTable       = `relationship_family_expected_metrics`
	relationshipFamilyExpectedInputsTable        = `relationship_family_expected_inputs`

	relationshipFamilyRequiredNetSaving = 220*time.Second + 691*time.Millisecond
	relationshipFamilyUpperWriteTax     = 45*time.Second + 8*time.Millisecond
)

var relationshipFamilyBinaryProofManifestTables = []string{
	relationshipFamilyExpectedLoadedFactIDsTable,
	relationshipFamilyExpectedEvidenceTable,
	relationshipFamilyExpectedReadinessTable,
	relationshipFamilyExpectedMemosTable,
	relationshipFamilyExpectedMetricsTable,
	relationshipFamilyExpectedInputsTable,
}

func assertRelationshipFamilyBinaryProofManifestState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	mode string,
	expected relationshipFamilyBinaryProofExpected,
) {
	t.Helper()
	if mode == `candidate` || mode == `local-candidate` {
		assertRelationshipFamilyBinaryProofManifestPrepared(t, ctx, db, expected)
		return
	}
	if mode != `baseline` && mode != `local-baseline` {
		return
	}
	for _, table := range relationshipFamilyBinaryProofManifestTables {
		if relationshipFamilyBinaryProofTableExists(t, ctx, db, table) {
			t.Fatalf(`refusing baseline binary proof with existing manifest table %q`, table)
		}
	}
}

func writeRelationshipFamilyBinaryProofManifest(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	loadedIDs map[string]struct{},
	elapsed time.Duration,
	inputs map[string]relationshipFamilyBinaryProofInputSnapshot,
) {
	t.Helper()
	for _, table := range relationshipFamilyBinaryProofManifestTables {
		if relationshipFamilyBinaryProofTableExists(t, ctx, db, table) {
			t.Fatalf(`refusing to replace existing binary proof manifest table %q`, table)
		}
	}

	statements := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: `loaded fact IDs`,
			query: `CREATE TABLE ` + relationshipFamilyExpectedLoadedFactIDsTable +
				` (fact_id TEXT PRIMARY KEY)`,
		},
		{
			name: `evidence`,
			query: `CREATE TABLE ` + relationshipFamilyExpectedEvidenceTable + ` AS
SELECT evidence_id, generation_id, evidence_kind, relationship_type,
       source_repo_id, target_repo_id, source_entity_id, target_entity_id,
       confidence, rationale, details
FROM relationship_evidence_facts`,
		},
		{
			name: `evidence identity`,
			query: `ALTER TABLE ` + relationshipFamilyExpectedEvidenceTable +
				` ADD PRIMARY KEY (evidence_id)`,
		},
		{
			name: `readiness`,
			query: `CREATE TABLE ` + relationshipFamilyExpectedReadinessTable + ` AS
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase
FROM graph_projection_phase_state
WHERE keyspace = $1 AND phase = $2`,
			args: []any{
				string(reducer.GraphProjectionKeyspaceCrossRepoEvidence),
				string(reducer.GraphProjectionPhaseBackwardEvidenceCommitted),
			},
		},
		{
			name: `readiness identity`,
			query: `ALTER TABLE ` + relationshipFamilyExpectedReadinessTable +
				` ADD PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)`,
		},
		{
			name: `memos`,
			query: `CREATE TABLE ` + relationshipFamilyExpectedMemosTable + ` AS
SELECT scope_id, generation_id, catalog_fingerprint
FROM deferred_backfill_partition_memo`,
		},
		{
			name: `memo identity`,
			query: `ALTER TABLE ` + relationshipFamilyExpectedMemosTable +
				` ADD PRIMARY KEY (scope_id, generation_id)`,
		},
		{
			name: `metrics`,
			query: `CREATE TABLE ` + relationshipFamilyExpectedMetricsTable +
				` (metric_name TEXT PRIMARY KEY, value_bigint BIGINT NOT NULL)`,
		},
		{
			name:  `baseline duration`,
			query: `INSERT INTO ` + relationshipFamilyExpectedMetricsTable + ` (metric_name, value_bigint) VALUES ($1, $2)`,
			args:  []any{`baseline_elapsed_nanoseconds`, elapsed.Nanoseconds()},
		},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf(`create binary proof %s manifest: %v`, statement.name, err)
		}
	}

	ids := relationshipFamilyBinaryProofSortedIDs(loadedIDs)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO `+relationshipFamilyExpectedLoadedFactIDsTable+` (fact_id) SELECT unnest($1::text[])`,
		pq.Array(ids),
	); err != nil {
		t.Fatalf(`write binary proof loaded fact IDs: %v`, err)
	}
	writeRelationshipFamilyBinaryProofInputManifest(t, ctx, db, inputs)
}

func assertRelationshipFamilyBinaryProofManifestPrepared(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expected relationshipFamilyBinaryProofExpected,
) {
	t.Helper()
	wantedRows := map[string]int64{
		relationshipFamilyExpectedLoadedFactIDsTable: expected.loaded,
		relationshipFamilyExpectedEvidenceTable:      expected.evidence,
		relationshipFamilyExpectedReadinessTable:     expected.readiness,
		relationshipFamilyExpectedMemosTable:         expected.memos,
		relationshipFamilyExpectedMetricsTable:       1,
		relationshipFamilyExpectedInputsTable:        int64(len(relationshipFamilyBinaryProofInputQueries)),
	}
	for _, table := range relationshipFamilyBinaryProofManifestTables {
		if !relationshipFamilyBinaryProofTableExists(t, ctx, db, table) {
			t.Fatalf(`candidate binary proof missing baseline manifest table %q`, table)
		}
		if got := countRelationshipFamilyBinaryProofRows(t, ctx, db, table); got != wantedRows[table] {
			t.Fatalf(`baseline manifest %s rows = %d, want %d`, table, got, wantedRows[table])
		}
	}
}

func assertRelationshipFamilyBinaryProofManifest(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	loadedIDs map[string]struct{},
	candidateElapsed time.Duration,
	requireTarget bool,
) {
	t.Helper()
	expectedIDs := loadRelationshipFamilyBinaryProofExpectedIDs(t, ctx, db)
	missing, extra := relationshipFamilyBinaryProofSetDiff(expectedIDs, loadedIDs)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf(`loaded fact_id diff missing=%d %v extra=%d %v, want 0/0`,
			len(missing), relationshipFamilyBinaryProofSample(missing),
			len(extra), relationshipFamilyBinaryProofSample(extra))
	}

	assertRelationshipFamilyBinaryProofSQLDiff(t, ctx, db, `evidence`, fmt.Sprintf(`
SELECT evidence_id, generation_id, evidence_kind, relationship_type,
       source_repo_id, target_repo_id, source_entity_id, target_entity_id,
       confidence, rationale, details
FROM relationship_evidence_facts
EXCEPT
SELECT evidence_id, generation_id, evidence_kind, relationship_type,
       source_repo_id, target_repo_id, source_entity_id, target_entity_id,
       confidence, rationale, details
FROM %s`, relationshipFamilyExpectedEvidenceTable))
	assertRelationshipFamilyBinaryProofSQLDiff(t, ctx, db, `evidence inverse`, fmt.Sprintf(`
SELECT evidence_id, generation_id, evidence_kind, relationship_type,
       source_repo_id, target_repo_id, source_entity_id, target_entity_id,
       confidence, rationale, details
FROM %s
EXCEPT
SELECT evidence_id, generation_id, evidence_kind, relationship_type,
       source_repo_id, target_repo_id, source_entity_id, target_entity_id,
       confidence, rationale, details
FROM relationship_evidence_facts`, relationshipFamilyExpectedEvidenceTable))
	assertRelationshipFamilyBinaryProofSQLDiff(t, ctx, db, `readiness`, fmt.Sprintf(`
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase
FROM graph_projection_phase_state
WHERE keyspace = $1 AND phase = $2
EXCEPT
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase
FROM %s`, relationshipFamilyExpectedReadinessTable),
		string(reducer.GraphProjectionKeyspaceCrossRepoEvidence),
		string(reducer.GraphProjectionPhaseBackwardEvidenceCommitted))
	assertRelationshipFamilyBinaryProofSQLDiff(t, ctx, db, `readiness inverse`, fmt.Sprintf(`
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase
FROM %s
EXCEPT
SELECT scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase
FROM graph_projection_phase_state
WHERE keyspace = $1 AND phase = $2`, relationshipFamilyExpectedReadinessTable),
		string(reducer.GraphProjectionKeyspaceCrossRepoEvidence),
		string(reducer.GraphProjectionPhaseBackwardEvidenceCommitted))
	assertRelationshipFamilyBinaryProofSQLDiff(t, ctx, db, `memos`, fmt.Sprintf(`
SELECT scope_id, generation_id, catalog_fingerprint
FROM deferred_backfill_partition_memo
EXCEPT
SELECT scope_id, generation_id, catalog_fingerprint
FROM %s`, relationshipFamilyExpectedMemosTable))
	assertRelationshipFamilyBinaryProofSQLDiff(t, ctx, db, `memos inverse`, fmt.Sprintf(`
SELECT scope_id, generation_id, catalog_fingerprint
FROM %s
EXCEPT
SELECT scope_id, generation_id, catalog_fingerprint
FROM deferred_backfill_partition_memo`, relationshipFamilyExpectedMemosTable))

	var baselineNanoseconds int64
	if err := db.QueryRowContext(ctx,
		`SELECT value_bigint FROM `+relationshipFamilyExpectedMetricsTable+` WHERE metric_name = $1`,
		`baseline_elapsed_nanoseconds`,
	).Scan(&baselineNanoseconds); err != nil {
		t.Fatalf(`read baseline binary proof duration: %v`, err)
	}
	baselineElapsed := time.Duration(baselineNanoseconds)
	if !requireTarget {
		t.Logf(`relationship-family local A/B exactness: baseline=%s candidate=%s target_gate=remote_only`,
			baselineElapsed, candidateElapsed)
		return
	}
	netSaving := baselineElapsed - candidateElapsed - relationshipFamilyUpperWriteTax
	t.Logf(`relationship-family A/B: baseline=%s candidate=%s upper_write_tax=%s net_saving=%s required=%s`,
		baselineElapsed, candidateElapsed, relationshipFamilyUpperWriteTax, netSaving,
		relationshipFamilyRequiredNetSaving)
	if netSaving < relationshipFamilyRequiredNetSaving {
		t.Fatalf(`relationship-family net saving = %s, want >= %s (baseline=%s candidate=%s write_tax=%s)`,
			netSaving, relationshipFamilyRequiredNetSaving, baselineElapsed, candidateElapsed,
			relationshipFamilyUpperWriteTax)
	}
}

func relationshipFamilyBinaryProofTableExists(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	table string,
) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		t.Fatalf(`check binary proof manifest table %q: %v`, table, err)
	}
	return exists
}

func loadRelationshipFamilyBinaryProofExpectedIDs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
) map[string]struct{} {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT fact_id FROM `+relationshipFamilyExpectedLoadedFactIDsTable)
	if err != nil {
		t.Fatalf(`load baseline fact IDs: %v`, err)
	}
	defer func() { _ = rows.Close() }()
	result := make(map[string]struct{})
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			t.Fatalf(`scan baseline fact ID: %v`, err)
		}
		result[factID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf(`iterate baseline fact IDs: %v`, err)
	}
	return result
}

func assertRelationshipFamilyBinaryProofSQLDiff(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	name string,
	query string,
	args ...any,
) {
	t.Helper()
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM (`+query+`) AS diff`, args...).Scan(&count); err != nil {
		t.Fatalf(`calculate %s manifest diff: %v`, name, err)
	}
	if count != 0 {
		t.Fatalf(`%s manifest diff = %d, want 0`, name, count)
	}
}

func relationshipFamilyBinaryProofSortedIDs(ids map[string]struct{}) []string {
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func relationshipFamilyBinaryProofSetDiff(
	expected map[string]struct{},
	actual map[string]struct{},
) (missing []string, extra []string) {
	for id := range expected {
		if _, ok := actual[id]; !ok {
			missing = append(missing, id)
		}
	}
	for id := range actual {
		if _, ok := expected[id]; !ok {
			extra = append(extra, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func relationshipFamilyBinaryProofSample(values []string) []string {
	const limit = 5
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

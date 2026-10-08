// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sort"
	"testing"
)

// TestReducerFairnessIsolatedSchemaDerivesFromBootstrap is the seeded-violation
// guard for the isolated-schema drift class behind #6680 and #7493: a live
// proof helper that hand-picks bootstrap definitions (or hand-writes DDL)
// silently falls behind the production schema, and the proof then fails with
// a missing column, table, or function far from the real cause.
//
// The guard builds one schema through the helper under test and one through
// the production bootstrap, then diffs their schema objects (relations,
// columns, indexes, sequences, routines, triggers, constraints). Any object
// the helper-built schema lacks fails the test. Extension-owned objects are
// excluded: whichever schema bootstraps first in a pristine database hosts
// pg_trgm, so they can never match by construction.
//
// It is RED on the pre-fix helpers (both lack production objects) and GREEN
// once the helpers derive from ApplyBootstrapWithoutContentSearchIndexes.
// It is enrolled in the reducer contention gate so a reintroduced hand-picked
// list fails a PR, not the scheduled lane.
func TestReducerFairnessIsolatedSchemaDerivesFromBootstrap(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the isolated-schema derivation guard")
	}

	ctx := context.Background()
	helperDB, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	// The reference is the same production bootstrap the deadlock proof
	// applies: full definitions minus the deferred content-search indexes.
	referenceDB := openClaimDeadlockProofDB(t, dsn, 1)
	assertIsolatedSchemaMatchesBootstrap(t, ctx, helperDB, referenceDB)
}

// assertIsolatedSchemaMatchesBootstrap fails when the schema behind helperDB
// lacks any object present in the schema behind referenceDB, or carries an
// object the reference does not have. Each handle is queried through its own
// search_path so type names render identically on both sides.
func assertIsolatedSchemaMatchesBootstrap(t *testing.T, ctx context.Context, helperDB, referenceDB *sql.DB) {
	t.Helper()

	helperSchema := isolatedSchemaCurrentSchema(t, ctx, helperDB)
	referenceSchema := isolatedSchemaCurrentSchema(t, ctx, referenceDB)
	helperObjects := listIsolatedSchemaObjects(t, ctx, helperDB, helperSchema)
	referenceObjects := listIsolatedSchemaObjects(t, ctx, referenceDB, referenceSchema)

	helperSet := make(map[string]struct{}, len(helperObjects))
	for _, object := range helperObjects {
		helperSet[object] = struct{}{}
	}
	referenceSet := make(map[string]struct{}, len(referenceObjects))
	for _, object := range referenceObjects {
		referenceSet[object] = struct{}{}
	}
	var missing, extra []string
	for _, object := range referenceObjects {
		if _, ok := helperSet[object]; !ok {
			missing = append(missing, object)
		}
	}
	for _, object := range helperObjects {
		if _, ok := referenceSet[object]; !ok {
			extra = append(extra, object)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return
	}
	t.Fatalf("isolated schema %q drifted from the production bootstrap %q: %d object(s) missing, %d extra\nmissing: %v\nextra: %v",
		helperSchema, referenceSchema, len(missing), len(extra),
		firstIsolatedSchemaObjects(missing, 20), firstIsolatedSchemaObjects(extra, 20))
}

// isolatedSchemaCurrentSchema returns the schema a proof handle builds into:
// the first entry of its pinned search_path.
func isolatedSchemaCurrentSchema(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()

	var schema string
	if err := db.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatalf("read current_schema: %v", err)
	}
	if schema == "" {
		t.Fatal("current_schema() is empty: the proof handle lost its pinned search_path")
	}
	return schema
}

// isolatedSchemaInventoryQuery lists every comparable object in one schema.
// Constraint, trigger, routine, and index fingerprints compare definitions,
// not just names (#7693): a CHECK with an altered predicate, a retargeted
// foreign key, a rebound trigger, an edited routine body, or a changed index
// expression must fail the comparison.
//
// pg_get_constraintdef renders schema-free under the pinned search_path each
// proof handle carries, and md5(prosrc) is schema-free by construction, so
// both compare directly. pg_get_triggerdef and pg_get_indexdef embed the
// schema name (ON <schema>.<table>), so the arms strip "<schema>." before
// comparing. Isolated schema names are prefix_<unixnanos>, which cannot
// appear in a definition except as a qualifier.
// Extension-owned objects are excluded (see the guard's doc comment).
const isolatedSchemaInventoryQuery = `
SELECT 'rel:' || c.relname || ':' || c.relkind::text
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
UNION ALL
SELECT 'col:' || c.relname || '.' || a.attname || ':' || format_type(a.atttypid, a.atttypmod)
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND a.attnum > 0 AND NOT a.attisdropped
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
UNION ALL
SELECT 'idx:' || c.relname || ':' || tc.relname || ':' || am.amname || ':' || i.indisunique::text || ':' || i.indkey::text ||
       ':' || COALESCE(pg_get_expr(i.indpred, i.indrelid), '') || ':' || replace(pg_get_indexdef(c.oid), $1 || '.', '')
FROM pg_class c
JOIN pg_index i ON i.indexrelid = c.oid
JOIN pg_class tc ON tc.oid = i.indrelid
JOIN pg_am am ON am.oid = c.relam
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
UNION ALL
SELECT 'seq:' || c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relkind = 'S'
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
UNION ALL
SELECT 'fn:' || p.proname || ':' || pg_get_function_identity_arguments(p.oid) || ':' || p.prorettype::regtype::text || ':' || md5(p.prosrc)
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = $1
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
UNION ALL
SELECT 'trg:' || t.tgname || ':' || c.relname || ':' || t.tgtype::text || ':' || replace(pg_get_triggerdef(t.oid), $1 || '.', '')
FROM pg_trigger t
JOIN pg_class c ON c.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND NOT t.tgisinternal
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = t.oid AND d.deptype = 'e')
UNION ALL
SELECT 'con:' || c.conname || ':' || t.relname || ':' || c.contype::text || ':' || c.confdeltype::text || ':' || c.confupdtype::text || ':' || pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class t ON t.oid = c.conrelid
JOIN pg_namespace n ON n.oid = c.connamespace
WHERE n.nspname = $1
  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = c.oid AND d.deptype = 'e')
ORDER BY 1`

// listIsolatedSchemaObjects returns the sorted inventory of one schema.
func listIsolatedSchemaObjects(t *testing.T, ctx context.Context, db *sql.DB, schema string) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, isolatedSchemaInventoryQuery, schema)
	if err != nil {
		t.Fatalf("inventory schema %q: %v", schema, err)
	}
	defer rows.Close()
	var objects []string
	for rows.Next() {
		var object string
		if err := rows.Scan(&object); err != nil {
			t.Fatalf("scan inventory row: %v", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read inventory rows: %v", err)
	}
	sort.Strings(objects)
	return objects
}

// firstIsolatedSchemaObjects caps a failure listing so a fully drifted schema
// does not dump ten thousand objects into the log.
func firstIsolatedSchemaObjects(objects []string, limit int) []string {
	if len(objects) <= limit {
		return objects
	}
	return objects[:limit]
}

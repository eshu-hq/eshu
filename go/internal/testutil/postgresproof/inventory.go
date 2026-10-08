// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"sort"
	"testing"
)

// schemaInventoryQuery lists every comparable object in one schema.
// Constraint, trigger, routine, and index fingerprints compare definitions,
// not just names (#7693): a CHECK with an altered predicate, a retargeted
// foreign key, a rebound trigger, an edited routine body, or a changed index
// expression must fail the comparison.
//
// pg_get_constraintdef renders schema-free under the pinned search_path each
// proof handle carries, and the sha256(prosrc) digest is schema-free by
// construction, so both compare directly. pg_get_triggerdef and
// pg_get_indexdef embed the schema name (ON <schema>.<table>), so the arms
// strip "<schema>." before comparing. Isolated schema names are
// prefix_<unixnanos>, which cannot
// appear in a definition except as a qualifier.
// Extension-owned objects are excluded: whichever schema bootstraps first in
// a pristine database hosts pg_trgm, so they can never match by construction.
//
// prosrc is NULL for internal-language functions, and 'x' || NULL is NULL,
// so the fn: arm coalesces it: a NULL body fingerprints as the empty digest
// instead of NULLing the whole row.
const schemaInventoryQuery = `
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
SELECT 'fn:' || p.proname || ':' || pg_get_function_identity_arguments(p.oid) || ':' || p.prorettype::regtype::text || ':' || encode(sha256(convert_to(COALESCE(p.prosrc, ''), 'UTF8')), 'hex')
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

// ListSchemaObjects returns the sorted definition-level inventory of the
// named schema: relations, columns, indexes, sequences, routines, triggers,
// and constraints, fingerprinted by schemaInventoryQuery. Both the
// isolated-schema derivation guard and the advisory-suppression guard compare
// a helper-built schema against the production bootstrap through this one
// helper, so a future definition-arm change cannot update one copy and miss
// the other. It fails the test on any query or scan error.
func ListSchemaObjects(t *testing.T, ctx context.Context, db *sql.DB, schema string) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, schemaInventoryQuery, schema)
	if err != nil {
		t.Fatalf("inventory schema %q: %v", schema, err)
	}
	defer func() { _ = rows.Close() }()
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

// FirstSchemaObjects caps a failure listing so a fully drifted schema does
// not dump ten thousand objects into the log.
func FirstSchemaObjects(objects []string, limit int) []string {
	if len(objects) <= limit {
		return objects
	}
	return objects[:limit]
}

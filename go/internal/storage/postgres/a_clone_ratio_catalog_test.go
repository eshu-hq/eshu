// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Catalog fingerprint and equality half of the #10148 clone-ratio probe; see
// a_clone_ratio_probe_test.go for the whole probe.

// probeCatalogSQL lists one text line per catalog fact, tagged by category:
// relations, columns, indexes, constraints, functions, triggers, policies,
// schemas, extensions, types, sequences, default ACLs, database settings, the
// migration ledger, and exact row counts of every table.
const probeCatalogSQL = `SELECT cat, line FROM (
SELECT 'relation' AS cat, format('%s.%s kind=%s persist=%s rls=%s/%s opts=%s acl=%s owner=%s', n.nspname, c.relname, c.relkind, c.relpersistence, c.relrowsecurity, c.relforcerowsecurity, coalesce(c.reloptions::text,''), coalesce(c.relacl::text,''), pg_get_userbyid(c.relowner)) AS line
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'column', format('%s.%s.%s #%s %s notnull=%s default=%s ident=%s gen=%s coll=%s acl=%s opts=%s storage=%s', n.nspname, c.relname, a.attname, a.attnum, format_type(a.atttypid,a.atttypmod), a.attnotnull, coalesce(pg_get_expr(d.adbin,d.adrelid),''), a.attidentity, a.attgenerated, a.attcollation::regcollation, coalesce(a.attacl::text,''), coalesce(a.attoptions::text,''), a.attstorage)
  FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum
  WHERE a.attnum>0 AND NOT a.attisdropped AND n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'index', format('%s valid=%s ready=%s live=%s opts=%s', pg_get_indexdef(i.indexrelid), i.indisvalid, i.indisready, i.indislive, coalesce(ic.reloptions::text,''))
  FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid JOIN pg_namespace n ON n.oid=ic.relnamespace WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'constraint', format('%s %s %s %s validated=%s', n.nspname, c.conrelid::regclass::text, c.conname, pg_get_constraintdef(c.oid), c.convalidated)
  FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'function', format('%s.%s(%s) md5=%s secdef=%s lang=%s config=%s acl=%s owner=%s vol=%s', n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), md5(p.prosrc), p.prosecdef, l.lanname, coalesce(p.proconfig::text,''), coalesce(p.proacl::text,''), pg_get_userbyid(p.proowner), p.provolatile)
  FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang WHERE n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'trigger', format('%s enabled=%s', pg_get_triggerdef(t.oid), t.tgenabled) FROM pg_trigger t WHERE NOT t.tgisinternal
UNION ALL SELECT 'policy', format('%s.%s %s cmd=%s permissive=%s roles=%s qual=%s check=%s', schemaname, tablename, policyname, cmd, permissive, roles::text, coalesce(qual,''), coalesce(with_check,'')) FROM pg_policies WHERE schemaname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'schema', format('%s acl=%s owner=%s', nspname, coalesce(nspacl::text,''), pg_get_userbyid(nspowner)) FROM pg_namespace WHERE nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'extension', format('%s %s schema=%s', extname, extversion, extnamespace::regnamespace) FROM pg_extension
UNION ALL SELECT 'type', format('%s.%s %s %s', n.nspname, t.typname, t.typtype, coalesce((SELECT string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder) FROM pg_enum e WHERE e.enumtypid=t.oid),''))
  FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE t.typtype IN ('e','d') AND n.nspname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'sequence', format('%s.%s %s start=%s inc=%s min=%s max=%s cycle=%s cache=%s', schemaname, sequencename, data_type, start_value, increment_by, min_value, max_value, cycle, cache_size) FROM pg_sequences WHERE schemaname !~ '^(pg_|information_schema)'
UNION ALL SELECT 'default_acl', format('%s %s %s %s', pg_get_userbyid(defaclrole), defaclnamespace::regnamespace, defaclobjtype, defaclacl) FROM pg_default_acl
UNION ALL SELECT 'database', format('enc=%s collate=%s ctype=%s connlimit=%s allowconn=%s acl=%s owner=%s template=%s', pg_encoding_to_char(encoding), datcollate, datctype, datconnlimit, datallowconn, coalesce(datacl::text,''), pg_get_userbyid(datdba), datistemplate) FROM pg_database WHERE datname=current_database()
UNION ALL SELECT 'db_settings', format('%s %s', setrole, setconfig) FROM pg_db_role_setting WHERE setdatabase=(SELECT oid FROM pg_database WHERE datname=current_database())
UNION ALL SELECT 'ledger', format('%s %s %s', path, checksum_sha256, variant) FROM eshu_schema_migrations
UNION ALL SELECT 'rowcount', format('%s.%s=%s', n.nspname, c.relname, (xpath('/row/c/text()', query_to_xml(format('select count(*) as c from %I.%I', n.nspname, c.relname), false, true, '')))[1]::text)
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','p') AND n.nspname !~ '^(pg_|information_schema)'
) q ORDER BY 1, 2`

// probeFingerprint returns the catalog lines grouped by category.
func probeFingerprint(ctx context.Context, db *sql.DB) (map[string][]string, error) {
	rows, err := db.QueryContext(ctx, probeCatalogSQL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]string{}
	for rows.Next() {
		var cat, line string
		if err := rows.Scan(&cat, &line); err != nil {
			return nil, err
		}
		out[cat] = append(out[cat], line)
	}
	return out, rows.Err()
}

func probeDigest(lines []string) string {
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:4])
}

// probeCompare reports whether two fingerprints are identical and, per
// category, a count and short digest plus the first differing lines.
func probeCompare(a, b map[string][]string) (bool, string) {
	cats := map[string]bool{}
	for c := range a {
		cats[c] = true
	}
	for c := range b {
		cats[c] = true
	}
	names := make([]string, 0, len(cats))
	for c := range cats {
		names = append(names, c)
	}
	sort.Strings(names)
	equal := true
	var parts []string
	for _, c := range names {
		parts = append(parts, fmt.Sprintf("%s=%d:%s", c, len(a[c]), probeDigest(a[c])))
		if probeDigest(a[c]) == probeDigest(b[c]) {
			continue
		}
		equal = false
		parts = append(parts, fmt.Sprintf("DIFF_%s(a=%d,b=%d)", c, len(a[c]), len(b[c])))
		for _, l := range probeOnlyIn(a[c], b[c], 3) {
			parts = append(parts, "only_a["+strings.ReplaceAll(l, " ", "_")+"]")
		}
		for _, l := range probeOnlyIn(b[c], a[c], 3) {
			parts = append(parts, "only_b["+strings.ReplaceAll(l, " ", "_")+"]")
		}
	}
	return equal, strings.Join(parts, " ")
}

func probeOnlyIn(a, b []string, limit int) []string {
	seen := map[string]bool{}
	for _, l := range b {
		seen[l] = true
	}
	var out []string
	for _, l := range a {
		if !seen[l] && len(out) < limit {
			out = append(out, l[:min(len(l), 240)])
		}
	}
	return out
}

// probeEquality compares a fresh real bootstrap, the template, and a clone.
func probeEquality(t *testing.T, e probeEnv, flavour string, fresh, template *probeBuilt, searchPath string) bool {
	t.Helper()
	var clone *probeBuilt
	t.Run("equality_"+flavour, func(t *testing.T) {
		_, db := e.clone(t, template, searchPath)
		clone = e.describe(t.Context(), t, db, func() {})
	})
	if clone == nil {
		return false
	}
	freshEq, freshDetail := probeCompare(fresh.catalog, clone.catalog)
	tplEq, tplDetail := probeCompare(template.catalog, clone.catalog)
	probeLine(t, "kind=equality flavour=%s migrations=%d bootstrap_vs_clone=%t template_vs_clone=%t", flavour, template.applied, freshEq, tplEq)
	probeLine(t, "kind=equality_detail flavour=%s compare=bootstrap_vs_clone %s", flavour, freshDetail)
	probeLine(t, "kind=equality_detail flavour=%s compare=template_vs_clone %s", flavour, tplDetail)
	return freshEq && tplEq
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// keyRangeTokenBodySHA256 is the frozen v4 body digest, the same pin as
// foreignMemberTokensBodySHA256 in schema_foreign_member_tokens_test.go,
// checked here against what the migrations stored in pg_proc.prosrc.
const keyRangeTokenBodySHA256 = "f27cf0276991ca3faf9ab96a436dd3699e02427a064ffe3d989f0a1d0216bb90"

// keyRangeArrayOnlyFunction is the array-only token function (v2 of the
// shim, .claude/pr2-t-2026-11-22.md), created by the tests only as a seeded
// RED: it drops object keys and string scalars, which jsonb ?| matches.
const keyRangeArrayOnlyFunction = "eshu_sci_foreign_member_tokens_red_array_only"

const keyRangeArrayOnlyFunctionSQL = `
CREATE OR REPLACE FUNCTION ` + keyRangeArrayOnlyFunction + `(p_fact_id text, p_payload jsonb) RETURNS text[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
  SELECT CASE WHEN jsonb_typeof(p_payload #> '{provenance,advisory_group_members}') = 'array' THEN (
    SELECT array_agg(DISTINCT strpos('0123456789abcdef', right(p_fact_id, 1))::text || ':' || (e #>> '{}'))
    FROM jsonb_array_elements(p_payload #> '{provenance,advisory_group_members}') AS e
    WHERE jsonb_typeof(e) = 'string'
      AND (e #>> '{}') IS DISTINCT FROM (p_payload->>'cve_id')
      AND (e #>> '{}') IS DISTINCT FROM (p_payload->>'advisory_id'))
  END
$$`

// TestHolderKeyRangeTokenIndexValidFromMigrationsLive proves the migration
// path (the schema Job's ApplyBootstrap over the embedded migrations) leaves
// the token function as frozen v4 (IMMUTABLE, STRICT, PARALLEL SAFE, the
// pinned body) and the token index valid and ready: a GIN array_ops index on
// the function, partial on the finding kind and live rows. The reader can
// only use it after the Job, which returns only once the concurrent build is
// valid.
func TestHolderKeyRangeTokenIndexValidFromMigrationsLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	var volatile, parallel, src string
	var strict bool
	if err := sqldb.QueryRowContext(ctx, `
SELECT provolatile::text, proisstrict, proparallel::text, prosrc FROM pg_proc WHERE proname = $1`,
		postgres.SupplyChainImpactForeignMemberTokensFunction).Scan(&volatile, &strict, &parallel, &src); err != nil {
		t.Fatalf("read token function: %v", err)
	}
	sum := sha256.Sum256([]byte(src))
	if volatile != "i" || !strict || parallel != "s" || hex.EncodeToString(sum[:]) != keyRangeTokenBodySHA256 {
		t.Fatalf("token function = volatile %s strict %t parallel %s body sha256 %x; want i, true, s, %s",
			volatile, strict, parallel, sum, keyRangeTokenBodySHA256)
	}
	var valid, ready bool
	var am, opclass, def string
	if err := sqldb.QueryRowContext(ctx, `
SELECT i.indisvalid, i.indisready, a.amname, o.opcname, pg_get_indexdef(i.indexrelid)
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_am a ON a.oid = c.relam
JOIN pg_opclass o ON o.oid = i.indclass[0]
WHERE c.relname = $1`, postgres.SupplyChainImpactForeignMemberTokensIndex).Scan(&valid, &ready, &am, &opclass, &def); err != nil {
		t.Fatalf("read token index: %v", err)
	}
	if !valid || !ready || am != "gin" || opclass != "array_ops" {
		t.Fatalf("token index valid=%t ready=%t am=%s opclass=%s; want valid, ready, gin, array_ops: %s", valid, ready, am, opclass, def)
	}
	for _, want := range []string{
		postgres.SupplyChainImpactForeignMemberTokensFunction + "(fact_id, payload)",
		"fact_kind = 'reducer_supply_chain_impact_finding'::text",
		"is_tombstone = false",
	} {
		if !strings.Contains(def, want) {
			t.Fatalf("token index definition lacks %q: %s", want, def)
		}
	}
}

// keyRangeTokenMatrixSQL is the F1 shape matrix of .claude/pr2-t-2026-11-22.md
// over function %s: 14 members shapes x 3 own-id variants x 4 fact-id buckets
// x 5 lookup ids = 840 cells. A cell wants true exactly when jsonb ?| (one
// id) matches the members value and the id is neither of the row's own keys;
// it gets true when the function's tokens contain bucket:id.
const keyRangeTokenMatrixSQL = `
WITH shapes(name, m) AS (VALUES
  ('array_strings', '["GHSA-P","CVE-OWN","GHSA-OWN"]'::jsonb),
  ('mixed_array', '["GHSA-P",5,{"GHSA-Q":1},null,true,"CVE-OWN"]'),
  ('nested_array', '[["GHSA-P"],"GHSA-OWN",["GHSA:P:x"]]'),
  ('empty_array', '[]'),
  ('object_one_key', '{"GHSA-P":1}'),
  ('object_several', '{"GHSA-P":1,"CVE-OWN":2,"GHSA-OWN":[1],"x":{"GHSA-Q":1}}'),
  ('object_colon_key', '{"GHSA:P:x":1,"GHSA-P":null}'),
  ('empty_object', '{}'),
  ('string_scalar', '"GHSA-P"'),
  ('string_scalar_own', '"CVE-OWN"'),
  ('number', '5'),
  ('boolean', 'true'),
  ('json_null', 'null'),
  ('path_missing', NULL)
), owns(variant, cve, adv) AS (VALUES ('own_both', 'CVE-OWN', 'GHSA-OWN'), ('cve_null', NULL, 'GHSA-OWN'), ('adv_null', 'CVE-OWN', NULL)),
ids(id) AS (VALUES ('GHSA-P'), ('ABSENT'), ('CVE-OWN'), ('GHSA-OWN'), ('GHSA:P:x')),
fids(fid) AS (VALUES ('reducer_supply_chain_impact_finding:abc0'), ('reducer_supply_chain_impact_finding:abcf'), ('reducer_supply_chain_impact_finding:abcZ'), ('reducer_supply_chain_impact_finding:ab日')),
rows AS (
  SELECT s.name, o.variant, f.fid,
         jsonb_strip_nulls(jsonb_build_object('cve_id', o.cve, 'advisory_id', o.adv))
           || CASE WHEN s.m IS NULL THEN '{}'::jsonb ELSE jsonb_build_object('provenance', jsonb_build_object('advisory_group_members', s.m)) END AS payload
  FROM shapes s CROSS JOIN owns o CROSS JOIN fids f
), cells AS (
  SELECT COALESCE((r.payload #> '{provenance,advisory_group_members}') ? i.id, false)
           AND i.id IS DISTINCT FROM (r.payload->>'cve_id') AND i.id IS DISTINCT FROM (r.payload->>'advisory_id') AS want,
         COALESCE(%s(r.fid, r.payload) @> ARRAY[strpos('0123456789abcdef', right(r.fid, 1))::text || ':' || i.id], false) AS got,
         r.name
  FROM rows r CROSS JOIN ids i
)
SELECT count(*), count(*) FILTER (WHERE want = got), count(*) FILTER (WHERE want), count(DISTINCT name)
FROM cells`

// keyRangeTokenMatrix runs the F1 matrix over function fn.
func keyRangeTokenMatrix(t *testing.T, fn string) (cells, agree, wantTrue, shapes int) {
	t.Helper()
	ctx, sqldb := fanOutLiveDB(t)
	if fn == keyRangeArrayOnlyFunction {
		if _, err := sqldb.ExecContext(ctx, keyRangeArrayOnlyFunctionSQL); err != nil {
			t.Fatalf("create array-only RED function: %v", err)
		}
	}
	query := strings.ReplaceAll(keyRangeTokenMatrixSQL, "%s", fn)
	if err := sqldb.QueryRowContext(ctx, query).Scan(&cells, &agree, &wantTrue, &shapes); err != nil {
		t.Fatalf("F1 matrix over %s: %v", fn, err)
	}
	return cells, agree, wantTrue, shapes
}

// TestHolderKeyRangeTokenShapeMatrixLive is the F1 shape matrix on the
// migrated function: all 840 cells agree with jsonb ?| minus the row's own
// keys, 112 of them true. The seeded RED, the array-only function, must
// disagree on the object and scalar cells.
func TestHolderKeyRangeTokenShapeMatrixLive(t *testing.T) {
	cells, agree, wantTrue, shapes := keyRangeTokenMatrix(t, postgres.SupplyChainImpactForeignMemberTokensFunction)
	if cells != 840 || agree != 840 || wantTrue != 112 || shapes != 14 {
		t.Fatalf("F1 matrix = %d cells, %d agree, %d true, %d shapes; want 840, 840, 112, 14", cells, agree, wantTrue, shapes)
	}
	_, redAgree, _, _ := keyRangeTokenMatrix(t, keyRangeArrayOnlyFunction)
	if redAgree == 840 {
		t.Fatal("seeded RED: the array-only function agrees on every cell; the matrix cannot see object or scalar members")
	}
}

// keyRangeTokenCase is one stored row of the token agreement test.
type keyRangeTokenCase struct {
	factID, payload, lookup string
	foreign                 bool // jsonb ?| matches lookup and it is not an own key
}

// TestHolderKeyRangeTokenAgreementLive proves the Go lookup tokens and the
// SQL stored tokens agree: for a fact id ending in every lower-case hex
// digit, an upper-case hex digit, a non-hex character and two multibyte
// characters, with members holding colons and token-prefix look-alikes, a
// foreign member is matched by exactly one of the 16 statements' token
// arrays (HolderMemberTokens), and anything else by none.
func TestHolderKeyRangeTokenAgreementLive(t *testing.T) {
	ctx, sqldb := fanOutLiveDB(t)
	kind := "reducer_supply_chain_impact_finding:"
	members := func(m string) string {
		return `{"cve_id":"CVE-OWN","advisory_id":"GHSA-OWN","provenance":{"advisory_group_members":` + m + `}}`
	}
	var cases []keyRangeTokenCase
	for _, last := range strings.Split("0123456789abcdefAZé日", "") {
		fid := kind + "ab" + last
		cases = append(cases,
			keyRangeTokenCase{fid, members(`["GHSA-T2","CVE-OWN"]`), "GHSA-T2", true},
			keyRangeTokenCase{fid, members(`["GHSA:colon:x"]`), "GHSA:colon:x", true},
			keyRangeTokenCase{fid, members(`{"OBJ-KEY":1}`), "OBJ-KEY", true},
			keyRangeTokenCase{fid, members(`"SCALAR"`), "SCALAR", true},
			keyRangeTokenCase{fid, members(`["2:X"]`), "X", false},
			keyRangeTokenCase{fid, members(`["X"]`), "2:X", false},
			keyRangeTokenCase{fid, members(`["CVE-OWN"]`), "CVE-OWN", false},
			keyRangeTokenCase{fid, members(`[["NESTED"]]`), "NESTED", false},
			keyRangeTokenCase{fid, members(`5`), "5", false},
		)
	}
	statements := len(supplychaincore.HolderKeyRanges())
	for _, tc := range cases {
		hits := 0
		for i := 0; i < statements; i++ {
			var hit bool
			if err := sqldb.QueryRowContext(ctx, `SELECT COALESCE(`+postgres.SupplyChainImpactForeignMemberTokensFunction+`($1, $2::jsonb) && $3::text[], false)`,
				tc.factID, tc.payload, postgres.HolderMemberTokens([]string{tc.lookup}, i, statements)).Scan(&hit); err != nil {
				t.Fatalf("token overlap %+v statement %d: %v", tc, i+1, err)
			}
			if hit {
				hits++
			}
		}
		var shipped bool
		if err := sqldb.QueryRowContext(ctx, `SELECT COALESCE(($1::jsonb #> '{provenance,advisory_group_members}') ?| ARRAY[$2::text], false)
  AND $2 IS DISTINCT FROM ($1::jsonb->>'cve_id') AND $2 IS DISTINCT FROM ($1::jsonb->>'advisory_id')`,
			tc.payload, tc.lookup).Scan(&shipped); err != nil {
			t.Fatalf("shipped oracle %+v: %v", tc, err)
		}
		if shipped != tc.foreign {
			t.Fatalf("case %+v: shipped ?| oracle says %t; the case table is wrong", tc, shipped)
		}
		want := 0
		if tc.foreign {
			want = 1
		}
		if hits != want {
			t.Fatalf("case %+v: matched by %d statements, want %d", tc, hits, want)
		}
	}
}

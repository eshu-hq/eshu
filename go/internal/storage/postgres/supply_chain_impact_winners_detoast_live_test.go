// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersDetoastFieldEquivalenceLive is the live half of the #11120
// single-detoast differential: the shipped once-extracted record returns the
// same values as the legacy per-field `->>` reads on PG18, row for row, over
// edge-case payloads. The hermetic half (TestWinnersDetoastReversesToLegacy)
// proves the shipped statements differ from the legacy text only by these
// pairs; together the two halves show byte-identical winners.
//
// The seed covers the coercion edges where a record extraction could diverge
// from `->>`: missing keys, JSON nulls, empty strings, numbers (including
// 5.0), booleans, padded and unicode strings, a ~3.4KB toasted payload, and
// non-object payloads (scalar string/number/boolean/array, JSON null and SQL
// NULL), which the CASE guard maps to all-NULLs exactly like the legacy
// reads. The query would ERROR without the guard (jsonb_to_record rejects
// scalars), so a green run proves the guard too.
//
// The test also pins why the three jsonb arrays stay on `->`: a JSON null
// array reads as jsonb null through `->` but as SQL NULL through a record,
// so converting them would store '[]' where the legacy stored null.
func TestSupplyChainImpactWinnersDetoastFieldEquivalenceLive(t *testing.T) {
	dsn := supplyChainImpactLiveDSN(t, "detoast field equivalence")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn postgres: %v", err)
	}
	defer func() { _ = conn.Close() }()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	exec(`CREATE TEMP TABLE detoast_seed(id text PRIMARY KEY, payload jsonb)`)
	exec(`INSERT INTO detoast_seed(id, payload) VALUES
		('full', '{"package_id":"pkg:npm/a","repository_id":"repo-1","subject_digest":"sha256:abc","cve_id":"CVE-2026-1","advisory_id":"GHSA-1","finding_id":"F-1","priority_score":"30","purl":"pkg:npm/a@1","product_criteria":"c","match_criteria_id":"m","observed_version":"1.2.3","requested_range":"^1","impact_status":"affected","suppression_state":"suppressed","ecosystem":"npm","image_ref":"img://a","priority_bucket":"high","detection_profile":"p","match_reason":"r"}'),
		('empty', '{}'),
		('sparse', '{"package_id":"only-pkg"}'),
		('json-nulls', '{"package_id":null,"cve_id":null,"priority_score":null,"suppression_state":null}'),
		('empty-strings', '{"package_id":"","cve_id":"","advisory_id":"","finding_id":"","priority_score":"","suppression_state":""}'),
		('numbers', '{"package_id":12345,"priority_score":5,"observed_version":2,"match_criteria_id":0}'),
		('float', '{"purl":5.0,"priority_score":7}'),
		('bools', '{"ecosystem":true,"impact_status":false,"suppression_state":true}'),
		('padded-unicode', '{"package_id":"  padded  ","ecosystem":"ünïcodé ✓","match_reason":"tab\there"}'),
		('scalar-string', '"just-a-string"'),
		('scalar-number', '5'),
		('scalar-bool', 'true'),
		('scalar-array', '[1,2]'),
		('scalar-null', 'null'),
		('sql-null', NULL),
		('arrays-null', '{"package_id":"p","service_ids":null}'),
		('arrays-empty', '{"package_id":"p","service_ids":[]}'),
		('arrays-full', '{"package_id":"p","service_ids":["a"],"workload_ids":["w"],"environments":["e"]}')
	`)
	exec(`UPDATE detoast_seed SET payload = payload || jsonb_build_object('notes', repeat('x', 3400)) WHERE id = 'full'`)

	fields := postgres.WinnersDetoastFieldsForTest
	if len(fields) == 0 {
		t.Fatal("the record field list is empty: the live proof covers nothing")
	}
	var ors []string
	for _, f := range fields {
		ors = append(ors, fmt.Sprintf("(fact.payload->>'%s') IS DISTINCT FROM d.%s", f, f))
	}
	// Derived-expression pairs, old versus new, over the same record.
	ors = append(ors,
		`COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), '') IS DISTINCT FROM COALESCE(NULLIF(d.cve_id, ''), NULLIF(d.advisory_id, ''), '')`,
		`COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) IS DISTINCT FROM COALESCE(NULLIF(d.priority_score, '')::int, 0)`,
		`(CASE WHEN NULLIF(fact.payload->>'finding_id', '') IS NULL THEN 0 ELSE 1 END) IS DISTINCT FROM (CASE WHEN NULLIF(d.finding_id, '') IS NULL THEN 0 ELSE 1 END)`,
		`COALESCE(NULLIF(fact.payload->>'suppression_state', ''), 'active') IS DISTINCT FROM COALESCE(NULLIF(d.suppression_state, ''), 'active')`,
	)
	query := `SELECT count(*) FROM detoast_seed AS fact ` +
		postgres.WinnersDetoastFactLateralForTest +
		` WHERE ` + strings.Join(ors, "\n OR ")
	var mismatches int
	if err := conn.QueryRowContext(ctx, query).Scan(&mismatches); err != nil {
		t.Fatalf("field-equivalence differential: %v", err)
	}
	if mismatches != 0 {
		t.Fatalf("field-equivalence differential found %d mismatching rows: the record diverges from ->>", mismatches)
	}
	// Row presence: the guarded lateral must not drop scalar or NULL rows.
	// A count-only differential would pass on a shrunken table, and a bare
	// seed count would not prove the lateral preserves rows, so count the
	// lateral-joined output itself.
	var seedTotal, throughTotal int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM detoast_seed`).Scan(&seedTotal); err != nil {
		t.Fatalf("seed row count: %v", err)
	}
	if seedTotal != 18 {
		t.Fatalf("seed holds %d rows, want the 18 seeded fixtures", seedTotal)
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM detoast_seed AS fact `+postgres.WinnersDetoastFactLateralForTest).Scan(&throughTotal); err != nil {
		t.Fatalf("lateral row count: %v", err)
	}
	if throughTotal != seedTotal {
		t.Fatalf("guarded lateral preserves %d of %d seed rows: scalar or NULL rows lost", throughTotal, seedTotal)
	}

	// The array exception: `->` keeps a JSON null as jsonb null, while a
	// record maps it to SQL NULL. Pin the legacy semantics the body keeps.
	var keptNull, coalescedNull bool
	if err := conn.QueryRowContext(ctx, `SELECT (payload->'service_ids') IS NULL, COALESCE(payload->'service_ids', '[]'::jsonb) = 'null'::jsonb FROM detoast_seed WHERE id = 'arrays-null'`).Scan(&keptNull, &coalescedNull); err != nil {
		t.Fatalf("array null semantics: %v", err)
	}
	if keptNull || !coalescedNull {
		t.Fatalf("array null semantics changed: -> IS NULL = %v, COALESCE keeps null = %v", keptNull, coalescedNull)
	}
	var recordNull bool
	if err := conn.QueryRowContext(ctx, `SELECT r.service_ids IS NULL FROM detoast_seed, jsonb_to_record(payload) AS r(service_ids jsonb) WHERE id = 'arrays-null'`).Scan(&recordNull); err != nil {
		t.Fatalf("record array null semantics: %v", err)
	}
	if !recordNull {
		t.Fatal("record array extraction keeps JSON null: the documented array exception no longer holds")
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// The finding-fact statements #11197 retired, kept as the oracle the supports
// statements must equal (arbiter-b2b: "supports statements equal the fact
// statements kept as oracle"). They are the shipped text of the fan-out holder
// read, its key-range statements with the foreign-member token arm, and the mass
// probe, before the move.
const oracleFactKind = "'" + facts.ReducerSupplyChainImpactFindingFactKind + "'"

const oracleHoldersTail = `
SELECT DISTINCT candidates.scope_id
FROM candidates
JOIN ingestion_scopes AS scope
  ON scope.scope_id = candidates.scope_id
 AND scope.active_generation_id = candidates.generation_id
WHERE candidates.scope_id <> $2
ORDER BY candidates.scope_id
`

const oracleHoldersQuery = `
WITH candidates AS MATERIALIZED (
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = ` + oracleFactKind + `
      AND finding.is_tombstone = FALSE
      AND finding.payload->>'cve_id' = ANY($1::text[])
    UNION
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = ` + oracleFactKind + `
      AND finding.is_tombstone = FALSE
      AND finding.payload->>'advisory_id' = ANY($1::text[])
    UNION
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = ` + oracleFactKind + `
      AND finding.is_tombstone = FALSE
      AND finding.payload #> '{provenance,advisory_group_members}' ?| $1::text[]
)` + oracleHoldersTail

func oracleKeyRangeArm(expr string, bounded bool) string {
	upper := ""
	if bounded {
		upper = "\n      AND finding.fact_id < $4"
	}
	return `
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = ` + oracleFactKind + `
      AND finding.is_tombstone = FALSE
      AND finding.payload->>'` + expr + `' = ANY($1::text[])
      AND finding.fact_id >= $3` + upper
}

func oracleTokenArm(param string) string {
	return `
    SELECT finding.scope_id, finding.generation_id
    FROM fact_records AS finding
    WHERE finding.fact_kind = ` + oracleFactKind + `
      AND finding.is_tombstone = FALSE
      AND ` + postgres.SupplyChainImpactForeignMemberTokensFunction + `(finding.fact_id, finding.payload) && ` + param + `::text[]`
}

var oracleKeyRangeQuery = `
WITH candidates AS MATERIALIZED (` + oracleKeyRangeArm("cve_id", true) + `
    UNION` + oracleKeyRangeArm("advisory_id", true) + `
    UNION` + oracleTokenArm("$5") + `
)` + oracleHoldersTail

var oracleOpenKeyRangeQuery = `
WITH candidates AS MATERIALIZED (` + oracleKeyRangeArm("cve_id", false) + `
    UNION` + oracleKeyRangeArm("advisory_id", false) + `
    UNION` + oracleTokenArm("$4") + `
)` + oracleHoldersTail

// oracleMass returns the per-id mass the retired probe reported, modelled from
// exact fact counts instead of the probe's LATERAL shape, so the comparison does
// not depend on either statement's plan: each arm (cve_id, advisory_id, group
// member) reports its exact count at or under HolderMassProbeFillToCapRows and
// saturates to HolderMassProbeCap above it, the member arm joins only while the
// two cheap arms stay under the cap, and the total is capped (the probe's
// documented saturation, which marks an id hot and splits it).
func oracleMass(t *testing.T, ctx context.Context, db *sql.DB, id string) int64 {
	t.Helper()
	count := func(where string) int64 {
		var n int64
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_records WHERE fact_kind = `+oracleFactKind+` AND is_tombstone = FALSE AND `+where, id).Scan(&n); err != nil {
			t.Fatalf("oracle mass arm %q: %v", where, err)
		}
		return n
	}
	arm := func(n int64) int64 {
		if n > supplychaincore.HolderMassProbeFillToCapRows {
			return supplychaincore.HolderMassProbeCap
		}
		return n
	}
	total := arm(count(`payload->>'cve_id' = $1`)) + arm(count(`payload->>'advisory_id' = $1`))
	if total < supplychaincore.HolderMassProbeCap {
		total += arm(count(`payload #> '{provenance,advisory_group_members}' ? $1`))
	}
	return min(total, supplychaincore.HolderMassProbeCap)
}

// holderFamilySeed writes a corpus that exercises every arm and the #10643 token
// fixtures: a hot CVE across many scopes, advisory-only and member-only holders,
// own-key members, a hot foreign member, members stored as an array, an object, a
// string and a value over 1,024 bytes, a tombstoned copy, a superseded-generation
// copy, and a blank-id row.
func holderFamilySeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for i := 0; i < 12; i++ {
		fanOutSeedScope(t, ctx, db, fmt.Sprintf("hf-scope-%02d", i), "g2", "g1")
	}
	fanOutSeedScope(t, ctx, db, "hf-own", "g2")
	long := strings.Repeat("L", 1100)
	// The corpus plant is frozen finding history, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the holder reads under test run armed.
	postgresproof.SeedFindingFacts(t, db, func() {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1 || ':' || md5(i::text), 'hf-scope-' || lpad((i % 12)::text, 2, '0'),
       'hf-scope-' || lpad((i % 12)::text, 2, '0') || '/' || CASE WHEN i % 9 = 0 THEN 'g1' ELSE 'g2' END,
       $1, 'k' || i, '1.0.0', 'synthetic', 'reported', 'synthetic', 'k' || i, now(), now(), (i % 17 = 0),
       jsonb_build_object(
         'cve_id', CASE WHEN i % 4 = 0 THEN 'CVE-HOT' WHEN i % 11 = 0 THEN '' WHEN i % 13 = 0 THEN NULL ELSE 'CVE-' || (i % 50) END,
         'advisory_id', CASE WHEN i % 6 = 0 THEN 'GHSA-HOT' WHEN i % 7 = 0 THEN '' ELSE 'GHSA-' || (i % 40) END,
         'impact_status', 'affected_exact',
         'provenance', jsonb_build_object('advisory_group_members',
            CASE i % 8
              WHEN 0 THEN jsonb_build_array('GHSA-HOT', 'own-' || i)
              WHEN 1 THEN jsonb_build_array('adv-foreign-' || (i % 5), 'CVE-' || (i % 50))
              WHEN 2 THEN to_jsonb('adv-string-' || (i % 3))
              WHEN 3 THEN jsonb_build_object('k', 'adv-object-' || (i % 3))
              WHEN 4 THEN jsonb_build_array($2::text)
              ELSE jsonb_build_array('adv-foreign-hot')
            END))
FROM generate_series(1, 2400) AS i`,
			facts.ReducerSupplyChainImpactFindingFactKind, long); err != nil {
			t.Fatalf("seed family corpus: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// TestSupplyChainImpactSupportsHolderFamilyEqualsTheFactOracleLive proves the
// moved fan-out family returns what the retired fact statements returned, then
// keeps answering once every finding fact is deleted (#11197): the 5-id holder
// statement and its singletons, the 16-range key-range read with its open range
// and foreign-member token arm on one snapshot, the over-1,024-byte fallback, and
// the exact per-id mass, on a corpus with the #10643 token fixtures.
func TestSupplyChainImpactSupportsHolderFamilyEqualsTheFactOracleLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	// The seed plants frozen finding facts, so it runs in a scoped
	// postgresproof.SeedFindingFacts window: the proof database stays armed
	// and the holder reads under test run armed.
	postgresproof.SeedFindingFacts(t, db, func() {
		holderFamilySeed(t, ctx, db)
	})
	settleSupplyChainImpactSupports(t, ctx, db)

	chunks := [][]string{
		{"CVE-HOT", "CVE-3", "GHSA-HOT", "GHSA-7", "adv-foreign-hot"},
		{"CVE-1", "CVE-2", "GHSA-1", "adv-foreign-2", "own-8"},
		{"adv-string-1", "adv-object-2", "CVE-none", "GHSA-none", "adv-foreign-0"},
		{"CVE-HOT"},
		{"GHSA-HOT"},
		{"adv-foreign-hot"},
		{"adv-string-0"},
		{"adv-object-1"},
	}
	var oracleHolders, oracleRanged [][]string
	var oracleMasses []map[string]int64
	ranges := supplychaincore.HolderKeyRanges()
	for _, ids := range chunks {
		oracleHolders = append(oracleHolders, queryScopes(t, ctx, db, oracleHoldersQuery, ids, "hf-own"))
		seen := map[string]struct{}{}
		for i, r := range ranges {
			tokens := postgres.HolderMemberTokens(ids, i, len(ranges))
			var got []string
			if r.Upper == "" {
				got = oracleRangeRead(t, ctx, db, oracleOpenKeyRangeQuery, ids, "hf-own", r.Lower, "", tokens)
			} else {
				got = oracleRangeRead(t, ctx, db, oracleKeyRangeQuery, ids, "hf-own", r.Lower, r.Upper, tokens)
			}
			for _, s := range got {
				seen[s] = struct{}{}
			}
		}
		union := make([]string, 0, len(seen))
		for s := range seen {
			union = append(union, s)
		}
		slices.Sort(union)
		oracleRanged = append(oracleRanged, union)
		masses := map[string]int64{}
		for _, id := range ids {
			masses[id] = oracleMass(t, ctx, db, id)
		}
		oracleMasses = append(oracleMasses, masses)
	}
	if len(oracleHolders[0]) == 0 || len(oracleRanged[3]) == 0 {
		t.Fatal("test bug: the hot lookups must have holders")
	}

	// The write stop: every finding fact is deleted, only supports remain.
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE fact_kind = $1`, facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("delete finding facts: %v", err)
	}
	store := postgres.SupplyChainImpactFanOutStore{Beginner: postgres.SQLDB{DB: db}, DB: postgres.SQLDB{DB: db}, LeaseOwner: fanOutLiveOwner}
	for i, ids := range chunks {
		got, err := store.FindHolderScopes(ctx, "hf-own", ids)
		if err != nil {
			t.Fatalf("FindHolderScopes(%v): %v", ids, err)
		}
		if !sameStrings(got, oracleHolders[i]) {
			t.Errorf("FindHolderScopes(%v) = %v, want the fact oracle's %v", ids, got, oracleHolders[i])
		}
		read, err := store.FindHolderScopesByKeyRange(ctx, "hf-own", ids, ranges)
		if err != nil {
			t.Fatalf("FindHolderScopesByKeyRange(%v): %v", ids, err)
		}
		if !sameStrings(read.Scopes, oracleRanged[i]) {
			t.Errorf("FindHolderScopesByKeyRange(%v) = %v, want the fact oracle's %v", ids, read.Scopes, oracleRanged[i])
		}
		if !sameStrings(oracleRanged[i], oracleHolders[i]) {
			t.Errorf("fact oracle: the 16-range union %v differs from the one-statement read %v for %v", oracleRanged[i], oracleHolders[i], ids)
		}
		masses, err := store.EstimateHolderMasses(ctx, ids)
		if err != nil {
			t.Fatalf("EstimateHolderMasses(%v): %v", ids, err)
		}
		for id, want := range oracleMasses[i] {
			if masses[id] != want {
				t.Errorf("mass of %q = %d, want the exact fact count %d", id, masses[id], want)
			}
		}
	}

	// The over-1,024-byte lookup id takes the one-statement fallback and still
	// answers from supports.
	longID := strings.Repeat("L", 1100)
	got, err := store.FindHolderScopesByKeyRange(ctx, "hf-own", []string{longID, "CVE-HOT"}, ranges)
	if err != nil || !got.Fallback || len(got.Scopes) == 0 {
		t.Fatalf("over-length lookup = %+v, %v, want the fallback statement and the CVE-HOT holders", got, err)
	}
	// Blank ids never reach a statement.
	if blank, err := store.FindHolderScopes(ctx, "hf-own", []string{"", ""}); err != nil || len(blank) != 0 {
		t.Fatalf("blank ids = %v, %v, want no holders", blank, err)
	}
}

// oracleRangeRead runs one retired key-range fact statement on a fresh snapshot.
func oracleRangeRead(t *testing.T, ctx context.Context, db *sql.DB, query string, ids []string, own, lower, upper string, tokens []string) []string {
	t.Helper()
	args := []any{array.StringArray(ids), own, lower}
	if upper != "" {
		args = append(args, upper)
	}
	args = append(args, array.StringArray(tokens))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("oracle key-range read: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

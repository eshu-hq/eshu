// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	adaptSplitOwn   = "adapt:own"
	adaptSplitTomb  = "adapt:tomb-only"
	adaptSplitHot   = "CVE-2026-9901"
	adaptSplitWarm  = "CVE-2026-9902"
	adaptSplitCold1 = "CVE-2026-9903"
	adaptSplitCold2 = "CVE-2026-9904"
	adaptSplitCold3 = "CVE-2026-9905"
	adaptSplitMbr   = "GHSA-adapt-9901"
)

// adaptSplitSeedFixture seeds a hot-CVE holder fan-out fixture: a hot CVE
// with 2,000 live holder rows, a warm CVE, a member-flavored id, three cold
// ids, and the exclusion edges (own-scope live rows, tombstoned hot rows).
// Background rows carry distinct ids so only the named ids concentrate.
func adaptSplitSeedFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	holders := []string{"adapt:hold-a", "adapt:hold-b", "adapt:hold-c"}
	fanOutSeedScope(t, ctx, db, adaptSplitOwn, "g1")
	fanOutSeedScope(t, ctx, db, adaptSplitTomb, "g1")
	for _, scope := range holders {
		fanOutSeedScope(t, ctx, db, scope, "g1")
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'adapt:bg:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'adapt:bg:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'adapt:bg', now(), now(), false,
  jsonb_build_object(
    'cve_id', 'CVE-2026-' || lpad((g % 40000)::text, 5, '0'),
    'advisory_id', 'GHSA-adapt-' || lpad((g % 40000)::text, 5, '0'),
    'impact_status', 'active')
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id LIKE 'adapt:%') scopes
CROSS JOIN generate_series(1, 8000) g`, facts.ReducerSupplyChainImpactFindingFactKind); err != nil {
		t.Fatalf("seed background findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'adapt:hot:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'adapt:hot:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'adapt:hot', now(), now(), false,
  jsonb_build_object(
    'cve_id', $2::text,
    'advisory_id', 'GHSA-adapt-distinct-' || lpad(g::text, 5, '0'),
    'impact_status', 'active')
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id IN ('adapt:hold-a', 'adapt:hold-b')) scopes
CROSS JOIN generate_series(1, 1000) g`, facts.ReducerSupplyChainImpactFindingFactKind, adaptSplitHot); err != nil {
		t.Fatalf("seed hot CVE findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'adapt:warm:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'adapt:warm:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'adapt:warm', now(), now(), false,
  jsonb_build_object(
    'cve_id', $2::text,
    'advisory_id', 'GHSA-adapt-distinct-w-' || lpad(g::text, 5, '0'),
    'impact_status', 'active')
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id IN ('adapt:hold-a', 'adapt:hold-c')) scopes
CROSS JOIN generate_series(1, 35) g`, facts.ReducerSupplyChainImpactFindingFactKind, adaptSplitWarm); err != nil {
		t.Fatalf("seed warm CVE findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'adapt:mbr:' || scopes.scope_id || ':' || g, scopes.scope_id, scopes.active_generation_id, $1,
  'adapt:mbr:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'adapt:mbr', now(), now(), false,
  jsonb_build_object(
    'cve_id', 'CVE-2026-distinct-m-' || lpad(g::text, 5, '0'),
    'advisory_id', 'GHSA-adapt-distinct-m-' || lpad(g::text, 5, '0'),
    'impact_status', 'active',
    'provenance', jsonb_build_object('advisory_group_members', jsonb_build_array($2::text)))
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id = 'adapt:hold-b') scopes
CROSS JOIN generate_series(1, 100) g`, facts.ReducerSupplyChainImpactFindingFactKind, adaptSplitMbr); err != nil {
		t.Fatalf("seed member findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'adapt:cold:' || s.scope_id || ':' || g, s.scope_id, s.active_generation_id, $1,
  'adapt:cold:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'adapt:cold', now(), now(), false,
  jsonb_build_object(
    'cve_id', (ARRAY[$2::text, $3::text, $4::text])[((g - 1) % 3) + 1],
    'advisory_id', 'GHSA-adapt-distinct-c-' || lpad(g::text, 5, '0'),
    'impact_status', 'active')
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id = 'adapt:hold-c') s
CROSS JOIN generate_series(1, 6) g`, facts.ReducerSupplyChainImpactFindingFactKind,
		adaptSplitCold1, adaptSplitCold2, adaptSplitCold3); err != nil {
		t.Fatalf("seed cold findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'adapt:edge:' || s.scope_id || ':' || g, s.scope_id, s.active_generation_id, $1,
  'adapt:edge:' || g, '1.0.0', 'synthetic', 'reported', 'synthetic', 'adapt:edge', now(), now(),
  s.scope_id = $3,
  jsonb_build_object('cve_id', $2::text, 'advisory_id', 'GHSA-adapt-edge', 'impact_status', 'active')
FROM (SELECT scope_id, active_generation_id FROM ingestion_scopes
      WHERE scope_id IN ($4, $3)) s
CROSS JOIN generate_series(1, 5) g`, facts.ReducerSupplyChainImpactFindingFactKind,
		adaptSplitHot, adaptSplitTomb, adaptSplitOwn); err != nil {
		t.Fatalf("seed exclusion edges: %v", err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE) fact_records`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}
}

// TestHolderAdaptiveSplitRowIdentityLive is the #10643 row-identity proof:
// the scope set of one 5-id holder statement equals the union of the five
// singleton statements, in both EXCEPT directions, on a hot-CVE fixture with
// the exclusion edges. Both sides call the shipped
// FindSupplyChainImpactHoldersQuery constant — the singleton side by parameter
// restriction, so a change to the query shape cannot skew one side — and the
// byte-prefix guard pins the MATERIALIZED premise the proof relies on.
func TestHolderAdaptiveSplitRowIdentityLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	trimmed := strings.TrimSpace(postgres.FindSupplyChainImpactHoldersQuery)
	if !strings.HasPrefix(trimmed, holderCoveringCTEHead) {
		t.Fatal("derived split is not a byte-prefix of the shipped constant")
	}
	adaptSplitSeedFixture(t, ctx, db)

	ids := []string{adaptSplitHot, adaptSplitWarm, adaptSplitCold1, adaptSplitCold2, adaptSplitCold3}
	startMulti := time.Now()
	want := queryScopes(t, ctx, db, postgres.FindSupplyChainImpactHoldersQuery, ids, adaptSplitOwn)
	multiElapsed := time.Since(startMulti)

	var singles []string
	var singleElapsed, singleMax time.Duration
	for _, id := range ids {
		start := time.Now()
		got := queryScopes(t, ctx, db, postgres.FindSupplyChainImpactHoldersQuery, []string{id}, adaptSplitOwn)
		elapsed := time.Since(start)
		singleElapsed += elapsed
		if elapsed > singleMax {
			singleMax = elapsed
		}
		if len(got) > 0 {
			singles = append(singles, got...)
		}
	}
	slices.Sort(singles)
	got := slices.Compact(singles)
	t.Logf("holder adaptive split timing: multi-id %v, %d singleton statements in %v total (max single %v)",
		multiElapsed, len(ids), singleElapsed, singleMax)
	if !slices.Equal(got, want) {
		t.Fatalf("scope sets differ: multi=%v singles-union=%v", want, got)
	}
	if len(want) != 3 {
		t.Fatalf("holder scopes = %v, want the 3 holder scopes", want)
	}
	for _, tc := range []struct {
		name        string
		first, rest []string
	}{
		{"multi EXCEPT singles", want, got},
		{"singles EXCEPT multi", got, want},
	} {
		var missing int
		rows, err := db.QueryContext(ctx,
			`SELECT count(*) FROM ((SELECT unnest($1::text[]) AS s) EXCEPT (SELECT unnest($2::text[]) AS s)) d`,
			tc.first, tc.rest)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if rows.Next() {
			if err := rows.Scan(&missing); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if missing != 0 {
			t.Fatalf("%s = %d rows, want bidirectional row identity (multi=%v singles=%v)", tc.name, missing, want, got)
		}
	}
	t.Logf("holder adaptive split row identity: %d scopes identical multi vs split-single %v", len(want), want)
}

// TestHolderMassProbeLive proves the #10643 mass probe: one row per requested
// id with its live finding-row count, zero for an unknown id, on the hot
// fixture. The probe counts live rows without the active-generation join, so
// the hot count includes the 5 own-scope edge rows and excludes the 5
// tombstoned rows.
func TestHolderMassProbeLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	adaptSplitSeedFixture(t, ctx, db)

	store := fanOutLiveStore(db)
	masses, err := store.EstimateHolderMasses(ctx, []string{
		adaptSplitHot, adaptSplitWarm, adaptSplitCold1, "CVE-2026-nope",
	})
	if err != nil {
		t.Fatalf("EstimateHolderMasses() error = %v", err)
	}
	if len(masses) != 4 {
		t.Fatalf("EstimateHolderMasses() returned %d ids, want one row per requested id: %v", len(masses), masses)
	}
	t.Logf("holder mass probe: %v", masses)
	if masses[adaptSplitHot] != 2005 {
		t.Fatalf("hot mass = %d, want 2005 live rows (2000 holder + 5 own-scope, tombstoned excluded)", masses[adaptSplitHot])
	}
	if masses[adaptSplitWarm] != 70 {
		t.Fatalf("warm mass = %d, want 70", masses[adaptSplitWarm])
	}
	if masses[adaptSplitCold1] != 2 {
		t.Fatalf("cold mass = %d, want 2", masses[adaptSplitCold1])
	}
	if masses["CVE-2026-nope"] != 0 {
		t.Fatalf("unknown mass = %d, want 0", masses["CVE-2026-nope"])
	}
	if empty, err := store.EstimateHolderMasses(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("EstimateHolderMasses(nil) = %v, %v, want empty", empty, err)
	}

	startProbe := time.Now()
	if _, err := store.EstimateHolderMasses(ctx, []string{adaptSplitHot}); err != nil {
		t.Fatalf("EstimateHolderMasses() error = %v", err)
	}
	t.Logf("holder mass probe latency: %v", time.Since(startProbe))
}

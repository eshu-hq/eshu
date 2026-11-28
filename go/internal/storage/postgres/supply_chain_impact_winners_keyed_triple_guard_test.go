// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"strings"
	"testing"
)

// Hermetic pins for the #9975 round-6 triple-guarded probe: the flip selects
// exactly one of the two budgeted probe texts, both render from a single
// constructor per shape, and the new text carries the five plain-column
// equalities with triple-only guards and no fact_records filters.

// TestWinnersKeyedProbeVariantSelectsExactlyOneProbeText pins the flip: the
// guarded text runs only once the fill marker completes, the shipped
// payload-expression text before that (including a missing marker row).
func TestWinnersKeyedProbeVariantSelectsExactlyOneProbeText(t *testing.T) {
	t.Parallel()

	if got := winnersKeyedProbeVariant(true); got != insertWinnersKeyedNarrowKeyPresenceBatchSQL {
		t.Fatal("a complete fill marker does not select the triple-guarded probe")
	}
	if got := winnersKeyedProbeVariant(false); got != insertWinnersKeyedNarrowBatchSQL {
		t.Fatal("an incomplete fill marker does not select the shipped probe")
	}
	// Both variants accumulate into the same narrow table over the same
	// output columns, so the flip cannot split what the tick builds from
	// what it attributes: only the match predicate changes.
	for name, stmt := range map[string]string{
		"shipped":             insertWinnersKeyedNarrowBatchSQL,
		"guarded":             insertWinnersKeyedNarrowKeyPresenceBatchSQL,
		"fallback":            buildWinnersKeyedNarrowFromSupportsSQL,
		"guarded whole-chunk": buildWinnersKeyedNarrowKeyPresenceSQL,
	} {
		if !strings.Contains(stmt, "INSERT INTO "+winnersKeyedNarrowTable) && !strings.Contains(stmt, "CREATE TEMP TABLE "+winnersKeyedNarrowTable) {
			t.Errorf("%s probe does not write the narrow table %q", name, winnersKeyedNarrowTable)
		}
		if !strings.Contains(stmt, winnersKeyedNarrowColumns) {
			t.Errorf("%s probe does not select the probe output columns %q", name, winnersKeyedNarrowColumns)
		}
	}
}

// TestWinnersKeyedProbeTwinsShareOneConstructorPerShape pins that each probe
// shape's whole-chunk build and budgeted per-identifier insert render from the
// same constructor, so the two cannot silently split.
func TestWinnersKeyedProbeTwinsShareOneConstructorPerShape(t *testing.T) {
	t.Parallel()

	guardedBatchBody := strings.TrimPrefix(insertWinnersKeyedNarrowKeyPresenceBatchSQL, "INSERT INTO "+winnersKeyedNarrowTable+"\n")
	if guardedBatchBody != winnersKeyedNarrowProbeSelectKeyPresence(winnersKeyedBatchIdentsTable) {
		t.Error("the guarded budgeted probe is not the guarded constructor over the batch table")
	}
	guardedWholeBody := strings.TrimPrefix(buildWinnersKeyedNarrowKeyPresenceSQL,
		"\nCREATE TEMP TABLE winners_keyed_narrow ON COMMIT DROP AS\n")
	if guardedWholeBody != winnersKeyedNarrowProbeSelectKeyPresence(winnersKeyedIdentsTable) {
		t.Error("the guarded whole-chunk build is not the guarded constructor over the identifier table")
	}
}

// TestWinnersKeyedGuardedProbeDropsTripleGuardsAndNoFactFilters pins the
// guarded probe's shape: five plain-column equalities with NO key-presence
// guards on the triple legs (Refs #11691: the supports writer stores a
// missing key and a JSON null as ”, so the plain equality already reads both
// as the coalesced identifier), plain COALESCE c0/advisory legs with no
// guards, and no fact_records row source or finding-kind filter (a support
// exists only for a live finding fact). The round-6 guard conjuncts are gone;
// the key_presence column, fill, and covering index stay (see the forbidden
// list), the probe just no longer filters on them.
func TestWinnersKeyedGuardedProbeDropsTripleGuardsAndNoFactFilters(t *testing.T) {
	t.Parallel()

	probe := winnersKeyedNarrowProbeSelectKeyPresence(winnersKeyedIdentsTable)
	for _, frag := range []string{
		"FROM supply_chain_impact_finding_supports AS s",
		"s.package_id = ident.package_id",
		"s.repository_id = ident.repository_id",
		"s.subject_digest = ident.subject_digest",
		"COALESCE(NULLIF(s.cve_id, ''), NULLIF(s.advisory_id, ''), '') = ident.c0",
		"s.advisory_id = ident.advisory_id",
		"MATERIALIZED",
		"OFFSET 0",
	} {
		if !strings.Contains(probe, frag) {
			t.Errorf("guarded probe lost the fragment %q", frag)
		}
	}
	for _, dropped := range []string{
		"(ident.package_id <> '' OR (s.key_presence & 1) <> 0)",
		"(ident.repository_id <> '' OR (s.key_presence & 2) <> 0)",
		"(ident.subject_digest <> '' OR (s.key_presence & 4) <> 0)",
	} {
		if strings.Contains(probe, dropped) {
			t.Errorf("guarded probe still carries the round-6 guard %q: the ruled leniency drops all three (Refs #11691)", dropped)
		}
	}
	for _, forbidden := range []string{
		"fact_records",
		"f.payload",
		"payload->>",
		"fact_kind",
		"is_tombstone",
		"cve_id & 8",
		"(s.key_presence & 8)",
		"(s.key_presence & 16)",
	} {
		if strings.Contains(probe, forbidden) {
			t.Errorf("guarded probe contains %q: advisory keys carry no bits and the probe reads no payload", forbidden)
		}
	}
}

// TestKeyPresenceMigrationAndIndexAreInvalidCleanupCovered pins the migration
// shapes the schema apply path relies on: 362 is one idempotent additive
// column (re-runnable), and the extractor that drops INVALID concurrent
// indexes before re-running a migration finds the 363 index name in its file,
// so a failed 363 build is cleaned up on retry.
func TestKeyPresenceMigrationAndIndexAreInvalidCleanupCovered(t *testing.T) {
	t.Parallel()

	raw362, err := os.ReadFile("migrations/362_supply_chain_impact_supports_key_presence.sql")
	if err != nil {
		t.Fatalf("read migration 362: %v", err)
	}
	body362 := migrationStatement(string(raw362))
	for _, want := range []string{
		"ALTER TABLE IF EXISTS supply_chain_impact_finding_supports ADD COLUMN IF NOT EXISTS key_presence SMALLINT NOT NULL DEFAULT 0",
	} {
		if !strings.Contains(body362, want) {
			t.Errorf("migration 362 body lacks %q:\n%s", want, body362)
		}
	}
	if strings.Count(body362, ";") != 1 {
		t.Errorf("migration 362 must hold exactly one statement:\n%s", body362)
	}
	raw363, err := os.ReadFile("migrations/363_supply_chain_impact_supports_keyed_triple_guard_idx.sql")
	if err != nil {
		t.Fatalf("read migration 363: %v", err)
	}
	got := concurrentIndexNamesForInvalidCleanup(string(raw363))
	if len(got) != 1 || got[0] != "supply_chain_impact_finding_supports_keyed_triple_guard_idx" {
		t.Errorf("invalid-cleanup names for migration 363 = %v, want exactly the triple-guard index", got)
	}
}

// TestKeyPresenceFillMarkerNeverExceedsTheSupportsContractVersionOrTouchesItsRow
// is the reader-503 hazard pin (supports backfill, "contract_version > $2"),
// mirrored from the stable-key fill: the fill's marker row carries a
// contract_version at most the supports contract version, is a different row
// from the supports marker, and the flip reads only its own marker name.
func TestKeyPresenceFillMarkerNeverExceedsTheSupportsContractVersionOrTouchesItsRow(t *testing.T) {
	t.Parallel()

	if SupplyChainImpactKeyPresenceFillContractVersion > SupplyChainImpactSupportsContractVersion {
		t.Fatalf("the fill marker's contract_version %d exceeds the supports contract version %d: every supports reader would answer 503",
			SupplyChainImpactKeyPresenceFillContractVersion, SupplyChainImpactSupportsContractVersion)
	}
	if SupplyChainImpactKeyPresenceFillMarker == SupplyChainImpactSupportsBackfillMarker {
		t.Fatal("the fill shares the supports backfill's marker row")
	}
	if SupplyChainImpactKeyPresenceFillMarker == SupplyChainImpactStableKeyFillMarker {
		t.Fatal("the fill shares the stable-key fill's marker row")
	}
	// The marker name rides the bind parameter (the chunk passes
	// SupplyChainImpactKeyPresenceFillMarker as $1), so the read pins the
	// marker row filter, not the name literal.
	if !strings.Contains(selectWinnersKeyPresenceFillReadySQL, "marker_name = $1") {
		t.Fatal("the flip's readiness read does not filter on the marker row")
	}
}

// TestKeyPresenceFillSharesTheWriterExpression pins the single source: the
// writer projection's key_presence column is the fill expression over the
// fact alias, so the backfill cannot disagree with the writer on a bit.
func TestKeyPresenceFillSharesTheWriterExpression(t *testing.T) {
	t.Parallel()

	var found bool
	for _, column := range supplyChainImpactSupportFillOnlyColumns {
		if column.name != "key_presence" {
			continue
		}
		found = true
		if column.expr != supplyChainImpactKeyPresenceFillExpr("fact") {
			t.Fatalf("the writer's key_presence expression is not the fill expression over the fact alias:\n%s", column.expr)
		}
	}
	if !found {
		t.Fatal("the writer projection has no key_presence fill-only column")
	}
	for _, alias := range []string{"s", "x"} {
		expr := supplyChainImpactKeyPresenceFillExpr(alias)
		for _, bit := range []string{
			alias + ".payload->>'package_id' IS NOT NULL THEN 1",
			alias + ".payload->>'repository_id' IS NOT NULL THEN 2",
			alias + ".payload->>'subject_digest' IS NOT NULL THEN 4",
		} {
			if !strings.Contains(expr, bit) {
				t.Errorf("the fill expression over %q lacks %q", alias, bit)
			}
		}
	}
}

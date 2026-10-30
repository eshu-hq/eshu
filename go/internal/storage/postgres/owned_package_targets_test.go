// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestListOwnedPackageDependencyTargetsQueryIsActiveAndBounded(t *testing.T) {
	t.Parallel()

	query := listOwnedPackageDependencyTargetsQuery(true, false)
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.fact_kind = 'content_entity'",
		"fact.source_system = 'git'",
		"fact.payload->>'entity_type' = 'Variable'",
		"fact.payload->'entity_metadata'->>'config_kind' = 'dependency'",
		"fact.payload->'entity_metadata'->>'package_manager' = ANY($1::text[])",
		"COALESCE((fact.payload->'entity_metadata'->>'lockfile') = 'true', FALSE) AS lockfile",
		"COALESCE(fact.payload->'entity_metadata'->>'source_location', '') AS source_location",
		"SELECT DISTINCT ON (ecosystem, package_name, version)",
		"CASE WHEN source_location <> '' THEN 0 ELSE 1 END ASC",
		"ROW_NUMBER() OVER",
		"COUNT(*) OVER () AS total_targets",
		"sha256(convert_to(COALESCE(ecosystem, '') || '|' || COALESCE(package_name, '') || '|' || COALESCE(version, ''), 'UTF8')) AS key_hash",
		"MOD(key_hash_low24::bigint, slot_count) AS slot_of_key",
		"MOD($3::bigint, slot_count) AS window_slot",
		"COUNT(*) OVER (PARTITION BY slot_of_key) AS slot_size",
		"FROM (SELECT COUNT(*) AS total_targets FROM distinct_targets) AS counted_targets",
		"FROM window_stats\nLEFT JOIN window_targets\n  ON window_targets.slot_of_key = window_stats.window_slot",
		"COALESCE(window_targets.slot_size, 0) AS slot_size",
		"ORDER BY window_targets.target_rank ASC\nLIMIT $2",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("listOwnedPackageDependencyTargetsQuery missing %q:\n%s", want, query)
		}
	}
	// The rotating offset window is gone (#9422), and md5 is unavailable on a
	// FIPS server, so neither may come back.
	// P has one SQL definition, rendered once per target and once on the
	// window stats row.
	if got := strings.Count(query, ownedPackageSlotCountExpression); got != 2 {
		t.Fatalf("slot count expression rendered %d times, want 2:\n%s", got, query)
	}
	for _, banned := range []string{"rotated_rank", "md5(", "$3::bigint, total_targets"} {
		if strings.Contains(query, banned) {
			t.Fatalf("listOwnedPackageDependencyTargetsQuery contains %q:\n%s", banned, query)
		}
	}
}

// TestOwnedPackageSlotDifferentialDerivesFromTheShippedQuery is the prefix
// guard for the live differential: the all-slots query must be the shipped
// statement up to the final SELECT, byte for byte, for both identity modes.
func TestOwnedPackageSlotDifferentialDerivesFromTheShippedQuery(t *testing.T) {
	t.Parallel()

	for _, versionSpecific := range []bool{true, false} {
		query := listOwnedPackageDependencyTargetsQuery(versionSpecific, true)
		if got := strings.Count(query, ownedPackageSlotSelectMarker); got != 1 {
			t.Fatalf("final SELECT marker appears %d times, want exactly 1:\n%s", got, query)
		}
		derived := ownedPackageAllSlotsQuery(versionSpecific, true)
		prefix, ok := strings.CutSuffix(derived, ownedPackageAllSlotsTail)
		if !ok {
			t.Fatalf("differential query does not end with the all-slots tail")
		}
		if !strings.HasPrefix(query, prefix) {
			t.Fatalf("differential prefix is not a byte-prefix of the shipped query")
		}
		if !strings.HasSuffix(prefix, "\n") || !strings.Contains(prefix, "window_targets AS (") {
			t.Fatalf("differential prefix does not end at the CTE chain:\n%s", prefix)
		}
	}
	if !strings.Contains(listOwnedPackageDependencyTargetsQuery(false, false),
		"sha256(convert_to(COALESCE(ecosystem, '') || '|' || COALESCE(package_name, ''), 'UTF8'))") {
		t.Fatalf("package-level query must hash the package identity without the version")
	}
}

// ownedPackageAllSlotsTail replaces the shipped final SELECT with a read-only
// tail that returns every target with its slot, so a live test can compare the
// reader's per-window rows with the whole partition.
const ownedPackageAllSlotsTail = `SELECT ecosystem, package_name, version, slot_of_key, slot_count, total_targets, slot_size
FROM window_targets
ORDER BY target_rank ASC
LIMIT $2
`

// ownedPackageAllSlotsQuery derives the differential query from the shipped
// statement by truncating it at the final SELECT marker.
func ownedPackageAllSlotsQuery(versionSpecific bool, expandAliases bool) string {
	query := listOwnedPackageDependencyTargetsQuery(versionSpecific, expandAliases)
	chain, _, found := strings.Cut(query, ownedPackageSlotSelectMarker)
	if !found {
		// The hermetic prefix guard fails first with the reason; return a
		// query that cannot run so a live caller fails too.
		return "SELECT missing_final_select_marker"
	}
	return chain + ownedPackageAllSlotsTail
}

func TestListOwnedPackageDependencyTargetsQueryCanUsePackageLevelIdentity(t *testing.T) {
	t.Parallel()

	query := listOwnedPackageDependencyTargetsQuery(false, false)
	if !strings.Contains(query, "SELECT DISTINCT ON (ecosystem, package_name)") {
		t.Fatalf("package-level target query should distinct by package identity:\n%s", query)
	}
	if strings.Contains(query, "SELECT DISTINCT ON (ecosystem, package_name, version)") {
		t.Fatalf("package-level target query should not spend package-registry budget on per-version rows:\n%s", query)
	}
}

func TestOwnedPackageDependencyTargetLimitSupportsFullCorpusBudgets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  int
		want int
	}{
		{name: "default", raw: 0, want: 100},
		{name: "historical cap no longer downshifts", raw: 1000, want: 1000},
		{name: "max", raw: 5000, want: 5000},
		{name: "over max clamps", raw: 5001, want: 5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ownedPackageDependencyTargetLimit(tc.raw); got != tc.want {
				t.Fatalf("ownedPackageDependencyTargetLimit(%d) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestOwnedPackageEcosystemAliasExpansionSharesOneTable(t *testing.T) {
	t.Parallel()

	raw := listOwnedPackageDependencyTargetsQuery(true, false)
	if strings.Contains(raw, "CASE LOWER(") {
		t.Fatalf("raw package_manager query must not fold aliases:\n%s", raw)
	}
	expanded := listOwnedPackageDependencyTargetsQuery(true, true)
	for _, want := range []string{
		"CASE LOWER(fact.payload->'entity_metadata'->>'package_manager')",
		"WHEN 'gradle' THEN 'maven'",
		"WHEN 'gomod' THEN 'go'",
		"END AS ecosystem",
		"SELECT DISTINCT ON (ecosystem, package_name, version)",
		"fact.payload->'entity_metadata'->>'sbt_cross_version' AS sbt_cross_version",
		"fact.payload->'entity_metadata'->>'sbt_binary_artifact' AS sbt_binary_artifact",
		"window_targets.sbt_cross_version",
		"window_targets.sbt_binary_artifact",
	} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("alias-expanded query missing %q:\n%s", want, expanded)
		}
	}
	if strings.Contains(expanded, "WHEN 'sbt'") {
		t.Fatalf("sbt must never fold onto another ecosystem in SQL:\n%s", expanded)
	}
	if got, want := ownedPackageManagerFilterValues([]string{"go", "maven"}, true, true),
		[]string{"go", "go-module", "go_module", "gomod", "gradle", "maven", "sbt"}; !slices.Equal(got, want) {
		t.Fatalf("expanded filter values with sbt = %#v, want %#v", got, want)
	}
	// A registry-style read leaves the sbt flag off: the filter stays the
	// plain alias expansion with no sbt rows, so registry maven reads keep
	// the base selection and drop nothing (#9313 review).
	if got, want := ownedPackageManagerFilterValues([]string{"go", "maven"}, true, false),
		[]string{"go", "go-module", "go_module", "gomod", "gradle", "maven"}; !slices.Equal(got, want) {
		t.Fatalf("expanded filter values without sbt = %#v, want %#v", got, want)
	}
	if got, want := ownedPackageManagerFilterValues([]string{"maven"}, false, false), []string{"maven"}; !slices.Equal(got, want) {
		t.Fatalf("raw filter values = %#v, want %#v", got, want)
	}
}

// ownedPackageSlotOracleTail replaces the shipped final SELECT with a
// read-only tail that returns each target's slot key parts, the statement's
// 24-bit hash value and its slot, so a Go oracle can recompute both.
const ownedPackageSlotOracleTail = `SELECT ecosystem, package_name, version, key_hash_low24, slot_of_key, slot_count
FROM window_targets
ORDER BY target_rank ASC
LIMIT $2
`

// ownedPackageSlotOracleLow24 is the D1 slot hash computed in Go:
// sha256 of the UTF-8 key, low 24 bits read as byte 0 + 256*byte 1 +
// 65536*byte 2. A query that reorders or drops a byte disagrees with it.
func ownedPackageSlotOracleLow24(key string) int64 {
	sum := sha256.Sum256([]byte(key))
	return int64(sum[0]) + 256*int64(sum[1]) + 65536*int64(sum[2])
}

// assertOwnedPackageSlotHashOracle pins the D1 slot contract byte for byte:
// every target's key_hash_low24 and slot_of_key, computed by the CTE chain of
// the shipped statement, must equal the Go oracle's value for the key
// ecosystem|package_name|version with a NULL version as an empty string. The
// read uses the slot fixture's target limit, so P comes from the fill rule.
func assertOwnedPackageSlotHashOracle(ctx context.Context, t *testing.T, db *sql.DB, total int64) {
	t.Helper()
	chain := ownedPackageAllSlotsQuery(true, true)
	chain = chain[:len(chain)-len(ownedPackageAllSlotsTail)]
	rows, err := db.QueryContext(ctx, chain+ownedPackageSlotOracleTail,
		ownedPackageManagerFilterValues([]string{"npm", "rubygems"}, true, false), int64(100000), int64(0), slotLiveTargetLimit)
	if err != nil {
		t.Fatalf("slot oracle query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var checked int64
	for rows.Next() {
		var (
			ecosystem, name    string
			version            sql.NullString
			low24, slot, count int64
		)
		if err := rows.Scan(&ecosystem, &name, &version, &low24, &slot, &count); err != nil {
			t.Fatalf("scan slot oracle row: %v", err)
		}
		key := ecosystem + "|" + name + "|" + version.String
		if want := ownedPackageSlotOracleLow24(key); low24 != want {
			t.Fatalf("key %q hash low24 = %d, want %d (sha256 bytes 0, 1, 2)", key, low24, want)
		}
		if want := low24 % count; slot != want {
			t.Fatalf("key %q slot = %d, want low24 mod P = %d", key, slot, want)
		}
		checked++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("slot oracle rows: %v", err)
	}
	if checked != total {
		t.Fatalf("slot oracle checked %d targets, want T=%d", checked, total)
	}
}

// assertOwnedPackageEmptySlotWindow reads a slot that holds no target and
// proves the window still reports T, P and the slot with slot_size 0, on the
// returned page and on the read span's window event. A target limit of 1 gives
// P = next_pow2(2T), so most slots of the fixture are empty; the oracle picks
// the lowest one.
func assertOwnedPackageEmptySlotWindow(ctx context.Context, t *testing.T, store *FactStore, db *sql.DB, total int64) {
	t.Helper()
	const targetLimit = 1
	wantP := workflow.OwnedPackageTargetSlotCount(total, targetLimit)
	occupied := map[int64]bool{}
	rows, err := db.QueryContext(ctx, ownedPackageAllSlotsQuery(true, true),
		ownedPackageManagerFilterValues([]string{"npm", "rubygems"}, true, false), int64(100000), int64(0), targetLimit)
	if err != nil {
		t.Fatalf("all-slots query: %v", err)
	}
	for rows.Next() {
		var ecosystem, name string
		var version sql.NullString
		var slot, count, size, rowTotal int64
		if err := rows.Scan(&ecosystem, &name, &version, &slot, &count, &rowTotal, &size); err != nil {
			t.Fatalf("scan all-slots row: %v", err)
		}
		occupied[slot] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("all-slots rows: %v", err)
	}
	_ = rows.Close()
	emptySlot := int64(-1)
	for slot := int64(0); slot < wantP; slot++ {
		if !occupied[slot] {
			emptySlot = slot
			break
		}
	}
	if emptySlot < 0 {
		t.Fatalf("no empty slot among %d slots for T=%d", wantP, total)
	}

	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	defer otel.SetTracerProvider(previous)
	// Bucket = a later lap of the same slot, so the slot comes from bucket mod P.
	page, err := store.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems: []string{"npm", "rubygems"}, Limit: targetLimit + 1, TargetLimit: targetLimit,
		VersionSpecific: true, ExpandPackageManagerAliases: true, RotationBucket: 3*wantP + emptySlot,
	})
	if err != nil {
		t.Fatalf("empty slot %d read error = %v, want nil", emptySlot, err)
	}
	want := workflow.OwnedPackageDependencyTargetWindow{TotalTargets: total, SlotCount: wantP, Slot: emptySlot}
	if page.Window != want || len(page.Targets) != 0 || page.Window.Truncated() {
		t.Fatalf("empty slot window = %#v with %d targets, want %#v, no targets, no truncation",
			page.Window, len(page.Targets), want)
	}
	assertOwnedPackageWindowEvent(t, recorder, page.Window)
}

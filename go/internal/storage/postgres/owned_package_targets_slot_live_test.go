// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9422 PostgreSQL 18.6 full-bootstrap proof that the owned-package reader's hash-slot window plans every target of a 319-row minority ecosystem within P windows while a 2,000-row majority block grows by one target per window, and that the window stats report T, P, slot and slot_size; isolated bootstrap schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	slotLiveMajorityTargets = 2000
	slotLiveMinorityTargets = 319
	slotLiveTargetLimit     = 150
	// slotLiveFirstBucket is a realistic 30-minute rotation bucket (the QA
	// coordinator read bucket 996,226 on 2026-10-28), so the shipped
	// bucket*limit offset is large against T, as it is in production.
	slotLiveFirstBucket = int64(996226)
)

// TestListOwnedPackageDependencyTargetsSlotCoverageLive proves the #9422
// coverage contract on a real Postgres: while T grows by one target per
// window, every target of a small ecosystem that sorts last in the global
// order is planned within P consecutive windows, P being the slot count the
// fill rule derives from T and the target limit.
func TestListOwnedPackageDependencyTargetsSlotCoverageLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_slot_9422", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, slotLiveFixture(slotLiveMajorityTargets, slotLiveMinorityTargets)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})

	total := int64(slotLiveMajorityTargets + slotLiveMinorityTargets)
	windows := workflow.OwnedPackageTargetSlotCount(total, slotLiveTargetLimit)
	minority := make(map[string]bool, slotLiveMinorityTargets)
	for i := 0; i < slotLiveMinorityTargets; i++ {
		minority[fmt.Sprintf("gem-%03d", i)] = false
	}
	for w := int64(0); w < windows; w++ {
		page := readSlotLiveWindow(ctx, t, store, slotLiveFirstBucket+w)
		for _, target := range page.Targets {
			if target.Ecosystem == "rubygems" {
				minority[target.PackageName] = true
			}
		}
		// One new majority target per window: T+1, as git generations
		// activate between reconcile passes in production.
		if _, err := db.ExecContext(ctx, slotLiveInsertMajority, fmt.Sprintf("add-%03d", w)); err != nil {
			t.Fatalf("insert majority target for window %d: %v", w, err)
		}
	}
	missing := 0
	for _, planned := range minority {
		if !planned {
			missing++
		}
	}
	if missing > 0 {
		t.Fatalf("%d of %d minority targets never planned within %d windows (T grew %d to %d)",
			missing, slotLiveMinorityTargets, windows, total, total+windows)
	}
}

// TestListOwnedPackageDependencyTargetsSlotWindowStatsLive proves the reader
// reports T, P, slot and slot_size beside the rows, that the rows are exactly
// one slot, and that single_pass (bucket 0, P from T) is a stable first page.
func TestListOwnedPackageDependencyTargetsSlotWindowStatsLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_slot_stats_9422", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, slotLiveFixture(slotLiveMajorityTargets, slotLiveMinorityTargets)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	// A dependency fact with no version: sha256 of a NULL key is NULL, so
	// without the COALESCE in the slot key this target would sit in no slot
	// and the slot sizes below would sum to T-1.
	if _, err := db.ExecContext(ctx, slotLiveInsertNullVersion); err != nil {
		t.Fatalf("seed NULL-version target: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})
	total := int64(slotLiveMajorityTargets + slotLiveMinorityTargets + 1)
	wantP := workflow.OwnedPackageTargetSlotCount(total, slotLiveTargetLimit)

	seen := map[string]int64{}
	var slotSum int64
	for w := int64(0); w < wantP; w++ {
		page := readSlotLiveWindow(ctx, t, store, slotLiveFirstBucket+w)
		window := page.Window
		if window.TotalTargets != total || window.SlotCount != wantP {
			t.Fatalf("window %d stats T=%d P=%d, want T=%d P=%d", w, window.TotalTargets, window.SlotCount, total, wantP)
		}
		if want := (slotLiveFirstBucket + w) % wantP; window.Slot != want {
			t.Fatalf("window %d slot = %d, want bucket mod P = %d", w, window.Slot, want)
		}
		if window.SlotSize != int64(len(page.Targets)) || window.RowsRead != window.SlotSize {
			t.Fatalf("window %d slot_size=%d rows_read=%d targets=%d, want one whole slot",
				w, window.SlotSize, window.RowsRead, len(page.Targets))
		}
		if window.Truncated() {
			t.Fatalf("window %d reports truncation under the fill rule", w)
		}
		slotSum += window.SlotSize
		for _, target := range page.Targets {
			key := target.Ecosystem + "|" + target.PackageName + "|" + target.Version
			if previous, ok := seen[key]; ok {
				t.Fatalf("target %s in slot %d and slot %d; slots must be disjoint", key, previous, window.Slot)
			}
			seen[key] = window.Slot
		}
	}
	if slotSum != total {
		t.Fatalf("slot sizes sum to %d over %d slots, want T=%d", slotSum, wantP, total)
	}

	// A read limit below the slot size leaves slot targets unread: the window
	// must say so, so the derivation reports budget exhaustion.
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	cut, err := store.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
		Ecosystems: []string{"npm", "rubygems"}, Limit: 10, TargetLimit: slotLiveTargetLimit,
		VersionSpecific: true, ExpandPackageManagerAliases: true, RotationBucket: slotLiveFirstBucket,
	})
	if err != nil {
		t.Fatalf("truncated read error = %v, want nil", err)
	}
	if !cut.Window.Truncated() || cut.Window.RowsRead != 10 || cut.Window.UnreadTargets() != cut.Window.SlotSize-10 {
		t.Fatalf("truncated read window = %#v, want rows_read 10 and the rest of the slot unread", cut.Window)
	}
	assertOwnedPackageWindowEvent(t, recorder, cut.Window)

	first := readSlotLiveWindowFilter(ctx, t, store, workflow.OwnedPackageDependencyTargetFilter{RotationBucket: 0})
	again := readSlotLiveWindowFilter(ctx, t, store, workflow.OwnedPackageDependencyTargetFilter{RotationBucket: 0})
	if len(first.Targets) == 0 || len(first.Targets) != len(again.Targets) {
		t.Fatalf("bucket 0 page sizes %d and %d, want equal and non-empty", len(first.Targets), len(again.Targets))
	}
	for i := range first.Targets {
		if first.Targets[i] != again.Targets[i] {
			t.Fatalf("bucket 0 page differs at row %d: %#v vs %#v", i, first.Targets[i], again.Targets[i])
		}
	}

	t.Run("slot hash follows the D1 byte layout", func(t *testing.T) {
		assertOwnedPackageSlotHashOracle(ctx, t, db, total)
	})
	t.Run("empty slot still reports the window stats", func(t *testing.T) {
		assertOwnedPackageEmptySlotWindow(ctx, t, store, db, total)
	})
	t.Run("no targets reports T 0 and one slot", func(t *testing.T) {
		for _, targetLimit := range []int{1, 0} {
			page, err := store.ListOwnedPackageDependencyTargets(ctx, workflow.OwnedPackageDependencyTargetFilter{
				Ecosystems: []string{"cargo"}, Limit: 2, TargetLimit: targetLimit, RotationBucket: slotLiveFirstBucket,
			})
			if err != nil {
				t.Fatalf("target limit %d: read error = %v, want nil", targetLimit, err)
			}
			want := workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 0, SlotCount: 1, Slot: 0}
			if page.Window != want || len(page.Targets) != 0 {
				t.Fatalf("target limit %d: window = %#v with %d targets, want %#v and none",
					targetLimit, page.Window, len(page.Targets), want)
			}
		}
	})
}

// TestListOwnedPackageDependencyTargetsSlotDifferentialLive compares the
// reader with the whole partition: the all-slots query derived from the
// shipped statement assigns every target one slot, and the reader's window for
// each slot returns exactly that slot's targets in the global order.
func TestListOwnedPackageDependencyTargetsSlotDifferentialLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "owned_slot_diff_9422", "ESHU_POSTGRES_DSN is not set")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, slotLiveFixture(slotLiveMajorityTargets, slotLiveMinorityTargets)); err != nil {
		t.Fatalf("seed owned-package slot fixture: %v", err)
	}
	store := NewFactStore(SQLDB{DB: db})

	rows, err := db.QueryContext(ctx, ownedPackageAllSlotsQuery(true, true),
		ownedPackageManagerFilterValues([]string{"npm", "rubygems"}, true), int64(100000), int64(0), slotLiveTargetLimit)
	if err != nil {
		t.Fatalf("all-slots differential query: %v", err)
	}
	partition := map[int64][]string{}
	var slotCount int64
	for rows.Next() {
		var ecosystem, name, version string
		var slot, total, size int64
		if err := rows.Scan(&ecosystem, &name, &version, &slot, &slotCount, &total, &size); err != nil {
			t.Fatalf("scan all-slots row: %v", err)
		}
		partition[slot] = append(partition[slot], ecosystem+"|"+name+"|"+version)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("all-slots rows: %v", err)
	}
	_ = rows.Close()
	if want := workflow.OwnedPackageTargetSlotCount(slotLiveMajorityTargets+slotLiveMinorityTargets, slotLiveTargetLimit); slotCount != want {
		t.Fatalf("all-slots slot count = %d, want %d", slotCount, want)
	}
	for slot := int64(0); slot < slotCount; slot++ {
		page := readSlotLiveWindow(ctx, t, store, slot)
		got := make([]string, 0, len(page.Targets))
		for _, target := range page.Targets {
			got = append(got, target.Ecosystem+"|"+target.PackageName+"|"+target.Version)
		}
		if !slices.Equal(got, partition[slot]) {
			t.Fatalf("slot %d reader rows (%d) differ from the partition (%d)", slot, len(got), len(partition[slot]))
		}
	}
	t.Run("read follows active generations with output identical to the all-history read", func(t *testing.T) {
		assertOwnedPackageReadFollowsActiveGenerations(ctx, t, db, slotLiveMajorityTargets+slotLiveMinorityTargets)
	})
}

// assertOwnedPackageWindowEvent proves the read span carries the window
// event with the stats an operator reads at 3 AM.
func assertOwnedPackageWindowEvent(t *testing.T, recorder *tracetest.SpanRecorder, window workflow.OwnedPackageDependencyTargetWindow) {
	t.Helper()
	for _, span := range recorder.Ended() {
		if span.Name() != telemetry.SpanOwnedPackageTargetsRead {
			continue
		}
		for _, event := range span.Events() {
			if event.Name != telemetry.EventOwnedPackageTargetsWindow {
				continue
			}
			got := map[string]int64{}
			truncated := false
			for _, kv := range event.Attributes {
				switch kv.Value.Type() {
				case attribute.INT64:
					got[string(kv.Key)] = kv.Value.AsInt64()
				case attribute.BOOL:
					truncated = kv.Value.AsBool()
				}
			}
			want := map[string]int64{
				"owned_package_targets.total_targets": window.TotalTargets,
				"owned_package_targets.slot_count":    window.SlotCount,
				"owned_package_targets.slot":          window.Slot,
				"owned_package_targets.slot_size":     window.SlotSize,
				"owned_package_targets.rows_read":     window.RowsRead,
			}
			for key, value := range want {
				if got[key] != value {
					t.Fatalf("window event %s = %d, want %d (event %v)", key, got[key], value, event.Attributes)
				}
			}
			if truncated != window.Truncated() {
				t.Fatalf("window event truncated = %v, want %v", truncated, window.Truncated())
			}
			return
		}
	}
	t.Fatalf("no %s span with a %s event among %d ended spans",
		telemetry.SpanOwnedPackageTargetsRead, telemetry.EventOwnedPackageTargetsWindow, len(recorder.Ended()))
}

func readSlotLiveWindow(ctx context.Context, t *testing.T, store *FactStore, bucket int64) workflow.OwnedPackageDependencyTargetPage {
	t.Helper()
	return readSlotLiveWindowFilter(ctx, t, store, workflow.OwnedPackageDependencyTargetFilter{RotationBucket: bucket})
}

func readSlotLiveWindowFilter(
	ctx context.Context,
	t *testing.T,
	store *FactStore,
	filter workflow.OwnedPackageDependencyTargetFilter,
) workflow.OwnedPackageDependencyTargetPage {
	t.Helper()
	filter.Ecosystems = []string{"npm", "rubygems"}
	filter.TargetLimit = slotLiveTargetLimit
	filter.Limit = slotLiveTargetLimit + 1
	filter.VersionSpecific = true
	filter.ExpandPackageManagerAliases = true
	page, err := store.ListOwnedPackageDependencyTargets(ctx, filter)
	if err != nil {
		t.Fatalf("ListOwnedPackageDependencyTargets(bucket %d) error = %v, want nil", filter.RotationBucket, err)
	}
	return page
}

// slotLiveFixture seeds one active generation holding a majority npm block
// (pkg-0000..) and a minority rubygems block (gem-000..). rubygems sorts after
// npm, so it is the last block of the global target order, as in QA.
func slotLiveFixture(majority, minority int) string {
	return fmt.Sprintf(`
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ('scope-slot', 'repository', 'git', 'repo-slot', 'git', 'repository:slot', now(), now(), 'active', 'gen-slot');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-slot', 'scope-slot', 'snapshot', now(), now(), 'active');
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT d.fact_id, 'scope-slot', 'gen-slot', 'content_entity', 'stable-' || d.fact_id, 'git',
    'git', d.fact_id, '2026-10-28T12:00:00Z', now(), FALSE,
    jsonb_build_object(
        'entity_type', 'Variable',
        'entity_name', d.package_name,
        'repo_id', 'scope-slot',
        'entity_metadata', jsonb_build_object(
            'config_kind', 'dependency',
            'package_manager', d.package_manager,
            'value', '1.0.0',
            'lockfile', 'true'))
FROM (
    SELECT 'npm-' || n AS fact_id, 'npm' AS package_manager, 'pkg-' || lpad(n::text, 4, '0') AS package_name
    FROM generate_series(0, %d) AS n
    UNION ALL
    SELECT 'gem-' || n, 'rubygems', 'gem-' || lpad(n::text, 3, '0')
    FROM generate_series(0, %d) AS n
) AS d;
`, majority-1, minority-1)
}

// slotLiveInsertMajority adds one npm target to the active generation. Its
// name sorts before the pkg- block, so it shifts every later rank by one.
const slotLiveInsertMajority = `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('npm-' || $1, 'scope-slot', 'gen-slot', 'content_entity', 'stable-npm-' || $1, 'git',
    'git', 'npm-' || $1, '2026-10-28T12:00:00Z', now(), FALSE,
    jsonb_build_object(
        'entity_type', 'Variable',
        'entity_name', $1::text,
        'repo_id', 'scope-slot',
        'entity_metadata', jsonb_build_object(
            'config_kind', 'dependency',
            'package_manager', 'npm',
            'value', '1.0.0',
            'lockfile', 'true')))
`

// slotLiveInsertNullVersion adds one npm dependency fact whose metadata has
// no value key, so its version reads as NULL.
const slotLiveInsertNullVersion = `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, collector_kind,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('npm-null-version', 'scope-slot', 'gen-slot', 'content_entity', 'stable-npm-null-version', 'git',
    'git', 'npm-null-version', '2026-10-28T12:00:00Z', now(), FALSE,
    jsonb_build_object(
        'entity_type', 'Variable',
        'entity_name', 'pkg-null-version',
        'repo_id', 'scope-slot',
        'entity_metadata', jsonb_build_object(
            'config_kind', 'dependency',
            'package_manager', 'npm',
            'lockfile', 'false')))
`

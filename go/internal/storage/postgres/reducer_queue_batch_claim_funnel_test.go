// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// funnelBatchClaimDB answers the batch claim with fixed claim rows and the
// funnel counts query with one fixed counts row, so the funnel recording is
// observable without a live database. The counts row must describe the same
// pre-claim membership the fake claim rows admit from: a canned row behind
// zero claim rows is an impossible production pairing (F3).
type funnelBatchClaimDB struct {
	claimRows [][]any
	counts    []int64
}

func (f funnelBatchClaimDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if strings.Contains(query, "batch_claim_funnel_counts") {
		row := make([]any, len(f.counts))
		for i, c := range f.counts {
			row[i] = c
		}
		return &fakeRows{rows: [][]any{row}}, nil
	}
	return &fakeRows{rows: f.claimRows}, nil
}

// funnelClaimRow renders one batch-claim intent row for the funnel hermetic
// fake: the eleven columns scanReducerIntent reads.
func funnelClaimRow(id string, now time.Time) []any {
	return []any{
		id, "scope-funnel", "gen-funnel", "code_call_materialization",
		int64(1), int64(0), now, now, now, now,
		[]byte(`{"entity_key":"` + id + `","reason":"funnel-proof","fact_id":"f1","source_system":"git"}`),
	}
}

func (f funnelBatchClaimDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

// TestClaimBatchRecordsFunnelTelemetry is the #10565 box-6 gate: every batch
// claim records candidates in, representatives after the fence, and drops by
// closed reason. The crafted funnel row reconciles exactly: every candidate
// is admitted or dropped under exactly one reason.
func TestClaimBatchRecordsFunnelTelemetry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("batch-claim-funnel-proof"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		database: funnelBatchClaimDB{
			// F3: the fake claim admits two rows from the membership the
			// counts row describes (admitted > 0 consistent with the fake
			// claim rows — the pre-claim pairing production always has).
			claimRows: [][]any{
				funnelClaimRow("funnel-item-1", now),
				funnelClaimRow("funnel-item-2", now),
			},
			counts: []int64{
				16, // candidates in (base rows)
				2,  // not_ready
				1,  // not_representative
				1,  // projector_drain
				1,  // semantic_cap
				11, // pre_fence (representatives before the live-lease fence)
				10, // representatives after fence
			},
		},
		LeaseOwner:    "batch-claim-funnel-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		Instruments:   instruments,
	}
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	if len(intents) != 2 {
		t.Fatalf("ClaimBatch on the funnel fake queue = %d intents, want 2", len(intents))
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	histogramSum := func(suffix string) (int64, bool) {
		for _, scope := range collected.ScopeMetrics {
			for _, metric := range scope.Metrics {
				histogram, ok := metric.Data.(metricdata.Histogram[int64])
				if !ok || len(metric.Name) < len(suffix) ||
					metric.Name[len(metric.Name)-len(suffix):] != suffix {
					continue
				}
				if len(histogram.DataPoints) != 1 {
					t.Fatalf("%s data points = %d, want 1", suffix, len(histogram.DataPoints))
				}
				return int64(histogram.DataPoints[0].Sum), true
			}
		}
		return 0, false
	}
	if sum, ok := histogramSum("reducer_batch_claim_candidates"); !ok || sum != 16 {
		t.Fatalf("candidates histogram = (%d, %v), want (16, true)", sum, ok)
	}
	if sum, ok := histogramSum("reducer_batch_claim_representatives"); !ok || sum != 10 {
		t.Fatalf("representatives histogram = (%d, %v), want (10, true)", sum, ok)
	}

	drops := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			const suffix = "reducer_batch_claim_drops_total"
			if len(metric.Name) < len(suffix) ||
				metric.Name[len(metric.Name)-len(suffix):] != suffix {
				continue
			}
			for _, point := range sum.DataPoints {
				value, ok := point.Attributes.Value("reason")
				if !ok {
					t.Fatalf("drops point without a reason attribute: %+v", point.Attributes)
				}
				drops[value.AsString()] = int64(point.Value)
			}
		}
	}
	// limit 8, admitted 2: over_limit = 10-8, lock_lost = min(10,8)-2.
	admitted := int64(len(intents))
	wantDrops := map[string]int64{
		"not_ready":          2,
		"not_representative": 1,
		"projector_drain":    1,
		"semantic_cap":       1,
		"live_lease_fence":   1,
		"over_limit":         2,
		"lock_lost":          6,
	}
	if len(drops) != len(wantDrops) {
		t.Fatalf("drop reasons = %v, want %d closed reasons", drops, len(wantDrops))
	}
	accounted := int64(0)
	for reason, want := range wantDrops {
		got, ok := drops[reason]
		if !ok {
			t.Fatalf("drop reason %q missing from %v", reason, drops)
		}
		if got != want {
			t.Fatalf("drop reason %q = %d, want %d", reason, got, want)
		}
		accounted += got
	}
	if admitted+accounted != 16 {
		t.Fatalf("admitted %d + drops %d = %d, want candidates 16 (pre-claim membership must reconcile exactly)", admitted, accounted, admitted+accounted)
	}
}

// TestClaimBatchFunnelQueryReusesShippedChain pins the box-6 single-source
// contract: the funnel counts query composes the candidate chain and the
// precomputed gate CTEs from the same Go sources as the shipped claim, so a
// future gate edit lands in both. A hand-copied funnel that drifts from
// admission fails here instead of silently misattributing drops.
func TestClaimBatchFunnelQueryReusesShippedChain(t *testing.T) {
	t.Parallel()

	chain := reducerClaimBatchCandidateChainCTEs()
	if !strings.Contains(claimReducerWorkBatchQuery, chain) {
		t.Fatal("shipped batch claim does not contain reducerClaimBatchCandidateChainCTEs() verbatim: " +
			"the funnel chain must generate from the same Go source as the claim")
	}
	for _, half := range []string{
		reducerClaimBatchUpstreamCTEs(),
		reducerClaimBatchRepresentativeCTEs(),
	} {
		if !strings.Contains(claimReducerWorkBatchQuery, half) {
			t.Fatal("shipped batch claim does not contain a chain half verbatim: " +
				"the funlen split must rejoin without changing the claim text")
		}
		if !strings.Contains(reducerClaimBatchFunnelQuery, half) {
			t.Fatal("funnel counts query does not contain a chain half verbatim")
		}
	}
	if !strings.Contains(chain, reducerClaimBatchGatePrecomputeCTEs()) {
		t.Fatal("funnel chain does not contain reducerClaimBatchGatePrecomputeCTEs() verbatim")
	}
	if !strings.Contains(reducerClaimBatchFunnelQuery, chain) {
		t.Fatal("funnel counts query does not contain the candidate chain verbatim")
	}
	for _, cte := range []string{
		"base AS MATERIALIZED (",
		"reps_ranked AS MATERIALIZED (",
		"reps AS MATERIALIZED (",
		"source_heads AS MATERIALIZED (",
		"candidate AS (",
	} {
		if !strings.Contains(chain, cte) {
			t.Fatalf("funnel chain missing %q: the funnel must count the stages the claim admits", cte)
		}
	}
}

// TestClaimBatchFunnelQueryIsReadOnly is the F1 hermetic guard for #10565:
// the funnel census must replay the sweep membership without executing it.
// A data-modifying CTE runs on every claim even when nothing reads it, so
// any data-modifying statement in this text reintroduces the second sweep
// lock pass F1 removes. The final assertion keeps the guard non-vacuous:
// the shipped claim still carries its sweep UPDATE, so a guard that cannot
// see one proves nothing.
func TestClaimBatchFunnelQueryIsReadOnly(t *testing.T) {
	t.Parallel()

	// Statement shapes, not bare words: the shared sweep sources carry
	// comments that name the claim's UPDATE (e.g. "the UPDATE probes it"),
	// and comments never execute. A reintroduced sweep would reappear as
	// a statement with a target and lock clause, which no comment matches.
	for _, banned := range []string{
		"UPDATE fact_work_items",
		"DELETE FROM",
		"INSERT INTO",
		"FOR NO KEY UPDATE",
		"FOR UPDATE OF",
		"SKIP LOCKED",
	} {
		if strings.Contains(reducerClaimBatchFunnelQuery, banned) {
			t.Fatalf("funnel counts query contains data-modifying %q: it must stay a pure SELECT census", banned)
		}
	}
	for _, want := range []string{
		"superseded_stale_reducer_locked AS (",
		"superseded_crossplane_newest_carrier AS MATERIALIZED (",
		// MATERIALIZED is load-bearing (see the marker's comment): without
		// it the planner inlines the sweep under base's NOT EXISTS.
		"superseded_stale_reducer_generations AS MATERIALIZED (",
		supersedeInactiveReducerLockedColumnsSQL,
		supersedeInactiveReducerCarrierArmSQL,
		supersededCrossplaneNewestCarrierCTE,
		supersedeInactiveReducerUpdateRowFiltersSQL,
	} {
		if !strings.Contains(reducerClaimBatchFunnelQuery, want) {
			t.Fatalf("funnel counts query missing read-only sweep piece %q: the exclusion set must derive from the shared sweep sources", want)
		}
	}
	if strings.Contains(reducerClaimBatchFunnelQuery, "UPDATE fact_work_items AS stale") {
		t.Fatal("funnel counts query contains the sweep UPDATE: it must use the read-only generations SELECT")
	}
	if !strings.Contains(claimReducerWorkBatchQuery, "UPDATE fact_work_items AS stale") {
		t.Fatal("shipped claim lost its sweep UPDATE: the read-only guard above is vacuous without a mutating reference")
	}
}

// TestClaimBatchFunnelSkippedWithoutInstruments pins the nil contract: a
// queue without instruments issues no funnel query, so ClaimBatch keeps
// working on wiring that never observes it.
func TestClaimBatchFunnelSkippedWithoutInstruments(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		database:      funnelBatchClaimDB{counts: []int64{5, 1, 1, 1, 1, 1, 0}},
		LeaseOwner:    "batch-claim-funnel-skipped",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if _, err := queue.ClaimBatch(context.Background(), 8); err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// reservedSlotClaimRow renders one batch-claim intent row for the
// reserved-slot hermetic fake: the eleven columns scanReducerIntent reads,
// with a caller-chosen domain.
func reservedSlotClaimRow(id, domain string, now time.Time) []any {
	return []any{
		id, "scope-reserved", "gen-reserved", domain,
		int64(1), int64(0), now, now, now, now,
		[]byte(`{"entity_key":"` + id + `","reason":"reserved-proof","fact_id":"f1","source_system":"git"}`),
	}
}

// reservedSlotOutcomes collects eshu_dp_reducer_batch_claim_reserved_slot_total
// points by outcome attribute.
func reservedSlotOutcomes(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	const name = "eshu_dp_reducer_batch_claim_reserved_slot_total"
	got := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != name {
				continue
			}
			for _, point := range sum.DataPoints {
				value, ok := point.Attributes.Value("outcome")
				if !ok {
					t.Fatalf("reserved-slot point without an outcome attribute: %+v", point.Attributes)
				}
				outcome := value.AsString()
				if !containsReservedSlotOutcome(outcome) {
					t.Fatalf("reserved-slot outcome %q outside the closed set %v",
						outcome, telemetry.ReservedSlotClaimOutcomes())
				}
				got[outcome] += point.Value
			}
		}
	}
	return got
}

func containsReservedSlotOutcome(outcome string) bool {
	for _, want := range telemetry.ReservedSlotClaimOutcomes() {
		if outcome == want {
			return true
		}
	}
	return false
}

func reservedSlotTestInstruments(t *testing.T, reader *sdkmetric.ManualReader) *telemetry.Instruments {
	t.Helper()
	instruments, err := telemetry.NewInstruments(
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("reserved-slot-claim-proof"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments
}

// TestClaimBatchEmitsReservedUsedWhenSearchDocClaimed is the #10581
// engagement proof (hermetic): a batch that admits an eshu_search_document
// row records one reserved_used outcome.
func TestClaimBatchEmitsReservedUsedWhenSearchDocClaimed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		database: funnelBatchClaimDB{
			claimRows: [][]any{
				reservedSlotClaimRow("reserved-search-1", string(reducercontract.DomainEshuSearchDocument), now),
				reservedSlotClaimRow("reserved-advisory-1", string(reducercontract.DomainAdvisoryGroupProjection), now),
				reservedSlotClaimRow("reserved-advisory-2", string(reducercontract.DomainAdvisoryGroupProjection), now),
			},
		},
		LeaseOwner:    "reserved-slot-used-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		Instruments:   reservedSlotTestInstruments(t, reader),
	}
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	if len(intents) != 3 {
		t.Fatalf("ClaimBatch on the reserved-slot fake queue = %d intents, want 3", len(intents))
	}
	got := reservedSlotOutcomes(t, ctx, reader)
	if got[telemetry.ReservedSlotOutcomeUsed] != 1 {
		t.Fatalf("reserved-slot outcomes = %v, want exactly one %q",
			got, telemetry.ReservedSlotOutcomeUsed)
	}
	if _, ok := got[telemetry.ReservedSlotOutcomeFallback]; ok {
		t.Fatalf("reserved-slot outcomes = %v, want no %q when a search-doc row is claimed",
			got, telemetry.ReservedSlotOutcomeFallback)
	}
}

// TestClaimBatchEmitsFallbackWhenNoSearchDocClaimed is the #10581 fallback
// proof (hermetic): a non-empty batch with no eshu_search_document row
// records one fallback outcome.
func TestClaimBatchEmitsFallbackWhenNoSearchDocClaimed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		database: funnelBatchClaimDB{
			claimRows: [][]any{
				reservedSlotClaimRow("reserved-advisory-1", string(reducercontract.DomainAdvisoryGroupProjection), now),
				reservedSlotClaimRow("reserved-advisory-2", string(reducercontract.DomainAdvisoryGroupProjection), now),
			},
		},
		LeaseOwner:    "reserved-slot-fallback-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		Instruments:   reservedSlotTestInstruments(t, reader),
	}
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	if len(intents) != 2 {
		t.Fatalf("ClaimBatch on the reserved-slot fake queue = %d intents, want 2", len(intents))
	}
	got := reservedSlotOutcomes(t, ctx, reader)
	if got[telemetry.ReservedSlotOutcomeFallback] != 1 {
		t.Fatalf("reserved-slot outcomes = %v, want exactly one %q",
			got, telemetry.ReservedSlotOutcomeFallback)
	}
	if _, ok := got[telemetry.ReservedSlotOutcomeUsed]; ok {
		t.Fatalf("reserved-slot outcomes = %v, want no %q when no search-doc row is claimed",
			got, telemetry.ReservedSlotOutcomeUsed)
	}
}

// TestClaimBatchSkipsReservedSlotOnEmptyBatch pins the empty-batch edge: an
// empty claim records no reserved-slot outcome, so idle polls never inflate
// the fallback series.
func TestClaimBatchSkipsReservedSlotOnEmptyBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		database:      funnelBatchClaimDB{},
		LeaseOwner:    "reserved-slot-empty-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		Instruments:   reservedSlotTestInstruments(t, reader),
	}
	intents, err := queue.ClaimBatch(ctx, 8)
	if err != nil {
		t.Fatalf("ClaimBatch: %v", err)
	}
	if len(intents) != 0 {
		t.Fatalf("ClaimBatch on the empty fake queue = %d intents, want 0", len(intents))
	}
	if got := reservedSlotOutcomes(t, ctx, reader); len(got) != 0 {
		t.Fatalf("reserved-slot outcomes = %v, want none for an empty batch", got)
	}
}

// TestClaimBatchReservedSlotMovesLive is the #10581 on/off proof on the
// production claim path: behind a 40-row advisory backlog, every batch that
// still has a ready search-doc row records reserved_used (the slot
// engaging), and once the 4 search-doc rows drain, the next batch records
// fallback. The signal moves with the mechanism on one fixture — no
// simulation stands between the verdict and claimReducerWorkBatchQuery.
func TestClaimBatchReservedSlotMovesLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the reserved-slot live proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		busyDomain   = reducercontract.DomainAdvisoryGroupProjection
		searchDomain = reducercontract.DomainEshuSearchDocument
		busyCount    = 40
		searchCount  = 4
		batchSize    = 4
	)

	now := time.Date(2026, time.November, 12, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-search-reserved-live", now)

	for i := 0; i < busyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("live-advisory-%03d", i),
			scopeID:        "scope-search-reserved-live",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainAdvisoryGroup,
			conflictKey:    fmt.Sprintf("live-advisory-key-%03d", i),
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	searchBase := now.Add(-72 * time.Hour)
	for i := 0; i < searchCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("live-searchdoc-%03d", i),
			scopeID:        "scope-search-reserved-live",
			generationID:   "gen-fair",
			domain:         string(searchDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("live-searchdoc-key-%03d", i),
			updatedAt:      searchBase.Add(time.Duration(i) * time.Second),
		})
	}

	reader := sdkmetric.NewManualReader()
	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "reserved-slot-live-proof",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(2 * time.Hour) },
		ClaimDomains:  []reducercontract.Domain{busyDomain, searchDomain},
		Instruments:   reservedSlotTestInstruments(t, reader),
	}

	var searchClaimed int
	for round := 0; round < 6; round++ {
		intents, err := queue.ClaimBatch(ctx, batchSize)
		if err != nil {
			t.Fatalf("round %d: ClaimBatch() error = %v", round, err)
		}
		if len(intents) == 0 {
			t.Fatalf("round %d: claimed an empty batch with %d advisory rows still pending",
				round, busyCount-searchClaimed*3)
		}
		for _, intent := range intents {
			if intent.Domain == searchDomain {
				searchClaimed++
			}
		}
	}
	if searchClaimed != searchCount {
		t.Fatalf("claimed %d search-doc rows in 6 rounds of %d, want all %d drained",
			searchClaimed, batchSize, searchCount)
	}
	got := reservedSlotOutcomes(t, ctx, reader)
	if got[telemetry.ReservedSlotOutcomeUsed] != searchCount {
		t.Fatalf("reserved-slot outcomes = %v, want %d %q (one per batch while a search-doc row was ready)",
			got, searchCount, telemetry.ReservedSlotOutcomeUsed)
	}
	if got[telemetry.ReservedSlotOutcomeFallback] < 1 {
		t.Fatalf("reserved-slot outcomes = %v, want at least one %q after the search-doc rows drained",
			got, telemetry.ReservedSlotOutcomeFallback)
	}
}

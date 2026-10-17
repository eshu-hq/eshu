// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ledgerScopeFilter is the shared filter of the ledger-read unit tests: one
// repository scope with an explicit prior generation.
func ledgerScopeFilter() changedsince.Filter {
	return changedsince.Filter{
		ScopeID:           "scope-ledger",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	}.Normalize()
}

// ledgerScopeResponses queues the scope and prior resolution rows both the
// legacy and the ledger path read first.
func ledgerScopeResponses(priorObserved time.Time) []fakeRows {
	return []fakeRows{
		{rows: scopeRow("scope-ledger", "repository", "gen-current", time.Now().UTC(), false)},
		{rows: priorRow("gen-prior", priorObserved)},
	}
}

// ledgerCursorRow builds the cursor row the ledger read scans: the linked
// state generation, its activation sequence, and the digest version.
func ledgerCursorRow(state string, seq int64, digest int64) []any {
	if state == "" {
		return []any{nil, seq, digest}
	}
	return []any{state, seq, digest}
}

// ledgerChainRow builds one chain-walk row: the link's generation, its
// prior, kind, digest version, stored delta rows, and per-category live
// key counts.
func ledgerChainRow(generation, prior, kind string, digest, deltas, files, entities, facts int64) []any {
	return []any{generation, prior, kind, digest, deltas, files, entities, facts}
}

// ledgerDeltaRow builds one link-delta row the ledger read scans.
func ledgerDeltaRow(category, classification, key string, priorKind, currentKind any, priorState, currentState []byte, tombstoned bool) []any {
	return []any{category, classification, key, priorKind, currentKind, priorState, currentState, tombstoned}
}

// hasLedgerQuery reports whether any issued query touches the link ledger
// tables.
func hasLedgerQuery(queries []string) bool {
	for _, query := range queries {
		if strings.Contains(query, "changed_since_link") || strings.Contains(query, "changed_since_scope_cursor") {
			return true
		}
	}
	return false
}

// hasLegacyDiffQuery reports whether the legacy two-generation diff ran.
func hasLegacyDiffQuery(queries []string) bool {
	for _, query := range queries {
		if strings.Contains(query, "current_tombstones") {
			return true
		}
	}
	return false
}

func TestComputeChangedSinceDeltaLedgerReadOffIssuesNoLedgerSQL(t *testing.T) {
	t.Parallel()
	queryer := &fakeQueryer{responses: append(ledgerScopeResponses(time.Now().UTC()),
		fakeRows{rows: [][]any{
			diffRow("files", "unchanged", 2, "file:a.go", "file"),
			diffRow("files", "unchanged", 2, "file:b.go", "file"),
		}},
	)}
	store := NewStatusStore(queryer)
	summary, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter())
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if hasLedgerQuery(queryer.queries) {
		t.Fatalf("flag-off read issued ledger SQL: %d queries", len(queryer.queries))
	}
	if !hasLegacyDiffQuery(queryer.queries) {
		t.Fatal("flag-off read did not run the legacy diff")
	}
	if summary.ScopeID != "scope-ledger" || summary.SinceGenerationID != "gen-prior" {
		t.Fatalf("summary = %+v, want scope-ledger/gen-prior", summary)
	}
}

func TestComputeChangedSinceDeltaLedgerReadServesSingleLink(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	queryer := &fakeQueryer{responses: append(ledgerScopeResponses(now),
		fakeRows{rows: [][]any{ledgerCursorRow("gen-current", 2, 1)}},
		fakeRows{rows: [][]any{ledgerChainRow("gen-current", "gen-prior", "incremental", 1, 2, 2, 0, 0)}},
		fakeRows{rows: [][]any{
			ledgerDeltaRow("files", "added", "file:new.go", nil, "file", nil, []byte("state-new"), false),
			ledgerDeltaRow("files", "updated", "file:a.go", "file", "file", []byte("state-old"), []byte("state-new"), false),
		}},
		// No tombstone probe: nothing was removed.
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{ledgerCursorRow("gen-current", 2, 1)}},
	)}
	store := NewStatusStore(queryer)
	store.LedgerReadEnabled = true
	summary, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter())
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if hasLegacyDiffQuery(queryer.queries) {
		t.Fatal("ledger-served read also ran the legacy diff")
	}
	if !hasLedgerQuery(queryer.queries) {
		t.Fatal("flag-on read issued no ledger SQL")
	}
	counts := map[changedsince.Category]changedsince.Counts{}
	for _, category := range summary.Categories {
		counts[category.Category] = category.Counts
	}
	if got := counts[changedsince.CategoryFiles]; got.Added != 1 || got.Updated != 1 || got.Unchanged != 0 {
		t.Fatalf("files counts = %+v, want added=1 updated=1 unchanged=0", got)
	}
	if len(summary.Categories) != len(changedsince.Categories) {
		t.Fatalf("categories = %d, want %d", len(summary.Categories), len(changedsince.Categories))
	}
}

func TestComputeChangedSinceDeltaLedgerReadDigestMismatchFallsBack(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	queryer := &fakeQueryer{responses: append(ledgerScopeResponses(now),
		fakeRows{rows: [][]any{ledgerCursorRow("gen-current", 2, 999)}},
		fakeRows{rows: [][]any{
			diffRow("files", "unchanged", 1, "file:a.go", "file"),
		}},
	)}
	store := NewStatusStore(queryer)
	store.LedgerReadEnabled = true
	if _, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter()); err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if !hasLegacyDiffQuery(queryer.queries) {
		t.Fatal("digest-mismatched cursor did not fall back to the legacy diff")
	}
}

func TestComputeChangedSinceDeltaLedgerReadErrorFallsBack(t *testing.T) {
	t.Parallel()
	ledgerErr := errors.New("cursor read failed")
	queryer := &fakeQueryer{responses: append(ledgerScopeResponses(time.Now().UTC()),
		fakeRows{err: ledgerErr},
		fakeRows{rows: [][]any{
			diffRow("files", "unchanged", 1, "file:a.go", "file"),
		}},
	)}
	store := NewStatusStore(queryer)
	store.LedgerReadEnabled = true
	if _, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter()); err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v, want fallback to legacy", err)
	}
	if !hasLegacyDiffQuery(queryer.queries) {
		t.Fatal("ledger error did not fall back to the legacy diff")
	}
}

func TestComputeChangedSinceDeltaLedgerReadCountsOutcome(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	ledgerReads := func(t *testing.T, reader sdkmetric.Reader) map[string]int64 {
		t.Helper()
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		counts := map[string]int64{}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name != "eshu_dp_changed_since_ledger_reads_total" {
					continue
				}
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
				}
				for _, dp := range sum.DataPoints {
					outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
					counts[outcome.AsString()] += dp.Value
				}
			}
		}
		return counts
	}
	newInstruments := func(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
		t.Helper()
		reader := sdkmetric.NewManualReader()
		instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
		if err != nil {
			t.Fatalf("NewInstruments: %v", err)
		}
		return instruments, reader
	}

	// A served single-link read counts ledger_single once.
	instruments, reader := newInstruments(t)
	queryer := &fakeQueryer{responses: append(ledgerScopeResponses(now),
		fakeRows{rows: [][]any{ledgerCursorRow("gen-current", 2, 1)}},
		fakeRows{rows: [][]any{ledgerChainRow("gen-current", "gen-prior", "incremental", 1, 1, 1, 0, 0)}},
		fakeRows{rows: [][]any{
			ledgerDeltaRow("files", "added", "file:new.go", nil, "file", nil, []byte("state-new"), false),
		}},
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{ledgerCursorRow("gen-current", 2, 1)}},
	)}
	store := NewStatusStore(queryer)
	store.LedgerReadEnabled = true
	store.Instruments = instruments
	if _, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter()); err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if got := ledgerReads(t, reader); got["ledger_single"] != 1 {
		t.Fatalf("ledger reads = %v, want ledger_single=1", got)
	}

	// An unlinked current generation falls back and counts it.
	instruments, reader = newInstruments(t)
	queryer = &fakeQueryer{responses: append(ledgerScopeResponses(now),
		// No cursor row: the current generation is not linked yet.
		fakeRows{rows: [][]any{}},
		fakeRows{rows: [][]any{
			diffRow("files", "unchanged", 1, "file:a.go", "file"),
		}},
	)}
	store = NewStatusStore(queryer)
	store.LedgerReadEnabled = true
	store.Instruments = instruments
	if _, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter()); err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if !hasLegacyDiffQuery(queryer.queries) {
		t.Fatal("unlinked current generation did not fall back to the legacy diff")
	}
	wantFallback := "fallback_" + string(linksfreshnessstore.ReadFallbackCurrentUnlinked)
	if got := ledgerReads(t, reader); got[wantFallback] != 1 {
		t.Fatalf("ledger reads = %v, want %s=1", got, wantFallback)
	}

	// Flag off: the legacy diff answers and nothing is counted.
	instruments, reader = newInstruments(t)
	queryer = &fakeQueryer{responses: append(ledgerScopeResponses(now),
		fakeRows{rows: [][]any{
			diffRow("files", "unchanged", 1, "file:a.go", "file"),
		}},
	)}
	store = NewStatusStore(queryer)
	store.Instruments = instruments
	if _, err := store.ComputeChangedSinceDelta(context.Background(), ledgerScopeFilter()); err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if got := ledgerReads(t, reader); len(got) != 0 {
		t.Fatalf("ledger reads = %v, want none with the flag off", got)
	}
}

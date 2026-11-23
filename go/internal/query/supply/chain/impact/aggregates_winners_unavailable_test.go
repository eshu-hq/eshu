// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// Failing-first proof for #11169 (arbiter-ruled shape a): the aggregate
// count and inventory reads served from the maintained winners read model
// fail closed with ErrAggregateReadModelUnavailable when the model is
// absent, stale, or unprobed, and carry freshness when they serve. Exact
// totals and buckets can never be labeled partial, so unlike the grouped
// read there is no serve-marked-degraded state: unavailable means 503, not
// an approximate answer.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func aggregateWinnersTestNow() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) }

// winnersUnavailableQueryer serves the watermark probe from watermarkRows and
// the aggregate statements from countRow/inventoryRows. A nil watermarkRows
// answers no watermark row (maintainer never populated); watermarkErr fails
// the probe.
type winnersUnavailableQueryer struct {
	cov7StoreQueryer
	watermarkRows [][]any
	watermarkErr  error
}

func (q *winnersUnavailableQueryer) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	_ = ctx
	_ = args
	q.queries = append(q.queries, query)
	if query == SelectWinnersWatermarkQuery {
		if q.watermarkErr != nil {
			return nil, q.watermarkErr
		}
		return &cov7StoreRows{rows: q.watermarkRows}, nil
	}
	return q.onRows(query)
}

func aggregateWinnersCountRow() db.Row {
	return &cov7StoreRow{values: []any{
		cov7NullInt(3), cov7NullInt(2), cov7NullInt(1),
		cov7NullInt(1), cov7NullInt(0), cov7NullInt(1),
		[]byte(`[{"bucket":"p1","bucket_count":2}]`), []byte(`[{"bucket":"critical","bucket_count":3}]`),
	}}
}

func aggregateWinnersInventoryRows() (db.Rows, error) {
	return &cov7StoreRows{rows: [][]any{{"affected_exact", int64(3)}}}, nil
}

func TestAggregateWinnersCountRequiresFreshWatermark(t *testing.T) {
	t.Parallel()

	freshAt := aggregateWinnersTestNow().Add(-time.Minute)

	for _, tc := range []struct {
		name          string
		watermarkRows [][]any
		watermarkErr  error
		wantErr       bool
	}{
		{"absent watermark", nil, nil, true},
		{"stale watermark", [][]any{{aggregateWinnersTestNow().Add(-10 * time.Minute)}}, nil, true},
		{"probe error", nil, errors.New("conn reset by peer"), true},
		{"fresh watermark", [][]any{{freshAt}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			probe := &winnersUnavailableQueryer{
				watermarkRows: tc.watermarkRows,
				watermarkErr:  tc.watermarkErr,
			}
			probe.onRow = func(string) db.Row { return aggregateWinnersCountRow() }
			probe.onRows = func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() }
			store := NewPostgresAggregateStoreWithReadStore(probe, true)
			store.Now = aggregateWinnersTestNow

			count, err := store.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{Status: "affected_exact"})
			if tc.wantErr {
				if !errors.Is(err, ErrAggregateReadModelUnavailable) {
					t.Fatalf("count err = %v, want ErrAggregateReadModelUnavailable", err)
				}
				for _, query := range probe.queries {
					if strings.Contains(query, "aggregate_winners AS") {
						t.Fatalf("unavailable count still issued the winners statement")
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("fresh count err = %v, want envelope", err)
			}
			if count.TotalFindings != 3 {
				t.Fatalf("fresh count total = %d, want 3", count.TotalFindings)
			}
			if count.Freshness.State != packet.FreshnessLabelFresh {
				t.Fatalf("fresh count freshness state = %q, want fresh", count.Freshness.State)
			}
			if count.Freshness.LatestObservedAt != freshAt.UTC().Format(time.RFC3339Nano) {
				t.Fatalf("fresh count observed_at = %q, want watermark", count.Freshness.LatestObservedAt)
			}
			if len(probe.queries) != 2 || probe.queries[0] != SelectWinnersWatermarkQuery {
				t.Fatalf("fresh count issued %d queries, want watermark probe then one combined statement", len(probe.queries))
			}
		})
	}
}

func TestAggregateWinnersInventoryRequiresFreshWatermark(t *testing.T) {
	t.Parallel()

	freshAt := aggregateWinnersTestNow().Add(-time.Minute)
	probe := &winnersUnavailableQueryer{watermarkRows: [][]any{{freshAt}}}
	probe.onRow = func(string) db.Row { return aggregateWinnersCountRow() }
	probe.onRows = func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() }
	store := NewPostgresAggregateStoreWithReadStore(probe, true)
	store.Now = aggregateWinnersTestNow

	page, err := store.SupplyChainImpactInventory(t.Context(), AggregateFilter{Status: "affected_exact"}, InventoryByStatus, 10, 0)
	if err != nil {
		t.Fatalf("fresh inventory err = %v, want page", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].Count != 3 {
		t.Fatalf("fresh inventory rows = %+v, want one bucket of 3", page.Rows)
	}
	if page.Freshness.State != packet.FreshnessLabelFresh {
		t.Fatalf("fresh inventory freshness state = %q, want fresh", page.Freshness.State)
	}

	stale := &winnersUnavailableQueryer{watermarkRows: [][]any{{aggregateWinnersTestNow().Add(-10 * time.Minute)}}}
	stale.onRows = func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() }
	staleStore := NewPostgresAggregateStoreWithReadStore(stale, true)
	staleStore.Now = aggregateWinnersTestNow
	if _, err := staleStore.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("stale inventory err = %v, want ErrAggregateReadModelUnavailable", err)
	}

	absent := &winnersUnavailableQueryer{}
	absent.onRows = func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() }
	absentStore := NewPostgresAggregateStoreWithReadStore(absent, true)
	absentStore.Now = aggregateWinnersTestNow
	if _, err := absentStore.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("absent-model inventory err = %v, want ErrAggregateReadModelUnavailable", err)
	}
}

func TestAggregateLegacyPathSkipsWatermarkProbe(t *testing.T) {
	t.Parallel()

	probe := &cov7StoreQueryer{
		onRow:  func(string) db.Row { return aggregateWinnersCountRow() },
		onRows: func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() },
	}
	legacy := NewPostgresAggregateStoreWithReadStore(probe, false)
	legacy.Now = aggregateWinnersTestNow

	count, err := legacy.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{Status: "affected_exact"})
	if err != nil {
		t.Fatalf("legacy count err = %v, want envelope", err)
	}
	if count.Freshness.State != packet.FreshnessLabelFresh {
		t.Fatalf("legacy count freshness state = %q, want fresh (live facts are always current)", count.Freshness.State)
	}
	if count.Freshness.LatestObservedAt != "" {
		t.Fatalf("legacy count observed_at = %q, want empty (no watermark on the live read)", count.Freshness.LatestObservedAt)
	}
	for _, query := range probe.queries {
		if query == SelectWinnersWatermarkQuery {
			t.Fatalf("legacy count issued a watermark probe, want exactly one legacy statement")
		}
	}
	if len(probe.queries) != 1 {
		t.Fatalf("legacy count issued %d queries, want exactly one legacy statement", len(probe.queries))
	}
}

// TestAggregateWinnersProbeFailureLogsOnce pins the operator signal for a
// failed aggregate watermark probe: one WARN line with the bounded event
// name, while the call answers ErrAggregateReadModelUnavailable (exact
// totals cannot be served marked degraded the way grouped buckets can).
func TestAggregateWinnersProbeFailureLogsOnce(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	probe := &winnersUnavailableQueryer{watermarkErr: errors.New("conn reset by peer")}
	probe.onRow = func(string) db.Row { return aggregateWinnersCountRow() }
	probe.onRows = func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() }
	store := NewPostgresAggregateStoreWithReadStore(probe, true)
	store.Now = aggregateWinnersTestNow
	store.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if _, err := store.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{Status: "affected_exact"}); !errors.Is(err, ErrAggregateReadModelUnavailable) {
		t.Fatalf("count err = %v, want ErrAggregateReadModelUnavailable", err)
	}
	line := logs.String()
	for _, want := range []string{`"level":"WARN"`, `"event_name":"` + aggregateWinnersProbeFailedEvent + `"`, "conn reset by peer"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log = %s, want %s", line, want)
		}
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("log lines = %d, want exactly one WARN line", strings.Count(line, "\n"))
	}
}

func TestAggregateWinnersStatementsSkipLegacyCTE(t *testing.T) {
	t.Parallel()

	freshAt := aggregateWinnersTestNow().Add(-time.Minute)
	probe := &winnersUnavailableQueryer{watermarkRows: [][]any{{freshAt}}}
	probe.onRow = func(string) db.Row { return aggregateWinnersCountRow() }
	probe.onRows = func(string) (db.Rows, error) { return aggregateWinnersInventoryRows() }
	store := NewPostgresAggregateStoreWithReadStore(probe, true)
	store.Now = aggregateWinnersTestNow

	if _, err := store.CountSupplyChainImpactFindings(t.Context(), AggregateFilter{}); err != nil {
		t.Fatalf("count err = %v, want envelope", err)
	}
	if _, err := store.SupplyChainImpactInventory(t.Context(), AggregateFilter{}, InventoryByStatus, 10, 0); err != nil {
		t.Fatalf("inventory err = %v, want page", err)
	}
	for _, query := range probe.queries {
		if query == SelectWinnersWatermarkQuery {
			continue
		}
		if !strings.Contains(query, "supply_chain_impact_canonical_winners") {
			t.Fatalf("winners statement never scans the winners table:\n%s", query)
		}
		if strings.Contains(query, "canonical_facts") {
			t.Fatalf("winners statement still reads the legacy CTE:\n%s", query)
		}
		if strings.Contains(query, "DISTINCT ON") {
			t.Fatalf("winners statement still carries the request-time DISTINCT ON dedup:\n%s", query)
		}
	}
}

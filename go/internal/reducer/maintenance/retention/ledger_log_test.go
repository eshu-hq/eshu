// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retention

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// logRecords decodes one JSON log record per line.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func runOnceWithLog(t *testing.T, result Result) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	runner := &Runner{
		Pruner: &fakeGenerationRetentionPruner{results: []Result{result}},
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	return logRecords(t, &buf)
}

// TestGenerationRetentionLogCarriesLedgerCounts is P9 of arbiter ruling
// arb-7127-3d: the retention cycle log carries the four changed-since ledger
// counts the batch deleted.
func TestGenerationRetentionLogCarriesLedgerCounts(t *testing.T) {
	ledger := map[string]int64{
		"changed_since_links": 3, "changed_since_link_deltas": 250,
		"changed_since_link_bucket_counts": 7, "changed_since_activations": 2,
	}
	records := runOnceWithLog(t, Result{
		GenerationsPruned: 2,
		RowsPruned:        map[string]int64{"fact_records": 10},
		LedgerRowsPruned:  ledger,
		LedgerRowsCounted: ledger,
		LockedScopeRows:   2,
	})
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1 (no WARN when the counts agree): %v", len(records), records)
	}
	// PC4 of arbiter ruling arb-7127-3d-c: the scope rows held at the delete.
	if n, _ := records[0]["locked_scope_rows"].(float64); n != 2 {
		t.Errorf("locked_scope_rows = %v, want 2: %v", records[0]["locked_scope_rows"], records[0])
	}
	got, ok := records[0]["changed_since_ledger_rows_pruned"].(map[string]any)
	if !ok {
		t.Fatalf("completion log has no changed_since_ledger_rows_pruned: %v", records[0])
	}
	for table, want := range ledger {
		if n, _ := got[table].(float64); int64(n) != want {
			t.Errorf("changed_since_ledger_rows_pruned[%s] = %v, want %d", table, got[table], want)
		}
	}
}

// TestGenerationRetentionWarnsWhenLedgerDeleteDiffersFromCount is the WARN of
// arbiter ruling arb-7127-3d: a link that commits between the pre-count and the
// delete makes them differ, and the log names both figures.
func TestGenerationRetentionWarnsWhenLedgerDeleteDiffersFromCount(t *testing.T) {
	records := runOnceWithLog(t, Result{
		GenerationsPruned: 1,
		RowsPruned:        map[string]int64{},
		LedgerRowsPruned:  map[string]int64{"changed_since_links": 1, "changed_since_link_deltas": 5},
		LedgerRowsCounted: map[string]int64{"changed_since_links": 2, "changed_since_link_deltas": 10},
	})
	var warn map[string]any
	for _, record := range records {
		if record["level"] == "WARN" {
			warn = record
		}
	}
	if warn == nil {
		t.Fatalf("no WARN record: %v", records)
	}
	for _, key := range []string{"changed_since_ledger_rows_pruned", "changed_since_ledger_rows_counted"} {
		if _, ok := warn[key].(map[string]any); !ok {
			t.Errorf("WARN record lacks %s: %v", key, warn)
		}
	}
}

// overLimitRun runs one cycle with instruments and a JSON logger and returns
// the over-limit counter's value (0 when never emitted) and the cycle log.
func overLimitRun(t *testing.T, result Result) (int64, map[string]any) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	var buf bytes.Buffer
	runner := &Runner{
		Pruner:      &fakeGenerationRetentionPruner{results: []Result{result}},
		Logger:      slog.New(slog.NewJSONHandler(&buf, nil)),
		Instruments: instruments,
	}
	if _, err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var batches int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_generation_retention_over_limit_batches_total" {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range sum.DataPoints {
					batches += dp.Value
				}
			}
		}
	}
	records := logRecords(t, &buf)
	if len(records) == 0 {
		t.Fatal("no cycle log")
	}
	return batches, records[0]
}

// TestGenerationRetentionOverLimitBatchTelemetry is PB6 of arbiter ruling
// arb-7127-3d-b: a batch of one generation over BatchRowLimit (admitted alone
// because of its changed-since ledger rows) counts once on
// eshu_dp_generation_retention_over_limit_batches_total and logs
// rows_over_batch_row_limit; a normal batch does neither.
func TestGenerationRetentionOverLimitBatchTelemetry(t *testing.T) {
	batches, record := overLimitRun(t, Result{GenerationsPruned: 1, RowsPruned: map[string]int64{}, RowsOverLimit: 7})
	if batches != 1 {
		t.Fatalf("over-limit batch counted %d, want 1", batches)
	}
	if got, _ := record["rows_over_batch_row_limit"].(float64); got != 7 {
		t.Fatalf("rows_over_batch_row_limit = %v, want 7: %v", record["rows_over_batch_row_limit"], record)
	}
	batches, record = overLimitRun(t, Result{GenerationsPruned: 3, RowsPruned: map[string]int64{}})
	if batches != 0 {
		t.Fatalf("normal batch counted %d over-limit batches, want 0", batches)
	}
	if got, _ := record["rows_over_batch_row_limit"].(float64); got != 0 {
		t.Fatalf("normal batch logged rows_over_batch_row_limit = %v, want 0", record["rows_over_batch_row_limit"])
	}
}

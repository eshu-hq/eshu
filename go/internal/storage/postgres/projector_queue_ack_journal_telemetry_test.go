// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ajJournalCounts returns eshu_dp_changed_since_ack_journal_total by outcome.
func ajJournalCounts(t *testing.T, reader sdkmetric.Reader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_changed_since_ack_journal_total" {
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

// TestProjectorAckJournalRecordsOutcome proves each committed flag-on Ack
// counts its journal outcome once on eshu_dp_changed_since_ack_journal_total,
// a gate skip also logs the scope and generation for the operator (a sampled
// line: TestProjectorAckGateSkipLogIsSampled), and a
// flag-off Ack records nothing (#7127 PR-3b).
func TestProjectorAckJournalRecordsOutcome(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	for _, tc := range []struct {
		journal bool
		row     []any
		want    string
	}{
		{journal: true, row: []any{int64(1), int64(1), true}, want: ackJournalOutcomeJournaled},
		{journal: true, row: []any{int64(1), int64(0), true}, want: ackJournalOutcomeExists},
		{journal: true, row: []any{int64(1), int64(0), false}, want: ackJournalOutcomeGateSkipped},
		{journal: false, want: ""},
	} {
		logs.Reset()
		reader := sdkmetric.NewManualReader()
		instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
		if err != nil {
			t.Fatalf("NewInstruments: %v", err)
		}
		q := ackJournalQueue(&ackStatementDB{journalRow: tc.row}, tc.journal)
		q.Instruments = instruments
		// A fresh sampler per case: the process sampler may have logged a
		// gate skip from another test inside its interval.
		q.ackSkipLog = newLogSampler(ackSkipLogInterval, time.Now)
		if err := q.Ack(context.Background(), ackWorkForLockTimeoutTest(), runtime.Result{}); err != nil {
			t.Fatalf("Ack: %v", err)
		}
		counts := ajJournalCounts(t, reader)
		if tc.want == "" {
			if len(counts) != 0 {
				t.Fatalf("flag-off Ack recorded %v", counts)
			}
			continue
		}
		if len(counts) != 1 || counts[tc.want] != 1 {
			t.Fatalf("outcome counts = %v, want one %q", counts, tc.want)
		}
		skipLogged := strings.Contains(logs.String(), `"outcome":"gate_skipped"`) &&
			strings.Contains(logs.String(), `"scope_id":"scope-1"`) && strings.Contains(logs.String(), `"generation_id":"generation-1"`)
		if skipLogged != (tc.want == ackJournalOutcomeGateSkipped) {
			t.Fatalf("outcome %s: gate-skip log present = %v; logs:\n%s", tc.want, skipLogged, logs.String())
		}
	}
}

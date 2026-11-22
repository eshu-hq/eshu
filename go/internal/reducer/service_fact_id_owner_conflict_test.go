// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type zeroRowsExecer struct{}

func (zeroRowsExecer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return zeroRowsResult{}, nil
}

type zeroRowsResult struct{}

func (zeroRowsResult) LastInsertId() (int64, error) { return 0, nil }
func (zeroRowsResult) RowsAffected() (int64, error) { return 0, nil }

// TestWorkloadIdentityWriterRefusesARowAnotherScopeHolds is the #11213
// regression through a real single-row writer: the conflict WHERE skipped the
// row (zero rows affected because another scope holds the fact id), and the
// write must fail with the typed, non-retryable error, never read as written.
func TestWorkloadIdentityWriterRefusesARowAnotherScopeHolds(t *testing.T) {
	t.Parallel()

	writer := PostgresWorkloadIdentityWriter{DB: zeroRowsExecer{}, Now: func() time.Time { return time.Unix(0, 0) }}
	_, err := writer.WriteWorkloadIdentity(context.Background(), WorkloadIdentityWrite{
		IntentID: "intent-1", ScopeID: "scope-b", GenerationID: "gen-1", SourceSystem: "git",
	})
	var conflict *factwrite.OwnerConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("WriteWorkloadIdentity() error = %v, want *factwrite.OwnerConflictError", err)
	}
	if conflict.Writer != factwrite.WriterSingle || conflict.Samples[0].FactID != "intent-1" || conflict.Samples[0].IncomingScopeID != "scope-b" {
		t.Fatalf("conflict = %+v", conflict)
	}
	if reducerExecutionFailureClass(err) != telemetry.RefreshSkipReasonFactIDOwnerConflict {
		t.Fatalf("failure class = %q, want %q", reducerExecutionFailureClass(err), telemetry.RefreshSkipReasonFactIDOwnerConflict)
	}
}

// TestOwnerConflictClassAndWriterLabelsMatchTelemetry keeps the leaf factwrite
// package's strings in lockstep with the telemetry contract it cannot import.
func TestOwnerConflictClassAndWriterLabelsMatchTelemetry(t *testing.T) {
	t.Parallel()

	if factwrite.OwnerConflictFailureClass != telemetry.RefreshSkipReasonFactIDOwnerConflict {
		t.Errorf("factwrite class %q != telemetry %q", factwrite.OwnerConflictFailureClass, telemetry.RefreshSkipReasonFactIDOwnerConflict)
	}
	for factwriteLabel, telemetryLabel := range map[string]string{
		factwrite.WriterSingle:    telemetry.FactIDOwnerConflictWriterReducerSingle,
		factwrite.WriterBatch:     telemetry.FactIDOwnerConflictWriterReducerBatch,
		factwrite.WriterVersioned: telemetry.FactIDOwnerConflictWriterReducerVersioned,
	} {
		if factwriteLabel != telemetryLabel {
			t.Errorf("factwrite writer label %q != telemetry %q", factwriteLabel, telemetryLabel)
		}
	}
}

// TestServiceCountsAFactIDOwnerConflictByWriter: the reducer service increments
// the shared eshu_dp_fact_id_scope_conflicts_total with reason scope and the
// writer label from the typed error, and only for that error.
func TestServiceCountsAFactIDOwnerConflictByWriter(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	service := Service{Instruments: instruments}
	conflict := &factwrite.OwnerConflictError{Writer: factwrite.WriterBatch, Count: 3}
	service.recordReducerFactIDOwnerConflict(context.Background(), fmt.Errorf("write facts: %w", conflict))
	service.recordReducerFactIDOwnerConflict(context.Background(), errors.New("an unrelated failure"))
	Service{}.recordReducerFactIDOwnerConflict(context.Background(), conflict) // nil instruments: no panic

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_fact_id_scope_conflicts_total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			for _, point := range sum.DataPoints {
				reason, _ := point.Attributes.Value("reason")
				writerLabel, _ := point.Attributes.Value("writer")
				got[reason.AsString()+"/"+writerLabel.AsString()] += point.Value
			}
		}
	}
	if len(got) != 1 || got["scope/reducer_batch"] != 3 {
		t.Fatalf("counter = %v, want exactly {scope/reducer_batch: 3}", got)
	}
}

// TestBothReducerFailurePathsCountOwnerConflicts pins the wiring: the sequential
// path (service.go) and the batch path (service_batch.go) both record an owner
// conflict beside the handler-timeout counter, so neither path can drop it.
func TestBothReducerFailurePathsCountOwnerConflicts(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"service.go", "service_batch.go"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if !strings.Contains(string(body), "s.recordReducerFactIDOwnerConflict(ctx, err)") {
			t.Errorf("%s no longer records a fact id owner conflict on a failed intent", file)
		}
	}
}

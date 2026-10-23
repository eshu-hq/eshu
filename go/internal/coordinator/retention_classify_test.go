// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/work/items"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workItemRetentionPassesMetric = "eshu_dp_workflow_coordinator_work_item_retention_passes_total"

// retentionPass is what one runWorkItemRetention call reported through the
// production metrics and the structured log.
type retentionPass struct {
	logs    string
	metrics metricdata.ResourceMetrics
}

// passes returns the passes_total count for one outcome, or 0 when the pass
// recorded no data point for it.
func (p retentionPass) passes(t *testing.T, outcome string) int64 {
	t.Helper()
	want := map[string]string{"outcome": outcome}
	for _, scopeMetrics := range p.metrics.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != workItemRetentionPassesMetric {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricRecord.Name, metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				if coordinatorAttrsMatch(dp.Attributes.ToSlice(), want) {
					return dp.Value
				}
			}
		}
	}
	return 0
}

// runClassifiedRetentionPass runs one pass against store with the production
// metrics recorder and a JSON logger.
func runClassifiedRetentionPass(t *testing.T, ctx context.Context, store WorkItemRetentionStore, policy workflow.WorkItemRetentionPolicy) retentionPass {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	now := time.Date(2026, time.October, 23, 12, 0, 0, 0, time.UTC)
	retention := enabledRetention()
	retention.Policy = policy
	service, _ := activeRetentionService(now, retention)
	var logs bytes.Buffer
	service.WorkItemRetention = store
	service.Metrics = recorder
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	service.runWorkItemRetention(ctx)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return retentionPass{logs: logs.String(), metrics: rm}
}

// assertFailedPass checks that a pass was reported as a failure: one ERROR
// log line, passes_total{outcome="error"} incremented, nothing counted as
// canceled.
func assertFailedPass(t *testing.T, pass retentionPass, wantInLog string) {
	t.Helper()
	if got := pass.passes(t, workItemRetentionOutcomeError); got != 1 {
		t.Fatalf("passes{error} = %d, want 1:\n%s", got, pass.logs)
	}
	if got := pass.passes(t, workItemRetentionOutcomeCanceled); got != 0 {
		t.Fatalf("passes{canceled} = %d, want 0 for a genuine failure", got)
	}
	for _, want := range []string{`"level":"ERROR"`, `"msg":"workflow work item retention pass failed"`, `"outcome":"error"`, wantInLog} {
		if !strings.Contains(pass.logs, want) {
			t.Fatalf("failed pass log missing %s:\n%s", want, pass.logs)
		}
	}
	if strings.Contains(pass.logs, "retention pass canceled") {
		t.Fatalf("genuine failure logged as a canceled pass:\n%s", pass.logs)
	}
}

// TestRunWorkItemRetentionKeepsServerTimeoutsAsErrorsDuringShutdown proves a
// batch that fails with a Postgres lock or statement timeout is a failure
// even when coordinator shutdown has already canceled the pass context. The
// error, not the context, decides the outcome.
func TestRunWorkItemRetentionKeepsServerTimeoutsAsErrorsDuringShutdown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  *pgconn.PgError
	}{
		{name: "lock timeout", err: &pgconn.PgError{Severity: "ERROR", Code: "55P03", Message: "canceling statement due to lock timeout"}},
		{name: "statement timeout", err: &pgconn.PgError{Severity: "ERROR", Code: "57014", Message: "canceling statement due to statement timeout"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Wrapped the way items.Sweeper wraps a failed batch statement.
			batchErr := fmt.Errorf("workflow work item retention batch after %q: %w", "wi-0042",
				fmt.Errorf("delete window: %w", tc.err))
			store := &fakeWorkItemRetention{
				result:  workflow.WorkItemRetentionResult{Batches: 2, DeletedCompleted: 7},
				err:     batchErr,
				sizeErr: context.Canceled,
			}

			pass := runClassifiedRetentionPass(t, cancelledContext(), store, workflow.DefaultWorkItemRetentionPolicy())

			assertFailedPass(t, pass, "SQLSTATE "+tc.err.Code)
			if store.sizeReads != 1 {
				t.Fatalf("size reads = %d, want 1 (a failed pass reads the size like any failure)", store.sizeReads)
			}
			if !strings.Contains(pass.logs, `"table_size_error":"context canceled"`) {
				t.Fatalf("failed pass log missing the size read error:\n%s", pass.logs)
			}
		})
	}
}

// TestRunWorkItemRetentionTreatsWrappedCancellationAsCanceled proves a pass
// whose store error wraps context.Canceled while the pass context is done is
// counted as canceled, logged at INFO, and skips the size read.
func TestRunWorkItemRetentionTreatsWrappedCancellationAsCanceled(t *testing.T) {
	t.Parallel()

	store := &fakeWorkItemRetention{
		result: workflow.WorkItemRetentionResult{Batches: 1, DeletedFailedTerminal: 2},
		err: fmt.Errorf("workflow work item retention batch after %q: %w", "wi-0042",
			fmt.Errorf("delete window: %w", context.Canceled)),
	}

	pass := runClassifiedRetentionPass(t, cancelledContext(), store, workflow.DefaultWorkItemRetentionPolicy())

	if got := pass.passes(t, workItemRetentionOutcomeCanceled); got != 1 {
		t.Fatalf("passes{canceled} = %d, want 1:\n%s", got, pass.logs)
	}
	if got := pass.passes(t, workItemRetentionOutcomeError); got != 0 {
		t.Fatalf("passes{error} = %d, want 0 for a shutdown", got)
	}
	if store.sizeReads != 0 {
		t.Fatalf("size reads = %d, want 0 (a canceled pass skips the size read)", store.sizeReads)
	}
	for _, want := range []string{`"level":"INFO"`, `"msg":"workflow work item retention pass canceled"`, `"outcome":"canceled"`} {
		if !strings.Contains(pass.logs, want) {
			t.Fatalf("canceled pass log missing %s:\n%s", want, pass.logs)
		}
	}
	if strings.Contains(pass.logs, `"level":"ERROR"`) {
		t.Fatalf("canceled pass logged an ERROR:\n%s", pass.logs)
	}
}

// TestRunWorkItemRetentionTreatsUnrequestedCancellationAsError proves a
// context.Canceled error that the pass context did not produce is a failure:
// only coordinator shutdown makes a pass canceled.
func TestRunWorkItemRetentionTreatsUnrequestedCancellationAsError(t *testing.T) {
	t.Parallel()

	store := &fakeWorkItemRetention{err: fmt.Errorf("delete window: %w", context.Canceled)}

	pass := runClassifiedRetentionPass(t, context.Background(), store, workflow.DefaultWorkItemRetentionPolicy())

	assertFailedPass(t, pass, "context canceled")
	if store.sizeReads != 1 {
		t.Fatalf("size reads = %d, want 1", store.sizeReads)
	}
}

// unusedRetentionDatabase satisfies items.Database for a pre-flight check
// that must fail before any batch runs. Begin is not implemented: calling it
// panics, which a test would report.
type unusedRetentionDatabase struct {
	items.Database
}

func (unusedRetentionDatabase) QueryContext(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("unexpected query")
}

// TestRunWorkItemRetentionKeepsPreflightErrorsAsErrorsDuringShutdown proves
// the production sweeper's pre-flight rejections (no database, invalid
// policy) stay failures when the pass context is already done.
func TestRunWorkItemRetentionKeepsPreflightErrorsAsErrorsDuringShutdown(t *testing.T) {
	t.Parallel()

	invalid := workflow.DefaultWorkItemRetentionPolicy()
	invalid.BatchSize = 0
	cases := []struct {
		name      string
		store     WorkItemRetentionStore
		policy    workflow.WorkItemRetentionPolicy
		wantInLog string
	}{
		{name: "nil database", store: items.NewSweeper(nil), policy: workflow.DefaultWorkItemRetentionPolicy(), wantInLog: "database is required"},
		{name: "invalid policy", store: items.NewSweeper(unusedRetentionDatabase{}), policy: invalid, wantInLog: "batch size 0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pass := runClassifiedRetentionPass(t, cancelledContext(), tc.store, tc.policy)
			assertFailedPass(t, pass, tc.wantInLog)
		})
	}
}

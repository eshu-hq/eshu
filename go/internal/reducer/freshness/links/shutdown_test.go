// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cancelingLinker returns err from LinkNext after cancelling the runner's
// parent context when cancel is set: the shape of a reducer shutdown that
// lands while a link transaction is in flight.
type cancelingLinker struct {
	fakeLinker
	cancel context.CancelFunc
	err    error
}

func (c *cancelingLinker) LinkNext(_ context.Context, scopeID string) (store.LinkResult, error) {
	c.mu.Lock()
	c.calls[scopeID]++
	c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	return store.LinkResult{}, c.err
}

// ctxJournal is a fakeJournal whose reads fail once their context is done,
// as the Postgres store's do.
type ctxJournal struct{ fakeJournal }

func (j *ctxJournal) Stats(ctx context.Context) (store.LedgerStats, error) {
	return store.LedgerStats{}, ctx.Err()
}

func (j *ctxJournal) OrphanScopes(ctx context.Context, limit int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return j.fakeJournal.OrphanScopes(ctx, limit)
}

// observedRunner returns a runner over linker with a manual metric reader and
// a JSON logger at DEBUG writing to the returned buffer.
func observedRunner(t *testing.T, linker Linker, scopes ...string) (*Runner, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("links-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Runner{Linker: linker, Journal: &ctxJournal{fakeJournal{scopes: scopes}}, Instruments: instruments, Logger: logger}, reader, &logs
}

// counterPoints sums a counter's data points by the value of one label.
func counterPoints(t *testing.T, reader *sdkmetric.ManualReader, name, label string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want an int64 counter", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				value, _ := point.Attributes.Value(attribute.Key(label))
				out[value.AsString()] += point.Value
			}
		}
	}
	return out
}

// logLevels returns the level of every log line whose message starts with
// msgPrefix.
func logLevels(t *testing.T, logs *bytes.Buffer, msgPrefix string) []string {
	t.Helper()
	var levels []string
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry struct{ Level, Msg string }
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		if strings.HasPrefix(entry.Msg, msgPrefix) {
			levels = append(levels, entry.Level)
		}
	}
	return levels
}

// TestShutdownDuringALinkIsNotAFailure is review thread PRRT_kwDOSU-uHM6mZfHb
// on #7307: when the runner's parent context is cancelled while a link is in
// flight, the link's error is the shutdown, not a link failure. It must not
// be recorded on the cursor (the cursor did not move and the next cycle
// retries), must not log an ERROR, and must not count outcome=failed or a
// failure class; it counts outcome=canceled.
func TestShutdownDuringALinkIsNotAFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"counting error after the statement ran", &store.FailureError{
			Class: store.FailureInternal, ScopeID: "a", ActivationSeq: 4, Err: fmt.Errorf("link: %w", context.Canceled),
		}},
		{"error before the statement ran", fmt.Errorf("begin link transaction: %w", context.Canceled)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			linker := &cancelingLinker{fakeLinker: fakeLinker{calls: map[string]int{}}, cancel: cancel, err: tc.err}
			runner, reader, logs := observedRunner(t, linker, "a")

			result, _ := runner.RunOnce(ctx)

			if len(linker.recorded) != 0 {
				t.Fatalf("RecordFailure called %d times on shutdown; the cursor did not move, nothing to record", len(linker.recorded))
			}
			if result.Failures != 0 {
				t.Fatalf("RunOnce = %+v, want no failure for a shutdown", result)
			}
			if levels := logLevels(t, logs, "changed-since"); strings.Contains(strings.Join(levels, ","), "ERROR") {
				t.Fatalf("shutdown logged at %v, want no ERROR\n%s", levels, logs.String())
			}
			outcomes := counterPoints(t, reader, "eshu_dp_changed_since_links_total", telemetry.MetricDimensionOutcome)
			if outcomes["failed"] != 0 || outcomes["canceled"] != 1 {
				t.Fatalf("links_total by outcome = %v, want canceled 1 and no failed", outcomes)
			}
			if classes := counterPoints(t, reader, "eshu_dp_changed_since_link_failures_total", telemetry.MetricDimensionFailureClass); len(classes) != 0 {
				t.Fatalf("link_failures_total = %v, want none for a shutdown", classes)
			}
		})
	}
}

// TestLinkDeadlineWithALiveParentStillCounts guards the other side of the
// shutdown rule: the link's own transaction deadline or statement timeout
// ends in a context error too, but the runner's parent context is live, so
// it is a real counting failure.
func TestLinkDeadlineWithALiveParentStillCounts(t *testing.T) {
	failure := &store.FailureError{
		Class: store.FailureStatementTimeout, ScopeID: "a", ActivationSeq: 4,
		Err: fmt.Errorf("link transaction deadline: %w", context.DeadlineExceeded),
	}
	linker := &cancelingLinker{fakeLinker: fakeLinker{calls: map[string]int{}}, err: failure}
	runner, reader, logs := observedRunner(t, linker, "a")

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(linker.recorded) != 1 || !errors.Is(linker.recorded[0], context.DeadlineExceeded) {
		t.Fatalf("recorded = %v, want the deadline failure counted once", linker.recorded)
	}
	if result.Failures != 1 {
		t.Fatalf("RunOnce = %+v, want one failure", result)
	}
	if levels := logLevels(t, logs, "changed-since link failed"); len(levels) != 1 || levels[0] != "ERROR" {
		t.Fatalf("failure log levels = %v, want one ERROR\n%s", levels, logs.String())
	}
	outcomes := counterPoints(t, reader, "eshu_dp_changed_since_links_total", telemetry.MetricDimensionOutcome)
	if outcomes["failed"] != 1 || outcomes["canceled"] != 0 {
		t.Fatalf("links_total by outcome = %v, want failed 1", outcomes)
	}
	classes := counterPoints(t, reader, "eshu_dp_changed_since_link_failures_total", telemetry.MetricDimensionFailureClass)
	if classes[string(store.FailureStatementTimeout)] != 1 {
		t.Fatalf("link_failures_total = %v, want statement_timeout 1", classes)
	}
}

// recordingLinker fails LinkNext with a genuine counting failure while the
// runner's context is live, then makes RecordFailure fail: after cancelling
// the parent context when cancel is set (a shutdown during the short record
// transaction), or with recordErr against a live parent.
type recordingLinker struct {
	fakeLinker
	cancel    context.CancelFunc
	recordErr error
}

func (l *recordingLinker) LinkNext(_ context.Context, scopeID string) (store.LinkResult, error) {
	l.mu.Lock()
	l.calls[scopeID]++
	l.mu.Unlock()
	return store.LinkResult{}, &store.FailureError{
		Class: store.FailureSQLError, ScopeID: scopeID, GenerationID: "g1", ActivationSeq: 9, Err: errors.New("deadlock detected"),
	}
}

func (l *recordingLinker) RecordFailure(ctx context.Context, _ *store.FailureError, _ int) (store.FailureRecord, error) {
	if l.cancel != nil {
		l.cancel()
		return store.FailureRecord{}, fmt.Errorf("begin record transaction: %w", ctx.Err())
	}
	return store.FailureRecord{}, l.recordErr
}

// TestShutdownDuringTheFailureRecordIsNotAFailure is review thread
// PRRT_kwDOSU-uHM6mZ2t1 on #7307: the link failed for real while the runner
// was live, but the reducer shut down during the short transaction that
// records the attempt on the cursor. Nothing was recorded and the cursor did
// not move, so the outcome is canceled, not failed, and there is no ERROR.
// The failure itself was observed, so its failure class still counts.
func TestShutdownDuringTheFailureRecordIsNotAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	linker := &recordingLinker{fakeLinker: fakeLinker{calls: map[string]int{}}, cancel: cancel}
	runner, reader, logs := observedRunner(t, linker, "a")

	result, _ := runner.RunOnce(ctx)

	if result.Failures != 0 {
		t.Fatalf("RunOnce = %+v, want no failure: the record was cut short by shutdown", result)
	}
	if levels := logLevels(t, logs, "changed-since"); strings.Contains(strings.Join(levels, ","), "ERROR") {
		t.Fatalf("shutdown during the record logged at %v, want no ERROR\n%s", levels, logs.String())
	}
	outcomes := counterPoints(t, reader, "eshu_dp_changed_since_links_total", telemetry.MetricDimensionOutcome)
	if outcomes["failed"] != 0 || outcomes["canceled"] != 1 {
		t.Fatalf("links_total by outcome = %v, want canceled 1 and no failed", outcomes)
	}
	classes := counterPoints(t, reader, "eshu_dp_changed_since_link_failures_total", telemetry.MetricDimensionFailureClass)
	if classes[string(store.FailureSQLError)] != 1 {
		t.Fatalf("link_failures_total = %v, want the observed sql_error counted once", classes)
	}
}

// TestFailureRecordErrorWithALiveParentStillCounts keeps the other side: a
// record error while the runner is live is a real failure to report.
func TestFailureRecordErrorWithALiveParentStillCounts(t *testing.T) {
	linker := &recordingLinker{fakeLinker: fakeLinker{calls: map[string]int{}}, recordErr: errors.New("connection reset by peer")}
	runner, reader, logs := observedRunner(t, linker, "a")

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Failures != 1 {
		t.Fatalf("RunOnce = %+v, want one failure", result)
	}
	if levels := logLevels(t, logs, "changed-since link failure could not be recorded"); len(levels) != 1 || levels[0] != "ERROR" {
		t.Fatalf("record-error log levels = %v, want one ERROR\n%s", levels, logs.String())
	}
	outcomes := counterPoints(t, reader, "eshu_dp_changed_since_links_total", telemetry.MetricDimensionOutcome)
	if outcomes["failed"] != 1 || outcomes["canceled"] != 0 {
		t.Fatalf("links_total by outcome = %v, want failed 1", outcomes)
	}
}

// recordedThenShutdownLinker records the failure (the cursor write commits,
// here as a poisoning) and the reducer shuts down right after.
type recordedThenShutdownLinker struct {
	recordingLinker
}

func (l *recordedThenShutdownLinker) RecordFailure(context.Context, *store.FailureError, int) (store.FailureRecord, error) {
	l.cancel()
	return store.FailureRecord{Counted: true, Attempts: 5, Poisoned: true}, nil
}

// TestARecordedPoisoningAtShutdownStillLogsError guards against hiding real
// events behind the shutdown rule: once the cursor write committed, the
// poisoning happened, and it keeps its ERROR line and its outcome even if the
// reducer stops a moment later.
func TestARecordedPoisoningAtShutdownStillLogsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	linker := &recordedThenShutdownLinker{recordingLinker{fakeLinker: fakeLinker{calls: map[string]int{}}, cancel: cancel}}
	runner, reader, logs := observedRunner(t, linker, "a")

	result, _ := runner.RunOnce(ctx)

	if result.Poisoned != 1 {
		t.Fatalf("RunOnce = %+v, want the recorded poisoning", result)
	}
	if levels := logLevels(t, logs, "changed-since link poisoned"); len(levels) != 1 || levels[0] != "ERROR" {
		t.Fatalf("poisoning log levels = %v, want one ERROR\n%s", levels, logs.String())
	}
	if outcomes := counterPoints(t, reader, "eshu_dp_changed_since_links_total", telemetry.MetricDimensionOutcome); outcomes["poisoned"] != 1 {
		t.Fatalf("links_total by outcome = %v, want poisoned 1", outcomes)
	}
}

// TestLedgerReadsAtShutdownDoNotLogError covers the ledger reads of a cycle
// (the orphan scan here): on a done runner context they fail with the
// shutdown itself and log at INFO, not ERROR.
func TestLedgerReadsAtShutdownDoNotLogError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner, _, logs := observedRunner(t, &cancelingLinker{fakeLinker: fakeLinker{calls: map[string]int{}}}, "a")

	_, _ = runner.RunOnce(ctx)

	levels := logLevels(t, logs, "changed-since orphan scan failed")
	if len(levels) != 1 || levels[0] != "INFO" {
		t.Fatalf("orphan scan at shutdown logged at %v, want one INFO\n%s", levels, logs.String())
	}
}
